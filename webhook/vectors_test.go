package webhook_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// expectedType maps a webhook_type to the concrete event type Verify returns.
var expectedType = map[string]reflect.Type{
	"status.updated":             reflect.TypeOf(&webhook.StatusUpdatedEvent{}),
	"data.updated":               reflect.TypeOf(&webhook.DataUpdatedEvent{}),
	"user.status.updated":        reflect.TypeOf(&webhook.UserStatusUpdatedEvent{}),
	"transaction.status.updated": reflect.TypeOf(&webhook.TransactionStatusUpdatedEvent{}),
	"travel_rule.status.updated": reflect.TypeOf(&webhook.TravelRuleStatusUpdatedEvent{}),
}

func TestVectorsCanonical(t *testing.T) {
	for _, v := range loadVectors(t).Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := webhook.Canonicalize([]byte(v.Body))
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}
			if v.Name == "tampered_body" {
				// canonical_v2 of this vector was computed before the body was
				// altered (status Approved became Declined), so it must differ.
				if string(got) == v.CanonicalV2 {
					t.Fatal("canonical JSON of the tampered body equals the original canonical_v2")
				}
				return
			}
			if string(got) != v.CanonicalV2 {
				t.Fatalf("canonical JSON differs\n got: %s\nwant: %s", got, v.CanonicalV2)
			}
		})
	}
}

func TestVectorsVerify(t *testing.T) {
	for _, v := range loadVectors(t).Vectors {
		t.Run(v.Name, func(t *testing.T) {
			now := clockAt(v.Now)
			ev, err := webhook.Verify([]byte(v.Body), v.header(), v.Secret, now)
			switch v.Expect {
			case "valid":
				if err != nil {
					t.Fatalf("Verify: %v", err)
				}
				checkVectorEvent(t, v, ev, true)
				if !bytes.Equal(ev.Raw(), []byte(v.Body)) {
					t.Fatal("Raw() differs from the body")
				}
			case "valid_only_with_allow_simple":
				wantReason(t, err, didit.WebhookReasonInvalidSignature)
				ev, err := webhook.Verify([]byte(v.Body), v.header(), v.Secret, now, webhook.WithAllowSimple())
				if err != nil {
					t.Fatalf("Verify with WithAllowSimple: %v", err)
				}
				checkVectorEvent(t, v, ev, false)
			default:
				wantReason(t, err, didit.WebhookVerificationReason(v.Expect))
				if ev != nil {
					t.Fatal("got an event together with an error")
				}
			}
		})
	}
}

func TestVectorsVerifyParsed(t *testing.T) {
	for _, v := range loadVectors(t).Vectors {
		t.Run(v.Name, func(t *testing.T) {
			parsed := decodeUseNumber(t, []byte(v.Body))
			now := clockAt(v.Now)
			ev, err := webhook.VerifyParsed(parsed, v.header(), v.Secret, now)
			switch v.Expect {
			case "valid":
				if err != nil {
					t.Fatalf("VerifyParsed: %v", err)
				}
				checkVectorEvent(t, v, ev, true)
				if v.Name != "tampered_body" && string(ev.Raw()) != v.CanonicalV2 {
					t.Fatalf("Raw() = %s, want the canonical JSON", ev.Raw())
				}
			case "valid_only_with_allow_simple":
				wantReason(t, err, didit.WebhookReasonInvalidSignature)
				ev, err := webhook.VerifyParsed(parsed, v.header(), v.Secret, now, webhook.WithAllowSimple())
				if err != nil {
					t.Fatalf("VerifyParsed with WithAllowSimple: %v", err)
				}
				checkVectorEvent(t, v, ev, false)
			default:
				wantReason(t, err, didit.WebhookVerificationReason(v.Expect))
			}
		})
	}
}

func checkVectorEvent(t *testing.T, v vector, ev webhook.Event, authenticated bool) {
	t.Helper()
	var body struct {
		EventID     string `json:"event_id"`
		WebhookType string `json:"webhook_type"`
		Timestamp   int64  `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(v.Body), &body); err != nil {
		t.Fatal(err)
	}
	want, ok := expectedType[body.WebhookType]
	if !ok {
		t.Fatalf("no expected type for %q", body.WebhookType)
	}
	if got := reflect.TypeOf(ev); got != want {
		t.Fatalf("event type %v, want %v", got, want)
	}
	if ev.Authenticated() != authenticated {
		t.Fatalf("Authenticated() = %v, want %v", ev.Authenticated(), authenticated)
	}
	env := ev.Envelope()
	if string(ev.Type()) != body.WebhookType || env.EventID != body.EventID || env.Timestamp != body.Timestamp {
		t.Fatalf("envelope %+v does not match the body", env)
	}
	if env.Environment != webhook.EnvironmentLive {
		t.Fatalf("Environment = %q", env.Environment)
	}
}

func TestVectorsRotationWithSecrets(t *testing.T) {
	for _, v := range loadVectors(t).Vectors {
		if v.Expect != "valid" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			// The receiver's primary secret is unrelated; the delivery's
			// secret is accepted as an additional one.
			ev := mustVerify(t, []byte(v.Body), v.header(), otherTestSecret, clockAt(v.Now), webhook.WithSecrets(v.Secret))
			if !ev.Authenticated() {
				t.Fatal("not authenticated")
			}
			if _, err := webhook.Verify([]byte(v.Body), v.header(), otherTestSecret, clockAt(v.Now)); err == nil {
				t.Fatal("accepted without the right secret")
			}
		})
	}
}

func TestVectorsRotationPreviousHeaders(t *testing.T) {
	for _, v := range loadVectors(t).Vectors {
		if v.PreviousSecret == nil {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			now := clockAt(v.Now)
			// A receiver still holding the previous secret.
			mustVerify(t, []byte(v.Body), v.header(), *v.PreviousSecret, now)

			// Each -Previous header alone is enough.
			for _, name := range []string{"X-Signature-Previous", "X-Signature-V2-Previous"} {
				h := http.Header{}
				h.Set(name, v.Headers[name])
				h.Set("X-Timestamp", v.Headers["X-Timestamp"])
				mustVerify(t, []byte(v.Body), h, *v.PreviousSecret, now)
				if _, err := webhook.Verify([]byte(v.Body), h, v.Secret, now); err == nil {
					t.Fatalf("%s accepted with the new secret", name)
				}
			}
			h := http.Header{}
			h.Set("X-Signature-V2-Previous", v.Headers["X-Signature-V2-Previous"])
			if _, err := webhook.VerifyParsed(decodeUseNumber(t, []byte(v.Body)), h, *v.PreviousSecret, now); err != nil {
				t.Fatalf("VerifyParsed with X-Signature-V2-Previous: %v", err)
			}
			h = http.Header{}
			h.Set("X-Signature-Simple-Previous", v.Headers["X-Signature-Simple-Previous"])
			ev := mustVerify(t, []byte(v.Body), h, *v.PreviousSecret, now, webhook.WithAllowSimple())
			if ev.Authenticated() {
				t.Fatal("simple signature reported as authenticated")
			}
		})
	}
}

func TestVectorReceiverOnNewSecretDuringRotation(t *testing.T) {
	// A receiver that already switched to the new secret while Didit still
	// signs with the old one (no -Previous headers) keeps the old secret in
	// WithSecrets.
	var v vector
	for _, x := range loadVectors(t).Vectors {
		if x.Name == "rotation_receiver_still_on_previous_secret" {
			v = x
		}
	}
	if v.Name == "" {
		t.Fatal("vector rotation_receiver_still_on_previous_secret not found")
	}
	h := http.Header{}
	h.Set("X-Signature-V2", v.Headers["X-Signature-V2-Previous"])
	h.Set("X-Timestamp", v.Headers["X-Timestamp"])
	if _, err := webhook.Verify([]byte(v.Body), h, otherTestSecret, clockAt(v.Now)); err == nil {
		t.Fatal("accepted with the wrong secret")
	}
	mustVerify(t, []byte(v.Body), h, otherTestSecret, clockAt(v.Now), webhook.WithSecrets(v.Secret))
}

func TestVectorSimpleSignatureWithoutSessionID(t *testing.T) {
	// user.status.updated has no session_id: the simple message uses "" for it.
	for _, v := range loadVectors(t).Vectors {
		if v.Name != "user_status_updated" {
			continue
		}
		h := http.Header{}
		h.Set("X-Signature-Simple", v.Headers["X-Signature-Simple"])
		ev := mustVerify(t, []byte(v.Body), h, v.Secret, clockAt(v.Now), webhook.WithAllowSimple())
		if ev.Authenticated() {
			t.Fatal("simple signature reported as authenticated")
		}
		return
	}
	t.Fatal("vector user_status_updated not found")
}

func TestVectorMalformedSignaturesWithAllowSimple(t *testing.T) {
	// malformed_signatures carries a valid X-Signature-Simple; with
	// WithAllowSimple it is accepted, unauthenticated.
	for _, v := range loadVectors(t).Vectors {
		if v.Name != "malformed_signatures" {
			continue
		}
		ev := mustVerify(t, []byte(v.Body), v.header(), v.Secret, clockAt(v.Now), webhook.WithAllowSimple())
		if ev.Authenticated() {
			t.Fatal("simple signature reported as authenticated")
		}
		return
	}
	t.Fatal("vector malformed_signatures not found")
}
