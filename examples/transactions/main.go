// Command transactions submits a transaction for AML and risk screening,
// mints an SDK token so a mobile app can submit transactions for one user,
// and lists your transactions with auto-pagination.
//
//	DIDIT_API_KEY=... go run ./transactions -amount 250 -currency EUR
//
// Environment:
//
//	DIDIT_API_KEY   your application's API key (required)
//	DIDIT_BASE_URL  API base URL, for example a local mock (optional)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

func main() {
	log.SetFlags(0)
	txnID := flag.String("id", fmt.Sprintf("example-%d", time.Now().UnixNano()), "your unique transaction id (the idempotency key)")
	amount := flag.Float64("amount", 250, "amount")
	currency := flag.String("currency", "EUR", "ISO 4217 code or crypto symbol")
	user := flag.String("user", "user-42", "your id for the customer (vendor_data)")
	maxItems := flag.Int("max", 25, "list at most this many transactions")
	flag.Parse()

	var opts []option.RequestOption
	if baseURL := os.Getenv("DIDIT_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := didit.NewClient(opts...) // reads DIDIT_API_KEY
	ctx := context.Background()

	// 1. Screen an outbound bank transfer. Rules run inline: the returned
	// status is the verdict (APPROVED, IN_REVIEW or DECLINED).
	tx, err := client.Transactions.Create(ctx, didit.TransactionsCreateParams{
		TransactionID:       *txnID,
		TransactionCategory: didit.TransactionsCreateParamsTransactionCategoryFinance,
		TransactionDetails: didit.TransactionsCreateParamsTransactionDetails{
			Direction:  didit.TransactionsCreateParamsTransactionDetailsDirectionOutbound,
			Amount:     *amount,
			Currency:   *currency,
			ActionType: didit.String("transfer"),
		},
		Subject: didit.TransactionsCreateParamsSubject{
			VendorData: *user,
			FullName:   "Jane Example",
			EntityType: didit.String("individual"),
		},
		Counterparty: didit.Some(didit.TransactionsCreateParamsCounterparty{
			FullName: didit.String("Acme Supplies Ltd"),
			PaymentMethod: didit.Some(didit.TransactionsCreateParamsCounterpartyPaymentMethod{
				MethodType:     didit.String("bank_account"),
				IssuingCountry: didit.String("DE"),
			}),
		}),
	})
	if err != nil {
		fail("create transaction", err)
	}
	fmt.Printf("transaction %s (%s): %s, score %d, severity %s, %d alert(s)\n",
		tx.TxnID, tx.UUID, tx.Status, tx.Score, tx.Severity, len(tx.Alerts))

	// 2. Mint a short-lived token for the Didit mobile SDKs, bound to one user.
	token, err := client.Transactions.CreateSDKToken(ctx, didit.TransactionsCreateSDKTokenParams{
		VendorData: *user,
		TTLSeconds: didit.Int(900),
	})
	if err != nil {
		fail("create SDK token", err)
	}
	fmt.Printf("SDK token for %s expires at %s\n", *user, token.ExpiresAt.Format(time.RFC3339))

	// 3. List transactions, newest first; the iterator fetches pages as needed.
	iter := client.Transactions.ListAutoPaging(ctx, didit.TransactionsListParams{Limit: didit.Int(50)})
	n := 0
	for iter.Next() {
		t := iter.Current()
		fmt.Printf("  %-36s %-10s %10s %-4s %s\n", t.TxnID, t.Status, t.Amount, t.Currency, t.TxnDate.Format(time.DateOnly))
		if n++; n == *maxItems {
			break
		}
	}
	if err := iter.Err(); err != nil {
		fail("list transactions", err)
	}
	fmt.Printf("listed %d transaction(s)\n", n)
}

func fail(action string, err error) {
	var (
		configErr  *didit.ConfigurationError
		badRequest *didit.BadRequestError
		statusErr  *didit.APIStatusError
		connErr    *didit.APIConnectionError
	)
	switch {
	case errors.As(err, &configErr):
		log.Fatalf("%s: %s", action, configErr.Message)
	case errors.As(err, &badRequest):
		// For example a transaction id that was already used.
		log.Fatalf("%s: invalid request (400): %v", action, badRequest.Body)
	case errors.As(err, &statusErr):
		log.Fatalf("%s: API error %d (request id %q): %v", action, statusErr.StatusCode, statusErr.RequestID, err)
	case errors.As(err, &connErr):
		log.Fatalf("%s: could not reach the API: %v", action, err)
	default:
		log.Fatalf("%s: %v", action, err)
	}
}
