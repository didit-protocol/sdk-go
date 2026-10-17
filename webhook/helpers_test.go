package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

// Synthetic secrets; the whsec_test_ prefix is allowlisted for secret scanners.
const (
	testSecret      = "whsec_test_9d2f7a1c4b8e6f30a5c7e1d9b3f2a846"
	otherTestSecret = "whsec_test_other_5e1a9c3f7b2d4e6081a3c5e7f9b1d2c4"
	testTimestamp   = int64(1760702400)
)

// testNow is ten seconds after testTimestamp, well inside the tolerance.
var testNow = webhook.WithClock(func() time.Time { return time.Unix(testTimestamp+10, 0) })

func clockAt(unix int64) webhook.Option {
	return webhook.WithClock(func() time.Time { return time.Unix(unix, 0) })
}

func hmacHex(secret string, msg []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(msg)
	return hex.EncodeToString(mac.Sum(nil))
}

// signedBody encodes payload like a JSON encoder would and returns it with
// the X-Signature, X-Signature-V2 and X-Timestamp headers Didit would send.
func signedBody(t *testing.T, payload map[string]any, secret string) ([]byte, http.Header) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return body, signHeaders(t, body, secret)
}

func signHeaders(t *testing.T, body []byte, secret string) http.Header {
	t.Helper()
	canonical, err := webhook.Canonicalize(body)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	h := http.Header{}
	h.Set("X-Signature", hmacHex(secret, body))
	h.Set("X-Signature-V2", hmacHex(secret, canonical))
	var env struct {
		Timestamp json.Number `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Timestamp != "" {
		h.Set("X-Timestamp", env.Timestamp.String())
	}
	return h
}

// envelope returns the envelope fields of a test payload for webhookType.
func envelope(webhookType string) map[string]any {
	return map[string]any{
		"event_id":       "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		"webhook_type":   webhookType,
		"timestamp":      testTimestamp,
		"created_at":     testTimestamp - 6,
		"application_id": "0b5e8d2c-1f4a-4c3b-9d6e-2a7f8c1b3e5d",
		"environment":    "live",
	}
}

func with(base map[string]any, kv ...any) map[string]any {
	out := make(map[string]any, len(base)+len(kv)/2)
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func decodeUseNumber(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	dec := json.NewDecoder(bytesReader(body))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func wantReason(t *testing.T, err error, reason didit.WebhookVerificationReason) {
	t.Helper()
	var verr *didit.WebhookVerificationError
	if !errors.As(err, &verr) {
		t.Fatalf("got error %v (%T), want *didit.WebhookVerificationError %s", err, err, reason)
	}
	if verr.Reason != reason {
		t.Fatalf("got reason %s (%v), want %s", verr.Reason, err, reason)
	}
}

func mustVerify(t *testing.T, body []byte, h http.Header, secret string, opts ...webhook.Option) webhook.Event {
	t.Helper()
	ev, err := webhook.Verify(body, h, secret, opts...)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return ev
}

type vectorFile struct {
	ToleranceSeconds int      `json:"tolerance_seconds"`
	Vectors          []vector `json:"vectors"`
}

type vector struct {
	Name           string            `json:"name"`
	Secret         string            `json:"secret"`
	PreviousSecret *string           `json:"previous_secret"`
	Body           string            `json:"body"`
	CanonicalV2    string            `json:"canonical_v2"`
	Headers        map[string]string `json:"headers"`
	Now            int64             `json:"now"`
	Expect         string            `json:"expect"`
}

func (v vector) header() http.Header {
	h := http.Header{}
	for k, val := range v.Headers {
		h.Set(k, val)
	}
	return h
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("../openapi/webhook-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var f vectorFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Vectors) != 25 {
		t.Fatalf("got %d vectors, want 25", len(f.Vectors))
	}
	if f.ToleranceSeconds != int(webhook.DefaultTolerance/time.Second) {
		t.Fatalf("vectors use tolerance %d s, DefaultTolerance is %s", f.ToleranceSeconds, webhook.DefaultTolerance)
	}
	return f
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
