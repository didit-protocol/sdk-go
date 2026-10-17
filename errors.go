package didit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Error is implemented by every error this module returns, so
//
//	var e didit.Error
//	if errors.As(err, &e) { ... }
//
// tells SDK errors apart from anything else. Use errors.As with the concrete
// types below to read their fields.
type Error interface {
	error
	diditError()
}

// ConfigurationError reports a missing or invalid setting, such as a missing
// API key or a missing organization or application id. It is returned before
// any request is sent.
type ConfigurationError struct {
	Message string
}

func (e *ConfigurationError) Error() string { return "didit: " + e.Message }
func (*ConfigurationError) diditError()     {}

// APIConnectionError reports a request that got no HTTP response: DNS and
// connection failures, TLS errors, a connection dropped while the response was
// read, or a cancelled context. Cause holds the underlying error.
type APIConnectionError struct {
	Method string
	// URL is the request URL without query string or credentials.
	URL   string
	Cause error
}

func (e *APIConnectionError) Error() string {
	msg := "didit: connection error"
	if e.Method != "" || e.URL != "" {
		msg += " on " + strings.TrimSpace(e.Method+" "+e.URL)
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *APIConnectionError) Unwrap() error { return e.Cause }
func (*APIConnectionError) diditError()     {}

// APITimeoutError reports a request that timed out before a complete response
// arrived. It unwraps to the *APIConnectionError that carries the details.
type APITimeoutError struct {
	*APIConnectionError
}

func (e *APITimeoutError) Error() string {
	msg := "didit: request timed out"
	if e.APIConnectionError != nil && (e.Method != "" || e.URL != "") {
		msg += " on " + strings.TrimSpace(e.Method+" "+e.URL)
	}
	return msg
}

func (e *APITimeoutError) Unwrap() error { return e.APIConnectionError }

// APIStatusError reports a response with a status code outside 2xx.
//
// Use errors.As with *APIStatusError for every status, or with one of the
// status-specific types (*NotFoundError, *RateLimitError, ...) which all
// unwrap to *APIStatusError.
type APIStatusError struct {
	StatusCode int
	// Body is the response body parsed as JSON when possible (map[string]any,
	// []any, ...), otherwise the body as a string, or nil when empty.
	Body any
	// RawBody is the response body as received.
	RawBody []byte
	Header  http.Header
	// RequestID is the request id of the response (X-Request-Id, Request-Id,
	// X-Amzn-Requestid or X-Correlation-Id), if any.
	RequestID string
	Method    string
	// URL is the request URL without query string or credentials.
	URL string
	// Response is the HTTP response. Its body has already been read; RawBody
	// holds it.
	Response *http.Response
}

func (e *APIStatusError) Error() string {
	var b strings.Builder
	b.WriteString("didit: ")
	if e.Method != "" || e.URL != "" {
		b.WriteString(strings.TrimSpace(e.Method + " " + e.URL))
		b.WriteString(": ")
	}
	fmt.Fprintf(&b, "%d %s", e.StatusCode, http.StatusText(e.StatusCode))
	if msg := errorMessage(e.Body); msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	if e.RequestID != "" {
		b.WriteString(" (request id ")
		b.WriteString(e.RequestID)
		b.WriteString(")")
	}
	return b.String()
}

func (*APIStatusError) diditError() {}

// errorMessage extracts a short, human readable message from an error body.
func errorMessage(body any) string {
	const max = 300
	var msg string
	switch v := body.(type) {
	case nil:
		return ""
	case string:
		msg = v
	case map[string]any:
		for _, key := range []string{"detail", "error", "message"} {
			if s, ok := v[key].(string); ok && s != "" {
				msg = s
				break
			}
		}
		if msg == "" {
			raw, err := json.Marshal(v)
			if err != nil {
				return ""
			}
			msg = string(raw)
		}
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		msg = string(raw)
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > max {
		msg = msg[:max] + "..."
	}
	return msg
}

// BadRequestError is an *APIStatusError with status 400.
type BadRequestError struct{ *APIStatusError }

// AuthenticationError is an *APIStatusError with status 401: the API key is
// missing, wrong, revoked or expired.
type AuthenticationError struct{ *APIStatusError }

// PermissionDeniedError is an *APIStatusError with status 403.
type PermissionDeniedError struct{ *APIStatusError }

// NotFoundError is an *APIStatusError with status 404.
type NotFoundError struct{ *APIStatusError }

// ConflictError is an *APIStatusError with status 409.
type ConflictError struct{ *APIStatusError }

// UnprocessableEntityError is an *APIStatusError with status 422.
type UnprocessableEntityError struct{ *APIStatusError }

// RateLimitError is an *APIStatusError with status 429.
type RateLimitError struct{ *APIStatusError }

// InternalServerError is an *APIStatusError with a 5xx status.
type InternalServerError struct{ *APIStatusError }

func (e *BadRequestError) Unwrap() error          { return e.APIStatusError }
func (e *AuthenticationError) Unwrap() error      { return e.APIStatusError }
func (e *PermissionDeniedError) Unwrap() error    { return e.APIStatusError }
func (e *NotFoundError) Unwrap() error            { return e.APIStatusError }
func (e *ConflictError) Unwrap() error            { return e.APIStatusError }
func (e *UnprocessableEntityError) Unwrap() error { return e.APIStatusError }
func (e *RateLimitError) Unwrap() error           { return e.APIStatusError }
func (e *InternalServerError) Unwrap() error      { return e.APIStatusError }

// NewAPIStatusError returns the most specific error type for e.StatusCode:
// *BadRequestError for 400, *NotFoundError for 404, and so on, or e itself
// when no specific type exists. The other packages of this module use it so
// that every HTTP error has the same shape.
func NewAPIStatusError(e *APIStatusError) error {
	switch {
	case e.StatusCode == http.StatusBadRequest:
		return &BadRequestError{e}
	case e.StatusCode == http.StatusUnauthorized:
		return &AuthenticationError{e}
	case e.StatusCode == http.StatusForbidden:
		return &PermissionDeniedError{e}
	case e.StatusCode == http.StatusNotFound:
		return &NotFoundError{e}
	case e.StatusCode == http.StatusConflict:
		return &ConflictError{e}
	case e.StatusCode == http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{e}
	case e.StatusCode == http.StatusTooManyRequests:
		return &RateLimitError{e}
	case e.StatusCode >= 500 && e.StatusCode <= 599:
		return &InternalServerError{e}
	default:
		return e
	}
}

// WebhookVerificationReason is the machine-readable reason of a
// *WebhookVerificationError.
type WebhookVerificationReason string

// Webhook verification failure reasons (docs/DESIGN.md section 9.3).
const (
	// WebhookReasonMissingSignature: no signature header at all.
	WebhookReasonMissingSignature WebhookVerificationReason = "missing_signature"
	// WebhookReasonInvalidSignature: no signature matched any secret. Also
	// returned when only X-Signature-Simple is present and AllowSimple is off.
	WebhookReasonInvalidSignature WebhookVerificationReason = "invalid_signature"
	// WebhookReasonTimestampMismatch: X-Timestamp differs from the body's
	// timestamp.
	WebhookReasonTimestampMismatch WebhookVerificationReason = "timestamp_mismatch"
	// WebhookReasonTimestampOutOfTolerance: the timestamp is further from now
	// than the tolerance.
	WebhookReasonTimestampOutOfTolerance WebhookVerificationReason = "timestamp_out_of_tolerance"
	// WebhookReasonMissingTimestamp: the body has no integer timestamp.
	WebhookReasonMissingTimestamp WebhookVerificationReason = "missing_timestamp"
	// WebhookReasonInvalidPayload: the body is not a UTF-8 JSON object.
	WebhookReasonInvalidPayload WebhookVerificationReason = "invalid_payload"
)

// WebhookVerificationError reports a webhook delivery that failed
// verification. Reason is machine-readable.
type WebhookVerificationError struct {
	Reason  WebhookVerificationReason
	Message string
}

func (e *WebhookVerificationError) Error() string {
	if e.Message == "" {
		return "didit: webhook verification failed: " + string(e.Reason)
	}
	return "didit: webhook verification failed: " + string(e.Reason) + ": " + e.Message
}

func (*WebhookVerificationError) diditError() {}

// TokenVerificationReason is the machine-readable reason of a
// *TokenVerificationError.
type TokenVerificationReason string

// Authentication token verification failure reasons.
const (
	// TokenReasonMalformed: not a compact JWS with a JSON header and claims.
	TokenReasonMalformed TokenVerificationReason = "malformed"
	// TokenReasonUnsupportedAlgorithm: alg is not ES256.
	TokenReasonUnsupportedAlgorithm TokenVerificationReason = "unsupported_algorithm"
	// TokenReasonInvalidType: the typ header is not the expected one.
	TokenReasonInvalidType TokenVerificationReason = "invalid_type"
	// TokenReasonUnknownKey: no JSON Web Key matches the kid, even after a
	// refetch.
	TokenReasonUnknownKey TokenVerificationReason = "unknown_key"
	// TokenReasonInvalidSignature: the signature does not verify.
	TokenReasonInvalidSignature TokenVerificationReason = "invalid_signature"
	// TokenReasonInvalidIssuer: iss differs from the issuer.
	TokenReasonInvalidIssuer TokenVerificationReason = "invalid_issuer"
	// TokenReasonInvalidAudience: aud does not contain the expected audience.
	TokenReasonInvalidAudience TokenVerificationReason = "invalid_audience"
	// TokenReasonExpired: exp is in the past (beyond the leeway).
	TokenReasonExpired TokenVerificationReason = "expired"
	// TokenReasonNotYetValid: nbf or iat is in the future (beyond the leeway).
	TokenReasonNotYetValid TokenVerificationReason = "not_yet_valid"
	// TokenReasonInvalidNonce: the ID token nonce differs from the expected one.
	TokenReasonInvalidNonce TokenVerificationReason = "invalid_nonce"
	// TokenReasonMissingClaim: a required claim is missing or has the wrong type.
	TokenReasonMissingClaim TokenVerificationReason = "missing_claim"
)

// TokenVerificationError reports an Authentication access or ID token that
// failed verification. Reason is machine-readable.
type TokenVerificationError struct {
	Reason  TokenVerificationReason
	Message string
}

func (e *TokenVerificationError) Error() string {
	if e.Message == "" {
		return "didit: token verification failed: " + string(e.Reason)
	}
	return "didit: token verification failed: " + string(e.Reason) + ": " + e.Message
}

func (*TokenVerificationError) diditError() {}
