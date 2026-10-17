// Command fiber receives Didit webhooks with Fiber v2 at POST /webhooks/didit.
//
//	DIDIT_WEBHOOK_SECRET=... go run ./webhooks/fiber
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

	"github.com/gofiber/fiber/v2"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

func main() {
	secret := os.Getenv("DIDIT_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("set DIDIT_WEBHOOK_SECRET to the secret of your webhook destination")
	}

	// BodyLimit caps request bodies at 1 MiB (Fiber answers 413 above it).
	app := fiber.New(fiber.Config{BodyLimit: 1 << 20, DisableStartupMessage: true})
	app.Post("/webhooks/didit", func(c *fiber.Ctx) error {
		// Verify needs the raw body exactly as sent, so do not BodyParser it
		// first. Fiber reuses the body buffer once the handler returns; the
		// copy keeps the event valid if you hand it to a queue.
		body := append([]byte(nil), c.Body()...)
		event, err := webhook.Verify(body, http.Header(c.GetReqHeaders()), secret)
		if err != nil {
			var verr *didit.WebhookVerificationError
			if errors.As(err, &verr) {
				log.Printf("rejected webhook: %s", verr.Reason)
			}
			return c.Status(fiber.StatusBadRequest).SendString("invalid webhook")
		}
		handle(event)
		return c.SendStatus(fiber.StatusOK)
	})

	addr := ":" + envOr("PORT", "8080")
	log.Printf("listening on %s", addr)
	log.Fatal(app.Listen(addr))
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
