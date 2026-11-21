// Command authentication protects a net/http API with Didit Authentication:
// a middleware verifies the access token from the Authorization header and
// answers 401 when it is missing or invalid. A sensitive route also calls
// GetSession, which fails as soon as the session is revoked, where offline
// token verification only notices when the token expires (within five
// minutes).
//
//	DIDIT_PUBLISHABLE_KEY=pk_test_... go run ./authentication
//	curl -H "Authorization: Bearer $ACCESS_TOKEN" localhost:8080/api/me
//
// Environment:
//
//	DIDIT_PUBLISHABLE_KEY  your application's publishable key, or
//	DIDIT_AUTH_ISSUER      your application's issuer, such as https://acme.auth.didit.me
//	DIDIT_APPLICATION_ID   when set, tokens must be issued for this application (aud)
//	PORT                   port to listen on (default 8080)
//
// The Didit API key is not needed: tokens are checked against the issuer's
// public keys.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/authentication"
)

type ctxKey struct{}

type server struct {
	auth          *authentication.Client
	applicationID string
}

func main() {
	var (
		auth *authentication.Client
		err  error
	)
	if key := os.Getenv("DIDIT_PUBLISHABLE_KEY"); key != "" {
		auth, err = authentication.NewFromPublishableKey(key)
	} else if issuer := os.Getenv("DIDIT_AUTH_ISSUER"); issuer != "" {
		auth, err = authentication.New(issuer)
	} else {
		log.Fatal("set DIDIT_PUBLISHABLE_KEY or DIDIT_AUTH_ISSUER")
	}
	if err != nil {
		log.Fatalf("authentication: %v", err)
	}
	s := &server{auth: auth, applicationID: os.Getenv("DIDIT_APPLICATION_ID")}

	mux := http.NewServeMux()
	mux.Handle("GET /api/me", s.requireUser(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/payouts", s.requireUser(http.HandlerFunc(s.payout)))

	addr := ":" + envOr("PORT", "8080")
	log.Printf("issuer %s, listening on %s", auth.Issuer(), addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

// requireUser verifies the bearer access token and stores its claims in the
// request context.
func (s *server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		var opts []authentication.VerifyOption
		if s.applicationID != "" {
			opts = append(opts, authentication.WithAudience(s.applicationID))
		}
		claims, err := s.auth.VerifyAccessToken(r.Context(), token, opts...)
		var tokenErr *didit.TokenVerificationError
		switch {
		case errors.As(err, &tokenErr):
			// Expired, wrong issuer or audience, bad signature, ...
			http.Error(w, "invalid token: "+string(tokenErr.Reason), http.StatusUnauthorized)
			return
		case err != nil:
			// The issuer's keys could not be fetched: not the caller's fault.
			log.Printf("verify access token: %v", err)
			http.Error(w, "try again later", http.StatusServiceUnavailable)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(ctxKey{}).(*authentication.AccessTokenClaims)
	writeJSON(w, map[string]any{
		"sub":         claims.Subject,
		"vendor_data": claims.VendorData,
		"amr":         claims.AMR,
		"verified":    claims.Verified.Level,
		"agent":       claims.Act != nil,
	})
}

// payout is a sensitive action: besides the token, check that the session is
// still live right now.
func (s *server) payout(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(ctxKey{}).(*authentication.AccessTokenClaims)
	if claims.Act != nil {
		http.Error(w, "agents cannot request payouts", http.StatusForbidden)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	session, err := s.auth.GetSession(r.Context(), token)
	var authErr *didit.AuthenticationError
	switch {
	case errors.As(err, &authErr):
		// Signed out, revoked, expired, idle, or the token was refreshed.
		http.Error(w, "session is no longer active", http.StatusUnauthorized)
		return
	case err != nil:
		log.Printf("get session: %v", err)
		http.Error(w, "try again later", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{
		"status":       "accepted",
		"session_id":   session.SessionID,
		"mfa_verified": session.MFAVerified,
		"expires_at":   session.ExpiresAt,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
