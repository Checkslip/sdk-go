# Checkslip Go SDK

[ภาษาไทย](README.th.md) · [API docs](https://checkslip.com/docs) · [API reference](https://checkslip.com/docs/reference) · [Go reference](https://pkg.go.dev/github.com/Checkslip/sdk-go)

The official Go client for the [Checkslip](https://checkslip.com) API.

Checkslip looks up a Thai bank transfer from its slip. You send the QR text printed on the slip, or a picture of the slip, and Checkslip asks the sending bank for the transfer it holds and returns that data. Checkslip reports what the bank has. Whether to accept the payment is up to you, so compare the amount, the receiver and the time with your order.

The SDK needs Go 1.22 or later and has no dependencies outside the standard library.

## Install

```sh
go get github.com/Checkslip/sdk-go
```

## Quick start

Create an API key in the Checkslip web app. Keys start with `csk_`. Keep the key out of your source code and pass it in through the environment or a secret manager.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Checkslip/sdk-go"
)

func main() {
	client := checkslip.NewClient(os.Getenv("CHECKSLIP_API_KEY"))

	res, err := client.Slips.Lookup(context.Background(), &checkslip.LookupParams{
		QR:              "00330006000001010306902126302105340875102TH9104F86F",
		ClientReference: "order-1001", // optional, comes back in history and webhooks
	})
	if err != nil {
		log.Fatal(err)
	}

	switch res.Outcome {
	case checkslip.OutcomeFound:
		fmt.Println("amount:", res.Slip.Amount)
		fmt.Println("sent at:", res.Slip.TransferredAt)
		fmt.Println("receiver account:", *res.Slip.Receiver.Account)
	case checkslip.OutcomeNotFound:
		fmt.Println("the bank has no transfer for this reference")
	case checkslip.OutcomeDuplicate:
		fmt.Println("looked up before, first at", res.DuplicateOf.CheckedAt)
	}
}
```

Only a `FOUND` lookup uses a credit.

To send an image instead of the QR text, set `Image` on `LookupParams` instead of `QR`. JPEG, PNG and WebP up to 10 MB are accepted, and Checkslip does not store the image. Setting both, or neither, is an error the client reports before it sends anything.

```go
f, err := os.Open("slip.jpg")
if err != nil {
	return err
}
defer f.Close()

res, err := client.Slips.Lookup(ctx, &checkslip.LookupParams{Image: f})
```

A lookup is answered in one call, so `Lookup` does not return `OutcomePending` and `res.RetryAfter` is always zero. `RetryAfter` is deprecated and stays for callers that read it. When the bank does not show the transfer you get a 503 `UNAVAILABLE` error instead.

## What the client covers

| Service | Methods |
| --- | --- |
| `client.Slips` | `Lookup`, `Get`, `List`, `ListAll`, `Counts`, and `Exports` (`Create`, `Get`, `DownloadPart`) |
| `client.Banks` | `List` |
| `client.Billing` | `Balance.Get`, `Info` (`Get`, `Update`), `Packages.List`, `Ledger` (`List`, `ListAll`), `Orders` (`List`, `ListAll`, `Get`, `Sync`), `Receipts` (`List`, `ListAll`, `Get`) |
| `client.Usage` | `Get` |
| `client.Audit` | `Events` (`List`, `ListAll`, `Get`), `Requests` (`List`, `ListAll`, `Get`), `Checkpoints` (`List`, `ListAll`, `ListLatest`), `Chain.Check`, `Exports` (`List`, `ListAll`, `Create`, `Get`, `Download`), `SigningKeys.Get` |
| `client.Webhooks` | `Verify`, `EventTypes.List`, `Endpoints` (`List`, `ListAll`, `Create`, `Get`, `Update`, `Delete`, `Recover`, `RotateSecret`, `SendTest`), `Deliveries` (`List`, `ListAll`, `Get`, `Redeliver`) |

The same names, in the same shape, are in the JavaScript, PHP and Rust clients.

Every method takes a `context.Context` first. Use it for deadlines and cancellation.

## Errors

Any response with status 400 or above comes back as a `*checkslip.Error`. It carries the [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details the API sent.

```go
res, err := client.Slips.Lookup(ctx, params)
if err != nil {
	var apiErr *checkslip.Error
	if errors.As(err, &apiErr) {
		log.Printf("status=%d code=%s title=%q request=%s",
			apiErr.StatusCode, apiErr.Code, apiErr.Title, apiErr.RequestID)
	}

	switch {
	case checkslip.HasCode(err, checkslip.CodeInvalidQR):
		// the QR could not be read as a slip QR
	case checkslip.HasCode(err, checkslip.CodeTooOld):
		// older than the bank lets us look back
	case checkslip.IsNoCredit(err):
		// top up credits
	case checkslip.IsRateLimited(err):
		// wait apiErr.RetryAfter
	}
	return err
}
```

Quote `RequestID` when you contact Checkslip support. A QR that cannot be used (`INVALID_QR`, `QR_TAMPERED`, `TOO_OLD`, `INVALID_FILE`) returns an error and uses no credit.

The helpers are `IsRateLimited`, `IsUnauthenticated`, `IsForbidden`, `IsNotFound`, `IsNoCredit`, `IsValidation`, `IsServerError`, `HasCode` and `AsError`. A failed connection or a cancelled context is returned as a plain error, not as `*checkslip.Error`.

## Retries

The client sends a failed request again, up to 2 more times by default, waiting longer each time (from 0.5 s, doubling up to 8 s, with random jitter).

- `429 Too Many Requests` is always retried after the `Retry-After` time the API sent.
- `500`, `502`, `503`, `504` and network failures are retried only when sending the request twice cannot do the work twice: `GET`, `PUT` and `DELETE` requests, and any request with an `Idempotency-Key`.
- `409 IDEMPOTENCY_IN_FLIGHT` is retried with the same key while the first request is still running.

The client does not wait out a `Retry-After` longer than 60 seconds or longer than the time left on your context. It returns the error at once, with `RetryAfter` set.

## Idempotency

`Slips.Lookup` always sends an `Idempotency-Key`. So do the calls the API requires one for: creating an evidence file, recovering, rotating a secret, sending a test and redelivering webhooks. When you do not pass a key, the client makes a random one and reuses it on its own retries. A lookup sent again with the same key returns the first answer and is never charged twice.

Pass your own key when your program may resend the same request after a restart, for example a key built from your order number:

```go
res, err := client.Slips.Lookup(ctx, params,
	checkslip.WithIdempotencyKey("order-1001-slip-1"))
```

The key that was sent is in `res.IdempotencyKey`, and in `apiErr.IdempotencyKey` when the call failed. Keys are 1 to 255 printable ASCII characters.

## Pagination

`List` methods return one `Page`. Pass its `NextCursor` back in `ListOptions.Cursor` for the next page, or let `ListAll` do it:

```go
it := client.Slips.ListAll(ctx, &checkslip.ListSlipsParams{
	Outcome: []checkslip.Outcome{checkslip.OutcomeFound},
})
for it.Next() {
	rec := it.Current()
	fmt.Println(rec.RequestID, rec.CheckedAt)
}
if err := it.Err(); err != nil {
	return err
}
```

On Go 1.23 or later you can range over the iterator:

```go
for rec, err := range client.Slips.ListAll(ctx, nil).All() {
	if err != nil {
		return err
	}
	fmt.Println(rec.RequestID)
}
```

`it.Cursor()` gives the cursor of the next page, so a long walk can stop and pick up later.

## Webhooks

Checkslip signs webhook messages with [Standard Webhooks](https://www.standardwebhooks.com) v1. Each request has a `webhook-id`, a `webhook-timestamp` and a `webhook-signature` header. The signature is HMAC-SHA256 over `{webhook-id}.{webhook-timestamp}.{raw body}`, keyed with the endpoint secret (`whsec_...`).

Check every message before you act on it:

```go
verifier, err := checkslip.NewWebhookVerifier(os.Getenv("CHECKSLIP_WEBHOOK_SECRET"))
if err != nil {
	log.Fatal(err)
}

http.HandleFunc("/hooks/checkslip", func(w http.ResponseWriter, r *http.Request) {
	ev, err := verifier.VerifyRequest(r)
	if err != nil {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	if seenBefore(ev.ID) { // the same message can arrive more than once
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if ev.Type == checkslip.EventSlipChecked {
		data, err := ev.SlipChecked()
		if err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		markOrder(data.ClientReference, data.Outcome, data.Slip)
	}
	w.WriteHeader(http.StatusNoContent)
})
```

`VerifyRequest` reads the raw body, so do not parse the JSON before it. It refuses a message whose timestamp is more than 5 minutes from your clock (`SetTolerance` changes that). Any 2xx answer counts as delivered; other answers are retried 8 times over about 27.5 hours, and `410 Gone` turns the endpoint off.

After `RotateSecret`, the old secret keeps signing for 24 hours and each message carries both signatures. Pass both secrets while you switch:

```go
verifier, err := checkslip.NewWebhookVerifier(newSecret, oldSecret)
```

If you already have the body and headers, `verifier.Verify(header, body)` and `checkslip.VerifyWebhook(secret, header, body)` do the same check.

## Configuration

```go
client := checkslip.NewClient(apiKey,
	checkslip.WithTimeout(10*time.Second),          // per attempt; default 30 s
	checkslip.WithMaxRetries(3),                    // default 2; 0 turns retries off
	checkslip.WithUserAgent("my-shop/1.4"),         // put in front of the SDK's User-Agent
	checkslip.WithHTTPClient(httpClient),           // your own proxy, TLS or tracing
	checkslip.WithBaseURL("http://127.0.0.1:8080"), // a local mock server
)
```

`WithBaseURL` accepts `https`, and plain `http` only for loopback addresses, so the API key never crosses a network in clear text. A bad option is reported by the first request. `WithHeader` adds a header to one call, for example a W3C `traceparent`.

A `Client` is safe to share between goroutines. Create one and reuse it.

## Versioning

The SDK follows [semantic versioning](https://semver.org). Before v1.0.0, a minor release may change the API; each change is listed in [CHANGELOG.md](CHANGELOG.md).

## License

MIT. See [LICENSE](LICENSE). To report a security issue, see [SECURITY.md](SECURITY.md).
