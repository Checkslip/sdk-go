package checkslip

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Date is a calendar day without a time or time zone, sent as "2006-01-02".
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the calendar day of t in t's location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// ParseDate parses a day in the form "2006-01-02".
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, err
	}
	return DateOf(t), nil
}

// String returns the day as "2006-01-02".
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// IsZero reports whether d is the zero Date.
func (d Date) IsZero() bool { return d == Date{} }

// MarshalText implements encoding.TextMarshaler.
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Date) UnmarshalText(b []byte) error {
	v, err := ParseDate(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Money is an amount in the smallest unit of its currency. For THB that is
// satang: 5000 satang is 50.00 baht.
type Money struct {
	// Satang is the amount in satang (1/100 baht).
	Satang int64 `json:"satang"`
	// Currency is the ISO 4217 code, "THB" for every slip today.
	Currency string `json:"currency"`
}

// String formats the amount with two decimals and the currency, for example
// "1250.50 THB".
func (m Money) String() string {
	sign := ""
	v := m.Satang
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d %s", sign, v/100, v%100, m.Currency)
}

// Bank identifies a Thai bank.
type Bank struct {
	// Code is Checkslip's short code, for example "KBANK".
	Code string `json:"code"`
	// BOTCode is the three-digit Bank of Thailand code, for example "004".
	BOTCode string `json:"bot_code"`
	// NameTH is the bank's Thai name, when known.
	NameTH *string `json:"name_th"`
	// NameEN is the bank's English name, when known.
	NameEN *string `json:"name_en"`
}

// Party is the sender of a transfer as the bank returned it.
type Party struct {
	Bank Bank `json:"bank"`
	// NameTH and NameEN are the names the bank holds, often shortened by
	// the bank. Either may be nil.
	NameTH *string `json:"name_th"`
	NameEN *string `json:"name_en"`
	// Account is the part of the account number the bank shows, or nil.
	Account *string `json:"account"`
}

// Receiver is the receiving side of a transfer.
type Receiver struct {
	Kind ReceiverKind `json:"kind"`
	// Bank is nil for some bill payments.
	Bank    *Bank   `json:"bank"`
	NameTH  *string `json:"name_th"`
	NameEN  *string `json:"name_en"`
	Account *string `json:"account"`
}

// Slip is the transfer data returned with a lookup.
//
// A FOUND lookup carries the full slip: Sender and Receiver are set. Other
// outcomes and some history rows carry a summary: SenderBank (and maybe
// ReceiverBank) are set and Sender and Receiver are nil.
type Slip struct {
	// TransRef is the transfer reference printed on the slip.
	TransRef string `json:"trans_ref"`
	// TransferredAt is when the bank says the money moved. It is the zero
	// time on summaries that do not carry it.
	TransferredAt time.Time `json:"transferred_at"`
	// Amount is the transferred amount. It is nil on summaries that do not
	// carry it.
	Amount *Money `json:"amount,omitempty"`

	// Sender and Receiver are set on a full slip.
	Sender   *Party    `json:"sender,omitempty"`
	Receiver *Receiver `json:"receiver,omitempty"`
	// NamesErased is true when the names and masked accounts of this
	// transfer were erased together with the account's data. The name and
	// account fields are then nil because Checkslip no longer has them. A
	// nil name without NamesErased means the bank did not report it.
	NamesErased bool `json:"names_erased,omitempty"`

	// SenderBank and ReceiverBank are set on a summary.
	SenderBank   *Bank `json:"sender_bank,omitempty"`
	ReceiverBank *Bank `json:"receiver_bank,omitempty"`
}

// IsSummary reports whether s is a summary without sender and receiver
// details.
func (s *Slip) IsSummary() bool { return s != nil && s.Sender == nil }

// DuplicateOf points at the earlier lookup of the same transfer.
type DuplicateOf struct {
	RequestID string    `json:"request_id"`
	CheckedAt time.Time `json:"checked_at"`
}

// SlipCheck is the answer to Slips.Lookup.
//
// The API returns what the sending bank holds. Whether the payment is
// acceptable for your order is your decision: compare at least Amount,
// Receiver and TransferredAt with what you expected.
type SlipCheck struct {
	// RequestID identifies this lookup. Read it again with Slips.Get.
	RequestID string `json:"request_id"`
	// Outcome is FOUND, NOT_FOUND, DUPLICATE or PENDING.
	Outcome   Outcome   `json:"outcome"`
	CheckedAt time.Time `json:"checked_at"`
	// LatencyMs is how long the lookup took end to end.
	LatencyMs int `json:"latency_ms,omitempty"`
	// UpstreamMs is how long the bank took, when that is known.
	UpstreamMs *int `json:"upstream_ms,omitempty"`
	// CheckedElsewhere reports whether another account has looked this
	// transfer up before.
	CheckedElsewhere *bool `json:"checked_elsewhere,omitempty"`
	// Slip is the transfer data. It may be a summary; see Slip.
	Slip *Slip `json:"slip"`
	// DuplicateOf is set when Outcome is DUPLICATE.
	DuplicateOf *DuplicateOf `json:"duplicate_of,omitempty"`
	// ClientReference echoes the reference you sent, if any.
	ClientReference *string `json:"client_reference,omitempty"`

	// IdempotencyKey is the Idempotency-Key the request carried. Send the
	// same lookup again with this key to get this same answer.
	IdempotencyKey string `json:"-"`
	// RetryAfter is set on a PENDING answer: the API's suggested wait
	// before reading the result again with Slips.Get.
	RetryAfter time.Duration `json:"-"`
}

// SlipCheckRecord is one row of the lookup history.
type SlipCheckRecord struct {
	RequestID string    `json:"request_id"`
	Outcome   Outcome   `json:"outcome"`
	CheckedAt time.Time `json:"checked_at"`
	// APIKeyID is the key that made the lookup, or nil for lookups made in
	// the web app.
	APIKeyID *string `json:"api_key_id,omitempty"`
	// Status is the HTTP status the lookup was answered with.
	Status int `json:"status,omitempty"`
	// LatencyMs is how long the lookup took end to end.
	LatencyMs int `json:"latency_ms,omitempty"`
	// UpstreamMs is how long the bank took, when that is known.
	UpstreamMs *int `json:"upstream_ms,omitempty"`
	// CheckedElsewhere reports whether another account has looked this
	// transfer up before.
	CheckedElsewhere *bool        `json:"checked_elsewhere,omitempty"`
	Channel          Channel      `json:"channel"`
	Slip             *Slip        `json:"slip,omitempty"`
	DuplicateOf      *DuplicateOf `json:"duplicate_of,omitempty"`
	ClientReference  *string      `json:"client_reference,omitempty"`
	// Charged is true when the lookup used a credit, false when it did
	// not, and nil while that is not settled yet.
	Charged *bool `json:"charged,omitempty"`
	// ChargeConfirmed is nil until the charge is settled.
	ChargeConfirmed *bool `json:"charge_confirmed,omitempty"`
	// FreeReason says why no credit was used, when Charged is false.
	FreeReason *FreeReason `json:"free_reason,omitempty"`
}

// Page is one page of a list. NextCursor is empty on the last page.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

// HasMore reports whether another page follows.
func (p *Page[T]) HasMore() bool { return p != nil && p.NextCursor != "" }

// pageJSON decodes next_cursor, which the API sends as null on the last page.
type pageJSON[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *Page[T]) UnmarshalJSON(b []byte) error {
	var raw pageJSON[T]
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	p.Items = raw.Items
	p.NextCursor = ""
	if raw.NextCursor != nil {
		p.NextCursor = strings.TrimSpace(*raw.NextCursor)
	}
	return nil
}

// ListOptions holds the paging fields every list call accepts.
type ListOptions struct {
	// Limit is the page size, 1 to 100. Zero uses the API default of 20.
	Limit int
	// Cursor is the NextCursor of the previous page. Leave it empty for the
	// first page.
	Cursor string
}
