// Command chi receives Didit webhooks with Chi at POST /webhooks/didit.
//
//	DIDIT_WEBHOOK_SECRET=... go run ./webhooks/chi
//
// Environment:
//
//	DIDIT_WEBHOOK_SECRET  the webhook destination's secret (required)
//	PORT                  port to listen on (default 8080)
package main

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

func main() {
	secret := os.Getenv("DIDIT_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("set DIDIT_WEBHOOK_SECRET to the secret of your webhook destination")
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	// RequestSize caps the body at 1 MiB.
	r.With(middleware.RequestSize(1<<20)).Post("/webhooks/didit", func(w http.ResponseWriter, r *http.Request) {
		// Verify needs the raw body exactly as sent.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}
		event, err := webhook.Verify(body, r.Header, secret)
		if err != nil {
			var verr *didit.WebhookVerificationError
			if errors.As(err, &verr) {
				log.Printf("rejected webhook: %s", verr.Reason)
			}
			http.Error(w, "invalid webhook", http.StatusBadRequest)
			return
		}
		handle(event)
		w.WriteHeader(http.StatusOK)
	})

	addr := ":" + envOr("PORT", "8080")
	log.Printf("listening on %s", addr)
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
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
