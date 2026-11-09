package checkslip

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBaseURLValidation(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https", "https://api.checkslip.com", false},
		{"https trailing slash", "https://api.checkslip.com/", false},
		{"http loopback v4", "http://127.0.0.1:8080", false},
		{"http localhost", "http://localhost:8080", false},
		{"http loopback v6", "http://[::1]:8080", false},
		{"http remote", "http://api.checkslip.com", true},
		{"no scheme", "api.checkslip.com", true},
		{"ftp", "ftp://api.checkslip.com", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBaseURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseBaseURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestBadBaseURLFailsFirstRequest(t *testing.T) {
	c := NewClient(testKey, WithBaseURL("http://example.com"))
	_, err := c.Billing.Balance.Get(context.Background())
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("got %v, want base URL error", err)
	}
}

func TestRequestHeaders(t *testing.T) {
	var got http.Header
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		writeJSON(w, 200, `{"credits":5}`)
	}, WithUserAgent("my-shop/1.4"))

	_, err := c.Billing.Balance.Get(context.Background(),
		WithHeader("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"),
		WithHeader("Authorization", "Bearer stolen"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if v := got.Get("Authorization"); v != "Bearer "+testKey {
		t.Errorf("Authorization = %q", v)
	}
	if v := got.Get("User-Agent"); !strings.HasPrefix(v, "my-shop/1.4 checkslip-go/"+Version) {
		t.Errorf("User-Agent = %q", v)
	}
	if v := got.Get("traceparent"); v == "" {
		t.Error("traceparent header not sent")
	}
	if v := got.Get("Idempotency-Key"); v != "" {
		t.Errorf("GET sent Idempotency-Key %q", v)
	}
}

func TestNoAuthEndpointsSendNoKey(t *testing.T) {
	var auth atomic.Value
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		writeJSON(w, 200, `{"items":[]}`)
	})
	if _, err := c.Banks.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v := auth.Load().(string); v != "" {
		t.Fatalf("Banks.List sent Authorization %q", v)
	}
}

func TestPerAttemptTimeout(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		writeJSON(w, 200, `{"credits":1}`)
	}, WithTimeout(50*time.Millisecond))

	b, err := c.Billing.Balance.Get(context.Background())
	if err != nil {
		t.Fatalf("want success after a timed-out first attempt, got %v", err)
	}
	if b.Credits != 1 || calls.Load() != 2 {
		t.Fatalf("credits=%d calls=%d", b.Credits, calls.Load())
	}
}

func TestContextCanceled(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Billing.Balance.Get(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
}

func TestNilContext(t *testing.T) {
	c := NewClient(testKey)
	//lint:ignore SA1012 testing the nil guard on purpose
	_, err := c.Billing.Balance.Get(nil) //nolint:staticcheck
	if err == nil {
		t.Fatal("want error for nil context")
	}
}

func TestIdempotencyKeyFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		k := NewIdempotencyKey()
		if len(k) != 36 || k[14] != '4' {
			t.Fatalf("bad key %q", k)
		}
		if seen[k] {
			t.Fatalf("duplicate key %q", k)
		}
		seen[k] = true
	}
	if err := checkIdempotencyKey(strings.Repeat("a", 256)); err == nil {
		t.Error("256-char key accepted")
	}
	if err := checkIdempotencyKey("tab\tkey"); err == nil {
		t.Error("control character accepted")
	}
}

func TestDownloadNotCutByAttemptTimeout(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"kind\":\"API_REQUEST\"}\n"))
		w.(http.Flusher).Flush()
		time.Sleep(150 * time.Millisecond) // slower than the attempt timeout
		_, _ = w.Write([]byte("{\"kind\":\"MANIFEST\"}\n"))
	}, WithTimeout(50*time.Millisecond))
	f, err := c.Audit.Exports.Download(context.Background(), "x1")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil || !strings.Contains(string(b), "MANIFEST") {
		t.Fatalf("read %q err %v", b, err)
	}
}

func TestAttemptTimeoutError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}, WithTimeout(20*time.Millisecond), WithMaxRetries(0))
	_, err := c.Billing.Balance.Get(context.Background())
	if !errors.Is(err, errAttemptTimeout) {
		t.Fatalf("got %v, want attempt timeout", err)
	}
}
