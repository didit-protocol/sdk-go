# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately, never in a public issue or pull request:

- through GitHub private vulnerability reporting: [Report a vulnerability](https://github.com/didit-protocol/sdk-go/security/advisories/new), or
- by email to [support@didit.me](mailto:support@didit.me) with "Security" in the subject.

Include the SDK version, a description of the issue and, if you can, steps to reproduce it. Never include real API keys, webhook secrets, tokens or personal data; use placeholders. We acknowledge reports within three working days and keep you informed until the issue is fixed.

## Supported versions

Security fixes go to the latest minor version. The SDK is in `0.x` until its first public release.

## Handling credentials

- The SDK sends the API key only as the `x-api-key` header, only to the configured base URL's origin. It refuses pagination links and redirects to any other origin, and never writes the key to logs or error messages.
- Webhook signatures are compared in constant time. Keep the webhook secret in your secret store and verify every delivery.
- Authentication tokens are verified locally with ES256 against the issuer's published keys; other algorithms are rejected.
