// Command id_verification verifies an identity document with the standalone
// ID Verification API: it uploads the front (and optionally the back) image
// and prints the extracted data and any warnings.
//
//	DIDIT_API_KEY=... go run ./id_verification -front front.jpg -back back.jpg
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
	front := flag.String("front", "", "path to the image of the front of the document (required)")
	back := flag.String("back", "", "path to the image of the back of the document")
	vendorData := flag.String("vendor-data", "user-42", "your own id for the person")
	flag.Parse()
	if *front == "" {
		flag.Usage()
		os.Exit(2)
	}

	var opts []option.RequestOption
	if baseURL := os.Getenv("DIDIT_BASE_URL"); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := didit.NewClient(opts...) // reads DIDIT_API_KEY

	params := didit.IDVerificationVerifyParams{
		FrontImage: didit.FileFromPath(*front),
		VendorData: didit.String(*vendorData),
	}
	if *back != "" {
		params.BackImage = didit.Some(didit.FileFromPath(*back))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := client.IDVerification.Verify(ctx, params)
	if err != nil {
		fail(err)
	}

	id := res.IDVerification
	fmt.Printf("request %s: %s\n", res.RequestID, id.Status)
	fmt.Printf("document: %s %s issued by %s, expires %s\n", id.DocumentType, id.DocumentNumber, id.IssuingStateName, id.ExpirationDate)
	fmt.Printf("holder:   %s, born %s (age %d), nationality %s\n", id.FullName, id.DateOfBirth, id.Age, id.Nationality)
	for _, w := range id.Warnings {
		fmt.Printf("warning:  [%s] %s: %s\n", w.Risk, w.Feature, w.ShortDescription)
	}
}

func fail(err error) {
	var (
		configErr  *didit.ConfigurationError
		badRequest *didit.BadRequestError
		statusErr  *didit.APIStatusError
		connErr    *didit.APIConnectionError
	)
	switch {
	case errors.As(err, &configErr):
		// A missing API key, or an image file that cannot be read.
		log.Fatal(configErr.Message)
	case errors.As(err, &badRequest):
		// The body names the field that was rejected, for example an image
		// that is too large or not a supported format.
		log.Fatalf("the API rejected the request (400): %v", badRequest.Body)
	case errors.As(err, &statusErr):
		log.Fatalf("API error %d (request id %q): %v", statusErr.StatusCode, statusErr.RequestID, err)
	case errors.As(err, &connErr):
		log.Fatalf("could not reach the API: %v", err)
	default:
		log.Fatal(err)
	}
}
