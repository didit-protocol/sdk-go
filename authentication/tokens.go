package authentication

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// TokenResponse is the token endpoint's answer to [Client.ExchangeCode] and
// [Client.Refresh]. Keep the tokens server-side and never log them.
type TokenResponse struct {
	// AccessToken is an ES256 JWT (typ at+jwt) that expires within five
	// minutes. Verify it with [Client.VerifyAccessToken].
	AccessToken string `json:"access_token"`
	// IDToken is an ES256 JWT (typ JWT). Verify it with
	// [Client.VerifyIDToken]: with the sign-in nonce after a code exchange,
	// with an empty nonce after a refresh.
	IDToken string `json:"id_token"`
	// RefreshToken is opaque and single-use: every refresh returns a new one.
	RefreshToken string `json:"refresh_token"`
	// TokenType is "Bearer".
	TokenType string `json:"token_type"`
	// ExpiresIn is the access token lifetime in seconds.
	ExpiresIn int64 `json:"expires_in"`
	// SessionID is the session id, the sid claim of the tokens.
	SessionID string `json:"session_id"`
	// Raw is the response body as received, with fields this version does
	// not know.
	Raw json.RawMessage `json:"-"`
}

// ExchangeCode exchanges an authorization code for tokens at the issuer's
// token endpoint (grant_type authorization_code). code comes from the redirect
// after the caller checked its state; codeVerifier is the [PKCE] Verifier of
// that sign-in; redirectURI is the exact registered redirect URI sent to the
// authorization endpoint. A code is single-use and expires 60 seconds after
// it is issued.
//
// The request is form-encoded (the issuer also accepts JSON) and is never
// retried. A used, expired or unknown code, or a verifier or redirect URI
// that does not match, is a *didit.BadRequestError with body
// {"error": "invalid_grant"}; "mfa_required" means the person still has to
// complete a second factor.
func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI string) (*TokenResponse, error) {
	switch {
	case code == "":
		return nil, configError("code is required")
	case codeVerifier == "":
		return nil, configError("code verifier is required")
	case redirectURI == "":
		return nil, configError("redirect URI is required")
	}
	return c.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"redirect_uri":  {redirectURI},
	})
}

// Refresh exchanges a refresh token for new tokens (grant_type
// refresh_token). The refresh token is single-use: store the new one from the
// response.
//
// Serialise refreshes per session. Reusing a refresh token that was already
// consumed, for example by two concurrent requests, revokes the whole session;
// the second request gets a *didit.BadRequestError with body
// {"error": "invalid_grant"} and the person must sign in again. The request is
// never retried, for the same reason.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if refreshToken == "" {
		return nil, configError("refresh token is required")
	}
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (*TokenResponse, error) {
	meta, err := c.metadata(ctx)
	if err != nil {
		return nil, err
	}
	if c.clientID != "" {
		form.Set("client_id", c.clientID)
	}
	r := request{
		method:      http.MethodPost,
		url:         meta.tokenURL,
		contentType: contentTypeForm,
		body:        []byte(form.Encode()),
	}
	resp, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	var out TokenResponse
	if err := decodeJSON(r, resp.body, &out); err != nil {
		return nil, err
	}
	out.Raw = resp.body
	return &out, nil
}
