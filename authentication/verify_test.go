package authentication

import (
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

func TestVerifyAccessTokenValid(t *testing.T) {
	key := newTestKey(t, "k_one")
	stub := newStub(t, key)
	c := newTestClient(t, stub, newClock())
	ctx := context.Background()

	claims, err := c.VerifyAccessToken(ctx, key.mint(t, nil, accessClaims()), WithAudience(testAudience))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != testIssuer || claims.Subject != "6f0f7a43-1c64-4b1e-8d1f-7f0e8f4a2b10" ||
		claims.SessionID != "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d" || claims.JWTID != "jti-access-1" ||
		claims.ClientID != testAudience || claims.VendorData != "customer-42" {
		t.Fatalf("claims = %+v", claims)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != testAudience {
		t.Fatalf("audience = %v", claims.Audience)
	}
	if !claims.IssuedAt.Equal(testNow) || !claims.ExpiresAt.Equal(testNow.Add(5*time.Minute)) || !claims.NotBefore.IsZero() {
		t.Fatalf("times = %v %v %v", claims.IssuedAt, claims.ExpiresAt, claims.NotBefore)
	}
	if claims.ExpiresAt.Location() != time.UTC {
		t.Fatalf("times are not UTC")
	}
	if len(claims.AMR) != 1 || claims.AMR[0] != "passkey" {
		t.Fatalf("amr = %v", claims.AMR)
	}
	if claims.Org == nil || claims.Org.ID != "org-1" || claims.Org.Role != "admin" {
		t.Fatalf("org = %+v", claims.Org)
	}
	if claims.Verified.Level != "full_id" || claims.Verified.Over18 == nil || !*claims.Verified.Over18 ||
		claims.Verified.Over21 != nil || claims.Verified.GivenName != "Ada" || claims.Verified.Country != "ES" {
		t.Fatalf("verified = %+v", claims.Verified)
	}
	if claims.Act != nil {
		t.Fatalf("act = %+v", claims.Act)
	}
	if claims.Raw["vendor_data"] != "customer-42" || claims.Raw["exp"] != json.Number(strconv.FormatInt(testNow.Add(5*time.Minute).Unix(), 10)) {
		t.Fatalf("raw = %v", claims.Raw)
	}

	// Without an audience, aud is not checked; the key set is cached.
	if _, err := c.VerifyAccessToken(ctx, key.mint(t, nil, with(accessClaims(), map[string]any{"aud": "other"}))); err != nil {
		t.Fatal(err)
	}
	if stub.count(discoveryPath) != 1 || stub.count(jwksPath) != 1 {
		t.Fatalf("discovery %d, jwks %d fetches, want 1 and 1", stub.count(discoveryPath), stub.count(jwksPath))
	}
	for _, r := range stub.all() {
		if r.header.Get("User-Agent") != userAgent || r.header.Get("X-Didit-Client") != clientHeader ||
			r.header.Get("Accept") != "application/json" || r.header.Get("Authorization") != "" {
			t.Fatalf("headers of %s = %v", r.path, r.header)
		}
	}
}

func TestVerifyAccessTokenAcceptedVariants(t *testing.T) {
	key := newTestKey(t, "k_one")
	c := newTestClient(t, newStub(t, key), newClock())
	tests := map[string]struct {
		header map[string]any
		claims map[string]any
	}{
		"aud array":            {claims: with(accessClaims(), map[string]any{"aud": []string{"other", testAudience}})},
		"typ upper case":       {header: map[string]any{"typ": "AT+JWT"}},
		"typ media type":       {header: map[string]any{"typ": "application/at+jwt"}},
		"exp within leeway":    {claims: with(accessClaims(), map[string]any{"exp": testNow.Add(-59 * time.Second).Unix()})},
		"nbf within leeway":    {claims: with(accessClaims(), map[string]any{"nbf": testNow.Add(59 * time.Second).Unix()})},
		"iat within leeway":    {claims: with(accessClaims(), map[string]any{"iat": testNow.Add(59 * time.Second).Unix()})},
		"fractional exp":       {claims: with(accessClaims(), map[string]any{"exp": float64(testNow.Unix()) + 300.5})},
		"null optional claims": {claims: with(accessClaims(), map[string]any{"org": nil, "verified": nil, "jti": nil})},
		"agent token": {claims: with(accessClaims(), map[string]any{
			"amr": []string{}, "act": map[string]any{"sub": "agent-1", "agent": "Nova", "scopes": []string{"read"}},
		})},
		"unknown claims and header": {
			header: map[string]any{"jku": "https://elsewhere.example/keys"},
			claims: with(accessClaims(), map[string]any{"future": map[string]any{"x": 1}}),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			claims := tt.claims
			if claims == nil {
				claims = accessClaims()
			}
			got, err := c.VerifyAccessToken(context.Background(), key.mint(t, tt.header, claims), WithAudience(testAudience))
			if err != nil {
				t.Fatal(err)
			}
			if name == "agent token" && (got.Act == nil || got.Act.Subject != "agent-1" || got.Act.Agent != "Nova" || got.Act.Scopes[0] != "read") {
				t.Fatalf("act = %+v", got.Act)
			}
			if name == "fractional exp" && got.ExpiresAt.Nanosecond() != 500000000 {
				t.Fatalf("exp = %v", got.ExpiresAt)
			}
		})
	}
}

func TestVerifyAccessTokenRejections(t *testing.T) {
	key := newTestKey(t, "k_one")
	other := newTestKey(t, "k_one") // same kid, different key
	c := newTestClient(t, newStub(t, key), newClock())

	valid := key.mint(t, nil, accessClaims())
	parts := strings.Split(valid, ".")
	input := parts[0] + "." + parts[1]
	sig := key.rawSignature(t, input)

	der, err := x509.MarshalPKIXPublicKey(&key.priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	hs256 := func(secret []byte) string {
		in := signingInput(t, key.kid, map[string]any{"alg": "HS256"}, accessClaims())
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(in))
		return in + "." + b64(mac.Sum(nil))
	}
	derSignature := func() string {
		for {
			digest := sha256.Sum256([]byte(input))
			asn1Sig, err := ecdsa.SignASN1(rand.Reader, key.priv, digest[:])
			if err != nil {
				t.Fatal(err)
			}
			if len(asn1Sig) != 64 {
				return input + "." + b64(asn1Sig)
			}
		}
	}
	tamperedClaims := b64(mustJSON(t, with(accessClaims(), map[string]any{"amr": []string{"password"}})))
	notObject := func() string {
		in := b64(mustJSON(t, map[string]any{"alg": "ES256", "typ": "at+jwt", "kid": key.kid})) + "." + b64([]byte(`["x"]`))
		return in + "." + b64(key.rawSignature(t, in))
	}

	tests := []struct {
		name     string
		token    string
		audience string
		reason   didit.TokenVerificationReason
	}{
		{"empty", "", "", didit.TokenReasonMalformed},
		{"too long", strings.Repeat("a", maxTokenLength+1), "", didit.TokenReasonMalformed},
		{"two parts", input, "", didit.TokenReasonMalformed},
		{"four parts", valid + ".x", "", didit.TokenReasonMalformed},
		{"padded header", parts[0] + "=." + parts[1] + "." + parts[2], "", didit.TokenReasonMalformed},
		{"standard base64", strings.ReplaceAll(valid, "-", "+") + "+/", "", didit.TokenReasonMalformed},
		{"newline in part", parts[0] + "\n." + parts[1] + "." + parts[2], "", didit.TokenReasonMalformed},
		{"empty payload", parts[0] + ".." + parts[2], "", didit.TokenReasonMalformed},
		{"header not JSON", b64([]byte("not json")) + "." + parts[1] + "." + parts[2], "", didit.TokenReasonMalformed},
		{"header array", b64([]byte("[]")) + "." + parts[1] + "." + parts[2], "", didit.TokenReasonMalformed},
		{"header null", b64([]byte("null")) + "." + parts[1] + "." + parts[2], "", didit.TokenReasonMalformed},
		{"alg none unsigned", signingInput(t, key.kid, map[string]any{"alg": "none"}, accessClaims()) + ".", "", didit.TokenReasonUnsupportedAlgorithm},
		{"alg none signed", key.mint(t, map[string]any{"alg": "none"}, accessClaims()), "", didit.TokenReasonUnsupportedAlgorithm},
		{"HS256 keyed with public key PEM", hs256(publicPEM), "", didit.TokenReasonUnsupportedAlgorithm},
		{"HS256 keyed with public key point", hs256(key.point(t)), "", didit.TokenReasonUnsupportedAlgorithm},
		{"RS256", key.mint(t, map[string]any{"alg": "RS256"}, accessClaims()), "", didit.TokenReasonUnsupportedAlgorithm},
		{"PS256", key.mint(t, map[string]any{"alg": "PS256"}, accessClaims()), "", didit.TokenReasonUnsupportedAlgorithm},
		{"ES384", key.mint(t, map[string]any{"alg": "ES384"}, accessClaims()), "", didit.TokenReasonUnsupportedAlgorithm},
		{"lowercase es256", key.mint(t, map[string]any{"alg": "es256"}, accessClaims()), "", didit.TokenReasonUnsupportedAlgorithm},
		{"missing alg", key.mint(t, map[string]any{"alg": remove}, accessClaims()), "", didit.TokenReasonUnsupportedAlgorithm},
		{"alg not a string", key.mint(t, map[string]any{"alg": 256}, accessClaims()), "", didit.TokenReasonUnsupportedAlgorithm},
		{"typ JWT", key.mint(t, map[string]any{"typ": "JWT"}, accessClaims()), "", didit.TokenReasonInvalidType},
		{"typ missing", key.mint(t, map[string]any{"typ": remove}, accessClaims()), "", didit.TokenReasonInvalidType},
		{"typ not a string", key.mint(t, map[string]any{"typ": true}, accessClaims()), "", didit.TokenReasonInvalidType},
		{"crit", key.mint(t, map[string]any{"crit": []string{"exp"}}, accessClaims()), "", didit.TokenReasonMalformed},
		{"missing kid", key.mint(t, map[string]any{"kid": remove}, accessClaims()), "", didit.TokenReasonMalformed},
		{"empty kid", key.mint(t, map[string]any{"kid": ""}, accessClaims()), "", didit.TokenReasonMalformed},
		{"unknown kid", newTestKey(t, "k_unknown").mint(t, nil, accessClaims()), "", didit.TokenReasonUnknownKey},
		{"DER signature", derSignature(), "", didit.TokenReasonInvalidSignature},
		{"63-byte signature", input + "." + b64(sig[:63]), "", didit.TokenReasonInvalidSignature},
		{"65-byte signature", input + "." + b64(append(append([]byte{}, sig...), 0)), "", didit.TokenReasonInvalidSignature},
		{"zero signature", input + "." + b64(make([]byte, 64)), "", didit.TokenReasonInvalidSignature},
		{"tampered payload", parts[0] + "." + tamperedClaims + "." + parts[2], "", didit.TokenReasonInvalidSignature},
		{"signed by another key", other.mint(t, nil, accessClaims()), "", didit.TokenReasonInvalidSignature},
		{"claims not an object", notObject(), "", didit.TokenReasonMalformed},
		{"wrong iss", key.mint(t, nil, with(accessClaims(), map[string]any{"iss": "https://other.auth.didit.me"})), "", didit.TokenReasonInvalidIssuer},
		{"iss with trailing slash", key.mint(t, nil, with(accessClaims(), map[string]any{"iss": testIssuer + "/"})), "", didit.TokenReasonInvalidIssuer},
		{"missing iss", key.mint(t, nil, with(accessClaims(), map[string]any{"iss": remove})), "", didit.TokenReasonMissingClaim},
		{"iss not a string", key.mint(t, nil, with(accessClaims(), map[string]any{"iss": 1})), "", didit.TokenReasonMissingClaim},
		{"missing sub", key.mint(t, nil, with(accessClaims(), map[string]any{"sub": remove})), "", didit.TokenReasonMissingClaim},
		{"sub not a string", key.mint(t, nil, with(accessClaims(), map[string]any{"sub": 7})), "", didit.TokenReasonMissingClaim},
		{"wrong aud", key.mint(t, nil, with(accessClaims(), map[string]any{"aud": "other"})), testAudience, didit.TokenReasonInvalidAudience},
		{"wrong aud array", key.mint(t, nil, with(accessClaims(), map[string]any{"aud": []string{"a", "b"}})), testAudience, didit.TokenReasonInvalidAudience},
		{"missing aud", key.mint(t, nil, with(accessClaims(), map[string]any{"aud": remove})), testAudience, didit.TokenReasonMissingClaim},
		{"aud not a string", key.mint(t, nil, with(accessClaims(), map[string]any{"aud": 5})), "", didit.TokenReasonMissingClaim},
		{"expired", key.mint(t, nil, with(accessClaims(), map[string]any{"exp": testNow.Add(-61 * time.Second).Unix()})), "", didit.TokenReasonExpired},
		{"exactly at exp plus leeway", key.mint(t, nil, with(accessClaims(), map[string]any{"exp": testNow.Add(-60 * time.Second).Unix()})), "", didit.TokenReasonExpired},
		{"missing exp", key.mint(t, nil, with(accessClaims(), map[string]any{"exp": remove})), "", didit.TokenReasonMissingClaim},
		{"exp as string", key.mint(t, nil, with(accessClaims(), map[string]any{"exp": "1760702700"})), "", didit.TokenReasonMissingClaim},
		{"negative exp", key.mint(t, nil, with(accessClaims(), map[string]any{"exp": -1})), "", didit.TokenReasonMissingClaim},
		{"huge exp", key.mint(t, nil, with(accessClaims(), map[string]any{"exp": 1e300})), "", didit.TokenReasonMissingClaim},
		{"nbf in the future", key.mint(t, nil, with(accessClaims(), map[string]any{"nbf": testNow.Add(61 * time.Second).Unix()})), "", didit.TokenReasonNotYetValid},
		{"nbf as bool", key.mint(t, nil, with(accessClaims(), map[string]any{"nbf": true})), "", didit.TokenReasonMissingClaim},
		{"iat in the future", key.mint(t, nil, with(accessClaims(), map[string]any{"iat": testNow.Add(61 * time.Second).Unix()})), "", didit.TokenReasonNotYetValid},
		{"iat as object", key.mint(t, nil, with(accessClaims(), map[string]any{"iat": map[string]any{}})), "", didit.TokenReasonMissingClaim},
		{"amr wrong type", key.mint(t, nil, with(accessClaims(), map[string]any{"amr": "passkey"})), "", didit.TokenReasonMissingClaim},
		{"org wrong type", key.mint(t, nil, with(accessClaims(), map[string]any{"org": []string{"x"}})), "", didit.TokenReasonMissingClaim},
		{"client_id wrong type", key.mint(t, nil, with(accessClaims(), map[string]any{"client_id": 1})), "", didit.TokenReasonMissingClaim},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []VerifyOption
			if tt.audience != "" {
				opts = append(opts, WithAudience(tt.audience))
			}
			claims, err := c.VerifyAccessToken(context.Background(), tt.token, opts...)
			if claims != nil {
				t.Fatalf("claims = %+v, want nil", claims)
			}
			wantReason(t, err, tt.reason)
			if tt.token != "" && strings.Contains(err.Error(), tt.token) {
				t.Fatalf("error message contains the token")
			}
		})
	}
}

func TestVerifyAccessTokenRefusesIDToken(t *testing.T) {
	key := newTestKey(t, "k_one")
	c := newTestClient(t, newStub(t, key), newClock())
	idToken := key.mint(t, map[string]any{"typ": "JWT"}, idClaims())
	_, err := c.VerifyAccessToken(context.Background(), idToken)
	wantReason(t, err, didit.TokenReasonInvalidType)
}

func TestVerifyAccessTokenEmptyAudience(t *testing.T) {
	key := newTestKey(t, "k_one")
	stub := newStub(t, key)
	c := newTestClient(t, stub, newClock())
	_, err := c.VerifyAccessToken(context.Background(), key.mint(t, nil, accessClaims()), WithAudience(""), nil)
	wantConfigError(t, err, "audience")
	if len(stub.all()) != 0 {
		t.Fatal("requests were sent")
	}
}

func TestLeeway(t *testing.T) {
	key := newTestKey(t, "k_one")
	expired90s := key.mint(t, nil, with(accessClaims(), map[string]any{"exp": testNow.Add(-90 * time.Second).Unix()}))
	expired1s := key.mint(t, nil, with(accessClaims(), map[string]any{"exp": testNow.Add(-time.Second).Unix()}))

	lenient := newTestClient(t, newStub(t, key), newClock(), WithLeeway(2*time.Minute))
	if _, err := lenient.VerifyAccessToken(context.Background(), expired90s); err != nil {
		t.Fatal(err)
	}
	strict := newTestClient(t, newStub(t, key), newClock(), WithLeeway(0))
	_, err := strict.VerifyAccessToken(context.Background(), expired1s)
	wantReason(t, err, didit.TokenReasonExpired)
}

func TestVerifyIDToken(t *testing.T) {
	key := newTestKey(t, "k_one")
	stub := newStub(t, key)
	c := newTestClient(t, stub, newClock())
	ctx := context.Background()
	mintID := func(claims map[string]any) string { return key.mint(t, map[string]any{"typ": "JWT"}, claims) }

	claims, err := c.VerifyIDToken(ctx, mintID(idClaims()), "nonce-1", testAudience)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Nonce != "nonce-1" || claims.Email != "person@example.test" || !claims.EmailVerified ||
		claims.Name != "Ada Example" || claims.PhoneNumber != "+34600000000" ||
		!claims.AuthTime.Equal(testNow.Add(-time.Hour)) || claims.Subject == "" || claims.JWTID != "jti-id-1" {
		t.Fatalf("claims = %+v", claims)
	}

	// A refreshed ID token has no nonce and is checked with "".
	refreshed := with(idClaims(), map[string]any{"nonce": remove, "email": remove, "name": remove, "phone_number": remove, "email_verified": false})
	claims, err = c.VerifyIDToken(ctx, mintID(refreshed), "", testAudience)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Nonce != "" || claims.Email != "" || claims.EmailVerified {
		t.Fatalf("claims = %+v", claims)
	}

	// Other accepted typ spellings.
	for _, typ := range []string{"jwt", "application/jwt", "APPLICATION/JWT"} {
		if _, err := c.VerifyIDToken(ctx, key.mint(t, map[string]any{"typ": typ}, idClaims()), "nonce-1", testAudience); err != nil {
			t.Fatalf("typ %s: %v", typ, err)
		}
	}

	tests := []struct {
		name     string
		token    string
		nonce    string
		audience string
		reason   didit.TokenVerificationReason
	}{
		{"wrong nonce", mintID(idClaims()), "nonce-2", testAudience, didit.TokenReasonInvalidNonce},
		{"nonce prefix", mintID(idClaims()), "nonce", testAudience, didit.TokenReasonInvalidNonce},
		{"missing nonce", mintID(with(idClaims(), map[string]any{"nonce": remove})), "nonce-1", testAudience, didit.TokenReasonInvalidNonce},
		{"nonce present, none expected", mintID(idClaims()), "", testAudience, didit.TokenReasonInvalidNonce},
		{"empty nonce present, none expected", mintID(with(idClaims(), map[string]any{"nonce": ""})), "", testAudience, didit.TokenReasonInvalidNonce},
		{"nonce wrong type", mintID(with(idClaims(), map[string]any{"nonce": 1})), "1", testAudience, didit.TokenReasonMissingClaim},
		{"wrong audience", mintID(idClaims()), "nonce-1", "another-application", didit.TokenReasonInvalidAudience},
		{"access token as ID token", key.mint(t, nil, idClaims()), "nonce-1", testAudience, didit.TokenReasonInvalidType},
		{"wrong iss", mintID(with(idClaims(), map[string]any{"iss": "https://evil.example"})), "nonce-1", testAudience, didit.TokenReasonInvalidIssuer},
		{"expired", mintID(with(idClaims(), map[string]any{"exp": testNow.Add(-time.Hour).Unix()})), "nonce-1", testAudience, didit.TokenReasonExpired},
		{"auth_time wrong type", mintID(with(idClaims(), map[string]any{"auth_time": "yesterday"})), "nonce-1", testAudience, didit.TokenReasonMissingClaim},
		{"email_verified wrong type", mintID(with(idClaims(), map[string]any{"email_verified": "yes"})), "nonce-1", testAudience, didit.TokenReasonMissingClaim},
		{"email wrong type", mintID(with(idClaims(), map[string]any{"email": false})), "nonce-1", testAudience, didit.TokenReasonMissingClaim},
		{"alg none", signingInput(t, key.kid, map[string]any{"alg": "none", "typ": "JWT"}, idClaims()) + ".", "nonce-1", testAudience, didit.TokenReasonUnsupportedAlgorithm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := c.VerifyIDToken(ctx, tt.token, tt.nonce, tt.audience)
			if claims != nil {
				t.Fatalf("claims = %+v, want nil", claims)
			}
			wantReason(t, err, tt.reason)
			if strings.Contains(err.Error(), "nonce-1") {
				t.Fatalf("error message contains the nonce: %v", err)
			}
		})
	}

	before := len(stub.all())
	_, err = c.VerifyIDToken(ctx, mintID(idClaims()), "nonce-1", "")
	wantConfigError(t, err, "audience is required")
	if len(stub.all()) != before {
		t.Fatal("requests were sent")
	}
}

func TestUnknownKidRefetchesAtMostOncePerMinute(t *testing.T) {
	k1 := newTestKey(t, "k_one")
	stub := newStub(t, k1)
	clock := newClock()
	c := newTestClient(t, stub, clock)
	ctx := context.Background()
	verify := func(key *testKey) error {
		claims := with(accessClaims(), map[string]any{"iat": clock.Now().Unix(), "exp": clock.Now().Add(5 * time.Minute).Unix()})
		_, err := c.VerifyAccessToken(ctx, key.mint(t, nil, claims))
		return err
	}

	if err := verify(k1); err != nil {
		t.Fatal(err)
	}
	if n := stub.count(jwksPath); n != 1 {
		t.Fatalf("jwks fetches = %d, want 1", n)
	}

	// Within a minute of the last fetch an unknown kid does not refetch.
	clock.Advance(30 * time.Second)
	missing := newTestKey(t, "k_missing")
	wantReason(t, verify(missing), didit.TokenReasonUnknownKey)
	if n := stub.count(jwksPath); n != 1 {
		t.Fatalf("jwks fetches = %d, want 1", n)
	}

	// After a minute it refetches once, then not again within a minute.
	clock.Advance(31 * time.Second)
	wantReason(t, verify(missing), didit.TokenReasonUnknownKey)
	if n := stub.count(jwksPath); n != 2 {
		t.Fatalf("jwks fetches = %d, want 2", n)
	}
	for i := 0; i < 5; i++ {
		clock.Advance(10 * time.Second)
		wantReason(t, verify(missing), didit.TokenReasonUnknownKey)
	}
	if n := stub.count(jwksPath); n != 2 {
		t.Fatalf("jwks fetches = %d, want 2 (one refetch per minute)", n)
	}

	// Rotation: a newly published key is picked up by the next allowed refetch,
	// and the old key keeps verifying.
	k2 := newTestKey(t, "k_two")
	stub.setKeys(k1, k2)
	clock.Advance(11 * time.Second)
	if err := verify(k2); err != nil {
		t.Fatal(err)
	}
	if err := verify(k1); err != nil {
		t.Fatal(err)
	}
	if n := stub.count(jwksPath); n != 3 {
		t.Fatalf("jwks fetches = %d, want 3", n)
	}
	// The discovery document stayed cached (max-age 300).
	if n := stub.count(discoveryPath); n != 1 {
		t.Fatalf("discovery fetches = %d, want 1", n)
	}
}

func TestAbandonedKeyFetchCountsTowardsTheRefetchLimit(t *testing.T) {
	// A server that passes r.Context() ends the fetch when its client hangs
	// up, but the request may have reached the issuer. Were the fetch not
	// counted, a client sending unknown kids and disconnecting could make
	// every request refetch the key set.
	k1 := newTestKey(t, "k_one")
	stub := newStub(t, k1)
	clock := newClock()
	c := newTestClient(t, stub, clock)
	mint := func(key *testKey) string {
		return key.mint(t, nil, with(accessClaims(), map[string]any{"iat": clock.Now().Unix(), "exp": clock.Now().Add(5 * time.Minute).Unix()}))
	}
	if _, err := c.VerifyAccessToken(context.Background(), mint(k1)); err != nil {
		t.Fatal(err)
	}

	clock.Advance(61 * time.Second)
	missing := newTestKey(t, "k_missing")
	ctx, cancel := context.WithCancel(context.Background())
	stub.setTransportError(func(r *http.Request) error {
		cancel() // the caller hangs up while the request is on its way
		return r.Context().Err()
	})
	_, err := c.VerifyAccessToken(ctx, mint(missing))
	var conn *didit.APIConnectionError
	if !errors.As(err, &conn) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v (%T), want APIConnectionError wrapping context.Canceled", err, err)
	}
	stub.setTransportError(nil)
	for i := 0; i < 5; i++ {
		clock.Advance(10 * time.Second)
		_, err := c.VerifyAccessToken(context.Background(), mint(missing))
		wantReason(t, err, didit.TokenReasonUnknownKey)
	}
	if n := stub.count(jwksPath); n != 2 {
		t.Fatalf("jwks fetches = %d, want 2 (the abandoned one counts)", n)
	}
}

func TestKeySetCacheHonoursMaxAge(t *testing.T) {
	k1 := newTestKey(t, "k_one")
	stub := newStub(t, k1)
	stub.cacheControl = "public, max-age=120"
	clock := newClock()
	c := newTestClient(t, stub, clock)
	verify := func() {
		t.Helper()
		claims := with(accessClaims(), map[string]any{"iat": clock.Now().Unix(), "exp": clock.Now().Add(5 * time.Minute).Unix()})
		if _, err := c.VerifyAccessToken(context.Background(), k1.mint(t, nil, claims)); err != nil {
			t.Fatal(err)
		}
	}
	verify()
	clock.Advance(119 * time.Second)
	verify()
	if n := stub.count(jwksPath); n != 1 {
		t.Fatalf("jwks fetches = %d, want 1", n)
	}
	clock.Advance(2 * time.Second)
	verify()
	if stub.count(jwksPath) != 2 || stub.count(discoveryPath) != 2 {
		t.Fatalf("jwks %d, discovery %d fetches, want 2 and 2", stub.count(jwksPath), stub.count(discoveryPath))
	}

	// A revoked key disappears from the key set at the next fetch.
	k2 := newTestKey(t, "k_two")
	stub.setKeys(k2)
	clock.Advance(121 * time.Second)
	claims := with(accessClaims(), map[string]any{"iat": clock.Now().Unix(), "exp": clock.Now().Add(5 * time.Minute).Unix()})
	_, err := c.VerifyAccessToken(context.Background(), k1.mint(t, nil, claims))
	wantReason(t, err, didit.TokenReasonUnknownKey)
}

func TestConcurrentVerificationFetchesOnce(t *testing.T) {
	key := newTestKey(t, "k_one")
	stub := newStub(t, key)
	c := newTestClient(t, stub, newClock())
	token := key.mint(t, nil, accessClaims())
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.VerifyAccessToken(context.Background(), token, WithAudience(testAudience))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if stub.count(discoveryPath) != 1 || stub.count(jwksPath) != 1 {
		t.Fatalf("discovery %d, jwks %d fetches, want 1 and 1", stub.count(discoveryPath), stub.count(jwksPath))
	}
}

func TestVerifyReturnsFetchErrors(t *testing.T) {
	key := newTestKey(t, "k_one")
	token := key.mint(t, nil, accessClaims())

	t.Run("key set unavailable", func(t *testing.T) {
		stub := newStub(t, key)
		stub.handle(jwksPath, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "down"})
		})
		c := newTestClient(t, stub, newClock())
		_, err := c.VerifyAccessToken(context.Background(), token)
		var serverErr *didit.InternalServerError
		if !errors.As(err, &serverErr) || serverErr.StatusCode != 503 {
			t.Fatalf("error = %v (%T), want InternalServerError 503", err, err)
		}
		var tokenErr *didit.TokenVerificationError
		if errors.As(err, &tokenErr) {
			t.Fatal("a fetch failure must not look like an invalid token")
		}
		if n := stub.count(jwksPath); n != 2 {
			t.Fatalf("jwks fetches = %d, want 2 (one retry)", n)
		}
	})

	t.Run("cancelled while waiting for a fetch", func(t *testing.T) {
		stub := newStub(t, key)
		c := newTestClient(t, stub, newClock())
		c.discovery = &discoveryState{tokenURL: testIssuer + "/token/", jwksURL: testIssuer + jwksPath, expiry: testNow.Add(time.Hour)}
		c.keysFlight = &flight{done: make(chan struct{})} // another caller is fetching the key set
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.VerifyAccessToken(ctx, token)
		var conn *didit.APIConnectionError
		if !errors.As(err, &conn) || !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v (%T), want APIConnectionError wrapping context.Canceled", err, err)
		}

		c.keysFlight = nil
		c.discovery = nil
		c.discoveryFlight = &flight{done: make(chan struct{})} // another caller is fetching discovery
		_, err = c.publicKey(ctx, "k_one")
		if !errors.As(err, &conn) || !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v (%T), want APIConnectionError wrapping context.Canceled", err, err)
		}
		if len(stub.all()) != 0 {
			t.Fatal("requests were sent")
		}
		if c.keysFetch.IsZero() {
			t.Fatal("an abandoned fetch did not count towards the refetch limit")
		}
	})

	t.Run("waiters use the document a concurrent fetch stored", func(t *testing.T) {
		stub := newStub(t, key)
		c := newTestClient(t, stub, newClock())
		f := &flight{done: make(chan struct{})}
		c.discoveryFlight = f
		got := make(chan error, 1)
		go func() {
			_, err := c.metadata(context.Background())
			got <- err
		}()
		time.Sleep(20 * time.Millisecond) // let the caller reach the fetch in progress
		c.mu.Lock()
		c.discovery = &discoveryState{tokenURL: testIssuer + "/token/", jwksURL: testIssuer + jwksPath, expiry: testNow.Add(time.Hour)}
		c.discoveryFlight = nil
		c.mu.Unlock()
		close(f.done)
		if err := <-got; err != nil {
			t.Fatal(err)
		}
		if len(stub.all()) != 0 {
			t.Fatal("the waiter fetched again")
		}
	})

	t.Run("waiters share a failed fetch", func(t *testing.T) {
		stub := newStub(t, key)
		c := newTestClient(t, stub, newClock())
		shared := errors.New("shared failure")
		done := make(chan struct{})
		close(done)
		c.keysFlight = &flight{done: done, err: shared}
		c.discoveryFlight = &flight{done: done, err: shared}
		if _, err := c.publicKey(context.Background(), "k_one"); !errors.Is(err, shared) {
			t.Fatalf("error = %v, want the shared error", err)
		}
		if _, err := c.metadata(context.Background()); !errors.Is(err, shared) {
			t.Fatalf("error = %v, want the shared error", err)
		}
		if len(stub.all()) != 0 {
			t.Fatal("waiters fetched again")
		}
	})

	t.Run("a waiter takes over when the fetching caller gives up", func(t *testing.T) {
		stub := newStub(t, key)
		started := make(chan struct{})
		var once sync.Once
		stub.setTransportError(func(r *http.Request) error {
			first := false
			if r.URL.Path == jwksPath {
				once.Do(func() { first = true })
			}
			if first {
				close(started)
				<-r.Context().Done()
				return r.Context().Err()
			}
			return nil
		})
		c := newTestClient(t, stub, newClock())
		leaderCtx, cancel := context.WithCancel(context.Background())
		leaderErr := make(chan error, 1)
		go func() {
			_, err := c.VerifyAccessToken(leaderCtx, token)
			leaderErr <- err
		}()
		<-started
		waiterErr := make(chan error, 1)
		go func() {
			_, err := c.VerifyAccessToken(context.Background(), token)
			waiterErr <- err
		}()
		time.Sleep(20 * time.Millisecond) // let the waiter reach the fetch in progress
		cancel()
		if err := <-leaderErr; !errors.Is(err, context.Canceled) {
			t.Fatalf("leader error = %v, want context.Canceled", err)
		}
		if err := <-waiterErr; err != nil {
			t.Fatalf("waiter error = %v", err)
		}
		if n := stub.count(jwksPath); n != 2 {
			t.Fatalf("jwks fetches = %d, want 2", n)
		}
	})
}
