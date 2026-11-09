// Package checkslip is the official Go client for the Checkslip API
// (https://checkslip.com/docs).
//
// Checkslip looks up a Thai bank transfer from its slip. Send the QR text on
// the slip, or a picture of the slip, and Checkslip asks the sending bank for
// the transfer it holds and returns that data. Whether to accept the payment
// is your decision: compare the amount, receiver and time with your order.
//
// # Getting started
//
//	client := checkslip.NewClient(os.Getenv("CHECKSLIP_API_KEY"))
//	res, err := client.Slips.Lookup(ctx, &checkslip.LookupParams{QR: qrText})
//	if err != nil {
//		return err
//	}
//	switch res.Outcome {
//	case checkslip.OutcomeFound:
//		fmt.Println(res.Slip.Amount, res.Slip.TransferredAt)
//	case checkslip.OutcomePending:
//		// Read it later with client.Slips.Get(ctx, res.RequestID).
//	}
//
// # Errors
//
// Every response with status 400 or above is returned as an *Error that holds
// the problem details the API sent (RFC 9457): StatusCode, Code, Title,
// Detail and RequestID. Use errors.As, AsError, HasCode or helpers such as
// IsRateLimited and IsNoCredit to tell failures apart.
//
// # Retries
//
// The client sends a failed request again, up to WithMaxRetries times (2 by
// default), with exponential backoff and jitter:
//
//   - 429 Too Many Requests is always retried, after the Retry-After the API
//     asked for.
//   - 500, 502, 503 and 504 and network failures are retried only when the
//     request is safe to repeat: GET, PUT and DELETE, or any request that
//     carries an Idempotency-Key.
//   - 409 IDEMPOTENCY_IN_FLIGHT is retried with the same key.
//
// A Retry-After longer than 60 seconds, or longer than the time left on the
// context, is not waited out; the *Error is returned with RetryAfter set.
//
// # Idempotency
//
// Slips.Lookup and every call the API requires a key for send an
// Idempotency-Key. The client generates a random one unless you pass
// WithIdempotencyKey. A request sent again with the same key returns the
// first result, and a lookup is never charged twice.
//
// # Pagination
//
// List methods return one Page. ListAll methods return an Iter that follows
// next_cursor across pages; on Go 1.23 or later, Iter.All works with range.
//
// # Webhooks
//
// Messages follow Standard Webhooks v1. Check each one with
// WebhookVerifier.VerifyRequest before you act on it, and deduplicate on its
// webhook-id because a message can arrive more than once.
package checkslip
