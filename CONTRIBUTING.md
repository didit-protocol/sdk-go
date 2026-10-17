# Contributing

Thanks for helping. This repository is one of the seven Didit server SDKs. They share one design contract, [docs/DESIGN.md](docs/DESIGN.md), and one API contract, the OpenAPI documents in `openapi/`. Read both first.

## Layout

| Path | What | Edited by |
|---|---|---|
| `*_gen.go`, `*_gen_test.go` | Generated resources, types and one test per operation | `go run ./codegen` only |
| `codegen/` | The generator (Go, standard library), and `codegen sync` for the spec sync | hand |
| `openapi/verification.json`, `openapi/account.json`, `openapi/webhook-vectors.json` | Upstream copies | the spec sync workflow only |
| `openapi/overlay.json` | SDK naming (`x-didit-sdk`) for operations upstream does not name yet | hand, identical in all seven SDKs |
| `openapi/deprecated-operations.json` | Removed or renamed operations kept for one minor version | `codegen sync` |
| `client.go`, `core.go`, `errors.go`, `opt.go`, `page.go`, `file.go`, `params.go`, `json.go`, `option/`, `internal/` | Hand-written core | hand |
| `webhook/` | Webhook verification and typed events | hand |
| `authentication/` | Authentication token verification and issuer calls | hand |
| `examples/` | Runnable examples (a separate module) | hand |

## Commands

```sh
go run ./codegen            # regenerate from openapi/
go run ./codegen -check     # fail when the generated code is stale (CI)
go test -race ./...         # unit, generated, contract, webhook vector and Authentication tests
go vet ./...
staticcheck ./...           # honnef.co/go/tools/cmd/staticcheck
golangci-lint run           # v2, configured in .golangci.yml
gofmt -l .                  # must print nothing
(cd examples && go vet ./...)
```

Core coverage must stay at 90% or more:

```sh
go test -coverpkg=./... -coverprofile=coverage.out ./...
go run ./internal/tools/corecover -min 90 coverage.out
```

Live smoke tests run only with a sandbox key and are never part of default CI:

```sh
DIDIT_SANDBOX_API_KEY=... DIDIT_SANDBOX_WORKFLOW_ID=... go test -run Live ./...
```

Unit tests never touch the network. Fixtures are synthetic: webhook secrets start with `whsec_test_` (the gitleaks allowlist relies on it), test keys are generated at test time, and no real customer data is ever committed.

## Changing the API surface

The SDK follows the API. Never edit generated files: change the generator, or the naming in `openapi/overlay.json`, then regenerate.

- **The API changed.** The change belongs in the OpenAPI documents of mintlify-docs first. The daily spec sync (`.github/workflows/spec-sync.yml`, `go run ./codegen sync`) downloads them, regenerates, runs the tests and opens a `chore(spec): sync API spec YYYY-MM-DD` pull request listing added, removed and changed operations, with breaking changes called out.
- **A new operation fails the generator** with a message naming it: add its `x-didit-sdk` entry to `openapi/overlay.json` in all seven SDK repositories (or upstream).
- **Removed operations** are never deleted silently: the sync records them in `openapi/deprecated-operations.json` and the generator keeps the method, marked `Deprecated:`, for one minor version.
- **Core behaviour** that docs/DESIGN.md describes (options, errors, retries, pagination, webhooks, Authentication) changes in all seven SDKs, one pull request each, and docs/DESIGN.md changes in all seven in the same words.

## Commits and releases

Use [conventional commits](https://www.conventionalcommits.org): `feat:`, `fix:`, `docs:`, `chore:`, and `feat!:` or a `BREAKING CHANGE:` footer for breaking changes. The type decides the next version.

Releases are cut by release-please (`.github/workflows/release-please.yml`, started by hand): it opens a release pull request that bumps `internal/version/version.go` and generates `CHANGELOG.md` (never edit it by hand). Merging it and running the workflow again pushes the `vX.Y.Z` tag and the GitHub release, which starts `.github/workflows/release.yml` (tests, source archive with build provenance, Go proxy fetch).

Nothing is released before Alex's explicit approval and the 2026-11-01 Didit release; the version stays `0.x` until then, and the repository stays private.

### One-time setup before the first release

1. Make the repository public (only with Alex's approval, after the full-history gitleaks scan passes).
2. Create the `release` environment (Settings > Environments) with required reviewers.
3. Add the `RELEASE_PLEASE_TOKEN` secret: a fine-grained token with Contents, Issues and Pull requests read and write on this repository only, so release-please's pull requests get CI and its releases start `release.yml`.
4. Allow GitHub Actions to create pull requests (Settings > Actions > General), for release-please and the spec sync, and add the `SPEC_SYNC_TOKEN` secret (same scopes as above) so the spec sync pull requests get CI.
5. Protect `main` (required checks: the CI jobs) and tags `v*` (only release-please may create them).
6. Replace `@didit-protocol` in `.github/CODEOWNERS` with the SDK team.
7. After the first tag, check `https://pkg.go.dev/github.com/didit-protocol/sdk-go` (the release workflow asks proxy.golang.org to fetch the version). Go has no registry account, namespace or trusted publisher to configure.
