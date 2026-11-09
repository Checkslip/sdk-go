# Changelog

All notable changes to this module are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the module uses
[semantic versioning](https://semver.org).

## [Unreleased]

## [0.1.0] - 2026-11-09

First release.

### Added

- `NewClient` with `WithBaseURL`, `WithHTTPClient`, `WithTimeout`, `WithMaxRetries` and `WithUserAgent`.
- Per-call options `WithIdempotencyKey` and `WithHeader`.
- Slip lookups from QR text (`Slips.Lookup`) and from an image (`Slips.LookupImage`), lookup history (`Get`, `List`, `ListAll`, `Counts`) and CSV exports.
- Billing (balance, billing details, ledger, orders, packages, receipts), usage, supported banks.
- Webhook endpoints and deliveries, and `WebhookVerifier` for Standard Webhooks v1 signatures, including the two-secret window after a rotation.
- Audit activity, request records, evidence files and signing keys.
- `*Error` with the RFC 9457 problem fields, plus `IsRateLimited`, `IsNoCredit`, `HasCode` and other helpers.
- Retries with exponential backoff and jitter: 429 after `Retry-After`; 5xx and network failures only for idempotent requests or requests with an `Idempotency-Key`. Lookups always carry a key.
- `Iter` for walking every page, with `Iter.All` for range loops on Go 1.23 or later.

[Unreleased]: https://github.com/Checkslip/checkslip-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Checkslip/checkslip-go/releases/tag/v0.1.0
