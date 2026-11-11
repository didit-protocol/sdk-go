package authentication

import (
	"context"
	"encoding/json"
	"net/http"
)

// AgentConsentDecision is the person's decision on an agent request.
type AgentConsentDecision string

// Agent consent decisions.
const (
	// AgentConsentApprove grants the agent request.
	AgentConsentApprove AgentConsentDecision = "approve"
	// AgentConsentDeny declines it; no assertion is needed.
	AgentConsentDeny AgentConsentDecision = "deny"
)

// AgentConsentStatusDenied is the Status of a denied [AgentConsent].
const AgentConsentStatusDenied = "denied"

// AgentAuthenticatorResponse is the WebAuthn authentication response of an
// [AgentPasskeyAssertion]. Every binary field is base64url.
type AgentAuthenticatorResponse struct {
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	// UserHandle is set when the authenticator supplied one.
	UserHandle *string `json:"userHandle,omitempty"`
}

// AgentPasskeyAssertion is the JSON-serialised result of
// navigator.credentials.get() for the options of
// [Client.CreateAgentApprovalOptions]. Encode binary fields as base64url;
// never send an unconverted PublicKeyCredential.
type AgentPasskeyAssertion struct {
	ID    string `json:"id"`
	RawID string `json:"rawId"`
	// Type is "public-key"; empty is sent as "public-key".
	Type                   string                     `json:"type"`
	Response               AgentAuthenticatorResponse `json:"response"`
	ClientExtensionResults map[string]any             `json:"clientExtensionResults,omitempty"`
}

// AgentConsentParams are the parameters of [Client.SubmitAgentConsent].
// Empty optional fields are not sent.
type AgentConsentParams struct {
	// Challenge is the pending agent request secret. Required.
	Challenge string
	// Decision is [AgentConsentApprove] or [AgentConsentDeny]. Required.
	Decision AgentConsentDecision
	// Assertion is the fresh passkey assertion bound to the request. When the
	// application enables agent_humans_only, approval requires it; denial
	// never does.
	Assertion *AgentPasskeyAssertion
	// Scopes narrows the requested scopes ("read", "act"); empty means all
	// requested scopes.
	Scopes []string
	// OrganizationIDs are the organizations the person belongs to that the
	// grant covers; empty means none.
	OrganizationIDs []string
	// Lifetime is the grant lifetime in seconds (86400, 604800 or 2592000),
	// limited by the application policy; zero means the policy lifetime.
	Lifetime int
}

// AgentConsent is the issuer's answer to [Client.SubmitAgentConsent]. The
// response has no shared discriminator: an approval carries Code,
// RedirectURI and State, a denial carries Status "denied".
type AgentConsent struct {
	// Code is the single-use agent authorization code, valid for 60 seconds;
	// exchange it through the agent token flow with its PKCE verifier. Empty
	// when denied.
	Code string `json:"code"`
	// RedirectURI is the registered redirect URI of the agent request.
	RedirectURI string `json:"redirect_uri"`
	// State is the state of the agent request.
	State string `json:"state"`
	// Status is "denied" when the person declined; empty on approval.
	Status string `json:"status"`
	// Raw is the response body as received, with fields this version does
	// not know.
	Raw json.RawMessage `json:"-"`
}

// Approved reports whether the consent was approved, that is whether the
// issuer returned an authorization code.
func (a *AgentConsent) Approved() bool { return a.Code != "" }

// Denied reports whether the person declined the request.
func (a *AgentConsent) Denied() bool { return a.Status == AgentConsentStatusDenied }

// AgentWebAuthnCredential is a passkey of the approving person.
type AgentWebAuthnCredential struct {
	// ID is the base64url credential identifier.
	ID   string `json:"id"`
	Type string `json:"type"`
	// Transports are optional authenticator transport hints.
	Transports []string `json:"transports"`
}

// AgentWebAuthnOptions are WebAuthn request options for a fresh approval:
// pass them as publicKey to navigator.credentials.get() after decoding the
// binary fields.
type AgentWebAuthnOptions struct {
	// Challenge is the base64url WebAuthn challenge, valid for less than 300
	// seconds and bound to this approval.
	Challenge string `json:"challenge"`
	// RPID is the relying-party hostname.
	RPID string `json:"rpId"`
	// Timeout is the browser timeout in milliseconds; it does not extend the
	// server expiry.
	Timeout int64 `json:"timeout"`
	// UserVerification is always "required".
	UserVerification string `json:"userVerification"`
	// AllowCredentials are only the approving person's enrolled passkeys.
	AllowCredentials []AgentWebAuthnCredential `json:"allowCredentials"`
}

// AgentApprovalOptions is the answer of [Client.CreateAgentApprovalOptions].
type AgentApprovalOptions struct {
	Options AgentWebAuthnOptions `json:"options"`
	// Raw is the response body as received, with fields this version does
	// not know.
	Raw json.RawMessage `json:"-"`
}

type agentConsentBody struct {
	Challenge       string                 `json:"challenge"`
	Decision        AgentConsentDecision   `json:"decision"`
	Assertion       *AgentPasskeyAssertion `json:"assertion,omitempty"`
	Scopes          []string               `json:"scopes,omitempty"`
	OrganizationIDs []string               `json:"organization_ids,omitempty"`
	Lifetime        int                    `json:"lifetime,omitempty"`
}

// SubmitAgentConsent records the signed-in person's approval or denial of an
// agent request (POST agents/consent/). An approval returns an
// [AgentConsent] with Code, RedirectURI and State; a denial returns Status
// "denied". When the application requires humans only, approval needs a
// fresh Assertion obtained with [Client.CreateAgentApprovalOptions]; without
// one the issuer answers 403 passkey_required.
//
// The request is not retried, and the challenge and assertion never appear
// in errors. An invalid or expired challenge is a *didit.BadRequestError
// (invalid_grant).
func (c *Client) SubmitAgentConsent(ctx context.Context, accessToken string, params AgentConsentParams) (*AgentConsent, error) {
	if err := checkBearer(accessToken); err != nil {
		return nil, err
	}
	if params.Challenge == "" {
		return nil, configError("agent consent challenge is required")
	}
	if params.Decision != AgentConsentApprove && params.Decision != AgentConsentDeny {
		return nil, configError(`agent consent decision must be "approve" or "deny"`)
	}
	if params.Assertion != nil && params.Assertion.Type == "" {
		a := *params.Assertion
		a.Type = "public-key"
		params.Assertion = &a
	}
	body, _ := json.Marshal(agentConsentBody(params)) // plain values: cannot fail
	r := request{
		method:      http.MethodPost,
		url:         c.endpoint("agents/consent/"),
		bearer:      accessToken,
		contentType: contentTypeJSON,
		body:        body,
		secrets:     assertionSecrets(body, accessToken, params.Challenge),
	}
	resp, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	var out AgentConsent
	if err := decodeJSON(r, resp, &out); err != nil {
		return nil, err
	}
	out.Raw = resp.body
	return &out, nil
}

type agentApprovalOptionsBody struct {
	Challenge string `json:"challenge"`
}

// CreateAgentApprovalOptions returns the WebAuthn request options that bind
// a fresh passkey approval to the pending agent request and the signed-in
// person (POST agents/consent/passkey/). Run navigator.credentials.get() with
// them in the browser and pass the serialised result as
// [AgentConsentParams.Assertion].
//
// The request is not retried, and the challenge never appears in errors.
func (c *Client) CreateAgentApprovalOptions(ctx context.Context, accessToken, challenge string) (*AgentApprovalOptions, error) {
	if err := checkBearer(accessToken); err != nil {
		return nil, err
	}
	if challenge == "" {
		return nil, configError("agent consent challenge is required")
	}
	body, _ := json.Marshal(agentApprovalOptionsBody{Challenge: challenge}) // string only: cannot fail
	r := request{
		method:      http.MethodPost,
		url:         c.endpoint("agents/consent/passkey/"),
		bearer:      accessToken,
		contentType: contentTypeJSON,
		body:        body,
		secrets:     []string{challenge, accessToken},
	}
	resp, err := c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	var out AgentApprovalOptions
	if err := decodeJSON(r, resp, &out); err != nil {
		return nil, err
	}
	out.Raw = resp.body
	return &out, nil
}
