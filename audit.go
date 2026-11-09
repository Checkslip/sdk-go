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
type AuditService struct {
	c *Client
	// Events is account activity, such as API keys created or members removed.
	Events *AuditEventsService
	// Requests is the log of requests this account sent.
	Requests *AuditRequestsService
	// Checkpoints are the signed daily seals of the audit hash chain.
	Checkpoints *AuditCheckpointsService
	// Chain recomputes one day of the audit hash chain.
	Chain *AuditChainService
	// Exports builds evidence files for an auditor, a bank or a court.
	Exports *AuditExportsService
	// Get are the public keys that sign checkpoints.
	SigningKeys *AuditSigningKeysService
}

// AuditEventsService reads account activity.
type AuditEventsService struct{ c *Client }

// AuditRequestsService reads the account's request log.
type AuditRequestsService struct{ c *Client }

// AuditCheckpointsService reads the signed seals of the audit hash chain.
type AuditCheckpointsService struct{ c *Client }

// AuditChainService recomputes a day of the audit hash chain.
type AuditChainService struct{ c *Client }

// AuditExportsService builds and downloads evidence files.
type AuditExportsService struct{ c *Client }

// AuditSigningKeysService reads the checkpoint signing keys.
type AuditSigningKeysService struct{ c *Client }

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

// List returns one page of account activity, newest first.
func (s *AuditEventsService) List(ctx context.Context, params *ListAuditEventsParams, opts ...RequestOption) (*Page[AuditEventRecord], error) {
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

// ListAll walks every activity record that matches params.
func (s *AuditEventsService) ListAll(ctx context.Context, params *ListAuditEventsParams, opts ...RequestOption) *Iter[AuditEventRecord] {
	p := ListAuditEventsParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[AuditEventRecord], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Get returns one activity record.
func (s *AuditEventsService) Get(ctx context.Context, eventID string, opts ...RequestOption) (*AuditEventRecord, error) {
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

// List returns one page of the requests this account sent, newest
// first.
func (s *AuditRequestsService) List(ctx context.Context, params *ListRequestsParams, opts ...RequestOption) (*Page[APIRequestRecord], error) {
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

// ListAll walks every request record that matches params.
func (s *AuditRequestsService) ListAll(ctx context.Context, params *ListRequestsParams, opts ...RequestOption) *Iter[APIRequestRecord] {
	p := ListRequestsParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[APIRequestRecord], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Get returns one request record.
func (s *AuditRequestsService) Get(ctx context.Context, requestID string, opts ...RequestOption) (*APIRequestRecord, error) {
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

// Create starts building an evidence file. It returns at once with
// State QUEUED; poll GetExport until it is READY or FAILED. The call always
// carries an Idempotency-Key, so a retry returns the same file.
func (s *AuditExportsService) Create(ctx context.Context, params *CreateEvidenceExportParams, opts ...RequestOption) (*EvidenceExport, error) {
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

// List returns one page of evidence files, newest first.
func (s *AuditExportsService) List(ctx context.Context, params *ListOptions, opts ...RequestOption) (*Page[EvidenceExport], error) {
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

// ListAll walks every evidence file.
func (s *AuditExportsService) ListAll(ctx context.Context, params *ListOptions, opts ...RequestOption) *Iter[EvidenceExport] {
	p := ListOptions{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[EvidenceExport], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Get returns the state of one evidence file.
func (s *AuditExportsService) Get(ctx context.Context, exportID string, opts ...RequestOption) (*EvidenceExport, error) {
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

// Download downloads a READY evidence file. Before it is READY the API
// answers 409; after ExpiresAt it answers 410. Every download is written to
// the account activity.
func (s *AuditExportsService) Download(ctx context.Context, exportID string, opts ...RequestOption) (*EvidenceFile, error) {
	if exportID == "" {
		return nil, errors.New("checkslip: export ID is required")
	}
	resp, _, err := s.c.do(ctx, &request{ //nolint:bodyclose // the caller closes the EvidenceFile
		method: http.MethodGet, path: "/api/audit/exports/" + pathEscape(exportID) + "/file",
		accept: "application/x-ndjson", idempotent: true, stream: true, opts: opts,
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
func (s *AuditSigningKeysService) Get(ctx context.Context, opts ...RequestOption) ([]JWK, error) {
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

// CheckpointLane is one lane of a sealed checkpoint, with the row hash that
// seals it.
type CheckpointLane struct {
	Lane    int    `json:"lane"`
	Seq     int64  `json:"seq"`
	RowHash string `json:"row_hash"`
}

// Checkpoint is a signed daily seal of one record stream. Verify the signature
// with a key from SigningKeys.Get.
type Checkpoint struct {
	CheckpointID string           `json:"checkpoint_id"`
	AccountID    string           `json:"account_id"`
	Stream       Stream           `json:"stream"`
	Day          Date             `json:"day"`
	Sealed       bool             `json:"sealed"`
	Lanes        []CheckpointLane `json:"lanes"`
	CreatedAt    time.Time        `json:"created_at"`
	KeyID        string           `json:"key_id"`
	Signature    string           `json:"signature"`
}

// ListCheckpointsParams filters a checkpoint listing.
type ListCheckpointsParams struct {
	ListOptions
	FromDay Date
	ToDay   Date
	Stream  Stream
}

// List returns one page of sealed checkpoints, newest first.
func (s *AuditCheckpointsService) List(ctx context.Context, params *ListCheckpointsParams, opts ...RequestOption) (*Page[Checkpoint], error) {
	q := query{}
	if params != nil {
		q.page(params.ListOptions)
		q.date("from_day", params.FromDay)
		q.date("to_day", params.ToDay)
		q.str("stream", string(params.Stream))
	}
	var out Page[Checkpoint]
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/checkpoints", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll walks every checkpoint that matches params.
func (s *AuditCheckpointsService) ListAll(ctx context.Context, params *ListCheckpointsParams, opts ...RequestOption) *Iter[Checkpoint] {
	p := ListCheckpointsParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[Checkpoint], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// ListLatest returns the newest checkpoint of each record stream.
//
// The API wraps these in an object with nothing else in it, so the client
// returns the checkpoints themselves.
func (s *AuditCheckpointsService) ListLatest(ctx context.Context, opts ...RequestOption) ([]Checkpoint, error) {
	var out struct {
		Items []Checkpoint `json:"items"`
	}
	if _, err := s.c.doJSON(ctx, getReq("/api/audit/checkpoints/latest", nil, opts), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// ChainCheckLane is the result of recomputing one lane of a day.
type ChainCheckLane struct {
	Lane   int  `json:"lane"`
	Intact bool `json:"intact"`
	// FirstBrokenSeq is the first row that did not match, when Intact is false.
	FirstBrokenSeq *int64 `json:"first_broken_seq,omitempty"`
	// MatchesCheckpoint reports whether the recomputed hash matches the seal.
	MatchesCheckpoint *bool `json:"matches_checkpoint,omitempty"`
}

// ChainCheck is the result of recomputing one day of the audit hash chain.
type ChainCheck struct {
	Intact  bool             `json:"intact"`
	Day     Date             `json:"day"`
	Stream  Stream           `json:"stream"`
	Checked int              `json:"checked"`
	Lanes   []ChainCheckLane `json:"lanes"`
}

// CheckChainParams is the input of AuditChainService.Check.
type CheckChainParams struct {
	// Day is the UTC day to recompute. Required.
	Day Date `json:"day"`
	// Stream is the record stream to recompute. Required.
	Stream Stream `json:"stream"`
}

// Check recomputes one day of the audit hash chain and compares it with the
// sealed checkpoint. Intact is false when a row no longer matches its hash.
func (s *AuditChainService) Check(ctx context.Context, params *CheckChainParams, opts ...RequestOption) (*ChainCheck, error) {
	if params == nil || params.Day.IsZero() || params.Stream == "" {
		return nil, errors.New("checkslip: CheckChainParams.Day and Stream are required")
	}
	body, err := jsonBody(params)
	if err != nil {
		return nil, err
	}
	var out ChainCheck
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: "/api/audit/chain/checks",
		body: body, ctype: "application/json", opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
