package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Tests of the spec sync. They never touch the network: the documents are
// small fixtures built here, and downloads go to an httptest server.

var syncTestNow = time.Date(2026, 10, 18, 6, 0, 0, 0, time.UTC)

// syncTestVersion is the SDK version the tests sync with (remove_in 0.5.0).
const syncTestVersion = "0.4.2"

const fixtureVerification = `{
 "openapi": "3.1.0",
 "info": {"title": "Didit verification (fixture)", "version": "1"},
 "paths": {
  "/v3/billing/balance/": {
   "get": {
    "operationId": "billing_balance_retrieve",
    "summary": "Get the credit balance",
    "parameters": [{"$ref": "#/components/parameters/CurrencyQuery"}],
    "responses": {
     "200": {
      "description": "The balance.",
      "content": {"application/json": {"schema": {
       "type": "object",
       "properties": {
        "credits": {"type": "string", "description": "Credits left, a decimal string."},
        "currency": {"$ref": "#/components/schemas/Currency"},
        "breakdown": {"$ref": "#/components/schemas/BalanceBreakdown"}
       },
       "required": ["credits"]
      }}}
     },
     "404": {"$ref": "#/components/responses/NotFound"}
    }
   }
  },
  "/v3/billing/top-up/": {
   "post": {
    "operationId": "billing_top_up",
    "summary": "Buy credits",
    "requestBody": {"required": true, "content": {"application/json": {"schema": {
     "type": "object",
     "properties": {"amount": {"type": "integer"}, "currency": {"$ref": "#/components/schemas/Currency"}},
     "required": ["amount"]
    }}}},
    "responses": {"200": {"description": "Checkout.", "content": {"application/json": {"schema": {
     "type": "object", "properties": {"checkout_url": {"type": "string"}}
    }}}}}
   }
  },
  "/v3/session/": {
   "post": {
    "operationId": "session_create",
    "summary": "Create a session",
    "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/SessionCreate"}}}},
    "responses": {"201": {"description": "Created.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Session"}}}}}
   }
  },
  "/v3/session/{session_id}/decision/": {
   "parameters": [{"name": "session_id", "in": "path", "required": true, "schema": {"type": "string"}}],
   "get": {
    "operationId": "session_decision",
    "summary": "Get the decision",
    "parameters": [{"name": "verbose", "in": "query", "schema": {"type": "boolean"}}],
    "responses": {"200": {"description": "The decision.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Session"}}}}}
   }
  }
 },
 "components": {
  "parameters": {
   "CurrencyQuery": {"name": "currency", "in": "query", "schema": {"$ref": "#/components/schemas/Currency"}}
  },
  "responses": {
   "NotFound": {"description": "Not found.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Problem"}}}}
  },
  "schemas": {
   "BalanceBreakdown": {"type": "object", "properties": {"lines": {"type": "array", "items": {"$ref": "#/components/schemas/BalanceLine"}}}},
   "BalanceLine": {"type": "object", "properties": {"feature": {"type": "string"}, "credits": {"type": "string"}}},
   "Currency": {"type": "string", "enum": ["EUR", "USD"]},
   "Problem": {"type": "object", "properties": {"detail": {"type": "string"}}},
   "Session": {
    "type": "object",
    "properties": {"session_id": {"type": "string"}, "status": {"type": "string", "enum": ["Approved", "Declined"]}},
    "required": ["session_id"]
   },
   "SessionCreate": {
    "type": "object",
    "properties": {"workflow_id": {"type": "string"}, "vendor_data": {"type": "string"}},
    "required": ["workflow_id"]
   }
  }
 }
}`

const fixtureAccount = `{
 "openapi": "3.1.0",
 "info": {"title": "Didit account (fixture)", "version": "1"},
 "paths": {
  "/organizations/": {
   "get": {
    "operationId": "organizations_list",
    "summary": "List organizations",
    "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {
     "type": "array", "items": {"$ref": "#/components/schemas/Organization"}
    }}}}}
   }
  }
 },
 "components": {"schemas": {"Organization": {"type": "object", "properties": {"uuid": {"type": "string"}, "name": {"type": "string"}}}}}
}`

const fixtureOverlay = `{
 "description": "SDK naming (fixture).",
 "version": 1,
 "operations": {
  "GET /v3/billing/balance/": {"auth": "api_key", "method": "retrieve_balance", "resource": "billing", "server": true},
  "GET /v3/session/{session_id}/decision/": {"auth": "api_key", "method": "retrieve_decision", "resource": "sessions", "server": true},
  "POST /v3/billing/top-up/": {"auth": "api_key", "method": "top_up", "resource": "billing", "server": true},
  "POST /v3/session/": {"auth": "api_key", "method": "create", "resource": "sessions", "server": true},
  "account GET /organizations/": {"auth": "account_token", "method": "list", "resource": "account.organizations", "server": true}
 }
}`

const fixtureSource = `{
 "verification.json": {"url": "https://docs.didit.me/openapi-25.json", "sha256": "0", "fetched_at": "2026-10-17T10:00:00Z"},
 "account.json": {"url": "https://docs.didit.me/openapi-auth.json", "sha256": "0", "fetched_at": "2026-10-17T10:00:00Z"},
 "webhook-vectors.json": {"generator": "fixture", "source": "fixture"}
}`

// syncFixture is a repository root with a small openapi/ directory.
type syncFixture struct {
	t    *testing.T
	root string
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	f := &syncFixture{t: t, root: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(f.root, "openapi"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeNode("verification.json", mustNode(t, fixtureVerification))
	f.writeNode("account.json", mustNode(t, fixtureAccount))
	f.writeNode("overlay.json", mustNode(t, fixtureOverlay))
	f.writeNode("SOURCE.json", mustNode(t, fixtureSource))
	// An empty file of its own, like the one the SDK was seeded with. Not the
	// repository's: the first sync that deprecates a method fills that one.
	deprecated := syncObject()
	deprecated.Set("description", syncString(syncDeprecatedDescription))
	deprecated.Set("methods", syncObject())
	f.write("deprecated-operations.json", []byte(syncEncode(deprecated, syncFileStyle)+"\n"))
	return f
}

func mustNode(t *testing.T, s string) *Node {
	t.Helper()
	n, err := ParseNode([]byte(s))
	if err != nil {
		t.Fatalf("fixture JSON: %v\n%s", err, s)
	}
	return n
}

func (f *syncFixture) file(name string) string { return filepath.Join(f.root, "openapi", name) }

func (f *syncFixture) read(name string) []byte {
	f.t.Helper()
	data, err := os.ReadFile(f.file(name))
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}

func (f *syncFixture) node(name string) *Node {
	f.t.Helper()
	return mustNode(f.t, string(f.read(name)))
}

func (f *syncFixture) write(name string, data []byte) {
	f.t.Helper()
	if err := os.WriteFile(f.file(name), data, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *syncFixture) writeNode(name string, n *Node) {
	f.write(name, []byte(syncEncode(n, syncFileStyle)))
}

// snapshot returns every file of openapi/.
func (f *syncFixture) snapshot() map[string]string {
	f.t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.root, "openapi"))
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		out[e.Name()] = string(f.read(e.Name()))
	}
	return out
}

func (f *syncFixture) sync(downloads map[string][]byte) (syncResult, error) {
	return syncSpecs(filepath.Join(f.root, "openapi"), downloads, syncTestNow, syncTestVersion)
}

// syncVerification syncs with doc as the new verification.json and the
// current account.json, and returns the summary.
func (f *syncFixture) syncVerification(doc *Node) string {
	f.t.Helper()
	res, err := f.sync(map[string][]byte{
		"verification.json": []byte(syncEncode(doc, syncFileStyle)),
		"account.json":      f.read("account.json"),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if !res.changed {
		f.t.Fatal("changed = false, want true")
	}
	return res.summary
}

// generate runs the generator on the fixture.
func (f *syncFixture) generate() map[string]string {
	f.t.Helper()
	files, err := Generate(f.root)
	if err != nil {
		f.t.Fatalf("generate: %v", err)
	}
	out := map[string]string{}
	for _, file := range files {
		out[file.Name] = string(file.Content)
	}
	return out
}

func (f *syncFixture) deprecated() *Node {
	f.t.Helper()
	return f.node("deprecated-operations.json").Get("methods")
}

// section returns the body of a "## title" section of a summary.
func section(summary, title string) string {
	_, rest, ok := strings.Cut(summary, "## "+title+"\n")
	if !ok {
		return ""
	}
	body, _, _ := strings.Cut(rest, "\n## ")
	return body
}

func expectContains(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

// withoutDeprecated removes the "Deprecated:" paragraphs from generated code.
func withoutDeprecated(src string) string {
	lines := strings.Split(src, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "// Deprecated:") {
			if n := len(out); n > 0 && out[n-1] == "//" {
				out = out[:n-1]
			}
			for i+1 < len(lines) && lines[i+1] != "//" && strings.HasPrefix(lines[i+1], "// ") {
				i++
			}
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

// expectSameGenerated checks that after is before plus Deprecated
// paragraphs, file by file.
func expectSameGenerated(t *testing.T, before, after map[string]string) {
	t.Helper()
	for name, want := range before {
		got, ok := after[name]
		if !ok {
			t.Errorf("%s is no longer generated", name)
			continue
		}
		if got := withoutDeprecated(got); got != want {
			t.Errorf("%s differs (beyond Deprecated paragraphs):\n--- before\n%s\n--- after\n%s", name, want, got)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("%s is newly generated", name)
		}
	}
}

// decl returns the doc comment and the declaration starting with header.
func decl(t *testing.T, src, header string) string {
	t.Helper()
	i := strings.Index(src, "\n"+header)
	if i < 0 {
		t.Fatalf("no %q in generated code", header)
	}
	start := strings.LastIndex(src[:i], "\n\n") + 2
	line, _, _ := strings.Cut(src[i+1:], "\n")
	if !strings.HasSuffix(line, "{") {
		return src[start : i+1+len(line)+1]
	}
	end := strings.Index(src[i:], "\n}\n")
	return src[start : i+end+3]
}

func TestSyncWithoutUpstreamChangeTouchesNothing(t *testing.T) {
	f := newSyncFixture(t)
	before := f.snapshot()
	res, err := f.sync(map[string][]byte{
		"verification.json": f.read("verification.json"),
		"account.json":      f.read("account.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.changed || res.summary != "No upstream change.\n" {
		t.Fatalf("got changed=%t summary %q", res.changed, res.summary)
	}
	if after := f.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("files changed")
	}
}

func TestSyncReportsAddedOperations(t *testing.T) {
	f := newSyncFixture(t)
	doc := f.node("verification.json")
	paths := doc.Get("paths")
	paths.Set("/v3/things/", mustNode(t, `{"get": {"operationId": "things_list",
		"x-didit-sdk": {"resource": "things", "method": "list", "auth": "api_key", "server": true},
		"responses": {"200": {"description": "ok"}}}}`))
	paths.Set("/v3/device/", mustNode(t, `{"post": {"operationId": "device_ping",
		"x-didit-sdk": {"resource": "device", "method": "ping", "auth": "device_token", "server": false},
		"responses": {"200": {"description": "ok"}}}}`))
	paths.Set("/v3/unnamed/", mustNode(t, `{"post": {"operationId": "unnamed_create", "responses": {"201": {"description": "ok"}}}}`))
	summary := f.syncVerification(doc)
	expectContains(t, section(summary, "Added operations (3)"),
		"- `GET /v3/things/` -> `things.list`",
		"- `POST /v3/device/` (not exposed by server SDKs: device_token)",
		"- `POST /v3/unnamed/`: **no SDK naming**; add an entry to `openapi/overlay.json`")
	if got := section(summary, "Breaking changes"); got != "None detected.\n" {
		t.Errorf("breaking changes: %q", got)
	}
	// The generator refuses the operation without naming, and names it.
	_, err := LoadSpec(f.root)
	if err == nil {
		t.Fatal("LoadSpec accepted an operation without naming")
	}
	expectContains(t, err.Error(), "POST /v3/unnamed/", "unnamed_create", "no SDK naming", "openapi/overlay.json")
}

func TestSyncKeepsRemovedOperationAsDeprecatedMethod(t *testing.T) {
	f := newSyncFixture(t)
	before := f.generate()

	doc := f.node("verification.json")
	syncDeleteKey(doc.Get("paths"), "/v3/billing/balance/")
	comps := doc.Get("components")
	syncDeleteKey(comps, "parameters")
	syncDeleteKey(comps, "responses")
	syncDeleteKey(comps.Get("schemas"), "BalanceBreakdown")
	syncDeleteKey(comps.Get("schemas"), "BalanceLine")
	summary := f.syncVerification(doc)

	expectContains(t, section(summary, "Breaking changes"),
		"`billing.retrieve_balance` was removed upstream (GET /v3/billing/balance/); kept as a deprecated method until version 0.5.0")
	expectContains(t, section(summary, "Removed operations (1)"),
		"- `GET /v3/billing/balance/` (`billing.retrieve_balance`): kept as a deprecated method until version 0.5.0, recorded in `openapi/deprecated-operations.json`")
	// The schemas it took along stay generated, so their removal breaks nobody yet.
	expectContains(t, section(summary, "Schemas"),
		"- verification.json removed: `BalanceBreakdown`, `BalanceLine`",
		"- verification.json removed upstream but kept for the deprecated methods that use them: `BalanceBreakdown`, `BalanceLine`")
	if strings.Contains(section(summary, "Breaking changes"), "schema `Balance") {
		t.Error("a schema kept by a deprecated method is listed as breaking")
	}

	entry := f.deprecated().Get("billing.retrieve_balance")
	for field, want := range map[string]string{
		"reason": "removed", "operation_key": "GET /v3/billing/balance/", "api": "verification",
		"http_method": "GET", "path": "/v3/billing/balance/", "since": "2026-10-18",
		"deprecated_since": "0.4.2", "remove_in": "0.5.0",
		"note": "Removed from the API contract on 2026-10-18; this method is removed in version 0.5.0.",
	} {
		if got := entry.Get(field).Text(); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	if entry.Has("replacement") {
		t.Error("a removed method has a replacement")
	}
	if want := mustNode(t, fixtureOverlay).Path("operations", "GET /v3/billing/balance/"); !syncEqual(entry.Get("x-didit-sdk"), want) {
		t.Errorf("x-didit-sdk = %s", entry.Get("x-didit-sdk").JSON())
	}
	// The operation keeps its references; components holds what they reach.
	op := entry.Get("operation")
	if got := op.Get("parameters").Elems()[0].Get("$ref").Text(); got != "#/components/parameters/CurrencyQuery" {
		t.Errorf("parameter $ref = %q", got)
	}
	if got := op.Path("responses", "200", "content", "application/json", "schema", "properties", "breakdown", "$ref").Text(); got != "#/components/schemas/BalanceBreakdown" {
		t.Errorf("breakdown $ref = %q", got)
	}
	got := map[string][]string{}
	for _, s := range entry.Get("components").ObjKeys() {
		got[s] = entry.Path("components", s).ObjKeys()
	}
	want := map[string][]string{
		"parameters": {"CurrencyQuery"},
		"responses":  {"NotFound"},
		"schemas":    {"BalanceBreakdown", "BalanceLine", "Currency", "Problem"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("components = %v, want %v", got, want)
	}
	if f.node("overlay.json").Path("operations", "GET /v3/billing/balance/") != nil {
		t.Error("the overlay entry of the removed operation is still there")
	}

	// The generator keeps the method exactly as it was, marked deprecated.
	after := f.generate()
	expectSameGenerated(t, before, after)
	expectContains(t, after["billing_gen.go"],
		"// Deprecated: The operation was removed from the API on 2026-10-18. This method\n// is removed in version 0.5.0.\n//\n//\tGET /v3/billing/balance/\nfunc (r *BillingService) RetrieveBalance(")
}

func TestSyncKeepsPathLevelParametersOfRemovedOperation(t *testing.T) {
	f := newSyncFixture(t)
	before := f.generate()
	doc := f.node("verification.json")
	syncDeleteKey(doc.Get("paths"), "/v3/session/{session_id}/decision/")
	f.syncVerification(doc)

	op := f.deprecated().Path("sessions.retrieve_decision", "operation")
	var names []string
	for _, p := range op.Get("parameters").Elems() {
		names = append(names, p.Get("in").Text()+":"+p.Get("name").Text())
	}
	// The operation's own parameters first, then the path item's: the order
	// the generator reads them in.
	if want := []string{"query:verbose", "path:session_id"}; !reflect.DeepEqual(names, want) {
		t.Errorf("parameters = %v, want %v", names, want)
	}
	expectSameGenerated(t, before, f.generate())
}

func TestSyncKeepsRenamedMethodAsDeprecatedAlias(t *testing.T) {
	f := newSyncFixture(t)
	before := f.generate()

	doc := f.node("verification.json")
	op := doc.Path("paths", "/v3/billing/balance/", "get")
	op.Set("x-didit-sdk", mustNode(t, `{"resource": "billing", "method": "get_balance", "auth": "api_key", "server": true}`))
	summary := f.syncVerification(doc)

	expectContains(t, section(summary, "Breaking changes"),
		"`billing.retrieve_balance` was renamed upstream to `billing.get_balance` (GET /v3/billing/balance/); the old name is a deprecated alias until version 0.5.0")
	expectContains(t, section(summary, "Renamed methods (1)"),
		"- `GET /v3/billing/balance/`: `billing.retrieve_balance` renamed to `billing.get_balance`")
	entry := f.deprecated().Get("billing.retrieve_balance")
	if entry.Get("reason").Text() != "renamed" || entry.Get("replacement").Text() != "billing.get_balance" {
		t.Errorf("entry = %s", entry.JSON())
	}
	// Upstream wins: the overlay entry is deleted, and the summary says so.
	expectContains(t, section(summary, "Overlay"),
		"- `GET /v3/billing/balance/`: upstream x-didit-sdk {\"auth\": \"api_key\", \"method\": \"get_balance\", \"resource\": \"billing\", \"server\": true} differs from the overlay entry {\"auth\": \"api_key\", \"method\": \"retrieve_balance\", \"resource\": \"billing\", \"server\": true}; upstream wins and the entry was deleted; `billing.retrieve_balance` stays as a deprecated alias of `billing.get_balance` until version 0.5.0.",
		"Dropped 1 overlay entries that upstream now carries.")
	if f.node("overlay.json").Path("operations", "GET /v3/billing/balance/") != nil {
		t.Error("the overlay entry upstream overrides was kept")
	}

	// Both methods exist; the old one is unchanged but for its Deprecated
	// paragraph, and its types keep their names next to the new ones.
	after := f.generate()
	billing := after["billing_gen.go"]
	expectContains(t, billing,
		"func (r *BillingService) GetBalance(ctx context.Context, params BillingGetBalanceParams, opts ...option.RequestOption) (*BillingGetBalanceResponse, error)",
		"type BillingGetBalanceResponse struct",
		"// Deprecated: Use [BillingService.GetBalance] instead; the API renamed this\n// operation on 2026-10-18. This method is removed in version 0.5.0.")
	for _, header := range []string{
		"func (r *BillingService) RetrieveBalance(",
		"type BillingRetrieveBalanceParams struct",
		"type BillingRetrieveBalanceResponse struct",
		"type BalanceBreakdown struct",
		"type Currency string",
	} {
		if got, want := withoutDeprecated(decl(t, billing, header)), decl(t, before["billing_gen.go"], header); got != want {
			t.Errorf("%s changed:\n--- before\n%s\n--- after\n%s", header, want, got)
		}
	}

	// Renamed back: the old name is live again (its entry is dropped), and the
	// interim name becomes the deprecated alias in turn.
	op.Set("x-didit-sdk", mustNode(t, `{"resource": "billing", "method": "retrieve_balance", "auth": "api_key", "server": true}`))
	summary = f.syncVerification(doc)
	expectContains(t, summary,
		"- `billing.retrieve_balance` is provided again by `GET /v3/billing/balance/`; its deprecation entry was dropped.")
	methods := f.deprecated()
	if got := methods.ObjKeys(); !reflect.DeepEqual(got, []string{"billing.get_balance"}) {
		t.Fatalf("deprecated methods = %v", got)
	}
	if got := methods.Path("billing.get_balance", "replacement").Text(); got != "billing.retrieve_balance" {
		t.Errorf("replacement = %q", got)
	}
	if _, err := LoadSpec(f.root); err != nil {
		t.Fatal(err)
	}
}

func TestSyncMovedOperationIsNotDeprecated(t *testing.T) {
	f := newSyncFixture(t)
	doc := f.node("verification.json")
	paths := doc.Get("paths")
	item := paths.Get("/v3/billing/balance/")
	syncDeleteKey(paths, "/v3/billing/balance/")
	item.Get("get").Set("x-didit-sdk", mustNode(t, fixtureOverlay).Path("operations", "GET /v3/billing/balance/"))
	paths.Set("/v3/billing/balance-v2/", item)
	summary := f.syncVerification(doc)

	expectContains(t, summary,
		"- `GET /v3/billing/balance/` moved to `GET /v3/billing/balance-v2/`; `billing.retrieve_balance` keeps its name",
		"- `billing.retrieve_balance` moved from `GET /v3/billing/balance/` to `GET /v3/billing/balance-v2/`: ")
	if got := section(summary, "Breaking changes"); got != "None detected.\n" {
		t.Errorf("breaking changes: %q", got)
	}
	if got := f.deprecated().ObjKeys(); len(got) != 0 {
		t.Errorf("deprecated methods = %v", got)
	}
	if _, err := LoadSpec(f.root); err != nil {
		t.Fatal(err)
	}
}

func TestSyncMovedOperationReportsWhatChanged(t *testing.T) {
	f := newSyncFixture(t)
	doc := f.node("verification.json")
	paths := doc.Get("paths")
	item := paths.Get("/v3/billing/balance/")
	syncDeleteKey(paths, "/v3/billing/balance/")
	op := item.Get("get")
	op.Set("x-didit-sdk", mustNode(t, fixtureOverlay).Path("operations", "GET /v3/billing/balance/"))
	op.Get("parameters").Arr = append(op.Get("parameters").Arr,
		mustNode(t, `{"name": "dry_run", "in": "query", "required": true, "schema": {"type": "boolean"}}`))
	op.Path("responses", "200", "content", "application/json").Set("schema", mustNode(t, `{"type": "array", "items": {"type": "object"}}`))
	paths.Set("/v4/billing/balance/", item)
	breaking := section(f.syncVerification(doc), "Breaking changes")
	expectContains(t, breaking,
		"`billing.retrieve_balance` moved from `GET /v3/billing/balance/` to `GET /v4/billing/balance/`",
		"parameter added: dry_run (query, required) (breaking)",
		"response: type object -> array (breaking)")
}

func TestSyncSwappedNamesAreBreaking(t *testing.T) {
	f := newSyncFixture(t)
	doc := f.node("verification.json")
	doc.Path("paths", "/v3/billing/balance/", "get").Set("x-didit-sdk", mustNode(t, `{"resource": "billing", "method": "top_up", "auth": "api_key", "server": true}`))
	doc.Path("paths", "/v3/billing/top-up/", "post").Set("x-didit-sdk", mustNode(t, `{"resource": "billing", "method": "retrieve_balance", "auth": "api_key", "server": true}`))
	breaking := section(f.syncVerification(doc), "Breaking changes")
	expectContains(t, breaking,
		"`billing.top_up` now calls `GET /v3/billing/balance/` instead of `POST /v3/billing/top-up/`",
		"`billing.retrieve_balance` now calls `POST /v3/billing/top-up/` instead of `GET /v3/billing/balance/`")
	if got := f.deprecated().ObjKeys(); len(got) != 0 {
		t.Errorf("deprecated methods = %v", got)
	}
}

func TestSyncOperationThatStopsBeingServerIsRemoved(t *testing.T) {
	f := newSyncFixture(t)
	doc := f.node("verification.json")
	doc.Path("paths", "/v3/billing/balance/", "get").Set("x-didit-sdk", mustNode(t, `{"resource": "billing", "method": "retrieve_balance", "auth": "device_token", "server": false}`))
	breaking := section(f.syncVerification(doc), "Breaking changes")
	expectContains(t, breaking, "`billing.retrieve_balance` (GET /v3/billing/balance/) is removed: the operation now needs device_token")
	// Never kept: server SDKs do not expose device-token operations (DESIGN 4).
	if got := f.deprecated().ObjKeys(); len(got) != 0 {
		t.Errorf("deprecated methods = %v", got)
	}
	if strings.Contains(f.generate()["billing_gen.go"], "RetrieveBalance(") {
		t.Error("RetrieveBalance is still generated")
	}
}

func TestSyncOperationThatLosesItsNamingIsCalledOut(t *testing.T) {
	f := newSyncFixture(t)
	// Upstream named the operation (so the overlay no longer does), then drops it.
	old := f.node("verification.json")
	overlay := f.node("overlay.json")
	naming := overlay.Path("operations", "GET /v3/billing/balance/")
	syncDeleteKey(overlay.Get("operations"), "GET /v3/billing/balance/")
	old.Path("paths", "/v3/billing/balance/", "get").Set("x-didit-sdk", naming)
	f.writeNode("verification.json", old)
	f.writeNode("overlay.json", overlay)

	doc := f.node("verification.json")
	syncDeleteKey(doc.Path("paths", "/v3/billing/balance/", "get"), "x-didit-sdk")
	summary := f.syncVerification(doc)
	expectContains(t, summary, "lost its SDK naming")
	expectContains(t, section(summary, "Breaking changes"), "`billing.retrieve_balance` (GET /v3/billing/balance/) has no SDK naming any more")
	if got := f.deprecated().ObjKeys(); len(got) != 0 {
		t.Errorf("deprecated methods = %v", got)
	}
	if _, err := LoadSpec(f.root); err == nil || !strings.Contains(err.Error(), "no SDK naming") {
		t.Fatalf("LoadSpec error = %v", err)
	}
}

func TestSyncOverlayEntries(t *testing.T) {
	f := newSyncFixture(t)
	doc := f.node("verification.json")
	// Upstream carries the overlay's naming (keys in another order).
	doc.Path("paths", "/v3/session/", "post").Set("x-didit-sdk", mustNode(t, `{"server": true, "resource": "sessions", "method": "create", "auth": "api_key"}`))
	// Upstream carries another name: a rename.
	doc.Path("paths", "/v3/billing/top-up/", "post").Set("x-didit-sdk", mustNode(t, `{"resource": "billing", "method": "buy_credits", "auth": "api_key", "server": true}`))
	// Upstream carries the same name with another auth.
	doc.Path("paths", "/v3/session/{session_id}/decision/", "get").Set("x-didit-sdk", mustNode(t, `{"resource": "sessions", "method": "retrieve_decision", "auth": "account_token", "server": true}`))
	summary := f.syncVerification(doc)

	// Every overlay entry upstream now carries is deleted; the identical one
	// only counts in the note.
	overlay := section(summary, "Overlay")
	expectContains(t, overlay,
		"- `POST /v3/billing/top-up/`: upstream x-didit-sdk {\"auth\": \"api_key\", \"method\": \"buy_credits\", \"resource\": \"billing\", \"server\": true} differs from the overlay entry {\"auth\": \"api_key\", \"method\": \"top_up\", \"resource\": \"billing\", \"server\": true}; upstream wins and the entry was deleted; `billing.top_up` stays as a deprecated alias of `billing.buy_credits` until version 0.5.0.",
		"- `GET /v3/session/{session_id}/decision/`: upstream x-didit-sdk {\"auth\": \"account_token\", \"method\": \"retrieve_decision\", \"resource\": \"sessions\", \"server\": true} differs from the overlay entry {\"auth\": \"api_key\", \"method\": \"retrieve_decision\", \"resource\": \"sessions\", \"server\": true}; upstream wins and the entry was deleted.",
		"Dropped 3 overlay entries that upstream now carries.")
	if strings.Contains(overlay, "`POST /v3/session/`") {
		t.Error("the identical overlay entry is reported as different")
	}
	ops := f.node("overlay.json").Get("operations")
	for _, key := range []string{"POST /v3/session/", "POST /v3/billing/top-up/", "GET /v3/session/{session_id}/decision/"} {
		if ops.Has(key) {
			t.Errorf("overlay entry %s was kept", key)
		}
	}
	if !ops.Has("GET /v3/billing/balance/") || !ops.Has("account GET /organizations/") {
		t.Error("overlay entries upstream does not carry were deleted")
	}

	// The different name goes through the rename path; the different auth
	// is breaking too.
	expectContains(t, section(summary, "Breaking changes"),
		"`billing.top_up` was renamed upstream to `billing.buy_credits` (POST /v3/billing/top-up/); the old name is a deprecated alias until version 0.5.0",
		"`sessions.retrieve_decision` (GET /v3/session/{session_id}/decision/): x-didit-sdk auth changed from \"api_key\" to \"account_token\"")
	expectContains(t, section(summary, "Renamed methods (1)"),
		"- `POST /v3/billing/top-up/`: `billing.top_up` renamed to `billing.buy_credits`")
	methods := f.deprecated()
	if got := methods.ObjKeys(); !reflect.DeepEqual(got, []string{"billing.top_up"}) {
		t.Fatalf("deprecated methods = %v", got)
	}
	if e := methods.Get("billing.top_up"); e.Get("reason").Text() != "renamed" || e.Get("replacement").Text() != "billing.buy_credits" {
		t.Errorf("entry = %s", e.JSON())
	}
	billing := f.generate()["billing_gen.go"]
	expectContains(t, billing,
		"func (r *BillingService) BuyCredits(",
		"func (r *BillingService) TopUp(",
		"// Deprecated: Use [BillingService.BuyCredits] instead;")

	// The overlay keeps the seeded layout.
	raw := string(f.read("overlay.json"))
	if raw != syncEncode(mustNode(t, raw), syncFileStyle) || strings.HasSuffix(raw, "\n") {
		t.Error("overlay.json lost its layout")
	}
}

func TestSyncReportsChangedOperationsAndSchemas(t *testing.T) {
	f := newSyncFixture(t)
	old := f.node("verification.json")
	old.Path("components", "schemas").Set("LegacyUnusedThing", mustNode(t, `{"type": "object"}`))
	f.writeNode("verification.json", old)

	doc := f.node("verification.json")
	schemas := doc.Path("components", "schemas")
	syncDeleteKey(schemas, "LegacyUnusedThing")
	schemas.Set("Brand", mustNode(t, `{"type": "string"}`))
	schemas.Path("SessionCreate", "properties").Set("workflow_id", mustNode(t, `{"type": "integer"}`))
	schemas.Path("Session", "properties", "status").Set("enum", mustNode(t, `["Approved"]`))
	doc.Path("paths", "/v3/session/", "post").Set("parameters", mustNode(t, `[{"name": "dry_run", "in": "query", "required": true, "schema": {"type": "boolean"}}]`))
	doc.Path("paths", "/v3/billing/top-up/", "post").Set("description", mustNode(t, `"Now documented."`))
	summary := f.syncVerification(doc)

	breaking := section(summary, "Breaking changes")
	expectContains(t, breaking,
		"`POST /v3/session/`: parameter added: dry_run (query, required) (breaking); body.workflow_id: type string -> integer (breaking)",
		"verification.json: schema `LegacyUnusedThing` removed (its type leaves package didit)")
	changed := section(summary, "Changed operations (3)")
	expectContains(t, changed,
		"- `POST /v3/session/` **(breaking)**: parameter added: dry_run (query, required) (breaking); body.workflow_id: type string -> integer (breaking); response.status: values removed [\"Declined\"]",
		"- `GET /v3/session/{session_id}/decision/`: response.status: values removed [\"Declined\"]",
		"- `POST /v3/billing/top-up/`: descriptions, examples or other details changed")
	expectContains(t, section(summary, "Schemas"),
		"- verification.json added: `Brand`",
		"- verification.json removed: `LegacyUnusedThing`",
		"- verification.json changed: `Session`, `SessionCreate`")
}

func TestSyncRefusesDownloadsThatAreNotOpenAPI(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"html", "<html>maintenance</html>", "account.json: the download is not JSON"},
		{"maintenance JSON", `{"message": "maintenance"}`, "account.json: the download is not an OpenAPI 3 document (openapi=None)."},
		{"swagger 2", `{"swagger": "2.0", "openapi": 2, "paths": {"/x": {}}}`, "account.json: the download is not an OpenAPI 3 document (openapi=2)."},
		{"no paths", `{"openapi": "3.1.0", "paths": {}}`, "account.json: the download has no paths."},
		{"array", `[]`, "account.json: the download is not a JSON object."},
		{"invalid UTF-8", "{\"openapi\": \"3.1.0\", \"paths\": {\"/\xff\": {}}}", "account.json: the download is not JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSyncFixture(t)
			before := f.snapshot()
			changed := f.node("verification.json")
			syncDeleteKey(changed.Get("paths"), "/v3/billing/balance/")
			_, err := f.sync(map[string][]byte{
				"verification.json": []byte(syncEncode(changed, syncFileStyle)),
				"account.json":      []byte(tc.body),
			})
			var refusal *syncRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			expectContains(t, err.Error(), "nothing was written", tc.want)
			if !reflect.DeepEqual(before, f.snapshot()) {
				t.Error("files changed")
			}
		})
	}
}

func TestSyncWritesDownloadsAndSource(t *testing.T) {
	f := newSyncFixture(t)
	doc := f.node("verification.json")
	doc.Path("paths", "/v3/session/", "post").Set("description", mustNode(t, `"Créer une session <now> & more."`))
	download := []byte(syncEncode(doc, syncFileStyle) + "\n")
	account := f.read("account.json")
	res, err := f.sync(map[string][]byte{"verification.json": download, "account.json": account})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.summary, "Spec sync 2026-10-18. Upstream files changed: verification.json.\n\n## Breaking changes\nNone detected.\n") {
		t.Errorf("summary starts with %q", res.summary)
	}
	if !bytes.Equal(f.read("verification.json"), download) {
		t.Error("verification.json is not the download byte for byte")
	}
	raw := string(f.read("SOURCE.json"))
	if raw != syncEncode(mustNode(t, raw), syncFileStyle) || strings.HasSuffix(raw, "\n") {
		t.Errorf("SOURCE.json layout:\n%s", raw)
	}
	source := mustNode(t, raw)
	if got := source.ObjKeys(); !reflect.DeepEqual(got, []string{"verification.json", "account.json", "webhook-vectors.json"}) {
		t.Errorf("SOURCE.json keys = %v", got)
	}
	for name, data := range map[string][]byte{"verification.json": download, "account.json": account} {
		sum := sha256.Sum256(data)
		e := source.Get(name)
		if e.Get("sha256").Text() != hex.EncodeToString(sum[:]) || e.Get("fetched_at").Text() != "2026-10-18T06:00:00Z" {
			t.Errorf("SOURCE.json %s = %s", name, e.JSON())
		}
	}
	if source.Path("verification.json", "url").Text() != "https://docs.didit.me/openapi-25.json" {
		t.Error("verification.json url")
	}
	if source.Path("webhook-vectors.json", "generator").Text() != "fixture" {
		t.Error("the webhook-vectors.json entry was not kept")
	}
	// A deprecated-operations.json without methods still ends with a newline.
	if dep := string(f.read("deprecated-operations.json")); !strings.HasSuffix(dep, "}\n") {
		t.Errorf("deprecated-operations.json = %q", dep)
	}
}

func TestSyncCommand(t *testing.T) {
	f := newSyncFixture(t)
	out := filepath.Join(t.TempDir(), "github_output")
	summary := filepath.Join(t.TempDir(), "summary.md")
	var stdout bytes.Buffer
	cmd := syncCommand{
		dir: f.root, summary: summary, fromDir: filepath.Join(f.root, "openapi"),
		version: syncTestVersion, githubOutput: out, stdout: &stdout,
		now: func() time.Time { return syncTestNow },
		fetch: func(string) ([]byte, error) {
			t.Error("fetch called with -from-dir")
			return nil, errors.New("no")
		},
	}
	if err := cmd.run(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "changed=false\ndate=2026-10-18\n" {
		t.Errorf("GITHUB_OUTPUT = %q", got)
	}
	if got, _ := os.ReadFile(summary); string(got) != "No upstream change.\n" || stdout.String() != string(got) {
		t.Errorf("summary = %q, stdout %q", got, stdout.String())
	}

	// Downloading: an httptest server stands in for docs.didit.me.
	doc := f.node("verification.json")
	syncDeleteKey(doc.Get("paths"), "/v3/billing/balance/")
	served := map[string][]byte{
		"/openapi-25.json":   []byte(syncEncode(doc, syncFileStyle)),
		"/openapi-auth.json": f.read("account.json"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "didit-sdk-go spec-sync" {
			t.Errorf("User-Agent = %q", ua)
		}
		_, _ = w.Write(served[r.URL.Path])
	}))
	defer srv.Close()
	cmd.fromDir = ""
	cmd.fetch = func(url string) ([]byte, error) { return syncFetch(srv.Client(), srv.URL+"/"+path.Base(url)) }
	if err := cmd.run(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "changed=false\ndate=2026-10-18\nchanged=true\ndate=2026-10-18\n" {
		t.Errorf("GITHUB_OUTPUT = %q", got)
	}
	if got, _ := os.ReadFile(summary); !strings.Contains(string(got), "`billing.retrieve_balance` was removed upstream") {
		t.Errorf("summary = %s", got)
	}
	if !bytes.Equal(f.read("verification.json"), served["/openapi-25.json"]) {
		t.Error("verification.json is not the download")
	}

	// A failed download writes nothing.
	before := f.snapshot()
	cmd.fetch = func(url string) ([]byte, error) { return nil, errors.New("connection refused") }
	if err := cmd.run(); err == nil || !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("err = %v", err)
	}
	if !reflect.DeepEqual(before, f.snapshot()) {
		t.Error("files changed")
	}
}

func TestSyncFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/down" {
			http.Error(w, "<html>maintenance</html>", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"openapi": "3.1.0"}`))
	}))
	defer srv.Close()
	data, err := syncFetch(srv.Client(), srv.URL+"/openapi-25.json")
	if err != nil || string(data) != `{"openapi": "3.1.0"}` {
		t.Errorf("got %q, %v", data, err)
	}
	if _, err := syncFetch(srv.Client(), srv.URL+"/down"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v", err)
	}
}

func TestRunSyncRejectsBadArguments(t *testing.T) {
	if err := runSync([]string{"extra"}); err == nil {
		t.Error("extra argument accepted")
	}
	if err := runSync([]string{"-no-such-flag"}); err == nil {
		t.Error("unknown flag accepted")
	}
}

func TestDiffSchemaRules(t *testing.T) {
	doc := &Doc{Name: "verification", Root: mustNode(t, `{}`)}
	diff := func(old, new string, request bool) []syncNote {
		t.Helper()
		var notes []syncNote
		newSyncDiffer(doc, doc).diffSchema(mustNode(t, old), mustNode(t, new), "x", request, &notes, 0)
		return notes
	}
	expect := func(got []syncNote, want ...syncNote) {
		t.Helper()
		if len(got) == 0 && len(want) == 0 {
			return
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("notes = %v, want %v", got, want)
		}
	}
	status := `{"type": "string", "enum": ["a", "b"]}`
	expect(diff(status, `{"type": "string", "enum": ["a"]}`, true), syncNote{`x: values removed ["b"]`, true})
	expect(diff(status, `{"type": "string", "enum": ["a"]}`, false), syncNote{`x: values removed ["b"]`, false})
	expect(diff(status, `{"type": "string", "enum": ["a", "b", "ü"]}`, false), syncNote{`x: values added ["\u00fc"]`, false})
	obj := `{"type": "object", "properties": {"a": {"type": "string"}}, "required": ["a"]}`
	optional := `{"type": "object", "properties": {"a": {"type": "string"}}, "required": []}`
	expect(diff(obj, optional, false), syncNote{"x.a: no longer always present", true})
	expect(diff(optional, obj, true), syncNote{"x.a: now required", true})
	expect(diff(`{"type": "integer"}`, `{"type": "number"}`, true), syncNote{"x: type integer -> number", false})
	expect(diff(`{"type": "integer"}`, `{"type": "number"}`, false), syncNote{"x: type integer -> number", true})
	expect(diff(`{"type": "string"}`, `{"type": "string", "nullable": true}`, false), syncNote{"x: can now be null", true})
	expect(diff(`{"type": ["string", "null"]}`, `{"type": "string"}`, true), syncNote{"x: no longer accepts null", true})
	expect(diff(`{"type": "array", "items": `+obj+`}`, `{"type": "array", "items": {"type": "object", "properties": {}}}`, false),
		syncNote{"x[].a: removed", true})
	expect(diff(`{"oneOf": [`+obj+`, `+status+`]}`, `{"oneOf": [`+obj+`]}`, false), syncNote{"x: 1 oneOf variant(s) removed", true})
	expect(diff(`{"type": "object", "properties": {}}`, `{"type": "object", "properties": {"b": {"type": "string"}}, "required": ["b"]}`, true),
		syncNote{"x.b: added (required)", true})
	documented := `{"type": "object", "properties": {"a": {"type": "string"}}, "required": ["a"], "description": "now documented", "example": {"a": "z"}, "x-internal": 1}`
	expect(diff(obj, documented, false))
	// A property called like a documentation keyword still counts (the
	// Python reference misses this one).
	expect(diff(`{"type": "object", "properties": {"title": {"type": "string"}}}`, `{"type": "object", "properties": {"title": {"type": "integer"}}}`, false),
		syncNote{"x.title: type string -> integer", true})
}

func TestDiffFollowsRecursiveReferences(t *testing.T) {
	old := &Doc{Name: "verification", Root: mustNode(t, `{"components": {"schemas": {"Node": {"type": "object", "properties": {"name": {"type": "string"}, "children": {"type": "array", "items": {"$ref": "#/components/schemas/Node"}}}}}}}`)}
	new := &Doc{Name: "verification", Root: mustNode(t, `{"components": {"schemas": {"Node": {"type": "object", "properties": {"name": {"type": "integer"}, "children": {"type": "array", "items": {"$ref": "#/components/schemas/Node"}}}}}}}`)}
	ref := mustNode(t, `{"$ref": "#/components/schemas/Node"}`)
	if !newSyncDiffer(old, old).sameShape(ref, ref, 0) {
		t.Error("a recursive schema differs from itself")
	}
	var notes []syncNote
	newSyncDiffer(old, new).diffSchema(ref, ref, "response", false, &notes, 0)
	texts := map[string]bool{}
	for _, n := range notes {
		texts[n.text] = true
	}
	// Reported at every level down to the depth limit, and no further.
	if !texts["response.name: type string -> integer"] || !texts["response.children[].name: type string -> integer"] || len(notes) > 20 {
		t.Errorf("notes = %v", notes)
	}
}

func TestDescribeChangeCapsNotes(t *testing.T) {
	doc := &Doc{Name: "verification", Root: mustNode(t, `{}`)}
	var props, params []string
	for i := 0; i < 30; i++ {
		props = append(props, `"p`+string(rune('a'+i%26))+strings.Repeat("x", i/26)+`": {"type": "string"}`)
		params = append(params, `{"name": "q`+strings.Repeat("x", i)+`", "in": "query", "required": true, "schema": {"type": "string"}}`)
	}
	old := mustNode(t, `{"responses": {"200": {"content": {"application/json": {"schema": {"type": "object", "properties": {`+strings.Join(props, ",")+`}}}}}}}`)
	new := mustNode(t, `{"parameters": [`+strings.Join(params, ",")+`], "responses": {"200": {"content": {"application/json": {"schema": {"type": "object"}}}}}}`)
	notes, breaking := newSyncDiffer(doc, doc).describeChange(old, new)
	if !breaking || len(notes) != syncMaxNotes+1 || notes[syncMaxNotes] != "and 35 more differences" {
		t.Errorf("breaking=%t, %d notes, last %q", breaking, len(notes), notes[len(notes)-1])
	}
}

func TestSyncNextMinor(t *testing.T) {
	for in, want := range map[string]string{"0.1.0": "0.2.0", "1.9.3": "1.10.0", "0.4.2-rc.1": "0.5.0", "garbage": "0.1.0"} {
		if got := syncNextMinor(in); got != want {
			t.Errorf("syncNextMinor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSyncCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1}, {"0.2.0", "0.2.0", 0}, {"0.10.0", "0.9.9", 1}, {"1.0.0", "0.99.0", 1},
		{"0.2.0-rc.1", "0.2.0", -1}, {"0.2.0", "0.2.0-rc.1", 1}, {"0.2.0-alpha", "0.2.0-alpha.1", -1},
		{"0.2.0-alpha.1", "0.2.0-alpha.beta", -1}, {"0.2.0-beta.2", "0.2.0-beta.11", -1},
		{"0.2.0-rc.1", "0.2.0-beta.11", 1}, {"0.2.0+build.5", "0.2.0", 0}, {"v0.3.0", "0.3.0", 0},
	} {
		got, err := syncCompareVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("syncCompareVersions(%q, %q) = %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "1.2", "1.2.3.4", "01.2.3", "next"} {
		if _, err := syncCompareVersions(bad, "1.0.0"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSyncJSONLayout(t *testing.T) {
	// The seeded files round-trip byte for byte (Python's
	// json.dumps(indent=1, ensure_ascii=False) layout).
	for _, name := range []string{"overlay.json", "SOURCE.json", "deprecated-operations.json"} {
		data, err := os.ReadFile(filepath.Join("..", "openapi", name))
		if err != nil {
			t.Fatal(err)
		}
		n, err := ParseNode(data)
		if err != nil {
			t.Fatal(err)
		}
		got := syncEncode(n, syncFileStyle)
		if name == "deprecated-operations.json" {
			got += "\n"
		}
		if got != string(data) {
			t.Errorf("%s does not round-trip", name)
		}
	}
	n := mustNode(t, `{"s": "é <a> & \u2028 \u0001 \"q\" \\ \n", "e": {}, "a": [], "n": [1, 2.50, true, null], "emoji": "😀"}`)
	if got, want := syncEncode(n, syncFileStyle), "{\n \"s\": \"é <a> & \u2028 \\u0001 \\\"q\\\" \\\\ \\n\",\n \"e\": {},\n \"a\": [],\n \"n\": [\n  1,\n  2.50,\n  true,\n  null\n ],\n \"emoji\": \"😀\"\n}"; got != want {
		t.Errorf("file style:\n%s\nwant\n%s", got, want)
	}
	if got, want := syncEncode(n, syncSortedInlineStyle), `{"a": [], "e": {}, "emoji": "\ud83d\ude00", "n": [1, 2.50, true, null], "s": "\u00e9 <a> & \u2028 \u0001 \"q\" \\ \n"}`; got != want {
		t.Errorf("inline style:\n%s\nwant\n%s", got, want)
	}
}

func TestSyncEqual(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{`{"a": 1, "b": [1, 2]}`, `{"b": [1, 2], "a": 1.0}`, true},
		{`{"a": 1}`, `{"a": 1, "b": null}`, false},
		{`[1, 2]`, `[2, 1]`, false},
		{`"1"`, `1`, false},
		{`1e2`, `100`, true},
	} {
		if got := syncEqual(mustNode(t, tc.a), mustNode(t, tc.b)); got != tc.want {
			t.Errorf("syncEqual(%s, %s) = %t", tc.a, tc.b, got)
		}
	}
}
