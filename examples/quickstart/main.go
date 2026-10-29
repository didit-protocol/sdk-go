// Command quickstart creates a KYC verification session, prints the URL to
// send the user to, reads the session's decision, and then verifies a webhook
// delivery that it signs itself the way Didit does.
//
//	DIDIT_API_KEY=... DIDIT_WORKFLOW_ID=... go run ./quickstart
//
// Environment:
//
//	DIDIT_API_KEY         your application's API key (required)
//	DIDIT_WORKFLOW_ID     the workflow the session runs (required)
//	DIDIT_WEBHOOK_SECRET  the webhook destination's secret (default: a test value)
//	DIDIT_BASE_URL        API base URL, for example a local mock (optional)
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
	"github.com/didit-protocol/sdk-go/webhook"
)

func main() {
	log.SetFlags(0)
	ctx := context.Background()

	workflowID := os.Getenv("DIDIT_WORKFLOW_ID")
	if workflowID == "" {
		log.Fatal("set DIDIT_WORKFLOW_ID to the id of a published workflow (Console > Workflows)")
	}

	var opts []option.RequestOption
	if baseURL := os.Getenv("DIDIT_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := didit.NewClient(opts...) // reads DIDIT_API_KEY

	// 1. Create a session and send the user to its URL.
	session, err := client.Sessions.Create(ctx, didit.SessionsCreateParams{
		WorkflowID: workflowID,
		VendorData: didit.String("user-42"), // your own id for this user
	})
	if err != nil {
		fail("create session", err)
	}
	fmt.Printf("session %s created (%s)\n", session.SessionID, session.Status)
	fmt.Printf("send the user to %s\n", session.URL)

	// 2. Read the decision. In production, wait for the status.updated
	// webhook and read the decision then.
	decision, err := client.Sessions.RetrieveDecision(ctx, session.SessionID, didit.SessionsRetrieveDecisionParams{})
	if err != nil {
		fail("retrieve decision", err)
	}
	fmt.Printf("decision: status %q, features %v\n", decision.Status, decision.Features)

	// 3. Verify a webhook delivery. Here we play Didit's part and sign a
	// sample status.updated delivery for the session.
	secret := os.Getenv("DIDIT_WEBHOOK_SECRET")
	if secret == "" {
		secret = "whsec_test_quickstart_3f9a1c7e5b2d4086"
	}
	body, header := signedDelivery(secret, map[string]any{
		"event_id":     newUUID(),
		"webhook_type": "status.updated",
		"timestamp":    time.Now().Unix(),
		"created_at":   time.Now().Unix(),
		"session_id":   session.SessionID,
		"status":       "Approved",
		"workflow_id":  session.WorkflowID,
		"vendor_data":  session.VendorData,
		"environment":  "sandbox",
	})

	event, err := webhook.Verify(body, header, secret)
	if err != nil {
		fail("verify webhook", err)
	}
	switch e := event.(type) {
	case *webhook.StatusUpdatedEvent:
		fmt.Printf("verified %s event %s: session %s is %s (vendor_data %q, environment %s)\n",
			e.Type(), e.EventID, e.SessionID, e.Status, e.VendorData, e.Environment)
	default:
		fmt.Printf("verified %s event %s\n", e.Type(), e.Envelope().EventID)
	}

	// A delivery whose body was changed after signing is rejected.
	tampered := []byte(string(body[:len(body)-1]) + `,"injected":true}`)
	_, err = webhook.Verify(tampered, header, secret)
	var verr *didit.WebhookVerificationError
	if errors.As(err, &verr) {
		fmt.Printf("tampered delivery rejected: %s\n", verr.Reason)
	}
}

// signedDelivery encodes payload like Didit does (sorted keys, no spaces) and
// returns it with the headers Didit sends: X-Signature is the hex
// HMAC-SHA256 of the body, X-Signature-V2 that of its canonical JSON, and
// X-Timestamp the body's timestamp.
func signedDelivery(secret string, payload map[string]any) ([]byte, http.Header) {
	body, err := json.Marshal(payload) // encoding/json sorts map keys
	if err != nil {
		log.Fatal(err)
	}
	canonical, err := webhook.Canonicalize(body)
	if err != nil {
		log.Fatal(err)
	}
	header := http.Header{}
	header.Set("X-Signature", hmacHex(secret, body))
	header.Set("X-Signature-V2", hmacHex(secret, canonical))
	header.Set("X-Timestamp", strconv.FormatInt(payload["timestamp"].(int64), 10))
	return body, header
}

func hmacHex(secret string, message []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(message)
	return hex.EncodeToString(mac.Sum(nil))
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// fail explains an SDK error and exits.
func fail(action string, err error) {
	var (
		configErr  *didit.ConfigurationError
		authErr    *didit.AuthenticationError
		notFound   *didit.NotFoundError
		statusErr  *didit.APIStatusError
		timeoutErr *didit.APITimeoutError
		connErr    *didit.APIConnectionError
		webhookErr *didit.WebhookVerificationError
	)
	switch {
	case errors.As(err, &configErr):
		log.Fatalf("%s: %s", action, configErr.Message)
	case errors.As(err, &authErr):
		log.Fatalf("%s: the API key was rejected (401); check DIDIT_API_KEY", action)
	case errors.As(err, &notFound):
		log.Fatalf("%s: not found (404): %v", action, err)
	case errors.As(err, &statusErr):
		log.Fatalf("%s: HTTP %d, request id %q: %v", action, statusErr.StatusCode, statusErr.RequestID, err)
	case errors.As(err, &timeoutErr):
		log.Fatalf("%s: timed out: %v", action, err)
	case errors.As(err, &connErr):
		log.Fatalf("%s: could not reach the API (check DIDIT_BASE_URL and your network): %v", action, err)
	case errors.As(err, &webhookErr):
		log.Fatalf("%s: rejected (%s): %v", action, webhookErr.Reason, err)
	default:
		log.Fatalf("%s: %v", action, err)
	}
}
