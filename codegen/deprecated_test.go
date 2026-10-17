package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/didit-protocol/sdk-go/internal/version"
)

// expiredDeprecations returns a message for every entry of a
// deprecated-operations.json whose remove_in the SDK version has reached.
func expiredDeprecations(data []byte, current string) ([]string, error) {
	root, err := ParseNode(data)
	if err != nil {
		return nil, err
	}
	methods := root.Get("methods")
	var out []string
	for _, name := range methods.ObjKeys() {
		e := methods.Get(name)
		if r := e.Get("reason").Text(); r != "removed" && r != "renamed" {
			return nil, fmt.Errorf("%s: reason %q is not removed or renamed", name, r)
		}
		removeIn := e.Get("remove_in").Text()
		cmp, err := syncCompareVersions(current, removeIn)
		if err != nil {
			return nil, fmt.Errorf("%s: remove_in: %w", name, err)
		}
		if cmp >= 0 {
			out = append(out, fmt.Sprintf(
				"%s was due for removal in %s and the SDK version is %s: delete its entry from openapi/deprecated-operations.json and run `go run ./codegen` in this release",
				name, removeIn, current))
		}
	}
	return out, nil
}

// TestDeprecatedMethodsExpire enforces DESIGN 3.1: a removed or renamed
// method stays deprecated for one minor version, and the release that
// reaches remove_in deletes it.
func TestDeprecatedMethodsExpire(t *testing.T) {
	expectNoExpiredDeprecations(t, filepath.Join("..", "openapi", "deprecated-operations.json"))
}

func expectNoExpiredDeprecations(t *testing.T, file string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := expiredDeprecations(data, version.Version)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	for _, msg := range expired {
		t.Error(msg)
	}
}

func TestExpiredDeprecations(t *testing.T) {
	file := []byte(`{"methods": {
		"billing.retrieve_balance": {"reason": "removed", "remove_in": "0.5.0"},
		"billing.old_top_up": {"reason": "renamed", "replacement": "billing.top_up", "remove_in": "0.6.0"}
	}}`)
	for current, want := range map[string]int{"0.4.9": 0, "0.5.0-rc.1": 0, "0.5.0": 1, "0.5.1": 1, "0.6.0": 2, "1.0.0": 2} {
		got, err := expiredDeprecations(file, current)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Errorf("version %s: %d expired, want %d: %v", current, len(got), want, got)
		}
	}
	got, _ := expiredDeprecations(file, "0.5.0")
	if len(got) != 1 || !strings.Contains(got[0], "billing.retrieve_balance was due for removal in 0.5.0") ||
		!strings.Contains(got[0], "delete its entry from openapi/deprecated-operations.json and run `go run ./codegen`") {
		t.Errorf("message = %v", got)
	}
	if _, err := expiredDeprecations([]byte(`{"methods": {"a.b": {"reason": "removed", "remove_in": "soon"}}}`), "0.1.0"); err == nil {
		t.Error("a remove_in that is not a version was accepted")
	}
	if _, err := expiredDeprecations([]byte(`{"methods": {"a.b": {"reason": "gone", "remove_in": "0.2.0"}}}`), "0.1.0"); err == nil {
		t.Error("an unknown reason was accepted")
	}
}

// deprecatedEntry is a valid hand-written entry for the fixture.
func deprecatedEntry(name, extra string) string {
	resource, method, _ := strings.Cut(name, ".")
	if i := strings.LastIndex(name, "."); i > 0 {
		resource, method = name[:i], name[i+1:]
	}
	return fmt.Sprintf(`{"reason": "removed", "remove_in": "0.5.0", "since": "2026-10-18",
		"api": "verification", "http_method": "GET", "path": "/v3/old/",
		"x-didit-sdk": {"resource": %q, "method": %q, "auth": "api_key", "server": true},
		"operation": {"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/OldThing"}}}}}},
		"components": {"schemas": {"OldThing": {"type": "object", "properties": {"id": {"type": "string"}}}}}%s}`, resource, method, extra)
}

func writeDeprecated(t *testing.T, f *syncFixture, entries map[string]string) {
	t.Helper()
	var parts []string
	for name, e := range entries {
		parts = append(parts, fmt.Sprintf("%q: %s", name, e))
	}
	f.write("deprecated-operations.json", []byte(`{"description": "", "methods": {`+strings.Join(parts, ", ")+`}}`))
}

func TestLoadDeprecatedAddsTheMethodWithItsComponents(t *testing.T) {
	f := newSyncFixture(t)
	// A Python-shaped entry (operation_key only) loads as well.
	entry := strings.Replace(deprecatedEntry("things.old_list", ""), `"api": "verification", "http_method": "GET", "path": "/v3/old/",`, `"operation_key": "GET /v3/old/",`, 1)
	writeDeprecated(t, f, map[string]string{"things.old_list": entry})
	spec, err := LoadSpec(f.root)
	if err != nil {
		t.Fatal(err)
	}
	var op *Operation
	for _, o := range spec.ServerOps() {
		if o.Deprecated != nil {
			op = o
		}
	}
	if op == nil || op.Key != "GET /v3/old/ (deprecated)" || op.Path != "/v3/old/" || op.HTTPMethod != "GET" || op.Deprecated.Method != "things.old_list" {
		t.Fatalf("deprecated operation = %+v", op)
	}
	files := f.generate()
	expectContains(t, files["things_gen.go"],
		"func (r *ThingsService) OldList(ctx context.Context, opts ...option.RequestOption) (*OldThing, error)",
		"type OldThing struct",
		"// Deprecated: The operation was removed from the API on 2026-10-18.")
}

func TestLoadDeprecatedRejectsBadEntries(t *testing.T) {
	for _, tc := range []struct {
		name, method, entry, want string
	}{
		{"provided again", "billing.retrieve_balance", deprecatedEntry("billing.retrieve_balance", ""),
			"GET /v3/billing/balance/ provides billing.retrieve_balance again, so it is no longer deprecated; delete this entry from openapi/deprecated-operations.json"},
		{"name mismatch", "things.other", deprecatedEntry("things.old_list", ""), "the entry name must equal its x-didit-sdk resource.method (things.old_list)"},
		{"bad reason", "things.old_list", strings.Replace(deprecatedEntry("things.old_list", ""), `"reason": "removed"`, `"reason": "gone"`, 1), `reason "gone" is not removed or renamed`},
		{"rename without replacement", "things.old_list", strings.Replace(deprecatedEntry("things.old_list", ""), `"reason": "removed"`, `"reason": "renamed"`, 1), "reason renamed needs a replacement"},
		{"key mismatch", "things.old_list", deprecatedEntry("things.old_list", `, "operation_key": "POST /v3/old/"`), `operation_key "POST /v3/old/" does not match`},
		{"bad method", "things.old_list", strings.Replace(deprecatedEntry("things.old_list", ""), `"http_method": "GET"`, `"http_method": "TRACE"`, 1), `"TRACE" "/v3/old/" is not an HTTP method and a path`},
		{"not server", "things.old_list", strings.Replace(deprecatedEntry("things.old_list", ""), `"server": true`, `"server": false`, 1), "only server methods are kept"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSyncFixture(t)
			writeDeprecated(t, f, map[string]string{tc.method: tc.entry})
			_, err := LoadSpec(f.root)
			if err == nil {
				t.Fatal("LoadSpec accepted the entry")
			}
			expectContains(t, err.Error(), "openapi/deprecated-operations.json "+tc.method+": ", tc.want)
		})
	}
}

func TestLoadDeprecatedPrefersCurrentComponents(t *testing.T) {
	// A component the document still has wins over the entry's copy (one Go
	// type per name), and one it lost comes back from the entry.
	f := newSyncFixture(t)
	entry := deprecatedEntry("things.old_list", "")
	entry = strings.Replace(entry, `"components": {"schemas": {`, `"components": {"schemas": {"Currency": {"type": "string", "enum": ["XXX"]}, `, 1)
	entry = strings.Replace(entry, `"properties": {"id": {"type": "string"}}`, `"properties": {"id": {"type": "string"}, "currency": {"$ref": "#/components/schemas/Currency"}}`, 1)
	writeDeprecated(t, f, map[string]string{"things.old_list": entry})
	files := f.generate()
	expectContains(t, files["things_gen.go"], "type OldThing struct")
	all := strings.Join([]string{files["billing_gen.go"], files["shared_gen.go"], files["things_gen.go"]}, "\n")
	expectContains(t, all, `CurrencyEur Currency = "EUR"`)
	if strings.Contains(all, `"XXX"`) {
		t.Error("the entry's copy of Currency replaced the document's")
	}
}

// TestDeprecatingSyncOfTheRepositorySpec runs a sync that fills
// openapi/deprecated-operations.json on a copy of the repository's openapi/:
// upstream removes one GET operation and renames another. What the
// repository's own tests read from that file must still hold. The two
// operations are picked from the current contract, so the test keeps working
// after real syncs. (CI also runs every test on a checkout synced like this.)
func TestDeprecatingSyncOfTheRepositorySpec(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "openapi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"verification.json", "account.json", "overlay.json", "SOURCE.json", "webhook-vectors.json", "deprecated-operations.json"} {
		data, err := os.ReadFile(filepath.Join("..", "openapi", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	countDeprecated := func() int {
		t.Helper()
		files, err := Generate(root)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		n := 0
		for _, f := range files {
			n += strings.Count(string(f.Content), "\n// Deprecated: ")
		}
		return n
	}
	before := countDeprecated()

	// The first two GET operations, in path order, that are server methods.
	doc := mustNode(t, string(read("verification.json")))
	overlay := mustNode(t, string(read("overlay.json"))).Get("operations")
	paths := doc.Get("paths")
	var picked []string
	keys := slices.Clone(paths.ObjKeys())
	slices.Sort(keys)
	for _, p := range keys {
		op := paths.Path(p, "get")
		naming := op.Get("x-didit-sdk")
		if naming == nil {
			naming = overlay.Get("GET " + p)
		}
		if op != nil && naming.Get("server").Truthy() && len(picked) < 2 {
			picked = append(picked, p)
		}
	}
	if len(picked) < 2 {
		t.Fatalf("found %d GET server operations, want 2", len(picked))
	}
	name := func(p string) (resource, method string) {
		naming := paths.Path(p, "get", "x-didit-sdk")
		if naming == nil {
			naming = overlay.Get("GET " + p)
		}
		return naming.Get("resource").Text(), naming.Get("method").Text()
	}
	removedRes, removedMethod := name(picked[0])
	renamedRes, renamedMethod := name(picked[1])
	syncDeleteKey(paths.Get(picked[0]), "get")
	renamed := mustNode(t, `{"auth": "api_key", "server": true}`)
	renamed.Set("resource", syncString(renamedRes))
	renamed.Set("method", syncString(renamedMethod+"_renamed"))
	paths.Path(picked[1], "get").Set("x-didit-sdk", renamed)

	res, err := syncSpecs(dir, map[string][]byte{
		"verification.json": []byte(syncEncode(doc, syncFileStyle)),
		"account.json":      read("account.json"),
	}, syncTestNow, version.Version)
	if err != nil {
		t.Fatal(err)
	}
	removedKey, renamedKey := removedRes+"."+removedMethod, renamedRes+"."+renamedMethod
	expectContains(t, section(res.summary, "Breaking changes"),
		"`"+removedKey+"` was removed upstream",
		"`"+renamedKey+"` was renamed upstream to `"+renamedKey+"_renamed`")
	methods := mustNode(t, string(read("deprecated-operations.json"))).Get("methods")
	for _, key := range []string{removedKey, renamedKey} {
		if !methods.Has(key) {
			t.Errorf("%s is not in deprecated-operations.json", key)
		}
	}

	file := filepath.Join(dir, "deprecated-operations.json")
	expectRoundTrip(t, file)
	expectNoExpiredDeprecations(t, file)
	if got := countDeprecated(); got != before+2 {
		t.Errorf("%d deprecated methods after the sync, want %d", got, before+2)
	}
}
