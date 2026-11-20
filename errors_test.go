package checkslip

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestErrorFromProblem(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("X-Request-Id", "0192f3a4-7b1c-7d2e-8f30-4a5b6c7d8e9f")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"https://checkslip.com/problems/validation-failed","title":"ข้อมูลที่ส่งมาไม่ถูกต้อง","status":400,"detail":"qr is empty","instance":"/api/slips/lookup","code":"VALIDATION_FAILED","errors":[{"pointer":"/qr","code":"REQUIRED"}]}`))
	})
	_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "x"}, WithIdempotencyKey("order-42"))
	var apiErr *Error
	if !errors.As(fmt.Errorf("wrapped: %w", err), &apiErr) {
		t.Fatalf("errors.As failed on %v", err)
	}
	want := Error{
		StatusCode:     400,
		Code:           CodeValidationFailed,
		Type:           "https://checkslip.com/problems/validation-failed",
		Title:          "ข้อมูลที่ส่งมาไม่ถูกต้อง",
		Detail:         "qr is empty",
		Instance:       "/api/slips/lookup",
		RequestID:      "0192f3a4-7b1c-7d2e-8f30-4a5b6c7d8e9f",
		IdempotencyKey: "order-42",
	}
	got := *apiErr
	got.Body, got.Fields = nil, nil
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if len(apiErr.Fields) != 1 || apiErr.Fields[0].Pointer != "/qr" {
		t.Fatalf("Fields = %+v", apiErr.Fields)
	}
	msg := apiErr.Error()
	for _, part := range []string{"400", "VALIDATION_FAILED", "qr is empty", "0192f3a4"} {
		if !strings.Contains(msg, part) {
			t.Errorf("Error() = %q, missing %q", msg, part)
		}
	}
}

func TestErrorNonJSONBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<h1>nope</h1>"))
	})
	_, err := c.Billing.Balance.Get(context.Background())
	e := AsError(err)
	if e == nil || e.StatusCode != http.StatusNotFound || e.Code != "" || e.Title != "Not Found" || string(e.Body) != "<h1>nope</h1>" {
		t.Fatalf("got %+v", e)
	}
	if !IsNotFound(err) {
		t.Fatal("IsNotFound = false")
	}
}

func TestErrorHelpers(t *testing.T) {
	mk := func(status int, code ErrorCode) error {
		return fmt.Errorf("ctx: %w", &Error{StatusCode: status, Code: code})
	}
	tests := []struct {
		name string
		fn   func(error) bool
		err  error
		want bool
	}{
		{"rate limited", IsRateLimited, mk(429, CodeRateLimited), true},
		{"rate limited no", IsRateLimited, mk(503, CodeUnavailable), false},
		{"unauthenticated", IsUnauthenticated, mk(401, CodeUnauthenticated), true},
		{"forbidden", IsForbidden, mk(403, CodeForbidden), true},
		{"not found", IsNotFound, mk(404, CodeEndpointNotFound), true},
		{"no credit", IsNoCredit, mk(402, CodeNoCredit), true},
		{"credit expired", IsNoCredit, mk(402, CodeCreditExpired), true},
		{"no credit no", IsNoCredit, mk(422, CodeInvalidQR), false},
		{"validation 400", IsValidation, mk(400, CodeValidationFailed), true},
		{"validation 422", IsValidation, mk(422, CodeInvalidQR), true},
		{"server", IsServerError, mk(502, ""), true},
		{"server no", IsServerError, mk(499, ""), false},
		{"plain error", IsRateLimited, errors.New("boom"), false},
		{"nil", IsNotFound, nil, false},
		{"has code", func(err error) bool { return HasCode(err, CodeTooOld) }, mk(422, CodeTooOld), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(tt.err); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorRetryAfterParsed(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, 503, "UNAVAILABLE", "Retry-After", "4")
	}, WithMaxRetries(0))
	_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "x"})
	if e := AsError(err); e == nil || e.RetryAfter != 4*time.Second || e.IdempotencyKey == "" {
		t.Fatalf("got %+v", e)
	}
}

// The reason on a 503 UNAVAILABLE (verify contract 0.80.2). It says why the
// lookup has no definite answer, and the Retry-After differs with it.
func TestUnavailableReason(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reason     string
		retryAfter string
		want       UnavailableReason
		wantWait   time.Duration
	}{
		{"not seen", `,"reason":"NOT_SEEN"`, "60", ReasonNotSeen, 60 * time.Second},
		{"source down", `,"reason":"SOURCE_DOWN"`, "30", ReasonSourceDown, 30 * time.Second},
		// An open set: a word added after this version of the client is kept
		// as it was sent, so the caller can log it and fall back.
		{"a reason this version does not know", `,"reason":"BANK_REFUSED_US"`, "30", UnavailableReason("BANK_REFUSED_US"), 30 * time.Second},
		// Our own failure carries no reason at all.
		{"no reason", "", "30", "", 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.Header().Set("X-Request-Id", "req-503")
				w.Header().Set("Retry-After", tc.retryAfter)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"type":"https://checkslip.com/problems/unavailable","title":"ติดต่อไม่ได้","status":503,"instance":"/api/slips/lookup","code":"UNAVAILABLE"` + tc.reason + `}`))
			}, WithMaxRetries(0))

			_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: contractTestQR})
			e := AsError(err)
			if e == nil {
				t.Fatalf("want *Error, got %v", err)
			}
			if e.Code != CodeUnavailable {
				t.Errorf("Code = %q, want UNAVAILABLE", e.Code)
			}
			if e.Reason != tc.want {
				t.Errorf("Reason = %q, want %q", e.Reason, tc.want)
			}
			if e.RetryAfter != tc.wantWait {
				t.Errorf("RetryAfter = %v, want %v", e.RetryAfter, tc.wantWait)
			}
		})
	}
}

// Every other error leaves Reason empty, so a caller cannot read a stale
// reason off an error that has nothing to do with a lookup.
func TestReasonEmptyOnOtherErrors(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusUnprocessableEntity, "INVALID_QR")
	}, WithMaxRetries(0))

	_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: contractTestQR})
	if e := AsError(err); e == nil || e.Reason != "" {
		t.Fatalf("Reason = %+v, want empty", e)
	}
}

// What the README promises about the wait: Lookup always sends an
// Idempotency-Key, so the client itself waits out the Retry-After and asks
// again twice before the caller sees NOT_SEEN.
func TestUnavailableIsRetriedWithTheReasonsRetryAfter(t *testing.T) {
	c, rec := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"type":"https://checkslip.com/problems/unavailable","title":"ติดต่อไม่ได้","status":503,"instance":"/api/slips/lookup","code":"UNAVAILABLE","reason":"NOT_SEEN"}`))
	})

	_, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: contractTestQR})
	if e := AsError(err); e == nil || e.Reason != ReasonNotSeen {
		t.Fatalf("want NOT_SEEN after the retries, got %v", err)
	}
	want := []time.Duration{60 * time.Second, 60 * time.Second}
	if got := rec.Waits(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
}
