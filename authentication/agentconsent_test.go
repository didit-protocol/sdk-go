package authentication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// echoSecrets are distinct markers for every position of the consent call.
var echoSecrets = []string{
	"CHALLENGE-m4rk", "TOKEN-m4rk", "ID-m4rk", "RAWID-m4rk", "CDJ-m4rk", "AUTHDATA-m4rk", "SIG-m4rk",
	"HANDLE-m4rk", "EXT-top-m4rk", "EXT-deep-m4rk", "EXT-list-m4rk", "EXT-q\"uo<te>-m4rk",
}

func TestSubmitAgentConsentNeverEchoesSecrets(t *testing.T) {
	handle := "HANDLE-m4rk"
	params := AgentConsentParams{
		Challenge: echoSecrets[0],
		Decision:  AgentConsentApprove,
		Assertion: &AgentPasskeyAssertion{
			ID: "ID-m4rk", RawID: "RAWID-m4rk",
			Response: AgentAuthenticatorResponse{
				ClientDataJSON: "CDJ-m4rk", AuthenticatorData: "AUTHDATA-m4rk", Signature: "SIG-m4rk", UserHandle: &handle,
			},
			ClientExtensionResults: map[string]any{
				"top": "EXT-top-m4rk", "n": map[string]any{"deep": "EXT-deep-m4rk", "l": []any{"EXT-list-m4rk"}},
				"q": "EXT-q\"uo<te>-m4rk",
			},
		},
	}
	echoJSON := map[string]any{"detail": "passkey_required", "echo": echoSecrets, "nested": map[string]any{"a": echoSecrets[2:]}}
	escaped, _ := json.Marshal(echoJSON)
	slashed := strings.ReplaceAll(string(escaped), "/", `\/`)
	cases := map[string]func(w http.ResponseWriter){
		"400 json": func(w http.ResponseWriter) { writeJSON(w, http.StatusBadRequest, echoJSON) },
		"400 raw text": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("bad " + strings.Join(echoSecrets, " ") + " " + string(escaped) + slashed))
		},
		"200 malformed": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":` + string(escaped)))
		},
		"200 wrong type": func(w http.ResponseWriter) { writeJSON(w, http.StatusOK, map[string]any{"code": echoJSON}) },
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newStub(t)
			stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) { respond(w) })
			c := newTestClient(t, stub, newClock())
			_, err := c.SubmitAgentConsent(context.Background(), "TOKEN-m4rk", params)
			if err == nil {
				t.Fatal("want an error")
			}
			var status *didit.APIStatusError
			var unexpected *didit.UnexpectedResponseError
			texts := []string{err.Error(), fmt.Sprintf("%v %+v %#v", err, err, err)}
			if errors.As(err, &status) {
				texts = append(texts, string(status.RawBody), fmt.Sprintf("%v", status.Body), status.Method+status.URL)
				if status.Response != nil && status.Response.Body != nil {
					b, _ := io.ReadAll(status.Response.Body)
					texts = append(texts, string(b))
				}
				if name == "400 json" {
					if m, _ := status.Body.(map[string]any); m["detail"] != "passkey_required" {
						t.Fatalf("non-secret detail lost: %v", status.Body)
					}
				}
			}
			if errors.As(err, &unexpected) {
				texts = append(texts, unexpected.Message, fmt.Sprint(unexpected.Cause))
			}
			for _, text := range texts {
				for _, secret := range echoSecrets {
					esc, _ := json.Marshal(secret)
					if strings.Contains(text, secret) || strings.Contains(text, strings.Trim(string(esc), `"`)) {
						t.Fatalf("%q leaks %q", text, secret)
					}
				}
			}
		})
	}
}

func TestCreateAgentApprovalOptionsNeverEchoesChallenge(t *testing.T) {
	stub := newStub(t)
	stub.handle("/agents/consent/passkey/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "x", "echo": "CHALLENGE-m4rk TOKEN-m4rk"})
	})
	c := newTestClient(t, stub, newClock())
	_, err := c.CreateAgentApprovalOptions(context.Background(), "TOKEN-m4rk", "CHALLENGE-m4rk")
	var status *didit.APIStatusError
	if !errors.As(err, &status) || strings.Contains(err.Error()+string(status.RawBody)+fmt.Sprint(status.Body), "m4rk") {
		t.Fatalf("error = %v", err)
	}
}

func TestSubmitAgentConsentRejectsUnencodableAssertionBeforeSending(t *testing.T) {
	stub := newStub(t)
	c := newTestClient(t, stub, newClock())
	_, err := c.SubmitAgentConsent(context.Background(), testAccessToken, AgentConsentParams{
		Challenge: testAgentChallenge,
		Decision:  AgentConsentApprove,
		Assertion: &AgentPasskeyAssertion{
			ID:                     "ID-m4rk",
			ClientExtensionResults: map[string]any{"bad": make(chan int), "leak": "EXT-m4rk"},
		},
	})
	wantConfigError(t, err, "cannot be encoded")
	if n := stub.count("/agents/consent/"); n != 0 {
		t.Fatalf("%d requests sent, want none", n)
	}
	for _, secret := range []string{testAgentChallenge, "ID-m4rk", "EXT-m4rk"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaks %q", err, secret)
		}
	}
}
