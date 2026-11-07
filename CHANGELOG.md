# Changelog

## [1.2.0](https://github.com/didit-protocol/sdk-go/compare/v1.1.0...v1.2.0) (2026-11-07)


### Features

* **spec:** sync API spec 2026-11-07 ([61aa72c](https://github.com/didit-protocol/sdk-go/commit/61aa72c50e15a5178dcce6a170c4901aa434458c))

## [1.1.0](https://github.com/didit-protocol/sdk-go/compare/v1.0.2...v1.1.0) (2026-11-05)


### Features

* **spec:** sync API spec 2026-11-05 (application data location) ([68684f9](https://github.com/didit-protocol/sdk-go/commit/68684f939c45af0a40496122fcff96a3d2e9fa38))

## [1.0.2](https://github.com/didit-protocol/sdk-go/compare/v1.0.1...v1.0.2) (2026-11-05)


### Bug Fixes

* **codegen:** import only the packages the generated code uses ([2e098d3](https://github.com/didit-protocol/sdk-go/commit/2e098d33b3721abd977cf901c30939c32159dc3b))
* **codegen:** keep multipart for identical JSON and multipart schemas with files ([0e7ca0a](https://github.com/didit-protocol/sdk-go/commit/0e7ca0a454628b391d104f2c788258a0eb4a4a08))
* **codegen:** keep the params type of identical JSON and multipart schemas ([0d3f8c9](https://github.com/didit-protocol/sdk-go/commit/0d3f8c910bcf8120bbf238a46420f46bd30518f2))
* **codegen:** multipart file lists send one file part per file ([867e1ce](https://github.com/didit-protocol/sdk-go/commit/867e1cef28dc80b02465769dbc073bd6f2604e41))
* **examples:** blockchain is optional in the wallet screening request ([bb8adb7](https://github.com/didit-protocol/sdk-go/commit/bb8adb72dc53d24677434507ce92e22343e3419d))

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
