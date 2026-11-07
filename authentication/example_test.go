package authentication_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/authentication"
)

func ExampleNew() {
	auth, err := authentication.New("https://acme.auth.didit.me/")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(auth.Issuer())
	// Output: https://acme.auth.didit.me
}

func ExampleNew_invalidIssuer() {
	_, err := authentication.New("http://acme.auth.didit.me")
	var cfg *didit.ConfigurationError
	fmt.Println(errors.As(err, &cfg))
	fmt.Println(err)
	// Output:
	// true
	// didit: issuer must use https (http only for localhost)
}

func ExampleParsePublishableKey() {
	// A publishable key is "pk_live_" or "pk_test_" plus the base64url issuer.
	key := "pk_test_" + base64.RawURLEncoding.EncodeToString([]byte("https://acme.sandbox.auth.didit.me"))
	issuer, live, err := authentication.ParsePublishableKey(key)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(issuer, live)
	// Output: https://acme.sandbox.auth.didit.me false
}

func ExampleCreatePKCE() {
	pkce, err := authentication.CreatePKCE()
	if err != nil {
		log.Fatal(err)
	}
	sum := sha256.Sum256([]byte(pkce.Verifier))
	fmt.Println(len(pkce.Verifier), pkce.Method, pkce.Challenge == base64.RawURLEncoding.EncodeToString(sum[:]))
	// Output: 43 S256 true
}

// Verify the bearer token of every API request in a middleware.
func ExampleClient_VerifyAccessToken() {
	auth, err := authentication.NewFromPublishableKey(os.Getenv("DIDIT_PUBLISHABLE_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	applicationID := os.Getenv("DIDIT_APPLICATION_ID")

	requireUser := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			claims, err := auth.VerifyAccessToken(r.Context(), token, authentication.WithAudience(applicationID))
			var tokenErr *didit.TokenVerificationError
			switch {
			case errors.As(err, &tokenErr):
				http.Error(w, "invalid token: "+string(tokenErr.Reason), http.StatusUnauthorized)
				return
			case err != nil: // the issuer's keys could not be fetched
				http.Error(w, "try again later", http.StatusServiceUnavailable)
				return
			case claims.Act != nil: // this endpoint is for people, not agents
				http.Error(w, "agents not allowed", http.StatusForbidden)
				return
			}
			log.Printf("request by %s (vendor_data %s)", claims.Subject, claims.VendorData)
			next.ServeHTTP(w, r)
		})
	}
	http.Handle("/api/", requireUser(http.NotFoundHandler()))
}

// The sign-in flow of a server-rendered application: redirect to the hosted
// sign-in page, then exchange the code on the way back.
func Example_signIn() {
	auth, err := authentication.New("https://acme.auth.didit.me")
	if err != nil {
		log.Fatal(err)
	}
	const (
		applicationID = "your-application-id"
		redirectURI   = "https://app.example.com/callback"
	)

	// 1. Before redirecting: keep the verifier, state and nonce server-side.
	pkce, err := authentication.CreatePKCE()
	if err != nil {
		log.Fatal(err)
	}
	state, err := authentication.CreateState()
	if err != nil {
		log.Fatal(err)
	}
	nonce, err := authentication.CreateNonce()
	if err != nil {
		log.Fatal(err)
	}
	signIn := auth.Issuer() + "/sign-in?" + url.Values{
		"client_id":             {applicationID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid email"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {pkce.Method},
	}.Encode()
	fmt.Println("redirect the browser to", signIn)

	// 2. In the redirect handler, after checking that the returned state
	// equals the stored one:
	ctx := context.Background()
	code := "code-from-the-redirect-query"
	tokens, err := auth.ExchangeCode(ctx, code, pkce.Verifier, redirectURI)
	if err != nil {
		log.Fatal(err)
	}
	person, err := auth.VerifyIDToken(ctx, tokens.IDToken, nonce, applicationID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("signed in:", person.Subject, person.Email)
}

// Refresh tokens are single-use: serialise refreshes of one session, or a
// concurrent second refresh revokes it.
func ExampleClient_Refresh() {
	auth, err := authentication.New("https://acme.auth.didit.me")
	if err != nil {
		log.Fatal(err)
	}
	type session struct {
		mu           sync.Mutex
		refreshToken string
		accessToken  string
	}
	refresh := func(ctx context.Context, s *session) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		tokens, err := auth.Refresh(ctx, s.refreshToken)
		var bad *didit.BadRequestError
		if errors.As(err, &bad) {
			return errors.New("the session ended; sign in again")
		}
		if err != nil {
			return err
		}
		s.refreshToken, s.accessToken = tokens.RefreshToken, tokens.AccessToken
		return nil
	}
	_ = refresh
}

// Ask for a verification before a payout.
func ExampleClient_CreateStepUp() {
	auth, err := authentication.New("https://acme.auth.didit.me")
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	accessToken := "the-person's-access-token"

	stepUp, err := auth.CreateStepUp(ctx, accessToken, authentication.StepUpParams{
		Action:   authentication.StepUpActionPayout,
		Amount:   "900.00",
		Currency: "USD",
		ReturnTo: "https://app.example.com/payouts/confirm",
	})
	if err != nil {
		log.Fatal(err)
	}
	if !stepUp.Required {
		fmt.Println("no verification needed: pay out")
		return
	}
	fmt.Println("send the person to", stepUp.VerificationURL)

	// Later, when the person is back at ReturnTo:
	stepUp, err = auth.RetrieveStepUp(ctx, accessToken, stepUp.ID)
	if err != nil {
		log.Fatal(err)
	}
	if stepUp.Status == authentication.StepUpStatusApproved {
		fmt.Println("verified: pay out")
	}
}
