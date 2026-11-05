package main

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"

	"github.com/didit-protocol/sdk-go/internal/naming"
)

// emitTests renders one test per operation, grouped by top-level resource.
// Each test calls the method with parameters built from the spec's example
// (or a synthesised one) against a mocked transport, and checks the HTTP
// method, path, query, headers, body encoding and response decoding.
func (g *Gen) emitTests(root *Service) ([]File, error) {
	bodies := map[string]*bytes.Buffer{}
	var walk func(s *Service) error
	walk = func(s *Service) error {
		for _, m := range s.Methods {
			b, ok := bodies[s.Top]
			if !ok {
				b = &bytes.Buffer{}
				bodies[s.Top] = b
			}
			if err := g.emitTest(b, m); err != nil {
				return fmt.Errorf("%s: %w", m.Op.Key, err)
			}
		}
		for _, c := range s.Children {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	var names []string
	for n := range bodies {
		names = append(names, n)
	}
	sort.Strings(names)
	var files []File
	for _, n := range names {
		body := bodies[n].String()
		var imports []string
		for _, imp := range []struct{ pkg, use string }{
			{"context", "context."},
			{"io", "io."},
			{"testing", "testing."},
			{"time", "time."},
		} {
			if strings.Contains(body, imp.use) {
				imports = append(imports, strconv.Quote(imp.pkg))
			}
		}
		var src bytes.Buffer
		src.WriteString(header)
		src.WriteString("package didit_test\n\nimport (\n")
		for _, imp := range imports {
			src.WriteString("\t" + imp + "\n")
		}
		if strings.Contains(body, "didit.") {
			src.WriteString("\n\t\"github.com/didit-protocol/sdk-go\"\n")
		}
		src.WriteString(")\n\n")
		src.WriteString(body)
		formatted, err := format.Source(src.Bytes())
		if err != nil {
			return nil, fmt.Errorf("formatting %s tests: %w\n%s", n, err, numbered(src.Bytes()))
		}
		files = append(files, File{Name: n + "_gen_test.go", Content: formatted})
	}
	return files, nil
}

func goString(s string) string {
	if !strings.Contains(s, "`") && !strings.ContainsAny(s, "\r") && strconv.CanBackquote(s) {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

func (g *Gen) emitTest(b *bytes.Buffer, m *Method) error {
	if m.ParamsMethod == "" {
		return g.emitTestAs(b, m, m.GoName, m.Params)
	}
	// openapi/compatibility.json: the method without parameters, then the
	// companion method with them.
	if err := g.emitTestAs(b, m, m.GoName, nil); err != nil {
		return err
	}
	return g.emitTestAs(b, m, m.ParamsMethod, m.Params)
}

// emitTestAs renders the test of method name of m, called with params when
// not nil.
func (g *Gen) emitTestAs(b *bytes.Buffer, m *Method, name string, params *GoType) error {
	op := m.Op
	testName := "Test" + naming.Resource(op.Naming.Resource) + name
	reqB := &exampleBuilder{doc: op.Doc, side: Request}
	respB := &exampleBuilder{doc: op.Doc, side: Response}

	// Response.
	status := m.ResponseStatus
	if status == "" || status == "default" {
		status = "200"
	}
	if _, err := strconv.Atoi(status); err != nil {
		status = "200"
	}
	contentType := "application/json"
	var respBody string
	var checkKeys []string
	switch m.Return {
	case RetNone:
		respBody = ""
		contentType = ""
	case RetBinary:
		contentType = m.BinaryType
		respBody = "binary-content"
	case RetJSON, RetPage:
		ex := sanitizeExample(m.ResponseExample)
		var fitted *Node
		if ex != nil && m.ResponseSchema != nil {
			fitted = respB.fit(ex, m.ResponseSchema, 0)
		}
		if ex != nil && (fitted != nil || m.ResponseSchema == nil) {
			respBody = ex.JSON()
		} else if m.ResponseSchema != nil {
			fitted = sanitizeExample(respB.synth(m.ResponseSchema, 0, map[string]bool{}))
			respBody = fitted.JSON()
		} else if m.Result != nil && m.Result.Kind == TRaw {
			// Raw JSON may be any value: an array is what a decoded object
			// would lose.
			respBody = `[{"ok":true}]`
		} else {
			respBody = `{"ok":true}`
		}
		if m.Return == RetPage && fitted != nil {
			fitted = fitted.Get("results")
			if fitted != nil && len(fitted.Arr) > 0 {
				fitted = fitted.Arr[0]
			} else {
				fitted = nil
			}
		}
		if fitted != nil && fitted.Kind == Object && m.Result != nil && m.Result.Kind == TNamed && g.byName[m.Result.Name] != nil && g.byName[m.Result.Name].Kind == DStruct {
			known := map[string]bool{}
			for _, f := range g.byName[m.Result.Name].Fields {
				known[f.Name] = true
			}
			for _, k := range fitted.Keys {
				if known[k] && fitted.Get(k).Kind != Null {
					checkKeys = append(checkKeys, k)
				}
			}
		}
	}

	fmt.Fprintf(b, "func %s(t *testing.T) {\n", testName)
	fmt.Fprintf(b, "\tclient, mock := newMockClient(t, %s, %s, %s)\n", status, strconv.Quote(contentType), goString(respBody))

	// Arguments.
	args := []string{"context.Background()"}
	wantPath := op.Path
	for _, a := range m.PathArgs {
		value := strings.ReplaceAll(a.Name, "_", "-") + "-1"
		args = append(args, strconv.Quote(value))
		wantPath = strings.Replace(wantPath, "{"+a.Name+"}", value, 1)
	}
	if op.Naming.Scope == "application" {
		wantPath = strings.Replace(wantPath, "{organization_id}", "org-test", 1)
		wantPath = strings.Replace(wantPath, "{application_id}", "app-test", 1)
	}
	if op.Doc.Name == "account" {
		wantPath = "/auth/v2" + wantPath
	}

	wantQuery := map[string]string{}
	wantHeaders := map[string]string{}
	var wantBody string
	var wantForm []string
	var wantFiles []string
	multipart := params != nil && m.BodyKind == "bodyMultipart"
	if params != nil {
		d := g.byName[params.Name]
		if d == nil {
			return fmt.Errorf("params type %s not declared", params.Name)
		}
		fmt.Fprintf(b, "\tvar params didit.%s\n", params.Name)
		// Body from the example.
		if m.RequestSchema != nil {
			var bodyField *Field
			for _, f := range d.Fields {
				if f.Tag == "body" {
					bodyField = f
				}
			}
			ex := sanitizeExample(reqB.value(sanitizeExample(requestExample(op)), m.RequestSchema))
			if ex == nil {
				ex = &Node{Kind: Object}
			}
			target := "&params"
			if bodyField != nil {
				target = "&params." + bodyField.GoName
			}
			fmt.Fprintf(b, "\tmustUnmarshal(t, %s, %s)\n", goString(ex.JSON()), target)
			if multipart {
				for _, k := range ex.Keys {
					if v := ex.Get(k); v.Kind != Null {
						wantForm = append(wantForm, k)
					}
				}
			} else {
				wantBody = ex.JSON()
			}
			// Files.
			for _, f := range d.Fields {
				if f.Type.Kind != TFile {
					continue
				}
				if !f.Required && !multipart {
					continue
				}
				if !f.Required && multipart {
					// Optional files: send one too, to cover Opt[File].
					fmt.Fprintf(b, "\tparams.%s = didit.Some(didit.FileFromBytes(%s, []byte(\"file-content\")))\n", f.GoName, strconv.Quote(f.Name+".png"))
				} else {
					fmt.Fprintf(b, "\tparams.%s = didit.FileFromBytes(%s, []byte(\"file-content\"))\n", f.GoName, strconv.Quote(f.Name+".png"))
				}
				wantFiles = append(wantFiles, f.Name)
			}
		}
		for _, f := range d.Fields {
			if f.Tag != "query" && f.Tag != "header" {
				continue
			}
			lit, wire := paramLiteral(op, f)
			if lit == "" {
				continue
			}
			fmt.Fprintf(b, "\tparams.%s = %s\n", f.GoName, lit)
			if f.Tag == "query" {
				wantQuery[f.Name] = wire
			} else {
				wantHeaders[f.Name] = wire
			}
		}
		args = append(args, "params")
	}

	call := fmt.Sprintf("client.%s.%s(%s)", clientPath(m.Service), name, strings.Join(args, ", "))
	switch m.Return {
	case RetNone:
		fmt.Fprintf(b, "\tif err := %s; err != nil {\n\t\tt.Fatal(err)\n\t}\n", call)
	default:
		fmt.Fprintf(b, "\tres, err := %s\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n", call)
	}

	// Request checks.
	fmt.Fprintf(b, "\treq := mock.single(t)\n")
	fmt.Fprintf(b, "\treq.expect(t, %s, %s)\n", strconv.Quote(op.HTTPMethod), strconv.Quote(wantPath))
	switch op.Naming.Auth {
	case "api_key":
		b.WriteString("\treq.expectAPIKey(t)\n")
	case "account_token":
		b.WriteString("\treq.expectAccountToken(t)\n")
	case "none":
		b.WriteString("\treq.expectNoCredentials(t)\n")
	}
	if len(wantQuery) > 0 {
		fmt.Fprintf(b, "\treq.expectQuery(t, %s)\n", mapLiteral(wantQuery))
	}
	for _, k := range sortedKeys(wantHeaders) {
		fmt.Fprintf(b, "\treq.expectHeader(t, %s, %s)\n", strconv.Quote(k), strconv.Quote(wantHeaders[k]))
	}
	if wantBody != "" {
		fmt.Fprintf(b, "\treq.expectJSON(t, %s)\n", goString(wantBody))
	}
	if params == nil && m.Params != nil && m.BodyKind != "bodyNone" {
		b.WriteString("\treq.expectNoBody(t)\n")
	}
	if multipart {
		fmt.Fprintf(b, "\treq.expectMultipart(t, %s, %s)\n", sliceLiteral(wantForm), sliceLiteral(wantFiles))
	}

	// Response checks.
	switch m.Return {
	case RetBinary:
		b.WriteString("\tdefer func() { _ = res.Body.Close() }()\n\tdata, err := io.ReadAll(res.Body)\n\tif err != nil || string(data) != \"binary-content\" {\n\t\tt.Fatalf(\"file body = %q, %v\", data, err)\n\t}\n")
	case RetPage:
		b.WriteString("\tif res == nil {\n\t\tt.Fatal(\"nil page\")\n\t}\n")
		if len(checkKeys) > 0 {
			fmt.Fprintf(b, "\tif len(res.Results) == 0 {\n\t\tt.Fatal(\"no results\")\n\t}\n\texpectFields(t, res.Results[0].JSON, %s)\n", sliceLiteral(checkKeys))
		}
	case RetJSON:
		if m.Result.Kind == TRaw {
			fmt.Fprintf(b, "\tif string(res) != %s {\n\t\tt.Fatalf(\"raw response = %%s\", res)\n\t}\n", goString(respBody))
		} else if m.Result.Kind == TNamed && g.byName[m.Result.Name] != nil && g.byName[m.Result.Name].Kind == DStruct {
			if len(checkKeys) > 0 {
				fmt.Fprintf(b, "\texpectFields(t, res.JSON, %s)\n", sliceLiteral(checkKeys))
			} else {
				b.WriteString("\tif res == nil {\n\t\tt.Fatal(\"nil result\")\n\t}\n")
			}
		} else {
			b.WriteString("\t_ = res\n")
		}
	}
	b.WriteString("}\n\n")
	return nil
}

// requestExample returns the example of the operation's request body.
func requestExample(op *Operation) *Node {
	rb := op.Doc.Resolve(op.Node.Get("requestBody"))
	if rb == nil {
		return nil
	}
	content := rb.Get("content")
	for _, ct := range []string{"application/json", "multipart/form-data"} {
		media := content.Get(ct)
		if media == nil {
			continue
		}
		if ex := mediaExample(media); ex != nil {
			return ex
		}
		if ex := schemaExample(op.Doc.Resolve(media.Get("schema"))); ex != nil {
			return ex
		}
	}
	return nil
}

// paramLiteral returns the Go expression for a query or header field and
// the value expected on the wire.
func paramLiteral(op *Operation, f *Field) (string, string) {
	var p *Node
	for _, prm := range op.Parameters() {
		if prm.Get("name").Text() == f.Name {
			p = prm
		}
	}
	schema := view(op.Doc, p.Get("schema"))
	ex := p.Get("example")
	if ex == nil {
		ex = schemaExample(schema)
	}
	wrap := func(inner string) string {
		if f.Opt {
			return "didit.Some(" + inner + ")"
		}
		return inner
	}
	t := f.Type
	switch t.Kind {
	case TString:
		v := "value"
		if strings.EqualFold(f.Name, "authorization") {
			v = "Bearer test-console-token"
		} else if ex != nil && ex.Kind == String {
			v = ex.Str
		} else if schema.Get("format").Text() == "uuid" {
			v = "3fa85f64-5717-4562-b3fc-2c963f66afa6"
		} else if schema.Get("format").Text() == "date" {
			v = "2025-01-02"
		}
		if f.Opt {
			return "didit.String(" + strconv.Quote(v) + ")", v
		}
		return strconv.Quote(v), v
	case TNamed:
		d := f.Type.Decl
		if d == nil || d.Kind != DEnum || len(d.Values) == 0 {
			return "", ""
		}
		v := d.Values[0]
		if ex != nil && ex.Kind == String {
			for _, ev := range d.Values {
				if ev.Value == ex.Str {
					v = ev
				}
			}
		}
		return wrap("didit." + v.GoName), v.Value
	case TInt:
		v := "1"
		if ex != nil && ex.Kind == Number {
			if _, err := strconv.ParseInt(ex.Str, 10, 64); err == nil {
				v = ex.Str
			}
		} else if d := schema.Get("default"); d != nil && d.Kind == Number {
			if _, err := strconv.ParseInt(d.Str, 10, 64); err == nil {
				v = d.Str
			}
		}
		if f.Opt {
			return "didit.Int(" + v + ")", v
		}
		return v, v
	case TFloat:
		if f.Opt {
			return "didit.Float(1.5)", "1.5"
		}
		return "1.5", "1.5"
	case TBool:
		if f.Opt {
			return "didit.Bool(true)", "true"
		}
		return "true", "true"
	case TTime:
		lit := "time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)"
		return wrap(lit), "2025-01-02T03:04:05Z"
	case TSlice:
		if t.Elem.Kind == TString {
			return wrap(`[]string{"a", "b"}`), "a,b"
		}
	}
	return "", ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mapLiteral(m map[string]string) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, strconv.Quote(k)+": "+strconv.Quote(m[k]))
	}
	return "map[string]string{" + strings.Join(parts, ", ") + "}"
}

func sliceLiteral(items []string) string {
	var parts []string
	for _, s := range items {
		parts = append(parts, strconv.Quote(s))
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}
