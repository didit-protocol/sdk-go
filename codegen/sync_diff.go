package main

import (
	"fmt"
	"sort"
	"strings"
)

// Comparison of operations and schemas for the spec sync summary.
// Operations are compared field by field, in parameters, request bodies and
// every success response, following $refs, allOf wrappers, array items and
// every oneOf/anyOf variant. Each difference is a syncChange; the ones of
// the docs/DESIGN.md section 3.1 list (a field or parameter whose type
// changed, a field or parameter that can now be null, a removed field, enum
// value or union variant, a newly required parameter or request field) are
// breaking: they can break typed code, and the pull request and the release
// notes list them. A removed operation is breaking too (sync_apply.go).
//
// References are followed lazily while comparing, so recursive schemas are
// never copied.

// syncMaxNotes caps the notes of one operation so that a huge change stays
// readable.
const syncMaxNotes = 25

// syncUnchangedNote describes an operation whose changes are documentation
// only.
const syncUnchangedNote = "descriptions, examples or other details changed"

// syncOtherNote describes an operation that changed outside what
// describeChange compares (error responses, security, headers...).
const syncOtherNote = "other request or response details changed"

// syncDocKeys document a schema without changing what is sent or received.
var syncDocKeys = map[string]bool{
	"description": true, "summary": true, "title": true,
	"example": true, "examples": true, "externalDocs": true,
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
}

func newSyncDiffer(old, new *Doc) *syncDiffer {
	return &syncDiffer{old: old, new: new, active: map[[2]*Node]bool{}, settled: map[[2]*Node]bool{}}
}

// sameShape reports whether two schemas send and receive the same thing:
// equal once references are followed and documentation keywords (syncDocKeys,
// x-*) are ignored. a belongs to the old document, b to the new one.
//
// Documentation keywords are only ignored where they are keywords: a
// property called "title" inside "properties" still counts. (The Python
// reference drops such keys at every level, so it misses changes to
// properties with those names.)
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
		default:
			if !syncEqual(va, vb) {
				return false
			}
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
	// where is the part of the operation: "response", "request body",
	// "parameter `limit` (query)", or "" for the operation itself.
	where string
	// path is the field inside it, for example
	// "registry_checks[].company.registry_data"; "" for the whole part.
	path string
	// kind says what changed: "can now be null", "type changed from string
	// to object", "removed", ...
	kind string
	// breaking marks a change of the docs/DESIGN.md section 3.1 list: one
	// that can break typed code.
	breaking bool
}

// target is where the change is: "response `a.b`", "request body".
func (c syncChange) target() string {
	switch {
	case c.path == "":
		return c.where
	case c.where == "":
		return "`" + c.path + "`"
	}
	return c.where + " `" + c.path + "`"
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

// syncSortBreaking sorts entries by operation, part, field and kind, and
// drops repeated ones (two success responses can report the same change), so
// that every sync lists the same change the same way.
func syncSortBreaking(list []syncBreaking) []syncBreaking {
	key := func(b syncBreaking) [4]string {
		return [4]string{b.operation, b.change.where, b.change.path, b.change.kind}
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := key(list[i]), key(list[j])
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
	var out []syncBreaking
	for i, b := range list {
		if i > 0 && key(b) == key(list[i-1]) {
			continue
		}
		out = append(out, b)
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

// ---- schemas as the generators type them -------------------------------------

// syncNormalize returns a schema the way the generators type it: $refs
// followed, a single-part allOf without properties of its own and a
// oneOf/anyOf with a single non-null variant unwrapped (an allOf wraps a
// reference to add a description or nullable; [X, null] is a nullable X).
// null reports whether null is allowed anywhere on the way, or by a variant
// of the union it ends on.
func syncNormalize(doc *Doc, s *Node) (*Node, bool) {
	return syncNormalizeDepth(doc, s, 0)
}

func syncNormalizeDepth(doc *Doc, s *Node, depth int) (*Node, bool) {
	null := false
	s = syncOrEmpty(s)
	for i := 0; i < 32; i++ {
		s = syncDeref(doc, s)
		if !syncIsObject(s) {
			break
		}
		if syncAllowsNull(doc, s) {
			null = true
		}
		if all := s.Get("allOf"); all != nil && len(all.Arr) == 1 && !s.Has("properties") {
			s = all.Arr[0]
			continue
		}
		v := syncUnion(doc, s)
		if len(v) == 1 {
			s = v[0]
			continue
		}
		for _, variant := range v {
			if depth < 8 {
				if _, n := syncNormalizeDepth(doc, variant, depth+1); n {
					null = true
				}
			}
		}
		break
	}
	return s, null
}

// syncAllowsNull reports whether a schema itself allows null: nullable, a
// "null" type, a null enum value or a null oneOf/anyOf variant.
func syncAllowsNull(doc *Doc, s *Node) bool {
	if s.Get("nullable").Truthy() {
		return true
	}
	switch t := s.Get("type"); {
	case t == nil:
	case t.Kind == String:
		if t.Str == "null" {
			return true
		}
	case t.Kind == Array:
		for _, v := range t.Strings() {
			if v == "null" {
				return true
			}
		}
	}
	for _, v := range s.Get("enum").Elems() {
		if v.Kind == Null {
			return true
		}
	}
	for _, kw := range []string{"oneOf", "anyOf"} {
		for _, v := range s.Get(kw).Elems() {
			if syncNullOnly(doc, v) {
				return true
			}
		}
	}
	return false
}

// syncNullOnly reports whether a schema allows null and nothing else.
func syncNullOnly(doc *Doc, s *Node) bool {
	r := syncDeref(doc, s)
	if !syncIsObject(r) {
		return false
	}
	if r.Get("type").Text() == "null" {
		return true
	}
	e := r.Get("enum")
	return e != nil && len(e.Arr) == 1 && e.Arr[0].Kind == Null
}

// syncUnion returns the non-null variants of a schema's oneOf (or else
// anyOf), or nil when it is not a union. Like the generators, an allOf with
// parts takes precedence.
func syncUnion(doc *Doc, s *Node) []*Node {
	if all := s.Get("allOf"); all != nil && len(all.Arr) > 0 {
		return nil
	}
	for _, kw := range []string{"oneOf", "anyOf"} {
		parts := s.Get(kw)
		if parts == nil || len(parts.Arr) == 0 {
			continue
		}
		out := []*Node{}
		for _, v := range parts.Arr {
			if !syncNullOnly(doc, v) {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

// syncKinds returns the JSON types a normalized schema allows besides null,
// empty when it allows any value. A string format the SDKs type apart is
// part of the type: "string (date-time)", "string (binary)".
func syncKinds(doc *Doc, s *Node, depth int) map[string]bool {
	out := map[string]bool{}
	if !syncIsObject(s) || depth > 16 {
		return out
	}
	switch t := s.Get("type"); {
	case t == nil:
	case t.Kind == String:
		if t.Str != "null" {
			out[t.Str] = true
		}
	case t.Kind == Array:
		for _, v := range t.Strings() {
			if v != "null" {
				out[v] = true
			}
		}
	}
	if len(out) == 0 {
		all := s.Get("allOf")
		switch {
		case s.Has("properties") || s.Has("additionalProperties") || s.Has("patternProperties"):
			out["object"] = true
		case s.Has("items"):
			out["array"] = true
		case s.Has("enum") || s.Has("const"):
			values := s.Get("enum").Elems()
			if c := s.Get("const"); c != nil {
				values = append(values, c)
			}
			for _, v := range values {
				if k := syncValueKind(v); k != "" {
					out[k] = true
				}
			}
		case all != nil && len(all.Arr) > 0:
			for _, p := range all.Arr {
				n, _ := syncNormalize(doc, p)
				for k := range syncKinds(doc, n, depth+1) {
					out[k] = true
				}
			}
		default:
			for _, v := range syncUnion(doc, s) {
				n, _ := syncNormalize(doc, v)
				kinds := syncKinds(doc, n, depth+1)
				if len(kinds) == 0 {
					return map[string]bool{} // a variant that allows any value
				}
				for k := range kinds {
					out[k] = true
				}
			}
		}
	}
	if len(out) == 1 && out["string"] {
		switch f := s.Get("format").Text(); f {
		case "date-time", "binary":
			return map[string]bool{"string (" + f + ")": true}
		}
	}
	return out
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
	case Object:
		return "object"
	case Array:
		return "array"
	}
	return ""
}

// syncKindLabel renders a set of kinds: "string", "integer/string", "any".
func syncKindLabel(kinds map[string]bool) string {
	if len(kinds) == 0 {
		return "any"
	}
	return syncJoinSorted(kinds)
}

// syncSubset reports whether every kind of a is in b.
func syncSubset(a, b map[string]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// syncMember returns keyword of a normalized schema, or else of the first of
// its allOf parts that has it.
func syncMember(doc *Doc, s *Node, keyword string) *Node {
	if v := s.Get(keyword); v != nil {
		return v
	}
	for _, p := range s.Get("allOf").Elems() {
		n, _ := syncNormalize(doc, p)
		if syncIsObject(n) {
			if v := n.Get(keyword); v != nil {
				return v
			}
		}
	}
	return nil
}

// syncEnum returns the enum values of a normalized schema without null, or
// nil when it has no enum.
func syncEnum(doc *Doc, s *Node) []*Node {
	e := syncMember(doc, s, "enum")
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

// syncRefName returns the component a schema references (directly or
// through a single-part allOf), or "".
func syncRefName(s *Node) string {
	for i := 0; syncIsObject(s) && i < 8; i++ {
		if ref := s.Get("$ref"); ref != nil && ref.Kind == String {
			return ref.Str[strings.LastIndex(ref.Str, "/")+1:]
		}
		all := s.Get("allOf")
		if all == nil || len(all.Arr) != 1 {
			break
		}
		s = all.Arr[0]
	}
	return ""
}

// syncObjectProps returns the properties and required names of an object
// schema, following allOf (in the Python reference's order, so the same
// definition wins when parts repeat a property).
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

// syncJoin appends a property name to a field path.
func syncJoin(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func syncJoinSorted(set map[string]bool) string {
	return strings.Join(syncSortedSet(set), "/")
}

func syncSameSet(a, b map[string]bool) bool {
	return len(a) == len(b) && syncSubset(a, b)
}

func syncSortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// ---- schema comparison ------------------------------------------------------

// diffSchema appends the differences between two schemas to out. where and
// path locate the schema in its operation; request tells which side it is
// on (a field that becomes required matters to callers that send it, one
// that is no longer always present to callers that read it).
func (d *syncDiffer) diffSchema(old, new *Node, where, path string, request bool, out *[]syncChange, depth int) {
	if depth > 16 {
		return
	}
	o, oNull := syncNormalize(d.old, old)
	n, nNull := syncNormalize(d.new, new)
	d.diffNormalized(o, n, oNull, nNull, where, path, request, out, depth)
}

// diffNormalized compares two normalized schemas (syncNormalize) and whether
// each allows null.
func (d *syncDiffer) diffNormalized(o, n *Node, oNull, nNull bool, where, path string, request bool, out *[]syncChange, depth int) {
	if oNull == nNull && d.sameShape(o, n, 0) {
		return
	}
	add := func(at, kind string, breaking bool) {
		*out = append(*out, syncChange{where: where, path: at, kind: kind, breaking: breaking})
	}
	switch {
	case nNull && !oNull:
		add(path, "can now be null", true)
	case oNull && !nNull && request:
		add(path, "no longer accepts null", false)
	case oNull && !nNull:
		add(path, "can no longer be null", false)
	}
	if !syncIsObject(o) || !syncIsObject(n) {
		if !syncEqual(o, n) {
			add(path, "schema replaced", true)
		}
		return
	}

	oKinds, nKinds := syncKinds(d.old, o, 0), syncKinds(d.new, n, 0)
	typeChanged := syncKindLabel(oKinds) != syncKindLabel(nKinds)
	if typeChanged {
		add(path, fmt.Sprintf("type changed from %s to %s", syncKindLabel(oKinds), syncKindLabel(nKinds)), true)
	}
	oVars, nVars := syncUnion(d.old, o), syncUnion(d.new, n)
	if len(oVars) > 1 || len(nVars) > 1 {
		// A union on either side: compare variant by variant, a plain schema
		// being the only variant of its side. Null was compared above.
		if len(oVars) <= 1 {
			oVars = []*Node{o}
		}
		if len(nVars) <= 1 {
			nVars = []*Node{n}
		}
		d.diffVariants(oVars, nVars, oKinds, nKinds, where, path, request, out, depth)
		return
	}
	if typeChanged {
		return
	}

	oEnum, nEnum := syncEnum(d.old, o), syncEnum(d.new, n)
	switch {
	case oEnum != nil && nEnum != nil:
		for _, v := range oEnum {
			if !syncContains(nEnum, v) {
				add(path, fmt.Sprintf("enum value %s removed", syncEncode(v, syncInlineStyle)), true)
			}
		}
		for _, v := range nEnum {
			if !syncContains(oEnum, v) {
				add(path, fmt.Sprintf("enum value %s added", syncEncode(v, syncInlineStyle)), false)
			}
		}
	case nEnum != nil:
		add(path, "now limited to "+syncEncode(syncArray(nEnum), syncInlineStyle), false)
	case oEnum != nil:
		add(path, "no longer limited to fixed values", false)
	}

	oProps, oReq := syncObjectProps(d.old, o)
	nProps, nReq := syncObjectProps(d.new, n)
	for _, name := range syncSortedNames(oProps) {
		if nProps[name] == nil {
			add(syncJoin(path, name), "removed", true)
		}
	}
	for _, name := range syncSortedNames(nProps) {
		if oProps[name] != nil {
			continue
		}
		if request && nReq[name] {
			add(syncJoin(path, name), "added, required", true)
		} else {
			add(syncJoin(path, name), "added", false)
		}
	}
	for _, name := range syncSortedNames(oProps) {
		if nProps[name] == nil {
			continue
		}
		at := syncJoin(path, name)
		switch was, now := oReq[name], nReq[name]; {
		case request && now && !was:
			add(at, "now required", true)
		case request && was && !now:
			add(at, "no longer required", false)
		case !request && was && !now:
			add(at, "no longer always present", false)
		case !request && now && !was:
			add(at, "now always present", false)
		}
		d.diffSchema(oProps[name], nProps[name], where, at, request, out, depth+1)
	}

	for _, kw := range []struct{ key, suffix string }{{"items", "[]"}, {"additionalProperties", "{}"}} {
		ov, nv := syncMember(d.old, o, kw.key), syncMember(d.new, n, kw.key)
		if syncIsObject(ov) || syncIsObject(nv) {
			d.diffSchema(syncFalsyEmpty(ov), syncFalsyEmpty(nv), where, path+kw.suffix, request, out, depth+1)
		}
	}
}

// diffVariants compares the variants of two unions. Variants pair up when
// they are equal, then when they reference the same component, then by type
// in order, and the changes inside each pair are reported like any other:
// a variant added or removed next to a changed one never hides the change.
// A removed variant is breaking unless the type change already reported
// covers it. Whether null is allowed was compared for the whole union, so a
// pair is compared without it.
func (d *syncDiffer) diffVariants(old, new []*Node, oKinds, nKinds map[string]bool, where, path string, request bool, out *[]syncChange, depth int) {
	kindsOf := func(doc *Doc, vs []*Node) []map[string]bool {
		out := make([]map[string]bool, len(vs))
		for i, v := range vs {
			n, _ := syncNormalize(doc, v)
			out[i] = syncKinds(doc, n, 0)
		}
		return out
	}
	oK, nK := kindsOf(d.old, old), kindsOf(d.new, new)
	pair := make([]int, len(old))
	for i := range pair {
		pair[i] = -1
	}
	taken := make([]bool, len(new))
	match := func(same func(i, j int) bool) {
		for i := range old {
			for j := 0; pair[i] < 0 && j < len(new); j++ {
				if !taken[j] && same(i, j) {
					pair[i], taken[j] = j, true
				}
			}
		}
	}
	match(func(i, j int) bool { return d.sameShape(old[i], new[j], 0) })
	match(func(i, j int) bool { r := syncRefName(old[i]); return r != "" && r == syncRefName(new[j]) })
	match(func(i, j int) bool { return syncKindLabel(oK[i]) == syncKindLabel(nK[j]) })
	name := func(v *Node, kinds map[string]bool) string {
		if r := syncRefName(v); r != "" {
			return r
		}
		return syncKindLabel(kinds)
	}
	for i, j := range pair {
		if j >= 0 {
			vo, _ := syncNormalize(d.old, old[i])
			vn, _ := syncNormalize(d.new, new[j])
			d.diffNormalized(vo, vn, false, false, where, path, request, out, depth+1)
			continue
		}
		if len(oK[i]) == 0 || syncSubset(oK[i], nKinds) {
			*out = append(*out, syncChange{where: where, path: path, kind: fmt.Sprintf("variant `%s` removed", name(old[i], oK[i])), breaking: true})
		}
	}
	for j := range new {
		if !taken[j] && (len(nK[j]) == 0 || syncSubset(nK[j], oKinds)) {
			*out = append(*out, syncChange{where: where, path: path, kind: fmt.Sprintf("variant `%s` added", name(new[j], nK[j]))})
		}
	}
}

// syncFalsyEmpty is Python's `value or {}`: nil and false become an empty
// object.
func syncFalsyEmpty(n *Node) *Node {
	if n == nil || (n.Kind == Bool && !n.Bool) {
		return syncObject()
	}
	return n
}

func syncSortedNames(m map[string]*Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// syncBodyTypes are the request content types whose schema is compared, in
// order of preference.
var syncBodyTypes = []string{"application/json", "multipart/form-data", "application/x-www-form-urlencoded"}

// syncRequestBody returns the request body, its content types and the
// schema of its preferred content type (syncBodyTypes).
func syncRequestBody(doc *Doc, op *Node) (*Node, map[string]bool, *Node) {
	rb := syncDeref(doc, op.Get("requestBody"))
	content := rb.Get("content")
	types := map[string]bool{}
	for _, ct := range content.ObjKeys() {
		types[ct] = true
	}
	var schema *Node
	for _, ct := range syncBodyTypes {
		if media := content.Get(ct); syncIsObject(media) && len(media.Keys) > 0 {
			schema = media.Get("schema")
			break
		}
	}
	return rb, types, schema
}

// syncResponse is one success response of an operation.
type syncResponse struct {
	types  map[string]bool // content types
	schema *Node           // of the JSON content, or nil
}

// syncSuccessResponses returns the 2xx responses of an operation by status,
// and the statuses in order.
func syncSuccessResponses(doc *Doc, op *Node) (map[string]syncResponse, []string) {
	out := map[string]syncResponse{}
	var codes []string
	responses := op.Get("responses")
	for _, code := range syncSortedKeys(syncOrEmpty(responses)) {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		content := syncDeref(doc, responses.Get(code)).Get("content")
		types := map[string]bool{}
		for _, ct := range content.ObjKeys() {
			types[ct] = true
		}
		out[code] = syncResponse{types: types, schema: content.Path("application/json", "schema")}
		codes = append(codes, code)
	}
	return out, codes
}

// diffBody compares two body schemas (request or response). A body that
// appears or disappears is compared with an empty object when the other side
// is an object, so its fields show as added or removed.
func (d *syncDiffer) diffBody(old, new *Node, where string, request bool, out *[]syncChange) {
	if old == nil && new == nil {
		return
	}
	empty := mustSyncNode(`{"type": "object"}`)
	if old == nil {
		if n, _ := syncNormalize(d.new, new); !syncKinds(d.new, n, 0)["object"] {
			return
		}
		old = empty
	}
	if new == nil {
		if o, _ := syncNormalize(d.old, old); !syncKinds(d.old, o, 0)["object"] {
			return
		}
		new = empty
	}
	d.diffSchema(old, new, where, "", request, out, 0)
}

// mustSyncNode parses a JSON literal of the code.
func mustSyncNode(s string) *Node {
	n, err := ParseNode([]byte(s))
	if err != nil {
		panic(err)
	}
	return n
}

// describeChange returns the differences of one operation (with the
// path-level parameters merged in): parameters, request body, every success
// response, deprecation and naming. Nothing when only documentation changed;
// sameOperation tells whether the rest of the operation (error responses,
// security, headers) changed. old belongs to d.old, new to d.new.
func (d *syncDiffer) describeChange(old, new *Node) []syncChange {
	var found []syncChange
	add := func(c syncChange) { found = append(found, c) }

	oldParams, oldKeys := d.params(d.old, old)
	newParams, newKeys := d.params(d.new, new)
	for _, k := range oldKeys {
		if newParams[k] == nil {
			add(syncChange{where: k.where(), kind: "removed"})
		}
	}
	for _, k := range newKeys {
		if oldParams[k] != nil {
			continue
		}
		if newParams[k].Get("required").Truthy() {
			add(syncChange{where: k.where(), kind: "added, required", breaking: true})
		} else {
			add(syncChange{where: k.where(), kind: "added"})
		}
	}
	for _, k := range oldKeys {
		np := newParams[k]
		if np == nil {
			continue
		}
		switch was, now := oldParams[k].Get("required").Truthy(), np.Get("required").Truthy(); {
		case now && !was:
			add(syncChange{where: k.where(), kind: "now required", breaking: true})
		case was && !now:
			add(syncChange{where: k.where(), kind: "no longer required"})
		}
		d.diffSchema(oldParams[k].Get("schema"), np.Get("schema"), k.where(), "", true, &found, 0)
	}

	oldRB, oldTypes, oldBody := syncRequestBody(d.old, old)
	newRB, newTypes, newBody := syncRequestBody(d.new, new)
	switch {
	case syncSameSet(oldTypes, newTypes):
	case len(oldTypes) == 0:
		add(syncChange{where: "request body", kind: "added"})
	case len(newTypes) == 0:
		add(syncChange{where: "request body", kind: "removed"})
	default:
		add(syncChange{where: "request body", kind: fmt.Sprintf("content types %s changed to %s", syncPyList(syncSortedSet(oldTypes)), syncPyList(syncSortedSet(newTypes)))})
	}
	switch was, now := oldRB.Get("required").Truthy(), newRB.Get("required").Truthy(); {
	case now && !was:
		add(syncChange{where: "request body", kind: "now required"})
	case was && !now && len(newTypes) > 0:
		add(syncChange{where: "request body", kind: "no longer required"})
	}
	d.diffBody(oldBody, newBody, "request body", true, &found)

	oldResp, oldCodes := syncSuccessResponses(d.old, old)
	newResp, newCodes := syncSuccessResponses(d.new, new)
	var pairs [][2]string
	for _, code := range oldCodes {
		if _, ok := newResp[code]; ok {
			pairs = append(pairs, [2]string{code, code})
		} else {
			add(syncChange{where: "response " + code, kind: "removed"})
		}
	}
	for _, code := range newCodes {
		if _, ok := oldResp[code]; !ok {
			add(syncChange{where: "response " + code, kind: "added"})
		}
	}
	if len(pairs) == 0 && len(oldCodes) > 0 && len(newCodes) > 0 {
		// The success status changed: compare what each one returns.
		pairs = append(pairs, [2]string{oldCodes[0], newCodes[0]})
	}
	for _, p := range pairs {
		o, n := oldResp[p[0]], newResp[p[1]]
		if !syncSameSet(o.types, n.types) {
			add(syncChange{where: "response", kind: fmt.Sprintf("content types %s changed to %s", syncPyList(syncSortedSet(o.types)), syncPyList(syncSortedSet(n.types)))})
		}
		d.diffBody(o.schema, n.schema, "response", false, &found)
	}

	switch was, now := old.Get("deprecated").Truthy(), new.Get("deprecated").Truthy(); {
	case now && !was:
		add(syncChange{kind: "marked deprecated upstream"})
	case was && !now:
		add(syncChange{kind: "no longer marked deprecated upstream"})
	}
	if ns := new.Get("x-didit-sdk"); ns != nil && ns.Kind != Null && !syncEqual(old.Get("x-didit-sdk"), ns) {
		add(syncChange{kind: "x-didit-sdk now " + syncEncode(ns, syncSortedInlineStyle)})
	}

	// Two success responses can report the same change once each.
	seen := map[[3]string]int{}
	var out []syncChange
	for _, c := range found {
		k := [3]string{c.where, c.path, c.kind}
		if i, dup := seen[k]; dup {
			out[i].breaking = out[i].breaking || c.breaking
			continue
		}
		seen[k] = len(out)
		out = append(out, c)
	}
	return out
}

// syncNameMaps are the objects of an operation whose keys are names (status
// codes, media types, headers), not keywords.
var syncNameMaps = map[string]bool{
	"responses": true, "content": true, "headers": true, "encoding": true, "links": true, "callbacks": true,
}

// sameOperation reports whether two operations send and receive the same
// thing: equal once references are followed and documentation (summaries,
// descriptions, examples, tags, operationId, x-* keys) is ignored. Unlike
// describeChange it covers the whole operation, error responses and
// security included.
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
				if names || (!syncDocKeys[k] && !strings.HasPrefix(k, "x-") && k != "tags" && k != "operationId") {
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
