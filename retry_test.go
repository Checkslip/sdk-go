package checkslip

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryPolicy(t *testing.T) {
	tests := []struct {
		name      string
		call      func(c *Client) error
		status    int
		code      string
		headers   []string
		wantCalls int32
	}{
		{
			name:      "GET 503 retried",
			call:      func(c *Client) error { _, err := c.Billing.Balance(context.Background()); return err },
			status:    503,
			wantCalls: 3,
		},
		{
			name:      "GET 500 retried",
			call:      func(c *Client) error { _, err := c.Billing.Balance(context.Background()); return err },
			status:    500,
			wantCalls: 3,
		},
		{
			name:      "GET 404 not retried",
			call:      func(c *Client) error { _, err := c.Billing.Balance(context.Background()); return err },
			status:    404,
			wantCalls: 1,
		},
		{
			name: "POST without key 503 not retried",
			call: func(c *Client) error {
				_, err := c.Webhooks.Endpoints.Create(context.Background(), &CreateEndpointParams{URL: "https://x.test/h", EventTypes: []EventType{EventSlipChecked}})
				return err
			},
			status:    503,
			wantCalls: 1,
		},
		{
			name: "POST without key 429 retried",
			call: func(c *Client) error {
				_, err := c.Webhooks.Endpoints.Create(context.Background(), &CreateEndpointParams{URL: "https://x.test/h", EventTypes: []EventType{EventSlipChecked}})
				return err
			},
			status:    429,
			headers:   []string{"Retry-After", "1"},
			wantCalls: 3,
		},
		{
			name: "lookup 503 retried (auto key)",
			call: func(c *Client) error {
				_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "qr"})
				return err
			},
			status:    503,
			code:      "UNAVAILABLE",
			wantCalls: 3,
		},
		{
			name: "lookup 409 in flight retried",
			call: func(c *Client) error {
				_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "qr"})
				return err
			},
			status:    409,
			code:      "IDEMPOTENCY_IN_FLIGHT",
			wantCalls: 3,
		},
		{
			name: "lookup 422 not retried",
			call: func(c *Client) error {
				_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "qr"})
				return err
			},
			status:    422,
			code:      "INVALID_QR",
			wantCalls: 1,
		},
		{
			name: "PATCH 502 not retried",
			call: func(c *Client) error {
				_, err := c.Webhooks.Endpoints.Update(context.Background(), "ep_1", &UpdateEndpointParams{})
				return err
			},
			status:    502,
			wantCalls: 1,
		},
		{
			name:      "Retry-After over limit returned at once",
			call:      func(c *Client) error { _, err := c.Billing.Balance(context.Background()); return err },
			status:    429,
			headers:   []string{"Retry-After", "3600"},
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				writeProblem(w, tt.status, tt.code, tt.headers...)
			})
			err := tt.call(c)
			if err == nil {
				t.Fatal("want error")
			}
			if e := AsError(err); e == nil || e.StatusCode != tt.status {
				t.Fatalf("got %v, want *Error with status %d", err, tt.status)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestRetryHonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	c, rec := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeProblem(w, 429, "RATE_LIMITED", "Retry-After", "7")
			return
		}
		writeJSON(w, 200, `{"credits":3}`)
	})
	b, err := c.Billing.Balance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Credits != 3 {
		t.Fatalf("credits = %d", b.Credits)
	}
	if w := rec.Waits(); len(w) != 1 || w[0] != 7*time.Second {
		t.Fatalf("waits = %v, want [7s]", w)
	}
}

func TestRetrySameIdempotencyKey(t *testing.T) {
	var keys []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if len(keys) < 3 {
			writeProblem(w, 503, "UNAVAILABLE")
			return
		}
		writeJSON(w, 200, `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","slip":{"trans_ref":"630210534087"}}`)
	})
	res, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "qr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[0] == "" || keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("keys = %q, want one key sent 3 times", keys)
	}
	if res.IdempotencyKey != keys[0] {
		t.Fatalf("IdempotencyKey = %q, want %q", res.IdempotencyKey, keys[0])
	}
}

func TestNetworkErrorRetry(t *testing.T) {
	tests := []struct {
		name      string
		call      func(c *Client) error
		wantCalls int32
	}{
		{"GET retried", func(c *Client) error { _, err := c.Billing.Balance(context.Background()); return err }, 3},
		{"lookup retried", func(c *Client) error {
			_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "q"})
			return err
		}, 3},
		{"create endpoint not retried", func(c *Client) error {
			_, err := c.Webhooks.Endpoints.Create(context.Background(), &CreateEndpointParams{URL: "https://x.test", EventTypes: []EventType{EventSlipChecked}})
			return err
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Error("no hijacker")
					return
				}
				conn, _, _ := hj.Hijack()
				_ = conn.Close() // drop the connection without an answer
			})
			err := tt.call(c)
			if err == nil {
				t.Fatal("want error")
			}
			if AsError(err) != nil {
				t.Fatalf("network failure came back as *Error: %v", err)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestMaxRetriesZero(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeProblem(w, 503, "INTERNAL")
	}, WithMaxRetries(0))
	_, _ = c.Billing.Balance(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestRetryStopsBeforeContextDeadline(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeProblem(w, 429, "RATE_LIMITED", "Retry-After", "30")
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.Billing.Balance(ctx)
	if !IsRateLimited(err) {
		t.Fatalf("got %v, want rate limited error", err)
	}
	if e := AsError(err); e.RetryAfter != 30*time.Second {
		t.Fatalf("RetryAfter = %v", e.RetryAfter)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestBackoffBounds(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		d := retryBase << uint(attempt)
		if d > retryCap {
			d = retryCap
		}
		for i := 0; i < 50; i++ {
			got := backoff(attempt)
			if got < d/2 || got > d {
				t.Fatalf("backoff(%d) = %v, want in [%v, %v]", attempt, got, d/2, d)
			}
		}
	}
	if got := backoff(100); got > retryCap {
		t.Fatalf("backoff(100) = %v over cap", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{" 12 ", 12 * time.Second},
		{"-1", 0},
		{"soon", 0},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestSleepCtxCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
