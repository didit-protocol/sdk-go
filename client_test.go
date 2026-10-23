package didit_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

// clearEnv makes sure the developer's environment does not leak into tests.
func clearEnv(t *testing.T) {
	t.Setenv("DIDIT_API_KEY", "")
	t.Setenv("DIDIT_ORGANIZATION_ID", "")
	t.Setenv("DIDIT_APPLICATION_ID", "")
	t.Setenv("DIDIT_LOG", "")
}

func TestMissingAPIKeyFailsBeforeSending(t *testing.T) {
	clearEnv(t)
	m := &mockTransport{responses: []response{{status: 200, body: `{}`}}}
	client := didit.NewClient(option.WithHTTPClient(&http.Client{Transport: m}))
	_, err := client.Sessions.RetrieveDecision(context.Background(), "s1", didit.SessionsRetrieveDecisionParams{})
	var cfgErr *didit.ConfigurationError
	if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "DIDIT_API_KEY") {
		t.Fatalf("err = %v, want a configuration error about the API key", err)
	}
	var base didit.Error
	if !errors.As(err, &base) {
		t.Fatal("ConfigurationError does not match didit.Error")
	}
	if len(m.all()) != 0 {
		t.Fatal("a request was sent without an API key")
	}
}

func TestEnvironmentFallbacks(t *testing.T) {
	clearEnv(t)
	t.Setenv("DIDIT_API_KEY", "env-key")
	t.Setenv("DIDIT_ORGANIZATION_ID", "env-org")
	t.Setenv("DIDIT_APPLICATION_ID", "env-app")
	m := &mockTransport{responses: []response{{status: 200, contentType: "application/json", body: `{"count":0,"next":null,"previous":null,"results":[]}`}}}
	client := didit.NewClient(option.WithHTTPClient(&http.Client{Transport: m}), option.WithBaseURL(testBaseURL))
	if _, err := client.Cases.List(context.Background(), didit.CasesListParams{}); err != nil {
		t.Fatal(err)
	}
	req := m.single(t)
	if req.Header.Get("X-Api-Key") != "env-key" {
		t.Errorf("x-api-key = %q", req.Header.Get("X-Api-Key"))
	}
	if !strings.HasPrefix(req.Path, "/v3/organization/env-org/application/env-app/") {
		t.Errorf("path = %s", req.Path)
	}
	// Explicit options win over the environment.
	client = didit.NewClient(option.WithHTTPClient(&http.Client{Transport: m}), option.WithAPIKey("explicit"), option.WithBaseURL(testBaseURL))
	if _, err := client.Cases.List(context.Background(), didit.CasesListParams{}); err != nil {
		t.Fatal(err)
	}
	if got := m.all()[1].Header.Get("X-Api-Key"); got != "explicit" {
		t.Errorf("x-api-key = %q, want explicit", got)
	}
}

func TestApplicationScopeNeedsBothIDs(t *testing.T) {
	clearEnv(t)
	m := &mockTransport{responses: []response{{status: 200, body: `{}`}}}
	client := didit.NewClient(option.WithAPIKey("k"), option.WithOrganizationID("org"), option.WithHTTPClient(&http.Client{Transport: m}))
	_, err := client.Cases.List(context.Background(), didit.CasesListParams{})
	var cfgErr *didit.ConfigurationError
	if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "WithApplicationID") {
		t.Fatalf("err = %v", err)
	}
	if len(m.all()) != 0 {
		t.Fatal("request sent")
	}
}

func TestPathParameters(t *testing.T) {
	client, m := newMockClient(t, 200, "application/json", `{}`)
	for _, bad := range []string{"", ".", ".."} {
		_, err := client.Sessions.RetrieveDecision(context.Background(), bad, didit.SessionsRetrieveDecisionParams{})
		var cfgErr *didit.ConfigurationError
		if !errors.As(err, &cfgErr) {
			t.Errorf("id %q: err = %v, want a configuration error", bad, err)
		}
	}
	if len(m.all()) != 0 {
		t.Fatal("request sent for an invalid id")
	}
	if _, err := client.Sessions.RetrieveDecision(context.Background(), "a/b c?d", didit.SessionsRetrieveDecisionParams{}); err != nil {
		t.Fatal(err)
	}
	if got := m.single(t).Path; got != "/v3/session/a%2Fb%20c%3Fd/decision/" {
		t.Errorf("path = %s", got)
	}
}

func TestHeaders(t *testing.T) {
	client, m := newMockClient(t, 200, "application/json", `{}`,
		option.WithHeader("X-Custom", "yes"), option.WithHeader("X-Didit-Client", ""))
	if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := m.single(t).Header
	ua := regexp.MustCompile(`^didit-go/\d+\.\d+\.\d+\S* \(Go \S+; \w+/\w+\)$`)
	if !ua.MatchString(h.Get("User-Agent")) {
		t.Errorf("User-Agent = %q", h.Get("User-Agent"))
	}
	if h.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q", h.Get("Accept"))
	}
	if h.Get("X-Custom") != "yes" {
		t.Error("custom header missing")
	}
	if _, ok := h["X-Didit-Client"]; ok {
		t.Error("an empty header value should remove the header")
	}
}

func TestDefaultClientHeader(t *testing.T) {
	client, m := newMockClient(t, 200, "application/json", `{}`)
	if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.single(t).Header.Get("X-Didit-Client"); !regexp.MustCompile(`^go/\d+\.\d+\.\d+`).MatchString(got) {
		t.Errorf("X-Didit-Client = %q", got)
	}
}

func TestErrorsByStatus(t *testing.T) {
	cases := []struct {
		status int
		check  func(error) bool
	}{
		{400, func(err error) bool { var e *didit.BadRequestError; return errors.As(err, &e) }},
		{401, func(err error) bool { var e *didit.AuthenticationError; return errors.As(err, &e) }},
		{403, func(err error) bool { var e *didit.PermissionDeniedError; return errors.As(err, &e) }},
		{404, func(err error) bool { var e *didit.NotFoundError; return errors.As(err, &e) }},
		{409, func(err error) bool { var e *didit.ConflictError; return errors.As(err, &e) }},
		{422, func(err error) bool { var e *didit.UnprocessableEntityError; return errors.As(err, &e) }},
		{429, func(err error) bool { var e *didit.RateLimitError; return errors.As(err, &e) }},
		{500, func(err error) bool { var e *didit.InternalServerError; return errors.As(err, &e) }},
		{503, func(err error) bool { var e *didit.InternalServerError; return errors.As(err, &e) }},
		{418, func(err error) bool { var e *didit.APIStatusError; return errors.As(err, &e) && e.StatusCode == 418 }},
	}
	for _, c := range cases {
		client, _ := newMockClientWith(t, []response{{
			status: c.status, contentType: "application/json",
			body:   `{"detail":"nope ` + testAPIKey + `"}`,
			header: http.Header{"X-Request-Id": {"req-123"}},
		}})
		_, err := client.Billing.RetrieveBalance(context.Background())
		if !c.check(err) {
			t.Errorf("status %d: wrong error type %T", c.status, err)
			continue
		}
		var se *didit.APIStatusError
		if !errors.As(err, &se) {
			t.Errorf("status %d: does not unwrap to *APIStatusError", c.status)
			continue
		}
		var base didit.Error
		if !errors.As(err, &base) {
			t.Errorf("status %d: does not match didit.Error", c.status)
		}
		if se.StatusCode != c.status || se.RequestID != "req-123" || se.Method != "GET" {
			t.Errorf("status %d: fields %+v", c.status, se)
		}
		body, ok := se.Body.(map[string]any)
		if !ok || !strings.HasPrefix(body["detail"].(string), "nope") {
			t.Errorf("status %d: body = %#v", c.status, se.Body)
		}
		msg := err.Error()
		if strings.Contains(msg, testAPIKey) || bytes.Contains(se.RawBody, []byte(testAPIKey)) {
			t.Errorf("status %d: the API key leaked into the error: %s", c.status, msg)
		}
		if !strings.Contains(msg, "nope") || !strings.Contains(msg, "req-123") {
			t.Errorf("status %d: message %q", c.status, msg)
		}
	}
}

func TestErrorBodyNotJSON(t *testing.T) {
	client, _ := newMockClient(t, 502, "text/html", "<html>bad gateway</html>")
	_, err := client.Billing.RetrieveBalance(context.Background())
	var se *didit.APIStatusError
	if !errors.As(err, &se) || se.Body != "<html>bad gateway</html>" {
		t.Fatalf("err = %v", err)
	}
}

// retryAfterZero makes retries immediate in tests.
var retryAfterZero = http.Header{"Retry-After": {"0"}}

func TestRetriesIdempotentRequests(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504} {
		client, m := newMockClientWith(t, []response{
			{status: status, header: retryAfterZero},
			{status: status, header: retryAfterZero},
			{status: 200, contentType: "application/json", body: `{"balance":"1.00"}`},
		}, option.WithMaxRetries(2))
		if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if n := len(m.all()); n != 3 {
			t.Errorf("status %d: %d attempts, want 3", status, n)
		}
	}
}

func TestRetriesStopAtMaxRetries(t *testing.T) {
	client, m := newMockClientWith(t, []response{{status: 503, header: retryAfterZero}}, option.WithMaxRetries(2))
	_, err := client.Billing.RetrieveBalance(context.Background())
	var e *didit.InternalServerError
	if !errors.As(err, &e) {
		t.Fatalf("err = %v", err)
	}
	if n := len(m.all()); n != 3 {
		t.Errorf("%d attempts, want 3", n)
	}
}

func TestNoRetryOfPostAfterServerError(t *testing.T) {
	for _, status := range []int{500, 503, 408} {
		client, m := newMockClientWith(t, []response{
			{status: status, header: retryAfterZero},
			{status: 201, contentType: "application/json", body: `{}`},
		}, option.WithMaxRetries(2))
		_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"})
		if err == nil {
			t.Fatalf("status %d: want an error", status)
		}
		if n := len(m.all()); n != 1 {
			t.Errorf("status %d: POST sent %d times, want 1", status, n)
		}
	}
}

func TestPostRetriedOn429(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 429, header: retryAfterZero},
		{status: 201, contentType: "application/json", body: `{"session_id":"s"}`},
	}, option.WithMaxRetries(2))
	res, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"})
	if err != nil || res.SessionID != "s" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	reqs := m.all()
	if len(reqs) != 2 || !bytes.Equal(reqs[0].Body, reqs[1].Body) {
		t.Fatalf("attempts = %d, bodies differ", len(reqs))
	}
}

// failingTransport fails the first calls with err; when written is true it
// reports the request headers as written first, as a real transport does when
// the connection drops after sending.
type failingTransport struct {
	failures int
	written  bool
	calls    atomic.Int32
	next     http.RoundTripper
}

func (f *failingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if int(f.calls.Add(1)) <= f.failures {
		if f.written {
			if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.WroteHeaders != nil {
				trace.WroteHeaders()
			}
		}
		return nil, errors.New("connection reset by peer")
	}
	return f.next.RoundTrip(req)
}

func TestPostRetriedWhenNothingWasSent(t *testing.T) {
	m := &mockTransport{responses: []response{{status: 201, contentType: "application/json", body: `{}`}}}
	ft := &failingTransport{failures: 1, next: m}
	client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: ft}), option.WithMaxRetries(2))
	if _, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}); err != nil {
		t.Fatal(err)
	}
	if ft.calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", ft.calls.Load())
	}
}

func TestPostNotRetriedAfterSending(t *testing.T) {
	m := &mockTransport{responses: []response{{status: 201, contentType: "application/json", body: `{}`}}}
	ft := &failingTransport{failures: 1, written: true, next: m}
	client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: ft}), option.WithMaxRetries(2))
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"})
	var connErr *didit.APIConnectionError
	if !errors.As(err, &connErr) {
		t.Fatalf("err = %v, want a connection error", err)
	}
	if ft.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", ft.calls.Load())
	}
	if strings.Contains(err.Error(), "?") {
		t.Errorf("error message contains a query string: %s", err)
	}
}

func TestGetRetriedAfterConnectionError(t *testing.T) {
	m := &mockTransport{responses: []response{{status: 200, contentType: "application/json", body: `{}`}}}
	ft := &failingTransport{failures: 2, written: true, next: m}
	client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: ft}), option.WithMaxRetries(2))
	if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ft.calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", ft.calls.Load())
	}
}

// blockingTransport waits until the request context ends.
type blockingTransport struct{ calls atomic.Int32 }

func (b *blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b.calls.Add(1)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestTimeout(t *testing.T) {
	bt := &blockingTransport{}
	client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: bt}), option.WithTimeout(20*time.Millisecond))
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"})
	var timeout *didit.APITimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("err = %v, want *APITimeoutError", err)
	}
	var conn *didit.APIConnectionError
	if !errors.As(err, &conn) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error does not unwrap: %v", err)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestCallerCancellationIsNotRetried(t *testing.T) {
	bt := &blockingTransport{}
	client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: bt}), option.WithMaxRetries(3))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := client.Billing.RetrieveBalance(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var conn *didit.APIConnectionError
	if !errors.As(err, &conn) {
		t.Fatalf("err = %T", err)
	}
	if bt.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", bt.calls.Load())
	}
}

// brokenBody fails while the body is read.
type brokenBodyTransport struct {
	calls atomic.Int32
	next  http.RoundTripper
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("unexpected EOF") }

func (b *brokenBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.calls.Add(1) == 1 {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(errReader{}), Request: req}, nil
	}
	return b.next.RoundTrip(req)
}

func TestBodyReadErrorIsAConnectionErrorAndRetried(t *testing.T) {
	m := &mockTransport{responses: []response{{status: 200, contentType: "application/json", body: `{"balance":"2"}`}}}
	bt := &brokenBodyTransport{next: m}
	client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: bt}), option.WithMaxRetries(1))
	if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Without retries the read error surfaces as an SDK error.
	bt = &brokenBodyTransport{next: m}
	client, _ = newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: bt}))
	_, err := client.Billing.RetrieveBalance(context.Background())
	var conn *didit.APIConnectionError
	if !errors.As(err, &conn) {
		t.Fatalf("err = %v, want *APIConnectionError", err)
	}
}

func TestPostBodyReadErrorNotRetried(t *testing.T) {
	m := &mockTransport{responses: []response{{status: 201, contentType: "application/json", body: `{}`}}}
	bt := &brokenBodyTransport{next: m}
	client, _ := newMockClient(t, 200, "", "", option.WithHTTPClient(&http.Client{Transport: bt}), option.WithMaxRetries(2))
	if _, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{WorkflowID: "w"}); err == nil {
		t.Fatal("want an error")
	}
	if bt.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", bt.calls.Load())
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	client, _ := newMockClient(t, 200, "text/html", "<html>maintenance</html>")
	_, err := client.Billing.RetrieveBalance(context.Background())
	var ue *didit.UnexpectedResponseError
	if !errors.As(err, &ue) || ue.Response == nil || ue.Response.StatusCode != 200 {
		t.Fatalf("err = %v, want *UnexpectedResponseError", err)
	}
	var base didit.Error
	if !errors.As(err, &base) {
		t.Fatal("does not match didit.Error")
	}
}

func TestLenientDecoding(t *testing.T) {
	body := `{
		"session_id": "s1",
		"session_number": "42",
		"status": "Some Future Status",
		"created_at": "2025-06-01T10:00:00",
		"metadata": null,
		"session_url": {"unexpected": "object"},
		"brand_new_field": {"a": 1}
	}`
	client, _ := newMockClient(t, 200, "application/json", body)
	res, err := client.Sessions.RetrieveDecision(context.Background(), "s1", didit.SessionsRetrieveDecisionParams{})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "s1" || res.SessionNumber != 42 {
		t.Errorf("decoded %q %d", res.SessionID, res.SessionNumber)
	}
	if string(res.Status) != "Some Future Status" {
		t.Errorf("unknown enum value lost: %q", res.Status)
	}
	if res.CreatedAt.IsZero() || res.CreatedAt.Hour() != 10 {
		t.Errorf("created_at = %v", res.CreatedAt)
	}
	if !res.JSON.Null("metadata") || !res.JSON.Present("metadata") {
		t.Error("metadata should be present and null")
	}
	if res.JSON.Present("expires_at") {
		t.Error("expires_at should be absent")
	}
	if !res.JSON.Invalid("session_url") {
		t.Error("session_url should be reported invalid")
	}
	if raw, ok := res.JSON.Field("session_url"); !ok || !strings.Contains(string(raw), "unexpected") {
		t.Errorf("raw invalid field = %s", raw)
	}
	if raw, ok := res.JSON.ExtraFields()["brand_new_field"]; !ok || string(raw) != `{"a": 1}` {
		t.Errorf("unknown field = %s", raw)
	}
	if !res.JSON.Present("brand_new_field") || res.JSON.Null("brand_new_field") {
		t.Error("Present/Null on unknown fields")
	}
	if !strings.Contains(res.JSON.Raw(), "brand_new_field") {
		t.Error("Raw() lost data")
	}
}

func TestPagination(t *testing.T) {
	page1 := `{"count":3,"next":"https://verification.test/v3/sessions/?limit=2&offset=2","previous":null,"results":[{"session_id":"a"},{"session_id":"b"}]}`
	page2 := `{"count":3,"next":null,"previous":"https://verification.test/v3/sessions/?limit=2","results":[{"session_id":"c"}]}`
	client, m := newMockClientWith(t, []response{
		{status: 200, contentType: "application/json", body: page1},
		{status: 200, contentType: "application/json", body: page2},
	})
	page, err := client.Sessions.List(context.Background(), didit.SessionsListParams{Limit: didit.Int(2)})
	if err != nil {
		t.Fatal(err)
	}
	if page.Count != 3 || len(page.Results) != 2 || !page.HasNextPage() {
		t.Fatalf("page = %+v", page)
	}
	next, err := page.GetNextPage(context.Background())
	if err != nil || next == nil || next.Results[0].SessionID != "c" || next.HasNextPage() {
		t.Fatalf("next = %+v, %v", next, err)
	}
	last, err := next.GetNextPage(context.Background())
	if last != nil || err != nil {
		t.Fatalf("after the last page: %v, %v", last, err)
	}
	reqs := m.all()
	if reqs[0].Query["limit"][0] != "2" || reqs[1].Query["offset"][0] != "2" {
		t.Errorf("queries %v %v", reqs[0].Query, reqs[1].Query)
	}
	if reqs[1].Header.Get("X-Api-Key") != testAPIKey {
		t.Error("next page sent without the API key")
	}
}

func TestAutoPaging(t *testing.T) {
	page1 := `{"count":3,"next":"/v3/sessions/?offset=2","previous":null,"results":[{"session_id":"a"},{"session_id":"b"}]}`
	page2 := `{"count":3,"next":null,"previous":null,"results":[{"session_id":"c"}]}`
	client, m := newMockClientWith(t, []response{
		{status: 200, contentType: "application/json", body: page1},
		{status: 200, contentType: "application/json", body: page2},
	})
	it := client.Sessions.ListAutoPaging(context.Background(), didit.SessionsListParams{})
	var ids []string
	for it.Next() {
		ids = append(ids, it.Current().SessionID)
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("ids = %v", ids)
	}
	if it.Page() == nil || it.Next() {
		t.Fatal("iterator state after the end")
	}
	if got := m.all()[1].Path; got != "/v3/sessions/" {
		t.Errorf("relative next link resolved to %s", got)
	}
}

func TestNextPageOnAnotherOriginIsRefused(t *testing.T) {
	for _, next := range []string{
		"https://evil.example/v3/sessions/?offset=2",
		"http://verification.test/v3/sessions/?offset=2",
		"https://verification.test:8443/v3/sessions/?offset=2",
	} {
		page1 := `{"count":3,"next":"` + next + `","previous":null,"results":[{"session_id":"a"}]}`
		client, m := newMockClient(t, 200, "application/json", page1)
		it := client.Sessions.ListAutoPaging(context.Background(), didit.SessionsListParams{})
		n := 0
		for it.Next() {
			n++
		}
		var ue *didit.UnexpectedResponseError
		if n != 1 || !errors.As(it.Err(), &ue) {
			t.Fatalf("%s: n = %d, err = %v", next, n, it.Err())
		}
		if len(m.all()) != 1 {
			t.Fatalf("%s: followed the link", next)
		}
	}
}

func TestPaginationCycleIsRefused(t *testing.T) {
	page := func(next, id string) response {
		return response{status: 200, contentType: "application/json", body: `{"count":9,"next":"` + next + `","previous":null,"results":[{"session_id":"` + id + `"}]}`}
	}
	for _, tc := range []struct {
		name   string
		params didit.SessionsListParams
		pages  []response
	}{
		{"a link to the same page", didit.SessionsListParams{}, []response{
			page("https://verification.test/v3/sessions/", "a"),
		}},
		{"a link back to the first page", didit.SessionsListParams{}, []response{
			page("/v3/sessions/?offset=1", "a"),
			page("https://verification.test/v3/sessions/", "b"),
		}},
		{"a link back to an earlier page", didit.SessionsListParams{Limit: didit.Int(1)}, []response{
			page("/v3/sessions/?limit=1&offset=1", "a"),
			page("/v3/sessions/?limit=1&offset=2", "b"),
			page("/v3/sessions/?limit=1&offset=1", "c"),
		}},
		{"the same page written differently", didit.SessionsListParams{Limit: didit.Int(1)}, []response{
			page("/v3/sessions/?limit=1&offset=1", "a"),
			page("HTTPS://Verification.TEST:443/v3/sessions/?offset=1&limit=1#again", "b"),
		}},
		{"an empty path written as /", didit.SessionsListParams{}, []response{
			page("https://verification.test?offset=1", "a"),
			page("https://verification.test/?offset=1", "b"),
		}},
	} {
		client, m := newMockClientWith(t, tc.pages)
		it := client.Sessions.ListAutoPaging(context.Background(), tc.params)
		n := 0
		for it.Next() {
			n++
		}
		var ue *didit.UnexpectedResponseError
		if !errors.As(it.Err(), &ue) || !strings.Contains(ue.Error(), "already fetched") {
			t.Fatalf("%s: err = %v", tc.name, it.Err())
		}
		if n != len(tc.pages) || len(m.all()) != len(tc.pages) {
			t.Fatalf("%s: items = %d, requests = %d, want %d", tc.name, n, len(m.all()), len(tc.pages))
		}
	}
}

func TestGetNextPageTwiceIsNotACycle(t *testing.T) {
	page1 := `{"count":2,"next":"/v3/sessions/?offset=1","previous":null,"results":[{"session_id":"a"}]}`
	page2 := `{"count":2,"next":null,"previous":"/v3/sessions/","results":[{"session_id":"b"}]}`
	client, m := newMockClientWith(t, []response{
		{status: 200, contentType: "application/json", body: page1},
		{status: 200, contentType: "application/json", body: page2},
	})
	first, err := client.Sessions.List(context.Background(), didit.SessionsListParams{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		next, err := first.GetNextPage(context.Background())
		if err != nil || next == nil || next.Results[0].SessionID != "b" {
			t.Fatalf("call %d: %+v, %v", i+1, next, err)
		}
	}
	if len(m.all()) != 3 {
		t.Fatalf("requests = %d", len(m.all()))
	}
}

func TestAutoPagingFirstPageError(t *testing.T) {
	client, _ := newMockClient(t, 403, "application/json", `{"detail":"no"}`)
	it := client.Sessions.ListAutoPaging(context.Background(), didit.SessionsListParams{})
	if it.Next() {
		t.Fatal("Next on an error")
	}
	var pd *didit.PermissionDeniedError
	if !errors.As(it.Err(), &pd) {
		t.Fatalf("err = %v", it.Err())
	}
}

func TestMultipartUploadIsBufferedForRetries(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 429, header: retryAfterZero},
		{status: 200, contentType: "application/json", body: `{"request_id":"r"}`},
	}, option.WithMaxRetries(1))
	reader := strings.NewReader("webp-bytes")
	_, err := client.IDVerification.Verify(context.Background(), didit.IDVerificationVerifyParams{
		FrontImage:              didit.FileFromReader(reader, "selfie.webp"),
		BackImage:               didit.Some(didit.FileFromBytes("back.JPG", []byte("jpg-bytes"))),
		PerformDocumentLiveness: didit.Bool(true),
		MinimumAge:              didit.Int(18),
		Metadata:                didit.Some(map[string]any{"k": "v"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	reqs := m.all()
	if len(reqs) != 2 || !bytes.Equal(reqs[0].Body, reqs[1].Body) {
		t.Fatalf("the retry did not resend the same body")
	}
	body := string(reqs[1].Body)
	for _, want := range []string{
		`name="front_image"; filename="selfie.webp"`, "Content-Type: image/webp", "webp-bytes",
		`name="back_image"; filename="back.JPG"`, "Content-Type: image/jpeg",
		`name="perform_document_liveness"`, "true", `name="minimum_age"`, `{"k":"v"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("multipart body lacks %q", want)
		}
	}
}

func TestFileFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, m := newMockClient(t, 200, "application/json", `{}`)
	_, err := client.ProofOfAddress.Verify(context.Background(), didit.ProofOfAddressVerifyParams{Document: didit.FileFromPath(path)})
	if err != nil {
		t.Fatal(err)
	}
	body := string(m.single(t).Body)
	if !strings.Contains(body, `filename="doc.pdf"`) || !strings.Contains(body, "Content-Type: application/pdf") {
		t.Errorf("body = %s", body)
	}
	_, err = client.ProofOfAddress.Verify(context.Background(), didit.ProofOfAddressVerifyParams{Document: didit.FileFromPath(filepath.Join(dir, "missing.pdf"))})
	var cfgErr *didit.ConfigurationError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("missing file: err = %v", err)
	}
}

func TestFileContentTypes(t *testing.T) {
	cases := map[string]didit.File{
		"image/png":                didit.FileFromBytes("a.png", []byte("x")),
		"image/heic":               didit.FileFromBytes("a.HEIC", []byte("x")),
		"text/csv":                 didit.FileFromBytes("import.csv", []byte("a,b")),
		"application/pdf":          didit.FileFromBytes("noext", []byte("%PDF-1.4 rest")),
		"application/x-custom":     didit.FileFromBytes("a.png", []byte("x")).WithContentType("application/x-custom"),
		"application/octet-stream": didit.FileFromBytes("blob", []byte{0x00, 0x01}),
	}
	for want, f := range cases {
		client, m := newMockClient(t, 200, "application/json", `{}`)
		if _, err := client.ProofOfAddress.Verify(context.Background(), didit.ProofOfAddressVerifyParams{Document: f.WithName(f.Name())}); err != nil {
			t.Fatal(err)
		}
		if body := string(m.single(t).Body); !strings.Contains(body, "Content-Type: "+want) {
			t.Errorf("%s: body = %s", want, body)
		}
	}
	if !(didit.File{}).IsZero() || didit.FileFromBytes("a", nil).IsZero() == false {
		t.Error("IsZero")
	}
}

func TestJSONOrMultipart(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 201, contentType: "application/json", body: `{}`},
	})
	params := didit.SessionsImportsCreateParams{WorkflowID: didit.String("w"), SourceFileURL: didit.String("https://example.com/a.csv")}
	if _, err := client.Sessions.Imports.Create(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	params.SourceFileURL = didit.Opt[string]{}
	params.File = didit.Some(didit.FileFromBytes("import.csv", []byte("a,b")))
	if _, err := client.Sessions.Imports.Create(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	reqs := m.all()
	if ct := reqs[0].Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("without a file: Content-Type = %q", ct)
	}
	if ct := reqs[1].Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Errorf("with a file: Content-Type = %q", ct)
	}
}

func TestBinaryResponseIsStreamed(t *testing.T) {
	client, _ := newMockClient(t, 200, "application/pdf", "%PDF-1.7 report", option.WithTimeout(time.Second))
	res, err := client.Sessions.GeneratePDF(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil || string(data) != "%PDF-1.7 report" || res.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("body = %q, err = %v", data, err)
	}
}

func TestBinaryErrorResponse(t *testing.T) {
	client, _ := newMockClient(t, 404, "application/json", `{"detail":"not found"}`)
	_, err := client.Sessions.GeneratePDF(context.Background(), "s1")
	var nf *didit.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v", err)
	}
}

func TestCrossOriginRedirectIsNotFollowed(t *testing.T) {
	client, m := newMockClient(t, 302, "", "", option.WithHeader("Location", ""))
	m.responses[0].header = http.Header{"Location": {"https://evil.example/steal"}}
	_, err := client.Billing.RetrieveBalance(context.Background())
	var se *didit.APIStatusError
	if !errors.As(err, &se) || se.StatusCode != 302 {
		t.Fatalf("err = %v, want the 302 as an error", err)
	}
	if len(m.all()) != 1 {
		t.Fatal("the redirect was followed")
	}
}

func TestSameOriginRedirectIsFollowed(t *testing.T) {
	client, m := newMockClientWith(t, []response{
		{status: 301, header: http.Header{"Location": {"https://verification.test/v3/billing/balance/"}}},
		{status: 200, contentType: "application/json", body: `{}`},
	})
	if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	reqs := m.all()
	if len(reqs) != 2 || reqs[1].Header.Get("X-Api-Key") != testAPIKey {
		t.Fatalf("requests = %d", len(reqs))
	}
}

func TestResponseInto(t *testing.T) {
	client, _ := newMockClientWith(t, []response{{status: 200, contentType: "application/json", body: `{"balance":"3"}`, header: http.Header{"X-Request-Id": {"rid"}}}})
	var raw *http.Response
	if _, err := client.Billing.RetrieveBalance(context.Background(), option.WithResponseInto(&raw)); err != nil {
		t.Fatal(err)
	}
	if raw == nil || raw.StatusCode != 200 || raw.Header.Get("X-Request-Id") != "rid" {
		t.Fatalf("raw = %+v", raw)
	}
	data, _ := io.ReadAll(raw.Body)
	if string(data) != `{"balance":"3"}` {
		t.Errorf("raw body = %s", data)
	}
	// Also on errors.
	client, _ = newMockClient(t, 404, "application/json", `{}`)
	raw = nil
	_, _ = client.Billing.RetrieveBalance(context.Background(), option.WithResponseInto(&raw))
	if raw == nil || raw.StatusCode != 404 {
		t.Fatal("raw response not set on error")
	}
}

func TestDebugLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client, _ := newMockClientWith(t, []response{{status: 200, contentType: "application/json", body: `{"secret":"body"}`, header: http.Header{"X-Request-Id": {"rid-9"}}}}, option.WithLogger(logger))
	if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"method=GET", "path=/v3/billing/balance/", "status=200", "request_id=rid-9", "duration="} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q: %s", want, out)
		}
	}
	if strings.Contains(out, testAPIKey) || strings.Contains(out, "secret") {
		t.Errorf("log leaks the key or the body: %s", out)
	}
}

func TestDebugLoggingFromEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("DIDIT_LOG", "debug")
	m := &mockTransport{responses: []response{{status: 200, contentType: "application/json", body: `{}`}}}
	client := didit.NewClient(option.WithAPIKey("k"), option.WithHTTPClient(&http.Client{Transport: m}))
	if _, err := client.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOptionErrors(t *testing.T) {
	for name, opt := range map[string]option.RequestOption{
		"base url scheme":    option.WithBaseURL("ftp://x"),
		"base url creds":     option.WithBaseURL("https://user:pass@x.test"),
		"base url query":     option.WithBaseURL("https://x.test/?a=1"),
		"base url host":      option.WithBaseURL("https://"),
		"account url":        option.WithAccountBaseURL("nope"),
		"negative timeout":   option.WithTimeout(-time.Second),
		"negative retries":   option.WithMaxRetries(-1),
		"nil http client":    option.WithHTTPClient(nil),
		"bad url characters": option.WithBaseURL("https://x.test/%zz"),
	} {
		client, m := newMockClient(t, 200, "application/json", `{}`)
		_, err := client.Billing.RetrieveBalance(context.Background(), opt)
		var cfgErr *didit.ConfigurationError
		if !errors.As(err, &cfgErr) {
			t.Errorf("%s: err = %v", name, err)
		}
		if len(m.all()) != 0 {
			t.Errorf("%s: request sent", name)
		}
		if strings.Contains(err.Error(), "pass") {
			t.Errorf("%s: credentials in the message: %s", name, err)
		}
	}
}

func TestPerRequestOptionsAndWithOptions(t *testing.T) {
	client, m := newMockClient(t, 200, "application/json", `{}`)
	other := client.WithOptions(option.WithAPIKey("other-key"), option.WithHeaderAdd("X-Trace", "1"))
	if _, err := other.Billing.RetrieveBalance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Billing.RetrieveBalance(context.Background(), option.WithQuery("extra", "1")); err != nil {
		t.Fatal(err)
	}
	reqs := m.all()
	if reqs[0].Header.Get("X-Api-Key") != "other-key" || reqs[0].Header.Get("X-Trace") != "1" {
		t.Error("WithOptions not applied")
	}
	if reqs[1].Header.Get("X-Api-Key") != testAPIKey || reqs[1].Header.Get("X-Trace") != "" {
		t.Error("WithOptions changed the original client")
	}
	if reqs[1].Query["extra"][0] != "1" {
		t.Error("WithQuery not applied")
	}
}

func TestAccountAPI(t *testing.T) {
	clearEnv(t)
	m := &mockTransport{responses: []response{{status: 200, contentType: "application/json", body: `{"access_token":"t"}`}}}
	client := didit.NewClient(option.WithAPIKey("k"), option.WithHTTPClient(&http.Client{Transport: m}))
	if _, err := client.Account.Login(context.Background(), didit.LoginRequest{Email: "a@example.com", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	req := m.single(t)
	if req.URL != "https://apx.didit.me/auth/v2/programmatic/login/" {
		t.Errorf("url = %s", req.URL)
	}
	if req.Header.Get("X-Api-Key") != "" {
		t.Error("the API key was sent to the account API")
	}
	_, err := client.Account.Organizations.List(context.Background(), didit.AccountOrganizationsListParams{})
	var cfgErr *didit.ConfigurationError
	if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "WithAccountToken") {
		t.Fatalf("err = %v", err)
	}
}

func TestOptValues(t *testing.T) {
	var o didit.Opt[string]
	if !o.IsOmitted() || o.IsSet() || o.IsNull() || o.String() != "omitted" || o.Or("x") != "x" {
		t.Error("zero Opt")
	}
	o = didit.String("")
	if v, ok := o.Value(); !ok || v != "" || o.String() != "" || !o.IsSet() {
		t.Error("Opt holding the empty string")
	}
	n := didit.Null[int64]()
	if !n.IsNull() || n.IsSet() || n.String() != "null" {
		t.Error("null Opt")
	}
	if b, _ := n.MarshalJSON(); string(b) != "null" {
		t.Error("null MarshalJSON")
	}
	var u didit.Opt[int64]
	if err := u.UnmarshalJSON([]byte("5")); err != nil || u.Or(0) != 5 {
		t.Error("UnmarshalJSON value")
	}
	if err := u.UnmarshalJSON([]byte("null")); err != nil || !u.IsNull() {
		t.Error("UnmarshalJSON null")
	}
	if err := u.UnmarshalJSON([]byte(`"x"`)); err == nil {
		t.Error("UnmarshalJSON wrong type")
	}
	if didit.Float(1.5).Or(0) != 1.5 || !didit.Bool(false).IsSet() || didit.Time(time.Unix(0, 0)).IsOmitted() {
		t.Error("constructors")
	}
}

func TestParamsEncoding(t *testing.T) {
	client, m := newMockClient(t, 201, "application/json", `{}`)
	_, err := client.Sessions.Create(context.Background(), didit.SessionsCreateParams{
		WorkflowID: "w",
		VendorData: didit.String(""),
		Callback:   didit.Null[string](),
		Metadata:   didit.Some[any](map[string]any{"html": "<b>&</b>"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	m.single(t).expectJSON(t, `{"workflow_id":"w","vendor_data":"","callback":null,"metadata":{"html":"<b>&</b>"}}`)
	if !bytes.Contains(m.single(t).Body, []byte("<b>&</b>")) {
		t.Error("HTML characters were escaped")
	}
}

func TestQueryEncoding(t *testing.T) {
	client, m := newMockClient(t, 200, "application/json", `{"count":0,"next":null,"previous":null,"results":[]}`)
	_, err := client.Sessions.List(context.Background(), didit.SessionsListParams{
		Limit:  didit.Int(5),
		Offset: didit.Null[int64](),
	})
	if err != nil {
		t.Fatal(err)
	}
	q := m.single(t).Query
	if q["limit"][0] != "5" {
		t.Errorf("limit = %v", q["limit"])
	}
	if _, ok := q["offset"]; ok {
		t.Error("a null query parameter was sent")
	}
}

func TestHeaderParameter(t *testing.T) {
	client, m := newMockClient(t, 200, "application/json", `{}`)
	_, err := client.AML.Screen(context.Background(), didit.AMLScreenParams{FullName: "Jane Doe", IdempotencyKey: didit.String("idem-42")})
	if err != nil {
		t.Fatal(err)
	}
	req := m.single(t)
	if req.Header.Get("Idempotency-Key") != "idem-42" {
		t.Errorf("Idempotency-Key = %q", req.Header.Get("Idempotency-Key"))
	}
	if bytes.Contains(req.Body, []byte("idem-42")) {
		t.Error("a header parameter leaked into the body")
	}
}
