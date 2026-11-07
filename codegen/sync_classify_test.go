package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/didit-protocol/sdk-go/internal/version"
)

// Spec sync classification (docs/DESIGN.md sections 3.1 and 13): the pinned
// upstream pairs every server SDK asserts, probes of the changes that are
// easy to miss, and the release block that carries the result to
// release-please.

// syncPinned are copies of sdk-python's openapi/verification.json at these
// commits, gzipped in testdata/spec-sync, with the SHA-256 of the original.
var syncPinned = map[string]string{
	// The API change of 2026-11-04 (tag v1.0.1, then the sync after it).
	"9fe57fc69299601e59cc7a64c1db370fea5c7461": "12b2fdb05572755d61b93791f6964b7d883ba7b20f7ea9b31543903b6edab8a9",
	"e002a815773bbd1599256e2774046ee76520c6c8": "d1783bd862848198f7e5275c28f6a1bcdd59737c0d92f72bba3f6f3cf8b8ae3e",
	// A documentation-only change of 2026-11-06 (POST /v3/poa/ descriptions).
	"fe9f57a1dfb6374bfd391750d2044fc7a206e4ea": "9f8cdf8be8157588facc8059c9bd306f3f516a3a5b66b78c8e63cf595ddb6627",
	"ccc799410f82eee1c25c7ce5abc6caa53846be6b": "1b0c7bdd8d7957bdf314651ebd7ecc491457810dd7a1430bce84feac9caea305",
}

// syncPinnedDir holds the pinned documents. They are in this repository but
// not in the module users download (the directory has its own go.mod).
var syncPinnedDir = filepath.Join("testdata", "spec-sync")

// skipWithoutPinned skips a test outside this repository, where the pinned
// documents are missing.
func skipWithoutPinned(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(syncPinnedDir, "go.mod")); errors.Is(err, fs.ErrNotExist) {
		t.Skip("the spec sync fixtures are not part of the downloaded module")
	}
}

// readPinned returns the upstream document of commit, checked against its
// SHA-256.
func readPinned(t *testing.T, commit string) []byte {
	t.Helper()
	skipWithoutPinned(t)
	f, err := os.Open(filepath.Join(syncPinnedDir, "verification-"+commit+".json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != syncPinned[commit] {
		t.Fatalf("verification-%s.json has SHA-256 %s, want %s", commit, got, syncPinned[commit])
	}
	return data
}

// syncPinnedPair syncs from before to after, with this repository's
// account.json, authentication.json and overlay.json.
func syncPinnedPair(t *testing.T, before, after []byte) syncResult {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"account.json", "authentication.json", "overlay.json", "SOURCE.json"} {
		data, err := os.ReadFile(filepath.Join("..", "openapi", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deprecated := syncObject()
	deprecated.Set("description", syncString(syncDeprecatedDescription))
	deprecated.Set("methods", syncObject())
	files := map[string][]byte{
		"verification.json":          before,
		"deprecated-operations.json": []byte(syncEncode(deprecated, syncFileStyle) + "\n"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := syncSpecs(dir, map[string][]byte{"verification.json": after}, syncTestNow, version.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !res.changed {
		t.Fatal("changed = false")
	}
	return res
}

// syncEntry is a breaking change as the issue lists it: operation, field and
// a short kind.
type syncEntry struct{ op, field, kind string }

func syncEntries(t *testing.T, res syncResult) []syncEntry {
	t.Helper()
	short := map[string]string{
		"can now be null":                    "now-null",
		"type changed from string to object": "string->object",
	}
	var out []syncEntry
	for _, b := range res.breaking {
		if b.change.where != "response" {
			t.Errorf("%s: where = %q, want response", b.text(), b.change.where)
		}
		kind := short[b.change.kind]
		if kind == "" {
			kind = b.change.kind
		}
		out = append(out, syncEntry{b.operation, b.change.path, kind})
	}
	return out
}

func TestSyncPinnedAPIChangeIsAMinorReleaseWithItsBreakingChanges(t *testing.T) {
	res := syncPinnedPair(t,
		readPinned(t, "9fe57fc69299601e59cc7a64c1db370fea5c7461"),
		readPinned(t, "e002a815773bbd1599256e2774046ee76520c6c8"))
	if res.typ != "feat" {
		t.Errorf("type = %q, want feat", res.typ)
	}
	// The list the issue gives (sdk-python PR #14 printed the same): it
	// announces 15 changes and names these 14.
	decision := "GET /v3/session/{sessionId}/decision/"
	imported := "POST /v3/session/import-shared/"
	want := []syncEntry{
		{decision, "registry_checks[].company.registry_data", "now-null"},
		{decision, "registry_checks[].ownership_structure", "now-null"},
		{decision, "signatures[].confirmation.channel", "now-null"},
		{"POST /v3/email/risk/", "email.email_intelligence.reason_codes[]", "string->object"},
		{"POST /v3/email/risk/", "email.enrichment.reason_codes[]", "string->object"},
		{"POST /v3/kyb/select/", "kyb_registry.registry_data", "now-null"},
		{"POST /v3/kyb/shareholders/", "kyb_registry.registry_data", "now-null"},
		{"POST /v3/kyb/ubo/", "kyb_registry.registry_data", "now-null"},
		{"POST /v3/phone/risk/", "phone.enrichment.reason_codes[]", "string->object"},
		{"POST /v3/phone/risk/", "phone.phone_intelligence.reason_codes[]", "string->object"},
		{imported, "registry_checks[].company.registry_data", "now-null"},
		{imported, "registry_checks[].ownership_structure", "now-null"},
		{imported, "signatures[].confirmation.channel", "now-null"},
		{"POST /v3/session/{sessionId}/signatures/{nodeId}/validate/", "signatures[].confirmation.channel", "now-null"},
	}
	if got := syncEntries(t, res); !reflect.DeepEqual(got, want) {
		t.Errorf("breaking changes (%d):\n%s\nwant (%d):\n%s", len(got), fmt.Sprint(got), len(want), fmt.Sprint(want))
	}
	expectBreaking(t, res, breakingTexts(res)...)
	expectContains(t, res.summary,
		"## Breaking changes\n- `GET /v3/session/{sessionId}/decision/` response `registry_checks[].company.registry_data`: can now be null\n",
		"- `POST /v3/email/risk/` response `email.email_intelligence.reason_codes[]`: type changed from string to object\n",
		"`feat(spec)`: callers can observe what changed, so this is a minor release",
		"## Added operations (14)\n")

	// The release block: one feat line (a minor version, never "!" or a
	// BREAKING CHANGE footer), then one line per breaking change.
	block := syncReleaseBlock(res.typ, "2026-11-04", res.breaking)
	expectContains(t, block, "BEGIN_COMMIT_OVERRIDE\nfeat(spec): sync API spec 2026-11-04\n\nBEGIN_NESTED_COMMIT\n"+
		"api(spec): `GET /v3/session/{sessionId}/decision/` response `registry_checks[].company.registry_data`: can now be null\nEND_NESTED_COMMIT\n")
	if n := strings.Count(block, "\napi(spec): "); n != len(want) {
		t.Errorf("%d release lines, want %d", n, len(want))
	}
	if strings.Contains(block, "feat(spec)!") || strings.Contains(block, "BREAKING CHANGE") || strings.ContainsAny(block, "<>") {
		t.Errorf("release block is not a minor release:\n%s", block)
	}
}

func TestSyncPinnedDocumentationChangeReleasesNothing(t *testing.T) {
	res := syncPinnedPair(t,
		readPinned(t, "fe9f57a1dfb6374bfd391750d2044fc7a206e4ea"),
		readPinned(t, "ccc799410f82eee1c25c7ce5abc6caa53846be6b"))
	if res.typ != "chore" {
		t.Errorf("type = %q, want chore", res.typ)
	}
	expectBreaking(t, res)
	if got := section(res.summary, "Changed operations (1)"); got != "- `POST /v3/poa/`: descriptions, examples or other details changed\n" {
		t.Errorf("Changed operations:\n%s", got)
	}
	block := syncReleaseBlock(res.typ, "2026-11-06", res.breaking)
	expectContains(t, block, "BEGIN_COMMIT_OVERRIDE\nchore(spec): sync API spec 2026-11-06\nEND_COMMIT_OVERRIDE\n",
		"`chore(spec)`: only documentation changed, so this releases nothing on its own")
	if strings.Contains(block, "api(spec)") || strings.Contains(block, "feat(") {
		t.Errorf("release block:\n%s", block)
	}
}

// probePaths are the operations the probes change; the probe document keeps
// only them (and every component), so that each probe parses 1 MB, not 6.
var probePaths = []string{"/v3/database-validation/", "/v3/webhook/destinations/", "/v3/session/imports/"}

var (
	probeOnce sync.Once
	probeBase string
)

// probeAfter syncs the 2026-11-04 "after" document (probePaths only),
// changed by setup (when set), against a copy changed by setup and edit.
func probeAfter(t *testing.T, setup, edit func(doc *Node)) syncResult {
	t.Helper()
	skipWithoutPinned(t)
	probeOnce.Do(func() {
		full := mustNode(t, string(readPinned(t, "e002a815773bbd1599256e2774046ee76520c6c8")))
		paths := syncObject()
		for _, p := range probePaths {
			paths.Set(p, full.Path("paths", p))
		}
		trimmed := syncCopyObject(full)
		trimmed.Set("paths", paths)
		probeBase = syncEncode(trimmed, syncFileStyle)
	})
	if probeBase == "" {
		t.Fatal("the probe document could not be built")
	}
	old, doc := mustNode(t, probeBase), mustNode(t, probeBase)
	if setup != nil {
		setup(old)
		setup(doc)
	}
	edit(doc)
	return syncPinnedPair(t, []byte(syncEncode(old, syncFileStyle)), []byte(syncEncode(doc, syncFileStyle)))
}

// Changes that a sync must report although something next to them changed
// too: the review of other SDKs found each of them missed once.
func TestSyncReportsChangesInsideUnionsAndWrappers(t *testing.T) {
	validations := func(doc *Node) *Node {
		return doc.Path("paths", "/v3/database-validation/", "post", "responses", "200", "content", "application/json",
			"schema", "properties", "database_validation", "properties", "validations")
	}
	const op = "`POST /v3/database-validation/` response "
	note := func(t *testing.T, doc *Node) {
		v := validations(doc).Get("oneOf")
		v.Arr = append(v.Arr, mustNode(t, `{"type": "object", "properties": {"note": {"type": "string"}}}`))
	}
	tests := []struct {
		name  string
		setup func(t *testing.T, doc *Node) // both documents
		edit  func(t *testing.T, doc *Node) // the new one
		want  []string
		other []string // listed, not breaking
	}{{
		name: "a retained variant changes while a variant of another type is added",
		edit: func(t *testing.T, doc *Node) {
			v := validations(doc).Get("oneOf")
			v.Arr[0].Path("items", "properties", "service_id").Set("type", syncString("integer"))
			v.Arr = append(v.Arr, mustNode(t, `{"type": "string"}`))
		},
		want: []string{
			op + "`database_validation.validations`: type changed from array or object to array or object or string",
			op + "`database_validation.validations(oneOf 1)[].service_id`: type changed from string to integer",
		},
		other: []string{"response `database_validation.validations`: 1 oneOf variant(s) added"},
	}, {
		name: "a retained variant changes while a variant of the same type is added",
		edit: func(t *testing.T, doc *Node) {
			v := validations(doc).Get("oneOf")
			v.Arr[0].Path("items", "properties", "service_id").Set("type", syncString("integer"))
			v.Arr = append(v.Arr, mustNode(t, `{"type": "object", "properties": {"note": {"type": "string"}}}`))
		},
		want:  []string{op + "`database_validation.validations(oneOf 1)[].service_id`: type changed from string to integer"},
		other: []string{"response `database_validation.validations`: 1 oneOf variant(s) added"},
	}, {
		name: "a field of a variant can now be null",
		edit: func(t *testing.T, doc *Node) {
			validations(doc).Get("oneOf").Arr[0].Path("items", "properties", "service_id").Set("nullable", &Node{Kind: Bool, Bool: true})
		},
		want: []string{op + "`database_validation.validations(oneOf 1)[].service_id`: can now be null"},
	}, {
		name: "an enum value of a variant is removed",
		edit: func(t *testing.T, doc *Node) {
			validations(doc).Get("oneOf").Arr[1].Path("additionalProperties").Set("enum", mustNode(t, `["full_match", "no_match"]`))
		},
		want: []string{op + "`database_validation.validations(oneOf 2){}`: enum value \"partial_match\" removed"},
	}, {
		name: "a variant is removed",
		edit: func(t *testing.T, doc *Node) {
			v := validations(doc).Get("oneOf")
			v.Arr = v.Arr[:1]
		},
		want: []string{op + "`database_validation.validations`: type changed from array or object to array"},
	}, {
		name:  "a variant of a type that is already there is added",
		edit:  note,
		other: []string{"response `database_validation.validations`: 1 oneOf variant(s) added"},
	}, {
		name:  "a variant of a type that stays is removed",
		setup: note,
		edit: func(t *testing.T, doc *Node) {
			v := validations(doc).Get("oneOf")
			v.Arr = v.Arr[:2]
		},
		want: []string{op + "`database_validation.validations`: type changed: 1 oneOf variant(s) removed"},
	}, {
		name: "the items of an array behind an allOf change",
		edit: func(t *testing.T, doc *Node) {
			// POST /v3/webhook/destinations/ sends subscribed_events as
			// {"allOf": [{"$ref": WebhookSubscribedEvents}]}.
			events := doc.Path("components", "schemas", "WebhookSubscribedEvents")
			if events == nil || events.Get("items") == nil {
				t.Fatal("WebhookSubscribedEvents is not an array schema any more; pick another probe")
			}
			events.Set("items", mustNode(t, `{"type": "integer"}`))
		},
		want: []string{"`POST /v3/webhook/destinations/` request body `subscribed_events[]`: type changed from string to integer"},
	}, {
		name: "a request field can now be null",
		edit: func(t *testing.T, doc *Node) {
			doc.Path("paths", "/v3/database-validation/", "post", "requestBody", "content", "application/json", "schema", "properties", "issuing_state").
				Set("nullable", &Node{Kind: Bool, Bool: true})
		},
		want: []string{"`POST /v3/database-validation/` request body `issuing_state`: can now be null"},
	}, {
		name: "a media type goes with a field only it carried",
		setup: func(t *testing.T, doc *Node) {
			// POST /v3/session/imports/ takes multipart/form-data; give it a JSON
			// body without the file.
			content := doc.Path("paths", "/v3/session/imports/", "post", "requestBody", "content")
			body := mustNode(t, syncEncode(content.Get("multipart/form-data"), syncFileStyle))
			syncDeleteKey(body.Path("schema", "properties"), "file")
			content.Set("application/json", body)
		},
		edit: func(t *testing.T, doc *Node) {
			syncDeleteKey(doc.Path("paths", "/v3/session/imports/", "post", "requestBody", "content"), "multipart/form-data")
		},
		want:  []string{"`POST /v3/session/imports/` request body `file`: removed"},
		other: []string{"request body: content type application/json, multipart/form-data -> application/json"},
	}, {
		name: "a success status without a body",
		edit: func(t *testing.T, doc *Node) {
			doc.Path("paths", "/v3/database-validation/", "post", "responses").Set("204", mustNode(t, `{"description": "Nothing to validate."}`))
		},
		want: []string{"`POST /v3/database-validation/` response: can now be null (status 204 has no body)"},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var setup func(doc *Node)
			if tt.setup != nil {
				setup = func(doc *Node) { tt.setup(t, doc) }
			}
			res := probeAfter(t, setup, func(doc *Node) { tt.edit(t, doc) })
			got := breakingTexts(res)
			for _, w := range tt.want {
				if !strings.Contains(strings.Join(got, "\n"), w) {
					t.Errorf("missing breaking change %q in:\n%s", w, strings.Join(got, "\n"))
				}
			}
			if len(tt.want) == 0 && len(got) > 0 {
				t.Errorf("breaking changes: %q", got)
			}
			expectContains(t, res.summary, tt.other...)
			if res.typ != "feat" {
				t.Errorf("type = %q, want feat", res.typ)
			}
		})
	}
}

func TestSyncClassifiesWhatCallersCanObserve(t *testing.T) {
	tests := []struct {
		name string
		edit func(t *testing.T, doc *Node)
		typ  string
		note string // in the summary
	}{
		{"documentation only", func(t *testing.T, doc *Node) {
			doc.Path("paths", "/v3/session/", "post").Set("summary", syncString("Create a verification session"))
			doc.Path("components", "schemas", "Session", "properties", "status").Set("description", syncString("The status."))
		}, "chore", ""},
		{"a schema no operation uses", func(t *testing.T, doc *Node) {
			doc.Path("components", "schemas").Set("Unused", mustNode(t, `{"type": "object"}`))
		}, "feat", "- verification.json added: `Unused`"},
		{"an error response of a server operation", func(t *testing.T, doc *Node) {
			doc.Path("components", "schemas", "Problem", "properties").Set("code", mustNode(t, `{"type": "string"}`))
		}, "feat", "- `GET /v3/billing/balance/`: " + syncOtherNote},
		{"a limit of a request field", func(t *testing.T, doc *Node) {
			doc.Path("paths", "/v3/billing/top-up/", "post", "requestBody", "content", "application/json", "schema", "properties", "amount").
				Set("maximum", mustNode(t, `1000`))
		}, "feat", "- `POST /v3/billing/top-up/`: request body `amount`: other constraints changed (format, pattern, limits or default)"},
		{"a removed parameter", func(t *testing.T, doc *Node) {
			syncDeleteKey(doc.Path("paths", "/v3/session/{session_id}/decision/", "get"), "parameters")
		}, "feat", "- `GET /v3/session/{session_id}/decision/`: parameter `verbose` (query): removed"},
		{"a new request content type", func(t *testing.T, doc *Node) {
			content := doc.Path("paths", "/v3/billing/top-up/", "post", "requestBody", "content")
			content.Set("multipart/form-data", content.Get("application/json"))
		}, "feat", "- `POST /v3/billing/top-up/`: request body: content type application/json -> application/json, multipart/form-data"},
		{"marked deprecated", func(t *testing.T, doc *Node) {
			doc.Path("paths", "/v3/session/", "post").Set("deprecated", &Node{Kind: Bool, Bool: true})
		}, "feat", "- `POST /v3/session/`: operation: marked deprecated upstream"},
		{"another success status", func(t *testing.T, doc *Node) {
			responses := doc.Path("paths", "/v3/session/", "post", "responses")
			responses.Set("200", responses.Get("201"))
			syncDeleteKey(responses, "201")
		}, "feat", "- `POST /v3/session/`: response: status 201 -> 200"},
		{"an operation server SDKs do not expose", func(t *testing.T, doc *Node) {
			doc.Get("paths").Set("/v3/device/", mustNode(t, `{"post": {"operationId": "device_ping",
				"x-didit-sdk": {"resource": "device", "method": "ping", "auth": "device_token", "server": false},
				"responses": {"200": {"description": "ok"}}}}`))
		}, "feat", "- `POST /v3/device/` (not exposed by server SDKs: device_token)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			doc := f.node("verification.json")
			tt.edit(t, doc)
			res := f.syncDoc("verification.json", doc)
			if res.typ != tt.typ {
				t.Errorf("type = %q, want %q\n%s", res.typ, tt.typ, res.summary)
			}
			expectBreaking(t, res)
			if tt.note != "" {
				expectContains(t, res.summary, tt.note)
			}
		})
	}

	// An operation server SDKs do not expose is compared like any other, so
	// that every SDK lists the same changes.
	f := newSyncFixture(t)
	old := f.node("verification.json")
	old.Get("paths").Set("/v3/device/", mustNode(t, `{"post": {"operationId": "device_ping",
		"x-didit-sdk": {"resource": "device", "method": "ping", "auth": "device_token", "server": false},
		"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "properties": {"ok": {"type": "boolean"}}}}}}}}}`))
	f.writeNode("verification.json", old)
	doc := f.node("verification.json")
	doc.Path("paths", "/v3/device/", "post", "responses", "200", "content", "application/json", "schema", "properties").Set("ok", mustNode(t, `{"type": "string"}`))
	res := f.syncDoc("verification.json", doc)
	if res.typ != "feat" {
		t.Errorf("type = %q, want feat", res.typ)
	}
	expectBreaking(t, res, "`POST /v3/device/` response `ok`: type changed from boolean to string")
	expectContains(t, res.summary, "- `POST /v3/device/` (not exposed by server SDKs) **(breaking)**: response `ok`: type changed from boolean to string (breaking)")
}

func TestSyncAuthenticationDocument(t *testing.T) {
	t.Run("a new server operation is a feature for the hand-written module", func(t *testing.T) {
		f := newSyncFixture(t)
		doc := f.node("authentication.json")
		doc.Get("paths").Set("/step-up/", mustNode(t, `{"post": {"operationId": "authentication_create_step_up",
			"x-didit-sdk": {"resource": "authentication", "method": "create_step_up", "auth": "access_token", "server": true},
			"responses": {"201": {"description": "ok"}}}}`))
		before := f.generate()
		res := f.syncDoc("authentication.json", doc)
		if res.typ != "feat" {
			t.Errorf("type = %q, want feat", res.typ)
		}
		expectBreaking(t, res)
		expectContains(t, section(res.summary, "Added operations (1)"),
			"- `authentication POST /step-up/` -> `authentication.create_step_up`: hand-written; add it to the `authentication` module (its contract test fails until then)")
		expectContains(t, res.summary, "Upstream files changed: authentication.json.")
		if !bytes.Equal(f.read("authentication.json"), []byte(syncEncode(doc, syncFileStyle))) {
			t.Error("authentication.json is not the download")
		}
		source := f.node("SOURCE.json")
		if got := source.Path("authentication.json", "url").Text(); got != "https://docs.didit.me/openapi-authentication.json" {
			t.Errorf("SOURCE.json authentication.json url = %q", got)
		}
		// The generator never reads it.
		expectSameGenerated(t, before, f.generate())
	})

	t.Run("a removed server operation is breaking and never deprecated", func(t *testing.T) {
		f := newSyncFixture(t)
		doc := f.node("authentication.json")
		syncDeleteKey(doc.Path("paths", "/session/"), "delete")
		res := f.syncDoc("authentication.json", doc)
		expectBreaking(t, res, "`authentication DELETE /session/`: operation removed (`authentication.revoke_session`)")
		expectContains(t, section(res.summary, "Removed operations (1)"),
			"- `authentication DELETE /session/` (`authentication.revoke_session`): hand-written; remove it from the `authentication` module (its contract test fails until then)")
		if got := f.deprecated().ObjKeys(); len(got) != 0 {
			t.Errorf("deprecated methods = %v", got)
		}
	})

	t.Run("a renamed method is listed and never deprecated", func(t *testing.T) {
		f := newSyncFixture(t)
		doc := f.node("authentication.json")
		doc.Path("paths", "/session/", "get").Set("x-didit-sdk", mustNode(t, `{"resource": "authentication", "method": "retrieve_session", "auth": "access_token", "server": true}`))
		res := f.syncDoc("authentication.json", doc)
		expectBreaking(t, res)
		expectContains(t, section(res.summary, "Renamed methods (1)"),
			"- `authentication GET /session/`: `authentication.get_session` renamed to `authentication.retrieve_session`; hand-written: rename it in the `authentication` module")
		if got := f.deprecated().ObjKeys(); len(got) != 0 {
			t.Errorf("deprecated methods = %v", got)
		}
	})

	t.Run("a response change is compared like any other", func(t *testing.T) {
		f := newSyncFixture(t)
		doc := f.node("authentication.json")
		doc.Path("components", "schemas", "Session", "properties").Set("expires_at", mustNode(t, `{"type": "string"}`))
		res := f.syncDoc("authentication.json", doc)
		expectBreaking(t, res, "`authentication GET /session/` response `expires_at`: type changed from string (date-time) to string")
	})

	t.Run("documentation only releases nothing", func(t *testing.T) {
		f := newSyncFixture(t)
		doc := f.node("authentication.json")
		doc.Path("paths", "/monitoring-token/", "post").Set("description", syncString("Creates a monitoring token."))
		res := f.syncDoc("authentication.json", doc)
		if res.typ != "chore" {
			t.Errorf("type = %q, want chore", res.typ)
		}
		expectBreaking(t, res)
	})
}

func TestSyncReleaseBlock(t *testing.T) {
	entry := func(i int) syncBreaking {
		return syncBreaking{operation: "GET /v3/x/", change: syncChange{where: "response", path: fmt.Sprintf("f%04d", i), kind: "can now be null", breaking: true}}
	}
	t.Run("feature without breaking changes", func(t *testing.T) {
		block := syncReleaseBlock("feat", "2026-11-07", nil)
		expectContains(t, block, "## Release notes\n\n`feat(spec)`: callers can observe what changed, so this is a minor release, never a major one (docs/DESIGN.md section 13).\n\n",
			"merge with **Squash and merge**",
			"BEGIN_COMMIT_OVERRIDE\nfeat(spec): sync API spec 2026-11-07\nEND_COMMIT_OVERRIDE\n")
		if strings.Contains(block, "API changes that can break typed code") {
			t.Error("a sync without breaking changes mentions the section")
		}
	})
	t.Run("every breaking change gets a line", func(t *testing.T) {
		var list []syncBreaking
		for i := 0; i < 300; i++ {
			list = append(list, entry(i))
		}
		block := syncReleaseBlock("feat", "2026-11-07", list)
		if n := strings.Count(block, "BEGIN_NESTED_COMMIT\napi(spec): "); n != len(list) {
			t.Errorf("%d nested commits, want %d", n, len(list))
		}
		expectContains(t, block, "api(spec): `GET /v3/x/` response `f0299`: can now be null\nEND_NESTED_COMMIT\nEND_COMMIT_OVERRIDE\n")
		if message := syncCommitMessage("feat", "2026-11-07", list); !strings.Contains(block, "BEGIN_COMMIT_OVERRIDE\n"+message+"END_COMMIT_OVERRIDE\n") {
			t.Error("the block does not carry the sync commit's message")
		}
	})
	t.Run("a block that does not fit the description", func(t *testing.T) {
		var list []syncBreaking
		for i := 0; i < 1000; i++ {
			list = append(list, entry(i))
		}
		block := syncReleaseBlock("feat", "2026-11-07", list)
		// release-please takes any occurrence of the marker as the start of a
		// block, so the prose never names it.
		if strings.Contains(block, "COMMIT_OVERRIDE") || strings.Contains(block, "api(spec)") || len(block) > syncMaxRelease {
			t.Errorf("block (%d bytes):\n%s", len(block), block)
		}
		expectContains(t, block, "The 1000 lines for \"API changes that can break typed code\" do not fit this description",
			"use the message of this branch's sync commit as the commit message")
		message := syncCommitMessage("feat", "2026-11-07", list)
		if n := strings.Count(message, "BEGIN_NESTED_COMMIT\napi(spec): "); n != len(list) || !strings.HasPrefix(message, "feat(spec): sync API spec 2026-11-07\n\n") {
			t.Errorf("commit message has %d lines:\n%.300s", n, message)
		}
	})
	t.Run("a line never ends the block", func(t *testing.T) {
		b := entry(0)
		b.change.kind = "enum value \"END_COMMIT_OVERRIDE\nx\" removed"
		block := syncReleaseBlock("feat", "2026-11-07", []syncBreaking{b})
		if strings.Count(block, "END_COMMIT_OVERRIDE") != 1 || !strings.Contains(block, "end_commit_override x") {
			t.Errorf("block:\n%s", block)
		}
	})
	t.Run("documentation only", func(t *testing.T) {
		if got := syncCommitMessage("chore", "2026-11-07", nil); got != "chore(spec): sync API spec 2026-11-07\n" {
			t.Errorf("commit message = %q", got)
		}
	})
}

// release-please-config.json shows the release lines under their heading and
// hides what releases nothing (docs/DESIGN.md section 13).
func TestReleasePleaseConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "release-please-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Packages map[string]struct {
			ReleaseType string `json:"release-type"`
			Sections    []struct {
				Type    string `json:"type"`
				Section string `json:"section"`
				Hidden  bool   `json:"hidden"`
			} `json:"changelog-sections"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	pkg := cfg.Packages["."]
	if pkg.ReleaseType != "go" {
		t.Errorf("release-type = %q", pkg.ReleaseType)
	}
	visible := map[string]string{}
	hidden := map[string]bool{}
	for _, s := range pkg.Sections {
		if s.Hidden {
			hidden[s.Type] = true
		} else {
			visible[s.Type] = s.Section
		}
	}
	for typ, section := range map[string]string{"feat": "Features", syncReleaseType: syncReleaseHeading, "fix": "Bug Fixes"} {
		if visible[typ] != section {
			t.Errorf("%s commits go to %q, want %q", typ, visible[typ], section)
		}
	}
	// A documentation-only sync (chore) and changes like this one release
	// nothing on their own.
	for _, typ := range []string{"chore", "docs", "test", "ci", "build", "style", "refactor"} {
		if !hidden[typ] {
			t.Errorf("%s commits are not hidden: they would release a version", typ)
		}
	}
}
