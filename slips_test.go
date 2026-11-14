package checkslip

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

const foundJSON = `{"request_id":"0192d4e0-7c1a-7b3e-9f00-1a2b3c4d5e6f","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00",
"slip":{"trans_ref":"630210534087","transferred_at":"2026-10-29T10:33:30+07:00","amount":{"satang":5000,"currency":"THB"},
"sender":{"bank":{"code":"KKP","bot_code":"069","name_th":"ธนาคารเกียรตินาคินภัทร","name_en":"Kiatnakin Phatra Bank"},"name_th":"นาย สมชาย ใ","name_en":"MR. SOMCHAI J","account":"0439"},
"receiver":{"kind":"BANK_ACCOUNT","bank":{"code":"GSB","bot_code":"030","name_th":"ธนาคารออมสิน","name_en":"Government Savings Bank"},"name_th":"นางสาว สมหญิง ร","name_en":"MISS SOMYING R","account":"9681"}},
"client_reference":"order-1001","latency_ms":812,"upstream_ms":640,"checked_elsewhere":true}`

func TestLookupFound(t *testing.T) {
	var body map[string]any
	var hdr http.Header
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/slips/lookup" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		hdr = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, 200, foundJSON)
	})
	res, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "0033000600000101030690212630", ClientReference: "order-1001"})
	if err != nil {
		t.Fatal(err)
	}
	if body["qr"] != "0033000600000101030690212630" || body["client_reference"] != "order-1001" {
		t.Fatalf("body = %v", body)
	}
	if hdr.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", hdr.Get("Content-Type"))
	}
	if k := hdr.Get("Idempotency-Key"); k == "" || k != res.IdempotencyKey {
		t.Errorf("Idempotency-Key = %q, result key %q", k, res.IdempotencyKey)
	}
	if res.Outcome != OutcomeFound || res.Slip.IsSummary() {
		t.Fatalf("outcome %s summary %v", res.Outcome, res.Slip.IsSummary())
	}
	if got := res.Slip.Amount.String(); got != "50.00 THB" {
		t.Errorf("amount = %q", got)
	}
	if res.Slip.Sender.Bank.Code != "KKP" || *res.Slip.Receiver.Account != "9681" || res.Slip.Receiver.Kind != ReceiverBankAccount {
		t.Errorf("parties = %+v / %+v", res.Slip.Sender, res.Slip.Receiver)
	}
	if !res.Slip.TransferredAt.Equal(time.Date(2026, 10, 29, 3, 33, 30, 0, time.UTC)) {
		t.Errorf("transferred_at = %v", res.Slip.TransferredAt)
	}
	// Fields that left the customer API must not leak back out.
	out, _ := json.Marshal(res)
	for _, withdrawn := range []string{"trace_id", "recorded_by", "chain", "build", "source_domain"} {
		if bytes.Contains(out, []byte(withdrawn)) {
			t.Errorf("re-encoded result contains %s", withdrawn)
		}
	}
}

func TestLookupOwnKey(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if k := r.Header.Get("Idempotency-Key"); k != "order-1001-try" {
			t.Errorf("key = %q", k)
		}
		writeJSON(w, 200, foundJSON)
	})
	res, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "q"}, WithIdempotencyKey("order-1001-try"))
	if err != nil {
		t.Fatal(err)
	}
	if res.IdempotencyKey != "order-1001-try" {
		t.Fatal(res.IdempotencyKey)
	}
}

// TestLookupParses202PendingShape locks the parsing of a 202 PENDING answer,
// which the lookup endpoint no longer sends. The parsing stays because
// SlipCheck.RetryAfter is deprecated rather than removed, and a caller who
// reads it must keep getting the same value from the same bytes.
func TestLookupParses202PendingShape(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "15")
		w.Header().Set("Location", "/api/slips/r2")
		writeJSON(w, 202, `{"request_id":"r2","outcome":"PENDING","checked_at":"2026-10-29T10:41:02+07:00","slip":{"trans_ref":"1","sender_bank":{"code":"KBANK","bot_code":"004","name_th":null,"name_en":null}}}`)
	})
	res, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomePending || res.RetryAfter != 15*time.Second {
		t.Fatalf("outcome %s retry %v", res.Outcome, res.RetryAfter)
	}
	if !res.Slip.IsSummary() || res.Slip.SenderBank.BOTCode != "004" || res.Slip.SenderBank.NameEN != nil {
		t.Fatalf("slip = %+v", res.Slip)
	}
}

func TestLookupValidation(t *testing.T) {
	c := NewClient(testKey)
	tests := []struct {
		name string
		run  func() error
	}{
		{"nil params", func() error { _, err := c.Slips.Lookup(context.Background(), nil); return err }},
		{"empty qr", func() error { _, err := c.Slips.Lookup(context.Background(), &LookupParams{QR: "  "}); return err }},
		{"nil image", func() error { _, err := c.Slips.Lookup(context.Background(), &LookupParams{}); return err }},
		{"empty image", func() error {
			_, err := c.Slips.Lookup(context.Background(), &LookupParams{Image: strings.NewReader("")})
			return err
		}},
		{"too large", func() error {
			_, err := c.Slips.Lookup(context.Background(), &LookupParams{Image: io.LimitReader(zeroReader{}, MaxImageSize+1)})
			return err
		}},
		{"get no id", func() error { _, err := c.Slips.Get(context.Background(), ""); return err }},
		{"counts no range", func() error { _, err := c.Slips.Counts(context.Background(), &SlipCountsParams{}); return err }},
		{"export no columns", func() error { _, err := c.Slips.Exports.Create(context.Background(), &SlipExportParams{}); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestLookupImage(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 64)...)
	var calls int
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return
		}
		f, fh, err := r.FormFile("image")
		if err != nil {
			t.Errorf("image part: %v", err)
			return
		}
		got, _ := io.ReadAll(f)
		if !bytes.Equal(got, png) {
			t.Error("image bytes differ")
		}
		if ct := fh.Header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("part Content-Type = %q", ct)
		}
		if fh.Filename != "slip_1.png" {
			t.Errorf("filename = %q", fh.Filename)
		}
		if v := r.FormValue("client_reference"); v != "order-7" {
			t.Errorf("client_reference = %q", v)
		}
		if calls == 1 {
			writeProblem(w, 503, "UNAVAILABLE")
			return
		}
		writeJSON(w, 200, foundJSON)
	})
	res, err := c.Slips.Lookup(context.Background(), &LookupParams{
		Image: bytes.NewReader(png), Filename: "slip\"1.png", ClientReference: "order-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeFound || calls != 2 {
		t.Fatalf("outcome %s calls %d", res.Outcome, calls)
	}
}

func TestListQueryEncoding(t *testing.T) {
	var raw string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		writeJSON(w, 200, `{"items":[],"next_cursor":null}`)
	})
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	page, err := c.Slips.List(context.Background(), &ListSlipsParams{
		ListOptions: ListOptions{Limit: 50, Cursor: "abc"},
		Outcome:     []Outcome{OutcomeFound, OutcomeNotFound},
		Charged:     []ChargedFilter{ChargedYes, ChargedUnsettled},
		FreeReason:  []CustomerFreeReason{CustomerFreeReasonDuplicate},
		TransRef:    "630210534087",
		From:        from,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore() {
		t.Error("HasMore on null cursor")
	}
	want := "charged=true&charged=unsettled&cursor=abc&free_reason=DUPLICATE&from=2026-11-01T00%3A00%3A00Z&limit=50&outcome=FOUND%2CNOT_FOUND&trans_ref=630210534087"
	if raw != want {
		t.Fatalf("query\n got %s\nwant %s", raw, want)
	}
}

func TestListAllPages(t *testing.T) {
	pages := map[string]string{
		"":   `{"items":[{"request_id":"a","outcome":"FOUND","checked_at":"2026-11-01T00:00:00Z","channel":"API"},{"request_id":"b","outcome":"NOT_FOUND","checked_at":"2026-11-01T00:00:00Z","channel":"API"}],"next_cursor":"c1"}`,
		"c1": `{"items":[],"next_cursor":"c2"}`,
		"c2": `{"items":[{"request_id":"c","outcome":"DUPLICATE","checked_at":"2026-11-01T00:00:00Z","channel":"DASHBOARD"}],"next_cursor":null}`,
	}
	var cursors []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		cur := r.URL.Query().Get("cursor")
		cursors = append(cursors, cur)
		if r.URL.Query().Get("outcome") != "FOUND,NOT_FOUND,DUPLICATE" {
			t.Errorf("filter lost on page %q", cur)
		}
		writeJSON(w, 200, pages[cur])
	})
	it := c.Slips.ListAll(context.Background(), &ListSlipsParams{Outcome: []Outcome{OutcomeFound, OutcomeNotFound, OutcomeDuplicate}})
	var ids []string
	for it.Next() {
		ids = append(ids, it.Current().RequestID)
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "a,b,c" || strings.Join(cursors, ",") != ",c1,c2" {
		t.Fatalf("ids %v cursors %q", ids, cursors)
	}
	if it.Next() || it.Cursor() != "" {
		t.Fatal("iterator not finished")
	}
}

func TestListAllStopsOnError(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeJSON(w, 200, `{"items":[{"request_id":"a"}],"next_cursor":"c1"}`)
			return
		}
		writeProblem(w, 400, "INVALID_CURSOR")
	})
	it := c.Slips.ListAll(context.Background(), nil)
	n := 0
	for it.Next() {
		n++
	}
	if n != 1 || !HasCode(it.Err(), CodeInvalidCursor) || it.Cursor() != "c1" {
		t.Fatalf("n=%d err=%v cursor=%q", n, it.Err(), it.Cursor())
	}
}

func TestListAllCursorLoop(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, `{"items":[{"request_id":"a"}],"next_cursor":"same"}`)
	})
	it := c.Slips.ListAll(context.Background(), &ListSlipsParams{ListOptions: ListOptions{Cursor: "same"}})
	for it.Next() {
	}
	if it.Err() == nil {
		t.Fatal("want cursor loop error")
	}
}

func TestGetAndCounts(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/slips/r%2F1", "/api/slips/r/1":
			if r.URL.RawPath != "/api/slips/r%2F1" {
				t.Errorf("ID not escaped: %q", r.URL.RawPath)
			}
			writeJSON(w, 200, `{"request_id":"r/1","outcome":"FOUND","checked_at":"2026-11-01T00:00:00Z","channel":"API","charged":true,"free_reason":null}`)
		case "/api/slips/counts":
			q := r.URL.Query()
			if q.Get("bucket") != "HOUR" || q.Get("from") == "" || q.Get("to") == "" {
				t.Errorf("counts query %v", q)
			}
			writeJSON(w, 200, `{"bucket":"HOUR","as_of":"2026-11-01T01:00:00Z","filters_applied":["from","to"],"filters_ignored":[],"series":[{"start":"2026-11-01T00:00:00Z","counts":{"FOUND":3,"NOT_FOUND":1}}]}`)
		default:
			writeProblem(w, 404, "NOT_FOUND")
		}
	})
	rec, err := c.Slips.Get(context.Background(), "r/1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Charged == nil || !*rec.Charged || rec.FreeReason != nil {
		t.Fatalf("record %+v", rec)
	}
	now := time.Now()
	counts, err := c.Slips.Counts(context.Background(), &SlipCountsParams{From: now.Add(-time.Hour), To: now, Bucket: BucketHour})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Series[0].Counts[OutcomeFound] != 3 {
		t.Fatalf("counts %+v", counts.Series)
	}
}

func TestExports(t *testing.T) {
	var srvURL string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/slips/exports":
			var p map[string]any
			_ = json.NewDecoder(r.Body).Decode(&p)
			if cols, _ := p["columns"].([]any); len(cols) != 2 {
				t.Errorf("columns = %v", p["columns"])
			}
			if r.Header.Get("Idempotency-Key") != "" {
				t.Error("export sent a key nobody asked for")
			}
			writeJSON(w, 200, `{"export_id":"e1","state":"READY","mode":"DIRECT","created_at":"2026-11-01T00:00:00Z","request":{"columns":["request_id","outcome"]},
"parts":[{"part":1,"url":"`+srvURL+`/api/slips/exports/e1/parts/1?sig=abc","url_expires_at":"2026-11-02T00:00:00Z"}]}`)
		case r.URL.Path == "/api/slips/exports/e1/parts/1":
			if r.URL.Query().Get("sig") != "abc" || r.Header.Get("Authorization") == "" {
				t.Errorf("download query %q auth %q", r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte("request_id,outcome\nr1,FOUND\n"))
		default:
			writeProblem(w, 404, "NOT_FOUND")
		}
	})
	srvURL = c.baseURL.String()
	exp, err := c.Slips.Exports.Create(context.Background(), &SlipExportParams{Columns: []ExportColumn{ColumnRequestID, ColumnOutcome}})
	if err != nil {
		t.Fatal(err)
	}
	if exp.State != ExportReady || len(exp.Parts) != 1 {
		t.Fatalf("export %+v", exp)
	}
	body, err := c.Slips.Exports.DownloadPart(context.Background(), exp.Parts[0])
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	csv, _ := io.ReadAll(body)
	if !strings.HasPrefix(string(csv), "request_id,outcome") {
		t.Fatalf("csv = %q", csv)
	}

	_, err = c.Slips.Exports.DownloadPart(context.Background(), ExportPart{Part: 2, URL: "https://evil.example/api/slips/exports/e1/parts/2?sig=x"})
	if err == nil || !strings.Contains(err.Error(), "not on the API host") {
		t.Fatalf("other host: got %v", err)
	}
}

// TestAuditRequestFiltersRepeatKeys covers the audit request filters that take
// several values. The spec declares them style=form with explode=true, so each
// value gets its own key rather than being joined.
func TestAuditRequestFiltersRepeatKeys(t *testing.T) {
	var q url.Values
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		writeJSON(w, 200, `{"items":[],"next_cursor":null}`)
	})
	if _, err := c.Audit.Requests.List(context.Background(), &ListRequestsParams{
		HTTPStatus:  []int{200, 429},
		OutcomeCode: []string{"FOUND", "NOT_FOUND"},
		Route:       []string{"/api/slips/lookup", "/api/usage"},
		Channel:     []Channel{ChannelAPI, ChannelDashboard},
		APIKeyID:    "ak_1",
	}); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string][]string{
		"http_status":  {"200", "429"},
		"outcome_code": {"FOUND", "NOT_FOUND"},
		"route":        {"/api/slips/lookup", "/api/usage"},
		"channel":      {"API", "DASHBOARD"},
		"api_key_id":   {"ak_1"},
	} {
		if got := q[key]; !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}
