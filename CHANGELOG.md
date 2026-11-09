# Changelog

All notable changes to this module are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the module uses
[semantic versioning](https://semver.org).

## 0.1.0 (2026-11-09)


### ⚠ BREAKING CHANGES

* **audit:** drop checkpoints, chain checks and signing keys
* **api:** nest services to match the shared surface

### Features

* add the Checkslip API client with typed models ([93b0bd6](https://github.com/Checkslip/sdk-go/commit/93b0bd645be671f8b6a83ec13e74a618e9f2f909))
* **audit:** drop checkpoints, chain checks and signing keys ([d5a77e6](https://github.com/Checkslip/sdk-go/commit/d5a77e63a6b6eb743661e0266987582dd9f03129))


### Bug Fixes

* **ci:** let release-please cut 0.1.0 as the first release ([6a5e378](https://github.com/Checkslip/sdk-go/commit/6a5e37898a976a37a32caf0cd20091830ed5d06e))
* **ci:** use the new module path when warming the proxy ([aabb842](https://github.com/Checkslip/sdk-go/commit/aabb842377040485a289d959c41dcf34f5ef21b0))
* **lint:** repair doc comments and formatting ([765ff46](https://github.com/Checkslip/sdk-go/commit/765ff46528e09637981e18898a88aad31dfc219e))
* **slips:** hide fields being withdrawn from the API ([4d930b9](https://github.com/Checkslip/sdk-go/commit/4d930b9ecf1c48bf6ce9230daf6aad4b04985bc7))


### Miscellaneous Chores

* cut the first release as 0.1.0 ([cb7347e](https://github.com/Checkslip/sdk-go/commit/cb7347eb6562109742f63d01cd04a659fd75c7c3))


### Code Refactoring

* **api:** nest services to match the shared surface ([ed592c6](https://github.com/Checkslip/sdk-go/commit/ed592c6da540ec014b4e61fa9a983c13e4b19841))

## [Unreleased]

## [0.1.0] - 2026-11-09

First release.

### Added

- `NewClient` with `WithBaseURL`, `WithHTTPClient`, `WithTimeout`, `WithMaxRetries` and `WithUserAgent`.
- Per-call options `WithIdempotencyKey` and `WithHeader`.
- Slip lookups with `Slips.Lookup`, from QR text or from an image, lookup history (`Get`, `List`, `ListAll`, `Counts`) and CSV exports under `Slips.Exports`.
- Billing (balance, billing details, ledger, orders, packages, receipts), usage, supported banks.
- Webhook endpoints and deliveries, and `WebhookVerifier` for Standard Webhooks v1 signatures, including the two-secret window after a rotation.
- Audit activity, request records and evidence files.
- `*Error` with the RFC 9457 problem fields, plus `IsRateLimited`, `IsNoCredit`, `HasCode` and other helpers.
- Retries with exponential backoff and jitter: 429 after `Retry-After`; 5xx and network failures only for idempotent requests or requests with an `Idempotency-Key`. Lookups always carry a key.
- `Iter` for walking every page, with `Iter.All` for range loops on Go 1.23 or later.

[Unreleased]: https://github.com/Checkslip/sdk-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Checkslip/sdk-go/releases/tag/v0.1.0
