// Package didit is the official Didit Go library: identity verification (KYC),
// business verification (KYB), AML screening, biometrics, transaction
// monitoring, Travel Rule and account management from your Go backend.
//
// Create one client per process and reuse it; it is safe for concurrent use:
//
//	client := didit.NewClient() // reads DIDIT_API_KEY
//	session, err := client.Sessions.Create(ctx, didit.SessionsCreateParams{
//		WorkflowID: workflowID,
//		VendorData: didit.String("user-42"),
//	})
//
// Every field of [Client] is a resource of the API, and every method is one
// operation: a context first, then the path parameters, then a parameters
// struct when the operation has a body or query parameters, then
// [option.RequestOption] values that override the client's settings for that
// call.
//
// Optional request fields have the type [Opt]: their zero value is not sent.
// Response types are plain structs decoded leniently; their JSON field
// ([ResponseJSON]) gives the raw object, which keys were present or null, and
// keys this version does not know.
//
// Errors implement [Error]; use errors.As with [*APIStatusError] or a
// status-specific type such as [*NotFoundError]. Failed requests are retried
// with backoff, except a POST or PATCH that may have reached the server.
//
// List methods return a [Page]; their AutoPaging variants return a
// [PageIterator] over every item of every page.
//
// Webhooks are verified by package [github.com/didit-protocol/sdk-go/webhook]
// and Authentication tokens by package
// [github.com/didit-protocol/sdk-go/authentication].
//
// API reference: https://docs.didit.me
//
// Most of this package is generated from Didit's OpenAPI documents; see
// https://github.com/didit-protocol/sdk-go for the design and the generator.
package didit

import "github.com/didit-protocol/sdk-go/option"

var _ option.RequestOption // the package documentation links to option
