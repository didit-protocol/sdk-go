// Command bank creates a Bank Link, waits for the bank.connection.created
// webhook and lists the connection's accounts.
//
//	DIDIT_API_KEY=... DIDIT_WEBHOOK_SECRET=... go run ./bank
//
// Register the public URL of this server (POST /webhooks/didit) as a webhook
// destination of your application. Run this example in the account holder's
// private interactive terminal to receive and open the hosted URL.
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
	"net"
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

	// A hosted URL is redeemable. Deliver it to the local account holder's
	// controlling terminal even when stdout and stderr are redirected.
	terminal, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		log.Fatal("run the example in a private interactive terminal")
	}
	defer terminal.Close()

	listener, err := net.Listen("tcp", ":"+envOr("PORT", "8080"))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	link, err := client.Bank.CreateLink(ctx, didit.BankLinkCreateRequest{
		Duplicates: didit.Some(didit.BankLinkCreateRequestDuplicatesAllow),
	}, option.WithIdempotencyKey(randomKey()))
	if err != nil {
		fail("create link", err)
	}
	accounts := make(chan *didit.BankAccountList, 1)
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
		created, ok := event.(*webhook.BankConnectionCreatedEvent)
		if !ok || created.ConnectionID == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		page, err := accountsForConnection(r.Context(), client, link.ID, created.ConnectionID)
		if err != nil {
			http.Error(w, "retry account read", http.StatusServiceUnavailable)
			return
		}
		if page == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		select {
		case accounts <- page:
		default:
		}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("webhook receiver listening on %s", listener.Addr())
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("webhook receiver: %v", err)
			stop()
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if _, err := fmt.Fprintln(terminal, "Open your Bank Link:", link.URL); err != nil {
		log.Fatal("could not deliver the hosted link to the terminal")
	}
	timer := time.NewTimer(15 * time.Minute)
	defer timer.Stop()
	select {
	case page := <-accounts:
		fmt.Printf("Read %d account(s) on the first page.\n", len(page.Items))
	case <-ctx.Done():
		log.Fatal("stopped before the connection was created")
	case <-timer.C:
		log.Fatal("timed out waiting for bank.connection.created; open the link and check the webhook destination")
	}
}

// An application receives connection events for all its users. A nil page
// means this delivery belongs to another link.
func accountsForConnection(ctx context.Context, client *didit.Client, linkID, connectionID string) (*didit.BankAccountList, error) {
	link, err := client.Bank.GetLink(ctx, linkID)
	if err != nil {
		return nil, err
	}
	if link.ConnectionID == "" {
		return nil, errors.New("link has no connection yet")
	}
	if connectionID != link.ConnectionID {
		return nil, nil
	}
	return client.Bank.ListAccounts(ctx, link.ConnectionID, didit.BankListAccountsParams{})
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
