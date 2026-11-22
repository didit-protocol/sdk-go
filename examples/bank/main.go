// Command bank creates a Bank Link, waits for the bank.connection.created
// webhook and lists the connection's accounts.
//
//	DIDIT_API_KEY=... DIDIT_WEBHOOK_SECRET=... go run ./bank
//
// Register the public URL of this server (POST /webhooks/didit) as a webhook
// destination of your application, then open the printed link.
//
// Environment:
//
//	DIDIT_API_KEY         your application's API key (required)
//	DIDIT_WEBHOOK_SECRET  the webhook destination's secret (required)
//	PORT                  port to listen on (default 8080)
//	DIDIT_BASE_URL        API base URL, for example a local mock (optional)
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
	"github.com/didit-protocol/sdk-go/webhook"
)

func main() {
	log.SetFlags(0)
	secret := os.Getenv("DIDIT_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("set DIDIT_WEBHOOK_SECRET to the secret of your webhook destination")
	}

	var opts []option.RequestOption
	if baseURL := os.Getenv("DIDIT_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := didit.NewClient(opts...) // reads DIDIT_API_KEY

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Receive the bank.connection.created webhook. Start the receiver
	// before creating the link so no delivery is missed.
	connections := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/didit", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
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
		switch e := event.(type) {
		case *webhook.BankConnectionCreatedEvent:
			fmt.Printf("connection %s created (first sync %s)\n", e.ConnectionID, e.InitialStatus)
			select {
			case connections <- e.ConnectionID:
			default:
			}
		case *webhook.BankLinkCompletedEvent:
			fmt.Printf("link %s completed: connection %s\n", e.LinkID, e.ConnectionID)
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              ":" + envOr("PORT", "8080"),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("webhook receiver listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("webhook receiver: %v", err)
			stop()
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// 2. Create the link and send the person to it.
	link, err := client.Bank.CreateLink(ctx, didit.BankLinkCreateRequest{
		Access: didit.Some(didit.BankLinkCreateRequestAccessOngoing),
		Data: didit.Some([]didit.BankLinkCreateRequestData{
			didit.BankLinkCreateRequestDataAccounts,
			didit.BankLinkCreateRequestDataBalances,
			didit.BankLinkCreateRequestDataTransactions,
		}),
		VendorData: didit.String("bank-example-" + randomKey()),
	}, option.WithIdempotencyKey(randomKey()))
	if err != nil {
		fail("create link", err)
	}
	fmt.Printf("bank link %s created (%s)\n", link.ID, link.Status)
	fmt.Printf("send the person to %s\n", link.URL)
	fmt.Println("waiting for bank.connection.created...")

	// 3. Wait for the webhook, then read the accounts.
	timer := time.NewTimer(15 * time.Minute)
	defer timer.Stop()

	var connectionID string
	select {
	case connectionID = <-connections:
	case <-ctx.Done():
		log.Fatal("stopped before the connection was created")
	case <-timer.C:
		log.Fatal("timed out waiting for bank.connection.created; open the link and check the webhook destination")
	}

	accounts, err := client.Bank.ListAccounts(ctx, connectionID, didit.BankListAccountsParams{})
	if err != nil {
		fail("list accounts", err)
	}
	fmt.Printf("connection %s has %d account(s)\n", connectionID, len(accounts.Items))
	for _, account := range accounts.Items {
		fmt.Printf("- %s %s (%s, %s)\n", account.ID, account.Name, account.Type, account.Currency)
	}
	if accounts.NextCursor != "" {
		fmt.Println("more accounts available: pass NextCursor back as Cursor")
	}
}

func randomKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func fail(action string, err error) {
	var statusErr *didit.APIStatusError
	if errors.As(err, &statusErr) {
		log.Fatalf("%s: HTTP %d: %v", action, statusErr.StatusCode, err)
	}
	log.Fatalf("%s: %v", action, err)
}
