# Changelog

## [1.0.1](https://github.com/didit-protocol/sdk-go/compare/v1.0.0...v1.0.1) (2026-10-29)


### Miscellaneous Chores

* release 1.0.1 ([e0fb65b](https://github.com/didit-protocol/sdk-go/commit/e0fb65bfd03fc388db8e55d4d7daccec69de141b))

## [1.0.0](https://github.com/didit-protocol/sdk-go/compare/v0.1.0...v1.0.0) (2026-10-29)


### ⚠ BREAKING CHANGES

* **spec:** the API spec sync changes the SDK surface (nullable fields, removed fields, account.organizations.list returns a page, the session decision response is the SessionDecision component).

### Features

* add bank.account_check (POST /v3/bank/account-check/) and bank_verifications on the decision ([3dfa1b0](https://github.com/didit-protocol/sdk-go/commit/3dfa1b08fe624c00d4f9a20f3ff2e4d3b14d7d6c))
* **spec:** sync API spec 2026-10-23 ([#2](https://github.com/didit-protocol/sdk-go/issues/2)) ([eed196a](https://github.com/didit-protocol/sdk-go/commit/eed196a12a23a865ea09215448f326173c744f29))
* **spec:** sync API spec 2026-10-27 ([#7](https://github.com/didit-protocol/sdk-go/issues/7)) ([55fba87](https://github.com/didit-protocol/sdk-go/commit/55fba87cc8e17ba9cf438d0d975aca469187c480))


### Miscellaneous Chores

* release 1.0.0 ([53e2047](https://github.com/didit-protocol/sdk-go/commit/53e204768729614e83013dcf463901e73e9f31bf))
