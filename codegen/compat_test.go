package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCompatSpec writes a minimal openapi/ tree with a compatibility.json.
func writeCompatSpec(t *testing.T, verification, overlay, compat string) string {
	t.Helper()
	dir := writeSpec(t, verification, overlay)
	if err := os.WriteFile(filepath.Join(dir, "openapi", "compatibility.json"), []byte(compat), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// generateCollapsed returns the generated files with whitespace collapsed
// (gofmt aligns fields and constants).
func generateCollapsed(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]string{}
	for _, f := range files {
		all[f.Name] = strings.Join(strings.FieldsFunc(string(f.Content), func(r rune) bool { return r == ' ' || r == '\t' }), " ")
	}
	return all
}

const compatDeleteSpec = `{"openapi":"3.0.0","paths":{
	"/v3/things/{thing_id}/":{"delete":{
		"parameters":[{"name":"thing_id","in":"path","required":true,"schema":{"type":"string"}}],
		"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{
			"confirm_delete":{"type":"boolean"}}}}}},
		"responses":{"204":{"description":"ok"}}}},
	"/v3/things/":{"get":{
		"parameters":[{"name":"limit","in":"query","schema":{"type":"integer"}},
			{"name":"offset","in":"query","schema":{"type":"integer"}}],
		"responses":{"200":{"content":{"application/json":{"schema":{"type":"object","properties":{
			"count":{"type":"integer"},"next":{"type":"string"},"previous":{"type":"string"},
			"results":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"}}}}}}}}}}}}}}`

const compatDeleteOverlay = `{"operations":{
	"DELETE /v3/things/{thing_id}/":{"resource":"things","method":"delete","auth":"api_key","server":true},
	"GET /v3/things/":{"resource":"things","method":"list","auth":"api_key","server":true}}}`

// An optional body added to a method released without parameters keeps the
// released signature; the companion method of openapi/compatibility.json
// sends the body, and each gets its own generated test.
func TestCompatParamsMethod(t *testing.T) {
	dir := writeCompatSpec(t, compatDeleteSpec, compatDeleteOverlay,
		`{"params_methods":{"things.delete":"delete_with_params","things.list":"list_with_params"}}`)
	all := generateCollapsed(t, dir)
	code := all["things_gen.go"]
	for _, want := range []string{
		"func (r *ThingsService) Delete(ctx context.Context, thingID string, opts ...option.RequestOption) error { _, err := r.client.do(ctx, &apiRequest{method: http.MethodDelete, path: \"/v3/things/{thing_id}/\", pathParams: []string{\"thing_id\", thingID}}, nil, opts)",
		"func (r *ThingsService) DeleteWithParams(ctx context.Context, thingID string, params ThingsDeleteParams, opts ...option.RequestOption) error { _, err := r.client.do(ctx, &apiRequest{method: http.MethodDelete, path: \"/v3/things/{thing_id}/\", pathParams: []string{\"thing_id\", thingID}, params: params, body: bodyJSON}, nil, opts)",
		"// Delete sends no parameters; DeleteWithParams sends ThingsDeleteParams.",
		"// ThingsDeleteParams holds the parameters of Client.Things.DeleteWithParams.",
		"ConfirmDelete Opt[bool] `json:\"confirm_delete\"`",
		"func (r *ThingsService) List(ctx context.Context, opts ...option.RequestOption) (*Page[ThingsListResponseResults], error)",
		"func (r *ThingsService) ListAutoPaging(ctx context.Context, opts ...option.RequestOption) *PageIterator[ThingsListResponseResults] { return newPageIterator(ctx, func() (*Page[ThingsListResponseResults], error) { return r.List(ctx, opts...)",
		"func (r *ThingsService) ListWithParamsAutoPaging(ctx context.Context, params ThingsListParams, opts ...option.RequestOption) *PageIterator[ThingsListResponseResults] { return newPageIterator(ctx, func() (*Page[ThingsListResponseResults], error) { return r.ListWithParams(ctx, params, opts...)",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("generated code lacks %q", want)
		}
	}
	tests := all["things_gen_test.go"]
	for _, want := range []string{
		"func TestThingsDelete(t *testing.T) { client, mock := newMockClient(t, 204, \"\", ``) if err := client.Things.Delete(context.Background(), \"thing-id-1\"); err != nil {",
		"req.expectNoBody(t) }",
		"func TestThingsDeleteWithParams(t *testing.T) {",
		"client.Things.DeleteWithParams(context.Background(), \"thing-id-1\", params)",
		"func TestThingsListWithParams(t *testing.T) {",
	} {
		if !strings.Contains(tests, want) {
			t.Errorf("generated tests lack %q:\n%s", want, tests)
		}
	}
}

func TestCompatParamsMethodThatNoLongerApplies(t *testing.T) {
	for _, tc := range []struct {
		name, spec, compat, want string
	}{
		{
			name:   "unknown operation",
			spec:   compatDeleteSpec,
			compat: `{"params_methods":{"things.archive":"archive_with_params"}}`,
			want:   "params_methods things.archive: no server operation",
		},
		{
			name:   "required body field",
			spec:   strings.Replace(compatDeleteSpec, `"schema":{"type":"object","properties":{`, `"schema":{"type":"object","required":["confirm_delete"],"properties":{`, 1),
			compat: `{"params_methods":{"things.delete":"delete_with_params"}}`,
			want:   "ThingsDeleteParams.ConfirmDelete is required",
		},
		{
			name:   "no parameters",
			spec:   strings.Replace(compatDeleteSpec, `"requestBody":`, `"x-request-body":`, 1),
			compat: `{"params_methods":{"things.delete":"delete_with_params"}}`,
			want:   "takes no parameters",
		},
		{
			name:   "companion collides",
			spec:   compatDeleteSpec,
			compat: `{"params_methods":{"things.delete":"list"}}`,
			want:   "collides",
		},
		{
			name:   "unknown key",
			spec:   compatDeleteSpec,
			compat: `{"params_method":{}}`,
			want:   "openapi/compatibility.json",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(writeCompatSpec(t, tc.spec, compatDeleteOverlay, tc.compat))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

const compatAliasSpec = `{"openapi":"3.0.0","paths":{
	"/v3/things/{thing_id}/":{"get":{
		"parameters":[{"name":"thing_id","in":"path","required":true,"schema":{"type":"string"}}],
		"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Thing"}}}}}}}},
	"components":{"schemas":{"Thing":{"type":"object","properties":{
		"mode":{"type":"string","enum":["own","off"]}}}}}}`

const compatAliasOverlay = `{"operations":{
	"GET /v3/things/{thing_id}/":{"resource":"things","method":"retrieve","auth":"api_key","server":true}}}`

// A released type name whose schema the API replaced is kept as a deprecated
// alias, next to its target, with the released constants of an enum.
func TestCompatTypeAliases(t *testing.T) {
	dir := writeCompatSpec(t, compatAliasSpec, compatAliasOverlay, `{"type_aliases":{
		"ThingsRetrieveResponseDetail":{"alias_of":"Thing"},
		"ThingsRetrieveResponseDetailMode":{"alias_of":"ThingMode","values":["own","gone"]}}}`)
	code := generateCollapsed(t, dir)["things_gen.go"]
	for _, want := range []string{
		"// ThingsRetrieveResponseDetail is an alias of Thing, kept from an earlier release so that code that uses it still compiles. // // Deprecated: Use Thing. type ThingsRetrieveResponseDetail = Thing",
		"// Deprecated: Use ThingMode. type ThingsRetrieveResponseDetailMode = ThingMode",
		"// Deprecated: Use ThingModeOwn. ThingsRetrieveResponseDetailModeOwn ThingsRetrieveResponseDetailMode = \"own\"",
		"// Deprecated: Use a value of ThingMode. ThingsRetrieveResponseDetailModeGone ThingsRetrieveResponseDetailMode = \"gone\"",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("generated code lacks %q:\n%s", want, code)
		}
	}
	// Each alias follows its target.
	if strings.Index(code, "type ThingsRetrieveResponseDetail = Thing") < strings.Index(code, "type Thing struct") ||
		strings.Index(code, "type ThingsRetrieveResponseDetailMode = ThingMode") < strings.Index(code, "type ThingMode string") {
		t.Errorf("aliases are not placed after their targets:\n%s", code)
	}
}

func TestCompatTypeAliasThatNoLongerApplies(t *testing.T) {
	for _, tc := range []struct{ name, compat, want string }{
		{"generated again", `{"type_aliases":{"ThingMode":{"alias_of":"Thing"}}}`, "ThingMode is generated again"},
		{"missing target", `{"type_aliases":{"OldThing":{"alias_of":"Gone"}}}`, "alias_of Gone is not a generated type"},
		{"values of a struct", `{"type_aliases":{"OldThing":{"alias_of":"Thing","values":["own"]}}}`, "Thing is not an enum"},
		{"reserved name", `{"type_aliases":{"Client":{"alias_of":"Thing"}}}`, "Client is generated again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Generate(writeCompatSpec(t, compatAliasSpec, compatAliasOverlay, tc.compat))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
