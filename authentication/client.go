package authentication

import (
	"crypto/ecdsa"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	didit "github.com/didit-protocol/sdk-go"
)

const (
	// DefaultLeeway is the clock skew tolerated for exp, nbf and iat.
	DefaultLeeway = 60 * time.Second
	// DefaultTimeout is the timeout of the default HTTP client.
	DefaultTimeout = 60 * time.Second

	livePrefix = "pk_live_"
	testPrefix = "pk_test_"

	defaultRetryDelay = 500 * time.Millisecond
)

// Client talks to one Didit Authentication issuer. Create it with [New] or
// [NewFromPublishableKey]. It is safe for concurrent use; create one per issuer
// and reuse it, because it caches the discovery document and the key set.
type Client struct {
	issuer   string
	origin   *url.URL
	clientID string
	leeway   time.Duration
	clock    func() time.Time
	http     *http.Client

	// retryDelay is the base delay before the single retry of a GET request.
	retryDelay time.Duration

	mu         sync.Mutex // guards the cached fields below
	discovery  *discoveryState
	keys       map[string]*ecdsa.PublicKey
	keysExpiry time.Time
	keysLoaded bool
	keysFetch  time.Time // when the key set was last fetched (or tried)

	// One fetch of each document at a time; waiters honour their context.
	discoverySem chan struct{}
	keysSem      chan struct{}
}

// Option configures a [Client].
type Option func(*Client)

// WithHTTPClient sets the HTTP client used for every request. The client is
// copied, not modified: the copy refuses redirects to another origin before
// consulting hc.CheckRedirect. A nil client keeps the default, which has a
// [DefaultTimeout] timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.http = hc
		}
	}
}

// WithClientID sets the OAuth client_id sent on token requests. Didit's
// client_id is the application id, which is also the aud of every token the
// issuer signs. It is optional: the issuer knows its application, and rejects
// a client_id that differs from it with 400 {"error": "invalid_client"}.
func WithClientID(clientID string) Option {
	return func(c *Client) { c.clientID = clientID }
}

// WithLeeway sets the clock skew tolerated when checking exp, nbf and iat.
// The default is [DefaultLeeway]. A negative leeway makes [New] fail.
func WithLeeway(d time.Duration) Option {
	return func(c *Client) { c.leeway = d }
}

// WithClock sets the function that returns the current time, for token time
// checks and for cache expiry. It is meant for tests; nil keeps time.Now.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.clock = now
		}
	}
}

// New returns a Client for the issuer, the application's origin such as
// "https://acme.auth.didit.me". A trailing slash is removed. The issuer must
// be an absolute https URL without credentials, query or fragment; http is
// accepted only for localhost, *.localhost, 127.0.0.1 and ::1. New makes no
// request: discovery happens on first use.
func New(issuer string, opts ...Option) (*Client, error) {
	normalized, origin, err := parseIssuer(issuer)
	if err != nil {
		return nil, err
	}
	c := &Client{
		issuer:       normalized,
		origin:       origin,
		leeway:       DefaultLeeway,
		clock:        time.Now,
		retryDelay:   defaultRetryDelay,
		discoverySem: make(chan struct{}, 1),
		keysSem:      make(chan struct{}, 1),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.leeway < 0 {
		return nil, configError("leeway must not be negative")
	}
	c.http = withRedirectPolicy(c.http)
	return c, nil
}

// NewFromPublishableKey returns a Client for the issuer carried by a
// publishable key ("pk_live_..." or "pk_test_..."); see [ParsePublishableKey].
func NewFromPublishableKey(key string, opts ...Option) (*Client, error) {
	issuer, _, err := ParsePublishableKey(key)
	if err != nil {
		return nil, err
	}
	return New(issuer, opts...)
}

// ParsePublishableKey returns the issuer a publishable key carries and whether
// it is a live key. The key is "pk_live_" (Live application) or "pk_test_"
// (Sandbox application) followed by the unpadded base64url encoding of the
// issuer. A publishable key is a public identifier, not a credential.
//
// Surrounding whitespace is ignored. A live key must carry an https issuer; a
// test key may also carry an http issuer on localhost or a loopback address.
func ParsePublishableKey(key string) (issuer string, live bool, err error) {
	key = strings.TrimSpace(key)
	var encoded string
	switch {
	case strings.HasPrefix(key, livePrefix):
		live, encoded = true, key[len(livePrefix):]
	case strings.HasPrefix(key, testPrefix):
		encoded = key[len(testPrefix):]
	default:
		return "", false, configError("publishable key must start with pk_live_ or pk_test_")
	}
	encoded = strings.TrimRight(encoded, "=")
	if encoded == "" || !isBase64URL(encoded) {
		return "", false, configError("publishable key is not valid base64url")
	}
	decoded, decodeErr := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if decodeErr != nil || !utf8.Valid(decoded) {
		return "", false, configError("publishable key is not valid base64url")
	}
	normalized, origin, err := parseIssuer(string(decoded))
	if err != nil {
		msg := "publishable key does not carry a valid issuer"
		var cfg *didit.ConfigurationError
		if errors.As(err, &cfg) {
			msg += ": " + cfg.Message
		}
		return "", false, configError(msg)
	}
	if live && origin.Scheme != "https" {
		return "", false, configError("a live publishable key must carry an https issuer")
	}
	return normalized, live, nil
}

// Issuer returns the issuer the Client verifies tokens for, without a trailing
// slash. It is the exact value the iss claim must have.
func (c *Client) Issuer() string { return c.issuer }

func (c *Client) now() time.Time { return c.clock() }

// endpoint returns the URL of a runtime route under the issuer, like
// "https://acme.auth.didit.me/session/".
func (c *Client) endpoint(path string) string { return c.issuer + "/" + path }

// parseIssuer validates an issuer and returns it without trailing slashes,
// with its origin.
func parseIssuer(raw string) (string, *url.URL, error) {
	if raw == "" {
		return "", nil, configError("issuer is required")
	}
	for _, r := range raw {
		if r <= ' ' || r == 0x7f {
			return "", nil, configError("issuer must not contain whitespace or control characters")
		}
	}
	trimmed := strings.TrimRight(raw, "/")
	u, err := url.Parse(trimmed)
	if err != nil || u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return "", nil, configError("issuer must be an absolute URL such as https://<slug>.auth.didit.me")
	}
	if u.User != nil {
		return "", nil, configError("issuer must not contain credentials")
	}
	if u.RawQuery != "" || u.ForceQuery || strings.Contains(trimmed, "?") {
		return "", nil, configError("issuer must not contain a query")
	}
	if u.Fragment != "" || strings.Contains(trimmed, "#") {
		return "", nil, configError("issuer must not contain a fragment")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", nil, configError("issuer has an invalid port")
		}
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return "", nil, configError("issuer must use https (http only for localhost)")
	}
	return trimmed, &url.URL{Scheme: u.Scheme, Host: u.Host}, nil
}

// isLoopback reports whether an issuer host may use plain http: the hosts
// Didit accepts for local Sandbox development.
func isLoopback(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "127.0.0.1" || host == "::1"
}

// isBase64URL reports whether s uses only the unpadded base64url alphabet.
// Go's decoder silently skips CR and LF, so they are refused here.
func isBase64URL(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !('A' <= b && b <= 'Z' || 'a' <= b && b <= 'z' || '0' <= b && b <= '9' || b == '-' || b == '_') {
			return false
		}
	}
	return true
}

func configError(msg string) error { return &didit.ConfigurationError{Message: msg} }
