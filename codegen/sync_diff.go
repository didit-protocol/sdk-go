package main

import (
	"fmt"
	"sort"
	"strings"
)

// Comparison of operations and schemas for the spec sync summary.
// Operations are compared field by field, in parameters, request bodies (per
// media type) and every success response, following $refs, allOf, array
// items and every oneOf/anyOf variant. Each difference is a syncChange; the
// ones the docs/DESIGN.md section 3.1 rules count are breaking: they can
// break typed code, and the pull request and the release notes list them.
// A removed operation is breaking too (sync_apply.go). The rules are
// checked by the shared vectors of openapi/breaking-vectors.json
// (sync_vectors_test.go), so that every SDK lists the same changes.
//
// References are followed lazily while comparing, so recursive schemas are
// never copied.

// syncMaxNotes caps the notes of one operation in the summary so that a huge
// change stays readable. The breaking changes are never capped.
const syncMaxNotes = 25

// syncUnchangedNote describes an operation whose changes are documentation
// only.
const syncUnchangedNote = "descriptions, examples or other details changed"

// syncOtherNote describes an operation that changed outside what
// describeChange compares (error responses, security, headers...).
const syncOtherNote = "other request or response details changed"

// syncDocKeys document without changing what is sent or received. In a map
// of names (properties, ...) a key is a name: a property called
// "description" is a field.
var syncDocKeys = map[string]bool{
	"description": true, "summary": true, "title": true,
	"example": true, "examples": true, "externalDocs": true, "tags": true,
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

// syncOpSet holds the operations of the documents in document order.
type syncOpSet struct {
	keys  []string
	byKey map[string]*syncOp
}

func (s *syncOpSet) add(op *syncOp) {
	if s.byKey == nil {
		s.byKey = map[string]*syncOp{}
	}
	if _, dup := s.byKey[op.key]; !dup {
		s.keys = append(s.keys, op.key)
	}
	s.byKey[op.key] = op
}

// syncCollectOps adds the operations of doc (the HTTP methods the generator
// reads) to set.
func syncCollectOps(set *syncOpSet, file string, doc *Doc, prefix string) {
	paths := doc.Root.Get("paths")
	for _, path := range paths.ObjKeys() {
		item := paths.Get(path)
		for _, m := range httpMethods {
			raw := item.Get(m)
			if !syncIsObject(raw) {
				continue
			}
			node := raw
			if shared := item.Get("parameters"); shared != nil && len(shared.Arr) > 0 {
				node = syncCopyObject(raw)
				merged := append(append([]*Node(nil), shared.Arr...), raw.Get("parameters").Elems()...)
				node.Set("parameters", syncArray(merged))
			}
			set.add(&syncOp{
				file: file, doc: doc, key: prefix + strings.ToUpper(m) + " " + path,
				method: strings.ToUpper(m), path: path, node: node, raw: raw, item: item,
			})
		}
	}
}

// syncDeref follows a local $ref of n, like the Python reference's dereference:
// the keys next to the $ref override the target's, and a reference that
// does not resolve becomes {"x-unresolved-ref": ref} so that it stays
// visible (the generator reports it).
func syncDeref(doc *Doc, n *Node) *Node {
	return syncDerefDepth(doc, n, 0)
}

func syncDerefDepth(doc *Doc, n *Node, depth int) *Node {
	if !syncIsObject(n) || depth > 64 {
		return n
	}
	ref := n.Get("$ref")
	if ref == nil || ref.Kind != String {
		return n
	}
	target := doc.Lookup(ref.Str)
	if target == nil {
		out := syncObject()
		out.Set("x-unresolved-ref", ref)
		return out
	}
	target = syncDerefDepth(doc, target, depth+1)
	if len(n.Keys) == 1 || !syncIsObject(target) {
		return target
	}
	out := syncCopyObject(target)
	for _, k := range n.Keys {
		if k != "$ref" {
			out.Set(k, n.Get(k))
		}
	}
	return out
}

// syncDiffer compares an operation of the old documents with one of the new.
type syncDiffer struct {
	old, new *Doc
	// Shape comparison state: pairs being compared (assumed equal when met
	// again, so recursive schemas terminate), settled results, and how often
	// an assumption was used (results that relied on one are not settled).
	active  map[[2]*Node]bool
	settled map[[2]*Node]bool
	assumed int
	// refs are the schemas single read from a component (see single).
	refs map[*Node]bool
}

func newSyncDiffer(old, new *Doc) *syncDiffer {
	return &syncDiffer{old: old, new: new, active: map[[2]*Node]bool{}, settled: map[[2]*Node]bool{}, refs: map[*Node]bool{}}
}

// sameShape reports whether two schemas send and receive the same thing:
// equal once references are followed and documentation keywords (syncDocKeys,
// x-*) are ignored, with required and type lists in any order. a belongs to
// the old document, b to the new one. Documentation keywords are only
// ignored where they are keywords: a property called "title" inside
// "properties" still counts.
func (d *syncDiffer) sameShape(a, b *Node, depth int) bool {
	if depth > 64 {
		d.assumed++
		return true
	}
	a, b = syncDeref(d.old, syncOrEmpty(a)), syncDeref(d.new, syncOrEmpty(b))
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

// syncSchemaMaps are keywords whose value maps names to schemas.
var syncSchemaMaps = map[string]bool{
	"properties": true, "patternProperties": true, "$defs": true, "definitions": true,
	"dependentSchemas": true,
}

// syncSchemaLists are keywords whose value is a list of schemas.
var syncSchemaLists = map[string]bool{"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true}

// syncSchemaValues are keywords whose value is a schema (or a boolean).
var syncSchemaValues = map[string]bool{
	"items": true, "additionalProperties": true, "not": true, "contains": true,
	"propertyNames": true, "if": true, "then": true, "else": true,
	"unevaluatedProperties": true, "unevaluatedItems": true, "additionalItems": true,
}

func (d *syncDiffer) compareSchemas(a, b *Node, depth int) bool {
	if a.Kind != Object || b.Kind != Object {
		return syncEqual(a, b)
	}
	ka, kb := syncShapeKeys(a), syncShapeKeys(b)
	if len(ka) != len(kb) {
		return false
	}
	for _, k := range ka {
		va, vb := a.Get(k), b.Get(k)
		if vb == nil {
			return false
		}
		switch {
		case syncSchemaMaps[k] && syncIsObject(va) && syncIsObject(vb):
			if len(va.Keys) != len(vb.Keys) {
				return false
			}
			for _, name := range va.Keys {
				if vb.Get(name) == nil || !d.sameShape(va.Get(name), vb.Get(name), depth+1) {
					return false
				}
			}
		case syncSchemaLists[k] && va.Kind == Array && vb.Kind == Array:
			if len(va.Arr) != len(vb.Arr) {
				return false
			}
			for i := range va.Arr {
				if !d.sameShape(va.Arr[i], vb.Arr[i], depth+1) {
					return false
				}
			}
		case syncSchemaValues[k] && syncIsObject(va) && syncIsObject(vb):
			if !d.sameShape(va, vb, depth+1) {
				return false
			}
		case (k == "required" || k == "type") && va.Kind == Array && vb.Kind == Array:
			if !syncSameItems(va, vb) {
				return false
			}
		default:
			if !syncEqual(va, vb) {
				return false
			}
		}
	}
	return true
}

// syncSameItems reports whether two arrays hold the same items in any order.
func syncSameItems(a, b *Node) bool {
	if len(a.Arr) != len(b.Arr) {
		return false
	}
	sorted := func(n *Node) []*Node {
		out := append([]*Node(nil), n.Arr...)
		sort.SliceStable(out, func(i, j int) bool { return out[i].SortedJSON() < out[j].SortedJSON() })
		return out
	}
	x, y := sorted(a), sorted(b)
	for i := range x {
		if !syncEqual(x[i], y[i]) {
			return false
		}
	}
	return true
}

// syncShapeKeys returns the keys of a schema that are not documentation.
func syncShapeKeys(n *Node) []string {
	var out []string
	for _, k := range n.Keys {
		if !syncDocKeys[k] && !strings.HasPrefix(k, "x-") {
			out = append(out, k)
		}
	}
	return out
}

// ---- changes ----------------------------------------------------------------

// syncChange is one difference of an operation.
type syncChange struct {
	// where is the part of the operation: "response", "request body", a
	// parameter's location ("query", "path", "header" or "cookie"),
	// "operation" or "x-didit-sdk"; "" for a change of the whole operation
	// (sync_apply.go).
	where string
	// path is the field inside it, for example
	// "registry_checks[].company.registry_data"; for a parameter its name,
	// then the field in it ("ids[]"); "" for the whole part.
	path string
	// kind says what changed: "can now be null", "type changed from string
	// to object", "removed", ...
	kind string
	// breaking marks a change the docs/DESIGN.md section 3.1 rules count:
	// one that can break typed code.
	breaking bool
	// code is a counted change's kind as the shared vectors spell it:
	// "removed", "type string -> object", "now required", "body now
	// required", "operation removed"... (syncBreaking.normalized); "" when
	// the change is not counted.
	code string
}

// syncParameterLocations are the values of a parameter's in.
var syncParameterLocations = map[string]bool{"path": true, "query": true, "header": true, "cookie": true}

// syncOperationKinds are the codes of a change of the operation itself,
// which the vectors write without where and field.
var syncOperationKinds = map[string]bool{"operation removed": true, "body now required": true}

// target is where the change is: "response `a.b`", "request body", "query
// parameter `limit`".
func (c syncChange) target() string {
	switch {
	case syncParameterLocations[c.where]:
		return c.where + " parameter `" + c.path + "`"
	case c.path == "":
		return c.where
	case c.where == "":
		return "`" + c.path + "`"
	}
	return c.where + " `" + c.path + "`"
}

// listed returns the change as one that is listed, not counted.
func (c syncChange) listed() syncChange {
	c.breaking, c.code = false, ""
	return c
}

// text renders the change for the summary.
func (c syncChange) text() string {
	if t := c.target(); t != "" {
		return t + ": " + c.kind
	}
	return c.kind
}

// syncBreaking is one change that can break typed code, as the summary and
// the release notes list it.
type syncBreaking struct {
	operation string // "GET /v3/session/{sessionId}/decision/"
	change    syncChange
}

// text renders the entry: "`GET /x/` response `a.b`: can now be null". It
// never contains "<" or ">", which release-please escapes in release notes.
func (b syncBreaking) text() string {
	if t := b.change.target(); t != "" {
		return fmt.Sprintf("`%s` %s: %s", b.operation, t, b.change.kind)
	}
	return fmt.Sprintf("`%s`: %s", b.operation, b.change.kind)
}

// normalized renders the entry as openapi/breaking-vectors.json spells it in
// every SDK: "<METHOD> <path> <where> <field>: <kind>", without the field for
// a whole body and without where and field for the operation itself
// ("GET /x/: operation removed").
func (b syncBreaking) normalized() string {
	if syncOperationKinds[b.change.code] {
		return b.operation + ": " + b.change.code
	}
	target := b.operation
	for _, part := range []string{b.change.where, b.change.path} {
		if part != "" {
			target += " " + part
		}
	}
	return target + ": " + b.change.code
}

// syncDedupBreaking drops repeated entries (a moved method and its new path
// can report the same change), keeping the first of each in order: removed
// operations, then moved methods, then changed operations in key order.
func syncDedupBreaking(list []syncBreaking) []syncBreaking {
	seen := map[[4]string]bool{}
	var out []syncBreaking
	for _, b := range list {
		k := [4]string{b.operation, b.change.where, b.change.path, b.change.kind}
		if !seen[k] {
			seen[k] = true
			out = append(out, b)
		}
	}
	return out
}

// syncNotes renders the changes of one operation for the summary: the
// breaking ones first, marked "(breaking)", then the others, capped at
// syncMaxNotes so that a huge change stays readable.
func syncNotes(changes []syncChange) []string {
	var notes []string
	for _, pass := range []bool{true, false} {
		for _, c := range changes {
			if c.breaking != pass {
				continue
			}
			text := c.text()
			if c.breaking {
				text += " (breaking)"
			}
			notes = append(notes, text)
		}
	}
	if len(notes) > syncMaxNotes {
		notes = append(notes[:syncMaxNotes:syncMaxNotes], fmt.Sprintf("and %d more differences", len(notes)-syncMaxNotes))
	}
	return notes
}

// syncHasBreaking reports whether any change is breaking.
func syncHasBreaking(changes []syncChange) bool {
	for _, c := range changes {
		if c.breaking {
			return true
		}
	}
	return false
}

// ---- schemas as every SDK reads them -----------------------------------------
//
// From here on this is a port of the comparison of sdk-python's and
// sdk-java's codegen/spec_sync.py (_nullable, _single, _kinds, diff_schema,
// _pair_variants, _diff_content, _diff_statuses, describe_change), with the
// same rules and the same words, so that the seven SDKs list the same
// breaking changes for one upstream change. Python inlines every $ref
// first; here a reference is followed when it is read, with the document of
// its side.

// syncTypedFormats are the string formats the shared list types apart:
// adding, removing or changing one is a type change ("string (date-time)").
// Go types date-time as time.Time and binary as a file; date stays a string
// in Go but has its own type in other languages, and every SDK reports the
// same list.
var syncTypedFormats = map[string]bool{"date-time": true, "date": true, "binary": true}

// syncMaxDepth bounds how deep a comparison follows schemas.
const syncMaxDepth = 32

// syncNoSchema stands for a request body or response without content (no
// media type at all): compared with the other side field by field, never as
// a type change. It is compared by identity and never modified.
var syncNoSchema = syncObject()

// syncOrObject is Python's `value or {}`: a missing or falsy value (null,
// false, 0, "", [] or {}) becomes an empty object.
func syncOrObject(n *Node) *Node {
	if n == nil {
		return syncObject()
	}
	switch n.Kind {
	case Null:
		return syncObject()
	case Bool:
		if !n.Bool {
			return syncObject()
		}
	case Number:
		if syncNumberEqual(n.Str, "0") {
			return syncObject()
		}
	case String:
		if n.Str == "" {
			return syncObject()
		}
	case Array:
		if len(n.Arr) == 0 {
			return syncObject()
		}
	case Object:
		if len(n.Keys) == 0 {
			return syncObject()
		}
	}
	return n
}

// syncNullOnly reports whether a schema allows null and nothing else.
func syncNullOnly(doc *Doc, s *Node) bool {
	r := syncDeref(doc, s)
	if !syncIsObject(r) {
		return false
	}
	if t := r.Get("type"); t != nil && t.Kind == String && t.Str == "null" {
		return true
	}
	e := r.Get("enum")
	return e != nil && e.Kind == Array && len(e.Arr) == 1 && e.Arr[0].Kind == Null
}

// syncVariants returns the oneOf and anyOf variants of a schema other than
// null (of keyword only, when given).
func syncVariants(doc *Doc, s *Node, keywords ...string) []*Node {
	if len(keywords) == 0 {
		keywords = []string{"oneOf", "anyOf"}
	}
	s = syncDeref(doc, s)
	var out []*Node
	for _, kw := range keywords {
		for _, v := range s.Get(kw).Elems() {
			if !syncNullOnly(doc, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// syncUnionKeyword returns the first of oneOf and anyOf that has a variant
// other than null, or "".
func syncUnionKeyword(doc *Doc, s *Node) string {
	for _, kw := range []string{"oneOf", "anyOf"} {
		if len(syncVariants(doc, s, kw)) > 0 {
			return kw
		}
	}
	return ""
}

// syncNullable reports whether a schema accepts null: a null type, nullable,
// null in its enum, an allOf without a type of its own whose parts all
// accept it, or a oneOf/anyOf variant that accepts it.
func syncNullable(doc *Doc, s *Node, depth int) bool {
	s = syncDeref(doc, s)
	if !syncIsObject(s) || depth > syncMaxDepth {
		return false
	}
	if t := s.Get("type"); t != nil && ((t.Kind == String && t.Str == "null") || (t.Kind == Array && syncHasString(t, "null"))) {
		return true
	}
	if s.Get("nullable").Truthy() {
		return true
	}
	if e := s.Get("enum"); e != nil && e.Kind == Array {
		for _, v := range e.Arr {
			if v.Kind == Null {
				return true
			}
		}
	}
	if all := s.Get("allOf"); !s.Has("type") && all != nil && len(all.Arr) > 0 {
		every := true
		for _, m := range all.Arr {
			every = every && syncNullable(doc, m, depth+1)
		}
		if every {
			return true
		}
	}
	for _, kw := range []string{"oneOf", "anyOf"} {
		for _, v := range s.Get(kw).Elems() {
			if syncNullable(doc, v, depth+1) {
				return true
			}
		}
	}
	return false
}

// syncHasString reports whether an array holds the string s.
func syncHasString(arr *Node, s string) bool {
	for _, v := range arr.Strings() {
		if v == s {
			return true
		}
	}
	return false
}

// single reads a schema as one type: the parts of an allOf merged into it
// (its own properties win, then each part's in order; required names are
// joined; any other keyword comes from the first that has it), and a
// oneOf/anyOf with a single variant other than null replaced by that
// variant with the schema's own keywords on top, so that anyOf [X, null]
// reads as a nullable X rather than as X's fields removed.
//
// It records in d.refs the results that come from a component, which Python
// marks with x-didit-ref: a component's title is not a union variant's own.
func (d *syncDiffer) single(doc *Doc, s *Node, depth int) *Node {
	ref := d.refs[s] || (syncIsObject(s) && s.Has("$ref"))
	s = syncDeref(doc, s)
	if !syncIsObject(s) || depth > syncMaxDepth {
		return s
	}
	if all := s.Get("allOf"); all != nil && all.Kind == Array && syncAllObjects(doc, all.Arr) {
		merged := syncObject()
		for _, k := range s.Keys {
			if k != "allOf" {
				merged.Set(k, s.Get(k))
			}
		}
		for _, m := range all.Arr {
			part := d.single(doc, m, depth+1)
			ref = ref || d.refs[part]
			for _, k := range part.Keys {
				v := part.Get(k)
				switch {
				case k == "properties" && syncIsObject(v):
					props := syncCopyObject(v)
					own := merged.Get("properties")
					if syncIsObject(own) {
						for _, name := range own.Keys {
							props.Set(name, own.Get(name))
						}
					}
					merged.Set("properties", props)
				case k == "required" && v.Kind == Array:
					names := map[string]bool{}
					for _, r := range append(merged.Get("required").Strings(), v.Strings()...) {
						names[r] = true
					}
					var list []*Node
					for _, r := range syncSortedSet(names) {
						list = append(list, syncString(r))
					}
					merged.Set("required", syncArray(list))
				case !merged.Has(k):
					merged.Set(k, v)
				}
			}
		}
		s = merged
	}
	if ref {
		d.refs[s] = true
	}
	variants := syncVariants(doc, s)
	if len(variants) != 1 {
		return s
	}
	v := syncDeref(doc, variants[0])
	if !syncIsObject(v) {
		return s
	}
	out := syncCopyObject(v)
	for _, k := range s.Keys {
		if k != "oneOf" && k != "anyOf" {
			out.Set(k, s.Get(k))
		}
	}
	if ref || variants[0].Has("$ref") || d.refs[variants[0]] {
		d.refs[out] = true
	}
	return d.single(doc, out, depth+1)
}

// syncAllObjects reports whether every schema of list is an object.
func syncAllObjects(doc *Doc, list []*Node) bool {
	for _, m := range list {
		if !syncIsObject(syncDeref(doc, m)) {
			return false
		}
	}
	return true
}

// syncKinds returns the JSON types a schema allows besides null, inferred
// when it has no type (a union allows its variants' types); empty when it
// does not say.
func syncKinds(doc *Doc, s *Node, depth int) map[string]bool {
	s = syncDeref(doc, s)
	kinds := map[string]bool{}
	if !syncIsObject(s) || depth > syncMaxDepth {
		return kinds
	}
	if t := s.Get("type"); t != nil && t.Kind == String {
		kinds[t.Str] = true
	} else if t != nil && t.Kind == Array {
		for _, v := range t.Strings() {
			kinds[v] = true
		}
	}
	delete(kinds, "null")
	if len(kinds) > 0 {
		return kinds
	}
	if s.Has("properties") || s.Has("additionalProperties") || s.Has("patternProperties") {
		return map[string]bool{"object": true}
	}
	if s.Has("items") {
		return map[string]bool{"array": true}
	}
	if variants := syncVariants(doc, s); len(variants) > 0 {
		for _, v := range variants {
			found := syncKinds(doc, v, depth+1)
			if len(found) == 0 {
				return map[string]bool{}
			}
			for k := range found {
				kinds[k] = true
			}
		}
		return kinds
	}
	for _, m := range s.Get("allOf").Elems() {
		for k := range syncKinds(doc, m, depth+1) {
			kinds[k] = true
		}
	}
	if len(kinds) == 0 {
		var values []*Node
		if e := s.Get("enum"); e != nil && e.Kind == Array {
			values = e.Arr
		}
		if c := s.Get("const"); c != nil {
			values = []*Node{c}
		}
		for _, v := range values {
			if v.Kind != Null {
				kinds[syncValueKind(v)] = true
			}
		}
	}
	return kinds
}

// syncValueKind returns the JSON type of an enum or const value.
func syncValueKind(v *Node) string {
	switch v.Kind {
	case String:
		return "string"
	case Bool:
		return "boolean"
	case Number:
		if strings.ContainsAny(v.Str, ".eE") {
			return "number"
		}
		return "integer"
	case Array:
		return "array"
	}
	return "object"
}

// syncTypeNames names kinds as the summary does: a string with a typed format
// is its own type, and a schema that does not say allows any value.
func syncTypeNames(s *Node, kinds map[string]bool) string {
	if len(kinds) == 0 {
		return "any"
	}
	format := s.Get("format").Text()
	names := syncSortedSet(kinds)
	for i, k := range names {
		if k == "string" && syncTypedFormats[format] {
			names[i] = "string (" + format + ")"
		}
	}
	return strings.Join(names, " or ")
}

// syncEnum returns the enum values of a schema other than null, or nil when
// it has no enum.
func syncEnum(s *Node) []*Node {
	e := s.Get("enum")
	if e == nil || e.Kind != Array {
		return nil
	}
	out := []*Node{}
	for _, v := range e.Arr {
		if v.Kind != Null {
			out = append(out, v)
		}
	}
	return out
}

// syncObjectProps returns the properties and required names of a schema,
// following allOf.
func syncObjectProps(doc *Doc, s *Node) (map[string]*Node, map[string]bool) {
	props := map[string]*Node{}
	required := map[string]bool{}
	stack := []*Node{s}
	for len(stack) > 0 {
		cur := syncDeref(doc, stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if !syncIsObject(cur) {
			continue
		}
		if ps := cur.Get("properties"); syncIsObject(ps) {
			for _, k := range ps.Keys {
				props[k] = ps.Get(k)
			}
		}
		for _, r := range cur.Get("required").Strings() {
			required[r] = true
		}
		stack = append(stack, cur.Get("allOf").Elems()...)
	}
	return props, required
}

// syncVisible returns the properties the generated type of a request or a
// response has (leftOut, as the generator reads them).
func syncVisible(doc *Doc, props map[string]*Node, request bool) map[string]*Node {
	side := Response
	if request {
		side = Request
	}
	out := make(map[string]*Node, len(props))
	for name, p := range props {
		if !leftOut(doc, p, side) {
			out[name] = p
		}
	}
	return out
}

// fieldNames returns the property names of a schema read as one type.
func (d *syncDiffer) fieldNames(doc *Doc, s *Node) map[string]bool {
	out := map[string]bool{}
	if !syncIsObject(syncDeref(doc, s)) {
		return out
	}
	props, _ := syncObjectProps(doc, d.single(doc, s, 0))
	for k := range props {
		out[k] = true
	}
	return out
}

// syncJoin appends a property name to a field path.
func syncJoin(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func syncSameSet(a, b map[string]bool) bool {
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

func syncSortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// syncCommon counts the names a and b share.
func syncCommon(a, b map[string]bool) int {
	n := 0
	for k := range a {
		if b[k] {
			n++
		}
	}
	return n
}

// syncContains reports whether list has an item equal to v.
func syncContains(list []*Node, v *Node) bool {
	for _, item := range list {
		if syncEqual(item, v) {
			return true
		}
	}
	return false
}

func syncSortedNames(m map[string]*Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- schema comparison ------------------------------------------------------

// diffSchema appends a change for every difference between two schemas. old
// belongs to d.old and new to d.new; where and path locate them in their
// operation, and request tells which side of the call they are on.
//
// Breaking (docs/DESIGN.md section 3.1, the same in requests and responses):
// a type change in either direction, a value that can now be null, a removed
// field, enum value or union variant, and in a request a field that is newly
// required. Everything else is listed, not counted.
func (d *syncDiffer) diffSchema(old, new *Node, where, path string, request bool, out *[]syncChange, depth int) {
	if depth > syncMaxDepth || d.sameShape(old, new, 0) {
		return
	}
	start := len(*out)
	// A body that appears or disappears is compared field by field, not as a
	// type change.
	present := old != syncNoSchema && new != syncNoSchema
	note := func(at, kind string, breaking bool) {
		*out = append(*out, syncChange{where: where, path: at, kind: kind, breaking: breaking})
	}
	if !syncIsObject(syncDeref(d.old, old)) || !syncIsObject(syncDeref(d.new, new)) {
		note(path, "schema replaced", true)
		return
	}
	oldNull, newNull := syncNullable(d.old, old, 0), syncNullable(d.new, new, 0)
	switch {
	case newNull && !oldNull:
		note(path, "can now be null", true)
	case oldNull && !newNull && request:
		note(path, "no longer accepts null", false)
	case oldNull && !newNull:
		note(path, "can no longer be null", false)
	}
	o, n := d.single(d.old, old, 0), d.single(d.new, new, 0)
	oldKinds, newKinds := syncKinds(d.old, o, 0), syncKinds(d.new, n, 0)
	before, after := syncTypeNames(o, oldKinds), syncTypeNames(n, newKinds)
	// "any" (a schema that does not say, an interface value in Go) against a
	// type counts too.
	if present && (len(oldKinds) > 0 || len(newKinds) > 0) && before != after {
		note(path, "type changed from "+before+" to "+after, true)
		if len(oldKinds) > 0 && len(newKinds) > 0 {
			// A union's variants can change too; the type change already
			// covers those removed.
			d.diffVariants(o, n, where, path, request, out, depth, true)
		}
		return
	}

	oldEnum, newEnum := syncEnum(o), syncEnum(n)
	switch {
	case oldEnum != nil && newEnum != nil:
		for _, v := range oldEnum {
			if !syncContains(newEnum, v) {
				note(path, "enum value "+syncEncode(v, syncUnicodeInlineStyle)+" removed", true)
			}
		}
		var added []*Node
		for _, v := range newEnum {
			if !syncContains(oldEnum, v) {
				added = append(added, v)
			}
		}
		if len(added) > 0 {
			note(path, "enum values added "+syncEncode(syncArray(added), syncUnicodeInlineStyle), false)
		}
	case newEnum != nil:
		note(path, "now limited to "+syncEncode(syncArray(newEnum), syncUnicodeInlineStyle), false)
	case oldEnum != nil:
		note(path, "no longer limited to fixed values", false)
	}

	oldProps, oldReq := syncObjectProps(d.old, o)
	newProps, newReq := syncObjectProps(d.new, n)
	// A union's fields live in its variants, compared below.
	if (len(oldProps) > 0 && len(newProps) > 0) || (len(syncVariants(d.old, o)) == 0 && len(syncVariants(d.new, n)) == 0) {
		// A field the generated type leaves out on this side is not there for
		// typed code: one that becomes readOnly in a request or writeOnly in
		// a response is removed.
		oldSeen, newSeen := syncVisible(d.old, oldProps, request), syncVisible(d.new, newProps, request)
		for _, name := range syncSortedNames(oldSeen) {
			switch {
			case newSeen[name] != nil:
			case newProps[name] != nil && request:
				note(syncJoin(path, name), "removed (now read-only)", true)
			case newProps[name] != nil:
				note(syncJoin(path, name), "removed (now write-only)", true)
			default:
				note(syncJoin(path, name), "removed", true)
			}
		}
		for _, name := range syncSortedNames(newSeen) {
			if oldSeen[name] != nil {
				continue
			}
			if request && newReq[name] {
				note(syncJoin(path, name), "added (required)", true)
			} else {
				note(syncJoin(path, name), "added", false)
			}
		}
		for _, name := range syncSortedNames(oldSeen) {
			if newSeen[name] == nil {
				continue
			}
			at := syncJoin(path, name)
			switch was, now := oldReq[name], newReq[name]; {
			case now && !was && request:
				note(at, "now required", true)
			case now && !was:
				note(at, "now always present", false)
			case was && !now && request:
				note(at, "no longer required", false)
			case was && !now:
				note(at, "no longer always present", false)
			}
			d.diffSchema(oldSeen[name], newSeen[name], where, at, request, out, depth+1)
		}
	}
	for _, kw := range []struct{ key, suffix string }{{"items", "[]"}, {"additionalProperties", "{}"}} {
		ov, nv := o.Get(kw.key), n.Get(kw.key)
		oObj, nObj := syncIsObject(syncDeref(d.old, ov)), syncIsObject(syncDeref(d.new, nv))
		if !oObj && !nObj {
			continue
		}
		if !oObj {
			ov = syncObject()
		}
		if !nObj {
			nv = syncObject()
		}
		d.diffSchema(ov, nv, where, path+kw.suffix, request, out, depth+1)
	}
	d.diffVariants(o, n, where, path, request, out, depth, false)
	if len(*out) == start {
		note(path, "other constraints changed (format, pattern, limits or default)", false)
	}
}

// diffVariants compares two schemas read as one type (single) variant by
// variant; a schema that is not a union is a union of one, so that [X, null]
// against X loses no variant and X against [X', Y] compares X with X'. A
// variant added or removed never hides a change inside the others. A variant
// is named by its new position. retyped tells that a type change was
// reported, which already covers the variants removed.
func (d *syncDiffer) diffVariants(old, new *Node, where, path string, request bool, out *[]syncChange, depth int, retyped bool) {
	oldVars, newVars := syncVariants(d.old, old), syncVariants(d.new, new)
	if len(oldVars) == 0 {
		oldVars = []*Node{old}
	}
	if len(newVars) == 0 {
		newVars = []*Node{new}
	}
	if len(oldVars) == 1 && len(newVars) == 1 {
		return // compared as the schema itself
	}
	keyword := syncUnionKeyword(d.new, new)
	if keyword == "" {
		keyword = syncUnionKeyword(d.old, old)
	}
	pairs, removed, added := d.pairVariants(oldVars, newVars)
	for _, p := range pairs {
		at := path
		if len(newVars) > 1 {
			at = fmt.Sprintf("%s(%s %d)", path, keyword, p[1]+1)
		}
		d.diffSchema(oldVars[p[0]], newVars[p[1]], where, at, request, out, depth+1)
	}
	if len(removed) > 0 && !retyped {
		*out = append(*out, syncChange{where: where, path: path, kind: fmt.Sprintf("type changed: %d %s variant(s) removed", len(removed), keyword), breaking: true})
	}
	if len(added) > 0 {
		*out = append(*out, syncChange{where: where, path: path, kind: fmt.Sprintf("%d %s variant(s) added", len(added), keyword)})
	}
	byOld := append([][2]int(nil), pairs...)
	sort.Slice(byOld, func(i, j int) bool { return byOld[i][0] < byOld[j][0] })
	for i := 1; i < len(byOld); i++ {
		if byOld[i][1] < byOld[i-1][1] {
			*out = append(*out, syncChange{where: where, path: path, kind: keyword + " variants reordered"})
			break
		}
	}
}

// pairVariants pairs each old union variant with the new one it became: an
// unchanged one first, then one with the same title, then one of the same
// type with the most fields in common. It returns the pairs (old and new
// position, in new order) and the positions left over on each side (the
// variants removed and added).
func (d *syncDiffer) pairVariants(old, new []*Node) (pairs [][2]int, leftOld, leftNew []int) {
	for i := range old {
		leftOld = append(leftOld, i)
	}
	for j := range new {
		leftNew = append(leftNew, j)
	}
	without := func(list []int, v int) []int {
		out := list[:0:0]
		for _, x := range list {
			if x != v {
				out = append(out, x)
			}
		}
		return out
	}
	pair := func(i, j int) {
		pairs = append(pairs, [2]int{i, j})
		leftOld, leftNew = without(leftOld, i), without(leftNew, j)
	}
	// A component's title is not the variant's own.
	title := func(doc *Doc, v *Node) *Node {
		if d.refs[v] || (syncIsObject(v) && v.Has("$ref")) {
			return nil
		}
		t := syncDeref(doc, v).Get("title")
		if t == nil || t.Kind == Null {
			return nil
		}
		return t
	}
	sameShape := func(i, j int) bool { return d.sameShape(old[i], new[j], 0) }
	sameTitle := func(i, j int) bool {
		t := title(d.old, old[i])
		return t != nil && syncEqual(t, title(d.new, new[j]))
	}
	for _, same := range []func(i, j int) bool{sameShape, sameTitle} {
		for _, i := range append([]int(nil), leftOld...) {
			for _, j := range leftNew {
				if same(i, j) {
					pair(i, j)
					break
				}
			}
		}
	}
	type candidate struct{ common, i, j int }
	var candidates []candidate
	for _, i := range leftOld {
		oldKinds, oldFields := syncKinds(d.old, old[i], 0), d.fieldNames(d.old, old[i])
		for _, j := range leftNew {
			newKinds := syncKinds(d.new, new[j], 0)
			if len(oldKinds) > 0 && len(newKinds) > 0 && !syncSameSet(oldKinds, newKinds) {
				continue
			}
			candidates = append(candidates, candidate{syncCommon(oldFields, d.fieldNames(d.new, new[j])), i, j})
		}
	}
	sort.Slice(candidates, func(a, b int) bool {
		x, y := candidates[a], candidates[b]
		if x.common != y.common {
			return x.common > y.common
		}
		if x.i != y.i {
			return x.i < y.i
		}
		return x.j < y.j
	})
	has := func(list []int, v int) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	for _, c := range candidates {
		if has(leftOld, c.i) && has(leftNew, c.j) {
			pair(c.i, c.j)
		}
	}
	sort.SliceStable(pairs, func(a, b int) bool { return pairs[a][1] < pairs[b][1] })
	return pairs, leftOld, leftNew
}

// ---- operations ---------------------------------------------------------------

// syncParamKey identifies a parameter.
type syncParamKey struct{ in, name string }

// where names the parameter in a change.
func (k syncParamKey) where() string { return fmt.Sprintf("parameter `%s` (%s)", k.name, k.in) }

func (d *syncDiffer) params(doc *Doc, op *Node) (map[syncParamKey]*Node, []syncParamKey) {
	out := map[syncParamKey]*Node{}
	for _, p := range op.Get("parameters").Elems() {
		p = syncDeref(doc, p)
		if !syncIsObject(p) {
			continue
		}
		k := syncParamKey{p.Get("in").Text(), p.Get("name").Text()}
		out[k] = p // a later definition (the operation's own) wins
	}
	keys := make([]syncParamKey, 0, len(out))
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

// syncMedia is a request body or response: the schema of each media type.
type syncMedia map[string]*Node

func (m syncMedia) sorted() []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// syncContent returns the schema of each media type of a request body or
// response (an empty object for a media type without one).
func syncContent(doc *Doc, n *Node) syncMedia {
	out := syncMedia{}
	content := syncDeref(doc, n).Get("content")
	if !syncIsObject(content) {
		return out
	}
	for _, media := range content.Keys {
		if entry := syncDeref(doc, content.Get(media)); syncIsObject(entry) {
			out[media] = syncOrObject(entry.Get("schema"))
		}
	}
	return out
}

// syncSuccesses returns the 2xx responses of an operation by status.
func syncSuccesses(doc *Doc, op *Node) map[string]syncMedia {
	out := map[string]syncMedia{}
	responses := syncOrObject(op.Get("responses"))
	for _, code := range responses.ObjKeys() {
		if strings.HasPrefix(code, "2") {
			out[code] = syncContent(doc, responses.Get(code))
		}
	}
	return out
}

func syncSortedStatuses(m map[string]syncMedia) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sameContent reports whether two bodies (old from d.old, new from d.new)
// have the same media types with the same schemas.
func (d *syncDiffer) sameContent(old, new syncMedia) bool {
	if len(old) != len(new) {
		return false
	}
	for media, s := range old {
		if n, ok := new[media]; !ok || !d.sameShape(s, n, 0) {
			return false
		}
	}
	return true
}

// diffContent compares a body per media type. A media type the new body no
// longer has is compared with the one that replaces it (an added one first,
// then the one with the most fields in common), so that a field only it
// carried (a multipart file) is not lost; against a media type both bodies
// have, only what breaks counts, since that one's own changes are compared
// directly. A body that appears or disappears is compared field by field.
func (d *syncDiffer) diffContent(old, new syncMedia, where string, request bool, out *[]syncChange) {
	oldTypes, newTypes := old.sorted(), new.sorted()
	if len(old) > 0 && len(new) > 0 && strings.Join(oldTypes, "\n") != strings.Join(newTypes, "\n") {
		*out = append(*out, syncChange{where: where, kind: fmt.Sprintf("content type %s -> %s", strings.Join(oldTypes, ", "), strings.Join(newTypes, ", "))})
	}
	if len(old) == 0 {
		for _, media := range newTypes {
			d.diffSchema(syncNoSchema, new[media], where, "", request, out, 0)
		}
		return
	}
	var added []string
	for _, media := range newTypes {
		if old[media] == nil {
			added = append(added, media)
		}
	}
	for _, media := range oldTypes {
		if n := new[media]; n != nil || len(new) == 0 {
			if n == nil {
				n = syncNoSchema
			}
			d.diffSchema(old[media], n, where, "", request, out, 0)
			continue
		}
		fields := d.fieldNames(d.old, old[media])
		candidates := added
		if len(candidates) == 0 {
			candidates = newTypes
		}
		after, best := "", -1
		for _, m := range candidates {
			if c := syncCommon(fields, d.fieldNames(d.new, new[m])); c > best {
				after, best = m, c
			}
		}
		if i := sort.SearchStrings(added, after); i < len(added) && added[i] == after {
			added = append(added[:i:i], added[i+1:]...)
			d.diffSchema(old[media], new[after], where, "", request, out, 0)
			continue
		}
		var found []syncChange
		d.diffSchema(old[media], new[after], where, "", request, &found, 0)
		for _, c := range found {
			if c.breaking {
				*out = append(*out, c)
			}
		}
	}
}

// diffStatuses lists the success statuses only one side has. A method
// returns every 2xx body, nullable when one has none: a status without a
// body makes it nullable, and a removed status whose body no other one
// returns removes a variant of it (docs/DESIGN.md section 3.1).
func (d *syncDiffer) diffStatuses(old, new map[string]syncMedia) []syncChange {
	var found []syncChange
	add := func(kind string, breaking bool) {
		found = append(found, syncChange{where: "response", kind: kind, breaking: breaking})
	}
	returnsNull := func(responses map[string]syncMedia) bool {
		for _, c := range responses {
			if len(c) == 0 {
				return true
			}
		}
		return false
	}
	for _, status := range syncSortedStatuses(new) {
		if _, ok := old[status]; ok {
			continue
		}
		body := new[status]
		returned := false
		for _, c := range old {
			returned = returned || (len(c) > 0 && d.sameContent(c, body))
		}
		switch {
		case len(body) == 0 && syncAnyBody(old) && !returnsNull(old):
			add(fmt.Sprintf("can now be null (status %s has no body)", status), true)
		case len(body) > 0 && !returned:
			add(fmt.Sprintf("status %s added with another body", status), false)
		default:
			add(fmt.Sprintf("status %s added", status), false)
		}
	}
	for _, status := range syncSortedStatuses(old) {
		if _, ok := new[status]; ok {
			continue
		}
		body := old[status]
		returned := false
		for _, c := range new {
			returned = returned || (len(c) > 0 && d.sameContent(body, c))
		}
		switch {
		case len(body) == 0 && syncAnyBody(new) && !returnsNull(new):
			add(fmt.Sprintf("can no longer be null (status %s removed)", status), false)
		case len(body) > 0 && !returned:
			add(fmt.Sprintf("type changed: status %s removed, and no other status returns its body", status), true)
		default:
			add(fmt.Sprintf("status %s removed", status), false)
		}
	}
	return found
}

// syncAnyBody reports whether a response of responses has a body.
func syncAnyBody(responses map[string]syncMedia) bool {
	for _, c := range responses {
		if len(c) > 0 {
			return true
		}
	}
	return false
}

// describeChange returns every difference of one operation, or of a method
// that moved to another one (both with their path-level parameters merged
// in): parameters, request body, every success response, deprecation and
// x-didit-sdk. Nothing when only documentation changed; sameOperation tells
// whether the rest of the operation (error responses, security, headers)
// changed. old belongs to d.old, new to d.new.
func (d *syncDiffer) describeChange(old, new *Node) []syncChange {
	var found []syncChange
	add := func(where, kind string, breaking bool) {
		found = append(found, syncChange{where: where, kind: kind, breaking: breaking})
	}

	oldParams, oldKeys := d.params(d.old, old)
	newParams, newKeys := d.params(d.new, new)
	for _, k := range oldKeys {
		if newParams[k] == nil {
			add(k.where(), "removed", false)
		}
	}
	for _, k := range newKeys {
		if oldParams[k] != nil {
			continue
		}
		if newParams[k].Get("required").Truthy() {
			add(k.where(), "added (required)", true)
		} else {
			add(k.where(), "added", false)
		}
	}
	for _, k := range oldKeys {
		np := newParams[k]
		if np == nil {
			continue
		}
		if !oldParams[k].Get("required").Truthy() && np.Get("required").Truthy() {
			add(k.where(), "now required", true)
		}
		d.diffSchema(syncOrObject(oldParams[k].Get("schema")), syncOrObject(np.Get("schema")), k.where(), "", true, &found, 0)
	}

	oldBody, newBody := syncOrObject(old.Get("requestBody")), syncOrObject(new.Get("requestBody"))
	if !syncDeref(d.old, oldBody).Get("required").Truthy() && syncDeref(d.new, newBody).Get("required").Truthy() {
		add("request body", "now required", false)
	}
	d.diffContent(syncContent(d.old, oldBody), syncContent(d.new, newBody), "request body", true, &found)

	// Every 2xx response both have; when the only success status changed
	// (200 to 201), the old body against the new one; otherwise the statuses
	// only one side has.
	oldResponses, newResponses := syncSuccesses(d.old, old), syncSuccesses(d.new, new)
	var statuses [][2]string
	for _, s := range syncSortedStatuses(oldResponses) {
		if _, ok := newResponses[s]; ok {
			statuses = append(statuses, [2]string{s, s})
		}
	}
	if len(statuses) == 0 && len(oldResponses) == 1 && len(newResponses) == 1 {
		statuses = append(statuses, [2]string{syncSortedStatuses(oldResponses)[0], syncSortedStatuses(newResponses)[0]})
		add("response", fmt.Sprintf("status %s -> %s", statuses[0][0], statuses[0][1]), false)
	} else {
		found = append(found, d.diffStatuses(oldResponses, newResponses)...)
	}
	for _, s := range statuses {
		d.diffContent(oldResponses[s[0]], newResponses[s[1]], "response", false, &found)
	}

	if !old.Get("deprecated").Truthy() && new.Get("deprecated").Truthy() {
		add("operation", "marked deprecated upstream", false)
	}
	if was, now := syncNotNull(old.Get("x-didit-sdk")), syncNotNull(new.Get("x-didit-sdk")); now != nil && !syncEqual(was, now) {
		add("x-didit-sdk", "now "+syncEncode(now, syncSortedInlineStyle), false)
	}

	// One entry per change, even when several responses or media types share
	// it.
	seen := map[syncChange]bool{}
	var out []syncChange
	for _, c := range found {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// syncNotNull returns n, or nil for JSON null.
func syncNotNull(n *Node) *Node {
	if n == nil || n.Kind == Null {
		return nil
	}
	return n
}

// syncNameMaps are the objects of an operation whose keys are names (status
// codes, media types, headers), not keywords.
var syncNameMaps = map[string]bool{
	"responses": true, "content": true, "headers": true, "encoding": true, "links": true, "callbacks": true,
}

// sameOperation reports whether two operations send and receive the same
// thing: equal once references are followed and documentation (summaries,
// descriptions, examples, tags, x-* keys) is ignored. Unlike describeChange
// it covers the whole operation, error responses, security and operationId
// included, as syncSameDocument does.
func (d *syncDiffer) sameOperation(old, new *Node) bool {
	return d.sameObservable(old, new, false, 0)
}

func (d *syncDiffer) sameObservable(a, b *Node, names bool, depth int) bool {
	if depth > 32 {
		return true
	}
	a, b = syncDeref(d.old, a), syncDeref(d.new, b)
	if a == nil || b == nil || a.Kind != b.Kind {
		return a == nil && b == nil
	}
	switch a.Kind {
	case Array:
		if len(a.Arr) != len(b.Arr) {
			return false
		}
		for i := range a.Arr {
			if !d.sameObservable(a.Arr[i], b.Arr[i], false, depth+1) {
				return false
			}
		}
		return true
	case Object:
		keys := func(n *Node) []string {
			var out []string
			for _, k := range n.Keys {
				if names || (!syncDocKeys[k] && !strings.HasPrefix(k, "x-")) {
					out = append(out, k)
				}
			}
			return out
		}
		ka, kb := keys(a), keys(b)
		if len(ka) != len(kb) {
			return false
		}
		for _, k := range ka {
			va, vb := a.Get(k), b.Get(k)
			if vb == nil {
				return false
			}
			same := false
			switch {
			case !names && k == "schema":
				same = d.sameShape(va, vb, 0)
			default:
				same = d.sameObservable(va, vb, !names && syncNameMaps[k], depth+1)
			}
			if !same {
				return false
			}
		}
		return true
	}
	return syncEqual(a, b)
}

// syncDocumentNameMaps are the objects of a document whose keys are names,
// as sdk-python's NAME_MAPS lists them.
var syncDocumentNameMaps = map[string]bool{
	"properties": true, "patternProperties": true, "paths": true, "schemas": true, "responses": true,
	"parameters": true, "requestBodies": true, "headers": true, "securitySchemes": true, "content": true,
	"encoding": true, "variables": true, "mapping": true, "x-webhooks": true,
}

// syncSameDocument reports whether a caller can observe no difference
// between two versions of an upstream document (sdk-python's observable):
// equal without info, documentation keys and x-* keys other than
// x-webhooks, with required and type lists in any order. References are
// compared as written. x-didit-sdk is left out: the sync compares the
// effective naming, overlay included, on its own.
func syncSameDocument(a, b *Node) bool {
	strip := func(n *Node) *Node {
		out := syncCopyObject(n)
		syncDeleteKey(out, "info")
		return out
	}
	return syncSameObservableJSON(strip(a), strip(b), false)
}

func syncSameObservableJSON(a, b *Node, names bool) bool {
	if a == nil || b == nil || a.Kind != b.Kind {
		return a == nil && b == nil
	}
	switch a.Kind {
	case Array:
		if len(a.Arr) != len(b.Arr) {
			return false
		}
		for i := range a.Arr {
			if !syncSameObservableJSON(a.Arr[i], b.Arr[i], false) {
				return false
			}
		}
		return true
	case Object:
		keep := func(k string) bool {
			return names || (!syncDocKeys[k] && (!strings.HasPrefix(k, "x-") || k == "x-webhooks"))
		}
		count := 0
		for _, k := range b.Keys {
			if keep(k) {
				count++
			}
		}
		for _, k := range a.Keys {
			if !keep(k) {
				continue
			}
			count--
			va, vb := a.Get(k), b.Get(k)
			switch {
			case vb == nil:
				return false
			case !names && (k == "required" || k == "type") && va.Kind == Array && vb.Kind == Array:
				if !syncSameItems(va, vb) {
					return false
				}
			case !syncSameObservableJSON(va, vb, !names && syncDocumentNameMaps[k]):
				return false
			}
		}
		return count == 0
	}
	return syncEqual(a, b)
}
