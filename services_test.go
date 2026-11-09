package checkslip

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// route is one expected call to the fake API.
type route struct {
	method, path string
	status       int
	body         string
	check        func(t *testing.T, r *http.Request, body []byte)
}

func routes(t *testing.T, rs ...route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, rt := range rs {
			if r.Method == rt.method && r.URL.EscapedPath() == rt.path {
				b, _ := io.ReadAll(r.Body)
				if rt.check != nil {
					rt.check(t, r, b)
				}
				if rt.status == http.StatusNoContent {
					w.WriteHeader(rt.status)
					return
				}
				writeJSON(w, rt.status, rt.body)
				return
			}
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.EscapedPath())
		writeProblem(w, 404, "NOT_FOUND")
	}
}

func wantKey(t *testing.T, r *http.Request, _ []byte) {
	t.Helper()
	if r.Header.Get("Idempotency-Key") == "" {
		t.Errorf("%s %s sent no Idempotency-Key", r.Method, r.URL.Path)
	}
}

func TestBillingUsageBanks(t *testing.T) {
	c, _ := newTestClient(t, routes(t,
		route{method: "GET", path: "/api/billing/balance", status: 200,
			body: `{"account_id":"a","credits":120,"low":true,"threshold":150,"threshold_basis":"AVG_7_DAYS","expires_at":null,"updated_at":"2026-11-01T00:00:00Z","last_entry_id":null}`},
		route{method: "PUT", path: "/api/billing/billing-info", status: 200, body: `{"buyer_type":"COMPANY","name":"ACME","tax_id":"0105555555555"}`,
			check: func(t *testing.T, r *http.Request, b []byte) {
				if !strings.Contains(string(b), `"tax_id":"0105555555555"`) {
					t.Errorf("body %s", b)
				}
			}},
		route{method: "GET", path: "/api/billing/ledger", status: 200,
			body: `{"items":[{"id":"e1","delta":-1,"balance_after":119,"reason":"SLIP_CHARGE","created_at":"2026-11-01T00:00:00Z"}],"next_cursor":null}`,
			check: func(t *testing.T, r *http.Request, _ []byte) {
				if r.URL.Query().Get("reason") != "PURCHASE,EXPIRE" {
					t.Errorf("reason %q", r.URL.Query().Get("reason"))
				}
			}},
		route{method: "GET", path: "/api/billing/orders/o1", status: 200, body: `{"id":"o1","status":"PAID","package_code":"P1","package_name_th":"x","credits":100,"amount":{"satang":19900,"currency":"THB"},"created_at":"2026-11-01T00:00:00Z"}`},
		route{method: "POST", path: "/api/billing/orders/o1/sync", status: 200, body: `{"id":"o1","status":"PAID","package_code":"P1","package_name_th":"x","credits":100,"amount":{"satang":19900,"currency":"THB"},"created_at":"2026-11-01T00:00:00Z"}`},
		route{method: "GET", path: "/api/billing/packages", status: 200, body: `{"checkout_open":true,"prices_include_vat":true,"items":[{"code":"P1","credits":100,"price":{"satang":19900,"currency":"THB"}}]}`},
		route{method: "GET", path: "/api/billing/receipts/rc1", status: 200, body: `{"id":"rc1","receipt_no":"RC-202611-0001","seller":{"name":"S"},"vat":{"rate_bp":700,"base":{"satang":100,"currency":"THB"},"amount":{"satang":7,"currency":"THB"}},"lines":[{"description":"d","quantity":1,"amount":{"satang":107,"currency":"THB"}}],"total":{"satang":107,"currency":"THB"},"payment_method":"CARD"}`},
		route{method: "GET", path: "/api/usage", status: 200,
			body: `{"interval":"DAY","as_of":"2026-11-02T00:00:00Z","from":"2026-11-01","to":"2026-11-02","timezone":"Asia/Bangkok","buckets":[{"start":"2026-11-01","checks":5,"charged":3,"free_not_found":2}],"totals":{"checks":5,"charged":3}}`,
			check: func(t *testing.T, r *http.Request, _ []byte) {
				q := r.URL.Query()
				if q.Get("from") != "2026-11-01" || q.Get("to") != "2026-11-02" || q.Get("interval") != "DAY" {
					t.Errorf("usage query %v", q)
				}
			}},
		route{method: "GET", path: "/api/banks", status: 200, body: `{"items":[{"code":"KBANK","bot_code":"004","name_th":"กสิกรไทย","name_en":"Kasikornbank","supported_since":"2026-10-01","landing_page":true,"lookback_days":30}]}`},
	))
	ctx := context.Background()

	b, err := c.Billing.Balance.Get(ctx)
	if err != nil || b.Credits != 120 || !b.Low || b.ExpiresAt != nil || b.Basis != ThresholdAvg7Days {
		t.Fatalf("balance %+v %v", b, err)
	}
	info, err := c.Billing.Info.Update(ctx, &BillingInfo{BuyerType: BuyerCompany, Name: "ACME", TaxID: "0105555555555"})
	if err != nil || info.Name != "ACME" {
		t.Fatalf("info %+v %v", info, err)
	}
	led, err := c.Billing.Ledger.List(ctx, &LedgerParams{Reason: []CreditReason{CreditPurchase, CreditExpire}})
	if err != nil || len(led.Items) != 1 || led.Items[0].Delta != -1 {
		t.Fatalf("ledger %+v %v", led, err)
	}
	o, err := c.Billing.Orders.Get(ctx, "o1")
	if err != nil || o.Amount.String() != "199.00 THB" {
		t.Fatalf("order %+v %v", o, err)
	}
	if _, err := c.Billing.Orders.Sync(ctx, "o1"); err != nil {
		t.Fatal(err)
	}
	pk, err := c.Billing.Packages.List(ctx)
	if err != nil || !pk.CheckoutOpen || pk.Items[0].Credits != 100 {
		t.Fatalf("packages %+v %v", pk, err)
	}
	rc, err := c.Billing.Receipts.Get(ctx, "rc1")
	if err != nil || rc.VAT.RateBP != 700 || rc.PaymentMethod != PaymentCard {
		t.Fatalf("receipt %+v %v", rc, err)
	}
	u, err := c.Usage.Get(ctx, &UsageParams{Interval: UsageDay, From: Date{2026, time.November, 1}, To: Date{2026, time.November, 2}})
	if err != nil || u.Buckets[0].Start.String() != "2026-11-01" || u.Buckets[0].FreeNotFound != 2 || u.Totals.Charged != 3 {
		t.Fatalf("usage %+v %v", u, err)
	}
	banks, err := c.Banks.List(ctx)
	if err != nil || len(banks) != 1 || banks[0].LookbackDays != 30 || banks[0].SupportedSince != (Date{2026, time.October, 1}) {
		t.Fatalf("banks %+v %v", banks, err)
	}
}

func TestWebhookEndpointsAndDeliveries(t *testing.T) {
	const ep = `{"id":"ep_1","url":"https://shop.test/hooks","description":null,"event_types":["slip.checked"],"enabled":true,"disabled_reason":null,"secret_hint":"…a1b2","created_at":"2026-11-01T00:00:00Z","updated_at":"2026-11-01T00:00:00Z"}`
	const dl = `{"id":"dl_1","message_id":"msg_1","endpoint_id":"ep_1","event_type":"webhook.test","status":"PENDING","attempt_count":0,"next_attempt_at":"2026-11-01T00:00:00Z","last_attempt_at":null,"last_http_status":null,"created_at":"2026-11-01T00:00:00Z"}`
	c, _ := newTestClient(t, routes(t,
		route{method: "POST", path: "/api/webhooks/endpoints", status: 201,
			body: strings.TrimSuffix(ep, "}") + `,"secret":"whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"}`},
		route{method: "GET", path: "/api/webhooks/endpoints", status: 200, body: `{"items":[` + ep + `],"next_cursor":null}`},
		route{method: "PATCH", path: "/api/webhooks/endpoints/ep_1", status: 200, body: ep,
			check: func(t *testing.T, r *http.Request, b []byte) {
				if r.Header.Get("Content-Type") != "application/merge-patch+json" {
					t.Errorf("Content-Type %q", r.Header.Get("Content-Type"))
				}
				var m map[string]any
				_ = json.Unmarshal(b, &m)
				if v, ok := m["description"]; !ok || v != nil {
					t.Errorf("description not cleared: %s", b)
				}
				if m["enabled"] != false || len(m) != 2 {
					t.Errorf("patch body %s", b)
				}
			}},
		route{method: "DELETE", path: "/api/webhooks/endpoints/ep_1", status: 204},
		route{method: "POST", path: "/api/webhooks/endpoints/ep_1/recover", status: 202,
			body: `{"endpoint_id":"ep_1","since":"2026-11-01T00:00:00Z","until":"2026-11-02T00:00:00Z"}`,
			check: func(t *testing.T, r *http.Request, b []byte) {
				wantKey(t, r, b)
				if strings.Contains(string(b), "until") {
					t.Errorf("zero Until sent: %s", b)
				}
			}},
		route{method: "POST", path: "/api/webhooks/endpoints/ep_1/secret/rotate", status: 200,
			body: `{"secret":"whsec_x","previous_secret_expires_at":"2026-11-02T00:00:00Z"}`, check: wantKey},
		route{method: "POST", path: "/api/webhooks/endpoints/ep_1/test", status: 202, body: dl, check: wantKey},
		route{method: "GET", path: "/api/webhooks/deliveries/dl_1", status: 200,
			body: strings.TrimSuffix(dl, "}") + `,"payload":{"type":"webhook.test","timestamp":"2026-11-01T00:00:00Z","data":{"endpoint_id":"ep_1"}},"payload_expires_at":"2026-12-01T00:00:00Z","attempts":[{"attempt_no":1,"attempted_at":"2026-11-01T00:00:01Z","outcome":"HTTP_ERROR","http_status":500,"duration_ms":120,"response_excerpt":"oops"}]}`},
		route{method: "POST", path: "/api/webhooks/deliveries/dl_1/redeliver", status: 202, body: dl, check: wantKey},
		route{method: "GET", path: "/api/webhooks/deliveries", status: 200, body: `{"items":[` + dl + `],"next_cursor":null}`,
			check: func(t *testing.T, r *http.Request, _ []byte) {
				if r.URL.Query().Get("status") != "FAILED" || r.URL.Query().Get("endpoint_id") != "ep_1" {
					t.Errorf("deliveries query %v", r.URL.Query())
				}
			}},
		route{method: "GET", path: "/api/webhooks/event-types", status: 200, body: `{"items":[{"type":"slip.checked","description":"d","subscribable":true}]}`},
	))
	ctx := context.Background()

	created, err := c.Webhooks.Endpoints.Create(ctx, &CreateEndpointParams{URL: "https://shop.test/hooks", EventTypes: []EventType{EventSlipChecked}})
	if err != nil || created.Secret == "" || created.ID != "ep_1" || created.DisabledReason != "" {
		t.Fatalf("create %+v %v", created, err)
	}
	if _, err := NewWebhookVerifier(created.Secret); err != nil {
		t.Fatalf("created secret unusable: %v", err)
	}
	all := c.Webhooks.Endpoints.ListAll(ctx, nil)
	n := 0
	for all.Next() {
		n++
	}
	if all.Err() != nil || n != 1 {
		t.Fatalf("list n=%d err=%v", n, all.Err())
	}
	off := false
	if _, err := c.Webhooks.Endpoints.Update(ctx, "ep_1", &UpdateEndpointParams{Enabled: &off, ClearDescription: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Webhooks.Endpoints.Delete(ctx, "ep_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Webhooks.Endpoints.Recover(ctx, "ep_1", &RecoverParams{Since: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rot, err := c.Webhooks.Endpoints.RotateSecret(ctx, "ep_1")
	if err != nil || rot.Secret != "whsec_x" {
		t.Fatalf("rotate %+v %v", rot, err)
	}
	if _, err := c.Webhooks.Endpoints.SendTest(ctx, "ep_1"); err != nil {
		t.Fatal(err)
	}
	d, err := c.Webhooks.Deliveries.Get(ctx, "dl_1")
	if err != nil || len(d.Attempts) != 1 || *d.Attempts[0].HTTPStatus != 500 {
		t.Fatalf("delivery %+v %v", d, err)
	}
	ev, err := ParseWebhookEvent(d.Payload)
	if err != nil || ev.Type != EventWebhookTest {
		t.Fatalf("payload %v %v", ev, err)
	}
	if _, err := c.Webhooks.Deliveries.Redeliver(ctx, "dl_1"); err != nil {
		t.Fatal(err)
	}
	page, err := c.Webhooks.Deliveries.List(ctx, &ListDeliveriesParams{EndpointID: "ep_1", Status: DeliveryFailed})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("deliveries %+v %v", page, err)
	}
	types, err := c.Webhooks.EventTypes.List(ctx)
	if err != nil || len(types) != 1 || !types[0].Subscribable {
		t.Fatalf("types %+v %v", types, err)
	}
}

func TestAudit(t *testing.T) {
	c, _ := newTestClient(t, routes(t,
		route{method: "GET", path: "/api/audit/events", status: 200,
			body: `{"items":[{"event_id":"ev1","source_domain":"auth","category":"SECURITY","outcome":"SUCCESS","occurred_at":"2026-11-01T00:00:00Z","actor":{"type":"USER","id":"u1"},"channel":"DASHBOARD","action":"api_key.created","target":{"type":"api_key","id":"k1"},"build":"abc","chain":{"stream":"AUDIT_EVENTS"}}],"next_cursor":null}`,
			check: func(t *testing.T, r *http.Request, _ []byte) {
				if r.URL.Query().Get("action") != "api_key.*" {
					t.Errorf("action %q", r.URL.Query().Get("action"))
				}
			}},
		route{method: "GET", path: "/api/audit/requests", status: 200,
			body: `{"items":[{"request_id":"rq1","trace_id":"t","received_at":"2026-11-01T00:00:00Z","latency_ms":5,"channel":"API","method":"POST","route":"/api/slips/lookup","http_status":200,"recorded_by":"gw","fingerprint":{"trans_ref":"630210534087"},"chain":{}}],"next_cursor":null}`},
		route{method: "POST", path: "/api/audit/exports", status: 202,
			body: `{"export_id":"x1","status":"QUEUED","from_day":"2026-11-01","to_day":"2026-11-02","streams":["API_REQUESTS"],"created_at":"2026-11-03T00:00:00Z"}`,
			check: func(t *testing.T, r *http.Request, b []byte) {
				wantKey(t, r, b)
				if !strings.Contains(string(b), `"from_day":"2026-11-01"`) {
					t.Errorf("body %s", b)
				}
			}},
		route{method: "GET", path: "/api/audit/exports/x1/file", status: 200, body: `{"kind":"MANIFEST"}` + "\n"},
		route{method: "GET", path: "/api/audit/signing-keys", status: 200, body: `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"abc","kid":"k1","use":"sig","status":"ACTIVE"}]}`,
			check: func(t *testing.T, r *http.Request, _ []byte) {
				if r.Header.Get("Authorization") != "" {
					t.Error("signing keys sent the API key")
				}
			}},
	))
	ctx := context.Background()
	ev, err := c.Audit.Events.List(ctx, &ListAuditEventsParams{Action: "api_key.*"})
	if err != nil || ev.Items[0].Target.ID != "k1" || ev.Items[0].Actor.Type != ActorUser {
		t.Fatalf("events %+v %v", ev, err)
	}
	rq, err := c.Audit.Requests.List(ctx, nil)
	if err != nil || rq.Items[0].Fingerprint.TransRef != "630210534087" || *rq.Items[0].Channel != ChannelAPI {
		t.Fatalf("requests %+v %v", rq, err)
	}
	ex, err := c.Audit.Exports.Create(ctx, &CreateEvidenceExportParams{FromDay: Date{2026, 11, 1}, ToDay: Date{2026, 11, 2}, Streams: []Stream{StreamAPIRequests}})
	if err != nil || ex.State != ExportQueued {
		t.Fatalf("export %+v %v", ex, err)
	}
	f, err := c.Audit.Exports.Download(ctx, "x1")
	if err != nil {
		t.Fatal(err)
	}
	line, _ := io.ReadAll(f)
	_ = f.Close()
	if !strings.Contains(string(line), "MANIFEST") {
		t.Fatalf("file %q", line)
	}
	keys, err := c.Audit.SigningKeys.Get(ctx)
	if err != nil || keys[0].Curve != "Ed25519" {
		t.Fatalf("keys %+v %v", keys, err)
	}
}

// hiddenFields are being removed from the customer API. No exported type of
// this package may carry them.
var hiddenFields = []string{
	"upstream_ms", "recorded_by", "chain", "trace_id", "checked_elsewhere",
	"latency_ms", "build", "source_domain",
}

func TestNoHiddenFields(t *testing.T) {
	types := []any{
		SlipCheck{}, SlipCheckRecord{}, Slip{}, SlipCheckedData{}, SlipExport{}, SlipExportParams{},
		APIRequestRecord{}, AuditEventRecord{}, EvidenceExport{}, ExportManifest{},
		Balance{}, CreditEntry{}, Order{}, Receipt{}, Usage{}, SupportedBank{},
		Endpoint{}, EndpointWithSecret{}, Delivery{}, DeliveryDetail{}, WebhookEvent{},
		ListSlipsParams{}, ListRequestsParams{}, ListAuditEventsParams{},
		Checkpoint{}, CheckpointLane{}, ChainCheck{}, ChainCheckLane{},
		CheckChainParams{}, ListCheckpointsParams{}, LookupParams{},
	}
	seen := map[reflect.Type]bool{}
	var walk func(rt reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] || rt.PkgPath() != reflect.TypeOf(Slip{}).PkgPath() {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			for _, h := range hiddenFields {
				if name == h || strings.EqualFold(f.Name, strings.ReplaceAll(h, "_", "")) {
					t.Errorf("%s.%s exposes %s", path, f.Name, h)
				}
			}
			walk(f.Type, path+"."+f.Name)
		}
	}
	for _, v := range types {
		rt := reflect.TypeOf(v)
		walk(rt, rt.Name())
	}
	// Strings too: enum values, query keys and JSON tags in the source.
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hiddenFields {
			if strings.Contains(string(src), `"`+h+`"`) || strings.Contains(string(src), `json:"`+h) {
				t.Errorf("%s mentions hidden field %q", f, h)
			}
		}
	}
}
