// Command gin receives Didit webhooks with Gin at POST /webhooks/didit.
//
//	DIDIT_WEBHOOK_SECRET=... go run ./webhooks/gin
//
// Environment:
//
//	DIDIT_WEBHOOK_SECRET  the webhook destination's secret (required)
//	PORT                  port to listen on (default 8080)
package main

import (
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

func main() {
	secret := os.Getenv("DIDIT_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("set DIDIT_WEBHOOK_SECRET to the secret of your webhook destination")
	}

	r := gin.Default()
	r.POST("/webhooks/didit", func(c *gin.Context) {
		// Verify needs the raw body exactly as sent, so do not bind it to a
		// struct first. Cap it at 1 MiB.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		body, err := c.GetRawData()
		if err != nil {
			c.String(http.StatusBadRequest, "cannot read body")
			return
		}
		event, err := webhook.Verify(body, c.Request.Header, secret)
		if err != nil {
			var verr *didit.WebhookVerificationError
			if errors.As(err, &verr) {
				log.Printf("rejected webhook: %s", verr.Reason)
			}
			c.String(http.StatusBadRequest, "invalid webhook")
			return
		}
		handle(event)
		c.Status(http.StatusOK)
	})

	log.Fatal(r.Run(":" + envOr("PORT", "8080")))
}

// handle processes a verified event. Didit retries failed deliveries, so the
// same event can arrive more than once: deduplicate on its EventID, for
// example with a unique key in your database, and answer 200 quickly.
func handle(event webhook.Event) {
	log.Printf("event %s (%s)", event.Envelope().EventID, event.Type())
	switch e := event.(type) {
	case *webhook.StatusUpdatedEvent:
		log.Printf("session %s (vendor_data %q) is %s", e.SessionID, e.VendorData, e.Status)
	case *webhook.TransactionStatusUpdatedEvent:
		log.Printf("transaction %s is %s (score %v)", e.TxnID, e.Status, e.Score)
	case *webhook.TravelRuleStatusUpdatedEvent:
		log.Printf("Travel Rule for transaction %s is %s", e.TxnID, e.TravelRuleStatus)
	case *webhook.UnknownEvent:
		log.Printf("ignoring webhook type %s", e.Type())
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
