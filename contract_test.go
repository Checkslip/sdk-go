package checkslip

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const contractTestQR = "00330006000001010306902126302105340875102TH9104F86F"

// These tests guard the cross-language SDK contract in docs/sdk-naming.md.
// They exist so the four SDKs cannot drift apart again.

// canonicalNames is every resource.method from the table, spelled the Go way:
// 43 API operations, 10 ListAll iterators and the local Webhooks.Verify.
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
	"Audit.Checkpoints.List", "Audit.Checkpoints.ListAll", "Audit.Checkpoints.ListLatest",
	"Audit.Chain.Check",
	"Audit.Exports.List", "Audit.Exports.ListAll", "Audit.Exports.Create",
	"Audit.Exports.Get", "Audit.Exports.Download",
	"Audit.SigningKeys.Get",
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
	if len(canonicalNames) != 54 {
		t.Fatalf("canonical list has %d names, want 54", len(canonicalNames))
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

// TestHiddenFieldsTolerated is the runtime half of tolerate-but-hide: the same
// payload with and without the fields the API is removing must decode to the
// same model, and neither may fail.
func TestHiddenFieldsTolerated(t *testing.T) {
	const slip = `"slip":{"trans_ref":"630210534087","transferred_at":"2026-10-29T10:33:30+07:00","amount":{"satang":5000,"currency":"THB"},"sender":{"bank":{"code":"KKP","bot_code":"069","name_th":null,"name_en":null},"name_th":null,"name_en":null,"account":null},"receiver":{"kind":"BANK_ACCOUNT","bank":null,"name_th":null,"name_en":null,"account":null}}`
	withHidden := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","latency_ms":812,"upstream_ms":640,"checked_elsewhere":false,` + slip + `}`
	withoutHidden := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00",` + slip + `}`

	var a, b SlipCheck
	if err := json.Unmarshal([]byte(withHidden), &a); err != nil {
		t.Fatalf("payload with the hidden fields must still decode: %v", err)
	}
	if err := json.Unmarshal([]byte(withoutHidden), &b); err != nil {
		t.Fatalf("payload without them must decode: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("the hidden fields changed the decoded model:\n with: %+v\n without: %+v", a, b)
	}

	// The webhook payload is the sharp case: checked_elsewhere is marked
	// required in the spec, so a strict decoder would reject the second body
	// once the API stops sending it.
	withCE := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","channel":"API","client_reference":"o-1","checked_elsewhere":true,"resolved_after_pending":false}`
	withoutCE := `{"request_id":"r1","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00","channel":"API","client_reference":"o-1","resolved_after_pending":false}`
	var c, d SlipCheckedData
	if err := json.Unmarshal([]byte(withCE), &c); err != nil {
		t.Fatalf("webhook payload with checked_elsewhere must decode: %v", err)
	}
	if err := json.Unmarshal([]byte(withoutCE), &d); err != nil {
		t.Fatalf("webhook payload without checked_elsewhere must decode: %v", err)
	}
	if !reflect.DeepEqual(c, d) {
		t.Errorf("checked_elsewhere changed the decoded webhook model")
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

	t.Run("latest checkpoints and event types unwrap", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "checkpoints") {
				writeJSON(w, 200, `{"items":[{"checkpoint_id":"cp1","account_id":"a1","stream":"API_REQUEST","day":"2026-11-08","sealed":true,"lanes":[],"created_at":"2026-11-09T00:00:00Z","key_id":"k1","signature":"s"}]}`)
				return
			}
			writeJSON(w, 200, `{"items":[{"type":"slip.checked","description":"d","subscribable":true}]}`)
		})
		cps, err := c.Audit.Checkpoints.ListLatest(context.Background())
		if err != nil || len(cps) != 1 {
			t.Fatalf("checkpoints %+v err %v", cps, err)
		}
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
			w.WriteHeader(204)
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
