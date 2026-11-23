package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Only the connection attached to the created link may trigger the account read.
// The IDs are from the OpenAPI GetLink "Completed" example.
func TestAccountsForConnection(t *testing.T) {
	const link = "0b9c4f1e-6a2d-4e8b-9f31-7c5d2e1a8b40"
	const own = "5e1d2a70-3b94-4c1f-8a6e-d2f07b9c1e35"
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
				var body string
				switch req.URL.Path {
				case "/v3/bank/links/" + link + "/":
					lookedUp = true
					body = `{"connection_id":"` + own + `"}`
				case "/v3/bank/connections/" + own + "/accounts/":
					read = true
					body = `{"items":[],"next_cursor":null}`
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			client := didit.NewClient(option.WithAPIKey("example"), option.WithHTTPClient(&http.Client{Transport: transport}))
			page, err := accountsForConnection(context.Background(), client, link, tc.eventID)
			if err != nil || !lookedUp || read != tc.read || (page != nil) != tc.read {
				t.Fatalf("lookup=%t read=%t page=%v err=%v", lookedUp, read, page, err)
			}
		})
	}
}
