package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/didit-protocol/sdk-go/internal/naming"
)

// Side tells whether a schema is used to build a request or to read a
// response: request types use Opt for optional fields, response types plain
// values.
type Side uint8

// Sides.
const (
	Request Side = iota
	Response
)

// TKind is the kind of a Go type.
type TKind uint8

// Go type kinds.
const (
	TAny TKind = iota
	TString
	TInt
	TFloat
	TBool
	TTime
	TFile
	TSlice
	TMap
	TNamed
	// TRaw is undecoded JSON, for a response that declares no content.
	TRaw
)

// GoType is a Go type expression.
type GoType struct {
	Kind TKind
	Name string // TNamed
	Elem *GoType
	// Decl is the declaration of a TNamed type.
	Decl *Decl
}

// Go returns the type expression.
func (t *GoType) Go() string {
	switch t.Kind {
	case TString:
		return "string"
	case TInt:
		return "int64"
	case TFloat:
		return "float64"
	case TBool:
		return "bool"
	case TTime:
		return "time.Time"
	case TFile:
		return "File"
	case TSlice:
		return "[]" + t.Elem.Go()
	case TMap:
		return "map[string]" + t.Elem.Go()
	case TNamed:
		return t.Name
	case TRaw:
		return "json.RawMessage"
	default:
		return "any"
	}
}

// DeclKind is the kind of a declaration.
type DeclKind uint8

// Declaration kinds.
const (
	DStruct DeclKind = iota
	DEnum
)

// Decl is a generated named type.
type Decl struct {
	Name   string
	Kind   DeclKind
	Side   Side
	Doc    string
	Fields []*Field
	Values []EnumValue
	// Params marks the parameter struct of an operation.
	Params bool
	Origin string
	Owners map[string]bool
	order  int
}

// Field is a struct field.
type Field struct {
	GoName string
	Name   string
	Type   *GoType
	// Opt wraps the type in Opt[T] (optional request fields).
	Opt bool
	// Pointer makes the field a pointer (recursive types).
	Pointer bool
	Doc     string
	// Tag is the struct tag kind: json, form, query, header or body.
	Tag      string
	Required bool
}

// EnumValue is one known value of an enum.
type EnumValue struct {
	GoName string
	Value  string
	Doc    string
}

// Gen holds the generator state.
type Gen struct {
	spec     *Spec
	decls    []*Decl
	byName   map[string]*Decl
	comps    map[string]*GoType
	compUse  map[string]uint8
	reserved map[string]string
	// paramPrefixes maps the request-side name of a component used on both
	// sides to its response-side name, so their enums are shared.
	paramPrefixes map[string]string
}

const (
	useRequest  uint8 = 1
	useResponse uint8 = 2
)

// reservedNames are hand-written identifiers of package didit.
var reservedNames = []string{
	"Client", "NewClient", "Opt", "Some", "Null", "String", "Int", "Float", "Bool", "Time",
	"File", "FileFromPath", "FileFromBytes", "FileFromReader", "Page", "PageIterator",
	"ResponseJSON", "Error", "ConfigurationError", "APIConnectionError", "APITimeoutError",
	"APIStatusError", "BadRequestError", "AuthenticationError", "PermissionDeniedError",
	"NotFoundError", "ConflictError", "UnprocessableEntityError", "RateLimitError",
	"InternalServerError", "NewAPIStatusError", "WebhookVerificationError",
	"WebhookVerificationReason", "TokenVerificationError", "TokenVerificationReason",
	"UnexpectedResponseError",
}

func newGen(spec *Spec) *Gen {
	g := &Gen{
		spec:     spec,
		byName:   map[string]*Decl{},
		comps:    map[string]*GoType{},
		compUse:  map[string]uint8{},
		reserved: map[string]string{},

		paramPrefixes: map[string]string{},
	}
	for _, n := range reservedNames {
		g.reserved[n] = "hand-written core"
	}
	return g
}

// addDecl registers a declaration; a name used twice is a generator error.
func (g *Gen) addDecl(d *Decl) (*Decl, error) {
	if origin, ok := g.reserved[d.Name]; ok {
		return nil, fmt.Errorf("type name %s (%s) collides with %s", d.Name, d.Origin, origin)
	}
	if prev, ok := g.byName[d.Name]; ok {
		return nil, fmt.Errorf("type name %s is used by %s and %s", d.Name, prev.Origin, d.Origin)
	}
	d.Owners = map[string]bool{}
	d.order = len(g.decls)
	g.byName[d.Name] = d
	g.decls = append(g.decls, d)
	return d, nil
}

// ---- component usage -------------------------------------------------------

// markUsage records which components requests and responses reach.
func (g *Gen) markUsage() {
	for _, op := range g.spec.ServerOps() {
		if rb := op.Doc.Resolve(op.Node.Get("requestBody")); rb != nil {
			content := rb.Get("content")
			for _, ct := range content.ObjKeys() {
				g.walkUsage(op.Doc, content.Get(ct).Get("schema"), useRequest, map[string]bool{})
			}
		}
		for _, p := range op.Parameters() {
			g.walkUsage(op.Doc, p.Get("schema"), useRequest, map[string]bool{})
		}
		responses := op.Node.Get("responses")
		for _, code := range responses.ObjKeys() {
			if !strings.HasPrefix(code, "2") {
				continue
			}
			r := op.Doc.Resolve(responses.Get(code))
			content := r.Get("content")
			for _, ct := range content.ObjKeys() {
				g.walkUsage(op.Doc, content.Get(ct).Get("schema"), useResponse, map[string]bool{})
			}
		}
	}
}

func (g *Gen) walkUsage(doc *Doc, s *Node, use uint8, seen map[string]bool) {
	if s == nil {
		return
	}
	if ref := s.Get("$ref").Text(); ref != "" {
		if seen[ref] {
			return
		}
		seen[ref] = true
		g.compUse[doc.Name+"|"+ref] |= use
		g.walkUsage(doc, doc.Lookup(ref), use, seen)
		return
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		for _, part := range s.Get(key).Elems() {
			g.walkUsage(doc, part, use, seen)
		}
	}
	if props := s.Get("properties"); props != nil {
		for _, k := range props.Keys {
			g.walkUsage(doc, props.Get(k), use, seen)
		}
	}
	g.walkUsage(doc, s.Get("items"), use, seen)
	if ap := s.Get("additionalProperties"); ap != nil && ap.Kind == Object {
		g.walkUsage(doc, ap, use, seen)
	}
}

// ---- schema classification -------------------------------------------------

// view is a schema with $ref and single-part allOf resolved.
func view(doc *Doc, s *Node) *Node {
	for i := 0; s != nil && i < 32; i++ {
		if s.Has("$ref") {
			s = doc.Resolve(s)
			continue
		}
		if all := s.Get("allOf"); all != nil && len(all.Arr) == 1 && !s.Has("properties") {
			s = all.Arr[0]
			continue
		}
		break
	}
	return s
}

// isObjectLike reports whether s describes a JSON object with properties.
func isObjectLike(doc *Doc, s *Node) bool {
	s = view(doc, s)
	if s == nil {
		return false
	}
	if s.Has("properties") {
		return true
	}
	if all := s.Get("allOf"); all != nil {
		for _, p := range all.Arr {
			if !isObjectLike(doc, p) {
				return false
			}
		}
		return len(all.Arr) > 0
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if parts := s.Get(key); parts != nil && len(parts.Arr) > 0 {
			for _, p := range parts.Arr {
				if !isObjectLike(doc, p) {
					return false
				}
			}
			return true
		}
	}
	return false
}

func schemaType(doc *Doc, s *Node) string {
	s = view(doc, s)
	if s == nil {
		return ""
	}
	if t := s.Get("type").Text(); t != "" {
		return t
	}
	if s.Has("properties") || isObjectLike(doc, s) {
		return "object"
	}
	if s.Has("items") {
		return "array"
	}
	if s.Has("enum") {
		allStrings := true
		for _, v := range s.Get("enum").Elems() {
			if v.Kind != String && v.Kind != Null {
				allStrings = false
			}
		}
		if allStrings {
			return "string"
		}
	}
	return ""
}

// ---- type resolution ---------------------------------------------------------

// typeOf returns the Go type of schema s. name is the type name to use if s
// needs a new named type.
func (g *Gen) typeOf(doc *Doc, s *Node, name string, side Side) (*GoType, error) {
	if s == nil {
		return &GoType{Kind: TAny}, nil
	}
	if ref := s.Get("$ref").Text(); ref != "" {
		if !strings.HasPrefix(ref, "#/components/schemas/") {
			// A reference to an inline schema elsewhere: treat it as inline.
			return g.typeOf(doc, doc.Lookup(ref), name, side)
		}
		return g.component(doc, ref, side)
	}
	if all := s.Get("allOf"); all != nil && len(all.Arr) > 0 {
		if len(all.Arr) == 1 && !s.Has("properties") {
			return g.typeOf(doc, all.Arr[0], name, side)
		}
		parts := append([]*Node(nil), all.Arr...)
		if s.Has("properties") {
			parts = append(parts, s)
		}
		if allObjects(doc, parts) {
			return g.structType(doc, name, side, parts, false, s.Get("description").Text())
		}
		return &GoType{Kind: TAny}, nil
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if parts := s.Get(key); parts != nil && len(parts.Arr) > 0 {
			return g.unionType(doc, s, parts.Arr, name, side)
		}
	}
	if enum := s.Get("enum"); enum != nil && len(enum.Arr) > 0 {
		if schemaType(doc, s) == "string" {
			return g.enumType(g.enumName(name), side, s.Get("description").Text(), [][]*Node{enum.Arr})
		}
		return &GoType{Kind: TAny}, nil
	}
	switch schemaType(doc, s) {
	case "string":
		switch s.Get("format").Text() {
		case "date-time":
			return &GoType{Kind: TTime}, nil
		case "binary":
			if side == Request {
				return &GoType{Kind: TFile}, nil
			}
		}
		return &GoType{Kind: TString}, nil
	case "integer":
		return &GoType{Kind: TInt}, nil
	case "number":
		return &GoType{Kind: TFloat}, nil
	case "boolean":
		return &GoType{Kind: TBool}, nil
	case "array":
		elem, err := g.typeOf(doc, s.Get("items"), name, side)
		if err != nil {
			return nil, err
		}
		return &GoType{Kind: TSlice, Elem: elem}, nil
	case "object":
		if props := s.Get("properties"); props != nil && len(props.Keys) > 0 {
			return g.structType(doc, name, side, []*Node{s}, false, s.Get("description").Text())
		}
		if ap := s.Get("additionalProperties"); ap != nil && ap.Kind == Object && len(ap.Keys) > 0 {
			elem, err := g.typeOf(doc, ap, name, side)
			if err != nil {
				return nil, err
			}
			return &GoType{Kind: TMap, Elem: elem}, nil
		}
		return &GoType{Kind: TMap, Elem: &GoType{Kind: TAny}}, nil
	}
	return &GoType{Kind: TAny}, nil
}

func allObjects(doc *Doc, parts []*Node) bool {
	for _, p := range parts {
		if !isObjectLike(doc, p) {
			return false
		}
	}
	return len(parts) > 0
}

// component returns the type of a $ref component.
func (g *Gen) component(doc *Doc, ref string, side Side) (*GoType, error) {
	compName := ref[strings.LastIndex(ref, "/")+1:]
	s := doc.Lookup(ref)
	if s == nil {
		return nil, fmt.Errorf("unresolved reference %s", ref)
	}
	structLike := isObjectLike(doc, s)
	key := doc.Name + "|" + ref
	if structLike {
		key += fmt.Sprintf("|%d", side)
	}
	if t, ok := g.comps[key]; ok {
		return t, nil
	}
	goName := naming.Pascal(compName)
	if structLike && side == Request && g.compUse[doc.Name+"|"+ref]&useResponse != 0 {
		g.paramPrefixes[goName+"Param"] = goName
		goName += "Param"
	}
	placeholder := &GoType{Kind: TNamed, Name: goName}
	g.comps[key] = placeholder
	t, err := g.typeOf(doc, s, goName, side)
	if err != nil {
		return nil, err
	}
	if t.Kind == TNamed && t.Name == goName {
		placeholder.Decl = t.Decl
		return placeholder, nil
	}
	// A component that is a primitive, list or map: use that type directly.
	g.comps[key] = t
	return t, nil
}

type propInfo struct {
	name     string
	schemas  []*Node
	required int
}

// structType declares a struct from one object schema, or from several:
// allOf parts (union=false: a field is required when any part requires it)
// or oneOf/anyOf variants (union=true: required only when every variant
// requires it).
func (g *Gen) structType(doc *Doc, name string, side Side, parts []*Node, union bool, doc0 string) (*GoType, error) {
	t := &GoType{Kind: TNamed, Name: name}
	d := &Decl{Name: name, Kind: DStruct, Side: side, Doc: doc0, Origin: name}
	if _, err := g.addDecl(d); err != nil {
		return nil, err
	}
	t.Decl = d
	props, order, nparts := g.collectProps(doc, parts)
	used := map[string]bool{"JSON": side == Response}
	for _, pn := range order {
		p := props[pn]
		first := view(doc, p.schemas[0])
		if side == Request && first.Get("readOnly").Truthy() {
			continue
		}
		if side == Response && first.Get("writeOnly").Truthy() {
			continue
		}
		var ft *GoType
		var err error
		childName := name + fieldName(pn)
		if len(p.schemas) == 1 || allSame(doc, p.schemas) {
			ft, err = g.typeOf(doc, p.schemas[0], childName, side)
		} else {
			ft, err = g.mergedType(doc, p.schemas, childName, side)
		}
		if err != nil {
			return nil, err
		}
		required := p.required > 0
		if union {
			required = p.required == nparts
		}
		nullable := p.schemas[0].Get("nullable").Truthy() || first.Get("nullable").Truthy()
		f := &Field{
			GoName:   uniqueName(fieldName(pn), used),
			Name:     pn,
			Type:     ft,
			Doc:      propDoc(p.schemas[0]),
			Tag:      "json",
			Required: required,
		}
		if side == Request {
			f.Opt = !required || nullable
			if ft.Kind == TFile {
				f.Tag = "form"
			}
		}
		d.Fields = append(d.Fields, f)
	}
	return t, nil
}

// collectProps gathers the properties of object schemas, following $ref and
// allOf.
func (g *Gen) collectProps(doc *Doc, parts []*Node) (map[string]*propInfo, []string, int) {
	props := map[string]*propInfo{}
	var order []string
	var visit func(s *Node, requiredBy map[string]bool) map[string]bool
	visit = func(s *Node, requiredBy map[string]bool) map[string]bool {
		s = view(doc, s)
		if s == nil {
			return requiredBy
		}
		for _, r := range s.Get("required").Strings() {
			requiredBy[r] = true
		}
		if all := s.Get("allOf"); all != nil {
			for _, p := range all.Arr {
				requiredBy = visit(p, requiredBy)
			}
		}
		if ps := s.Get("properties"); ps != nil {
			for _, k := range ps.Keys {
				p, ok := props[k]
				if !ok {
					p = &propInfo{name: k}
					props[k] = p
					order = append(order, k)
				}
				p.schemas = append(p.schemas, ps.Get(k))
			}
		}
		return requiredBy
	}
	for _, part := range parts {
		req := visit(part, map[string]bool{})
		for k := range req {
			if p, ok := props[k]; ok {
				p.required++
			}
		}
	}
	return props, order, len(parts)
}

func allSame(doc *Doc, nodes []*Node) bool {
	resolve := func(n *Node) *Node {
		if ref := n.Get("$ref").Text(); ref != "" && !strings.HasPrefix(ref, "#/components/schemas/") {
			return doc.Lookup(ref)
		}
		return n
	}
	first := resolve(nodes[0]).SortedJSON()
	for _, n := range nodes[1:] {
		if resolve(n).SortedJSON() != first {
			return false
		}
	}
	return true
}

// mergedType combines the schemas a property has in several variants.
func (g *Gen) mergedType(doc *Doc, nodes []*Node, name string, side Side) (*GoType, error) {
	if allSame(doc, nodes) {
		return g.typeOf(doc, nodes[0], name, side)
	}
	if allObjects(doc, nodes) {
		return g.structType(doc, name, side, nodes, true, "")
	}
	kinds := map[string]bool{}
	var enums [][]*Node
	allEnum := true
	allDateTime := true
	for _, n := range nodes {
		v := view(doc, n)
		kinds[schemaType(doc, v)] = true
		if e := v.Get("enum"); e != nil {
			enums = append(enums, e.Arr)
		} else {
			allEnum = false
		}
		if v.Get("format").Text() != "date-time" {
			allDateTime = false
		}
	}
	if len(kinds) == 1 {
		for k := range kinds {
			switch k {
			case "string":
				if allEnum {
					return g.enumType(g.enumName(name), side, "", enums)
				}
				if allDateTime {
					return &GoType{Kind: TTime}, nil
				}
				return &GoType{Kind: TString}, nil
			case "integer":
				return &GoType{Kind: TInt}, nil
			case "number":
				return &GoType{Kind: TFloat}, nil
			case "boolean":
				return &GoType{Kind: TBool}, nil
			case "array":
				var items []*Node
				for _, n := range nodes {
					items = append(items, view(doc, n).Get("items"))
				}
				elem, err := g.mergedType(doc, items, name, side)
				if err != nil {
					return nil, err
				}
				return &GoType{Kind: TSlice, Elem: elem}, nil
			}
		}
	}
	if len(kinds) == 2 && kinds["integer"] && kinds["number"] {
		return &GoType{Kind: TFloat}, nil
	}
	return &GoType{Kind: TAny}, nil
}

// unionType handles oneOf/anyOf.
func (g *Gen) unionType(doc *Doc, s *Node, parts []*Node, name string, side Side) (*GoType, error) {
	var nonNull []*Node
	for _, p := range parts {
		if view(doc, p).Get("type").Text() == "null" {
			continue
		}
		nonNull = append(nonNull, p)
	}
	if len(nonNull) == 1 {
		return g.typeOf(doc, nonNull[0], name, side)
	}
	if allObjects(doc, nonNull) {
		return g.structType(doc, name, side, nonNull, true, s.Get("description").Text())
	}
	return g.mergedType(doc, nonNull, name, side)
}

// enumType declares a string enum. Several value lists are merged.
func (g *Gen) enumType(name string, side Side, doc0 string, lists [][]*Node) (*GoType, error) {
	if prev, ok := g.byName[name]; ok && prev.Kind == DEnum {
		// The same inline enum reached again (for example a component used on
		// both sides): reuse it when the values match.
		if sameEnumValues(prev, lists) {
			return &GoType{Kind: TNamed, Name: name, Decl: prev}, nil
		}
	}
	d := &Decl{Name: name, Kind: DEnum, Side: side, Doc: doc0, Origin: name}
	if _, err := g.addDecl(d); err != nil {
		return nil, err
	}
	used := map[string]bool{}
	seen := map[string]bool{}
	for _, list := range lists {
		for _, v := range list {
			if v.Kind != String || seen[v.Str] {
				continue
			}
			seen[v.Str] = true
			suffix := naming.Pascal(v.Str)
			if suffix == "" {
				suffix = "Empty"
			}
			d.Values = append(d.Values, EnumValue{GoName: uniqueName(name+suffix, used), Value: v.Str})
		}
	}
	return &GoType{Kind: TNamed, Name: name, Decl: d}, nil
}

func sameEnumValues(d *Decl, lists [][]*Node) bool {
	want := map[string]bool{}
	for _, list := range lists {
		for _, v := range list {
			if v.Kind == String {
				want[v.Str] = true
			}
		}
	}
	if len(want) != len(d.Values) {
		return false
	}
	for _, v := range d.Values {
		if !want[v.Value] {
			return false
		}
	}
	return true
}

// enumName returns the name of an enum declared under name: enums of the
// request-side copy of a component take the response-side name.
func (g *Gen) enumName(name string) string {
	best := ""
	for prefix := range g.paramPrefixes {
		rest := strings.TrimPrefix(name, prefix)
		boundary := rest == "" || (rest[0] >= 'A' && rest[0] <= 'Z')
		if strings.HasPrefix(name, prefix) && boundary && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return name
	}
	return g.paramPrefixes[best] + strings.TrimPrefix(name, best)
}

// fieldName converts a property name to a Go field name.
func fieldName(prop string) string {
	n := naming.Pascal(prop)
	if n == "" {
		return "Field"
	}
	if n[0] >= '0' && n[0] <= '9' {
		return "Field" + n
	}
	return n
}

// uniqueName returns name, or name followed by a number when it is taken,
// and marks it as taken.
func uniqueName(name string, used map[string]bool) string {
	candidate := name
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s%d", name, i)
	}
	used[candidate] = true
	return candidate
}

func propDoc(s *Node) string {
	if d := s.Get("description").Text(); d != "" {
		return d
	}
	return ""
}

// sortedDecls returns the declarations in creation order.
func (g *Gen) sortedDecls() []*Decl {
	out := append([]*Decl(nil), g.decls...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out
}
