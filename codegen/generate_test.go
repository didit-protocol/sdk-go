package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateMatchesRepository regenerates from openapi/ and compares with
// the committed files, like `go run ./codegen -check`.
func TestGenerateMatchesRepository(t *testing.T) {
	files, err := Generate("..")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files generated")
	}
	for _, f := range files {
		if !bytes.HasPrefix(f.Content, generatedMarker) {
			t.Errorf("%s lacks the generated-code header", f.Name)
		}
		old, err := os.ReadFile(filepath.Join("..", f.Name))
		if err != nil || !bytes.Equal(old, f.Content) {
			t.Errorf("%s is stale; run go run ./codegen", f.Name)
		}
	}
}

// writeSpec writes a minimal openapi/ tree into a temporary directory.
func writeSpec(t *testing.T, verification, overlay string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openapi"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"verification.json": verification,
		"account.json":      `{"openapi":"3.0.0","paths":{}}`,
		"overlay.json":      overlay,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, "openapi", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestOperationWithoutNamingFails(t *testing.T) {
	dir := writeSpec(t,
		`{"openapi":"3.0.0","paths":{"/v3/things/":{"get":{"operationId":"list_things","responses":{"204":{"description":"ok"}}}}}}`,
		`{"operations":{}}`)
	_, err := Generate(dir)
	if err == nil || !strings.Contains(err.Error(), "GET /v3/things/") || !strings.Contains(err.Error(), "overlay.json") {
		t.Fatalf("err = %v, want a message naming the operation", err)
	}
}

func TestDuplicateNamingFails(t *testing.T) {
	dir := writeSpec(t,
		`{"openapi":"3.0.0","paths":{
			"/a/":{"get":{"responses":{"204":{"description":"ok"}}}},
			"/b/":{"get":{"responses":{"204":{"description":"ok"}}}}}}`,
		`{"operations":{
			"GET /a/":{"resource":"things","method":"list","auth":"api_key","server":true},
			"GET /b/":{"resource":"things","method":"list","auth":"api_key","server":true}}}`)
	if _, err := Generate(dir); err == nil || !strings.Contains(err.Error(), "things.list") {
		t.Fatalf("err = %v", err)
	}
}

func TestSmallSpec(t *testing.T) {
	dir := writeSpec(t, `{"openapi":"3.0.0","paths":{
		"/v3/things/{thing_id}/":{"get":{
			"parameters":[{"name":"thing_id","in":"path","required":true,"schema":{"type":"string"}},
				{"name":"expand","in":"query","schema":{"type":"string","enum":["a","b"]}}],
			"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Thing"},
				"example":{"id":"t1","kind":"big","token":"`+fakeJWT+`"}}}}}}},
		"/v3/things/":{"post":{
			"requestBody":{"required":true,"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Thing"}}}},
			"responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Thing"}}}}}}}},
		"components":{"schemas":{"Thing":{"type":"object","required":["id"],"properties":{
			"id":{"type":"string"},
			"kind":{"type":"string","enum":["big","small"]},
			"token":{"type":"string","readOnly":true},
			"created_at":{"type":"string","format":"date-time"},
			"tags":{"type":"array","items":{"type":"string"}}}}}}}`,
		`{"operations":{
			"GET /v3/things/{thing_id}/":{"resource":"things","method":"retrieve","auth":"api_key","server":true},
			"POST /v3/things/":{"resource":"things","method":"create","auth":"api_key","server":true}}}`)
	files, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]string{}
	for _, f := range files {
		all[f.Name] = string(f.Content)
	}
	// Compare with whitespace collapsed: gofmt aligns fields and constants.
	code := strings.Join(strings.FieldsFunc(all["things_gen.go"], func(r rune) bool { return r == ' ' || r == '\t' }), " ")
	for _, want := range []string{
		"type ThingsService struct",
		"func (r *ThingsService) Retrieve(ctx context.Context, thingID string, params ThingsRetrieveParams, opts ...option.RequestOption) (*Thing, error)",
		"func (r *ThingsService) Create(ctx context.Context, params ThingParam, opts ...option.RequestOption) (*Thing, error)",
		"Expand Opt[ThingsRetrieveParamsExpand] `json:\"-\" query:\"expand\"`",
		"type ThingKind string",
		"ThingKindBig ThingKind = \"big\"",
		"CreatedAt time.Time `json:\"created_at\"`",
		"ID string `json:\"id\"`",
		"JSON ResponseJSON `json:\"-\"`",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("generated code lacks %q", want)
		}
	}
	// The request copy of a shared component has Opt fields, no readOnly
	// field, and shares the response's enum.
	param := code[strings.Index(code, "type ThingParam struct"):]
	param = param[:strings.Index(param, "}")]
	if strings.Contains(param, "Token") || !strings.Contains(param, "Kind Opt[ThingKind]") || !strings.Contains(param, "ID string") {
		t.Errorf("ThingParam:\n%s", param)
	}
	tests := all["things_gen_test.go"]
	if !strings.Contains(tests, "func TestThingsRetrieve(t *testing.T)") || !strings.Contains(tests, `req.expect(t, "GET", "/v3/things/thing-id-1/")`) {
		t.Errorf("generated tests:\n%s", tests)
	}
	if strings.Contains(tests, fakeJWT) || !strings.Contains(tests, "sample-token") {
		t.Error("a JWT-like example value reached the generated tests")
	}
}

// A success without declared content returns raw JSON unless it is a 204.
func TestResponseWithoutContent(t *testing.T) {
	dir := writeSpec(t, `{"openapi":"3.0.0","paths":{
		"/v3/things/":{"get":{"responses":{"200":{"description":"Array of things"}}}},
		"/v3/things/{thing_id}/":{"delete":{
			"parameters":[{"name":"thing_id","in":"path","required":true,"schema":{"type":"string"}}],
			"responses":{"204":{"description":"Deleted"}}}}}}`,
		`{"operations":{
			"GET /v3/things/":{"resource":"things","method":"list","auth":"api_key","server":true},
			"DELETE /v3/things/{thing_id}/":{"resource":"things","method":"delete","auth":"api_key","server":true}}}`)
	files, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]string{}
	for _, f := range files {
		all[f.Name] = string(f.Content)
	}
	code := all["things_gen.go"]
	for _, want := range []string{
		"func (r *ThingsService) List(ctx context.Context, opts ...option.RequestOption) (json.RawMessage, error)",
		"func (r *ThingsService) Delete(ctx context.Context, thingID string, opts ...option.RequestOption) error",
		"\t\"encoding/json\"\n",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("generated code lacks %q:\n%s", want, code)
		}
	}
	if tests := all["things_gen_test.go"]; !strings.Contains(tests, "if string(res) != `[{\"ok\":true}]` {") {
		t.Errorf("generated tests do not check the raw body:\n%s", tests)
	}
}

func TestWriteDoc(t *testing.T) {
	var b bytes.Buffer
	writeDoc(&b, "\t", "First line \u2014 with a dash.\n\n- item one that is long enough to wrap around the configured width of the comment block\n- item two\n\n| a | b |\n\n1. numbered")
	got := b.String()
	for _, want := range []string{
		"\t// First line - with a dash.\n",
		"\t//   - item one that is long enough",
		"\t//     ",
		"\t//   - item two\n",
		"\t//\t| a | b |\n",
		"\t//   1. numbered\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doc lacks %q:\n%s", want, got)
		}
	}
	b.Reset()
	writeDoc(&b, "", "   ")
	if b.Len() != 0 {
		t.Error("empty doc written")
	}
}

// fakeJWT and fakeKey are built at run time so that no credential-like
// literal is committed (the secret scan covers the whole history).
var (
	fakeJWT = "eyJ" + strings.Repeat("a", 12) + ".eyJ" + strings.Repeat("b", 12) + ".sig"
	fakeKey = "sk" + "_live_" + strings.Repeat("0", 8)
)

func TestSanitize(t *testing.T) {
	n, err := ParseNode([]byte(`{"api_key":"abc123","nested":[{"refresh_token":"x"}],"plain":"keep","jwt":"` + fakeJWT + `","key":"` + fakeKey + `","empty_token":""}`))
	if err != nil {
		t.Fatal(err)
	}
	got := sanitizeExample(n).JSON()
	want := `{"api_key":"sample-api-key","nested":[{"refresh_token":"sample-refresh-token"}],"plain":"keep","jwt":"sample-jwt","key":"sample-key","empty_token":""}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSanitizeKeepsTimestamps(t *testing.T) {
	n, err := ParseNode([]byte(`{"credentials_expires_at":"2026-10-27T10:00:00Z","token_date":"2026-10-27"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := sanitizeExample(n).JSON()
	want := `{"credentials_expires_at":"2026-10-27T10:00:00Z","token_date":"2026-10-27"}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestTemplateParams(t *testing.T) {
	got := templateParams("/v3/organization/{organization_id}/x/{id}/y/{")
	if strings.Join(got, ",") != "organization_id,id" {
		t.Fatalf("got %v", got)
	}
}
