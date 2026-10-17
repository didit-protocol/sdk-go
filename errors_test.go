package didit_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/didit-protocol/sdk-go"
)

func TestErrorMessages(t *testing.T) {
	cause := errors.New("dial tcp: refused")
	conn := &didit.APIConnectionError{Method: "GET", URL: "https://verification.didit.me/v3/x/", Cause: cause}
	cases := []struct {
		err  error
		want string
	}{
		{&didit.ConfigurationError{Message: "missing API key"}, "didit: missing API key"},
		{conn, "didit: connection error on GET https://verification.didit.me/v3/x/: dial tcp: refused"},
		{&didit.APIConnectionError{}, "didit: connection error"},
		{&didit.APITimeoutError{APIConnectionError: conn}, "didit: request timed out on GET https://verification.didit.me/v3/x/"},
		{&didit.APITimeoutError{}, "didit: request timed out"},
		{&didit.WebhookVerificationError{Reason: didit.WebhookReasonInvalidSignature}, "didit: webhook verification failed: invalid_signature"},
		{&didit.WebhookVerificationError{Reason: didit.WebhookReasonMissingSignature, Message: "no headers"}, "didit: webhook verification failed: missing_signature: no headers"},
		{&didit.TokenVerificationError{Reason: didit.TokenReasonExpired}, "didit: token verification failed: expired"},
		{&didit.TokenVerificationError{Reason: didit.TokenReasonInvalidIssuer, Message: "iss"}, "didit: token verification failed: invalid_issuer: iss"},
		{&didit.UnexpectedResponseError{Message: "bad", Cause: cause}, "didit: unexpected response: bad: dial tcp: refused"},
		{&didit.UnexpectedResponseError{Message: "bad"}, "didit: unexpected response: bad"},
		{&didit.APIStatusError{StatusCode: 404}, "didit: 404 Not Found"},
		{&didit.APIStatusError{StatusCode: 400, Body: "plain text body"}, "didit: 400 Bad Request: plain text body"},
		{&didit.APIStatusError{StatusCode: 400, Body: []any{"a", "b"}}, `didit: 400 Bad Request: ["a","b"]`},
		{&didit.APIStatusError{StatusCode: 400, Body: map[string]any{"error": "boom"}}, "didit: 400 Bad Request: boom"},
		{&didit.APIStatusError{StatusCode: 400, Body: map[string]any{"message": "m"}}, "didit: 400 Bad Request: m"},
		{&didit.APIStatusError{StatusCode: 400, Body: map[string]any{"field": []any{"required"}}}, `didit: 400 Bad Request: {"field":["required"]}`},
		{&didit.APIStatusError{StatusCode: 400, Body: map[string]any{"bad": make(chan int)}}, "didit: 400 Bad Request"},
		{&didit.APIStatusError{StatusCode: 400, Body: make(chan int)}, "didit: 400 Bad Request"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("got  %q\nwant %q", got, c.want)
		}
		var base didit.Error
		if !errors.As(c.err, &base) {
			t.Errorf("%T does not match didit.Error", c.err)
		}
	}
	if !errors.Is(conn, cause) || !errors.Is(&didit.UnexpectedResponseError{Cause: cause}, cause) {
		t.Error("Unwrap")
	}
	long := &didit.APIStatusError{StatusCode: 400, Body: strings.Repeat("x", 500)}
	if msg := long.Error(); !strings.HasSuffix(msg, "...") || len(msg) > 340 {
		t.Errorf("long message not truncated: %d", len(msg))
	}
}

func TestNewAPIStatusErrorDefault(t *testing.T) {
	base := &didit.APIStatusError{StatusCode: 302}
	if err := didit.NewAPIStatusError(base); !errors.Is(err, base) {
		t.Fatalf("got %T", err)
	}
}

func TestStatusErrorKeepsResponse(t *testing.T) {
	client, _ := newMockClient(t, 409, "application/json", `{"detail":"exists"}`)
	_, err := client.Billing.RetrieveBalance(context.Background())
	var c *didit.ConflictError
	if !errors.As(err, &c) || c.Response == nil || c.Response.StatusCode != http.StatusConflict || c.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(c.Error(), "GET https://verification.test/v3/billing/balance/") {
		t.Errorf("message = %s", c.Error())
	}
}
