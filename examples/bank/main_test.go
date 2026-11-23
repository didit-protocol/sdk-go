package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Read the published response examples used by the generated Bank client tests.
func bankResponseExamples(t *testing.T) (link, accounts json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile("../../openapi/verification.json")
	if err != nil {
		t.Fatal(err)
	}
	type media struct {
		Examples map[string]struct{ Value json.RawMessage }
	}
	type response struct{ Content map[string]media }
	type operation struct{ Responses map[string]response }
	var doc struct {
		Paths map[string]struct{ Get operation }
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	example := func(path, name string) json.RawMessage {
		v := doc.Paths[path].Get.Responses["200"].Content["application/json"].Examples[name].Value
		if len(v) == 0 {
			t.Fatalf("missing %s example for %s", name, path)
		}
		return v
	}
	return example("/v3/bank/links/{id}/", "Completed"), example("/v3/bank/connections/{id}/accounts/", "Page")
}

// Only the connection attached to the created link may trigger the account read.
// The IDs are from the OpenAPI GetLink "Completed" example.
func TestAccountsForConnection(t *testing.T) {
	const link = "0b9c4f1e-6a2d-4e8b-9f31-7c5d2e1a8b40"
	const own = "5e1d2a70-3b94-4c1f-8a6e-d2f07b9c1e35"
	linkBody, accountsBody := bankResponseExamples(t)
	for _, tc := range []struct {
		name, eventID string
		read          bool
	}{
		{"unrelated", "different-connection", false},
		{"matching", own, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lookedUp, read bool
			transport := transportFunc(func(req *http.Request) (*http.Response, error) {
				var body json.RawMessage
				switch req.URL.Path {
				case "/v3/bank/links/" + link + "/":
					lookedUp = true
					body = linkBody
				case "/v3/bank/connections/" + own + "/accounts/":
					read = true
					body = accountsBody
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})
			client := didit.NewClient(option.WithAPIKey("example"), option.WithHTTPClient(&http.Client{Transport: transport}))
			page, err := accountsForConnection(context.Background(), client, link, tc.eventID)
			if tc.read && page != nil && len(page.Items) != 1 {
				t.Fatalf("got %d accounts, want 1", len(page.Items))
			}
			if err != nil || !lookedUp || read != tc.read || (page != nil) != tc.read {
				t.Fatalf("lookup=%t read=%t page=%v err=%v", lookedUp, read, page, err)
			}
		})
	}
}
