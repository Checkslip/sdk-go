package checkslip

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Webhook messages follow Standard Webhooks v1
// (https://www.standardwebhooks.com). Each request carries three headers:
//
//	webhook-id         the message ID, the same on every attempt; deduplicate on it
//	webhook-timestamp  Unix seconds of this attempt
//	webhook-signature  "v1,<base64 HMAC-SHA256>", space separated when there are several
//
// The signature is HMAC-SHA256 over "{webhook-id}.{webhook-timestamp}.{raw body}"
// with the key from the endpoint secret: the base64 text after "whsec_",
// decoded. During the 24 hours after a secret rotation each message carries
// one signature per secret.

// DefaultWebhookTolerance is how far webhook-timestamp may be from the
// receiver's clock before a message is refused, to stop replays.
const DefaultWebhookTolerance = 5 * time.Minute

// MaxWebhookBody is the largest body VerifyRequest reads.
const MaxWebhookBody = 1 << 20

// Webhook verification errors. Compare with errors.Is. Answer 400 or 401 to
// a message that fails, and do not process it.
var (
	// ErrWebhookMissingHeaders means a Standard Webhooks header is absent.
	ErrWebhookMissingHeaders = errors.New("checkslip: webhook headers missing")
	// ErrWebhookTimestamp means webhook-timestamp is unreadable or outside
	// the tolerance.
	ErrWebhookTimestamp = errors.New("checkslip: webhook timestamp outside tolerance")
	// ErrWebhookSignature means no signature matched any secret.
	ErrWebhookSignature = errors.New("checkslip: webhook signature does not match")
)

// WebhookVerifier checks the signature of webhook messages. Create it once
// with NewWebhookVerifier and reuse it; it is safe for concurrent use.
type WebhookVerifier struct {
	keys      [][]byte
	tolerance time.Duration
	now       func() time.Time
}

// NewWebhookVerifier returns a verifier for the given endpoint secrets
// ("whsec_..."). Pass two secrets while you switch to a rotated one; a
// message is accepted when any of them signed it.
func NewWebhookVerifier(secrets ...string) (*WebhookVerifier, error) {
	if len(secrets) == 0 {
		return nil, errors.New("checkslip: at least one webhook secret is required")
	}
	v := &WebhookVerifier{tolerance: DefaultWebhookTolerance, now: time.Now}
	for i, s := range secrets {
		key, err := decodeWebhookSecret(s)
		if err != nil {
			return nil, fmt.Errorf("checkslip: webhook secret %d: %w", i+1, err)
		}
		v.keys = append(v.keys, key)
	}
	return v, nil
}

func decodeWebhookSecret(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "whsec_")
	if s == "" {
		return nil, errors.New("empty secret")
	}
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("secret is not whsec_ followed by base64")
	}
	return key, nil
}

// SetTolerance changes how far webhook-timestamp may be from now. Zero or
// a negative value turns the timestamp check off, which you should only do
// in tests. It returns v so it can be chained.
func (v *WebhookVerifier) SetTolerance(d time.Duration) *WebhookVerifier {
	v.tolerance = d
	return v
}

// Verify checks a message from its headers and raw body. Pass the body
// exactly as received, before any JSON parsing.
func (v *WebhookVerifier) Verify(header http.Header, body []byte) error {
	id := header.Get("webhook-id")
	ts := header.Get("webhook-timestamp")
	sigs := header.Get("webhook-signature")
	if id == "" || ts == "" || sigs == "" {
		return ErrWebhookMissingHeaders
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if err != nil {
		return ErrWebhookTimestamp
	}
	if v.tolerance > 0 {
		diff := v.now().Sub(time.Unix(sec, 0))
		if diff > v.tolerance || diff < -v.tolerance {
			return ErrWebhookTimestamp
		}
	}

	msg := make([]byte, 0, len(id)+len(ts)+len(body)+2)
	msg = append(msg, id...)
	msg = append(msg, '.')
	msg = append(msg, strings.TrimSpace(ts)...)
	msg = append(msg, '.')
	msg = append(msg, body...)

	expected := make([][]byte, len(v.keys))
	for i, key := range v.keys {
		mac := hmac.New(sha256.New, key)
		mac.Write(msg)
		expected[i] = mac.Sum(nil)
	}
	for _, sig := range strings.Fields(sigs) {
		version, value, ok := strings.Cut(sig, ",")
		if !ok || version != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		for _, want := range expected {
			if hmac.Equal(got, want) {
				return nil
			}
		}
	}
	return ErrWebhookSignature
}

// VerifyRequest reads the body of an incoming webhook request (up to
// MaxWebhookBody bytes), checks its signature and returns the parsed event.
// The event's ID is the webhook-id header.
func (v *WebhookVerifier) VerifyRequest(r *http.Request) (*WebhookEvent, error) {
	if r.Body == nil {
		return nil, ErrWebhookSignature
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBody+1))
	if err != nil {
		return nil, fmt.Errorf("checkslip: read webhook body: %w", err)
	}
	if len(body) > MaxWebhookBody {
		return nil, fmt.Errorf("checkslip: webhook body larger than %d bytes", MaxWebhookBody)
	}
	if err := v.Verify(r.Header, body); err != nil {
		return nil, err
	}
	ev, err := ParseWebhookEvent(body)
	if err != nil {
		return nil, err
	}
	ev.ID = r.Header.Get("webhook-id")
	return ev, nil
}

// VerifyWebhook checks one message against one secret with the default
// tolerance. It is a shortcut for NewWebhookVerifier(secret) followed by
// Verify.
func VerifyWebhook(secret string, header http.Header, body []byte) error {
	v, err := NewWebhookVerifier(secret)
	if err != nil {
		return err
	}
	return v.Verify(header, body)
}

// WebhookEvent is a webhook message body. Check the signature before you
// trust it.
type WebhookEvent struct {
	// ID is the webhook-id header when the event came from VerifyRequest.
	ID   string    `json:"-"`
	Type EventType `json:"type"`
	// Timestamp is when the event happened.
	Timestamp time.Time `json:"timestamp"`
	// Data depends on Type. Decode it with SlipChecked or Test.
	Data json.RawMessage `json:"data"`
}

// ParseWebhookEvent decodes a message body. It does not check the
// signature; use WebhookVerifier.VerifyRequest for that.
func ParseWebhookEvent(body []byte) (*WebhookEvent, error) {
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, fmt.Errorf("checkslip: decode webhook: %w", err)
	}
	if ev.Type == "" {
		return nil, errors.New("checkslip: decode webhook: no type")
	}
	return &ev, nil
}

// SlipCheckedData is the data of a slip.checked message.
type SlipCheckedData struct {
	RequestID string    `json:"request_id"`
	Outcome   Outcome   `json:"outcome"`
	CheckedAt time.Time `json:"checked_at"`
	// Channel is API, DASHBOARD or ASSISTANT.
	Channel         Channel      `json:"channel"`
	ClientReference *string      `json:"client_reference"`
	Slip            *Slip        `json:"slip,omitempty"`
	DuplicateOf     *DuplicateOf `json:"duplicate_of,omitempty"`
	// ResolvedAfterPending is always false today: a lookup is answered in one
	// call, so no lookup is PENDING first. The field stays, and stays
	// required, so code that reads it keeps working. A true value used to mean
	// this lookup came back PENDING and this is its final answer.
	ResolvedAfterPending bool `json:"resolved_after_pending"`
}

// WebhookTestData is the data of a webhook.test message.
type WebhookTestData struct {
	EndpointID string `json:"endpoint_id"`
}

// SlipChecked decodes Data of a slip.checked event.
func (e *WebhookEvent) SlipChecked() (*SlipCheckedData, error) {
	if e.Type != EventSlipChecked {
		return nil, fmt.Errorf("checkslip: event type is %q, not %q", e.Type, EventSlipChecked)
	}
	var d SlipCheckedData
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return nil, fmt.Errorf("checkslip: decode slip.checked data: %w", err)
	}
	return &d, nil
}

// Test decodes Data of a webhook.test event.
func (e *WebhookEvent) Test() (*WebhookTestData, error) {
	if e.Type != EventWebhookTest {
		return nil, fmt.Errorf("checkslip: event type is %q, not %q", e.Type, EventWebhookTest)
	}
	var d WebhookTestData
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return nil, fmt.Errorf("checkslip: decode webhook.test data: %w", err)
	}
	return &d, nil
}
