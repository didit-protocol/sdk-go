package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/didit-protocol/sdk-go/internal/naming"
)

// Service is a resource: a struct with one method per operation and one
// field per sub-resource.
type Service struct {
	Path     string // "sessions.imports"
	Segment  string // "imports"
	TypeName string // "SessionsImportsService"
	Field    string // "Imports"
	Children []*Service
	Methods  []*Method
	Top      string // top-level resource segment, for file placement
}

// ReturnKind is what a method returns.
type ReturnKind uint8

// Return kinds.
const (
	RetNone ReturnKind = iota
	RetJSON
	RetBinary
	RetPage
)

// PathArg is a path parameter passed as a method argument.
type PathArg struct {
	Name string // template name
	Arg  string // Go argument name
	Doc  string
}

// Method is one generated method.
type Method struct {
	Op       *Operation
	Service  *Service
	GoName   string
	PathArgs []PathArg
	// Params is the parameter type, nil when the method takes none.
	Params   *GoType
	BodyKind string
	Return   ReturnKind
	// Result is the JSON result type (RetJSON) or the item type (RetPage).
	Result *GoType
	Accept string
	// Examples used by the generated test.
	RequestExample  *Node
	RequestSchema   *Node
	ResponseExample *Node
	ResponseSchema  *Node
	ResponseStatus  string
	BinaryType      string
}

var reservedArgs = map[string]bool{
	"ctx": true, "params": true, "opts": true, "r": true, "res": true, "err": true,
	"break": true, "case": true, "chan": true, "const": true, "continue": true, "default": true,
	"defer": true, "else": true, "fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true, "map": true, "package": true,
	"range": true, "return": true, "select": true, "struct": true, "switch": true, "type": true,
	"var": true, "url": true, "http": true, "time": true, "context": true, "option": true,
}

// buildServices builds the resource tree and resolves every method's types.
func (g *Gen) buildServices() (*Service, error) {
	root := &Service{TypeName: "Client"}
	byPath := map[string]*Service{"": root}
	var get func(path string) *Service
	get = func(path string) *Service {
		if s, ok := byPath[path]; ok {
			return s
		}
		parent := ""
		seg := path
		if i := strings.LastIndex(path, "."); i >= 0 {
			parent, seg = path[:i], path[i+1:]
		}
		p := get(parent)
		top := strings.SplitN(path, ".", 2)[0]
		s := &Service{
			Path:     path,
			Segment:  seg,
			TypeName: naming.Resource(path) + "Service",
			Field:    naming.Pascal(seg),
			Top:      top,
		}
		p.Children = append(p.Children, s)
		byPath[path] = s
		return s
	}
	var problems []string
	for _, op := range g.spec.ServerOps() {
		s := get(op.Naming.Resource)
		m, err := g.buildMethod(op, s)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", op.Key, err))
			continue
		}
		s.Methods = append(s.Methods, m)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("generating operations:\n  %s", strings.Join(problems, "\n  "))
	}
	var check func(s *Service) error
	check = func(s *Service) error {
		names := map[string]string{}
		for _, c := range s.Children {
			names[c.Field] = "sub-resource " + c.Path
		}
		for _, m := range s.Methods {
			for _, n := range []string{m.GoName, m.GoName + "AutoPaging"} {
				if prev, ok := names[n]; ok && (n == m.GoName || m.Return == RetPage) {
					return fmt.Errorf("%s.%s: %s collides with %s", s.TypeName, n, m.Op.Key, prev)
				}
			}
			names[m.GoName] = m.Op.Key
			if m.Return == RetPage {
				names[m.GoName+"AutoPaging"] = m.Op.Key
			}
		}
		sort.Slice(s.Children, func(i, j int) bool { return s.Children[i].Path < s.Children[j].Path })
		for _, c := range s.Children {
			if err := check(c); err != nil {
				return err
			}
		}
		return nil
	}
	return root, check(root)
}

func (g *Gen) buildMethod(op *Operation, s *Service) (*Method, error) {
	m := &Method{Op: op, Service: s, GoName: naming.Pascal(op.Naming.Method)}
	prefix := naming.Resource(op.Naming.Resource) + m.GoName

	// Path parameters, in template order.
	params := op.Parameters()
	byName := map[string]*Node{}
	for _, p := range params {
		if p.Get("in").Text() == "path" {
			byName[p.Get("name").Text()] = p
		}
	}
	usedArgs := map[string]bool{}
	for _, name := range templateParams(op.Path) {
		if op.Naming.Scope == "application" && (name == "organization_id" || name == "application_id") {
			continue
		}
		arg := naming.Camel(name)
		if arg == "" || reservedArgs[arg] {
			arg += "Param"
		}
		arg = uniqueName(arg, usedArgs)
		m.PathArgs = append(m.PathArgs, PathArg{Name: name, Arg: arg, Doc: byName[name].Get("description").Text()})
	}
	if op.Naming.Scope == "application" && !strings.HasPrefix(op.Path, "/v3/organization/{organization_id}/application/{application_id}/") {
		return nil, fmt.Errorf("scope application but the path does not start with /v3/organization/{organization_id}/application/{application_id}/")
	}

	// Parameters struct.
	if err := g.buildParams(op, m, prefix, params); err != nil {
		return nil, err
	}

	// Result.
	if err := g.buildResult(op, m, prefix); err != nil {
		return nil, err
	}
	return m, nil
}

// templateParams returns the {names} of a path template in order.
func templateParams(path string) []string {
	var out []string
	for {
		i := strings.IndexByte(path, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(path[i:], '}')
		if j < 0 {
			return out
		}
		out = append(out, path[i+1:i+j])
		path = path[i+j+1:]
	}
}

func (g *Gen) buildParams(op *Operation, m *Method, prefix string, params []*Node) error {
	var query, header []*Node
	for _, p := range params {
		switch p.Get("in").Text() {
		case "query":
			query = append(query, p)
		case "header":
			switch strings.ToLower(p.Get("name").Text()) {
			case "content-type", "accept", "x-api-key":
				continue
			}
			header = append(header, p)
		}
	}
	var jsonSchema, formSchema *Node
	if rb := op.Doc.Resolve(op.Node.Get("requestBody")); rb != nil {
		content := rb.Get("content")
		jsonSchema = content.Path("application/json", "schema")
		formSchema = content.Path("multipart/form-data", "schema")
		if jsonSchema == nil && formSchema == nil && len(content.ObjKeys()) > 0 {
			return fmt.Errorf("unsupported request content types %v", content.ObjKeys())
		}
		// DRF lists the same serializer for JSON, form and multipart: JSON.
		if jsonSchema != nil && formSchema != nil && jsonSchema.SortedJSON() == formSchema.SortedJSON() {
			formSchema = nil
		}
	}
	name := prefix + "Params"
	switch {
	case jsonSchema != nil && formSchema != nil:
		m.BodyKind = "bodyJSONOrMultipart"
	case formSchema != nil:
		m.BodyKind = "bodyMultipart"
	case jsonSchema != nil:
		m.BodyKind = "bodyJSON"
	default:
		m.BodyKind = "bodyNone"
	}
	body := jsonSchema
	if body == nil {
		body = formSchema
	}
	m.RequestSchema = body

	// A body that is a component, and nothing else: take the component.
	if body != nil && formSchema == nil && len(query) == 0 && len(header) == 0 {
		if ref := componentRef(body); ref != "" && isObjectLike(op.Doc, body) {
			t, err := g.component(op.Doc, ref, Request)
			if err != nil {
				return err
			}
			m.Params = t
			return nil
		}
	}
	if body == nil && len(query) == 0 && len(header) == 0 {
		return nil
	}
	var d *Decl
	used := map[string]bool{}
	bodyRef := ""
	if body != nil && formSchema == nil && isObjectLike(op.Doc, body) {
		bodyRef = componentRef(body)
	}
	if body != nil && bodyRef == "" {
		// An inline body: its properties become fields of the params struct.
		parts := []*Node{}
		if jsonSchema != nil {
			parts = append(parts, jsonSchema)
		}
		if formSchema != nil {
			parts = append(parts, formSchema)
		}
		if !allObjects(op.Doc, parts) {
			return fmt.Errorf("request body is not an object")
		}
		// A field required by only one of JSON and multipart is optional.
		bt, err := g.structType(op.Doc, name, Request, parts, len(parts) > 1, "")
		if err != nil {
			return err
		}
		d = bt.Decl
		for _, f := range d.Fields {
			used[f.GoName] = true
		}
	} else {
		d = &Decl{Name: name, Kind: DStruct, Side: Request}
		if _, err := g.addDecl(d); err != nil {
			return err
		}
		if bodyRef != "" {
			t, err := g.component(op.Doc, bodyRef, Request)
			if err != nil {
				return err
			}
			d.Fields = append(d.Fields, &Field{GoName: uniqueName("Body", used), Name: "json", Type: t, Tag: "body", Required: true, Doc: "Body is the JSON request body."})
		}
	}
	d.Params = true
	d.Origin = op.Key
	d.Doc = fmt.Sprintf("%s holds the parameters of %s.", name, methodRef(m))
	for _, p := range query {
		f, err := g.paramField(op, p, name, used, "query")
		if err != nil {
			return err
		}
		d.Fields = append(d.Fields, f)
	}
	for _, p := range header {
		f, err := g.paramField(op, p, name, used, "header")
		if err != nil {
			return err
		}
		d.Fields = append(d.Fields, f)
	}
	m.Params = &GoType{Kind: TNamed, Name: name, Decl: d}
	return nil
}

func (g *Gen) paramField(op *Operation, p *Node, structName string, used map[string]bool, tag string) (*Field, error) {
	pname := p.Get("name").Text()
	goName := fieldName(pname)
	if used[goName] {
		goName = uniqueName(goName+naming.Pascal(tag), used)
	} else {
		used[goName] = true
	}
	t, err := g.typeOf(op.Doc, p.Get("schema"), structName+goName, Request)
	if err != nil {
		return nil, err
	}
	required := p.Get("required").Truthy()
	doc := p.Get("description").Text()
	return &Field{GoName: goName, Name: pname, Type: t, Opt: !required, Tag: tag, Required: required, Doc: doc}, nil
}

func componentRef(s *Node) string {
	if ref := s.Get("$ref").Text(); ref != "" {
		return ref
	}
	if all := s.Get("allOf"); all != nil && len(all.Arr) == 1 && !s.Has("properties") {
		return all.Arr[0].Get("$ref").Text()
	}
	return ""
}

func methodRef(m *Method) string {
	parts := []string{"Client"}
	for _, seg := range strings.Split(m.Op.Naming.Resource, ".") {
		parts = append(parts, naming.Pascal(seg))
	}
	parts = append(parts, m.GoName)
	return strings.Join(parts, ".")
}

func (g *Gen) buildResult(op *Operation, m *Method, prefix string) error {
	responses := op.Node.Get("responses")
	var jsonSchemas []*Node
	var jsonExamples []*Node
	var statuses []string
	for _, code := range responses.ObjKeys() {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		r := op.Doc.Resolve(responses.Get(code))
		content := r.Get("content")
		if content == nil || len(content.ObjKeys()) == 0 {
			continue
		}
		if media := content.Get("application/json"); media != nil {
			statuses = append(statuses, code)
			if media.Has("schema") {
				jsonSchemas = append(jsonSchemas, media.Get("schema"))
			}
			jsonExamples = append(jsonExamples, mediaExample(media))
			continue
		}
		m.Return = RetBinary
		m.BinaryType = content.ObjKeys()[0]
		m.Accept = strings.Join(content.ObjKeys(), ", ")
		m.ResponseStatus = code
		return nil
	}
	if len(statuses) == 0 {
		m.Return = RetNone
		for _, code := range responses.ObjKeys() {
			if strings.HasPrefix(code, "2") {
				m.ResponseStatus = code
				break
			}
		}
		return nil
	}
	m.ResponseStatus = statuses[0]
	m.ResponseExample = jsonExamples[0]
	if len(jsonSchemas) == 0 {
		// A JSON response without a schema: return the decoded object.
		m.Return = RetJSON
		m.Result = &GoType{Kind: TMap, Elem: &GoType{Kind: TAny}}
		return nil
	}
	m.ResponseSchema = jsonSchemas[0]
	// Paginated list?
	if page := g.pageItems(op, jsonSchemas); page != nil {
		item, err := g.typeOf(op.Doc, page, listItemName(prefix), Response)
		if err != nil {
			return err
		}
		m.Return = RetPage
		m.Result = item
		return nil
	}
	name := prefix + "Response"
	var t *GoType
	var err error
	if len(jsonSchemas) == 1 || allSame(op.Doc, jsonSchemas) {
		t, err = g.typeOf(op.Doc, jsonSchemas[0], name, Response)
	} else if allObjects(op.Doc, jsonSchemas) {
		t, err = g.structType(op.Doc, name, Response, jsonSchemas, true, "")
	} else {
		t, err = g.mergedType(op.Doc, jsonSchemas, name, Response)
	}
	if err != nil {
		return err
	}
	m.Return = RetJSON
	m.Result = t
	return nil
}

// listItemName names the item type of a paginated list.
func listItemName(prefix string) string { return prefix + "ResponseResults" }

// pageItems returns the item schema when the operation is a paginated list:
// a GET with limit and offset query parameters returning
// {count, next, previous, results: [...]}.
func (g *Gen) pageItems(op *Operation, schemas []*Node) *Node {
	if op.HTTPMethod != "GET" || len(schemas) != 1 {
		return nil
	}
	hasLimit, hasOffset := false, false
	for _, p := range op.Parameters() {
		if p.Get("in").Text() != "query" {
			continue
		}
		switch p.Get("name").Text() {
		case "limit":
			hasLimit = true
		case "offset":
			hasOffset = true
		}
	}
	if !hasLimit || !hasOffset {
		return nil
	}
	s := view(op.Doc, schemas[0])
	props := s.Get("properties")
	for _, k := range []string{"count", "next", "previous", "results"} {
		if !props.Has(k) {
			return nil
		}
	}
	results := view(op.Doc, props.Get("results"))
	if schemaType(op.Doc, results) != "array" {
		return nil
	}
	return results.Get("items")
}

// mediaExample returns the example of a media type object.
func mediaExample(media *Node) *Node {
	if ex := media.Get("example"); ex != nil {
		return ex
	}
	if exs := media.Get("examples"); exs != nil && len(exs.Keys) > 0 {
		return exs.Get(exs.Keys[0]).Get("value")
	}
	return nil
}
