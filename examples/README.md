# Didit Go SDK examples

Runnable programs for the main Didit flows, and webhook receivers for the popular Go web frameworks.

The examples are a separate Go module (`github.com/didit-protocol/sdk-go/examples`), so the SDK itself stays standard-library only. It builds against the SDK in this repository through a `replace` directive. Run every command from this directory:

```sh
cd examples
export DIDIT_API_KEY=...   # your application's API key
```

The examples need Go 1.25 or later, because Gin v1.12 and Echo v4.16 do. The SDK itself supports Go 1.22.

Every program that calls the API reads the key from `DIDIT_API_KEY` (the SDK's default) and accepts `DIDIT_BASE_URL` to point it at another base URL, for example a local mock server.

## Flows

| Example | What it shows | Run |
|---|---|---|
| [quickstart](quickstart) | Create a KYC session, print its URL, read its decision, then verify a webhook delivery signed in-process | `DIDIT_WORKFLOW_ID=... go run ./quickstart` |
| [id_verification](id_verification) | Standalone ID Verification with image uploads | `go run ./id_verification -front front.jpg -back back.jpg` |
| [aml_screening](aml_screening) | Standalone AML screening of a name, with an `Idempotency-Key` | `go run ./aml_screening -name "John Smith" -dob 1980-04-12 -nationality GB` |
| [transactions](transactions) | Transaction screening, an SDK token for mobile apps, and listing with auto-pagination | `go run ./transactions -amount 250 -currency EUR` |
| [travel_rule](travel_rule) | Travel Rule settings, VASP directory search, wallet address book, and an inbound (after-deposit) transfer with `-txid` | `go run ./travel_rule -search coinbase` |
| [wallet_screening](wallet_screening) | Wallet address screening and the PDF report (a binary response) | `go run ./wallet_screening -address 0x... -blockchain ETH -out report.pdf` |
| [authentication](authentication) | Didit Authentication: a net/http middleware that verifies access tokens (401 on failure), and `GetSession` for instant revocation checks | `DIDIT_PUBLISHABLE_KEY=pk_test_... go run ./authentication` |

Extra environment variables:

- `quickstart`: `DIDIT_WORKFLOW_ID` (required), `DIDIT_WEBHOOK_SECRET` (default: a `whsec_test_` value).
- `authentication`: `DIDIT_PUBLISHABLE_KEY` or `DIDIT_AUTH_ISSUER` (for example `https://acme.auth.didit.me`), optional `DIDIT_APPLICATION_ID` to require that audience, and `PORT` (default 8080). It does not use the API key.

Run any program with `-h` for its flags.

## Webhook receivers

Each receiver listens on `PORT` (default 8080) at `POST /webhooks/didit`, reads the raw body with a 1 MiB limit, verifies it with `webhook.Verify`, switches on the event type, and answers 400 when verification fails and 200 when it succeeds. Didit may deliver an event more than once: deduplicate on `event.Envelope().EventID`.

| Framework | Version | Run |
|---|---|---|
| net/http (standard library) | Go 1.22+ routing patterns | `DIDIT_WEBHOOK_SECRET=... go run ./webhooks/nethttp` |
| [Gin](https://github.com/gin-gonic/gin) | v1.12.0 | `DIDIT_WEBHOOK_SECRET=... go run ./webhooks/gin` |
| [Echo](https://github.com/labstack/echo) | v4.16.0 | `DIDIT_WEBHOOK_SECRET=... go run ./webhooks/echo` |
| [Chi](https://github.com/go-chi/chi) | v5.5.0 | `DIDIT_WEBHOOK_SECRET=... go run ./webhooks/chi` |
| [Fiber](https://github.com/gofiber/fiber) | v2.52.15 | `DIDIT_WEBHOOK_SECRET=... go run ./webhooks/fiber` |

The Fiber example uses Fiber v2, because Fiber v3 requires Go 1.26.

To receive real deliveries during development, expose the receiver with a tunnel such as `ngrok http 8080` and register the public URL as a webhook destination of your application. The [quickstart](quickstart) shows how Didit signs a delivery, if you want to send one yourself.

Never parse the body into a struct before verifying it: `X-Signature` covers the exact bytes Didit sent. If your framework only gives you the decoded JSON, use `webhook.VerifyParsed`, which checks `X-Signature-V2`.

## Checks

```sh
go vet ./...
go build ./...
```
