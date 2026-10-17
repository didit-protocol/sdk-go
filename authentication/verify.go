package authentication

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

const (
	algorithm = "ES256"
	// maxTokenLength bounds the work done on an untrusted token.
	maxTokenLength = 16 << 10
	// maxNumericDate is 9999-12-31T23:59:59Z.
	maxNumericDate = 253402300799
)

var rawURL = base64.RawURLEncoding.Strict()

type tokenKind int

const (
	accessToken tokenKind = iota
	idToken
)

// acceptsType reports whether a JOSE typ header fits the token kind. Media
// type names are case-insensitive and may omit "application/" (RFC 7515
// section 4.1.9). Didit sends "at+jwt" for access tokens (RFC 9068) and "JWT"
// for ID tokens, so neither can be used as the other.
func (k tokenKind) acceptsType(typ string) bool {
	typ = strings.ToLower(typ)
	if k == accessToken {
		return typ == "at+jwt" || typ == "application/at+jwt"
	}
	return typ == "jwt" || typ == "application/jwt"
}

func (k tokenKind) String() string {
	if k == accessToken {
		return "at+jwt"
	}
	return "JWT"
}

// Claims holds the claims Didit access and ID tokens share. Times are UTC;
// a zero time means the claim is absent.
type Claims struct {
	// Issuer (iss) is the application's issuer; verification checks it equals
	// [Client.Issuer].
	Issuer string
	// Audience (aud) is the application id. A string claim becomes a
	// one-element slice.
	Audience []string
	// Subject (sub) is the identity id of the person.
	Subject string
	// SessionID (sid) is the session id, or the agent grant id for an agent
	// token.
	SessionID string
	// JWTID (jti) is the token id.
	JWTID string
	// IssuedAt (iat) is when the token was issued.
	IssuedAt time.Time
	// ExpiresAt (exp) is when the token expires. Didit access tokens live at
	// most five minutes.
	ExpiresAt time.Time
	// NotBefore (nbf) is when the token becomes valid. Didit does not send it.
	NotBefore time.Time
	// VendorData (vendor_data) is the person's reference across Didit
	// products: the external id given at import, otherwise the identity id.
	VendorData string
	// AMR (amr) lists the sign-in methods, for example ["passkey"]; empty for
	// an agent token.
	AMR []string
	// Org (org) is the person's earliest organization membership when the
	// application has customer organizations, otherwise nil.
	Org *Organization
	// Verified (verified) is the verified identity level and the claims the
	// application selected; the zero value when there is none.
	Verified VerifiedClaims
	// Act (act) identifies the agent acting for the person; nil for a token
	// the person holds. Check it when an endpoint must not accept agents.
	Act *Actor
	// Raw holds every claim of the token, including those this version does
	// not know. Numbers are json.Number.
	Raw map[string]any
}

// AccessTokenClaims are the verified claims of a Didit access token.
type AccessTokenClaims struct {
	Claims
	// ClientID (client_id) is the application id.
	ClientID string
}

// IDTokenClaims are the verified claims of a Didit ID token. ID tokens carry
// no client_id.
type IDTokenClaims struct {
	Claims
	// AuthTime (auth_time) is when the person signed in.
	AuthTime time.Time
	// Nonce (nonce) is the nonce sent to the authorization endpoint. ID tokens
	// returned by a refresh have none.
	Nonce string
	// Email (email) is the person's email address; empty when unset.
	Email string
	// EmailVerified (email_verified) reports whether Email was verified.
	EmailVerified bool
	// Name (name) is the person's display name; empty when unset.
	Name string
	// PhoneNumber (phone_number) is the person's phone number; empty when
	// unset.
	PhoneNumber string
}

// Organization is the org claim: the person's organization in the
// application.
type Organization struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

// Actor is the act claim of an agent token: the agent acting for the person.
type Actor struct {
	// Subject (sub) is the agent id.
	Subject string `json:"sub"`
	// Agent is the agent's name.
	Agent string `json:"agent"`
	// Scopes are the scopes the person granted the agent.
	Scopes []string `json:"scopes"`
}

// VerifiedClaims is the verified identity object of tokens (verified claim)
// and of [Session].Verified: the highest approved verification level and the
// claims the application selected. Fields the application did not select are
// empty or nil.
type VerifiedClaims struct {
	// Level is "liveness", "liveness_face_match" or "full_id"; empty when the
	// person has no approved verification.
	Level            string `json:"level"`
	Over18           *bool  `json:"over_18"`
	Over21           *bool  `json:"over_21"`
	GivenName        string `json:"given_name"`
	FamilyName       string `json:"family_name"`
	Birthdate        string `json:"birthdate"`   // YYYY-MM-DD
	Nationality      string `json:"nationality"` // ISO 3166-1 alpha-2
	Country          string `json:"country"`     // ISO 3166-1 alpha-2
	DocumentVerified *bool  `json:"document_verified"`
	Liveness         *bool  `json:"liveness"`
}

// VerifyOption configures one [Client.VerifyAccessToken] call.
type VerifyOption func(*verifyOptions)

type verifyOptions struct {
	audience    string
	audienceSet bool
}

// WithAudience requires the token's aud claim (a string or an array) to
// contain audience, the application id. An empty audience is a
// *didit.ConfigurationError, so a missing setting never disables the check.
func WithAudience(audience string) VerifyOption {
	return func(o *verifyOptions) { o.audience, o.audienceSet = audience, true }
}

// VerifyAccessToken verifies a Didit access token offline and returns its
// claims. It checks that the token is a compact JWS signed with ES256 by a key
// in the issuer's key set, that its typ header is at+jwt (also accepted:
// application/at+jwt, in any case), that iss equals the issuer, that aud
// contains the audience given with [WithAudience], that exp is present, and
// exp, nbf and iat against the clock with the leeway (see [WithLeeway]).
//
// A token that fails a check returns a *didit.TokenVerificationError whose
// Reason says which. When the key set cannot be fetched the error is the
// fetch error (*didit.APIConnectionError, *didit.APIStatusError, ...), so
// callers can answer 503 rather than 401.
//
// Offline verification sees a revoked session only when its access token
// expires (within five minutes). Use [Client.GetSession] for an instant check.
// Agent tokens are access tokens too; their Act claim is non-nil.
func (c *Client) VerifyAccessToken(ctx context.Context, token string, opts ...VerifyOption) (*AccessTokenClaims, error) {
	var o verifyOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.audienceSet && o.audience == "" {
		return nil, configError("audience must not be empty")
	}
	set, err := c.verifySignature(ctx, token, accessToken)
	if err != nil {
		return nil, err
	}
	claims := &AccessTokenClaims{}
	if err := c.readClaims(set, &claims.Claims, o.audience); err != nil {
		return nil, err
	}
	if claims.ClientID, err = set.str("client_id"); err != nil {
		return nil, err
	}
	return claims, nil
}

// VerifyIDToken verifies a Didit ID token offline and returns its claims. It
// makes the checks of [Client.VerifyAccessToken] with typ JWT (also accepted:
// application/jwt, in any case) and audience required, plus the nonce:
//
//   - nonce non-empty: the token must carry exactly this nonce. Use it for the
//     ID token of [Client.ExchangeCode], with the nonce from [CreateNonce] that
//     was sent to the authorization endpoint.
//   - nonce empty: the token must carry no nonce. ID tokens returned by
//     [Client.Refresh] have none.
//
// audience is the application id; an empty audience is a
// *didit.ConfigurationError.
func (c *Client) VerifyIDToken(ctx context.Context, token, nonce, audience string) (*IDTokenClaims, error) {
	if audience == "" {
		return nil, configError("audience is required to verify an ID token: pass the application id")
	}
	set, err := c.verifySignature(ctx, token, idToken)
	if err != nil {
		return nil, err
	}
	claims := &IDTokenClaims{}
	if err := c.readClaims(set, &claims.Claims, audience); err != nil {
		return nil, err
	}
	if claims.Nonce, err = set.str("nonce"); err != nil {
		return nil, err
	}
	switch hasNonce := set.present("nonce"); {
	case nonce == "" && hasNonce:
		return nil, tokenError(didit.TokenReasonInvalidNonce, "the token carries a nonce but none was expected")
	case nonce != "" && !hasNonce:
		return nil, tokenError(didit.TokenReasonInvalidNonce, "the token carries no nonce")
	case nonce != "" && subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1:
		return nil, tokenError(didit.TokenReasonInvalidNonce, "the nonce does not match")
	}
	if claims.AuthTime, err = set.numericDate("auth_time"); err != nil {
		return nil, err
	}
	if err := set.decodeAll([]member{
		{"email", &claims.Email},
		{"email_verified", &claims.EmailVerified},
		{"name", &claims.Name},
		{"phone_number", &claims.PhoneNumber},
	}); err != nil {
		return nil, err
	}
	return claims, nil
}

// verifySignature checks the JWS structure, the header and the signature, and
// returns the claims. It checks no claim.
func (c *Client) verifySignature(ctx context.Context, token string, kind tokenKind) (*claimSet, error) {
	if token == "" {
		return nil, tokenError(didit.TokenReasonMalformed, "the token is empty")
	}
	if len(token) > maxTokenLength {
		return nil, tokenError(didit.TokenReasonMalformed, "the token is longer than 16 KiB")
	}
	parts := strings.SplitN(token, ".", 4)
	if len(parts) != 3 {
		return nil, tokenError(didit.TokenReasonMalformed, "the token is not a compact JWS with three parts")
	}
	// The header first, so that an unsigned alg "none" token (empty third
	// part) is reported as the algorithm problem it is.
	headerJSON, ok := decodePart(parts[0])
	if !ok {
		return nil, tokenError(didit.TokenReasonMalformed, "the token parts are not unpadded base64url")
	}
	header, err := parseObject(headerJSON)
	if err != nil {
		return nil, tokenError(didit.TokenReasonMalformed, "the header is not a JSON object")
	}
	alg, err := header.str("alg")
	switch {
	case err != nil || !header.present("alg"):
		return nil, tokenError(didit.TokenReasonUnsupportedAlgorithm, "the header has no alg string")
	case alg != algorithm:
		return nil, tokenError(didit.TokenReasonUnsupportedAlgorithm, fmt.Sprintf("alg %q is not ES256", truncate(alg, 32)))
	}
	if typ, err := header.str("typ"); err != nil || !header.present("typ") {
		return nil, tokenError(didit.TokenReasonInvalidType, "the header has no typ string; expected "+kind.String())
	} else if !kind.acceptsType(typ) {
		return nil, tokenError(didit.TokenReasonInvalidType, fmt.Sprintf("typ %q is not %s", truncate(typ, 32), kind))
	}
	if header.present("crit") {
		return nil, tokenError(didit.TokenReasonMalformed, "the crit header is not supported")
	}
	kid, err := header.str("kid")
	if err != nil || kid == "" {
		return nil, tokenError(didit.TokenReasonMalformed, "the header has no kid")
	}
	payload, okPayload := decodePart(parts[1])
	signature, okSignature := decodePart(parts[2])
	if !okPayload || !okSignature {
		return nil, tokenError(didit.TokenReasonMalformed, "the token parts are not unpadded base64url")
	}
	if len(signature) != 64 {
		return nil, tokenError(didit.TokenReasonInvalidSignature, "the signature is not a 64-byte ES256 (R || S) signature")
	}
	key, err := c.publicKey(ctx, kid)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(key, digest[:], r, s) {
		return nil, tokenError(didit.TokenReasonInvalidSignature, "the signature does not verify")
	}
	set, err := parseObject(payload)
	if err != nil {
		return nil, tokenError(didit.TokenReasonMalformed, "the claims are not a JSON object")
	}
	return set, nil
}

// readClaims checks iss, sub, aud and the times, and fills the shared claims.
func (c *Client) readClaims(set *claimSet, out *Claims, audience string) error {
	var err error
	if !set.present("iss") {
		return missingClaim("iss")
	}
	if out.Issuer, err = set.str("iss"); err != nil {
		return err
	}
	if out.Issuer != c.issuer {
		return tokenError(didit.TokenReasonInvalidIssuer,
			fmt.Sprintf("iss %q is not the issuer %q", truncate(out.Issuer, 200), c.issuer))
	}
	if !set.present("sub") {
		return missingClaim("sub")
	}
	if out.Subject, err = set.str("sub"); err != nil {
		return err
	}
	if out.Audience, err = set.audience(); err != nil {
		return err
	}
	if audience != "" {
		if !set.present("aud") {
			return missingClaim("aud")
		}
		if !slices.Contains(out.Audience, audience) {
			return tokenError(didit.TokenReasonInvalidAudience, "aud does not contain the expected audience")
		}
	}
	if !set.present("exp") {
		return missingClaim("exp")
	}
	if out.ExpiresAt, err = set.numericDate("exp"); err != nil {
		return err
	}
	if out.NotBefore, err = set.numericDate("nbf"); err != nil {
		return err
	}
	if out.IssuedAt, err = set.numericDate("iat"); err != nil {
		return err
	}
	now := c.now()
	if !now.Before(out.ExpiresAt.Add(c.leeway)) {
		return tokenError(didit.TokenReasonExpired, "the token expired at "+out.ExpiresAt.Format(time.RFC3339))
	}
	if !out.NotBefore.IsZero() && out.NotBefore.After(now.Add(c.leeway)) {
		return tokenError(didit.TokenReasonNotYetValid, "nbf is in the future: "+out.NotBefore.Format(time.RFC3339))
	}
	if !out.IssuedAt.IsZero() && out.IssuedAt.After(now.Add(c.leeway)) {
		return tokenError(didit.TokenReasonNotYetValid, "iat is in the future: "+out.IssuedAt.Format(time.RFC3339))
	}
	if err := set.decodeAll([]member{
		{"sid", &out.SessionID},
		{"jti", &out.JWTID},
		{"vendor_data", &out.VendorData},
		{"amr", &out.AMR},
		{"org", &out.Org},
		{"verified", &out.Verified},
		{"act", &out.Act},
	}); err != nil {
		return err
	}
	out.Raw = set.raw
	return nil
}

// claimSet is a decoded JSON object: the members as raw JSON for typed
// decoding, and as values for Raw.
type claimSet struct {
	members map[string]json.RawMessage
	raw     map[string]any
}

func parseObject(data []byte) (*claimSet, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, err
	}
	if members == nil {
		return nil, fmt.Errorf("not an object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return &claimSet{members: members, raw: raw}, nil
}

// present reports whether the member exists and is not null.
func (s *claimSet) present(name string) bool {
	raw, ok := s.members[name]
	return ok && string(raw) != "null"
}

// decode decodes a member into v; an absent or null member leaves v as is.
func (s *claimSet) decode(name string, v any) error {
	if !s.present(name) {
		return nil
	}
	if err := json.Unmarshal(s.members[name], v); err != nil {
		return wrongType(name)
	}
	return nil
}

// member names a claim and where to decode it.
type member struct {
	name string
	dst  any
}

// decodeAll decodes the members in order and stops at the first error.
func (s *claimSet) decodeAll(members []member) error {
	for _, m := range members {
		if err := s.decode(m.name, m.dst); err != nil {
			return err
		}
	}
	return nil
}

func (s *claimSet) str(name string) (string, error) {
	var v string
	err := s.decode(name, &v)
	return v, err
}

// numericDate decodes a NumericDate claim: a JSON number of seconds since the
// epoch, possibly fractional (RFC 7519 section 2). Strings are refused.
func (s *claimSet) numericDate(name string) (time.Time, error) {
	if !s.present(name) {
		return time.Time{}, nil
	}
	raw := string(s.members[name])
	if raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return time.Time{}, wrongType(name)
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f < 0 || f > maxNumericDate {
		return time.Time{}, wrongType(name)
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC(), nil
}

// audience decodes aud, a string or an array of strings.
func (s *claimSet) audience() ([]string, error) {
	if !s.present("aud") {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(s.members["aud"], &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(s.members["aud"], &many); err != nil {
		return nil, wrongType("aud")
	}
	return many, nil
}

func tokenError(reason didit.TokenVerificationReason, msg string) error {
	return &didit.TokenVerificationError{Reason: reason, Message: msg}
}

func missingClaim(name string) error {
	return tokenError(didit.TokenReasonMissingClaim, "the token has no "+name+" claim")
}

func wrongType(name string) error {
	return tokenError(didit.TokenReasonMissingClaim, "the "+name+" claim has the wrong type or an invalid value")
}
