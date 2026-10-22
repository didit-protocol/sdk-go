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

// TestContract checks that every server operation of openapi/ has exactly one
// SDK method, and that the SDK has no other method (docs/DESIGN.md section
// 11). It reads the spec itself, so it needs no update when the API grows.
func TestContract(t *testing.T) {
	want := map[string]string{} // "Sessions.Imports.Create" -> operation
	overlay := readJSON(t, "openapi/overlay.json")["operations"].(map[string]any)
	for _, doc := range []struct{ file, prefix string }{
		{"openapi/verification.json", ""},
		{"openapi/account.json", "account "},
	} {
		spec := readJSON(t, doc.file)
		for path, item := range spec["paths"].(map[string]any) {
			for method, op := range item.(map[string]any) {
				switch method {
				case "get", "post", "put", "patch", "delete":
				default:
					continue
				}
				key := doc.prefix + strings.ToUpper(method) + " " + path
				sdk, ok := op.(map[string]any)["x-didit-sdk"].(map[string]any)
				if !ok {
					sdk, ok = overlay[key].(map[string]any)
				}
				if !ok {
					t.Errorf("%s has no SDK naming", key)
					continue
				}
				if server, _ := sdk["server"].(bool); !server {
					continue
				}
				var parts []string
				for _, seg := range strings.Split(sdk["resource"].(string), ".") {
					parts = append(parts, naming.Pascal(seg))
				}
				parts = append(parts, naming.Pascal(sdk["method"].(string)))
				name := strings.Join(parts, ".")
				if prev, dup := want[name]; dup {
					t.Errorf("%s and %s both map to %s", prev, key, name)
				}
				want[name] = key
			}
		}
	}
	deprecated := map[string]bool{}
	if data, err := os.ReadFile("openapi/deprecated-operations.json"); err == nil {
		var dep struct {
			Methods map[string]json.RawMessage `json:"methods"`
		}
		if err := json.Unmarshal(data, &dep); err != nil {
			t.Fatal(err)
		}
		for name := range dep.Methods {
			var parts []string
			for _, seg := range strings.Split(name, ".") {
				parts = append(parts, naming.Pascal(seg))
			}
			deprecated[strings.Join(parts, ".")] = true
		}
	}

	got := map[string]bool{}
	var walk func(v reflect.Value, prefix string)
	walk = func(v reflect.Value, prefix string) {
		typ := v.Type()
		for i := 0; i < typ.NumMethod(); i++ {
			m := typ.Method(i)
			if prefix == "" {
				continue // Client's own methods (WithOptions)
			}
			if strings.HasSuffix(m.Name, "AutoPaging") {
				if _, ok := typ.MethodByName(strings.TrimSuffix(m.Name, "AutoPaging")); !ok {
					t.Errorf("%s%s has no list method", prefix, m.Name)
				}
				continue
			}
			got[prefix+m.Name] = true
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

	var missing, extra []string
	for name, key := range want {
		if !got[name] {
			missing = append(missing, name+" ("+key+")")
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok && !deprecated[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("operations without an SDK method:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("SDK methods without an operation:\n  %s", strings.Join(extra, "\n  "))
	}
	if len(want) == 0 {
		t.Fatal("no operations found")
	}
	t.Logf("%d server operations, %d SDK methods", len(want), len(got))
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
