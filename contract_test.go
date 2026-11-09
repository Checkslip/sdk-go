package checkslip

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const contractTestQR = "00330006000001010306902126302105340875102TH9104F86F"

// These tests guard the surface the four Checkslip SDKs share.
// They exist so the four SDKs cannot drift apart again.

// canonicalNames is every resource.method from the table, spelled the Go way:
// 39 API operations, 9 ListAll iterators and the local Webhooks.Verify.
// Audit checkpoints, chain checks and signing keys left the customer API on
// 9 Nov 2026 (audit contract 0.8.0), so they are not here.
var canonicalNames = []string{
	"Slips.Lookup", "Slips.Get", "Slips.List", "Slips.ListAll", "Slips.Counts",
	"Slips.Exports.Create", "Slips.Exports.Get", "Slips.Exports.DownloadPart",
	"Banks.List",
	"Billing.Balance.Get",
	"Billing.Info.Get", "Billing.Info.Update",
	"Billing.Packages.List",
	"Billing.Ledger.List", "Billing.Ledger.ListAll",
	"Billing.Orders.List", "Billing.Orders.ListAll", "Billing.Orders.Get", "Billing.Orders.Sync",
	"Billing.Receipts.List", "Billing.Receipts.ListAll", "Billing.Receipts.Get",
	"Usage.Get",
	"Audit.Events.List", "Audit.Events.ListAll", "Audit.Events.Get",
	"Audit.Requests.List", "Audit.Requests.ListAll", "Audit.Requests.Get",
	"Audit.Exports.List", "Audit.Exports.ListAll", "Audit.Exports.Create",
	"Audit.Exports.Get", "Audit.Exports.Download",
	"Webhooks.Endpoints.List", "Webhooks.Endpoints.ListAll", "Webhooks.Endpoints.Create",
	"Webhooks.Endpoints.Get", "Webhooks.Endpoints.Update", "Webhooks.Endpoints.Delete",
	"Webhooks.Endpoints.Recover", "Webhooks.Endpoints.RotateSecret", "Webhooks.Endpoints.SendTest",
	"Webhooks.Deliveries.List", "Webhooks.Deliveries.ListAll", "Webhooks.Deliveries.Get",
	"Webhooks.Deliveries.Redeliver",
	"Webhooks.EventTypes.List",
	"Webhooks.Verify",
}

// walkServices collects every exported method reachable from the client's
// service fields, as a dotted path.
func walkServices(t *testing.T) []string {
	t.Helper()
	c := NewClient(testKey)
	var found []string
	pkg := reflect.TypeOf(Client{}).PkgPath()

	var walk func(v reflect.Value, prefix string, depth int)
	walk = func(v reflect.Value, prefix string, depth int) {
		if depth > 3 || !v.IsValid() || v.IsZero() {
			return
		}
		rt := v.Type()
		for i := 0; i < rt.NumMethod(); i++ {
			found = append(found, prefix+rt.Method(i).Name)
		}
		elem := rt
		if elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			return
		}
		sv := v
		if sv.Kind() == reflect.Pointer {
			sv = sv.Elem()
		}
		for i := 0; i < elem.NumField(); i++ {
			f := elem.Field(i)
			if f.PkgPath != "" || f.Type.Kind() != reflect.Pointer {
				continue // unexported, such as the client itself
			}
			if f.Type.Elem().PkgPath() != pkg || !strings.HasSuffix(f.Type.Elem().Name(), "Service") {
				continue
			}
			walk(sv.Field(i), prefix+f.Name+".", depth+1)
		}
	}

	cv := reflect.ValueOf(c).Elem()
	ct := cv.Type()
	for i := 0; i < ct.NumField(); i++ {
		f := ct.Field(i)
		if f.PkgPath != "" || f.Type.Kind() != reflect.Pointer {
			continue
		}
		if !strings.HasSuffix(f.Type.Elem().Name(), "Service") {
			continue
		}
		walk(cv.Field(i), f.Name+".", 1)
	}
	return found
}

func TestNamingParity(t *testing.T) {
	if len(canonicalNames) != 49 {
		t.Fatalf("canonical list has %d names, want 49", len(canonicalNames))
	}
	found := walkServices(t)
	if len(found) == 0 {
		t.Fatal("walked no methods at all; the parity check would pass vacuously")
	}
	have := map[string]bool{}
	for _, n := range found {
		have[n] = true
	}
	for _, want := range canonicalNames {
		if !have[want] {
			t.Errorf("missing canonical method %s", want)
		}
	}
	want := map[string]bool{}
	for _, n := range canonicalNames {
		want[n] = true
	}
	var extra []string
	for _, n := range found {
		if !want[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("methods not in the table: %v", extra)
	}
}

// TestUnmodelledFieldsTolerated: a response may carry fields the SDK does not
// model, including ones that left the customer API. Decoding must not depend
// on the exact set, and the result must be the same either way.
func TestUnmodelledFieldsTolerated(t *testing.T) {
	const slip = `"slip":{"trans_ref":"630210534087","transferred_at":"2026-10-29T10:33:30+07:00","amount":{"satang":5000,"currency":"THB"},"sender":{"bank":{"code":"KKP","bot_code":"069","name_th":null,"name_en":null},"name_th":null,"name_en":null,"account":null},"receiver":{"kind":"BANK_ACCOUNT","bank":null,"name_th":null,"name_en":null,"account":null}}`
	withExtras := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","trace_id":"t1","recorded_by":"gw","chain":{},"build":"2026.11.9",` + slip + `}`
	without := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00",` + slip + `}`

	var a, b SlipCheck
	if err := json.Unmarshal([]byte(withExtras), &a); err != nil {
		t.Fatalf("a payload with unmodelled fields must still decode: %v", err)
	}
	if err := json.Unmarshal([]byte(without), &b); err != nil {
		t.Fatalf("the same payload without them must decode: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("unmodelled fields changed the decoded model:\n with: %+v\n without: %+v", a, b)
	}

	// The webhook payload no longer marks checked_elsewhere required, but it
	// must still decode whether or not it is there.
	withCE := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","channel":"API","client_reference":"o-1","checked_elsewhere":true,"resolved_after_pending":false}`
	withoutCE := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","channel":"API","client_reference":"o-1","resolved_after_pending":false}`
	var c, d SlipCheckedData
	if err := json.Unmarshal([]byte(withCE), &c); err != nil {
		t.Fatalf("webhook payload with checked_elsewhere must decode: %v", err)
	}
	if err := json.Unmarshal([]byte(withoutCE), &d); err != nil {
		t.Fatalf("webhook payload without checked_elsewhere must decode: %v", err)
	}
}

// Fields being withdrawn from the Checkslip API must not appear on the
// models, so a caller never builds on something that is about to disappear.
func TestWithdrawnFieldsStayOut(t *testing.T) {
	for _, rt := range []reflect.Type{reflect.TypeOf(SlipCheck{}), reflect.TypeOf(SlipCheckRecord{})} {
		for _, name := range []string{"LatencyMs", "UpstreamMs", "CheckedElsewhere"} {
			if _, ok := rt.FieldByName(name); ok {
				t.Errorf("%s still exposes %s", rt.Name(), name)
			}
		}
	}
	for _, c := range []ExportColumn{"latency_ms", "upstream_ms", "checked_elsewhere"} {
		if strings.Contains(exportColumnSource(t), string(c)) {
			t.Errorf("ExportColumn still offers %s", c)
		}
	}
}

// exportColumnSource reads enums.go so the check covers the declared constants
// rather than a hand-kept list.
func exportColumnSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("enums.go")
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), "// Export columns.")
	if i < 0 {
		t.Fatal("could not find the export column block")
	}
	j := strings.Index(string(src)[i:], "\n)")
	return string(src)[i : i+j]
}

// TestContractFieldsArePresent: latency_ms is required on ApiRequestRecord in
// the contract, so the SDK must carry it.
func TestContractFieldsArePresent(t *testing.T) {
	var r APIRequestRecord
	if err := json.Unmarshal([]byte(`{"request_id":"rq1","received_at":"2026-11-01T00:00:00Z","latency_ms":42,"channel":"API","method":"POST","route":"/api/slips/lookup","http_status":200}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.LatencyMs != 42 {
		t.Errorf("latency_ms = %d, want 42", r.LatencyMs)
	}
}

func TestEnvelopeUnwrapping(t *testing.T) {
	t.Run("bare items unwrap to a slice", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, `{"items":[{"code":"KKP","bot_code":"069","name_th":"a","name_en":"b","supported_since":"2026-01-01","landing_page":true,"lookback_days":90}]}`)
		})
		banks, err := c.Banks.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(banks) != 1 || banks[0].Code != "KKP" {
			t.Errorf("got %+v", banks)
		}
	})

	t.Run("event types unwrap", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, `{"items":[{"type":"slip.checked","description":"d","subscribable":true}]}`)
		})
		ets, err := c.Webhooks.EventTypes.List(context.Background())
		if err != nil || len(ets) != 1 {
			t.Fatalf("event types %+v err %v", ets, err)
		}
	})

	t.Run("the package list keeps its envelope", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, `{"items":[],"prices_include_vat":true,"checkout_open":false}`)
		})
		pkgs, err := c.Billing.Packages.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !pkgs.PricesIncludeVAT || pkgs.CheckoutOpen {
			t.Errorf("envelope fields lost: %+v", pkgs)
		}
	})
}

func TestAcceptedIsNotAnError(t *testing.T) {
	t.Run("202 on a lookup is a PENDING result", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 202, `{"request_id":"r1","outcome":"PENDING","checked_at":"2026-10-29T10:41:02+07:00","slip":null}`)
		})
		res, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: contractTestQR})
		if err != nil {
			t.Fatalf("202 must not be an error: %v", err)
		}
		if res.Outcome != OutcomePending {
			t.Errorf("outcome %q, want PENDING", res.Outcome)
		}
	})

	t.Run("204 on a delete returns no error", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		if err := c.Webhooks.Endpoints.Delete(context.Background(), "ep_1"); err != nil {
			t.Fatalf("204 must not be an error: %v", err)
		}
	})
}

func TestLookupNeedsExactlyOneInput(t *testing.T) {
	c := NewClient(testKey)
	ctx := context.Background()
	if _, err := c.Slips.Lookup(ctx, &LookupParams{}); err == nil {
		t.Error("neither QR nor Image should be rejected")
	}
	if _, err := c.Slips.Lookup(ctx, &LookupParams{QR: contractTestQR, Image: strings.NewReader("x")}); err == nil {
		t.Error("both QR and Image should be rejected")
	}
}
