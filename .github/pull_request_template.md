## What changes

<!-- What and why, in a few sentences. Link the issue. -->

## Checklist

- [ ] Conventional commit type matches the semver impact (`feat`, `fix`, `feat!`/`BREAKING CHANGE:`, `chore`, `docs`, ...)
- [ ] Generated code regenerated from `openapi/` (`go run ./codegen`), never edited by hand
- [ ] Tests added or updated; `go test -race ./...`, `go vet ./...`, `golangci-lint run` pass
- [ ] README updated for public API changes
- [ ] No secrets, API keys or real customer data (fixtures are synthetic; webhook secrets start with `whsec_test_`)

## Parity

Parity: same change in the other six SDK repositories (`sdk-python`, `sdk-node`, `sdk-java`, `sdk-php`, `sdk-ruby`, `sdk-dotnet`): <!-- links to the pull requests -->, or not applicable because <!-- reason -->.

If this changes behaviour that `docs/DESIGN.md` describes, `docs/DESIGN.md` changes in all seven repositories in the same words.
