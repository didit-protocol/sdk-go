# Didit Go SDK

The official Didit Go library.

Identity verification (KYC), business verification (KYB), AML screening, biometrics, transaction monitoring, Travel Rule and customer Authentication, from your Go backend. Every server API of [Didit](https://didit.me) is available with full types, webhooks are verified safely, and Authentication tokens are validated locally.

> In development. Not released yet: the first release is planned with the 2026-11-01 Didit release.

- API reference: [docs.didit.me](https://docs.didit.me)
- Design contract shared by the seven Didit server SDKs: [docs/DESIGN.md](docs/DESIGN.md)

## Install

```sh
go get github.com/didit-protocol/sdk-go
```

Go 1.22 or later. The module has no dependencies outside the standard library.

## Quickstart

Create a verification session, read its decision, and verify the webhook Didit sends when it changes.

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

func main() {
	ctx := context.Background()
	client := didit.NewClient() // reads DIDIT_API_KEY

	session, err := client.Sessions.Create(ctx, didit.SessionsCreateParams{
		WorkflowID: os.Getenv("DIDIT_WORKFLOW_ID"),
		VendorData: didit.String("user-42"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("send the user to", session.URL)

	decision, err := client.Sessions.RetrieveDecision(ctx, session.SessionID, didit.SessionsRetrieveDecisionParams{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("status:", decision.Status)

	// Webhooks: verify the raw body with the destination's secret.
	http.HandleFunc("POST /webhooks/didit", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		event, err := webhook.Verify(body, r.Header, os.Getenv("DIDIT_WEBHOOK_SECRET"))
		if err != nil {
			http.Error(w, "invalid webhook", http.StatusBadRequest)
			return
		}
		if e, ok := event.(*webhook.StatusUpdatedEvent); ok {
			fmt.Println("session", e.SessionID, "is now", e.Status)
		}
		w.WriteHeader(http.StatusOK)
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

More runnable programs are in [examples/](examples/): standalone ID verification, AML screening, transaction screening, Travel Rule, wallet screening, Authentication token verification, and webhook receivers for net/http, Gin, Echo, Chi and Fiber.

## Configuration

```go
client := didit.NewClient(
	option.WithAPIKey("..."),           // default: DIDIT_API_KEY
	option.WithOrganizationID("..."),   // default: DIDIT_ORGANIZATION_ID
	option.WithApplicationID("..."),    // default: DIDIT_APPLICATION_ID
	option.WithTimeout(30*time.Second), // per attempt, default 60 s
	option.WithMaxRetries(3),           // default 2
)
```

| Option | Default | Notes |
|---|---|---|
| `WithAPIKey` | `DIDIT_API_KEY` | Sent as `x-api-key`, only ever to the base URL's origin. |
| `WithBaseURL` | `https://verification.didit.me` | |
| `WithAccountBaseURL` | `https://apx.didit.me/auth/v2` | For `client.Account`. |
| `WithOrganizationID`, `WithApplicationID` | `DIDIT_ORGANIZATION_ID`, `DIDIT_APPLICATION_ID` | Needed by application-scoped resources (cases, networks, transaction rules, ...). Without them those calls fail before sending. |
| `WithAccountToken` | none | Bearer token from `client.Account.Login`. |
| `WithTimeout` | 60 s | Per attempt. A context deadline bounds the whole call. |
| `WithMaxRetries` | 2 | See [Retries](#retries). |
| `WithHeader`, `WithHeaderAdd`, `WithQuery` | | Extra headers and query parameters. |
| `WithHTTPClient` | | Your own `*http.Client` (proxies, tracing). |
| `WithLogger` | `DIDIT_LOG=debug` | Debug log of method, path, status, duration and request id. Never bodies, headers or keys. |
| `WithResponseInto` | | The raw `*http.Response` of a call. |

Every option works for the whole client or for one call, as the last arguments of any method. `client.WithOptions(...)` returns a copy with more options. The client is safe for concurrent use: create one per process.

Each method takes a `context.Context` first, then the path parameters, then a parameters struct when the operation has a body or query parameters, then options:

```go
client.Sessions.Imports.Create(ctx, params)
client.Cases.Notes.List(ctx, caseUUID, didit.CasesNotesListParams{Limit: didit.Int(50)})
```

### Optional fields

Optional request fields have the type `didit.Opt[T]`. Their zero value is not sent at all, so a Go zero value never reaches the API by accident:

```go
didit.SessionsCreateParams{
	WorkflowID: workflowID,                // required: a plain value
	VendorData: didit.String("user-42"),   // sent
	Callback:   didit.Null[string](),      // sent as null
	Language:   didit.Some(didit.SessionsCreateParamsLanguageEn),
	// every other Opt field: not sent
}
```

Constructors: `didit.String`, `didit.Int`, `didit.Float`, `didit.Bool`, `didit.Time`, `didit.Some` and `didit.Null`.

### Responses

Response types are plain structs. Unknown enum values are kept as they are (enums are string types with constants for the known values), and decoding is lenient: a value that does not fit its Go field leaves it at its zero value instead of failing the call. Every response struct has a `JSON` field:

```go
decision.JSON.Raw()               // the object as received
decision.JSON.Present("metadata") // the key was sent, maybe as null
decision.JSON.Null("metadata")    // the key was sent as null
decision.JSON.ExtraFields()       // keys this SDK version does not know yet
```

When the API reference declares no schema for a response (today `Account.Applications.List`), the method returns the body as `json.RawMessage` for you to decode.

## Errors

Every error the SDK returns implements `didit.Error`. Use `errors.As` with the concrete types:

```go
_, err := client.Sessions.RetrieveDecision(ctx, id, didit.SessionsRetrieveDecisionParams{})
var notFound *didit.NotFoundError
var apiErr *didit.APIStatusError
switch {
case errors.As(err, &notFound):
	// 404
case errors.As(err, &apiErr):
	log.Println(apiErr.StatusCode, apiErr.Body, apiErr.RequestID)
}
```

| Type | When |
|---|---|
| `*APIStatusError` | any non-2xx status: `StatusCode`, `Body` (parsed JSON), `RawBody`, `Header`, `RequestID` |
| `*BadRequestError` 400, `*AuthenticationError` 401, `*PermissionDeniedError` 403, `*NotFoundError` 404, `*ConflictError` 409, `*UnprocessableEntityError` 422, `*RateLimitError` 429, `*InternalServerError` 5xx | unwrap to `*APIStatusError` |
| `*APIConnectionError`, `*APITimeoutError` | no response (`errors.Is(err, context.DeadlineExceeded)` works) |
| `*ConfigurationError` | missing API key, organization or application id, invalid option; nothing was sent |
| `*UnexpectedResponseError` | a 2xx response that is not JSON, or a next page link to another origin or back to a page already fetched |
| `*WebhookVerificationError` | webhook verification failed, with a machine-readable `Reason` |
| `*TokenVerificationError` | an Authentication token failed verification, with a machine-readable `Reason` |

Errors never hold the API key: a key the server echoes reads `[redacted]` in `RawBody` and `Response.Body`, and so does the `X-Api-Key` header of `Response.Request`.

### Retries

Failed calls are retried up to `WithMaxRetries` times (default 2) with exponential backoff from 0.5 s, capped at 8 s, with full jitter, honouring `Retry-After` (capped at 60 s). `GET`, `PUT` and `DELETE` are retried on connection errors, timeouts, 408, 429 and 5xx. `POST` and `PATCH` are never retried once the request may have reached the server: only when the connection failed before the request was sent, or on 429. Upload bodies are buffered, so a retry sends the same bytes.

## Pagination

List methods return one `*didit.Page[T]` (`Count`, `Next`, `Previous`, `Results`, `GetNextPage`). Their `AutoPaging` variants iterate over every item of every page:

```go
// Go 1.23 and later
for session, err := range client.Sessions.ListAutoPaging(ctx, didit.SessionsListParams{}).All() {
	if err != nil {
		return err
	}
	fmt.Println(session.SessionID)
}

// Go 1.22
iter := client.Sessions.ListAutoPaging(ctx, didit.SessionsListParams{})
for iter.Next() {
	fmt.Println(iter.Current().SessionID)
}
if err := iter.Err(); err != nil {
	return err
}
```

A next page link on another origin than the base URL is refused, so the API key never leaves it. So is a next page link back to a page the iteration already fetched, which would loop forever.

## Files

Uploads take a `didit.File` from a path, bytes or a reader, with an optional name and content type:

```go
res, err := client.IDVerification.Verify(ctx, didit.IDVerificationVerifyParams{
	FrontImage: didit.FileFromPath("front.jpg"),
	BackImage:  didit.Some(didit.FileFromReader(reader, "back.png")),
})
```

File responses (PDF reports, CSV templates, attachments) return the `*http.Response`; read its body and close it:

```go
resp, err := client.Sessions.GeneratePDF(ctx, sessionID)
if err != nil {
	return err
}
defer resp.Body.Close()
_, err = io.Copy(file, resp.Body)
```

## Webhooks

`webhook.Verify(body, header, secret)` checks a delivery and returns a typed event; it has no framework dependency.

- Pass the raw body. `X-Signature` signs the exact bytes and `X-Signature-V2` the canonical JSON, which survives middleware that re-encodes the body; either is accepted.
- During a secret rotation, deliveries also carry `-Previous` signatures made with the old secret. A receiver that already switched can accept both with `webhook.WithSecrets(oldSecret)`.
- The body's timestamp must be within 5 minutes of now (`webhook.WithTolerance`). Deduplicate on `event.Envelope().EventID`.
- `X-Signature-Simple` is deprecated and counts only with `webhook.WithAllowSimple()`; the event then reports `Authenticated() == false` and the record should be re-fetched from the API.
- `webhook.VerifyParsed` is for frameworks that only expose the decoded body (decode with `json.Decoder.UseNumber`).
- Anyone can call your endpoint, so until a signature matches, verification costs time and memory in proportion to the body's size. The canonical JSON is built only for a well-formed `X-Signature-V2`, and floats such as `1e308`, which expand to hundreds of digits in it, may add at most 64 KiB; Didit never sends them.

```go
switch e := event.(type) {
case *webhook.StatusUpdatedEvent:            // status.updated: e.SessionID, e.Status, e.Decision
case *webhook.TransactionStatusUpdatedEvent: // transaction.status.updated: e.TransactionID, e.Status, e.Score
case *webhook.TravelRuleStatusUpdatedEvent:  // travel_rule.status.updated
case *webhook.UnknownEvent:                  // a type this version does not know: e.Payload
}
```

Failures are `*didit.WebhookVerificationError` with `Reason` one of `missing_signature`, `invalid_signature`, `timestamp_mismatch`, `timestamp_out_of_tolerance`, `missing_timestamp` and `invalid_payload`.

## Transactions, wallet screening and Travel Rule

```go
tx, err := client.Transactions.Create(ctx, didit.TransactionsCreateParams{
	TransactionID:       "txn-1001",
	TransactionCategory: didit.TransactionsCreateParamsTransactionCategoryFinance,
	TransactionDetails:  didit.TransactionsCreateParamsTransactionDetails{ /* amount, currency, direction */ },
	Subject:             didit.TransactionsCreateParamsSubject{ /* your customer */ },
})

screening, err := client.WalletScreening.Screen(ctx, didit.WalletScreeningScreenParams{ /* address, chain */ })

// The PDF report takes the screening result back, signature included.
var result didit.WalletScreeningResultParam
err = json.Unmarshal([]byte(screening.JSON.Raw()), &result)
pdf, err := client.WalletScreening.ScreenPDF(ctx, result) // read and close pdf.Body

vasps := client.TravelRule.ListVASPsAutoPaging(ctx, didit.TravelRuleListVASPsParams{})
```

Rules (`client.Transactions.Rules`), Travel Rule transfers (`client.Transactions.TravelRule`), Travel Rule settings and wallet addresses (`client.TravelRule`, `client.TravelRule.WalletAddresses`) are covered too; the webhooks `transaction.created`, `transaction.status.updated` and `travel_rule.status.updated` have typed events.

## Authentication

The `authentication` package validates the tokens of [Didit Authentication](https://docs.didit.me/authentication) (customer sign-in for your application's users) in your backend. It talks to your application's issuer and never needs the API key.

```go
auth, err := authentication.NewFromPublishableKey(os.Getenv("DIDIT_PUBLISHABLE_KEY"))
// or authentication.New("https://acme.auth.didit.me")

claims, err := auth.VerifyAccessToken(ctx, bearerToken)
if err != nil {
	// *didit.TokenVerificationError: expired, invalid_signature, invalid_issuer, ...
}
fmt.Println(claims.Subject, claims.VendorData)
```

- Tokens are ES256 JWTs checked locally against the issuer's keys, which are fetched once and cached; any other algorithm is rejected.
- `VerifyIDToken` checks ID tokens and their nonce. `CreatePKCE`, `CreateState` and `CreateNonce` help with the authorization code flow.
- `ExchangeCode` and `Refresh` call the token endpoint. Serialise refreshes for one session: reusing a consumed refresh token revokes the session.
- `GetSession` checks a session instantly (revocation), `RevokeSession` signs it out.
- `CreateStepUp` and `RetrieveStepUp` drive step-up verification. Act only on status `approved`.

## Language notes

Choices this SDK makes where docs/DESIGN.md leaves room:

- **Packages.** `didit` holds the client, every resource and every type; `option` the client and request options; `webhook` and `authentication` are separate packages. Generated files end in `_gen.go`.
- **Names.** API names become Go identifiers with the usual initialisms: `id_verification` is `IDVerification`, `retrieve_decision` is `RetrieveDecision`, schema `VaspDirectoryEntry` is `VASPDirectoryEntry`. A schema component used by both requests and responses has a request copy with the `Param` suffix (`WalletScreeningResultParam`), whose optional fields use `Opt`.
- **Parameters.** An operation whose body is a schema component takes that type. When it also has header or query parameters, its `<Resource><Method>Params` struct holds the component in `Body`. Header parameters such as `Idempotency-Key` are fields of the parameters struct.
- **Unions.** `oneOf`/`anyOf` objects become one struct with the fields of every variant; the discriminating field (for example `type` or `session_kind`) says which apply. Unions of different JSON kinds (a string or an object) are `any`.
- **Absent and null.** Requests distinguish them with `Opt[T]`. Responses use plain fields; `JSON.Present` and `JSON.Null` tell them apart.
- **Pagination.** `iter.Seq2` needs Go 1.23, so `PageIterator.All` is built only on Go 1.23 and later; `Next`/`Current`/`Err` work on Go 1.22. Extra top-level list fields (`kpis`, `total_transactions`) are read with `page.JSON.Field`.
- **Errors.** Go has no exception hierarchy: the status-specific types embed `*APIStatusError` and unwrap to it, and `didit.Error` is the common interface. Invalid path parameters (empty, `.` or `..`) are a `*ConfigurationError`. `*UnexpectedResponseError` is Go-specific.
- **Raw responses.** `option.WithResponseInto(&resp)` instead of a wrapper type.
- **Redirects.** The SDK never follows a redirect to another origin, whatever your `http.Client` does.

## Supported Go versions

Go 1.22 and later. CI tests Go 1.22 to 1.27 on Linux, and the newest Go on macOS and Windows.

## Versioning

Semantic versioning. Releases are cut by release-please from conventional commits; [CHANGELOG.md](CHANGELOG.md) is generated. Until the first public release the version stays `0.x`. A future major version follows Go's semantic import versioning (`github.com/didit-protocol/sdk-go/v2`).

The SDK is generated from Didit's OpenAPI documents (`openapi/`), and a daily job opens a pull request when the API changes. Removed operations stay available, marked deprecated, for one minor version.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to regenerate, test and release, and [SECURITY.md](SECURITY.md) to report a vulnerability.

## License

MIT, see [LICENSE](LICENSE).
