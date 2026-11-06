// Package authentication is the server-side helper for Didit Authentication,
// the OpenID Connect issuer Didit runs for each application's end users
// (docs/DESIGN.md section 10).
//
// It never takes the Didit API key. A [Client] talks to one application's
// issuer, such as https://<slug>.auth.didit.me or the application's own
// domain, and to nothing else:
//
//   - [Client.VerifyAccessToken] and [Client.VerifyIDToken] verify ES256 JWTs
//     offline against the issuer's JSON Web Key Set. The discovery document
//     and the key set are fetched lazily and cached (see below).
//   - [CreatePKCE], [CreateState] and [CreateNonce] prepare a sign-in;
//     [Client.ExchangeCode] and [Client.Refresh] call the issuer's token
//     endpoint.
//   - [Client.GetSession] is the instant revocation check and
//     [Client.RevokeSession] signs the person out.
//   - [Client.CreateStepUp] and [Client.RetrieveStepUp] ask for a verification
//     before a sensitive action.
//
// Create one Client per issuer and reuse it: it is safe for concurrent use and
// it holds the key cache.
//
//	auth, err := authentication.NewFromPublishableKey(os.Getenv("DIDIT_PUBLISHABLE_KEY"))
//	if err != nil { ... }
//	claims, err := auth.VerifyAccessToken(ctx, bearerToken, authentication.WithAudience(applicationID))
//	var tokenErr *didit.TokenVerificationError
//	if errors.As(err, &tokenErr) {
//		// 401: the token is not valid (tokenErr.Reason says why)
//	} else if err != nil {
//		// 503: the issuer's keys could not be fetched
//	}
//
// # Issuers
//
// The issuer is the application's canonical origin, without a trailing slash.
// It must use https; plain http is accepted only for localhost, *.localhost,
// 127.0.0.1 and ::1, which is what Didit issues for local Sandbox development.
// The discovery document at <issuer>/.well-known/openid-configuration must name
// exactly the same issuer, otherwise every call fails with a
// *didit.ConfigurationError. Its token_endpoint and jwks_uri must be on the
// issuer's origin.
//
// # Caching
//
// The discovery document and the key set are cached for the Cache-Control
// max-age of their responses (Didit sends five minutes), at least one minute
// and at most a day, and five minutes when the header is missing. A token whose
// kid is not in the cached key set triggers a refetch, at most once a minute,
// so key rotation is picked up without letting forged kids hammer the issuer.
//
// # Errors
//
// Every error is a didit.Error:
//
//   - *didit.TokenVerificationError: the token is not valid; Reason is
//     machine-readable. Never retry with the same token.
//   - *didit.APIStatusError and its status-specific types such as
//     *didit.BadRequestError (the token endpoint answers 400
//     {"error": "invalid_grant"} for a used, expired or unknown code or refresh
//     token) and *didit.AuthenticationError (401 for an access token that is
//     invalid, refreshed or revoked).
//   - *didit.APIConnectionError and *didit.APITimeoutError: no response.
//   - *didit.UnexpectedResponseError: a 2xx response the package cannot use,
//     such as a body that is not valid JSON.
//   - *didit.ConfigurationError: an invalid issuer, publishable key, option or
//     argument, or a discovery document that does not belong to the issuer.
//
// # Retries and redirects
//
// GET requests (discovery, key set, session and step-up status) are retried
// once after a short randomized delay when the connection fails or the issuer
// answers 5xx. POST requests (token, step-up) and DELETE session/ are never
// retried: codes and refresh tokens are single-use, and a revoked session no
// longer accepts its own token. Redirects to another origin are not followed,
// so tokens never leave the issuer's origin; such a response is returned as a
// *didit.APIStatusError with the 3xx status.
//
// The package never logs, and its error messages never contain tokens, codes
// or secrets.
package authentication
