// Command travel_rule reads your Travel Rule settings, searches the VASP
// directory and lists your wallet address book. With -txid it also registers
// Travel Rule data for a crypto deposit that already settled on-chain.
//
//	DIDIT_API_KEY=... go run ./travel_rule -search coinbase
//	DIDIT_API_KEY=... go run ./travel_rule -txid 0xabc... -chain ethereum \
//	    -wallet 0xYourDepositAddress -amount 1500.00 -currency USDC
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

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

func main() {
	log.SetFlags(0)
	search := flag.String("search", "", "search VASPs by name")
	maxItems := flag.Int("max", 10, "show at most this many VASPs and wallet addresses")
	txid := flag.String("txid", "", "register an inbound deposit with this on-chain transaction hash")
	chain := flag.String("chain", "ethereum", "chain of the deposit")
	wallet := flag.String("wallet", "", "your wallet address that received the deposit")
	amount := flag.String("amount", "", "deposit amount, as a decimal string")
	currency := flag.String("currency", "USDC", "deposit asset")
	flag.Parse()

	var opts []option.RequestOption
	if baseURL := os.Getenv("DIDIT_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := didit.NewClient(opts...) // reads DIDIT_API_KEY
	ctx := context.Background()

	settings, err := client.TravelRule.RetrieveSettings(ctx)
	if err != nil {
		fail("retrieve settings", err)
	}
	fmt.Printf("Travel Rule enabled: %t (jurisdiction %q, legal name %q, threshold %s)\n",
		settings.IsEnabled, settings.Jurisdiction, settings.LegalName, settings.ThresholdAmount)

	params := didit.TravelRuleListVASPsParams{}
	if *search != "" {
		params.Search = didit.String(*search)
	}
	vasps := client.TravelRule.ListVASPsAutoPaging(ctx, params)
	for n := 0; n < *maxItems && vasps.Next(); n++ {
		v := vasps.Current()
		fmt.Printf("  VASP %-32s %-4s due diligence %s (%d), rails %v\n", v.Name, v.Jurisdiction, v.DdStatus, v.DdScore, v.ReachableRails)
	}
	if err := vasps.Err(); err != nil {
		fail("list VASPs", err)
	}

	wallets := client.TravelRule.WalletAddresses.ListAutoPaging(ctx, didit.TravelRuleWalletAddressesListParams{})
	for n := 0; n < *maxItems && wallets.Next(); n++ {
		w := wallets.Current()
		fmt.Printf("  wallet %s on %s, holder %q, ownership verified %t\n", w.Address, w.Chain, w.HolderName, w.IsOwnershipVerified)
	}
	if err := wallets.Err(); err != nil {
		fail("list wallet addresses", err)
	}

	if *txid == "" {
		return
	}
	if *wallet == "" || *amount == "" {
		log.Fatal("-txid needs -wallet and -amount")
	}
	inbound, err := client.TravelRule.RegisterInbound(ctx, didit.TravelRuleInboundRequest{
		Chain:         *chain,
		Txid:          *txid,
		WalletAddress: *wallet,
		Amount:        *amount,
		Currency:      *currency,
	})
	if err != nil {
		fail("register inbound transfer", err)
	}
	fmt.Printf("inbound transfer: transaction %s (new: %t), Travel Rule status %s via %s\n",
		inbound.TransactionID, inbound.Created, inbound.TravelRule.Status, inbound.TravelRule.Rail)
}

func fail(action string, err error) {
	var (
		configErr  *didit.ConfigurationError
		badRequest *didit.BadRequestError
		forbidden  *didit.PermissionDeniedError
		statusErr  *didit.APIStatusError
		connErr    *didit.APIConnectionError
	)
	switch {
	case errors.As(err, &configErr):
		log.Fatalf("%s: %s", action, configErr.Message)
	case errors.As(err, &badRequest):
		log.Fatalf("%s: invalid request (400): %v", action, badRequest.Body)
	case errors.As(err, &forbidden):
		log.Fatalf("%s: not allowed (403); is Travel Rule enabled for this application? %v", action, err)
	case errors.As(err, &statusErr):
		log.Fatalf("%s: API error %d (request id %q): %v", action, statusErr.StatusCode, statusErr.RequestID, err)
	case errors.As(err, &connErr):
		log.Fatalf("%s: could not reach the API: %v", action, err)
	default:
		log.Fatalf("%s: %v", action, err)
	}
}
