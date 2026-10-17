package authentication

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

// Synthetic values only: no real issuer, application or person.
const (
	testIssuer   = "https://acme.auth.didit.me"
	testAudience = "0b5d3a52-6c3e-4f7b-9a51-3c2f1e0d9a11"
)

var testNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// fakeClock is a settable clock for WithClock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock { return &fakeClock{now: testNow} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testKey is an ES256 key pair generated at test time; keys are never
// committed.
type testKey struct {
	kid  string
	priv *ecdsa.PrivateKey
}

func newTestKey(t testing.TB, kid string) *testKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &testKey{kid: kid, priv: priv}
}

// point returns the uncompressed public point 0x04 || X || Y.
func (k *testKey) point(t testing.TB) []byte {
	t.Helper()
	pub, err := k.priv.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	return pub.Bytes()
}

func (k *testKey) jwk(t testing.TB) map[string]any {
	p := k.point(t)
	return map[string]any{
		"kty": "EC", "crv": "P-256", "kid": k.kid, "use": "sig", "alg": "ES256",
		"x": b64(p[1:33]), "y": b64(p[33:]),
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// remove deletes a header member in mint.
type removeMarker struct{}

var remove = removeMarker{}

// signingInput returns base64url(header) "." base64url(claims) with the
// default header {alg ES256, typ at+jwt, kid} changed by header.
func signingInput(t testing.TB, kid string, header, claims map[string]any) string {
	t.Helper()
	h := map[string]any{"alg": "ES256", "typ": "at+jwt", "kid": kid}
	for name, value := range header {
		if value == remove {
			delete(h, name)
		} else {
			h[name] = value
		}
	}
	return b64(mustJSON(t, h)) + "." + b64(mustJSON(t, claims))
}

// rawSignature returns the 64-byte R || S ES256 signature of input.
func (k *testKey) rawSignature(t testing.TB, input string) []byte {
	t.Helper()
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig
}

// mint returns an ES256 JWS signed by k.
func (k *testKey) mint(t testing.TB, header, claims map[string]any) string {
	t.Helper()
	input := signingInput(t, k.kid, header, claims)
	return input + "." + b64(k.rawSignature(t, input))
}

func accessClaims() map[string]any {
	return map[string]any{
		"iss":         testIssuer,
		"aud":         testAudience,
		"client_id":   testAudience,
		"sub":         "6f0f7a43-1c64-4b1e-8d1f-7f0e8f4a2b10",
		"sid":         "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
		"jti":         "jti-access-1",
		"iat":         testNow.Unix(),
		"exp":         testNow.Add(5 * time.Minute).Unix(),
		"vendor_data": "customer-42",
		"amr":         []string{"passkey"},
		"org":         map[string]any{"id": "org-1", "role": "admin"},
		"verified":    map[string]any{"level": "full_id", "over_18": true, "given_name": "Ada", "country": "ES"},
		"act":         nil,
	}
}

func idClaims() map[string]any {
	c := accessClaims()
	delete(c, "client_id")
	c["jti"] = "jti-id-1"
	c["auth_time"] = testNow.Add(-time.Hour).Unix()
	c["email"] = "person@example.test"
	c["email_verified"] = true
	c["name"] = "Ada Example"
	c["phone_number"] = "+34600000000"
	c["nonce"] = "nonce-1"
	return c
}

func with(claims map[string]any, changes map[string]any) map[string]any {
	out := make(map[string]any, len(claims))
	for k, v := range claims {
		out[k] = v
	}
	for k, v := range changes {
		if v == remove {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

// recorded is one request the stub issuer received.
type recorded struct {
	method string
	host   string
	path   string
	header http.Header
	body   string
}

// stubIssuer implements an issuer as an http.RoundTripper: no sockets, no
// network. Handlers can be replaced per path.
type stubIssuer struct {
	t      testing.TB
	issuer string

	mu           sync.Mutex
	keys         []*testKey
	cacheControl string
	discovery    map[string]any
	handlers     map[string]http.HandlerFunc
	transportErr func(*http.Request) error
	requests     []recorded
}

func newStub(t testing.TB, keys ...*testKey) *stubIssuer {
	return &stubIssuer{
		t:            t,
		issuer:       testIssuer,
		keys:         keys,
		cacheControl: "public, max-age=300",
		discovery: map[string]any{
			"issuer":                                testIssuer,
			"authorization_endpoint":                testIssuer + "/sign-in",
			"token_endpoint":                        testIssuer + "/token/",
			"jwks_uri":                              testIssuer + "/.well-known/jwks.json",
			"id_token_signing_alg_values_supported": []string{"ES256"},
		},
		handlers: map[string]http.HandlerFunc{},
	}
}

func (s *stubIssuer) handle(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[path] = h
}

func (s *stubIssuer) setKeys(keys ...*testKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = keys
}

func (s *stubIssuer) setDiscovery(changes map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discovery = with(s.discovery, changes)
}

func (s *stubIssuer) setTransportError(f func(*http.Request) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transportErr = f
}

// count returns how many requests went to path.
func (s *stubIssuer) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.requests {
		if r.path == path {
			n++
		}
	}
	return n
}

func (s *stubIssuer) last(path string) recorded {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.requests) - 1; i >= 0; i-- {
		if s.requests[i].path == path {
			return s.requests[i]
		}
	}
	s.t.Fatalf("no request to %s", path)
	return recorded{}
}

func (s *stubIssuer) all() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.requests...)
}

func (s *stubIssuer) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		req.Body.Close()
	}
	s.mu.Lock()
	s.requests = append(s.requests, recorded{
		method: req.Method, host: req.URL.Host, path: req.URL.Path, header: req.Header.Clone(), body: string(body),
	})
	transportErr := s.transportErr
	handler := s.handlers[req.URL.Path]
	s.mu.Unlock()
	if transportErr != nil {
		if err := transportErr(req); err != nil {
			return nil, err
		}
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if handler == nil {
		handler = s.defaultHandler
	}
	// Handlers get a copy with a fresh body: a RoundTripper must not modify
	// the request.
	served := req.Clone(req.Context())
	served.Body = io.NopCloser(bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, served)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *stubIssuer) defaultHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cacheControl := s.cacheControl
	discovery := s.discovery
	keys := append([]*testKey(nil), s.keys...)
	s.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == discoveryPath:
		w.Header().Set("Cache-Control", cacheControl)
		writeJSON(w, http.StatusOK, discovery)
	case r.Method == http.MethodGet && r.URL.Path == jwksPath:
		set := []any{}
		for _, k := range keys {
			set = append(set, k.jwk(s.t))
		}
		w.Header().Set("Cache-Control", cacheControl)
		writeJSON(w, http.StatusOK, map[string]any{"keys": set})
	case r.Method == http.MethodPost && r.URL.Path == "/token/":
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  "access-token-value",
			"id_token":      "id-token-value",
			"refresh_token": "refresh-token-value-2",
			"token_type":    "Bearer",
			"expires_in":    300,
			"session_id":    "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
		})
	default:
		w.Header().Set("X-Request-Id", "req-404")
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Not found."})
	}
}

// newTestClient returns a Client for the stub with a fake clock and a 1 ms
// retry delay.
func newTestClient(t testing.TB, stub *stubIssuer, clock *fakeClock, opts ...Option) *Client {
	t.Helper()
	base := []Option{WithHTTPClient(&http.Client{Transport: stub}), WithClock(clock.Now)}
	c, err := New(stub.issuer, append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	c.retryDelay = time.Millisecond
	return c
}

// wantReason fails unless err is a *didit.TokenVerificationError with reason.
func wantReason(t testing.TB, err error, reason didit.TokenVerificationReason) {
	t.Helper()
	var tokenErr *didit.TokenVerificationError
	if !errors.As(err, &tokenErr) {
		t.Fatalf("error = %v (%T), want TokenVerificationError %s", err, err, reason)
	}
	if tokenErr.Reason != reason {
		t.Fatalf("reason = %s (%v), want %s", tokenErr.Reason, err, reason)
	}
}

// wantConfigError fails unless err is a *didit.ConfigurationError whose
// message contains substr.
func wantConfigError(t testing.TB, err error, substr string) {
	t.Helper()
	var cfg *didit.ConfigurationError
	if !errors.As(err, &cfg) {
		t.Fatalf("error = %v (%T), want ConfigurationError", err, err)
	}
	if !strings.Contains(cfg.Message, substr) {
		t.Fatalf("message = %q, want it to contain %q", cfg.Message, substr)
	}
}

// timeoutError is a net.Error that timed out.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
