package checkslip

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

// AuditService reads the account's request records and activity, and builds
// evidence files of them for an auditor, a bank or a court. It needs an API
// key with audit:read; evidence files also need audit:export.
type AuditService struct{ c *Client }

// Redaction notes a field removed from a record.
type Redaction struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Fingerprint identifies the slip a request carried without storing it.
type Fingerprint struct {
	QRSHA256 string `json:"qr_sha256,omitempty"`
	TransRef string `json:"trans_ref,omitempty"`
}

// APIRequestRecord is the record of one request this account sent.
// Records hold no names, phone numbers, emails or full account numbers.
type APIRequestRecord struct {
	RequestID  string    `json:"request_id"`
	ReceivedAt time.Time `json:"received_at"`
	// Channel may be nil.
	Channel    *Channel `json:"channel"`
	Method     string   `json:"method"`
	Route      string   `json:"route"`
	HTTPStatus int      `json:"http_status"`
	APIKeyID   *string  `json:"api_key_id,omitempty"`
	UserID     *string  `json:"user_id,omitempty"`
	// ClientIPPrefix is the caller's network (/24 or /48).
	ClientIPPrefix string `json:"client_ip_prefix,omitempty"`
	// ClientIPHMAC is a keyed hash of the full address, to match requests
	// from the same machine.
	ClientIPHMAC string       `json:"client_ip_hmac,omitempty"`
	UserAgent    string       `json:"user_agent,omitempty"`
	OutcomeCode  string       `json:"outcome_code,omitempty"`
	Fingerprint  *Fingerprint `json:"fingerprint,omitempty"`
	Redactions   []Redaction  `json:"redactions,omitempty"`
}

// AuditActor is who performed an audited action.
type AuditActor struct {
	Type           ActorType `json:"type"`
	ID             *string   `json:"id,omitempty"`
	OnBehalfOfUser *string   `json:"on_behalf_of_user_id,omitempty"`
	Assurance      *string   `json:"assurance,omitempty"`
}

// AuditTarget is what an audited action was done to.
type AuditTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// AuditEventRecord is one action on the account, such as an API key created
// or a member removed.
type AuditEventRecord struct {
	EventID    string        `json:"event_id"`
	Category   AuditCategory `json:"category"`
	Outcome    AuditOutcome  `json:"outcome"`
	OccurredAt time.Time     `json:"occurred_at"`
	// CausationID is the event that caused this one, if any.
	CausationID *string `json:"causation_id,omitempty"`
	// RequestID is the API request that caused this event, if any.
	RequestID *string     `json:"request_id,omitempty"`
	Actor     AuditActor  `json:"actor"`
	Channel   Channel     `json:"channel"`
	Action    string      `json:"action"`
	Target    AuditTarget `json:"target"`
	// Before and After are the values before and after the change, as raw
	// JSON objects.
	Before        map[string]any `json:"before,omitempty"`
	After         map[string]any `json:"after,omitempty"`
	Reason        *string        `json:"reason,omitempty"`
	UndoOfEventID *string        `json:"undo_of_event_id,omitempty"`
	Redactions    []Redaction    `json:"redactions,omitempty"`
}

// ListAuditEventsParams filters the account activity. Without From and To
// the API returns the last 24 hours; one call covers at most 31 days.
type ListAuditEventsParams struct {
	ListOptions
	From, To time.Time
	// Action is a full action name, or a group ending in ".*" such as
	// "api_key.*".
	Action     string
	ActorType  ActorType
	ActorID    string
	Channel    Channel
	TargetType string
	TargetID   string
}

// ListEvents returns one page of account activity, newest first.
func (s *AuditService) ListEvents(ctx context.Context, params *ListAuditEventsParams, opts ...RequestOption) (*Page[AuditEventRecord], error) {
	q := query{}
	if params != nil {
		q.page(params.ListOptions)
		q.time("from", params.From)
		q.time("to", params.To)
		q.str("action", params.Action)
		q.str("actor_type", string(params.ActorType))
		q.str("actor_id", params.ActorID)
		q.str("channel", string(params.Channel))
		q.str("target_type", params.TargetType)
		q.str("target_id", params.TargetID)
	}
	var out Page[AuditEventRecord]
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/events", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListEventsAll walks every activity record that matches params.
func (s *AuditService) ListEventsAll(ctx context.Context, params *ListAuditEventsParams, opts ...RequestOption) *Iter[AuditEventRecord] {
	p := ListAuditEventsParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[AuditEventRecord], error) {
		p.Cursor = cursor
		return s.ListEvents(ctx, &p, opts...)
	})
}

// GetEvent returns one activity record.
func (s *AuditService) GetEvent(ctx context.Context, eventID string, opts ...RequestOption) (*AuditEventRecord, error) {
	if eventID == "" {
		return nil, errors.New("checkslip: event ID is required")
	}
	var out AuditEventRecord
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/events/"+pathEscape(eventID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRequestsParams filters the request records. Without From and To the
// API returns the last 24 hours; one call covers at most 31 days.
type ListRequestsParams struct {
	ListOptions
	From, To    time.Time
	HTTPStatus  int
	OutcomeCode string
	Route       string
	Channel     Channel
	APIKeyID    string
	// TransRef or QRSHA256 find whether a slip was sent before.
	TransRef string
	QRSHA256 string
}

// ListRequests returns one page of the requests this account sent, newest
// first.
func (s *AuditService) ListRequests(ctx context.Context, params *ListRequestsParams, opts ...RequestOption) (*Page[APIRequestRecord], error) {
	q := query{}
	if params != nil {
		q.page(params.ListOptions)
		q.time("from", params.From)
		q.time("to", params.To)
		q.int("http_status", params.HTTPStatus)
		q.str("outcome_code", params.OutcomeCode)
		q.str("route", params.Route)
		q.str("channel", string(params.Channel))
		q.str("api_key_id", params.APIKeyID)
		q.str("trans_ref", params.TransRef)
		q.str("qr_sha256", params.QRSHA256)
	}
	var out Page[APIRequestRecord]
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/requests", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRequestsAll walks every request record that matches params.
func (s *AuditService) ListRequestsAll(ctx context.Context, params *ListRequestsParams, opts ...RequestOption) *Iter[APIRequestRecord] {
	p := ListRequestsParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[APIRequestRecord], error) {
		p.Cursor = cursor
		return s.ListRequests(ctx, &p, opts...)
	})
}

// GetRequest returns one request record.
func (s *AuditService) GetRequest(ctx context.Context, requestID string, opts ...RequestOption) (*APIRequestRecord, error) {
	if requestID == "" {
		return nil, errors.New("checkslip: request ID is required")
	}
	var out APIRequestRecord
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/requests/"+pathEscape(requestID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEvidenceExportParams is the input of AuditService.CreateExport.
type CreateEvidenceExportParams struct {
	// FromDay and ToDay are UTC days, at most 31 days apart. Only closed
	// days can be exported: not today, and yesterday only from 00:10 UTC.
	FromDay Date `json:"from_day"`
	ToDay   Date `json:"to_day"`
	// Streams picks request records, activity or both. Empty means both.
	Streams []Stream `json:"streams,omitempty"`
}

// ExportCreator is who asked for an evidence file.
type ExportCreator struct {
	Type ActorType `json:"type"`
	ID   *string   `json:"id,omitempty"`
}

// ExportManifest describes a READY evidence file.
type ExportManifest struct {
	Format string `json:"format"`
	// HashAlgorithm is "sha-256".
	HashAlgorithm string `json:"hash_algorithm"`
	// Canonicalization is "RFC8785".
	Canonicalization string `json:"canonicalization"`
	// SHA256 covers every byte of the file before its last line.
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// EvidenceExport is an evidence file of the account.
type EvidenceExport struct {
	ExportID  string         `json:"export_id"`
	State     ExportState    `json:"status"`
	FromDay   Date           `json:"from_day"`
	ToDay     Date           `json:"to_day"`
	Streams   []Stream       `json:"streams"`
	CreatedAt time.Time      `json:"created_at"`
	CreatedBy *ExportCreator `json:"created_by,omitempty"`
	ReadyAt   *time.Time     `json:"ready_at,omitempty"`
	// ExpiresAt is when the file is deleted. Download it before then.
	ExpiresAt *time.Time      `json:"expires_at,omitempty"`
	Manifest  *ExportManifest `json:"manifest,omitempty"`
}

// CreateExport starts building an evidence file. It returns at once with
// State QUEUED; poll GetExport until it is READY or FAILED. The call always
// carries an Idempotency-Key, so a retry returns the same file.
func (s *AuditService) CreateExport(ctx context.Context, params *CreateEvidenceExportParams, opts ...RequestOption) (*EvidenceExport, error) {
	if params == nil || params.FromDay.IsZero() || params.ToDay.IsZero() {
		return nil, errors.New("checkslip: CreateEvidenceExportParams.FromDay and ToDay are required")
	}
	body, err := jsonBody(params)
	if err != nil {
		return nil, err
	}
	var out EvidenceExport
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: "/api/audit/exports",
		body: body, ctype: "application/json", keyMode: keyAuto, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListExports returns one page of evidence files, newest first.
func (s *AuditService) ListExports(ctx context.Context, params *ListOptions, opts ...RequestOption) (*Page[EvidenceExport], error) {
	q := query{}
	if params != nil {
		q.page(*params)
	}
	var out Page[EvidenceExport]
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/exports", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListExportsAll walks every evidence file.
func (s *AuditService) ListExportsAll(ctx context.Context, params *ListOptions, opts ...RequestOption) *Iter[EvidenceExport] {
	p := ListOptions{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[EvidenceExport], error) {
		p.Cursor = cursor
		return s.ListExports(ctx, &p, opts...)
	})
}

// GetExport returns the state of one evidence file.
func (s *AuditService) GetExport(ctx context.Context, exportID string, opts ...RequestOption) (*EvidenceExport, error) {
	if exportID == "" {
		return nil, errors.New("checkslip: export ID is required")
	}
	var out EvidenceExport
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/exports/"+pathEscape(exportID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EvidenceFile is a downloaded evidence file. Read it as NDJSON, one record
// per line; the last line is the manifest. Close it when done.
type EvidenceFile struct {
	io.ReadCloser
	// ReprDigest is the Repr-Digest header, the file's SHA-256.
	ReprDigest string
	// Size is the Content-Length, or -1 when unknown.
	Size int64
}

// DownloadExport downloads a READY evidence file. Before it is READY the API
// answers 409; after ExpiresAt it answers 410. Every download is written to
// the account activity.
func (s *AuditService) DownloadExport(ctx context.Context, exportID string, opts ...RequestOption) (*EvidenceFile, error) {
	if exportID == "" {
		return nil, errors.New("checkslip: export ID is required")
	}
	resp, _, err := s.c.do(ctx, &request{
		method: http.MethodGet, path: "/api/audit/exports/" + pathEscape(exportID) + "/file",
		accept: "application/x-ndjson", idempotent: true, opts: opts,
	})
	if err != nil {
		return nil, err
	}
	size := int64(-1)
	if n, perr := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); perr == nil {
		size = n
	}
	return &EvidenceFile{ReadCloser: resp.Body, ReprDigest: resp.Header.Get("Repr-Digest"), Size: size}, nil
}

// JWK is an Ed25519 public key that signs audit checkpoints.
type JWK struct {
	KeyType string `json:"kty"`
	Curve   string `json:"crv"`
	X       string `json:"x"`
	KeyID   string `json:"kid"`
	Use     string `json:"use"`
	Alg     string `json:"alg,omitempty"`
	// Status is ACTIVE or RETIRED. Retired keys stay listed so old
	// signatures can still be checked.
	Status string `json:"status,omitempty"`
}

// SigningKeys returns the public keys that sign the checkpoints in evidence
// files, as a JWK Set. It needs no API key.
func (s *AuditService) SigningKeys(ctx context.Context, opts ...RequestOption) ([]JWK, error) {
	var out struct {
		Keys []JWK `json:"keys"`
	}
	req := getReq("/api/audit/signing-keys", nil, opts)
	req.accept = "application/jwk-set+json"
	req.noAuth = true
	if _, err := s.c.doJSON(ctx, req, &out); err != nil {
		return nil, err
	}
	return out.Keys, nil
}
