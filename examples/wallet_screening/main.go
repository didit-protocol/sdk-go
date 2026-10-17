// Command wallet_screening screens a crypto wallet address for AML risk and
// saves the result as a Didit-branded PDF report.
//
//	DIDIT_API_KEY=... go run ./wallet_screening -address 0x... -blockchain ETH -out report.pdf
//
// Environment:
//
//	DIDIT_API_KEY   your application's API key (required)
//	DIDIT_BASE_URL  API base URL, for example a local mock (optional)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

func main() {
	log.SetFlags(0)
	address := flag.String("address", "", "wallet address to screen (required)")
	blockchain := flag.String("blockchain", "ETH", "network: BTC, ETH, SOL, TRX, BASE, ...")
	out := flag.String("out", "wallet-screening.pdf", "where to save the PDF report")
	flag.Parse()
	if *address == "" {
		flag.Usage()
		os.Exit(2)
	}

	var opts []option.RequestOption
	if baseURL := os.Getenv("DIDIT_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := didit.NewClient(opts...) // reads DIDIT_API_KEY
	ctx := context.Background()

	result, err := client.WalletScreening.Screen(ctx, didit.WalletScreeningScreenParams{
		WalletAddress: *address,
		Blockchain:    didit.WalletScreeningScreenParamsBlockchain(*blockchain),
		Direction:     didit.Some(didit.WalletScreeningScreenParamsDirectionInbound),
	})
	if err != nil {
		fail("screen wallet", err)
	}
	fmt.Printf("%s on %s: risk score %d, severity %s, sanctions hit %t (provider %s)\n",
		result.WalletAddress, result.Blockchain, result.RiskScore, result.Severity, result.SanctionsHit, result.Provider)
	for _, f := range result.RiskFactors {
		fmt.Printf("  risk factor: %s (%s), %.1f%% of funds\n", f.Label, f.Category, f.Percentage)
	}

	// The PDF endpoint takes the screening result back, unmodified and with
	// its report_signature: decode the response exactly as received into the
	// request type.
	var report didit.WalletScreeningResultParam
	if err := json.Unmarshal([]byte(result.JSON.Raw()), &report); err != nil {
		log.Fatalf("decode screening result: %v", err)
	}
	resp, err := client.WalletScreening.ScreenPDF(ctx, report)
	if err != nil {
		fail("render PDF", err)
	}
	defer resp.Body.Close()

	file, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	n, err := io.Copy(file, resp.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Fatalf("save PDF: %v", err)
	}
	fmt.Printf("saved %s (%d bytes, %s)\n", *out, n, resp.Header.Get("Content-Type"))
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
		// For example an address that does not match the network, or a
		// result whose signed fields were changed before rendering.
		log.Fatalf("%s: invalid request (400): %v", action, badRequest.Body)
	case errors.As(err, &statusErr):
		log.Fatalf("%s: API error %d (request id %q): %v", action, statusErr.StatusCode, statusErr.RequestID, err)
	case errors.As(err, &connErr):
		log.Fatalf("%s: could not reach the API: %v", action, err)
	default:
		log.Fatalf("%s: %v", action, err)
	}
}
