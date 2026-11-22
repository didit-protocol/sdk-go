// Package webhook verifies Didit webhook deliveries and parses them into typed
// events. It has no framework dependency: give it the raw body and the request
// headers.
//
//	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
//	if err != nil { ... }
//	ev, err := webhook.Verify(body, r.Header, secret)
//	if err != nil {
//		http.Error(w, "invalid webhook", http.StatusBadRequest)
//		return
//	}
//	switch e := ev.(type) {
//	case *webhook.StatusUpdatedEvent:
//		// e.SessionID, e.Status, e.Decision
//	case *webhook.TransactionStatusUpdatedEvent:
//		// e.TransactionID, e.Status, e.Score
//	}
//
// # Signatures
//
// Didit signs every delivery with the destination's secret (HMAC-SHA256, hex):
// X-Signature covers the exact body bytes and X-Signature-V2 the canonical
// JSON of the body (see Canonicalize), which survives middleware that
// re-encodes the body. Verify accepts either. During a secret rotation the
// same signatures made with the previous secret arrive as X-Signature-Previous
// and X-Signature-V2-Previous, so a receiver still on the previous secret keeps
// working; a receiver that already switched can pass the old secret with
// WithSecrets.
//
// X-Signature-Simple is deprecated: it signs only the timestamp, session_id,
// status and webhook_type. It counts only with WithAllowSimple, and the event
// then reports Authenticated() == false; re-fetch the record from the API
// rather than trusting the body.
//
// The body's timestamp must be within the tolerance of the current time (300
// seconds by default, see WithTolerance), which limits replays. Deliveries
// may still arrive more than once; deduplicate on the envelope's EventID.
//
// Verification failures are *didit.WebhookVerificationError values whose
// Reason is machine-readable; an empty secret is a *didit.ConfigurationError.
// Respond with a 4xx status to a delivery that fails verification and with a
// 2xx status as soon as a verified event is stored.
//
// # Parsed bodies
//
// Use VerifyParsed when a framework only exposes the decoded body. It can
// check X-Signature-V2 but not X-Signature, and the body must be decoded with
// json.Decoder.UseNumber for integers above 2^53 to keep their digits.
//
// # Events
//
// Verify returns one event type per webhook type, and an *UnknownEvent with
// the whole payload for types this version does not know. Fields are decoded
// leniently: an unexpected value never fails a delivery whose signature
// matched, it leaves the field at its zero value, and Raw always returns the
// verified body for fields the types do not model.
package webhook
