package checkslip

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// ErrorCode is the machine-readable code of an API error, such as
// "RATE_LIMITED". Compare it with the Code constants. New codes may appear
// over time, so treat an unknown code like a generic failure of its status.
type ErrorCode string

// Error codes the Checkslip API returns. The list covers the codes a
// customer is likely to handle; Error.Code can hold others.
const (
	CodeValidationFailed       ErrorCode = "VALIDATION_FAILED"
	CodeInvalidCursor          ErrorCode = "INVALID_CURSOR"
	CodeUnauthenticated        ErrorCode = "UNAUTHENTICATED"
	CodeForbidden              ErrorCode = "FORBIDDEN"
	CodeRateLimited            ErrorCode = "RATE_LIMITED"
	CodeInternal               ErrorCode = "INTERNAL"
	CodeIdempotencyKeyRequired ErrorCode = "IDEMPOTENCY_KEY_REQUIRED"
	CodeIdempotencyKeyInvalid  ErrorCode = "IDEMPOTENCY_KEY_INVALID"
	CodeIdempotencyKeyReused   ErrorCode = "IDEMPOTENCY_KEY_REUSED"
	CodeIdempotencyInFlight    ErrorCode = "IDEMPOTENCY_IN_FLIGHT"

	// Slip lookup.
	CodeNoCredit             ErrorCode = "NO_CREDIT"
	CodeCreditExpired        ErrorCode = "CREDIT_EXPIRED"
	CodeCreditUnavailable    ErrorCode = "CREDIT_UNAVAILABLE"
	CodeInvalidQR            ErrorCode = "INVALID_QR"
	CodeQRTampered           ErrorCode = "QR_TAMPERED"
	CodeTooOld               ErrorCode = "TOO_OLD"
	CodeInvalidFile          ErrorCode = "INVALID_FILE"
	CodeMultipleSlipQR       ErrorCode = "MULTIPLE_SLIP_QR"
	CodePayloadTooLarge      ErrorCode = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMediaType ErrorCode = "UNSUPPORTED_MEDIA_TYPE"
	CodeUnavailable          ErrorCode = "UNAVAILABLE"

	// Billing.
	CodeOrderNotFound       ErrorCode = "ORDER_NOT_FOUND"
	CodeReceiptNotFound     ErrorCode = "RECEIPT_NOT_FOUND"
	CodePaymentsUnavailable ErrorCode = "PAYMENTS_UNAVAILABLE"

	// Webhooks.
	CodeEndpointNotFound         ErrorCode = "ENDPOINT_NOT_FOUND"
	CodeDeliveryNotFound         ErrorCode = "DELIVERY_NOT_FOUND"
	CodeEndpointLimitReached     ErrorCode = "ENDPOINT_LIMIT_REACHED"
	CodeEndpointDisabled         ErrorCode = "ENDPOINT_DISABLED"
	CodeEndpointDeleted          ErrorCode = "ENDPOINT_DELETED"
	CodeRotationInProgress       ErrorCode = "ROTATION_IN_PROGRESS"
	CodePayloadExpired           ErrorCode = "PAYLOAD_EXPIRED"
	CodeURLNotHTTPS              ErrorCode = "URL_NOT_HTTPS"
	CodeURLHasUserinfo           ErrorCode = "URL_HAS_USERINFO"
	CodeDestinationBlocked       ErrorCode = "DESTINATION_BLOCKED"
	CodeDNSUnresolvable          ErrorCode = "DNS_UNRESOLVABLE"
	CodeUnknownEventType         ErrorCode = "UNKNOWN_EVENT_TYPE"
	CodeRecoverWindowInvalid     ErrorCode = "RECOVER_WINDOW_INVALID"
	CodePayloadIntegrityMismatch ErrorCode = "PAYLOAD_INTEGRITY_MISMATCH"
)

// Error is returned for every API response with status 400 or above. The
// API sends errors as RFC 9457 problem details; Error holds their fields.
//
// Use errors.As to reach it:
//
//	var apiErr *checkslip.Error
//	if errors.As(err, &apiErr) {
//		log.Println(apiErr.StatusCode, apiErr.Code, apiErr.RequestID)
//	}
type Error struct {
	// StatusCode is the HTTP status, for example 422.
	StatusCode int
	// Code is the machine-readable error code, for example CodeInvalidQR.
	Code ErrorCode
	// Type is the problem type URI, a page that explains the error.
	Type string
	// Title is a short summary written for people. It may be in Thai.
	Title string
	// Detail explains this occurrence, when the API sends one.
	Detail string
	// Instance identifies this occurrence.
	Instance string
	// RequestID is the X-Request-Id of the response. Quote it when you
	// contact Checkslip support.
	RequestID string
	// RetryAfter is the wait the API asked for in its Retry-After header,
	// or zero.
	RetryAfter time.Duration
	// IdempotencyKey is the key the failed request carried, if any. Send
	// the request again with the same key (WithIdempotencyKey) to get the
	// first result instead of a second one.
	IdempotencyKey string
	// Fields lists the request fields that failed validation.
	Fields []FieldError
	// Body is the raw response body, kept for responses that are not
	// problem details.
	Body []byte
}

// FieldError points at one request field that failed validation.
type FieldError struct {
	// Pointer is a JSON Pointer to the field, for example "/qr".
	Pointer string `json:"pointer"`
	// Code says what is wrong with the field.
	Code string `json:"code"`
	// Detail explains it for people, when sent.
	Detail string `json:"detail,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "checkslip: %d", e.StatusCode)
	if e.Code != "" {
		b.WriteString(" " + string(e.Code))
	}
	if e.Title != "" {
		b.WriteString(": " + e.Title)
	}
	if e.Detail != "" {
		b.WriteString(" (" + e.Detail + ")")
	}
	if e.RequestID != "" {
		b.WriteString(" [request " + e.RequestID + "]")
	}
	return b.String()
}

type problemBody struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail"`
	Instance string       `json:"instance"`
	Code     ErrorCode    `json:"code"`
	Errors   []FieldError `json:"errors"`
}

const maxErrorBody = 64 << 10

// readError builds an *Error from a failed response and closes its body.
func readError(resp *http.Response, key string) *Error {
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	_, _ = io.Copy(io.Discard, resp.Body)
	e := &Error{
		StatusCode:     resp.StatusCode,
		RequestID:      resp.Header.Get("X-Request-Id"),
		RetryAfter:     parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		IdempotencyKey: key,
		Body:           raw,
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mt == "application/problem+json" || mt == "application/json" {
		var p problemBody
		if json.Unmarshal(raw, &p) == nil {
			e.Type, e.Title, e.Detail, e.Instance = p.Type, p.Title, p.Detail, p.Instance
			e.Code, e.Fields = p.Code, p.Errors
		}
	}
	if e.Title == "" {
		e.Title = http.StatusText(resp.StatusCode)
	}
	return e
}

// AsError returns the *Error inside err, or nil when err did not come from
// an API response.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// HasCode reports whether err is an API error with the given code.
func HasCode(err error, code ErrorCode) bool {
	e := AsError(err)
	return e != nil && e.Code == code
}

func hasStatus(err error, status int) bool {
	e := AsError(err)
	return e != nil && e.StatusCode == status
}

// IsRateLimited reports whether the API answered 429 Too Many Requests. The
// error's RetryAfter says how long to wait.
func IsRateLimited(err error) bool { return hasStatus(err, http.StatusTooManyRequests) }

// IsUnauthenticated reports whether the API key was missing, wrong or
// revoked (401).
func IsUnauthenticated(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsForbidden reports whether the API key lacks the permission for the call
// (403).
func IsForbidden(err error) bool { return hasStatus(err, http.StatusForbidden) }

// IsNotFound reports whether the requested record does not exist for this
// account (404).
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsNoCredit reports whether a lookup was refused because the account has no
// credit left or its credit expired (402).
func IsNoCredit(err error) bool {
	return HasCode(err, CodeNoCredit) || HasCode(err, CodeCreditExpired)
}

// IsValidation reports whether the request was rejected as malformed (400 or
// 422). Check Error.Code and Error.Fields for the reason.
func IsValidation(err error) bool {
	return hasStatus(err, http.StatusBadRequest) || hasStatus(err, http.StatusUnprocessableEntity)
}

// IsServerError reports whether the API failed on its side (status 500 or
// above). The client already retried it when that was safe.
func IsServerError(err error) bool {
	e := AsError(err)
	return e != nil && e.StatusCode >= 500
}
