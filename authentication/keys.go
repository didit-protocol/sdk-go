package authentication

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

const (
	discoveryPath = "/.well-known/openid-configuration"
	jwksPath      = "/.well-known/jwks.json"

	defaultCacheTTL = 5 * time.Minute
	minCacheTTL     = time.Minute
	maxCacheTTL     = 24 * time.Hour
	// keyRefetchInterval is the minimum time between two key set fetches
	// caused by an unknown kid.
	keyRefetchInterval = time.Minute
)

// discoveryDocument holds the members of the OpenID Provider metadata the
// package uses.
type discoveryDocument struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"`
	JWKSURI       string `json:"jwks_uri"`
}

type discoveryState struct {
	tokenURL string
	jwksURL  string
	expiry   time.Time
}

// jsonWebKey holds the members of a JSON Web Key the package uses.
type jsonWebKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// flight is a fetch in progress. Concurrent callers wait for it and share
// its result instead of fetching again, so an unavailable issuer costs one
// fetch per wave of callers, not one per caller in turn.
type flight struct {
	done chan struct{}
	err  error
	// abandoned reports that the fetch failed because the context of the
	// caller that made it ended; the callers waiting then fetch themselves.
	abandoned bool
}

// wait blocks until f ends or ctx is done. It returns again=true when the
// caller should look at the cache again, otherwise the error to return.
func (f *flight) wait(ctx context.Context, rawURL string) (again bool, err error) {
	select {
	case <-f.done:
		if f.err != nil && !f.abandoned {
			return false, f.err
		}
		return true, nil
	case <-ctx.Done():
		return false, connectionError(http.MethodGet, rawURL, ctx.Err())
	}
}

// end records the outcome of f; the caller holds c.mu and closes f.done
// after releasing it.
func (f *flight) end(ctx context.Context, err error) {
	f.err, f.abandoned = err, err != nil && ctx.Err() != nil
}

// metadata returns the token endpoint and key set URL from the issuer's
// discovery document, fetching it when the cached copy is missing or stale.
func (c *Client) metadata(ctx context.Context) (discoveryState, error) {
	discoveryURL := c.issuer + discoveryPath
	for {
		c.mu.Lock()
		if c.discovery != nil && c.now().Before(c.discovery.expiry) {
			state := *c.discovery
			c.mu.Unlock()
			return state, nil
		}
		if f := c.discoveryFlight; f != nil {
			c.mu.Unlock()
			if again, err := f.wait(ctx, discoveryURL); !again {
				return discoveryState{}, err
			}
			continue
		}
		f := &flight{done: make(chan struct{})}
		c.discoveryFlight = f
		c.mu.Unlock()

		state, err := c.fetchDiscovery(ctx, discoveryURL)

		c.mu.Lock()
		if err == nil {
			c.discovery = &state
		}
		c.discoveryFlight = nil
		f.end(ctx, err)
		c.mu.Unlock()
		close(f.done)
		return state, err
	}
}

// fetchDiscovery fetches and checks the discovery document. A document that
// names another issuer is refused and not cached.
func (c *Client) fetchDiscovery(ctx context.Context, discoveryURL string) (discoveryState, error) {
	r := request{method: http.MethodGet, url: discoveryURL, retry: true}
	resp, err := c.send(ctx, r)
	if err != nil {
		return discoveryState{}, err
	}
	var doc discoveryDocument
	if err := decodeJSON(r, resp.body, &doc); err != nil {
		return discoveryState{}, err
	}
	if doc.Issuer != c.issuer {
		return discoveryState{}, configError(fmt.Sprintf(
			"the discovery document at %s names issuer %q, not %q; use the issuer it names",
			r.url, truncate(doc.Issuer, 200), c.issuer))
	}
	state := discoveryState{expiry: c.now().Add(cacheTTL(resp.header))}
	if state.tokenURL, err = c.sameOriginURL("token_endpoint", doc.TokenEndpoint, c.endpoint("token/")); err != nil {
		return discoveryState{}, err
	}
	if state.jwksURL, err = c.sameOriginURL("jwks_uri", doc.JWKSURI, c.issuer+jwksPath); err != nil {
		return discoveryState{}, err
	}
	return state, nil
}

// sameOriginURL returns a discovery endpoint, or fallback when the document
// omits it. Endpoints on another origin are refused: codes and refresh tokens
// go to the token endpoint and must never leave the issuer's origin.
func (c *Client) sameOriginURL(name, value, fallback string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	u, err := url.Parse(value)
	if err != nil || !u.IsAbs() || u.User != nil || !sameOrigin(u, c.origin) {
		return "", configError(fmt.Sprintf("the discovery document's %s is not on the issuer's origin", name))
	}
	return value, nil
}

// publicKey returns the verification key for kid. A fresh cached key set is
// used as is; a stale or missing one is fetched; a kid missing from a fresh
// key set triggers a refetch only when the last fetch is at least a minute
// old, so forged kids cannot make the client hammer the issuer.
func (c *Client) publicKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	for {
		c.mu.Lock()
		now := c.now()
		fresh := c.keysLoaded && now.Before(c.keysExpiry)
		if key, found := c.keys[kid]; fresh && found {
			c.mu.Unlock()
			return key, nil
		}
		if f := c.keysFlight; f != nil {
			c.mu.Unlock()
			if again, err := f.wait(ctx, c.issuer+jwksPath); !again {
				return nil, err
			}
			continue
		}
		if fresh && now.Sub(c.keysFetch) < keyRefetchInterval {
			c.mu.Unlock()
			return nil, unknownKey()
		}
		f := &flight{done: make(chan struct{})}
		previous := c.keysFetch
		c.keysFlight, c.keysFetch = f, now // a failed fetch counts too
		c.mu.Unlock()

		err := c.fetchKeys(ctx)

		c.mu.Lock()
		c.keysFlight = nil
		f.end(ctx, err)
		if f.abandoned {
			c.keysFetch = previous
		}
		c.mu.Unlock()
		close(f.done)
		if err != nil {
			return nil, err
		}
		// Look again: the kid is in the new key set, or it is unknown.
	}
}

// fetchKeys replaces the cached key set. Keys that are not P-256 signature
// keys are ignored.
func (c *Client) fetchKeys(ctx context.Context) error {
	meta, err := c.metadata(ctx)
	if err != nil {
		return err
	}
	r := request{method: http.MethodGet, url: meta.jwksURL, retry: true}
	resp, err := c.send(ctx, r)
	if err != nil {
		return err
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := decodeJSON(r, resp.body, &set); err != nil {
		return err
	}
	if set.Keys == nil {
		return connectionError(r.method, r.url, fmt.Errorf("invalid response body: no keys member"))
	}
	keys := make(map[string]*ecdsa.PublicKey, len(set.Keys))
	for _, raw := range set.Keys {
		if kid, key, ok := parseJWK(raw); ok {
			if _, dup := keys[kid]; !dup {
				keys[kid] = key
			}
		}
	}
	c.mu.Lock()
	c.keys, c.keysLoaded, c.keysExpiry = keys, true, c.now().Add(cacheTTL(resp.header))
	c.mu.Unlock()
	return nil
}

// parseJWK returns the kid and public key of an EC P-256 signature key, or
// false for any other or invalid key.
func parseJWK(raw json.RawMessage) (string, *ecdsa.PublicKey, bool) {
	var k jsonWebKey
	if err := json.Unmarshal(raw, &k); err != nil {
		return "", nil, false
	}
	if k.Kty != "EC" || k.Crv != "P-256" || k.Kid == "" ||
		(k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != algorithm) {
		return "", nil, false
	}
	x, okX := coordinate(k.X)
	y, okY := coordinate(k.Y)
	if !okX || !okY {
		return "", nil, false
	}
	// Parsing the key as a SubjectPublicKeyInfo checks that the point is on
	// the curve, and needs neither the raw coordinate fields (deprecated
	// since Go 1.25) nor ecdsa.ParseUncompressedPublicKey (Go 1.25+).
	der := make([]byte, 0, len(p256SPKIPrefix)+65)
	der = append(append(append(append(der, p256SPKIPrefix...), 4), x...), y...)
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", nil, false
	}
	key, ok := pub.(*ecdsa.PublicKey)
	return k.Kid, key, ok
}

// p256SPKIPrefix is the DER SubjectPublicKeyInfo header (id-ecPublicKey,
// prime256v1) of an uncompressed P-256 point.
var p256SPKIPrefix = []byte{
	0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01,
	0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
}

// coordinate decodes a P-256 coordinate: unpadded base64url of exactly 32
// bytes (RFC 7518 section 6.2.1.2).
func coordinate(s string) ([]byte, bool) {
	if !isBase64URL(s) {
		return nil, false
	}
	b, err := rawURL.DecodeString(s)
	return b, err == nil && len(b) == 32
}

// cacheTTL returns how long a metadata response may be cached: its
// Cache-Control max-age minus its Age, five minutes without max-age, and
// never less than a minute or more than a day.
func cacheTTL(h http.Header) time.Duration {
	const maxSeconds = int64(maxCacheTTL / time.Second)
	ttl := defaultCacheTTL
	for _, directive := range strings.Split(strings.Join(h.Values("Cache-Control"), ","), ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "no-store", "no-cache":
			return minCacheTTL
		case "max-age":
			if seconds, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(value), `"`), 10, 64); err == nil && seconds >= 0 {
				ttl = time.Duration(min(seconds, maxSeconds)) * time.Second
			}
		}
	}
	if age, err := strconv.ParseInt(strings.TrimSpace(h.Get("Age")), 10, 64); err == nil && age > 0 {
		ttl -= time.Duration(min(age, maxSeconds)) * time.Second
	}
	return max(minCacheTTL, min(ttl, maxCacheTTL))
}

func unknownKey() error {
	return &didit.TokenVerificationError{
		Reason:  didit.TokenReasonUnknownKey,
		Message: "no key in the issuer's key set matches the token's kid",
	}
}

// truncate shortens untrusted text before it goes into an error message.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
