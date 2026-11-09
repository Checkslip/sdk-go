package checkslip

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Test vector published with the Standard Webhooks specification.
const (
	swSecret    = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	swID        = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	swTimestamp = 1614265330
	swBody      = `{"test": 2432232314}`
	swSignature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

func sign(secret, id string, ts int64, body string) string {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + strconv.FormatInt(ts, 10) + "." + body))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func headers(id string, ts int64, sig string) http.Header {
	h := http.Header{}
	if id != "" {
		h.Set("webhook-id", id)
	}
	if ts != 0 {
		h.Set("webhook-timestamp", strconv.FormatInt(ts, 10))
	}
	if sig != "" {
		h.Set("webhook-signature", sig)
	}
	return h
}

func fixedVerifier(t *testing.T, at time.Time, secrets ...string) *WebhookVerifier {
	t.Helper()
	v, err := NewWebhookVerifier(secrets...)
	if err != nil {
		t.Fatal(err)
	}
	v.now = func() time.Time { return at }
	return v
}

func TestStandardWebhooksVector(t *testing.T) {
	if got := sign(swSecret, swID, swTimestamp, swBody); got != swSignature {
		t.Fatalf("helper signature %s, want %s", got, swSignature)
	}
	v := fixedVerifier(t, time.Unix(swTimestamp, 0), swSecret)
	if err := v.Verify(headers(swID, swTimestamp, swSignature), []byte(swBody)); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookVerify(t *testing.T) {
	const newSecret = "whsec_" + "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	now := time.Unix(swTimestamp, 0)
	good := sign(swSecret, swID, swTimestamp, swBody)
	other := sign(newSecret, swID, swTimestamp, swBody)
	tests := []struct {
		name    string
		secrets []string
		hdr     http.Header
		body    string
		want    error
	}{
		{"valid", []string{swSecret}, headers(swID, swTimestamp, good), swBody, nil},
		{"rotation: two signatures, old secret", []string{swSecret}, headers(swID, swTimestamp, other+" "+good), swBody, nil},
		{"rotation: verifier holds both", []string{newSecret, swSecret}, headers(swID, swTimestamp, good), swBody, nil},
		{"unknown version ignored", []string{swSecret}, headers(swID, swTimestamp, "v2,abc "+good), swBody, nil},
		{"wrong secret", []string{newSecret}, headers(swID, swTimestamp, good), swBody, ErrWebhookSignature},
		{"body changed", []string{swSecret}, headers(swID, swTimestamp, good), `{"test": 1}`, ErrWebhookSignature},
		{"id changed", []string{swSecret}, headers("msg_other", swTimestamp, good), swBody, ErrWebhookSignature},
		{"garbage signature", []string{swSecret}, headers(swID, swTimestamp, "v1,!!!"), swBody, ErrWebhookSignature},
		{"too old", []string{swSecret}, headers(swID, swTimestamp-301, sign(swSecret, swID, swTimestamp-301, swBody)), swBody, ErrWebhookTimestamp},
		{"too new", []string{swSecret}, headers(swID, swTimestamp+301, sign(swSecret, swID, swTimestamp+301, swBody)), swBody, ErrWebhookTimestamp},
		{"missing id", []string{swSecret}, headers("", swTimestamp, good), swBody, ErrWebhookMissingHeaders},
		{"missing signature", []string{swSecret}, headers(swID, swTimestamp, ""), swBody, ErrWebhookMissingHeaders},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := fixedVerifier(t, now, tt.secrets...)
			err := v.Verify(tt.hdr, []byte(tt.body))
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestWebhookTimestampUnreadable(t *testing.T) {
	v := fixedVerifier(t, time.Unix(swTimestamp, 0), swSecret)
	h := headers(swID, 0, swSignature)
	h.Set("webhook-timestamp", "yesterday")
	if err := v.Verify(h, []byte(swBody)); !errors.Is(err, ErrWebhookTimestamp) {
		t.Fatalf("got %v", err)
	}
	// Tolerance off: an old vector verifies on today's clock.
	v2, _ := NewWebhookVerifier(swSecret)
	if err := v2.SetTolerance(0).Verify(headers(swID, swTimestamp, swSignature), []byte(swBody)); err != nil {
		t.Fatal(err)
	}
}

func TestNewWebhookVerifierErrors(t *testing.T) {
	for _, s := range [][]string{nil, {""}, {"whsec_"}, {"whsec_not base64!"}} {
		if _, err := NewWebhookVerifier(s...); err == nil {
			t.Errorf("NewWebhookVerifier(%q) accepted", s)
		}
	}
	if err := VerifyWebhook("whsec_%%", http.Header{}, nil); err == nil {
		t.Error("VerifyWebhook accepted a bad secret")
	}
}

func TestVerifyRequestSlipChecked(t *testing.T) {
	body := `{"type":"slip.checked","timestamp":"2026-10-29T10:41:03+07:00","data":{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","channel":"API","client_reference":"order-9","checked_elsewhere":false,"resolved_after_pending":true,"slip":{"trans_ref":"630210534087","transferred_at":"2026-10-29T10:33:30+07:00","amount":{"satang":125050,"currency":"THB"},"sender":{"bank":{"code":"KKP","bot_code":"069","name_th":null,"name_en":null},"name_th":null,"name_en":null,"account":null},"receiver":{"kind":"BILL_PAYMENT","bank":null,"name_th":"บริษัท ตัวอย่าง จำกัด","name_en":null,"account":null}}}}`
	ts := time.Now().Unix()
	req := httptest.NewRequest(http.MethodPost, "/hooks/checkslip", strings.NewReader(body))
	req.Header = headers("msg_1", ts, sign(swSecret, "msg_1", ts, body))

	v, _ := NewWebhookVerifier(swSecret)
	ev, err := v.VerifyRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "msg_1" || ev.Type != EventSlipChecked {
		t.Fatalf("event %+v", ev)
	}
	d, err := ev.SlipChecked()
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeFound || !d.ResolvedAfterPending || d.Slip.Amount.String() != "1250.50 THB" || d.Slip.Receiver.Bank != nil {
		t.Fatalf("data %+v", d)
	}
	if _, err := ev.Test(); err == nil {
		t.Error("Test() on slip.checked should fail")
	}
}

func TestVerifyRequestRejects(t *testing.T) {
	v, _ := NewWebhookVerifier(swSecret)
	big := strings.Repeat("a", MaxWebhookBody+1)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
	if _, err := v.VerifyRequest(req); err == nil {
		t.Error("oversized body accepted")
	}
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"webhook.test"}`))
	req.Header = headers("m", time.Now().Unix(), "v1,AAAA")
	if _, err := v.VerifyRequest(req); !errors.Is(err, ErrWebhookSignature) {
		t.Errorf("got %v", err)
	}
}

func TestParseWebhookEventTest(t *testing.T) {
	ev, err := ParseWebhookEvent([]byte(`{"type":"webhook.test","timestamp":"2026-11-01T00:00:00Z","data":{"endpoint_id":"ep_1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	d, err := ev.Test()
	if err != nil || d.EndpointID != "ep_1" {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := ev.SlipChecked(); err == nil {
		t.Error("SlipChecked() on webhook.test should fail")
	}
	for _, bad := range []string{`not json`, `{}`} {
		if _, err := ParseWebhookEvent([]byte(bad)); err == nil {
			t.Errorf("ParseWebhookEvent(%q) accepted", bad)
		}
	}
}
