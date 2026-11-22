package authentication

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Session is the answer of GET session/: the live session behind an access
// token.
type Session struct {
	// Subject (sub) is the identity id of the person.
	Subject string `json:"sub"`
	// SessionID (sid) is the session id.
	SessionID string `json:"sid"`
	// ApplicationID is the application id.
	ApplicationID string `json:"application_id"`
	// VendorData is the person's reference across Didit products.
	VendorData string `json:"vendor_data"`
	// AMR lists the sign-in methods, for example ["password"].
	AMR []string `json:"amr"`
	// MFAVerified reports whether the session passed a second factor (always
	// true for a passkey sign-in).
	MFAVerified bool `json:"mfa_verified"`
	// Email is the person's email address; empty for phone-only identities.
	Email string `json:"email"`
	// EmailVerified reports whether Email was verified.
	EmailVerified bool `json:"email_verified"`
	// Phone is the person's phone number; empty when unset.
	Phone string `json:"phone"`
	// VerifiedIdentity holds the verified claims the person shared at sign-in
	// (for example from the Didit ID Wallet); nil when none.
	VerifiedIdentity map[string]any `json:"verified_identity"`
	// Verified is the same object as the tokens' verified claim; the zero
	// value when there is none.
	Verified VerifiedClaims `json:"verified"`
	// ExpiresAt is when the session expires, regardless of activity.
	ExpiresAt time.Time `json:"expires_at"`
	// PostLogoutRedirectURI is the application's post-logout redirect URI;
	// empty when unset.
	PostLogoutRedirectURI string `json:"post_logout_redirect_uri"`
	// Organizations lists the person's organizations when the application has
	// customer organizations; empty otherwise.
	Organizations []SessionOrganization `json:"organizations"`
	// Raw is the response body as received, with fields this version does
	// not know.
	Raw json.RawMessage `json:"-"`
}

// SessionOrganization is one organization membership in [Session].
type SessionOrganization struct {
	// ID is the organization id.
	ID string `json:"organization_id"`
	// Name is the organization name. The issuer sends it as
	// "organization__name".
	Name string `json:"organization__name"`
	// Role is the member's built-in role.
	Role string `json:"role"`
	// CustomRole is the member's custom role; nil when none.
	CustomRole *CustomRole `json:"custom_role"`
}

// CustomRole is an organization custom role.
type CustomRole struct {
	Key         string   `json:"key"`
	Permissions []string `json:"permissions"`
}

// GetSession returns the session behind an access token (GET session/). It is
// the instant revocation check: unlike [Client.VerifyAccessToken] it fails as
// soon as the session is revoked, expired or idle, or the token was replaced
// by a refresh, with a *didit.AuthenticationError (401). Agent tokens are
// refused the same way.
func (c *Client) GetSession(ctx context.Context, accessToken string) (*Session, error) {
	if err := checkBearer(accessToken); err != nil {
		return nil, err
	}
	r := request{method: http.MethodGet, url: c.endpoint("session/"), bearer: accessToken, retry: true}
	resp, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	var out Session
	if err := decodeJSON(r, resp, &out); err != nil {
		return nil, err
	}
	out.Raw = resp.body
	return &out, nil
}

// RevokeSession signs the person out: it revokes the session behind the
// access token (DELETE session/). Its access and refresh tokens stop working
// at once for GetSession and Refresh; offline verification accepts the access
// token until it expires. The request is not retried, because a revoked
// session no longer accepts its token: an unknown outcome surfaces as an
// error, and calling RevokeSession again then answers 401.
func (c *Client) RevokeSession(ctx context.Context, accessToken string) error {
	if err := checkBearer(accessToken); err != nil {
		return err
	}
	_, err := c.send(ctx, request{method: http.MethodDelete, url: c.endpoint("session/"), bearer: accessToken})
	return err
}
