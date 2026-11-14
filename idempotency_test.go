package didit_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

const testRepeatToken = "order-123"

var okJSON = response{status: 200, contentType: "application/json", body: `{}`}

func TestIdempotencyKeyHeaderByMethod(t *testing.T) {
	ctx := context.Background()
	key := option.WithIdempotencyKey(testRepeatToken)
	calls := map[string]func(c *didit.Client) error{
		"POST": func(c *didit.Client) error {
			_, err := c.Sessions.Create(ctx, didit.SessionsCreateParams{WorkflowID: "w"}, key)
			return err
		},
		"GET": func(c *didit.Client) error {
			_, err := c.Billing.RetrieveBalance(ctx, key)
			return err
		},
	}
	for method, call := range calls {
		client, m := newMockClientWith(t, []response{okJSON})
		if err := call(client); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		req := m.single(t)
		if req.Method != method {
			t.Fatalf("method = %s, want %s", req.Method, method)
		}
		want := testRepeatToken
		if method == "GET" {
			want = ""
		}
		req.expectHeader(t, "Idempotency-Key", want)
	}
}

func TestIdempotencyKeyNotAcceptedOnClient(t *testing.T) {
	for name, build := range map[string]func(m *mockTransport) *didit.Client{
		"NewClient": func(m *mockTransport) *didit.Client {
			return didit.NewClient(option.WithAPIKey(testAPIKey), option.WithBaseURL(testBaseURL),
				option.WithHTTPClient(&http.Client{Transport: m}), option.WithIdempotencyKey("k"))
		},
		"WithOptions": func(m *mockTransport) *didit.Client {
			base := didit.NewClient(option.WithAPIKey(testAPIKey), option.WithBaseURL(testBaseURL),
				option.WithHTTPClient(&http.Client{Transport: m}))
			return base.WithOptions(option.WithIdempotencyKey("k"))
		},
	} {
		m := &mockTransport{responses: []response{okJSON}}
		_, err := build(m).Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"})
		var ce *didit.ConfigurationError
		if !errors.As(err, &ce) {
			t.Fatalf("%s: err = %v, want a ConfigurationError", name, err)
		}
		if n := len(m.all()); n != 0 {
			t.Errorf("%s: %d requests sent", name, n)
		}
	}
}

func TestIdempotencyKeyValidation(t *testing.T) {
	for name, key := range map[string]string{
		"empty":              "",
		"256 characters":     strings.Repeat("k", 256),
		"non-ASCII text":     "注文-1",
		"an accented letter": "café",
		"a newline":          "a\nb",
	} {
		client, m := newMockClientWith(t, []response{okJSON})
		_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithIdempotencyKey(key))
		var ce *didit.ConfigurationError
		if !errors.As(err, &ce) {
			t.Fatalf("%s: err = %v, want a ConfigurationError", name, err)
		}
		if n := len(m.all()); n != 0 {
			t.Errorf("%s: %d requests sent", name, n)
		}
	}
	for _, key := range []string{"k", strings.Repeat("k", 255), "order 1"} {
		client, _ := newMockClientWith(t, []response{okJSON})
		if _, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithIdempotencyKey(key)); err != nil {
			t.Errorf("%d characters: %v", len([]rune(key)), err)
		}
	}
}

func TestIdempotencyKeyRetriesPostOnServerError(t *testing.T) {
	for _, status := range []int{500, 503, 408} {
		client, m := newMockClientWith(t, []response{
			{status: status, header: retryAfterZero},
			{status: 201, contentType: "application/json", body: `{"session_id":"s"}`},
		}, option.WithMaxRetries(2))
		res, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithIdempotencyKey(testRepeatToken))
		if err != nil || res.SessionID != "s" {
			t.Fatalf("status %d: res = %+v, err = %v", status, res, err)
		}
		reqs := m.all()
		if len(reqs) != 2 {
			t.Fatalf("status %d: %d attempts, want 2", status, len(reqs))
		}
		for _, r := range reqs {
			r.expectHeader(t, "Idempotency-Key", testRepeatToken)
		}
		if !bytes.Equal(reqs[0].Body, reqs[1].Body) {
			t.Errorf("the retry sent another body")
		}
	}
}

// writtenThenBlockedTransport reports the headers as written, then waits for
// the context: a read timeout after the request reached the server.
type writtenThenBlockedTransport struct {
	calls atomic.Int32
	keys  []string
	next  http.RoundTripper
}

func (b *writtenThenBlockedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.calls.Add(1) == 1 {
		b.keys = append(b.keys, req.Header.Get("Idempotency-Key"))
		if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.WroteHeaders != nil {
			trace.WroteHeaders()
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	b.keys = append(b.keys, req.Header.Get("Idempotency-Key"))
	return b.next.RoundTrip(req)
}

func TestIdempotencyKeyRetriesPostOnReadTimeout(t *testing.T) {
	for _, withKey := range []bool{true, false} {
		m := &mockTransport{responses: []response{{status: 201, contentType: "application/json", body: `{}`}}}
		bt := &writtenThenBlockedTransport{next: m}
		client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: bt}),
			option.WithTimeout(20*time.Millisecond), option.WithMaxRetries(2))
		var opts []option.RequestOption
		if withKey {
			opts = append(opts, option.WithIdempotencyKey(testRepeatToken))
		}
		_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, opts...)
		if withKey {
			if err != nil || bt.calls.Load() != 2 {
				t.Fatalf("with a key: err = %v, calls = %d, want success after 2", err, bt.calls.Load())
			}
			if len(bt.keys) != 2 || bt.keys[0] != testRepeatToken || bt.keys[1] != testRepeatToken {
				t.Errorf("keys = %v, want the same key twice", bt.keys)
			}
			continue
		}
		var te *didit.APITimeoutError
		if !errors.As(err, &te) || bt.calls.Load() != 1 {
			t.Fatalf("without a key: err = %v, calls = %d, want a timeout after 1", err, bt.calls.Load())
		}
	}
}

func TestIdempotencyKeyReusedIsNotRetried(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 409, contentType: "application/json", body: `{"detail":"reused","code":"idempotency_key_reused"}`, header: retryAfterZero},
		{status: 201, contentType: "application/json", body: `{}`},
	}, option.WithMaxRetries(2))
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithIdempotencyKey(testRepeatToken))
	var ce *didit.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a ConflictError", err)
	}
	if n := len(m.all()); n != 1 {
		t.Errorf("%d attempts, want 1", n)
	}
}

func TestIdempotencyKeyInProgressWithoutKeyIsNotRetried(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 409, contentType: "application/json", body: `{"code":"idempotency_key_in_progress"}`, header: retryAfterZero},
		{status: 201, contentType: "application/json", body: `{}`},
	}, option.WithMaxRetries(2))
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"})
	var ce *didit.ConflictError
	if !errors.As(err, &ce) || len(m.all()) != 1 {
		t.Fatalf("err = %v, attempts = %d", err, len(m.all()))
	}
}

func TestIdempotencyKeyOptionWinsOverGeneratedParameter(t *testing.T) {
	client, m := newMockClientWith(t, []response{okJSON})
	_, err := client.AML.Screen(context.Background(), didit.AMLScreenParams{
		FullName:       "Jane Doe",
		IdempotencyKey: didit.String("from-param"),
	}, option.WithIdempotencyKey("from-option"))
	if err != nil {
		t.Fatal(err)
	}
	req := m.single(t)
	if got := req.Header.Values("Idempotency-Key"); len(got) != 1 || got[0] != "from-option" {
		t.Errorf("Idempotency-Key = %v, want one value from the option", got)
	}
}

func TestGeneratedKeyParameterAloneMakesPostRetryable(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 503, header: retryAfterZero},
		okJSON,
	}, option.WithMaxRetries(2))
	_, err := client.AML.Screen(context.Background(), didit.AMLScreenParams{FullName: "Jane Doe", IdempotencyKey: didit.String("from-param")})
	if err != nil || len(m.all()) != 2 {
		t.Fatalf("err = %v, attempts = %d, want a retry", err, len(m.all()))
	}
}

func TestIdempotencyKeyNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client, _ := newMockClientWith(t, []response{okJSON}, option.WithLogger(logger))
	if _, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithIdempotencyKey(testRepeatToken)); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected a debug log")
	}
	if strings.Contains(buf.String(), testRepeatToken) {
		t.Errorf("the key was logged: %s", buf.String())
	}
}

func TestIdempotencyKeyRawHeaderCountsAndIsRetried(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 503, header: retryAfterZero},
		{status: 201, contentType: "application/json", body: `{}`},
	}, option.WithMaxRetries(2))
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithHeader("idempotency-key", "raw-1"))
	if err != nil {
		t.Fatal(err)
	}
	reqs := m.all()
	if len(reqs) != 2 {
		t.Fatalf("%d attempts, want 2", len(reqs))
	}
	for _, r := range reqs {
		if got := r.Header.Values("Idempotency-Key"); len(got) != 1 || got[0] != "raw-1" {
			t.Errorf("Idempotency-Key = %v", got)
		}
	}
}

func TestIdempotencyKeyInvalidFromParameterOrRawHeader(t *testing.T) {
	ctx := context.Background()
	for name, call := range map[string]func(c *didit.Client) error{
		"parameter empty": func(c *didit.Client) error {
			_, err := c.AML.Screen(ctx, didit.AMLScreenParams{FullName: "Jane Doe", IdempotencyKey: didit.String("")})
			return err
		},
		"parameter 256": func(c *didit.Client) error {
			_, err := c.AML.Screen(ctx, didit.AMLScreenParams{FullName: "Jane Doe", IdempotencyKey: didit.String(strings.Repeat("k", 256))})
			return err
		},
		"raw empty": func(c *didit.Client) error {
			_, err := c.Sessions.Create(ctx, didit.SessionsCreateParams{WorkflowID: "w"}, option.WithHeader("Idempotency-Key", ""))
			return err
		},
		"raw 256": func(c *didit.Client) error {
			_, err := c.Sessions.Create(ctx, didit.SessionsCreateParams{WorkflowID: "w"}, option.WithHeader("idempotency-key", strings.Repeat("k", 256)))
			return err
		},
	} {
		client, m := newMockClientWith(t, []response{okJSON})
		var ce *didit.ConfigurationError
		if err := call(client); !errors.As(err, &ce) {
			t.Fatalf("%s: err = %v, want a ConfigurationError", name, err)
		}
		if n := len(m.all()); n != 0 {
			t.Errorf("%s: %d requests sent", name, n)
		}
	}
}

func TestIdempotencyKeyAllSourcesSendOneHeader(t *testing.T) {
	client, m := newMockClientWith(t, []response{okJSON}, option.WithHeader("Idempotency-Key", "client-default"))
	_, err := client.AML.Screen(context.Background(), didit.AMLScreenParams{FullName: "Jane Doe", IdempotencyKey: didit.String("from-param")},
		option.WithHeader("idempotency-key", "from-raw"), option.WithIdempotencyKey("from-option"))
	if err != nil {
		t.Fatal(err)
	}
	got := m.single(t).Header.Values("Idempotency-Key")
	if len(got) != 1 || got[0] != "from-option" {
		t.Errorf("Idempotency-Key = %v, want one value from the option", got)
	}
}

func TestIdempotencyKeyRawHeaderBeatsParameter(t *testing.T) {
	client, m := newMockClientWith(t, []response{okJSON})
	_, err := client.AML.Screen(context.Background(), didit.AMLScreenParams{FullName: "Jane Doe", IdempotencyKey: didit.String("from-param")},
		option.WithHeader("idempotency-key", "from-raw"))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.single(t).Header.Values("Idempotency-Key"); len(got) != 1 || got[0] != "from-raw" {
		t.Errorf("Idempotency-Key = %v, want one value from the raw header", got)
	}
}

func TestIdempotencyKeyConflictWithNonJSONBodyIsNotRetried(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 409, contentType: "text/plain", body: "idempotency_key_in_progress", header: retryAfterZero},
		{status: 201, contentType: "application/json", body: `{}`},
	}, option.WithMaxRetries(2))
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithIdempotencyKey(testRepeatToken))
	var ce *didit.ConflictError
	if !errors.As(err, &ce) || len(m.all()) != 1 {
		t.Fatalf("err = %v, attempts = %d, want a ConflictError after 1", err, len(m.all()))
	}
}

func TestIdempotencyKeyInProgressCountsAgainstMaxRetries(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 409, contentType: "application/json", body: `{"code":"idempotency_key_in_progress"}`, header: retryAfterZero},
	}, option.WithMaxRetries(2))
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}, option.WithIdempotencyKey(testRepeatToken))
	var ce *didit.ConflictError
	if !errors.As(err, &ce) || len(m.all()) != 3 {
		t.Fatalf("err = %v, attempts = %d, want a ConflictError after 3", err, len(m.all()))
	}
}
