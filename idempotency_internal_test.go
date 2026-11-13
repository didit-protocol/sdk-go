package didit

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/didit-protocol/sdk-go/internal/requestconfig"
	"github.com/didit-protocol/sdk-go/option"
)

// scriptedTransport answers with the canned (status, header, body) in order and
// records the requests.
type scriptedTransport struct {
	mu       sync.Mutex
	statuses []int
	headers  []http.Header
	bodies   []string
	seen     []*http.Request
}

func (s *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := min(len(s.seen), len(s.statuses)-1)
	s.seen = append(s.seen, req)
	h := http.Header{"Content-Type": {"application/json"}}
	if i < len(s.headers) {
		for k, v := range s.headers[i] {
			h[k] = v
		}
	}
	return &http.Response{StatusCode: s.statuses[i], Header: h, Request: req,
		Body: readCloser(s.bodies[i])}, nil
}

func readCloser(s string) *nopCloser { return &nopCloser{strings.NewReader(s)} }

type nopCloser struct{ *strings.Reader }

func (*nopCloser) Close() error { return nil }

func idempotencyClient(tr http.RoundTripper, opts ...option.RequestOption) *Client {
	base := []option.RequestOption{
		option.WithAPIKey("test-api-key"),
		option.WithBaseURL("https://verification.test"),
		option.WithHTTPClient(&http.Client{Transport: tr}),
	}
	return NewClient(append(base, opts...)...)
}

func TestIdempotencyKeyHeaderOnEveryVerb(t *testing.T) {
	for _, tc := range []struct {
		method string
		want   bool
	}{{"POST", true}, {"PUT", true}, {"PATCH", true}, {"DELETE", true}, {"GET", false}, {"HEAD", false}} {
		tr := &scriptedTransport{statuses: []int{200}, bodies: []string{`{}`}}
		client := idempotencyClient(tr)
		_, err := client.do(context.Background(), &apiRequest{method: tc.method, path: "/v3/x/", api: apiVerification, auth: authAPIKey}, nil,
			[]option.RequestOption{option.WithIdempotencyKey("key-1")})
		if err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
		got := tr.seen[0].Header.Get("Idempotency-Key")
		if (got == "key-1") != tc.want || (!tc.want && got != "") {
			t.Errorf("%s: Idempotency-Key = %q, sent want %v", tc.method, got, tc.want)
		}
	}
}

func TestIdempotencyKeyPatchRetriedOnServerError(t *testing.T) {
	for _, withKey := range []bool{true, false} {
		tr := &scriptedTransport{statuses: []int{503, 200}, bodies: []string{`{}`, `{}`}}
		client := idempotencyClient(tr, option.WithMaxRetries(2))
		opts := []option.RequestOption{func(c *requestconfig.Config) error {
			c.Sleep = func(time.Duration) <-chan time.Time { return instant() }
			return nil
		}}
		if withKey {
			opts = append(opts, option.WithIdempotencyKey("key-2"))
		}
		_, err := client.do(context.Background(), &apiRequest{method: "PATCH", path: "/v3/x/", api: apiVerification, auth: authAPIKey}, nil, opts)
		if withKey {
			if err != nil || len(tr.seen) != 2 {
				t.Fatalf("with a key: err = %v, attempts = %d, want success after 2", err, len(tr.seen))
			}
			for _, r := range tr.seen {
				if r.Header.Get("Idempotency-Key") != "key-2" {
					t.Errorf("an attempt lost the key")
				}
			}
			continue
		}
		if err == nil || len(tr.seen) != 1 {
			t.Fatalf("without a key: err = %v, attempts = %d, want an error after 1", err, len(tr.seen))
		}
	}
}

func instant() <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- time.Now()
	return c
}

func TestIdempotencyKeyInProgressHonoursRetryAfter(t *testing.T) {
	tr := &scriptedTransport{
		statuses: []int{409, 201},
		headers:  []http.Header{{"Retry-After": {"1"}}},
		bodies:   []string{`{"detail":"busy","code":"idempotency_key_in_progress"}`, `{}`},
	}
	var waited []time.Duration
	opts := []option.RequestOption{
		func(c *requestconfig.Config) error {
			c.Sleep = func(d time.Duration) <-chan time.Time {
				waited = append(waited, d)
				return instant()
			}
			return nil
		},
		option.WithIdempotencyKey("key-3"),
	}
	client := idempotencyClient(tr, option.WithMaxRetries(2))
	_, err := client.do(context.Background(), &apiRequest{method: "POST", path: "/v3/x/", api: apiVerification, auth: authAPIKey}, nil, opts)
	if err != nil || len(tr.seen) != 2 {
		t.Fatalf("err = %v, attempts = %d, want success after 2", err, len(tr.seen))
	}
	if len(waited) != 1 || waited[0] != time.Second {
		t.Errorf("waited %v, want [1s] from Retry-After", waited)
	}
	if tr.seen[1].Header.Get("Idempotency-Key") != "key-3" {
		t.Error("the retry lost the key")
	}
}

func TestIdempotencyKeyNotSentOnOtherMethods(t *testing.T) {
	for _, method := range []string{"OPTIONS", "HEAD", "GET", "TRACE"} {
		tr := &scriptedTransport{statuses: []int{200}, bodies: []string{`{}`}}
		client := idempotencyClient(tr)
		_, err := client.do(context.Background(), &apiRequest{method: method, path: "/v3/x/", api: apiVerification, auth: authAPIKey}, nil,
			[]option.RequestOption{option.WithIdempotencyKey("key-4"), option.WithHeader("Idempotency-Key", "raw-key")})
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if got := tr.seen[0].Header.Values("Idempotency-Key"); len(got) != 0 {
			t.Errorf("%s: sent Idempotency-Key %v", method, got)
		}
	}
}

func TestClientDefaultIdempotencyKeyHeaderIsIgnored(t *testing.T) {
	post := &apiRequest{method: "POST", path: "/v3/x/", api: apiVerification, auth: authAPIKey}
	noWait := func(c *requestconfig.Config) error {
		c.Sleep = func(time.Duration) <-chan time.Time { return instant() }
		return nil
	}
	for name, build := range map[string]func(tr http.RoundTripper) *Client{
		"NewClient": func(tr http.RoundTripper) *Client {
			return idempotencyClient(tr, option.WithMaxRetries(2), option.WithHeader("idempotency-key", "default-key"))
		},
		"WithOptions": func(tr http.RoundTripper) *Client {
			return idempotencyClient(tr, option.WithMaxRetries(2)).WithOptions(option.WithHeader("Idempotency-Key", "default-key"))
		},
	} {
		// Not sent, and a 5xx on POST is not retried.
		tr := &scriptedTransport{statuses: []int{503, 200}, bodies: []string{`{}`, `{}`}}
		_, err := build(tr).do(context.Background(), post, nil, []option.RequestOption{noWait})
		if err == nil || len(tr.seen) != 1 {
			t.Fatalf("%s: err = %v, attempts = %d, want an error after 1", name, err, len(tr.seen))
		}
		if got := tr.seen[0].Header.Values("Idempotency-Key"); len(got) != 0 {
			t.Errorf("%s: default header sent: %v", name, got)
		}
		// The per-request option still works on that client.
		tr = &scriptedTransport{statuses: []int{503, 200}, bodies: []string{`{}`, `{}`}}
		_, err = build(tr).do(context.Background(), post, nil, []option.RequestOption{noWait, option.WithIdempotencyKey("key-5")})
		if err != nil || len(tr.seen) != 2 || tr.seen[0].Header.Get("Idempotency-Key") != "key-5" {
			t.Errorf("%s: err = %v, attempts = %d", name, err, len(tr.seen))
		}
		// A raw per-request header counts.
		tr = &scriptedTransport{statuses: []int{503, 200}, bodies: []string{`{}`, `{}`}}
		_, err = build(tr).do(context.Background(), post, nil, []option.RequestOption{noWait, option.WithHeader("Idempotency-Key", "raw-key")})
		if err != nil || len(tr.seen) != 2 || tr.seen[1].Header.Get("Idempotency-Key") != "raw-key" {
			t.Errorf("%s raw: err = %v, attempts = %d", name, err, len(tr.seen))
		}
	}
}
