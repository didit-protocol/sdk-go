package authentication

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	didit "github.com/didit-protocol/sdk-go"
)

// randReader is the source of randomness; tests replace it.
var randReader io.Reader = rand.Reader

// PKCE is a Proof Key for Code Exchange pair (RFC 7636). Send Challenge and
// Method to the authorization endpoint, keep Verifier on the server (for
// example in the sign-in session), and pass it to [Client.ExchangeCode].
type PKCE struct {
	// Verifier is 43 characters of unpadded base64url from 32 random bytes.
	Verifier string
	// Challenge is the unpadded base64url SHA-256 of Verifier.
	Challenge string
	// Method is always "S256", the only method Didit accepts.
	Method string
}

// CreatePKCE returns a new S256 PKCE pair from crypto/rand.
func CreatePKCE() (PKCE, error) {
	verifier, err := randomString()
	if err != nil {
		return PKCE{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	return PKCE{Verifier: verifier, Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), Method: "S256"}, nil
}

// CreateState returns a new random OAuth state value (43 characters of
// unpadded base64url). Store it with the sign-in and compare it with the state
// the redirect returns before exchanging the code.
func CreateState() (string, error) { return randomString() }

// CreateNonce returns a new random OpenID Connect nonce (43 characters of
// unpadded base64url). Send it to the authorization endpoint and pass it to
// [Client.VerifyIDToken] for the ID token of the code exchange.
func CreateNonce() (string, error) { return randomString() }

// randomString returns 32 random bytes as unpadded base64url.
func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(randReader, b); err != nil {
		return "", &didit.ConfigurationError{Message: fmt.Sprintf("reading random bytes: %v", err)}
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
