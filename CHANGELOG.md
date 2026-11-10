# Changelog

All notable changes to this module are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the module uses
[semantic versioning](https://semver.org).

## [0.4.0](https://github.com/Checkslip/sdk-go/compare/v0.3.1...v0.4.0) (2026-11-10)


### ⚠ BREAKING CHANGES

* ListSlipsParams.FreeReason and SlipExportRequest.FreeReason are []CustomerFreeReason, the nine reasons a customer can receive, instead of []FreeReason. PENDING, NO_CREDIT and HOLD_EXPIRED are not among them, so code that passes one of those constants no longer compiles. The reason a lookup reports back is unchanged: still FreeReason, including a value this version does not know. Counts still take the filter and still ignore it, so nothing changes there.
* UsageCounts no longer has FreeNoCredit or FreeHoldExpired (free_no_credit and free_hold_expired in the API response). Both had been returning 0 on every account before this release, and the API has now removed them from UsageCounts. Code that reads either one no longer compiles, so delete those reads. A lookup that was not charged, with no reason given, counts in FreeOther.

### Features

* the free_reason filter takes the reasons a customer receives ([cb690eb](https://github.com/Checkslip/sdk-go/commit/cb690ebc3ba5e7a7e967991cf6488d5f819cf458))
* UsageCounts no longer has FreeNoCredit or FreeHoldExpired ([785a18a](https://github.com/Checkslip/sdk-go/commit/785a18a5f92ffd7d3207b80af79fecb94503da2c))

## [0.3.1](https://github.com/Checkslip/sdk-go/compare/v0.3.0...v0.3.1) (2026-11-10)


### Bug Fixes

* **docs:** say what an exhaustive match really needs ([f4e120c](https://github.com/Checkslip/sdk-go/commit/f4e120ce164d62e6d50585e00db436f22bf666d7))

## [0.3.0](https://github.com/Checkslip/sdk-go/compare/v0.2.0...v0.3.0) (2026-11-09)


### ⚠ BREAKING CHANGES

* **audit:** the four request filters take a list instead of a single value. A call that passed one value now passes a list of one.

### Features

* **audit:** take several values per request filter ([14db03a](https://github.com/Checkslip/sdk-go/commit/14db03a54bd1a8ce51b3a8d9332b8429068834a9))

## [0.2.0](https://github.com/Checkslip/sdk-go/compare/v0.1.1...v0.2.0) (2026-11-09)


### ⚠ BREAKING CHANGES

* **usage:** free_no_credit and free_hold_expired are deprecated and will disappear. The API no longer counts either reason, so both are always 0 and both fields will be removed from the response. They are optional from this version, so code that reads them has to handle them being absent. The counts they used to carry are in free_other.

### Features

* **usage:** add free_other and retire two free counts ([e20f717](https://github.com/Checkslip/sdk-go/commit/e20f717b9c9c2a55de8376aff56b52d0fb1d20cb))


### Bug Fixes

* **ci:** read the version constant past its trailing comment ([eb28255](https://github.com/Checkslip/sdk-go/commit/eb28255798632d349b56aaff3bd890ec9d7de2e0))

## [0.1.1](https://github.com/Checkslip/sdk-go/compare/v0.1.0...v0.1.1) (2026-11-09)


### Bug Fixes

* **ci:** bump the version constant and stop CI cancelling itself ([2c6ab80](https://github.com/Checkslip/sdk-go/commit/2c6ab80093bc7485237fbc0f227a57df56d44825))
* **ci:** call publish from the release job ([91cab7c](https://github.com/Checkslip/sdk-go/commit/91cab7c1ef17daa9724490113a47a9c47626ecad))
* **ci:** check the release pull request before it is merged ([09968c9](https://github.com/Checkslip/sdk-go/commit/09968c9c4b30f94910c3e0b8dbce8538b94269a3))
* **ci:** give the artifact step the tag it uses ([3259247](https://github.com/Checkslip/sdk-go/commit/3259247a930446275a0e475548e4c639a099d3fe))
* **ci:** publish through the release workflow, never directly ([f075042](https://github.com/Checkslip/sdk-go/commit/f075042c54d9b119b37589a53e6b4ecb7c0abcfa))
* **ci:** stop the publish job waiting for itself ([d3c2461](https://github.com/Checkslip/sdk-go/commit/d3c2461fd881fb37e5b1436b1e62a3acc28c4b2d))

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
