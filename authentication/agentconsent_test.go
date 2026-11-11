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
	"CHALLENGE-m4rk-padpad0123456", "TOKEN-m4rk-padpad0123456", "ID-m4rk-padpad0123456", "RAWID-m4rk-padpad0123456", "CDJ-m4rk-padpad0123456", "AUTHDATA-m4rk-padpad0123456", "SIG-m4rk-padpad0123456",
	"HANDLE-m4rk-padpad0123456", "EXT-top-m4rk-padpad0123456", "EXT-deep-m4rk-padpad0123456", "EXT-list-m4rk-padpad0123456", "EXT-q\"uo<te>-m4rk-padpad0123456",
}

func TestSubmitAgentConsentNeverEchoesSecrets(t *testing.T) {
	handle := "HANDLE-m4rk-padpad0123456"
	params := AgentConsentParams{
		Challenge: echoSecrets[0],
		Decision:  AgentConsentApprove,
		Assertion: &AgentPasskeyAssertion{
			ID: "ID-m4rk-padpad0123456", RawID: "RAWID-m4rk-padpad0123456",
			Response: AgentAuthenticatorResponse{
				ClientDataJSON: "CDJ-m4rk-padpad0123456", AuthenticatorData: "AUTHDATA-m4rk-padpad0123456", Signature: "SIG-m4rk-padpad0123456", UserHandle: &handle,
			},
			ClientExtensionResults: map[string]any{
				"top": "EXT-top-m4rk-padpad0123456", "n": map[string]any{"deep": "EXT-deep-m4rk-padpad0123456", "l": []any{"EXT-list-m4rk-padpad0123456"}},
				"q": "EXT-q\"uo<te>-m4rk-padpad0123456",
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
			_, err := c.SubmitAgentConsent(context.Background(), "TOKEN-m4rk-padpad0123456", params)
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "x", "echo": "CHALLENGE-m4rk-padpad0123456 TOKEN-m4rk-padpad0123456"})
	})
	c := newTestClient(t, stub, newClock())
	_, err := c.CreateAgentApprovalOptions(context.Background(), "TOKEN-m4rk-padpad0123456", "CHALLENGE-m4rk-padpad0123456")
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
			ID:                     "ID-m4rk-padpad0123456",
			ClientExtensionResults: map[string]any{"bad": make(chan int), "leak": "EXT-m4rk-padpad0123456"},
		},
	})
	wantConfigError(t, err, "cannot be encoded")
	if n := stub.count("/agents/consent/"); n != 0 {
		t.Fatalf("%d requests sent, want none", n)
	}
	for _, secret := range []string{testAgentChallenge, "ID-m4rk-padpad0123456", "EXT-m4rk-padpad0123456"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaks %q", err, secret)
		}
	}
}

func TestAssertionStraddlingTheErrorBodyCapLeavesNoPrefix(t *testing.T) {
	secret := "Zq9ClientDataJSONSecretValue-0123456789"
	stub := newStub(t)
	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes-16) + secret + strings.Repeat("y", 64)))
	})
	c := newTestClient(t, stub, newClock())
	_, err := c.SubmitAgentConsent(context.Background(), testAccessToken, AgentConsentParams{
		Challenge: testAgentChallenge,
		Decision:  AgentConsentApprove,
		Assertion: &AgentPasskeyAssertion{ID: "Y3JlZA", RawID: "Y3JlZA", Response: AgentAuthenticatorResponse{ClientDataJSON: secret, AuthenticatorData: "YXV0aA", Signature: "c2ln"}},
	})
	var bad *didit.BadRequestError
	if !errors.As(err, &bad) {
		t.Fatalf("error = %v (%T)", err, err)
	}
	if strings.Contains(string(bad.RawBody), secret[:16]) || strings.Contains(err.Error(), secret[:16]) {
		t.Fatalf("a prefix of the assertion survived the body cap")
	}
}

func structuredConsentError(t *testing.T, assertion *AgentPasskeyAssertion, respond func(w http.ResponseWriter)) *didit.APIStatusError {
	t.Helper()
	stub := newStub(t)
	stub.handle("/agents/consent/", func(w http.ResponseWriter, r *http.Request) { respond(w) })
	c := newTestClient(t, stub, newClock())
	_, err := c.SubmitAgentConsent(context.Background(), "TOKEN-m4rk-padpad0123456", AgentConsentParams{
		Challenge: testAgentChallenge, Decision: AgentConsentApprove, Assertion: assertion,
	})
	var status *didit.APIStatusError
	if !errors.As(err, &status) {
		t.Fatalf("want an APIStatusError, got %v", err)
	}
	return status
}

func shownError(status *didit.APIStatusError) string {
	return status.Error() + "\n" + string(status.RawBody) + "\n" + fmt.Sprintf("%v", status.Body)
}

func TestConsentRedactionStructural(t *testing.T) {
	assertion := func(id, sig string) *AgentPasskeyAssertion {
		return &AgentPasskeyAssertion{ID: id, RawID: "RAW-x", Response: AgentAuthenticatorResponse{Signature: sig}}
	}
	t.Run("prefixed secrets leave no fragment", func(t *testing.T) {
		status := structuredConsentError(t, assertion("abcCREDENTIALID0123", "abcCREDENTIALID0123456"), func(w http.ResponseWriter) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "bad abcCREDENTIALID0123456 and abcCREDENTIALID0123", "other": "abcCREDENTIALID0123456"})
		})
		shown := shownError(status)
		if strings.Contains(shown, "abcCRED") || strings.Contains(shown, "456") {
			t.Fatalf("fragment left: %s", shown)
		}
		if status.Body.(map[string]any)["detail"] != "[redacted]" {
			t.Fatalf("body = %v", status.Body)
		}
	})
	t.Run("a string containing a secret is redacted whole", func(t *testing.T) {
		status := structuredConsentError(t, assertion("SECRET-0123456789abcdef", "s"), func(w http.ResponseWriter) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "bad assertion xyzSECRET-0123456789abcdefxyz", "keep": "fine"})
		})
		body := status.Body.(map[string]any)
		if body["detail"] != "[redacted]" || body["keep"] != "fine" {
			t.Fatalf("body = %v", body)
		}
		if !strings.Contains(status.Error(), "[redacted]") || strings.Contains(status.Error(), "SECRET-0123456789abcdef") {
			t.Fatalf("message = %q", status.Error())
		}
	})
	t.Run("a secret used as an object key", func(t *testing.T) {
		status := structuredConsentError(t, assertion("SECRET-0123456789abcdef", "s"), func(w http.ResponseWriter) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "no", "echo-SECRET-0123456789abcdef-1": "a", "echo-SECRET-0123456789abcdef-2": "b"})
		})
		body := status.Body.(map[string]any)
		if len(body) != 3 || body["detail"] != "no" || body["[redacted]"] == nil || body["[redacted]2"] == nil {
			t.Fatalf("body = %v", body)
		}
		if strings.Contains(shownError(status), "SECRET-0123456789abcdef") {
			t.Fatalf("leak: %s", shownError(status))
		}
	})
	t.Run("other JSON escape spellings", func(t *testing.T) {
		status := structuredConsentError(t, assertion("a/b+\u00e9-padding0123456", "s"), func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"x","echo":"a\/b+\u00E9-padding0123456"}`))
		})
		body := status.Body.(map[string]any)
		if body["detail"] != "x" || body["echo"] != "[redacted]" {
			t.Fatalf("body = %v", body)
		}
		shown := shownError(status)
		for _, bad := range []string{"a/b", "00E9", "\u00e9", "padding0123456"} {
			if strings.Contains(shown, bad) {
				t.Fatalf("%q left in %s", bad, shown)
			}
		}
	})
	t.Run("error codes and short assertion strings stay readable", func(t *testing.T) {
		long := "Y3JlZGVudGlhbC1pZC0xMjM0NTY3ODkw"
		status := structuredConsentError(t, assertion(long, "s"), func(w http.ResponseWriter) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "detail": "id public-key " + long})
		})
		body := status.Body.(map[string]any)
		if body["error"] != "invalid_grant" || body["detail"] != "[redacted]" {
			t.Fatalf("body = %v", body)
		}
		if !strings.Contains(shownError(status), "invalid_grant") || strings.Contains(shownError(status), long) {
			t.Fatalf("shown = %s", shownError(status))
		}
	})
	t.Run("a body that is not JSON", func(t *testing.T) {
		status := structuredConsentError(t, assertion("x", "s"), func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>Bad gateway " + testAgentChallenge + "</html>"))
		})
		if status.Body != "[redacted]" || string(status.RawBody) != "[redacted]" {
			t.Fatalf("body = %v raw = %q", status.Body, status.RawBody)
		}
		if strings.Contains(shownError(status), testAgentChallenge) {
			t.Fatal("challenge left")
		}
	})
	t.Run("a secret past the message cap", func(t *testing.T) {
		status := structuredConsentError(t, assertion("x", "s"), func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(strings.Repeat("x", 600) + testAgentChallenge))
		})
		if status.Body != "[redacted]" || strings.Contains(shownError(status), testAgentChallenge) {
			t.Fatalf("body = %v", status.Body)
		}
	})
}
