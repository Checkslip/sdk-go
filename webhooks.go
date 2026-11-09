package checkslip

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// WebhooksService manages webhook endpoints and their deliveries. To check
// the signature of a message your server receives, see VerifyWebhook.
type WebhooksService struct {
	c *Client
	// Endpoints manages the URLs that receive messages.
	Endpoints *WebhookEndpointsService
	// Deliveries reads and resends sent messages.
	Deliveries *WebhookDeliveriesService
	// EventTypes lists the event types Checkslip can send.
	EventTypes *WebhookEventTypesService
}

// WebhookEventTypesService lists the event types Checkslip can send.
type WebhookEventTypesService struct{ c *Client }

// EventTypeInfo describes one webhook event type.
type EventTypeInfo struct {
	Type        EventType `json:"type"`
	Description string    `json:"description"`
	// Subscribable reports whether the type can be listed in an
	// endpoint's EventTypes.
	Subscribable bool `json:"subscribable"`
}

// List returns the webhook event types.
//
// The API wraps them in an object with nothing else in it, so the client
// returns the event types themselves.
func (s *WebhookEventTypesService) List(ctx context.Context, opts ...RequestOption) ([]EventTypeInfo, error) {
	var out struct {
		Items []EventTypeInfo `json:"items"`
	}
	if _, err := s.c.doJSON(ctx, getReq("/api/webhooks/event-types", nil, opts), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// Verify checks the signature of a message your server received and returns
// the parsed event. It sends nothing, so it works on a client whose API key is
// never used. During a secret rotation pass both secrets.
//
// NewWebhookVerifier and WebhookVerifier.VerifyRequest do the same without a
// client, and take an *http.Request directly.
func (s *WebhooksService) Verify(header http.Header, body []byte, secrets ...string) (*WebhookEvent, error) {
	v, err := NewWebhookVerifier(secrets...)
	if err != nil {
		return nil, err
	}
	if err := v.Verify(header, body); err != nil {
		return nil, err
	}
	ev, err := ParseWebhookEvent(body)
	if err != nil {
		return nil, err
	}
	ev.ID = header.Get("webhook-id")
	return ev, nil
}

// WebhookEndpointsService manages webhook endpoints.
type WebhookEndpointsService struct{ c *Client }

// Endpoint is a URL of your system that receives webhook messages.
type Endpoint struct {
	ID          string      `json:"id"`
	URL         string      `json:"url"`
	Description *string     `json:"description"`
	EventTypes  []EventType `json:"event_types"`
	Enabled     bool        `json:"enabled"`
	// DisabledReason says why Enabled is false, or is empty.
	DisabledReason DisabledReason `json:"disabled_reason"`
	// SecretHint is the end of the signing secret, to tell secrets apart.
	SecretHint string `json:"secret_hint"`
	// PreviousSecretExpiresAt is set during the 24 hours after a secret
	// rotation, while the old secret still signs messages.
	PreviousSecretExpiresAt *time.Time `json:"previous_secret_expires_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// EndpointWithSecret is a new endpoint together with its signing secret.
type EndpointWithSecret struct {
	Endpoint
	// Secret signs every message to this endpoint ("whsec_..."). It is
	// returned only once, when the endpoint is created. Store it in your
	// secret manager.
	Secret string `json:"secret"`
}

// CreateEndpointParams is the input of WebhookEndpointsService.Create.
type CreateEndpointParams struct {
	// URL must be https without user:pass@, and every address its domain
	// resolves to must be public. Required.
	URL string `json:"url"`
	// Description is a note for people. Optional.
	Description string `json:"description,omitempty"`
	// EventTypes lists the messages to send. Required.
	EventTypes []EventType `json:"event_types"`
}

// UpdateEndpointParams changes an endpoint. Only the fields you set are sent
// (JSON Merge Patch).
type UpdateEndpointParams struct {
	URL *string `json:"url,omitempty"`
	// Description sets the note. Use ClearDescription to remove it.
	Description *string `json:"-"`
	// ClearDescription removes the note.
	ClearDescription bool        `json:"-"`
	EventTypes       []EventType `json:"event_types,omitempty"`
	// Enabled turns sending off (false) or back on (true). While an
	// endpoint is off, new messages are not stored at all.
	Enabled *bool `json:"enabled,omitempty"`
}

// MarshalJSON implements json.Marshaler. It writes description as null
// when ClearDescription is set.
func (p UpdateEndpointParams) MarshalJSON() ([]byte, error) {
	type plain UpdateEndpointParams
	m := map[string]any{}
	b, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	switch {
	case p.ClearDescription:
		m["description"] = nil
	case p.Description != nil:
		m["description"] = *p.Description
	}
	return json.Marshal(m)
}

// List returns one page of the account's endpoints.
func (s *WebhookEndpointsService) List(ctx context.Context, params *ListOptions, opts ...RequestOption) (*Page[Endpoint], error) {
	q := query{}
	if params != nil {
		q.page(*params)
	}
	var out Page[Endpoint]
	if _, err := s.c.doJSON(ctx, getReq("/api/webhooks/endpoints", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll walks every endpoint.
func (s *WebhookEndpointsService) ListAll(ctx context.Context, params *ListOptions, opts ...RequestOption) *Iter[Endpoint] {
	p := ListOptions{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[Endpoint], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Create registers a URL to receive messages. The answer carries the
// signing secret, which is never shown again. An account can have up to 16
// endpoints, and the account owner gets an email for each new one.
//
// Create is not retried after a server error or a network failure, because
// a retry could register the URL twice.
func (s *WebhookEndpointsService) Create(ctx context.Context, params *CreateEndpointParams, opts ...RequestOption) (*EndpointWithSecret, error) {
	if params == nil || params.URL == "" || len(params.EventTypes) == 0 {
		return nil, errors.New("checkslip: CreateEndpointParams.URL and EventTypes are required")
	}
	body, err := jsonBody(params)
	if err != nil {
		return nil, err
	}
	var out EndpointWithSecret
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: "/api/webhooks/endpoints",
		body: body, ctype: "application/json", opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Get returns one endpoint. The full secret is never returned, only
// SecretHint.
func (s *WebhookEndpointsService) Get(ctx context.Context, endpointID string, opts ...RequestOption) (*Endpoint, error) {
	if endpointID == "" {
		return nil, errors.New("checkslip: endpoint ID is required")
	}
	var out Endpoint
	if _, err := s.c.doJSON(ctx, getReq(endpointPath(endpointID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update changes the fields set in params. A new URL is checked again as on
// Create.
func (s *WebhookEndpointsService) Update(ctx context.Context, endpointID string, params *UpdateEndpointParams, opts ...RequestOption) (*Endpoint, error) {
	if endpointID == "" {
		return nil, errors.New("checkslip: endpoint ID is required")
	}
	if params == nil {
		params = &UpdateEndpointParams{}
	}
	body, err := jsonBody(params)
	if err != nil {
		return nil, err
	}
	var out Endpoint
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPatch, path: endpointPath(endpointID),
		body: body, ctype: "application/merge-patch+json", opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete stops sending to an endpoint at once and cancels its waiting
// deliveries. A deleted endpoint cannot be restored. To pause sending
// instead, Update it with Enabled set to false.
func (s *WebhookEndpointsService) Delete(ctx context.Context, endpointID string, opts ...RequestOption) error {
	if endpointID == "" {
		return errors.New("checkslip: endpoint ID is required")
	}
	_, err := s.c.doJSON(ctx, &request{
		method: http.MethodDelete, path: endpointPath(endpointID), idempotent: true, opts: opts,
	}, nil)
	return err
}

// RecoverParams is the time range of WebhookEndpointsService.Recover.
type RecoverParams struct {
	// Since is the start of the range. Required.
	Since time.Time `json:"since"`
	// Until is the end, not included. Zero means now.
	Until time.Time `json:"-"`
}

// MarshalJSON implements json.Marshaler.
func (p RecoverParams) MarshalJSON() ([]byte, error) {
	m := map[string]any{"since": p.Since}
	if !p.Until.IsZero() {
		m["until"] = p.Until
	}
	return json.Marshal(m)
}

// RecoverResult confirms a recover request.
type RecoverResult struct {
	EndpointID string    `json:"endpoint_id"`
	Since      time.Time `json:"since"`
	Until      time.Time `json:"until"`
}

// Recover resends every FAILED delivery of the endpoint created in the time
// range, each with its original webhook-id. Use it after your system was
// down. The range can be at most 14 days and reach back at most 30 days.
// The work runs in the background; follow it through the endpoint's
// RETRYING deliveries.
func (s *WebhookEndpointsService) Recover(ctx context.Context, endpointID string, params *RecoverParams, opts ...RequestOption) (*RecoverResult, error) {
	if endpointID == "" {
		return nil, errors.New("checkslip: endpoint ID is required")
	}
	if params == nil || params.Since.IsZero() {
		return nil, errors.New("checkslip: RecoverParams.Since is required")
	}
	body, err := jsonBody(params)
	if err != nil {
		return nil, err
	}
	var out RecoverResult
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: endpointPath(endpointID) + "/recover",
		body: body, ctype: "application/json", keyMode: keyAuto, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SecretRotation is the result of WebhookEndpointsService.RotateSecret.
type SecretRotation struct {
	// Secret is the new signing secret, returned only once.
	Secret string `json:"secret"`
	// PreviousSecretExpiresAt is when the old secret stops signing. Until
	// then every message carries both signatures.
	PreviousSecretExpiresAt time.Time `json:"previous_secret_expires_at"`
}

// RotateSecret issues a new signing secret. The old one keeps signing for 24
// hours, so you can switch without missing a message; VerifyWebhook accepts
// both secrets during that time. You cannot rotate again within those 24
// hours (CodeRotationInProgress).
func (s *WebhookEndpointsService) RotateSecret(ctx context.Context, endpointID string, opts ...RequestOption) (*SecretRotation, error) {
	if endpointID == "" {
		return nil, errors.New("checkslip: endpoint ID is required")
	}
	var out SecretRotation
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: endpointPath(endpointID) + "/secret/rotate",
		keyMode: keyAuto, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SendTest queues a webhook.test message to the endpoint, signed and retried
// like a real one. It uses no credit. Follow the returned delivery with
// Deliveries.Get.
func (s *WebhookEndpointsService) SendTest(ctx context.Context, endpointID string, opts ...RequestOption) (*Delivery, error) {
	if endpointID == "" {
		return nil, errors.New("checkslip: endpoint ID is required")
	}
	var out Delivery
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: endpointPath(endpointID) + "/test",
		keyMode: keyAuto, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func endpointPath(id string) string { return "/api/webhooks/endpoints/" + pathEscape(id) }

// WebhookDeliveriesService reads and resends webhook deliveries.
type WebhookDeliveriesService struct{ c *Client }

// Delivery is one message sent, or being sent, to one endpoint.
type Delivery struct {
	ID string `json:"id"`
	// MessageID is the webhook-id header of the message. It stays the same
	// on every attempt and resend; deduplicate on it.
	MessageID      string         `json:"message_id"`
	EndpointID     string         `json:"endpoint_id"`
	EventType      EventType      `json:"event_type"`
	Status         DeliveryStatus `json:"status"`
	AttemptCount   int            `json:"attempt_count"`
	NextAttemptAt  *time.Time     `json:"next_attempt_at"`
	LastAttemptAt  *time.Time     `json:"last_attempt_at"`
	LastHTTPStatus *int           `json:"last_http_status"`
	CreatedAt      time.Time      `json:"created_at"`
}

// DeliveryDetail is a delivery with its payload and every attempt.
type DeliveryDetail struct {
	Delivery
	// Payload is the message body, or nil once it has expired. Decode it
	// with ParseWebhookEvent.
	Payload          json.RawMessage   `json:"payload"`
	PayloadExpiresAt time.Time         `json:"payload_expires_at"`
	Attempts         []DeliveryAttempt `json:"attempts"`
}

// DeliveryAttempt is one HTTP attempt of a delivery.
type DeliveryAttempt struct {
	AttemptNo   int            `json:"attempt_no"`
	Trigger     AttemptTrigger `json:"trigger,omitempty"`
	AttemptedAt time.Time      `json:"attempted_at"`
	Outcome     AttemptOutcome `json:"outcome"`
	HTTPStatus  *int           `json:"http_status"`
	DurationMS  int64          `json:"duration_ms"`
	// ResponseExcerpt is the start of your endpoint's response body.
	ResponseExcerpt *string `json:"response_excerpt"`
}

// ListDeliveriesParams filters the delivery list.
type ListDeliveriesParams struct {
	ListOptions
	EndpointID string
	Status     DeliveryStatus
	EventType  EventType
	// Since and Until limit the time range. Zero means no limit.
	Since, Until time.Time
}

// List returns one page of deliveries, newest first. Deliveries are kept
// for 30 days.
func (s *WebhookDeliveriesService) List(ctx context.Context, params *ListDeliveriesParams, opts ...RequestOption) (*Page[Delivery], error) {
	q := query{}
	if params != nil {
		q.page(params.ListOptions)
		q.str("endpoint_id", params.EndpointID)
		q.str("status", string(params.Status))
		q.str("event_type", string(params.EventType))
		q.time("since", params.Since)
		q.time("until", params.Until)
	}
	var out Page[Delivery]
	if _, err := s.c.doJSON(ctx, getReq("/api/webhooks/deliveries", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll walks every delivery that matches params.
func (s *WebhookDeliveriesService) ListAll(ctx context.Context, params *ListDeliveriesParams, opts ...RequestOption) *Iter[Delivery] {
	p := ListDeliveriesParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[Delivery], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Get returns one delivery with its payload and attempts.
func (s *WebhookDeliveriesService) Get(ctx context.Context, deliveryID string, opts ...RequestOption) (*DeliveryDetail, error) {
	if deliveryID == "" {
		return nil, errors.New("checkslip: delivery ID is required")
	}
	var out DeliveryDetail
	if _, err := s.c.doJSON(ctx, getReq("/api/webhooks/deliveries/"+pathEscape(deliveryID), nil, opts), &out); err != nil {
		return nil, err
	}
	if string(out.Payload) == "null" {
		out.Payload = nil
	}
	return &out, nil
}

// Redeliver sends the same payload again, byte for byte, with the original
// webhook-id and a new timestamp and signature. It works on FAILED and
// SUCCEEDED deliveries. Your system receives the message again, so
// deduplicate on webhook-id.
func (s *WebhookDeliveriesService) Redeliver(ctx context.Context, deliveryID string, opts ...RequestOption) (*Delivery, error) {
	if deliveryID == "" {
		return nil, errors.New("checkslip: delivery ID is required")
	}
	var out Delivery
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: "/api/webhooks/deliveries/" + pathEscape(deliveryID) + "/redeliver",
		keyMode: keyAuto, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
