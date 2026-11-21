// Command aml_screening screens a person or company against sanctions, PEP
// and warning lists with the standalone AML Screening API, and prints the
// verdict and the strongest hits.
//
// The request carries an Idempotency-Key: retrying with the same key and the
// same body returns the first result without running or billing a second
// screening.
//
//	DIDIT_API_KEY=... go run ./aml_screening -name "John Smith" -dob 1980-04-12 -nationality GB
//
// Environment:
//
//	DIDIT_API_KEY   your application's API key (required)
//	DIDIT_BASE_URL  API base URL, for example a local mock (optional)
package main

import (
	"context"
	"crypto/rand"
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
	name := flag.String("name", "", "full name of the person or company (required)")
	company := flag.Bool("company", false, "screen a company instead of a person")
	dob := flag.String("dob", "", "date of birth or incorporation, YYYY-MM-DD")
	nationality := flag.String("nationality", "", "ISO 3166-1 alpha-2 country code, for example ES")
	idempotencyKey := flag.String("idempotency-key", "", "reuse a key to retry safely (default: a new random key)")
	flag.Parse()
	if *name == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *idempotencyKey == "" {
		*idempotencyKey = "aml-" + rand.Text()
	}

	var opts []option.RequestOption
	if baseURL := os.Getenv("DIDIT_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := didit.NewClient(opts...) // reads DIDIT_API_KEY

	params := didit.AMLScreenParams{
		FullName:       *name,
		VendorData:     didit.String("user-42"),
		IdempotencyKey: didit.String(*idempotencyKey), // sent as the Idempotency-Key header
	}
	if *company {
		params.EntityType = didit.Some(didit.AMLScreenParamsEntityTypeCompany)
	}
	if *dob != "" {
		params.DateOfBirth = didit.String(*dob)
	}
	if *nationality != "" {
		params.Nationality = didit.String(*nationality)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := client.AML.Screen(ctx, params)
	if err != nil {
		fail(err, *idempotencyKey)
	}

	aml := res.AML
	fmt.Printf("request %s (idempotency key %s)\n", res.RequestID, *idempotencyKey)
	fmt.Printf("%s: status %s, risk score %.1f, %d hit(s)\n", *name, aml.Status, aml.Score, aml.TotalHits)
	for i, hit := range aml.Hits {
		if i == 5 {
			fmt.Printf("... and %d more\n", len(aml.Hits)-i)
			break
		}
		fmt.Printf("  %-40s match %.0f, risk %.0f, datasets %v\n", hit.Caption, hit.MatchScore, hit.RiskScore, hit.Datasets)
	}
}

func fail(err error, idempotencyKey string) {
	var (
		configErr  *didit.ConfigurationError
		badRequest *didit.BadRequestError
		conflict   *didit.ConflictError
		rateLimit  *didit.RateLimitError
		statusErr  *didit.APIStatusError
		connErr    *didit.APIConnectionError
	)
	switch {
	case errors.As(err, &configErr):
		log.Fatal(configErr.Message)
	case errors.As(err, &badRequest):
		log.Fatalf("invalid request (400), nothing was billed: %v", badRequest.Body)
	case errors.As(err, &conflict):
		log.Fatalf("idempotency key %q was already used with a different request (409)", idempotencyKey)
	case errors.As(err, &rateLimit):
		log.Fatalf("rate limited (429); retry after %s seconds", rateLimit.Header.Get("Retry-After"))
	case errors.As(err, &statusErr):
		log.Fatalf("API error %d (request id %q): %v", statusErr.StatusCode, statusErr.RequestID, err)
	case errors.As(err, &connErr):
		// The screening may or may not have run: retry with the same key.
		log.Fatalf("no response (%v); retry with -idempotency-key %s", err, idempotencyKey)
	default:
		log.Fatal(err)
	}
}
