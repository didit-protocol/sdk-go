package didit_test

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/internal/naming"
)

// TestContractRequestModels checks that every property of a server
// operation's JSON request body has a field in the params type its generated
// method takes. TestContract only checks that each operation has a method, so
// a body whose properties the generator did not read (transactions.batch_delete
// keeps them in oneOf variants) shipped as an empty params struct.
func TestContractRequestModels(t *testing.T) {
	overlay := readJSON(t, "openapi/overlay.json")["operations"].(map[string]any)
	companions := paramsMethods(t)
	methods := clientMethodTypes()

	type wantModel struct {
		operation string
		props     []string
	}
	want := map[string]wantModel{}
	for _, doc := range []struct{ file, prefix string }{
		{"openapi/verification.json", ""},
		{"openapi/account.json", "account "},
	} {
		spec := readJSON(t, doc.file)
		components, _ := spec["components"].(map[string]any)["schemas"].(map[string]any)
		for path, item := range spec["paths"].(map[string]any) {
			for method, raw := range item.(map[string]any) {
				switch method {
				case "get", "post", "put", "patch", "delete":
				default:
					continue
				}
				op := raw.(map[string]any)
				key := doc.prefix + strings.ToUpper(method) + " " + path
				sdk, ok := op["x-didit-sdk"].(map[string]any)
				if !ok {
					sdk, ok = overlay[key].(map[string]any)
				}
				if !ok {
					continue
				}
				if server, _ := sdk["server"].(bool); !server {
					continue
				}
				props := requestBodyProperties(spec, components, op)
				if len(props) == 0 {
					continue
				}
				var parts []string
				for _, seg := range strings.Split(sdk["resource"].(string), ".") {
					parts = append(parts, naming.Pascal(seg))
				}
				parts = append(parts, naming.Pascal(sdk["method"].(string)))
				if companion := companions[sdk["resource"].(string)+"."+sdk["method"].(string)]; companion != "" {
					parts[len(parts)-1] = naming.Pascal(companion)
				}
				want[strings.Join(parts, ".")] = wantModel{operation: key, props: sortedKeys(props)}
			}
		}
	}
	if len(want) == 0 {
		t.Fatal("no request bodies found")
	}
	for name, model := range want {
		mt, ok := methods[name]
		if !ok {
			t.Errorf("%s (%s) has no SDK method", name, model.operation)
			continue
		}
		params, ok := requestParamsType(mt)
		if !ok {
			t.Errorf("%s (%s): no params argument for a request body", name, model.operation)
			continue
		}
		if body, ok := bodyFieldType(params); ok {
			params = body
		}
		tags := jsonFieldTags(params)
		var missing []string
		for _, prop := range model.props {
			if !tags[prop] {
				missing = append(missing, prop)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s (%s): %s has no field for %s", name, model.operation, params, strings.Join(missing, ", "))
		}
	}
}

// requestBodyProperties returns the top-level properties of a server
// operation's JSON request body, read from the spec the same way the
// generator reads it: $ref and allOf are followed, and object oneOf/anyOf
// variants contribute their fields.
func requestBodyProperties(spec, components map[string]any, op map[string]any) map[string]bool {
	body, _ := op["requestBody"].(map[string]any)
	content, _ := body["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	schema, _ := media["schema"].(map[string]any)
	return objectProperties(spec, components, schema, map[string]bool{})
}

// objectProperties returns the top-level properties of an object schema.
// readOnly properties are left out, like the generator does for a request.
func objectProperties(spec, components map[string]any, s map[string]any, seen map[string]bool) map[string]bool {
	props := map[string]bool{}
	if s == nil {
		return props
	}
	if ref, _ := s["$ref"].(string); ref != "" {
		if seen[ref] {
			return props
		}
		seen[ref] = true
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		comp, _ := components[name].(map[string]any)
		return objectProperties(spec, components, comp, seen)
	}
	if all, ok := s["allOf"].([]any); ok {
		for _, part := range all {
			p, _ := part.(map[string]any)
			for k := range objectProperties(spec, components, p, seen) {
				props[k] = true
			}
		}
	}
	if p, ok := s["properties"].(map[string]any); ok {
		for k, raw := range p {
			if prop, _ := raw.(map[string]any); prop != nil && prop["readOnly"] == true {
				continue
			}
			props[k] = true
		}
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		variants, _ := s[key].([]any)
		if len(variants) == 0 {
			continue
		}
		var parts []map[string]any
		for _, raw := range variants {
			v, _ := raw.(map[string]any)
			parts = append(parts, v)
			if len(objectProperties(spec, components, v, map[string]bool{})) == 0 && !isNullSchema(v) {
				parts = nil
				break
			}
		}
		for _, v := range parts {
			for k := range objectProperties(spec, components, v, seen) {
				props[k] = true
			}
		}
	}
	return props
}

func isNullSchema(s map[string]any) bool {
	typ, _ := s["type"].(string)
	return typ == "null"
}

// paramsMethods reads openapi/compatibility.json's params_methods: a released
// method that took no parameters keeps that signature, and the named
// companion method takes them.
func paramsMethods(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("openapi/compatibility.json")
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}
		}
		t.Fatal(err)
	}
	var doc struct {
		ParamsMethods map[string]string `json:"params_methods"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.ParamsMethods
}

// clientMethodTypes returns the bound method type of every method reachable
// from the client, keyed like the contract test's names.
func clientMethodTypes() map[string]reflect.Type {
	out := map[string]reflect.Type{}
	var walk func(v reflect.Value, prefix string)
	walk = func(v reflect.Value, prefix string) {
		typ := v.Type()
		for i := 0; i < typ.NumMethod(); i++ {
			if prefix != "" {
				out[prefix+typ.Method(i).Name] = v.Method(i).Type()
			}
		}
		elem := v.Elem()
		for i := 0; i < elem.NumField(); i++ {
			f := elem.Type().Field(i)
			if !f.IsExported() || f.Type.Kind() != reflect.Pointer || !strings.HasSuffix(f.Type.Elem().Name(), "Service") {
				continue
			}
			walk(elem.Field(i), prefix+f.Name+".")
		}
	}
	walk(reflect.ValueOf(didit.NewClient()), "")
	return out
}

// requestParamsType returns the params argument of a generated method. The
// last parameter is the variadic request options.
func requestParamsType(mt reflect.Type) (reflect.Type, bool) {
	if mt.NumIn() < 3 {
		return nil, false
	}
	return mt.In(mt.NumIn() - 2), true
}

// bodyFieldType returns the type of a params struct's whole-body field.
func bodyFieldType(params reflect.Type) (reflect.Type, bool) {
	if params.Kind() != reflect.Struct {
		return nil, false
	}
	f, ok := params.FieldByName("Body")
	if !ok || f.Tag.Get("json") != "-" {
		return nil, false
	}
	t := f.Type
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, false
	}
	return t, true
}

func jsonFieldTags(t reflect.Type) map[string]bool {
	tags := map[string]bool{}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return tags
	}
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			tags[name] = true
		}
	}
	return tags
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
