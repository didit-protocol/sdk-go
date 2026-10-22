//go:build go1.23

package didit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/didit-protocol/sdk-go"
)

func TestAutoPagingRangeOverFunc(t *testing.T) {
	page1 := `{"count":3,"next":"https://verification.test/v3/sessions/?offset=2","previous":null,"results":[{"session_id":"a"},{"session_id":"b"}]}`
	page2 := `{"count":3,"next":null,"previous":null,"results":[{"session_id":"c"}]}`
	client, _ := newMockClientWith(t, []response{
		{status: 200, contentType: "application/json", body: page1},
		{status: 200, contentType: "application/json", body: page2},
	})
	var ids []string
	for s, err := range client.Sessions.ListAutoPaging(context.Background(), didit.SessionsListParams{}).All() {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.SessionID)
	}
	if len(ids) != 3 || ids[2] != "c" {
		t.Fatalf("ids = %v", ids)
	}

	// Breaking out of the loop stops fetching.
	client, m := newMockClientWith(t, []response{{status: 200, contentType: "application/json", body: page1}})
	for _, err := range client.Sessions.ListAutoPaging(context.Background(), didit.SessionsListParams{}).All() {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if len(m.all()) != 1 {
		t.Fatalf("requests = %d", len(m.all()))
	}

	// An error is yielded once, then the loop ends.
	client, _ = newMockClient(t, 401, "application/json", `{"detail":"bad key"}`)
	n := 0
	for _, err := range client.Sessions.ListAutoPaging(context.Background(), didit.SessionsListParams{}).All() {
		n++
		var auth *didit.AuthenticationError
		if !errors.As(err, &auth) {
			t.Fatalf("err = %v", err)
		}
	}
	if n != 1 {
		t.Fatalf("yielded %d times", n)
	}
}
