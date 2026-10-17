package main

import (
	"fmt"
	"sort"
	"strings"
)

// Comparison of operations and schemas for the spec sync summary. It is a
// port of describe_change and diff_schema of the Python reference
// (sdk-python codegen/spec_sync.py): changes are compared field by field,
// nested objects, arrays, types, enums, nullability and required fields
// included, and every change that can break code written against the SDK is
// flagged as breaking.
//
// The Python reference dereferences whole operations up front; here
// references are followed lazily while comparing, which gives the same
// result without copying recursive schemas.

// maxNotes caps the notes of one operation so that a huge change stays
// readable.
const maxNotes = 25

// unchangedNote is what describeChange returns when only documentation
// changed.
const unchangedNote = "descriptions, examples or other details changed"

// docKeys document a schema without changing what is sent or received.
var docKeys = map[string]bool{
	"description": true, "summary": true, "title": true,
	"example": true, "examples": true, "externalDocs": true,
}

// note is one difference and whether it can break code using the SDK.
type note struct {
	text     string
	breaking bool
}

// syncOp is one operation of a document as the sync sees it.
type syncOp struct {
	file   string // "verification.json" or "account.json"
	doc    *Doc
	key    string // "GET /v3/session/" or "account GET /v2/..."
	method string // "GET"
	path   string
	// node is the operation with the path-level parameters prepended, as
	// the Python reference compares it.
	node *Node
	// raw and item are the operation and its path item as in the document.
	raw  *Node
	item *Node
}

// opSet holds the operations of the documents in document order.
type opSet struct {
	keys []string
	byKey map[string]*syncOp
}

func (s *opSet) add(op *syncOp) {
	if s.byKey == nil {
		s.byKey = map[string]*syncOp{}
	}
	if _, dup := s.byKey[op.key]; !dup {
		s.keys = append(s.keys, op.key)
	}
	s.byKey[op.key] = op
}

// collectOps adds the operations of doc (the HTTP methods the generator
// reads) to set.
func collectOps(set *opSet, file string, doc *Doc, prefix string) {
	paths := doc.Root.Get("paths")
	for _, path := range paths.ObjKeys() {
		item := paths.Get(path)
		for _, m := range httpMethods {
			raw := item.Get(m)
			if !isObject(raw) {
				continue
			}
			node := raw
			if shared := item.Get("parameters"); shared != nil && len(shared.Arr) > 0 {
				node = copyObject(raw)
				merged := append(append([]*Node(nil), shared.Arr...), raw.Get("parameters").Elems()...)
				node.Set("parameters", newArray(merged))
			}
			set.add(&syncOp{
				file: file, doc: doc, key: prefix + strings.ToUpper(m) + " " + path,
				method: strings.ToUpper(m), path: path, node: node, raw: raw, item: item,
			})
		}
	}
}

// deref follows a local $ref of n, like the Python reference's dereference:
// the keys next to the $ref override the target's, and a reference that
// does not resolve becomes {"x-unresolved-ref": ref} so that it stays
// visible (the generator reports it).
func deref(doc *Doc, n *Node) *Node {
	return derefDepth(doc, n, 0)
}

func derefDepth(doc *Doc, n *Node, depth int) *Node {
	if !isObject(n) || depth > 64 {
		return n
	}
	ref := n.Get("$ref")
	if ref == nil || ref.Kind != String {
		return n
	}
	target := doc.Lookup(ref.Str)
	if target == nil {
		out := newObject()
		out.Set("x-unresolved-ref", ref)
		return out
	}
	target = derefDepth(doc, target, depth+1)
	if len(n.Keys) == 1 || !isObject(target) {
		return target
	}
	out := copyObject(target)
	for _, k := range n.Keys {
		if k != "$ref" {
			out.Set(k, n.Get(k))
		}
	}
	return out
}

// differ compares an operation of the old documents with one of the new.
type differ struct {
	old, new *Doc
	// Shape comparison state: pairs being compared (assumed equal when met
	// again, so recursive schemas terminate), settled results, and how often
	// an assumption was used (results that relied on one are not settled).
	active  map[[2]*Node]bool
	settled map[[2]*Node]bool
	assumed int
}

func newDiffer(old, new *Doc) *differ {
	return &differ{old: old, new: new, active: map[[2]*Node]bool{}, settled: map[[2]*Node]bool{}}
}

// sameShape reports whether two schemas send and receive the same thing:
// equal once references are followed and documentation keywords (docKeys,
// x-*) are ignored. a belongs to the old document, b to the new one.
//
// Documentation keywords are only ignored where they are keywords: a
// property called "title" inside "properties" still counts. (The Python
// reference drops such keys at every level, so it misses changes to
// properties with those names.)
func (d *differ) sameShape(a, b *Node, depth int) bool {
	if depth > 64 {
		d.assumed++
		return true
	}
	a, b = deref(d.old, orEmpty(a)), deref(d.new, orEmpty(b))
	key := [2]*Node{a, b}
	if r, ok := d.settled[key]; ok {
		return r
	}
	if d.active[key] {
		d.assumed++
		return true
	}
	d.active[key] = true
	before := d.assumed
	r := d.compareSchemas(a, b, depth)
	delete(d.active, key)
	// A difference is final; an equality that relied on an assumption is not.
	if !r || d.assumed == before {
		d.settled[key] = r
	}
	return r
}

// schemaMaps are keywords whose value maps names to schemas.
var schemaMaps = map[string]bool{
	"properties": true, "patternProperties": true, "$defs": true, "definitions": true,
	"dependentSchemas": true,
}

// schemaLists are keywords whose value is a list of schemas.
var schemaLists = map[string]bool{"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true}

// schemaValues are keywords whose value is a schema (or a boolean).
var schemaValues = map[string]bool{
	"items": true, "additionalProperties": true, "not": true, "contains": true,
	"propertyNames": true, "if": true, "then": true, "else": true,
	"unevaluatedProperties": true, "unevaluatedItems": true, "additionalItems": true,
}

func (d *differ) compareSchemas(a, b *Node, depth int) bool {
	if a.Kind != Object || b.Kind != Object {
		return nodeEqual(a, b)
	}
	ka, kb := shapeKeys(a), shapeKeys(b)
	if len(ka) != len(kb) {
		return false
	}
	for _, k := range ka {
		if b.Get(k) == nil || docKeys[k] || strings.HasPrefix(k, "x-") {
			return false
		}
		va, vb := a.Get(k), b.Get(k)
		switch {
		case schemaMaps[k] && isObject(va) && isObject(vb):
			if len(va.Keys) != len(vb.Keys) {
				return false
			}
			for _, name := range va.Keys {
				if vb.Get(name) == nil || !d.sameShape(va.Get(name), vb.Get(name), depth+1) {
					return false
				}
			}
		case schemaLists[k] && va.Kind == Array && vb.Kind == Array:
			if len(va.Arr) != len(vb.Arr) {
				return false
			}
			for i := range va.Arr {
				if !d.sameShape(va.Arr[i], vb.Arr[i], depth+1) {
					return false
				}
			}
		case schemaValues[k] && isObject(va) && isObject(vb):
			if !d.sameShape(va, vb, depth+1) {
				return false
			}
		default:
			if !nodeEqual(va, vb) {
				return false
			}
		}
	}
	return true
}

// shapeKeys returns the keys of a schema that are not documentation.
func shapeKeys(n *Node) []string {
	var out []string
	for _, k := range n.Keys {
		if !docKeys[k] && !strings.HasPrefix(k, "x-") {
			out = append(out, k)
		}
	}
	return out
}

// schemaTypes returns the JSON types a schema allows, "null" included.
func schemaTypes(doc *Doc, s *Node) map[string]bool {
	types := map[string]bool{}
	switch t := s.Get("type"); {
	case t == nil:
	case t.Kind == String:
		types[t.Str] = true
	case t.Kind == Array:
		for _, v := range t.Strings() {
			types[v] = true
		}
	}
	if s.Get("nullable").Truthy() {
		types["null"] = true
	}
	for _, kw := range []string{"anyOf", "oneOf"} {
		for _, v := range s.Get(kw).Elems() {
			if deref(doc, v).Get("type").Text() == "null" {
				types["null"] = true
			}
		}
	}
	return types
}

// objectProps returns the properties and required names of an object
// schema, following allOf (in the Python reference's order, so the same
// definition wins when parts repeat a property).
func objectProps(doc *Doc, s *Node) (map[string]*Node, map[string]bool) {
	props := map[string]*Node{}
	required := map[string]bool{}
	stack := []*Node{s}
	for len(stack) > 0 {
		cur := deref(doc, stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if !isObject(cur) {
			continue
		}
		ps := cur.Get("properties")
		for _, k := range ps.ObjKeys() {
			props[k] = ps.Get(k)
		}
		for _, r := range cur.Get("required").Strings() {
			required[r] = true
		}
		stack = append(stack, cur.Get("allOf").Elems()...)
	}
	return props, required
}

// variants returns the oneOf/anyOf variants of a schema without the null
// one.
func variants(doc *Doc, s *Node, keyword string) []*Node {
	var out []*Node
	for _, v := range s.Get(keyword).Elems() {
		if r := deref(doc, v); isObject(r) && r.Get("type").Text() == "null" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func joinSorted(set map[string]bool) string {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, "/")
}

func withoutNull(set map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range set {
		if k != "null" {
			out[k] = true
		}
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedSet(set map[string]bool) []string {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// containsNode reports whether list has an item equal to v.
func containsNode(list []*Node, v *Node) bool {
	for _, item := range list {
		if nodeEqual(item, v) {
			return true
		}
	}
	return false
}

// diffSchema appends a note for every difference between two schemas.
//
// request tells which side the schema is on: removing an enum value breaks
// callers that send it, while a response enum is open (DESIGN 5.1) and
// gaining or losing a value breaks nobody; a field that becomes required
// breaks callers, a response field that is no longer always present breaks
// readers.
func (d *differ) diffSchema(old, new *Node, where string, request bool, notes *[]note, depth int) {
	if depth > 16 {
		return
	}
	old, new = deref(d.old, orEmpty(old)), deref(d.new, orEmpty(new))
	if d.sameShape(old, new, 0) {
		return
	}
	add := func(text string, breaking bool) { *notes = append(*notes, note{text, breaking}) }
	if !isObject(old) || !isObject(new) {
		add(where+": schema replaced", true)
		return
	}
	oldTypes, newTypes := schemaTypes(d.old, old), schemaTypes(d.new, new)
	oldPlain, newPlain := withoutNull(oldTypes), withoutNull(newTypes)
	if len(oldPlain) > 0 && len(newPlain) > 0 && !sameSet(oldPlain, newPlain) {
		widened := sameSet(oldPlain, map[string]bool{"integer": true}) && sameSet(newPlain, map[string]bool{"number": true})
		narrowed := sameSet(oldPlain, map[string]bool{"number": true}) && sameSet(newPlain, map[string]bool{"integer": true})
		harmless := (request && widened) || (!request && narrowed)
		add(fmt.Sprintf("%s: type %s -> %s", where, joinSorted(oldPlain), joinSorted(newPlain)), !harmless)
		return
	}
	if newTypes["null"] && !oldTypes["null"] && !request {
		add(where+": can now be null", true)
	}
	if oldTypes["null"] && !newTypes["null"] && request {
		add(where+": no longer accepts null", true)
	}

	oldEnum, newEnum := old.Get("enum"), new.Get("enum")
	oldIsList, newIsList := oldEnum != nil && oldEnum.Kind == Array, newEnum != nil && newEnum.Kind == Array
	switch {
	case oldIsList && newIsList:
		var gone, added []*Node
		for _, v := range oldEnum.Arr {
			if !containsNode(newEnum.Arr, v) {
				gone = append(gone, v)
			}
		}
		for _, v := range newEnum.Arr {
			if !containsNode(oldEnum.Arr, v) {
				added = append(added, v)
			}
		}
		if len(gone) > 0 {
			add(fmt.Sprintf("%s: values removed %s", where, encodeJSON(newArray(gone), inlineStyle)), request)
		}
		if len(added) > 0 {
			add(fmt.Sprintf("%s: values added %s", where, encodeJSON(newArray(added), inlineStyle)), false)
		}
	case newIsList:
		add(fmt.Sprintf("%s: now limited to %s", where, encodeJSON(newEnum, inlineStyle)), request)
	case oldIsList:
		add(where+": no longer limited to fixed values", false)
	}

	oldProps, oldReq := objectProps(d.old, old)
	newProps, newReq := objectProps(d.new, new)
	for _, name := range sortedNames(oldProps) {
		if newProps[name] == nil {
			add(fmt.Sprintf("%s.%s: removed", where, name), true)
		}
	}
	for _, name := range sortedNames(newProps) {
		if oldProps[name] == nil {
			required := request && newReq[name]
			suffix := ""
			if required {
				suffix = " (required)"
			}
			add(fmt.Sprintf("%s.%s: added%s", where, name, suffix), required)
		}
	}
	for _, name := range sortedNames(oldProps) {
		if newProps[name] == nil {
			continue
		}
		if request && newReq[name] && !oldReq[name] {
			add(fmt.Sprintf("%s.%s: now required", where, name), true)
		}
		if !request && oldReq[name] && !newReq[name] {
			add(fmt.Sprintf("%s.%s: no longer always present", where, name), true)
		}
		d.diffSchema(oldProps[name], newProps[name], where+"."+name, request, notes, depth+1)
	}
	for _, kw := range []struct{ key, suffix string }{{"items", "[]"}, {"additionalProperties", "{}"}} {
		ov, nv := old.Get(kw.key), new.Get(kw.key)
		if isObject(ov) || isObject(nv) {
			d.diffSchema(falsyEmpty(ov), falsyEmpty(nv), where+kw.suffix, request, notes, depth+1)
		}
	}
	for _, kw := range []string{"oneOf", "anyOf"} {
		ov, nv := variants(d.old, old, kw), variants(d.new, new, kw)
		switch {
		case len(nv) < len(ov):
			add(fmt.Sprintf("%s: %d %s variant(s) removed", where, len(ov)-len(nv), kw), true)
		case len(nv) > len(ov):
			add(fmt.Sprintf("%s: %d %s variant(s) added", where, len(nv)-len(ov), kw), false)
		default:
			for i := range ov {
				d.diffSchema(ov[i], nv[i], fmt.Sprintf("%s<%s %d>", where, kw, i+1), request, notes, depth+1)
			}
		}
	}
}

// falsyEmpty is Python's `value or {}`: nil and false become an empty
// object.
func falsyEmpty(n *Node) *Node {
	if n == nil || (n.Kind == Bool && !n.Bool) {
		return newObject()
	}
	return n
}

func sortedNames(m map[string]*Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// paramKey identifies a parameter.
type paramKey struct{ in, name string }

func (d *differ) params(doc *Doc, op *Node) (map[paramKey]*Node, []paramKey) {
	out := map[paramKey]*Node{}
	for _, p := range op.Get("parameters").Elems() {
		p = deref(doc, p)
		k := paramKey{p.Get("in").Text(), p.Get("name").Text()}
		out[k] = p // a later definition (the operation's own) wins
	}
	keys := make([]paramKey, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].in != keys[j].in {
			return keys[i].in < keys[j].in
		}
		return keys[i].name < keys[j].name
	})
	return out, keys
}

// requestBody returns the content types of the request body and the schema
// of its JSON (or else multipart) content.
func requestBody(doc *Doc, op *Node) (*Node, map[string]bool, *Node) {
	rb := deref(doc, op.Get("requestBody"))
	content := rb.Get("content")
	types := map[string]bool{}
	for _, ct := range content.ObjKeys() {
		types[ct] = true
	}
	var schema *Node
	for _, ct := range []string{"application/json", "multipart/form-data"} {
		if media := content.Get(ct); isObject(media) && len(media.Keys) > 0 {
			schema = media.Get("schema")
			break
		}
	}
	return rb, types, schema
}

// successResponse returns the content types of the first 2xx response (in
// sorted status order) and the schema of its JSON content.
func successResponse(doc *Doc, op *Node) (map[string]bool, *Node) {
	responses := op.Get("responses")
	for _, code := range sortedKeys(orEmpty(responses)) {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		content := deref(doc, responses.Get(code)).Get("content")
		types := map[string]bool{}
		for _, ct := range content.ObjKeys() {
			types[ct] = true
		}
		return types, content.Path("application/json", "schema")
	}
	return map[string]bool{}, nil
}

// describeChange returns the differences of one operation and whether any
// of them is breaking. old belongs to oldDoc, new to newDoc.
func describeChange(oldDoc *Doc, old *Node, newDoc *Doc, new *Node) ([]string, bool) {
	d := newDiffer(oldDoc, newDoc)
	var found []note
	add := func(text string, breaking bool) { found = append(found, note{text, breaking}) }

	oldParams, oldKeys := d.params(oldDoc, old)
	newParams, newKeys := d.params(newDoc, new)
	for _, k := range oldKeys {
		if newParams[k] == nil {
			add(fmt.Sprintf("parameter removed: %s (%s)", k.name, k.in), true)
		}
	}
	for _, k := range newKeys {
		if oldParams[k] == nil {
			required := newParams[k].Get("required").Truthy()
			suffix := ""
			if required {
				suffix = ", required"
			}
			add(fmt.Sprintf("parameter added: %s (%s%s)", k.name, k.in, suffix), required)
		}
	}
	for _, k := range oldKeys {
		np := newParams[k]
		if np == nil {
			continue
		}
		op := oldParams[k]
		if !op.Get("required").Truthy() && np.Get("required").Truthy() {
			add("parameter now required: "+k.name, true)
		}
		d.diffSchema(op.Get("schema"), np.Get("schema"), "parameter "+k.name, true, &found, 0)
	}

	oldRB, oldTypes, oldBody := requestBody(oldDoc, old)
	newRB, newTypes, newBody := requestBody(newDoc, new)
	if !sameSet(oldTypes, newTypes) && len(oldTypes) > 0 && len(newTypes) > 0 {
		subset := true
		for t := range oldTypes {
			if !newTypes[t] {
				subset = false
			}
		}
		add(fmt.Sprintf("request content type %s -> %s", pyStrings(sortedSet(oldTypes)), pyStrings(sortedSet(newTypes))), !subset)
	}
	if !oldRB.Get("required").Truthy() && newRB.Get("required").Truthy() {
		add("request body now required", true)
	}
	d.diffSchema(oldBody, newBody, "body", true, &found, 0)

	oldTypes, oldResp := successResponse(oldDoc, old)
	newTypes, newResp := successResponse(newDoc, new)
	if !sameSet(oldTypes, newTypes) {
		add(fmt.Sprintf("response content type %s -> %s", pyStrings(sortedSet(oldTypes)), pyStrings(sortedSet(newTypes))), true)
	}
	d.diffSchema(oldResp, newResp, "response", false, &found, 0)

	if !old.Get("deprecated").Truthy() && new.Get("deprecated").Truthy() {
		add("marked deprecated upstream", false)
	}
	if ns := new.Get("x-didit-sdk"); ns != nil && !nodeEqual(old.Get("x-didit-sdk"), ns) {
		add("x-didit-sdk now "+encodeJSON(ns, sortedInlineStyle), false)
	}
	if len(found) == 0 {
		return []string{unchangedNote}, false
	}
	// Breaking notes first, then the rest, capped so that one huge change
	// stays readable.
	isBreaking := map[string]bool{}
	breaking := false
	for _, n := range found {
		if n.breaking {
			isBreaking[n.text] = true
			breaking = true
		}
	}
	var notes []string
	for _, pass := range []bool{true, false} {
		for _, n := range found {
			if n.breaking != pass {
				continue
			}
			text := n.text
			if isBreaking[text] {
				text += " (breaking)"
			}
			notes = append(notes, text)
		}
	}
	if len(notes) > maxNotes {
		notes = append(notes[:maxNotes:maxNotes], fmt.Sprintf("and %d more differences", len(notes)-maxNotes))
	}
	return notes, breaking
}
