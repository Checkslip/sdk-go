package checkslip

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	mrand "math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	retryBase = 500 * time.Millisecond
	retryCap  = 8 * time.Second
	// maxRetryAfter is the longest Retry-After the client waits on its own.
	// A longer one is returned to the caller as an *Error with RetryAfter set.
	maxRetryAfter = 60 * time.Second
)

// backoff returns the wait before retry number attempt+1: exponential from
// 500ms, capped at 8s, with jitter so many clients do not retry in step.
func backoff(attempt int) time.Duration {
	d := retryBase << uint(attempt)
	if d <= 0 || d > retryCap {
		d = retryCap
	}
	// Equal jitter: half fixed, half random.
	half := d / 2
	return half + time.Duration(mrand.Int64N(int64(half)+1))
}

// shouldRetry decides whether a failed attempt is sent again. apiErr is nil
// for network failures. repeatable is true when the request is idempotent by
// method or carries an Idempotency-Key.
func shouldRetry(apiErr *Error, repeatable bool) bool {
	if apiErr == nil {
		return repeatable
	}
	switch apiErr.StatusCode {
	case http.StatusTooManyRequests:
		// The request was turned away before it was processed.
		return true
	case http.StatusConflict:
		// The first request with this key is still running.
		return repeatable && apiErr.Code == CodeIdempotencyInFlight
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return repeatable
	}
	return false
}

// parseRetryAfter reads a Retry-After header given in seconds or as an HTTP
// date. It returns 0 when the header is missing or unreadable.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// NewIdempotencyKey returns a random UUID (version 4) to use as an
// Idempotency-Key. The client calls it for you when a method needs a key and
// you did not pass one with WithIdempotencyKey.
func NewIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; fall back to a
		// time-based value rather than panic.
		n := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(n >> (8 * (i % 8)))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
