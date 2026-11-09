package checkslip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaxImageSize is the largest slip image the API accepts, 10 MB.
const MaxImageSize = 10 << 20

// SlipsService looks up slips and reads the lookup history.
type SlipsService struct{ c *Client }

// LookupParams is the input of SlipsService.Lookup.
type LookupParams struct {
	// QR is the text of the QR code printed on the slip. Required.
	QR string `json:"qr"`
	// ClientReference is your own reference for this lookup, such as an
	// order number. It comes back in the answer, the history and webhooks.
	ClientReference string `json:"client_reference,omitempty"`
}

// LookupImageParams is the input of SlipsService.LookupImage.
type LookupImageParams struct {
	// Image is the slip picture: JPEG, PNG or WebP, at most 10 MB and 50
	// megapixels. Required. The client reads it fully into memory so the
	// request can be sent again on a retry.
	Image io.Reader
	// Filename is sent as the file name of the upload. Optional.
	Filename string
	// ContentType is the image type, such as "image/png". When empty the
	// client detects it from the first bytes.
	ContentType string
	// ClientReference is your own reference for this lookup. Optional.
	ClientReference string
}

// Lookup sends the QR text of a slip and asks the sending bank for the
// transfer it holds.
//
// The answer's Outcome is FOUND, NOT_FOUND, DUPLICATE or PENDING. Only FOUND
// uses a credit. A QR the API cannot use comes back as an *Error with a code
// such as CodeInvalidQR, CodeQRTampered or CodeTooOld, and uses no credit.
//
// Lookup always sends an Idempotency-Key, generating one unless you pass
// WithIdempotencyKey, so retries never charge twice. The key used is in
// SlipCheck.IdempotencyKey.
func (s *SlipsService) Lookup(ctx context.Context, params *LookupParams, opts ...RequestOption) (*SlipCheck, error) {
	if params == nil || strings.TrimSpace(params.QR) == "" {
		return nil, errors.New("checkslip: LookupParams.QR is required")
	}
	body, err := jsonBody(params)
	if err != nil {
		return nil, err
	}
	return s.lookup(ctx, body, "application/json", opts)
}

// LookupImage uploads a slip image instead of its QR text. The API reads the
// QR from the image and then works like Lookup. The image itself is not
// stored.
func (s *SlipsService) LookupImage(ctx context.Context, params *LookupImageParams, opts ...RequestOption) (*SlipCheck, error) {
	if params == nil || params.Image == nil {
		return nil, errors.New("checkslip: LookupImageParams.Image is required")
	}
	img, err := io.ReadAll(io.LimitReader(params.Image, MaxImageSize+1))
	if err != nil {
		return nil, fmt.Errorf("checkslip: read image: %w", err)
	}
	if len(img) == 0 {
		return nil, errors.New("checkslip: image is empty")
	}
	if len(img) > MaxImageSize {
		return nil, fmt.Errorf("checkslip: image is larger than %d bytes", MaxImageSize)
	}
	ctype := params.ContentType
	if ctype == "" {
		ctype = http.DetectContentType(img)
	}
	name := params.Filename
	if name == "" {
		name = "slip"
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename=%q`, sanitizeFilename(name)))
	h.Set("Content-Type", ctype)
	part, err := mw.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(img); err != nil {
		return nil, err
	}
	if params.ClientReference != "" {
		if err := mw.WriteField("client_reference", params.ClientReference); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return s.lookup(ctx, buf.Bytes(), mw.FormDataContentType(), opts)
}

func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' || r == 0x7f {
			return '_'
		}
		return r
	}, s)
}

func (s *SlipsService) lookup(ctx context.Context, body []byte, ctype string, opts []RequestOption) (*SlipCheck, error) {
	var out SlipCheck
	meta, err := s.c.doJSON(ctx, &request{
		method:  http.MethodPost,
		path:    "/api/slips/lookup",
		body:    body,
		ctype:   ctype,
		keyMode: keyAuto,
		opts:    opts,
	}, &out)
	if err != nil {
		return nil, err
	}
	out.IdempotencyKey = meta.idempotencyKey
	if meta.status == http.StatusAccepted {
		out.RetryAfter = parseRetryAfter(meta.header.Get("Retry-After"), time.Now())
	}
	return &out, nil
}

// Get returns one lookup of this account by its request ID. Use it to read
// the result of a PENDING lookup later. An unknown ID and another account's
// ID both return an error for which IsNotFound is true.
func (s *SlipsService) Get(ctx context.Context, requestID string, opts ...RequestOption) (*SlipCheckRecord, error) {
	if requestID == "" {
		return nil, errors.New("checkslip: request ID is required")
	}
	var out SlipCheckRecord
	if _, err := s.c.doJSON(ctx, getReq("/api/slips/"+pathEscape(requestID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSlipsParams filters the lookup history. Every field is optional.
type ListSlipsParams struct {
	ListOptions
	// Q is an exact match (never partial) on a request ID, transfer
	// reference or client reference. It ignores From and To.
	Q string
	// TransRef finds lookups of one transfer reference, for example to see
	// whether a slip was sent before.
	TransRef string
	// Outcome keeps only these outcomes.
	Outcome []Outcome
	// OutcomeNot drops these outcomes.
	OutcomeNot []Outcome
	// SenderBank is the three-digit Bank of Thailand code of the sender.
	SenderBank string
	// APIKey is the 12-character ID of the API key that made the lookup.
	APIKey string
	// After returns only lookups newer than this request ID. Do not
	// combine it with a cursor.
	After string
	// Charged filters by whether a credit was used.
	Charged []ChargedFilter
	// FreeReason keeps lookups that used no credit for these reasons.
	FreeReason []FreeReason
	// From and To limit the time range. Zero means no limit.
	From, To time.Time
}

func (p *ListSlipsParams) query() query {
	q := query{}
	if p == nil {
		return q
	}
	q.page(p.ListOptions)
	p.filters(q)
	return q
}

func (p *ListSlipsParams) filters(q query) {
	q.str("q", p.Q)
	q.str("trans_ref", p.TransRef)
	csv(q, "outcome", p.Outcome)
	csv(q, "outcome_not", p.OutcomeNot)
	q.str("sender_bank", p.SenderBank)
	q.str("api_key", p.APIKey)
	q.str("after", p.After)
	multi(q, "charged", p.Charged)
	multi(q, "free_reason", p.FreeReason)
	q.time("from", p.From)
	q.time("to", p.To)
}

// List returns one page of the lookup history, newest first. FOUND rows
// carry the payer and payee names with masked account numbers.
func (s *SlipsService) List(ctx context.Context, params *ListSlipsParams, opts ...RequestOption) (*Page[SlipCheckRecord], error) {
	var out Page[SlipCheckRecord]
	if _, err := s.c.doJSON(ctx, getReq("/api/slips", params.query().values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll walks the whole lookup history that matches params, page by page.
func (s *SlipsService) ListAll(ctx context.Context, params *ListSlipsParams, opts ...RequestOption) *Iter[SlipCheckRecord] {
	p := ListSlipsParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[SlipCheckRecord], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// SlipCountsParams is the input of SlipsService.Counts.
type SlipCountsParams struct {
	// From and To are the time range. Required.
	From, To time.Time
	// Bucket is the size of each count. Required.
	Bucket CountBucket
	// Outcome and OutcomeNot filter by outcome.
	Outcome    []Outcome
	OutcomeNot []Outcome
}

// SlipCounts is the number of lookups per time bucket.
type SlipCounts struct {
	Bucket CountBucket `json:"bucket"`
	// AsOf is the time the counts were computed.
	AsOf time.Time `json:"as_of"`
	// FiltersApplied lists the filters the counts honor.
	FiltersApplied []string `json:"filters_applied"`
	// FiltersIgnored lists filters that were sent but do not apply to
	// counts.
	FiltersIgnored []string      `json:"filters_ignored"`
	Series         []CountSeries `json:"series"`
}

// CountSeries is the count of one bucket, keyed by outcome.
type CountSeries struct {
	Start  time.Time         `json:"start"`
	Counts map[Outcome]int64 `json:"counts"`
}

// Counts returns how many lookups were made per time bucket, split by
// outcome.
func (s *SlipsService) Counts(ctx context.Context, params *SlipCountsParams, opts ...RequestOption) (*SlipCounts, error) {
	if params == nil || params.From.IsZero() || params.To.IsZero() || params.Bucket == "" {
		return nil, errors.New("checkslip: SlipCountsParams.From, To and Bucket are required")
	}
	q := query{}
	q.time("from", params.From)
	q.time("to", params.To)
	q.str("bucket", string(params.Bucket))
	csv(q, "outcome", params.Outcome)
	csv(q, "outcome_not", params.OutcomeNot)
	var out SlipCounts
	if _, err := s.c.doJSON(ctx, getReq("/api/slips/counts", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SlipExportParams describes a CSV export of the lookup history.
type SlipExportParams struct {
	// Columns lists the CSV columns in order. Required.
	Columns []ExportColumn `json:"columns"`
	// Q, TransRef and the other filters work as in ListSlipsParams.
	Q          string          `json:"q,omitempty"`
	TransRef   string          `json:"trans_ref,omitempty"`
	Outcome    []Outcome       `json:"outcome,omitempty"`
	OutcomeNot []Outcome       `json:"outcome_not,omitempty"`
	SenderBank string          `json:"sender_bank,omitempty"`
	APIKey     string          `json:"api_key,omitempty"`
	Charged    []ChargedFilter `json:"charged,omitempty"`
	FreeReason []FreeReason    `json:"free_reason,omitempty"`
	From       *time.Time      `json:"from,omitempty"`
	To         *time.Time      `json:"to,omitempty"`
	// Cursor continues a truncated export: pass SlipExport.ContinueCursor.
	Cursor string `json:"cursor,omitempty"`
}

// SlipExport is a CSV export of the lookup history.
type SlipExport struct {
	ExportID  string           `json:"export_id"`
	State     ExportState      `json:"state"`
	Mode      ExportMode       `json:"mode,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	Request   SlipExportParams `json:"request"`
	// Rows is the number of rows, once known.
	Rows *int64 `json:"rows,omitempty"`
	// Truncated is true when the export hit its row limit. Create another
	// export with Cursor set to ContinueCursor for the rest.
	Truncated      bool    `json:"truncated,omitempty"`
	ContinueCursor *string `json:"continue_cursor,omitempty"`
	// ExpiresAt is when the parts are deleted.
	ExpiresAt *time.Time   `json:"expires_at,omitempty"`
	Parts     []ExportPart `json:"parts,omitempty"`
}

// ExportPart is one downloadable CSV file of an export.
type ExportPart struct {
	Part int    `json:"part"`
	Rows *int64 `json:"rows,omitempty"`
	// URL is a signed download link. Pass the part to DownloadExportPart.
	URL          string    `json:"url"`
	URLExpiresAt time.Time `json:"url_expires_at"`
}

// CreateExport starts a CSV export of the lookup history. Up to 1,000,000
// rows come back at once (Mode DIRECT); more runs as a background job (Mode
// JOB): poll GetExport until State is READY. One export runs per account at a
// time. The file holds payer and payee names, so store it with care.
func (s *SlipsService) CreateExport(ctx context.Context, params *SlipExportParams, opts ...RequestOption) (*SlipExport, error) {
	if params == nil || len(params.Columns) == 0 {
		return nil, errors.New("checkslip: SlipExportParams.Columns is required")
	}
	body, err := jsonBody(params)
	if err != nil {
		return nil, err
	}
	var out SlipExport
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: "/api/slips/exports",
		body: body, ctype: "application/json", keyMode: keyOptional, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetExport returns the state of a lookup history export.
func (s *SlipsService) GetExport(ctx context.Context, exportID string, opts ...RequestOption) (*SlipExport, error) {
	if exportID == "" {
		return nil, errors.New("checkslip: export ID is required")
	}
	var out SlipExport
	if _, err := s.c.doJSON(ctx, getReq("/api/slips/exports/"+pathEscape(exportID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadExportPart downloads one CSV part of a READY export. The caller
// must close the returned body. The part's URL must point at the API host
// the client uses; the client refuses other hosts so the API key stays
// with Checkslip.
func (s *SlipsService) DownloadExportPart(ctx context.Context, part ExportPart, opts ...RequestOption) (io.ReadCloser, error) {
	if s.c.baseErr != nil {
		return nil, s.c.baseErr
	}
	u, err := url.Parse(part.URL)
	if err != nil {
		return nil, fmt.Errorf("checkslip: export part URL: %w", err)
	}
	if !u.IsAbs() {
		u = s.c.baseURL.ResolveReference(u)
	}
	if !s.c.sameOrigin(u) {
		return nil, fmt.Errorf("checkslip: export part %s is not on the API host %s", strconv.Itoa(part.Part), s.c.baseURL.Host)
	}
	resp, _, err := s.c.do(ctx, &request{
		method: http.MethodGet, path: u.Path, absURL: u,
		accept: "text/csv", idempotent: true, opts: opts,
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func getReq(path string, q url.Values, opts []RequestOption) *request {
	return &request{method: http.MethodGet, path: path, query: q, idempotent: true, opts: opts}
}
