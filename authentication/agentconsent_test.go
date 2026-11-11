package authentication

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	didit "github.com/didit-protocol/sdk-go"
)

const testAgentChallenge = "pending-agent-request-secret"

func TestSubmitAgentConsentApprove(t *testing.T) {
	stub := newStub(t)
	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"code": "single-use-agent-code", "redirect_uri": "https://agent.example/callback", "state": "request-state",
		})
	})
	c := newTestClient(t, stub, newClock())
	userHandle := "dXNlcg"

	got, err := c.SubmitAgentConsent(context.Background(), testAccessToken, AgentConsentParams{
		Challenge: testAgentChallenge,
		Decision:  AgentConsentApprove,
		Assertion: &AgentPasskeyAssertion{
			ID: "Y3JlZA", RawID: "Y3JlZA",
			Response: AgentAuthenticatorResponse{
				ClientDataJSON: "Y2xpZW50", AuthenticatorData: "YXV0aA", Signature: "c2ln", UserHandle: &userHandle,
			},
		},
		Scopes:          []string{"read"},
		OrganizationIDs: []string{"6f1c1b3e-0a52-4b0e-9d3f-2f6a3c9d8e11"},
		Lifetime:        86400,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Approved() || got.Denied() || got.Code != "single-use-agent-code" ||
		got.RedirectURI != "https://agent.example/callback" || got.State != "request-state" || len(got.Raw) == 0 {
		t.Fatalf("consent = %+v", got)
	}
	req := stub.last("/agents/consent/")
	if req.method != http.MethodPost || req.header.Get("Authorization") != "Bearer "+testAccessToken ||
		req.header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %+v", req)
	}
	want := `{"challenge":"` + testAgentChallenge + `","decision":"approve","assertion":{"id":"Y3JlZA","rawId":"Y3JlZA","type":"public-key",` +
		`"response":{"clientDataJSON":"Y2xpZW50","authenticatorData":"YXV0aA","signature":"c2ln","userHandle":"dXNlcg"}},` +
		`"scopes":["read"],"organization_ids":["6f1c1b3e-0a52-4b0e-9d3f-2f6a3c9d8e11"],"lifetime":86400}`
	if req.body != want {
		t.Fatalf("body = %s", req.body)
	}
}

func TestSubmitAgentConsentDeny(t *testing.T) {
	stub := newStub(t)
	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "denied"})
	})
	c := newTestClient(t, stub, newClock())

	got, err := c.SubmitAgentConsent(context.Background(), testAccessToken, AgentConsentParams{
		Challenge: testAgentChallenge, Decision: AgentConsentDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Denied() || got.Approved() || got.Code != "" || got.Status != "denied" {
		t.Fatalf("consent = %+v", got)
	}
	if body := stub.last("/agents/consent/").body; body != `{"challenge":"`+testAgentChallenge+`","decision":"deny"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestSubmitAgentConsentErrors(t *testing.T) {
	stub := newStub(t)
	c := newTestClient(t, stub, newClock())
	ctx := context.Background()
	params := AgentConsentParams{Challenge: testAgentChallenge, Decision: AgentConsentApprove}

	_, err := c.SubmitAgentConsent(ctx, "", params)
	wantConfigError(t, err, "access token")
	_, err = c.SubmitAgentConsent(ctx, testAccessToken, AgentConsentParams{Decision: AgentConsentDeny})
	wantConfigError(t, err, "challenge is required")
	_, err = c.SubmitAgentConsent(ctx, testAccessToken, AgentConsentParams{Challenge: testAgentChallenge})
	wantConfigError(t, err, "decision must be")

	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
	})
	_, err = c.SubmitAgentConsent(ctx, testAccessToken, params)
	var bad *didit.BadRequestError
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error = %v (%T)", err, err)
	}
	if strings.Contains(err.Error(), testAgentChallenge) {
		t.Fatalf("error leaks the challenge: %v", err)
	}

	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "passkey_required"})
	})
	_, err = c.SubmitAgentConsent(ctx, testAccessToken, params)
	var forbidden *didit.PermissionDeniedError
	if !errors.As(err, &forbidden) || !strings.Contains(err.Error(), "passkey_required") {
		t.Fatalf("error = %v (%T)", err, err)
	}

	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "down"})
	})
	before := stub.count("/agents/consent/")
	_, err = c.SubmitAgentConsent(ctx, testAccessToken, params)
	if !errors.As(err, new(*didit.InternalServerError)) || stub.count("/agents/consent/") != before+1 {
		t.Fatalf("error = %v; POST agents/consent/ must not be retried", err)
	}

	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("[]")) })
	_, err = c.SubmitAgentConsent(ctx, testAccessToken, params)
	_ = wantUnexpected(t, err, "the 200 response is not valid JSON")
}

func TestCreateAgentApprovalOptions(t *testing.T) {
	stub := newStub(t)
	stub.handle("/agents/consent/passkey/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"options": map[string]any{
			"challenge": "c3ludGhldGljLWNoYWxsZW5nZQ", "rpId": "acme.auth.didit.me", "timeout": 60000,
			"userVerification": "required",
			"allowCredentials": []map[string]any{{"id": "c3ludGhldGljLWNyZWRlbnRpYWw", "type": "public-key", "transports": []string{"internal"}}},
		}})
	})
	c := newTestClient(t, stub, newClock())
	ctx := context.Background()

	got, err := c.CreateAgentApprovalOptions(ctx, testAccessToken, testAgentChallenge)
	if err != nil {
		t.Fatal(err)
	}
	o := got.Options
	if o.Challenge != "c3ludGhldGljLWNoYWxsZW5nZQ" || o.RPID != "acme.auth.didit.me" || o.Timeout != 60000 ||
		o.UserVerification != "required" || len(o.AllowCredentials) != 1 ||
		o.AllowCredentials[0].ID != "c3ludGhldGljLWNyZWRlbnRpYWw" || o.AllowCredentials[0].Type != "public-key" ||
		len(o.AllowCredentials[0].Transports) != 1 || len(got.Raw) == 0 {
		t.Fatalf("options = %+v", got)
	}
	req := stub.last("/agents/consent/passkey/")
	if req.method != http.MethodPost || req.header.Get("Authorization") != "Bearer "+testAccessToken ||
		req.body != `{"challenge":"`+testAgentChallenge+`"}` {
		t.Fatalf("request = %+v", req)
	}

	_, err = c.CreateAgentApprovalOptions(ctx, "", testAgentChallenge)
	wantConfigError(t, err, "access token")
	_, err = c.CreateAgentApprovalOptions(ctx, testAccessToken, "")
	wantConfigError(t, err, "challenge is required")

	for status, target := range map[int]any{
		http.StatusBadRequest: new(*didit.BadRequestError),
		http.StatusForbidden:  new(*didit.PermissionDeniedError),
		http.StatusNotFound:   new(*didit.NotFoundError),
	} {
		stub.handle("/agents/consent/passkey/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, status, map[string]any{"error": "invalid_grant"})
		})
		_, err = c.CreateAgentApprovalOptions(ctx, testAccessToken, testAgentChallenge)
		if err == nil || !errors.As(err, target) || strings.Contains(err.Error(), testAgentChallenge) {
			t.Fatalf("status %d: error = %v (%T)", status, err, err)
		}
	}

	stub.handle("/agents/consent/passkey/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "down"})
	})
	before := stub.count("/agents/consent/passkey/")
	_, err = c.CreateAgentApprovalOptions(ctx, testAccessToken, testAgentChallenge)
	if !errors.As(err, new(*didit.InternalServerError)) || stub.count("/agents/consent/passkey/") != before+1 {
		t.Fatalf("error = %v; POST agents/consent/passkey/ must not be retried", err)
	}
}
