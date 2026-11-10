package checkslip

import (
	"context"
	"errors"
	"time"
)

// UsageService reads lookup counts per day or month.
type UsageService struct{ c *Client }

// UsageParams is the input of UsageService.Get.
type UsageParams struct {
	// Interval is DAY (the default) or MONTH.
	Interval UsageInterval
	// From and To are the first and last day, in Thailand time. Required.
	// One request covers at most 92 days or 24 months.
	From, To Date
}

// UsageCounts splits lookups by whether they used a credit and, if not, why.
type UsageCounts struct {
	Checks          int64 `json:"checks"`
	Charged         int64 `json:"charged"`
	FreeNotFound    int64 `json:"free_not_found"`
	FreeDuplicate   int64 `json:"free_duplicate"`
	FreeUnavailable int64 `json:"free_unavailable"`
	FreeInvalidQR   int64 `json:"free_invalid_qr"`
	FreeInvalidFile int64 `json:"free_invalid_file"`
	FreeQRTampered  int64 `json:"free_qr_tampered"`
	FreeTooOld      int64 `json:"free_too_old"`
	FreeSystemError int64 `json:"free_system_error"`
	// FreeOther counts lookups that were not charged with no reason given,
	// including any reason this version does not know. It reads as 0 against a
	// server that does not send it.
	//
	// The API used to also return free_no_credit and free_hold_expired. Both
	// had been returning 0 on every account, and those lookups count here.
	FreeOther int64 `json:"free_other"`
}

// UsageBucket is the usage of one day or month.
type UsageBucket struct {
	// Start is the first day of the bucket.
	Start Date `json:"start"`
	UsageCounts
}

// Usage is a usage report.
type Usage struct {
	Interval UsageInterval `json:"interval"`
	// AsOf is when the counts were computed. They trail real time by about
	// one minute.
	AsOf time.Time `json:"as_of"`
	From Date      `json:"from"`
	To   Date      `json:"to"`
	// Timezone is always "Asia/Bangkok".
	Timezone string        `json:"timezone"`
	Buckets  []UsageBucket `json:"buckets"`
	Totals   UsageCounts   `json:"totals"`
}

// Get returns how many lookups were made, how many used a credit and how
// many did not, per day or month in Thailand time. Days or months without
// lookups are returned as zero. A lookup counts on the day its credit was
// settled, which can be the next day for a lookup near midnight. It needs
// an API key with usage:read.
func (s *UsageService) Get(ctx context.Context, params *UsageParams, opts ...RequestOption) (*Usage, error) {
	if params == nil || params.From.IsZero() || params.To.IsZero() {
		return nil, errors.New("checkslip: UsageParams.From and To are required")
	}
	q := query{}
	q.str("interval", string(params.Interval))
	q.date("from", params.From)
	q.date("to", params.To)
	var out Usage
	if _, err := s.c.doJSON(ctx, getReq("/api/usage", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
