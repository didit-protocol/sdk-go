package authentication

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/didit-protocol/sdk-go/internal/version"
)

func TestNewValidatesIssuer(t *testing.T) {
	valid := map[string]string{
		"https://acme.auth.didit.me":          "https://acme.auth.didit.me",
		"https://acme.auth.didit.me/":         "https://acme.auth.didit.me",
		"https://login.example.com":           "https://login.example.com",
		"https://login.example.com:8443":      "https://login.example.com:8443",
		"https://example.com/tenant":          "https://example.com/tenant",
		"http://localhost:8000":               "http://localhost:8000",
		"http://acme.sandbox.auth.localhost/": "http://acme.sandbox.auth.localhost",
		"http://127.0.0.1:8000":               "http://127.0.0.1:8000",
		"http://[::1]:8000":                   "http://[::1]:8000",
	}
	for in, want := range valid {
		c, err := New(in)
		if err != nil {
			t.Fatalf("New(%q): %v", in, err)
		}
		if c.Issuer() != want {
			t.Fatalf("New(%q).Issuer() = %q, want %q", in, c.Issuer(), want)
		}
	}
	invalid := map[string]string{
		"":                                     "issuer is required",
		"acme.auth.didit.me":                   "absolute URL",
		"/relative":                            "absolute URL",
		"https:acme":                           "absolute URL",
		"https://":                             "absolute URL",
		"http://acme.auth.didit.me":            "must use https",
		"http://localhost.example.com":         "must use https",
		"http://127.0.0.2":                     "must use https",
		"ftp://acme.auth.didit.me":             "must use https",
		"https://user:pass@acme.auth.didit.me": "credentials",
		"https://acme.auth.didit.me?x=1":       "query",
		"https://acme.auth.didit.me?":          "query",
		"https://acme.auth.didit.me#frag":      "fragment",
		"https://acme.auth.didit.me#":          "fragment",
		"https://acme.auth.didit.me:0":         "port",
		"https://acme .auth.didit.me":          "whitespace",
		"https://acme.auth.didit.me\n":         "whitespace",
		"https://acme.auth.didit.me:99999":     "port",
		"https://[::1":                         "absolute URL",
	}
	for in, want := range invalid {
		_, err := New(in)
		wantConfigError(t, err, want)
	}
}

func TestNewOptions(t *testing.T) {
	_, err := New(testIssuer, WithLeeway(-time.Second))
	wantConfigError(t, err, "leeway")

	c, err := New(testIssuer, nil, WithHTTPClient(nil), WithClock(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.leeway != DefaultLeeway || c.http.Timeout != DefaultTimeout || c.clock == nil || c.retryDelay != defaultRetryDelay {
		t.Fatalf("defaults not applied: %+v", c)
	}

	// The caller's client is copied, never modified.
	own := &http.Client{Timeout: 5 * time.Second}
	c, err = New(testIssuer, WithHTTPClient(own), WithClientID(testAudience), WithLeeway(0))
	if err != nil {
		t.Fatal(err)
	}
	if own.CheckRedirect != nil || c.http == own || c.http.Timeout != 5*time.Second || c.clientID != testAudience || c.leeway != 0 {
		t.Fatal("options not applied as documented")
	}
}

func TestParsePublishableKey(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

	issuer, live, err := ParsePublishableKey(livePrefix + enc(testIssuer))
	if err != nil || issuer != testIssuer || !live {
		t.Fatalf("live key: %q %v %v", issuer, live, err)
	}
	issuer, live, err = ParsePublishableKey("  " + testPrefix + enc("https://acme.sandbox.auth.didit.me") + "\n")
	if err != nil || issuer != "https://acme.sandbox.auth.didit.me" || live {
		t.Fatalf("test key: %q %v %v", issuer, live, err)
	}
	// Padding is tolerated.
	padded := testPrefix + base64.URLEncoding.EncodeToString([]byte("https://abc.auth.didit.me"))
	if !strings.HasSuffix(padded, "=") {
		t.Fatal("test value is not padded")
	}
	if issuer, _, err = ParsePublishableKey(padded); err != nil || issuer != "https://abc.auth.didit.me" {
		t.Fatalf("padded key: %q %v", issuer, err)
	}
	// Sandbox keys may carry an http loopback issuer for local development.
	if issuer, _, err = ParsePublishableKey(testPrefix + enc("http://acme.sandbox.auth.localhost:8000")); err != nil ||
		issuer != "http://acme.sandbox.auth.localhost:8000" {
		t.Fatalf("local key: %q %v", issuer, err)
	}

	invalid := map[string]string{
		"":                                         "must start with pk_live_ or pk_test_",
		"sk_live_" + enc(testIssuer):               "must start with pk_live_ or pk_test_",
		"pk_prod_" + enc(testIssuer):               "must start with pk_live_ or pk_test_",
		"PK_LIVE_" + enc(testIssuer):               "must start with pk_live_ or pk_test_",
		livePrefix:                                 "not valid base64url",
		livePrefix + "===":                         "not valid base64url",
		livePrefix + "aHR0cHM6Ly9h+mU/":            "not valid base64url",
		livePrefix + "aHR0cHM6Ly9h\nZQ":            "not valid base64url",
		livePrefix + "A":                           "not valid base64url",
		livePrefix + enc("\xff\xfe"):               "not valid base64url",
		livePrefix + enc("http://localhost:8000"):  "a live publishable key must carry an https issuer",
		testPrefix + enc("http://acme.example"):    "does not carry a valid issuer: issuer must use https",
		livePrefix + enc("https://acme.example?x"): "does not carry a valid issuer: issuer must not contain a query",
	}
	for key, want := range invalid {
		_, _, err := ParsePublishableKey(key)
		wantConfigError(t, err, want)
	}
}

func TestNewFromPublishableKey(t *testing.T) {
	key := livePrefix + base64.RawURLEncoding.EncodeToString([]byte(testIssuer))
	c, err := NewFromPublishableKey(key, WithClientID(testAudience))
	if err != nil {
		t.Fatal(err)
	}
	if c.Issuer() != testIssuer || c.clientID != testAudience {
		t.Fatalf("client = %+v", c)
	}
	_, err = NewFromPublishableKey("pk_nope")
	wantConfigError(t, err, "pk_live_")
}

func TestCreatePKCE(t *testing.T) {
	charset := regexp.MustCompile(`\A[A-Za-z0-9_-]{43,128}\z`) // RFC 7636 section 4.1, within base64url
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p, err := CreatePKCE()
		if err != nil {
			t.Fatal(err)
		}
		if !charset.MatchString(p.Verifier) || len(p.Verifier) != 43 {
			t.Fatalf("verifier %q", p.Verifier)
		}
		sum := sha256.Sum256([]byte(p.Verifier))
		if p.Challenge != base64.RawURLEncoding.EncodeToString(sum[:]) || len(p.Challenge) != 43 {
			t.Fatalf("challenge %q does not match the verifier", p.Challenge)
		}
		if p.Method != "S256" {
			t.Fatalf("method %q", p.Method)
		}
		if seen[p.Verifier] {
			t.Fatal("repeated verifier")
		}
		seen[p.Verifier] = true
	}
	// Known answer from RFC 7636 appendix B.
	sum := sha256.Sum256([]byte("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge derivation = %q", got)
	}
}

func TestCreateStateAndNonce(t *testing.T) {
	state, err := CreateState()
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := CreateNonce()
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 43 || len(nonce) != 43 || state == nonce || !isBase64URL(state) || !isBase64URL(nonce) {
		t.Fatalf("state %q nonce %q", state, nonce)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestRandomFailure(t *testing.T) {
	previous := randReader
	randReader = failingReader{}
	t.Cleanup(func() { randReader = previous })
	if _, err := CreatePKCE(); err == nil {
		t.Fatal("CreatePKCE succeeded without randomness")
	}
	_, err := CreateState()
	wantConfigError(t, err, "entropy unavailable")
	_, err = CreateNonce()
	wantConfigError(t, err, "random")
}

func TestUserAgent(t *testing.T) {
	re := regexp.MustCompile(`\Adidit-go/` + regexp.QuoteMeta(version.Version) + ` \(Go [^;]+; [a-z0-9]+/[a-z0-9]+\)\z`)
	if !re.MatchString(userAgent) {
		t.Fatalf("User-Agent %q", userAgent)
	}
	if clientHeader != "go/"+version.Version {
		t.Fatalf("X-Didit-Client %q", clientHeader)
	}
}

func TestCacheTTL(t *testing.T) {
	tests := []struct {
		cacheControl []string
		age          string
		want         time.Duration
	}{
		{nil, "", 5 * time.Minute},
		{[]string{"public, max-age=300"}, "", 5 * time.Minute},
		{[]string{"max-age=120"}, "", 2 * time.Minute},
		{[]string{`max-age="600"`}, "", 10 * time.Minute},
		{[]string{"public", "MAX-AGE=900"}, "", 15 * time.Minute},
		{[]string{"max-age=10"}, "", time.Minute},
		{[]string{"max-age=0"}, "", time.Minute},
		{[]string{"max-age=9999999999999"}, "", 24 * time.Hour},
		{[]string{"max-age=abc"}, "", 5 * time.Minute},
		{[]string{"max-age=-5"}, "", 5 * time.Minute},
		{[]string{"no-store"}, "", time.Minute},
		{[]string{"max-age=300, no-cache"}, "", time.Minute},
		{[]string{"max-age=300"}, "100", 200 * time.Second},
		{[]string{"max-age=300"}, "290", time.Minute},
		{[]string{"max-age=300"}, "junk", 5 * time.Minute},
	}
	for _, tt := range tests {
		h := http.Header{}
		for _, v := range tt.cacheControl {
			h.Add("Cache-Control", v)
		}
		if tt.age != "" {
			h.Set("Age", tt.age)
		}
		if got := cacheTTL(h); got != tt.want {
			t.Fatalf("cacheTTL(%v, Age %q) = %v, want %v", tt.cacheControl, tt.age, got, tt.want)
		}
	}
}

func TestParseJWK(t *testing.T) {
	key := newTestKey(t, "k_one")
	good := key.jwk(t)
	if kid, pub, ok := parseJWK(mustJSON(t, good)); !ok || kid != "k_one" || !pub.Equal(&key.priv.PublicKey) {
		t.Fatalf("valid key refused")
	}
	for _, changes := range []map[string]any{{"use": remove}, {"alg": remove}} {
		if _, _, ok := parseJWK(mustJSON(t, with(good, changes))); !ok {
			t.Fatalf("key without %v refused", changes)
		}
	}
	p := key.point(t)
	offCurve := append([]byte{}, p[33:]...)
	offCurve[31] ^= 1
	ignored := []map[string]any{
		{"kty": "RSA"},
		{"crv": "P-384"},
		{"kid": ""},
		{"kid": remove},
		{"use": "enc"},
		{"alg": "ES384"},
		{"x": b64(p[1:32])},
		{"y": b64(append([]byte{0}, p[33:]...))},
		{"x": "not base64!"},
		{"y": b64(offCurve)},
		{"x": 12},
	}
	for _, changes := range ignored {
		if _, _, ok := parseJWK(mustJSON(t, with(good, changes))); ok {
			t.Fatalf("key with %v accepted", changes)
		}
	}
}

func TestHelpers(t *testing.T) {
	if got := redactURL("https://user:pw@acme.auth.didit.me/token/?code=secret#frag"); got != "https://acme.auth.didit.me/token/" {
		t.Fatalf("redactURL = %q", got)
	}
	if got := redactURL("https://[::1"); got != "" {
		t.Fatalf("redactURL of an invalid URL = %q", got)
	}
	if got := truncate(strings.Repeat("x", 10), 4); got != "xxxx..." {
		t.Fatalf("truncate = %q", got)
	}
	if parseBody(nil) != nil || parseBody([]byte("  ")) != nil || parseBody([]byte("oops")) != "oops" {
		t.Fatal("parseBody")
	}
	for _, token := range []string{"", "a b", "a\nb", "caf\xc3\xa9"} {
		if checkBearer(token) == nil {
			t.Fatalf("checkBearer(%q) accepted", token)
		}
	}
	if accessToken.String() != "at+jwt" || idToken.String() != "JWT" {
		t.Fatal("tokenKind.String")
	}
}
