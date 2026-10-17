package authentication

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

const (
	testAccessToken  = "access-token-under-test"
	testRefreshToken = "refresh-token-under-test"
	testCode         = "code-under-test"
)

func TestDiscoveryChecks(t *testing.T) {
	key := newTestKey(t, "k_one")
	token := key.mint(t, nil, accessClaims())
	ctx := context.Background()

	tests := []struct {
		name    string
		changes map[string]any
		want    string
	}{
		{"issuer mismatch", map[string]any{"issuer": "https://other.auth.didit.me"}, `names issuer "https://other.auth.didit.me"`},
		{"issuer with trailing slash", map[string]any{"issuer": testIssuer + "/"}, "names issuer"},
		{"issuer missing", map[string]any{"issuer": remove}, `names issuer ""`},
		{"token endpoint on another origin", map[string]any{"token_endpoint": "https://evil.example/token/"}, "token_endpoint is not on the issuer's origin"},
		{"token endpoint downgraded to http", map[string]any{"token_endpoint": "http://acme.auth.didit.me/token/"}, "token_endpoint"},
		{"token endpoint relative", map[string]any{"token_endpoint": "/token/"}, "token_endpoint"},
		{"jwks on another origin", map[string]any{"jwks_uri": "https://cdn.example/jwks.json"}, "jwks_uri is not on the issuer's origin"},
		{"jwks with credentials", map[string]any{"jwks_uri": "https://u:p@acme.auth.didit.me/jwks"}, "jwks_uri"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub(t, key)
			stub.setDiscovery(tt.changes)
			c := newTestClient(t, stub, newClock())
			_, err := c.VerifyAccessToken(ctx, token)
			wantConfigError(t, err, tt.want)
			_, err = c.ExchangeCode(ctx, testCode, "verifier", "https://app.example/callback")
			wantConfigError(t, err, tt.want)
			if stub.count(jwksPath) != 0 || stub.count("/token/") != 0 || stub.count("/jwks.json") != 0 {
				t.Fatal("requests went past discovery")
			}
			if stub.count(discoveryPath) != 2 {
				t.Fatalf("discovery fetches = %d; a refused document must not be cached", stub.count(discoveryPath))
			}
		})
	}

	t.Run("defaults when endpoints are omitted", func(t *testing.T) {
		stub := newStub(t, key)
		stub.setDiscovery(map[string]any{"token_endpoint": remove, "jwks_uri": remove})
		c := newTestClient(t, stub, newClock())
		if _, err := c.VerifyAccessToken(ctx, token); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Refresh(ctx, testRefreshToken); err != nil {
			t.Fatal(err)
		}
		if stub.count(jwksPath) != 1 || stub.count("/token/") != 1 {
			t.Fatal("default endpoints not used")
		}
	})

	t.Run("token endpoint from discovery", func(t *testing.T) {
		stub := newStub(t, key)
		stub.setDiscovery(map[string]any{"token_endpoint": "https://ACME.auth.didit.me:443/oauth/token"})
		stub.handle("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"access_token": "a"})
		})
		c := newTestClient(t, stub, newClock())
		if _, err := c.Refresh(ctx, testRefreshToken); err != nil {
			t.Fatal(err)
		}
		if stub.count("/oauth/token") != 1 {
			t.Fatal("discovery token_endpoint not used")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		stub := newStub(t, key)
		stub.handle(discoveryPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>welcome</html>"))
		})
		c := newTestClient(t, stub, newClock())
		_, err := c.VerifyAccessToken(ctx, token)
		var conn *didit.APIConnectionError
		if !errors.As(err, &conn) || !strings.Contains(err.Error(), "invalid response body") {
			t.Fatalf("error = %v (%T), want APIConnectionError", err, err)
		}
	})

	t.Run("404 is not retried, 500 is retried once", func(t *testing.T) {
		stub := newStub(t, key)
		stub.handle(discoveryPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-Id", "req-1")
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Not found."})
		})
		c := newTestClient(t, stub, newClock())
		_, err := c.VerifyAccessToken(ctx, token)
		var notFound *didit.NotFoundError
		if !errors.As(err, &notFound) || notFound.RequestID != "req-1" || stub.count(discoveryPath) != 1 {
			t.Fatalf("error = %v (%T), %d requests", err, err, stub.count(discoveryPath))
		}

		calls := 0
		stub.handle(discoveryPath, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				writeJSON(w, http.StatusBadGateway, map[string]any{"detail": "upstream"})
				return
			}
			stub.defaultHandler(w, r)
		})
		if _, err := c.VerifyAccessToken(ctx, token); err != nil {
			t.Fatalf("retry did not recover: %v", err)
		}
		if calls != 2 {
			t.Fatalf("discovery calls = %d, want 2", calls)
		}
	})
}

func TestKeySetResponses(t *testing.T) {
	key := newTestKey(t, "k_one")
	token := key.mint(t, nil, accessClaims())
	tests := map[string]string{
		"not JSON":       "nope",
		"no keys member": `{"other": []}`,
		"keys null":      `{"keys": null}`,
		"keys not array": `{"keys": {}}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			stub := newStub(t, key)
			stub.handle(jwksPath, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			c := newTestClient(t, stub, newClock())
			_, err := c.VerifyAccessToken(context.Background(), token)
			var conn *didit.APIConnectionError
			if !errors.As(err, &conn) {
				t.Fatalf("error = %v (%T), want APIConnectionError", err, err)
			}
		})
	}

	t.Run("unusable keys are ignored", func(t *testing.T) {
		stub := newStub(t, key)
		stub.handle(jwksPath, func(w http.ResponseWriter, r *http.Request) {
			impostor := newTestKey(t, "k_one").jwk(t)
			writeJSON(w, http.StatusOK, map[string]any{"keys": []any{
				map[string]any{"kty": "RSA", "kid": "k_one", "n": "AQAB", "e": "AQAB"},
				with(impostor, map[string]any{"use": "enc"}),
				"not an object",
				key.jwk(t),
				impostor, // a later key with the same kid does not replace the first
			}})
		})
		c := newTestClient(t, stub, newClock())
		if _, err := c.VerifyAccessToken(context.Background(), token); err != nil {
			t.Fatal(err)
		}
	})
}

func TestExchangeCode(t *testing.T) {
	stub := newStub(t)
	c := newTestClient(t, stub, newClock(), WithClientID(testAudience))
	resp, err := c.ExchangeCode(context.Background(), testCode, "verifier-1234567890123456789012345678901234567", "https://app.example/callback?x=1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken != "access-token-value" || resp.IDToken != "id-token-value" || resp.RefreshToken != "refresh-token-value-2" ||
		resp.TokenType != "Bearer" || resp.ExpiresIn != 300 || resp.SessionID != "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d" {
		t.Fatalf("response = %+v", resp)
	}
	var raw map[string]any
	if err := json.Unmarshal(resp.Raw, &raw); err != nil || raw["session_id"] == nil {
		t.Fatalf("raw = %s", resp.Raw)
	}

	req := stub.last("/token/")
	if req.method != http.MethodPost || req.host != "acme.auth.didit.me" {
		t.Fatalf("request = %s %s", req.method, req.host)
	}
	if req.header.Get("Content-Type") != "application/x-www-form-urlencoded" || req.header.Get("Authorization") != "" ||
		req.header.Get("User-Agent") != userAgent || req.header.Get("X-Didit-Client") != clientHeader {
		t.Fatalf("headers = %v", req.header)
	}
	form, err := url.ParseQuery(req.body)
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {testCode},
		"code_verifier": {"verifier-1234567890123456789012345678901234567"},
		"redirect_uri":  {"https://app.example/callback?x=1"},
		"client_id":     {testAudience},
	}
	if form.Encode() != want.Encode() {
		t.Fatalf("form = %v, want %v", form, want)
	}

	for _, args := range [][3]string{{"", "v", "r"}, {"c", "", "r"}, {"c", "v", ""}} {
		_, err := c.ExchangeCode(context.Background(), args[0], args[1], args[2])
		wantConfigError(t, err, "required")
	}
}

func TestRefresh(t *testing.T) {
	stub := newStub(t)
	c := newTestClient(t, stub, newClock())
	if _, err := c.Refresh(context.Background(), testRefreshToken); err != nil {
		t.Fatal(err)
	}
	form, err := url.ParseQuery(stub.last("/token/").body)
	if err != nil {
		t.Fatal(err)
	}
	// Without WithClientID no client_id is sent.
	if form.Encode() != (url.Values{"grant_type": {"refresh_token"}, "refresh_token": {testRefreshToken}}).Encode() {
		t.Fatalf("form = %v", form)
	}
	_, err = c.Refresh(context.Background(), "")
	wantConfigError(t, err, "refresh token is required")
}

func TestTokenRequestsAreNeverRetried(t *testing.T) {
	ctx := context.Background()

	t.Run("500", func(t *testing.T) {
		stub := newStub(t)
		stub.handle("/token/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "boom"})
		})
		c := newTestClient(t, stub, newClock())
		_, err := c.ExchangeCode(ctx, testCode, "verifier", "https://app.example/cb")
		var serverErr *didit.InternalServerError
		if !errors.As(err, &serverErr) {
			t.Fatalf("error = %v (%T)", err, err)
		}
		_, err = c.Refresh(ctx, testRefreshToken)
		if !errors.As(err, &serverErr) {
			t.Fatalf("error = %v (%T)", err, err)
		}
		if n := stub.count("/token/"); n != 2 {
			t.Fatalf("token requests = %d, want 2 (one per call)", n)
		}
	})

	t.Run("connection error", func(t *testing.T) {
		stub := newStub(t)
		stub.setTransportError(func(r *http.Request) error {
			if r.URL.Path == "/token/" {
				return errors.New("connection reset by peer")
			}
			return nil
		})
		c := newTestClient(t, stub, newClock())
		_, err := c.Refresh(ctx, testRefreshToken)
		var conn *didit.APIConnectionError
		if !errors.As(err, &conn) || conn.Method != http.MethodPost || conn.URL != testIssuer+"/token/" {
			t.Fatalf("error = %v (%T)", err, err)
		}
		if n := stub.count("/token/"); n != 1 {
			t.Fatalf("token requests = %d, want 1", n)
		}
		if strings.Contains(err.Error(), testRefreshToken) {
			t.Fatal("error message contains the refresh token")
		}
	})

	t.Run("invalid_grant", func(t *testing.T) {
		stub := newStub(t)
		stub.handle("/token/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-Id", "req-grant")
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		})
		c := newTestClient(t, stub, newClock())
		_, err := c.Refresh(ctx, testRefreshToken)
		var bad *didit.BadRequestError
		if !errors.As(err, &bad) {
			t.Fatalf("error = %v (%T)", err, err)
		}
		body, _ := bad.Body.(map[string]any)
		if body["error"] != "invalid_grant" || bad.RequestID != "req-grant" || bad.Method != http.MethodPost ||
			bad.URL != testIssuer+"/token/" || !strings.Contains(err.Error(), "invalid_grant") {
			t.Fatalf("error = %+v", bad.APIStatusError)
		}
		// The error does not carry the request body (the refresh token).
		if req := bad.Response.Request; req == nil || req.GetBody != nil || req.Body != http.NoBody {
			t.Fatal("the error's request still carries its body")
		}
		if strings.Contains(err.Error(), testRefreshToken) {
			t.Fatal("error message contains the refresh token")
		}
		if n := stub.count("/token/"); n != 1 {
			t.Fatalf("token requests = %d, want 1", n)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		stub := newStub(t)
		stub.handle("/token/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"access_token": 1}`)) })
		c := newTestClient(t, stub, newClock())
		_, err := c.Refresh(ctx, testRefreshToken)
		var conn *didit.APIConnectionError
		if !errors.As(err, &conn) {
			t.Fatalf("error = %v (%T)", err, err)
		}
	})
}

func sessionBody() map[string]any {
	return map[string]any{
		"sub":                      "6f0f7a43-1c64-4b1e-8d1f-7f0e8f4a2b10",
		"sid":                      "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
		"application_id":           testAudience,
		"vendor_data":              "customer-42",
		"amr":                      []string{"password"},
		"mfa_verified":             true,
		"email":                    "person@example.test",
		"email_verified":           true,
		"phone":                    nil,
		"verified_identity":        nil,
		"verified":                 map[string]any{"level": "liveness", "liveness": true},
		"expires_at":               "2026-11-16T12:00:00.123456Z",
		"post_logout_redirect_uri": "https://app.example/bye",
		"organizations": []any{map[string]any{
			"organization_id":    "org-1",
			"organization__name": "Northwind",
			"role":               "admin",
			"custom_role":        map[string]any{"key": "auditor", "permissions": []string{"read:reports"}},
		}},
		"new_field": "kept in Raw",
	}
}

func TestGetSession(t *testing.T) {
	stub := newStub(t)
	stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testAccessToken {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "invalid_token"})
			return
		}
		writeJSON(w, http.StatusOK, sessionBody())
	})
	c := newTestClient(t, stub, newClock())
	ctx := context.Background()

	s, err := c.GetSession(ctx, testAccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if s.Subject != "6f0f7a43-1c64-4b1e-8d1f-7f0e8f4a2b10" || s.SessionID != "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d" ||
		s.ApplicationID != testAudience || s.VendorData != "customer-42" || s.AMR[0] != "password" || !s.MFAVerified ||
		s.Email != "person@example.test" || !s.EmailVerified || s.Phone != "" || s.VerifiedIdentity != nil ||
		s.Verified.Level != "liveness" || s.Verified.Liveness == nil || !*s.Verified.Liveness ||
		s.PostLogoutRedirectURI != "https://app.example/bye" {
		t.Fatalf("session = %+v", s)
	}
	if want := time.Date(2026, 11, 16, 12, 0, 0, 123456000, time.UTC); !s.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at = %v", s.ExpiresAt)
	}
	if len(s.Organizations) != 1 || s.Organizations[0].ID != "org-1" || s.Organizations[0].Name != "Northwind" ||
		s.Organizations[0].Role != "admin" || s.Organizations[0].CustomRole == nil ||
		s.Organizations[0].CustomRole.Key != "auditor" || s.Organizations[0].CustomRole.Permissions[0] != "read:reports" {
		t.Fatalf("organizations = %+v", s.Organizations)
	}
	if !strings.Contains(string(s.Raw), "kept in Raw") {
		t.Fatalf("raw = %s", s.Raw)
	}
	req := stub.last("/session/")
	if req.method != http.MethodGet || req.header.Get("Accept") != "application/json" {
		t.Fatalf("request = %+v", req)
	}
	if stub.count(discoveryPath) != 0 {
		t.Fatal("session/ does not need discovery")
	}

	// 401: revoked, refreshed or invalid. The error never carries the token.
	_, err = c.GetSession(ctx, "revoked-access-token")
	var authErr *didit.AuthenticationError
	if !errors.As(err, &authErr) || authErr.StatusCode != 401 {
		t.Fatalf("error = %v (%T)", err, err)
	}
	if strings.Contains(err.Error(), "revoked-access-token") {
		t.Fatal("error message contains the token")
	}
	if got := authErr.Response.Request.Header.Get("Authorization"); got != "Bearer [redacted]" {
		t.Fatalf("error request Authorization = %q", got)
	}
	if n := stub.count("/session/"); n != 2 {
		t.Fatalf("session requests = %d, want 2 (401 is not retried)", n)
	}

	before := len(stub.all())
	for _, bad := range []string{"", "two words", "line\nbreak"} {
		_, err := c.GetSession(ctx, bad)
		wantConfigError(t, err, "access token")
	}
	if len(stub.all()) != before {
		t.Fatal("invalid tokens were sent")
	}
}

func TestGetSessionRetriesOnce(t *testing.T) {
	stub := newStub(t)
	calls := 0
	stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeJSON(w, http.StatusServiceUnavailable, nil)
			return
		}
		writeJSON(w, http.StatusOK, sessionBody())
	})
	c := newTestClient(t, stub, newClock())
	if _, err := c.GetSession(context.Background(), testAccessToken); err != nil {
		t.Fatal(err)
	}

	// Two failures: the second is returned.
	calls = 0
	stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	})
	_, err := c.GetSession(context.Background(), testAccessToken)
	var serverErr *didit.InternalServerError
	if !errors.As(err, &serverErr) || serverErr.Body != "bad gateway" || calls != 2 {
		t.Fatalf("error = %v (%T), %d calls", err, err, calls)
	}
}

func TestRevokeSession(t *testing.T) {
	stub := newStub(t)
	stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.Header.Get("Authorization") != "Bearer "+testAccessToken {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "invalid_token"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTestClient(t, stub, newClock())
	if err := c.RevokeSession(context.Background(), testAccessToken); err != nil {
		t.Fatal(err)
	}
	if err := c.RevokeSession(context.Background(), ""); err == nil {
		t.Fatal("empty token accepted")
	}

	stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": "boom"})
	})
	err := c.RevokeSession(context.Background(), testAccessToken)
	var serverErr *didit.InternalServerError
	if !errors.As(err, &serverErr) {
		t.Fatalf("error = %v (%T)", err, err)
	}
	if n := stub.count("/session/"); n != 2 {
		t.Fatalf("session requests = %d, want 2 (DELETE is not retried)", n)
	}
}

func stepUpRecord() map[string]any {
	return map[string]any{
		"id":           "4c1d2e3f-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
		"rule":         "payout",
		"method":       "selfie",
		"status":       "pending",
		"level":        nil,
		"amount":       "900.00",
		"currency":     "USD",
		"created_at":   "2026-10-17T12:00:00Z",
		"completed_at": nil,
		"expires_at":   "2026-10-17T12:30:00Z",
	}
}

func TestCreateStepUp(t *testing.T) {
	stub := newStub(t)
	stub.handle("/step-up/", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["action"] != "payout" {
			writeJSON(w, http.StatusOK, map[string]any{"required": false})
			return
		}
		writeJSON(w, http.StatusCreated, with(stepUpRecord(), map[string]any{
			"required":         true,
			"verification_url": testIssuer + "/verify/?verification=proof",
			"expires_in":       1800,
		}))
	})
	c := newTestClient(t, stub, newClock())
	ctx := context.Background()

	s, err := c.CreateStepUp(ctx, testAccessToken, StepUpParams{
		Action: StepUpActionPayout, Amount: "900", Currency: "USD", ReturnTo: "https://app.example/payout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Required || s.ID != "4c1d2e3f-5a6b-4c7d-8e9f-0a1b2c3d4e5f" || s.Rule != "payout" || s.Method != "selfie" ||
		s.Status != StepUpStatusPending || s.Level != "" || s.Amount != "900.00" || s.Currency != "USD" ||
		s.VerificationURL != testIssuer+"/verify/?verification=proof" || s.ExpiresIn != 1800 ||
		!s.CreatedAt.Equal(testNow) || !s.CompletedAt.IsZero() || !s.ExpiresAt.Equal(testNow.Add(30*time.Minute)) {
		t.Fatalf("step-up = %+v", s)
	}
	req := stub.last("/step-up/")
	if req.method != http.MethodPost || req.header.Get("Authorization") != "Bearer "+testAccessToken ||
		req.header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %+v", req)
	}
	if req.body != `{"action":"payout","amount":"900","currency":"USD","return_to":"https://app.example/payout"}` {
		t.Fatalf("body = %s", req.body)
	}

	// No rule applies: 200 {"required": false}. Empty optional fields are omitted.
	s, err = c.CreateStepUp(ctx, testAccessToken, StepUpParams{Action: StepUpActionBank})
	if err != nil {
		t.Fatal(err)
	}
	if s.Required || s.ID != "" || string(s.Raw) != "{\"required\":false}\n" {
		t.Fatalf("step-up = %+v (%s)", s, s.Raw)
	}
	if body := stub.last("/step-up/").body; body != `{"action":"bank"}` {
		t.Fatalf("body = %s", body)
	}

	_, err = c.CreateStepUp(ctx, testAccessToken, StepUpParams{})
	wantConfigError(t, err, "action is required")
	_, err = c.CreateStepUp(ctx, "", StepUpParams{Action: StepUpActionContact})
	wantConfigError(t, err, "access token")

	stub.handle("/step-up/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"return_to": []string{"This field is required."}})
	})
	_, err = c.CreateStepUp(ctx, testAccessToken, StepUpParams{Action: StepUpActionContact})
	var bad *didit.BadRequestError
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "return_to") {
		t.Fatalf("error = %v (%T)", err, err)
	}
	stub.handle("/step-up/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "down"})
	})
	before := stub.count("/step-up/")
	_, err = c.CreateStepUp(ctx, testAccessToken, StepUpParams{Action: StepUpActionContact})
	if !errors.As(err, new(*didit.InternalServerError)) || stub.count("/step-up/") != before+1 {
		t.Fatalf("error = %v; POST step-up/ must not be retried", err)
	}
}

func TestRetrieveStepUp(t *testing.T) {
	stub := newStub(t)
	id := "4c1d2e3f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	stub.handle("/step-up/"+id+"/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, with(stepUpRecord(), map[string]any{
			"status": "approved", "level": "full_id", "completed_at": "2026-10-17T12:05:00Z",
		}))
	})
	c := newTestClient(t, stub, newClock())
	ctx := context.Background()

	s, err := c.RetrieveStepUp(ctx, testAccessToken, id)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Required || s.Status != StepUpStatusApproved || s.Level != "full_id" || !s.CompletedAt.Equal(testNow.Add(5*time.Minute)) {
		t.Fatalf("step-up = %+v", s)
	}
	req := stub.last("/step-up/" + id + "/")
	if req.method != http.MethodGet || req.header.Get("Authorization") != "Bearer "+testAccessToken {
		t.Fatalf("request = %+v", req)
	}

	// Unknown statuses never fail.
	stub.handle("/step-up/"+id+"/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, with(stepUpRecord(), map[string]any{"status": "escalated"}))
	})
	if s, err = c.RetrieveStepUp(ctx, testAccessToken, id); err != nil || s.Status != "escalated" {
		t.Fatalf("step-up = %+v, %v", s, err)
	}

	// Another session's step-up, and ids are path-escaped.
	_, err = c.RetrieveStepUp(ctx, testAccessToken, "../session")
	var notFound *didit.NotFoundError
	if !errors.As(err, &notFound) || notFound.URL != testIssuer+"/step-up/..%2Fsession/" {
		t.Fatalf("error = %v (%T)", err, err)
	}
	_, err = c.RetrieveStepUp(ctx, testAccessToken, "")
	wantConfigError(t, err, "id is required")
	_, err = c.RetrieveStepUp(ctx, "", id)
	wantConfigError(t, err, "access token")

	stub.handle("/step-up/"+id+"/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("[]")) })
	_, err = c.RetrieveStepUp(ctx, testAccessToken, id)
	if !errors.As(err, new(*didit.APIConnectionError)) {
		t.Fatalf("error = %v (%T)", err, err)
	}
}

func TestRedirects(t *testing.T) {
	ctx := context.Background()

	t.Run("another origin is refused", func(t *testing.T) {
		for _, location := range []string{
			"https://evil.example/session/",
			"http://acme.auth.didit.me/session/",
			"https://acme.auth.didit.me:8443/session/",
		} {
			stub := newStub(t)
			stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, location, http.StatusTemporaryRedirect)
			})
			c := newTestClient(t, stub, newClock())
			_, err := c.GetSession(ctx, testAccessToken)
			var status *didit.APIStatusError
			if !errors.As(err, &status) || status.StatusCode != http.StatusTemporaryRedirect || status.Header.Get("Location") != location {
				t.Fatalf("%s: error = %v (%T)", location, err, err)
			}
			for _, r := range stub.all() {
				if r.host != "acme.auth.didit.me" {
					t.Fatalf("a request went to %s", r.host)
				}
			}
		}
	})

	t.Run("same origin is followed", func(t *testing.T) {
		stub := newStub(t)
		stub.handle("/session", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://ACME.auth.didit.me:443/session/", http.StatusPermanentRedirect)
		})
		stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, sessionBody()) })
		redirects := 0
		own := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { redirects++; return nil }}
		c, err := New(testIssuer, WithHTTPClient(own))
		if err != nil {
			t.Fatal(err)
		}
		c.http.Transport = stub
		r := request{method: http.MethodGet, url: testIssuer + "/session", bearer: testAccessToken}
		if _, err := c.send(ctx, r); err != nil {
			t.Fatal(err)
		}
		if redirects != 1 {
			t.Fatalf("the caller's CheckRedirect ran %d times, want 1", redirects)
		}
		if got := stub.last("/session/").header.Get("Authorization"); got != "Bearer "+testAccessToken {
			t.Fatalf("Authorization after a same-origin redirect = %q", got)
		}
	})

	t.Run("redirect loops stop", func(t *testing.T) {
		stub := newStub(t)
		stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/session/", http.StatusFound)
		})
		c := newTestClient(t, stub, newClock())
		_, err := c.GetSession(ctx, testAccessToken)
		var status *didit.APIStatusError
		if !errors.As(err, &status) || status.StatusCode != http.StatusFound || stub.count("/session/") != 11 {
			t.Fatalf("error = %v, %d requests", err, stub.count("/session/"))
		}
	})
}

func TestConnectionErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("timeout", func(t *testing.T) {
		stub := newStub(t)
		stub.setTransportError(func(*http.Request) error { return timeoutError{} })
		c := newTestClient(t, stub, newClock())
		_, err := c.GetSession(ctx, testAccessToken)
		var timeout *didit.APITimeoutError
		var conn *didit.APIConnectionError
		if !errors.As(err, &timeout) || !errors.As(err, &conn) || conn.URL != testIssuer+"/session/" {
			t.Fatalf("error = %v (%T)", err, err)
		}
		if n := stub.count("/session/"); n != 2 {
			t.Fatalf("session requests = %d, want 2 (GET retried once)", n)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		stub := newStub(t)
		c := newTestClient(t, stub, newClock())
		deadline, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer cancel()
		_, err := c.GetSession(deadline, testAccessToken)
		if !errors.As(err, new(*didit.APITimeoutError)) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v (%T)", err, err)
		}
	})

	t.Run("cancelled context is not retried", func(t *testing.T) {
		stub := newStub(t)
		cctx, cancel := context.WithCancel(ctx)
		stub.setTransportError(func(*http.Request) error { cancel(); return context.Canceled })
		c := newTestClient(t, stub, newClock())
		_, err := c.GetSession(cctx, testAccessToken)
		var conn *didit.APIConnectionError
		if !errors.As(err, &conn) || errors.As(err, new(*didit.APITimeoutError)) || stub.count("/session/") != 1 {
			t.Fatalf("error = %v (%T), %d requests", err, err, stub.count("/session/"))
		}
	})

	t.Run("cancelled during backoff", func(t *testing.T) {
		stub := newStub(t)
		cctx, cancel := context.WithCancel(ctx)
		stub.setTransportError(func(*http.Request) error { return errors.New("connection refused") })
		c := newTestClient(t, stub, newClock())
		c.retryDelay = time.Hour
		time.AfterFunc(10*time.Millisecond, cancel)
		_, err := c.GetSession(cctx, testAccessToken)
		if !errors.Is(err, context.Canceled) || stub.count("/session/") != 1 {
			t.Fatalf("error = %v, %d requests", err, stub.count("/session/"))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		c := newTestClient(t, newStub(t), newClock())
		//lint:ignore SA1012 testing the error path
		_, err := c.sendOnce(nil, request{method: http.MethodGet, url: testIssuer}) //nolint:staticcheck // nil context on purpose
		if !errors.As(err, new(*didit.APIConnectionError)) {
			t.Fatalf("error = %v (%T)", err, err)
		}
	})

	t.Run("body read error", func(t *testing.T) {
		c := newTestClient(t, newStub(t), newClock())
		c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: errorBody{}, Request: r}, nil
		})
		_, err := c.GetSession(ctx, testAccessToken)
		if !errors.As(err, new(*didit.APIConnectionError)) {
			t.Fatalf("error = %v (%T)", err, err)
		}
	})
}

func TestResponseSizeLimit(t *testing.T) {
	big := strings.Repeat("x", maxResponseBytes+10)
	stub := newStub(t)
	stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(big)) })
	c := newTestClient(t, stub, newClock())
	_, err := c.GetSession(context.Background(), testAccessToken)
	if !errors.As(err, new(*didit.APIConnectionError)) || !strings.Contains(err.Error(), "larger than 1 MiB") {
		t.Fatalf("error = %v (%T)", err, err)
	}

	stub.handle("/session/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(big))
	})
	_, err = c.GetSession(context.Background(), testAccessToken)
	var bad *didit.BadRequestError
	if !errors.As(err, &bad) || len(bad.RawBody) != maxResponseBytes {
		t.Fatalf("error = %T, body %d bytes", err, len(bad.RawBody))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errorBody struct{}

func (errorBody) Read([]byte) (int, error) { return 0, errors.New("connection dropped") }
func (errorBody) Close() error               { return nil }
