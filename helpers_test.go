package checkslip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

const testKey = "csk_test_0123456789"

// recorder captures what the client waited for between retries.
type recorder struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (r *recorder) sleep(_ context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waits = append(r.waits, d)
	return nil
}

func (r *recorder) Waits() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.waits...)
}

// newTestClient starts an httptest server with h and returns a client
// pointed at it. Retries do not really sleep.
func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) (*Client, *recorder) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient(testKey, append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	rec := &recorder{}
	c.sleep = rec.sleep
	return c, rec
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func writeProblem(w http.ResponseWriter, status int, code string, extra ...string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Request-Id", "req-123")
	for i := 0; i+1 < len(extra); i += 2 {
		w.Header().Set(extra[i], extra[i+1])
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"type":"https://checkslip.com/problems/x","title":"t","status":` +
		strconv.Itoa(status) + `,"code":"` + code + `","instance":"/x"}`))
}
