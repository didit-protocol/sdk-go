package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

// In production, read the secret from your configuration, for example
// os.Getenv("DIDIT_WEBHOOK_SECRET").
const webhookSecret = "whsec_test_example_0a1b2c3d4e5f60718293a4b5c6d7e8f9"

// handleDiditWebhook is a net/http handler for Didit webhooks.
func handleDiditWebhook(w http.ResponseWriter, r *http.Request) {
	// Read the raw body, with a size limit. Verify needs the exact bytes.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	ev, err := webhook.Verify(body, r.Header, webhookSecret)
	if err != nil {
		var verr *didit.WebhookVerificationError
		if errors.As(err, &verr) {
			log.Printf("rejected Didit webhook: %s", verr.Reason)
		}
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}

	// Didit retries deliveries: deduplicate on ev.Envelope().EventID.
	switch e := ev.(type) {
	case *webhook.StatusUpdatedEvent:
		fmt.Printf("session %s is %s\n", e.SessionID, e.Status)
	case *webhook.TransactionStatusUpdatedEvent:
		fmt.Printf("transaction %s is %s\n", e.TxnID, e.Status)
	case *webhook.UnknownEvent:
		fmt.Printf("ignoring webhook type %s\n", e.Type())
	default:
		fmt.Printf("received %s\n", ev.Type())
	}
	w.WriteHeader(http.StatusOK)
}

// This example receives a webhook in a net/http handler. The second half
// plays Didit's part and sends a signed delivery to the handler.
func ExampleVerify() {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := `{"event_id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","session_id":"11111111-2222-3333-4444-555555555555",` +
		`"status":"Approved","timestamp":` + ts + `,"webhook_type":"status.updated"}`
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(body))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/didit", strings.NewReader(body))
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-Timestamp", ts)
	rec := httptest.NewRecorder()
	handleDiditWebhook(rec, req)
	fmt.Println(rec.Code)

	// A delivery whose body was altered is rejected.
	req = httptest.NewRequest(http.MethodPost, "/webhooks/didit", strings.NewReader(strings.Replace(body, "Approved", "Declined", 1)))
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	rec = httptest.NewRecorder()
	handleDiditWebhook(rec, req)
	fmt.Println(rec.Code)

	// Output:
	// session 11111111-2222-3333-4444-555555555555 is Approved
	// 200
	// 400
}

// This example verifies a body that a framework has already decoded. The
// decoder must use UseNumber so that large integers keep their digits.
func ExampleVerifyParsed() {
	raw := `{"timestamp":1760702400,"webhook_type":"user.status.updated","vendor_user_id":"u-1","status":"BLOCKED"}`
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		fmt.Println(err)
		return
	}

	// Didit's X-Signature-V2 header: the HMAC of the canonical JSON.
	canonical, _ := webhook.Canonicalize([]byte(raw))
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(canonical)
	header := http.Header{}
	header.Set("X-Signature-V2", hex.EncodeToString(mac.Sum(nil)))

	ev, err := webhook.VerifyParsed(payload, header, webhookSecret,
		webhook.WithClock(func() time.Time { return time.Unix(1760702410, 0) }))
	if err != nil {
		fmt.Println(err)
		return
	}
	if e, ok := ev.(*webhook.UserStatusUpdatedEvent); ok {
		fmt.Println(e.VendorUserID, e.Status)
	}
	// Output: u-1 BLOCKED
}

func ExampleCanonicalize() {
	out, _ := webhook.Canonicalize([]byte(`{"b": 87.0, "a": "Jos\u00e9", "c": [1e-5, 0.1]}`))
	fmt.Println(string(out))
	// Output: {"a":"José","b":87,"c":[1e-05,0.1]}
}
