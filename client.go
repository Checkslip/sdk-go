package checkslip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// Version is the version of this SDK. It is sent in the User-Agent header.
const Version = "0.1.1" // x-release-please-version

// DefaultBaseURL is the Checkslip API address used when WithBaseURL is not given.
const DefaultBaseURL = "https://api.checkslip.com"

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 2
)

// Client talks to the Checkslip API. Create one with NewClient and share it:
// a Client is safe for concurrent use by multiple goroutines.
//
// The API is grouped into services that hang off the Client, for example
// client.Slips.Lookup or client.Billing.Balance.
type Client struct {
	apiKey     string
	baseURL    *url.URL
	baseErr    error
	httpClient *http.Client
	timeout    time.Duration
	maxRetries int
	userAgent  string

	// sleep and newKey are replaced in tests.
	sleep  func(ctx context.Context, d time.Duration) error
	newKey func() string

	// Slips looks up slips and reads the lookup history.
	Slips *SlipsService
	// Billing reads credits, orders, receipts and billing details.
	Billing *BillingService
	// Usage reads lookup counts per day or month.
	Usage *UsageService
	// Banks lists the banks whose transfers can be looked up.
	Banks *BanksService
	// Webhooks manages webhook endpoints and their deliveries.
	Webhooks *WebhooksService
	// Audit reads request records and activity and builds evidence files.
	Audit *AuditService
}

// Option configures a Client. Pass options to NewClient.
type Option func(*Client)

// WithBaseURL sets the API address, for example a local mock server in tests.
// The default is DefaultBaseURL. Plain http is accepted only for loopback
// hosts such as 127.0.0.1 and localhost, so an API key is never sent in clear
// text over a network.
func WithBaseURL(rawURL string) Option {
	return func(c *Client) {
		u, err := parseBaseURL(rawURL)
		c.baseURL, c.baseErr = u, err
	}
}

// WithHTTPClient sets the http.Client used to send requests. Use it to add a
// proxy, custom TLS settings or instrumentation. The client's own Timeout, if
// any, applies on top of the per-attempt timeout from WithTimeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithTimeout sets how long a single attempt may take, including reading the
// response body. Retries get a fresh timeout each. The default is 30 seconds.
// For downloads (DownloadExport, DownloadExportPart) it covers the wait for
// the response headers only; the context limits reading the file.
// Zero or a negative value turns the per-attempt timeout off; the context
// passed to each method still applies.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithMaxRetries sets how many times a failed request is sent again. The
// default is 2, so a request is sent at most 3 times. Zero turns retries off.
// See the package documentation for which failures are retried.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithUserAgent puts your application's name and version in front of the
// SDK's own User-Agent, for example "my-shop/1.4". It helps Checkslip support
// find your requests when you ask about them.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		ua = strings.TrimSpace(ua)
		if ua != "" {
			c.userAgent = ua + " " + defaultUserAgent()
		}
	}
}

// NewClient returns a Client that authenticates with apiKey, a key that
// starts with "csk_". Create keys in the Checkslip dashboard.
//
// NewClient does not contact the API. A bad option, such as an unparsable
// base URL, is reported by the first request.
func NewClient(apiKey string, opts ...Option) *Client {
	base, _ := url.Parse(DefaultBaseURL)
	c := &Client{
		apiKey:     apiKey,
		baseURL:    base,
		httpClient: &http.Client{},
		timeout:    defaultTimeout,
		maxRetries: defaultMaxRetries,
		userAgent:  defaultUserAgent(),
		sleep:      sleepCtx,
		newKey:     NewIdempotencyKey,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	c.Slips = &SlipsService{c: c, Exports: &SlipExportsService{c: c}}
	c.Billing = &BillingService{
		c:        c,
		Balance:  &BillingBalanceService{c: c},
		Info:     &BillingInfoService{c: c},
		Packages: &BillingPackagesService{c: c},
		Ledger:   &BillingLedgerService{c: c},
		Orders:   &BillingOrdersService{c: c},
		Receipts: &BillingReceiptsService{c: c},
	}
	c.Usage = &UsageService{c: c}
	c.Banks = &BanksService{c: c}
	c.Webhooks = &WebhooksService{
		c:          c,
		Endpoints:  &WebhookEndpointsService{c: c},
		Deliveries: &WebhookDeliveriesService{c: c},
		EventTypes: &WebhookEventTypesService{c: c},
	}
	c.Audit = &AuditService{
		c:        c,
		Events:   &AuditEventsService{c: c},
		Requests: &AuditRequestsService{c: c},
		Exports:  &AuditExportsService{c: c},
	}
	return c
}

func defaultUserAgent() string {
	return "checkslip-go/" + Version + " (" + runtime.Version() + ")"
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil {
		return nil, fmt.Errorf("checkslip: invalid base URL: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("checkslip: invalid base URL %q: no host", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(u.Hostname()) {
			return nil, fmt.Errorf("checkslip: base URL %q uses http on a non-loopback host; use https", raw)
		}
	default:
		return nil, fmt.Errorf("checkslip: invalid base URL %q: scheme must be https", raw)
	}
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RequestOption changes a single API call. Pass it as the last argument of
// any service method.
type RequestOption func(*requestConfig)

type requestConfig struct {
	idempotencyKey string
	headers        http.Header
}

// WithIdempotencyKey sends the Idempotency-Key header with this call. A call
// sent again with the same key returns the first result and is not processed
// twice; a lookup is never charged twice.
//
// Methods that need a key generate a random one when you do not pass one.
// Pass your own, for example derived from your order ID, when you may resend
// the same request after a restart of your program.
func WithIdempotencyKey(key string) RequestOption {
	return func(rc *requestConfig) { rc.idempotencyKey = key }
}

// WithHeader adds an HTTP header to this call, for example a W3C traceparent
// header. It cannot change Authorization, Content-Type or Idempotency-Key.
func WithHeader(key, value string) RequestOption {
	return func(rc *requestConfig) {
		if rc.headers == nil {
			rc.headers = http.Header{}
		}
		rc.headers.Add(key, value)
	}
}

// request describes one API call before it is sent.
type request struct {
	method string
	path   string // already escaped
	query  url.Values
	body   []byte
	ctype  string
	accept string
	// idempotent is true when the endpoint is safe to repeat on its own.
	idempotent bool
	// keyMode says whether the call sends an Idempotency-Key.
	keyMode keyMode
	opts    []RequestOption
	// absURL overrides baseURL+path (used for signed download links).
	absURL *url.URL
	noAuth bool
	// stream is true for downloads whose body the caller reads.
	stream bool
}

var errAttemptTimeout = errors.New("checkslip: request attempt timed out")

type keyMode int

const (
	keyNone     keyMode = iota // never send a key
	keyOptional                // send one only if the caller passed one
	keyAuto                    // send the caller's key or generate one
)

// response carries what a successful call returned besides the body.
type response struct {
	header         http.Header
	status         int
	idempotencyKey string
}

func jsonBody(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("checkslip: encode request: %w", err)
	}
	return b, nil
}

// doJSON sends req and decodes a JSON response into out (which may be nil).
func (c *Client) doJSON(ctx context.Context, req *request, out any) (*response, error) {
	resp, rmeta, err := c.do(ctx, req)
	if err != nil {
		return rmeta, err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return rmeta, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return rmeta, fmt.Errorf("checkslip: decode %s %s response: %w", req.method, req.path, err)
	}
	return rmeta, nil
}

// do sends req with retries and returns the first successful response with
// its body unread. The caller must close the body.
func (c *Client) do(ctx context.Context, req *request) (*http.Response, *response, error) {
	if ctx == nil {
		return nil, nil, errors.New("checkslip: nil context")
	}
	if c.baseErr != nil {
		return nil, nil, c.baseErr
	}
	var rc requestConfig
	for _, o := range req.opts {
		if o != nil {
			o(&rc)
		}
	}
	key := rc.idempotencyKey
	switch req.keyMode {
	case keyNone:
		key = ""
	case keyAuto:
		if key == "" {
			key = c.newKey()
		}
	}
	if key != "" {
		if err := checkIdempotencyKey(key); err != nil {
			return nil, nil, err
		}
	}
	meta := &response{idempotencyKey: key}
	// A request may be sent again after a 5xx or a network error only when
	// the server will not process it twice.
	repeatable := req.idempotent || key != ""

	target := req.absURL
	if target == nil {
		u := *c.baseURL
		// req.path is already escaped; keep the escaping so an ID with a
		// "/" stays one path segment.
		raw := strings.TrimRight(c.baseURL.EscapedPath(), "/") + req.path
		p, err := url.PathUnescape(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("checkslip: bad request path %q: %w", req.path, err)
		}
		u.Path, u.RawPath = p, raw
		if req.query != nil {
			u.RawQuery = req.query.Encode()
		}
		target = &u
	}

	for attempt := 0; ; attempt++ {
		resp, err := c.send(ctx, req, target, key, rc.headers)
		if err == nil && resp.StatusCode < 400 {
			meta.header, meta.status = resp.Header, resp.StatusCode
			return resp, meta, nil
		}

		var apiErr *Error
		switch {
		case err == nil:
			apiErr = readError(resp, key)
			err = apiErr
		case ctx.Err() != nil:
			// The caller gave up; do not wrap it as a network failure.
			return nil, meta, ctx.Err()
		default:
			err = fmt.Errorf("checkslip: %s %s: %w", req.method, req.path, err)
		}

		if attempt >= c.maxRetries || !shouldRetry(apiErr, repeatable) {
			return nil, meta, err
		}
		wait := backoff(attempt)
		if apiErr != nil && apiErr.RetryAfter > 0 {
			if apiErr.RetryAfter > maxRetryAfter {
				return nil, meta, err
			}
			wait = apiErr.RetryAfter
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < wait {
			// Waiting would outlive the context; report the last failure now.
			return nil, meta, err
		}
		if serr := c.sleep(ctx, wait); serr != nil {
			return nil, meta, serr
		}
	}
}

func (c *Client) send(ctx context.Context, req *request, target *url.URL, key string, extra http.Header) (*http.Response, error) {
	// The per-attempt timeout cancels with errAttemptTimeout as the cause.
	// For a streamed download it stops once the headers have arrived, so a
	// large file is limited only by ctx.
	actx, cancelCause := context.WithCancelCause(ctx)
	var timer *time.Timer
	if c.timeout > 0 {
		timer = time.AfterFunc(c.timeout, func() { cancelCause(errAttemptTimeout) })
	}
	cancel := func() {
		if timer != nil {
			timer.Stop()
		}
		cancelCause(context.Canceled)
	}
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	hreq, err := http.NewRequestWithContext(actx, req.method, target.String(), body)
	if err != nil {
		cancel()
		return nil, err
	}
	for k, vs := range extra {
		switch http.CanonicalHeaderKey(k) {
		case "Authorization", "Content-Type", "Idempotency-Key":
			continue
		}
		for _, v := range vs {
			hreq.Header.Add(k, v)
		}
	}
	hreq.Header.Set("User-Agent", c.userAgent)
	accept := req.accept
	if accept == "" {
		accept = "application/json"
	}
	hreq.Header.Set("Accept", accept+", application/problem+json")
	if !req.noAuth && c.apiKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if req.body != nil {
		hreq.Header.Set("Content-Type", req.ctype)
	}
	if key != "" {
		hreq.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.httpClient.Do(hreq)
	if err != nil {
		timedOut := errors.Is(context.Cause(actx), errAttemptTimeout)
		cancel()
		if timedOut {
			return nil, fmt.Errorf("%w (%v)", errAttemptTimeout, c.timeout)
		}
		return nil, err
	}
	if req.stream && timer != nil {
		timer.Stop()
	}
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnClose releases the per-attempt context once the body is closed, so
// the timeout keeps covering the body while it is being read.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func checkIdempotencyKey(key string) error {
	if len(key) > 255 {
		return errors.New("checkslip: idempotency key is longer than 255 characters")
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return errors.New("checkslip: idempotency key must be printable ASCII")
		}
	}
	return nil
}

// sameOrigin reports whether u points at the API host of c, so the API key is
// never sent to another host.
func (c *Client) sameOrigin(u *url.URL) bool {
	return strings.EqualFold(u.Scheme, c.baseURL.Scheme) && strings.EqualFold(u.Host, c.baseURL.Host)
}
