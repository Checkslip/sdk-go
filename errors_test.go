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
