package checkslip

// The API may add enum values over time. Every enum here is a string type,
// so an unknown value still decodes; handle it like a default case.

// Outcome is the result of one slip lookup.
type Outcome string

// Outcomes. Lookup returns FOUND, NOT_FOUND, DUPLICATE or PENDING; the other
// values appear in the lookup history and in webhooks for lookups the API
// answered with an error.
const (
	// OutcomeFound means the sending bank returned the transfer. This is
	// the only outcome that uses a credit.
	OutcomeFound Outcome = "FOUND"
	// OutcomeNotFound means the bank has no transfer for this reference.
	// Whether to accept the payment is your decision.
	OutcomeNotFound Outcome = "NOT_FOUND"
	// OutcomeDuplicate means this account looked up the same transfer
	// before; DuplicateOf points at the first lookup.
	OutcomeDuplicate Outcome = "DUPLICATE"
	// OutcomePending means the bank does not show the transfer yet. Read
	// the result later with Slips.Get or wait for the slip.checked webhook.
	OutcomePending Outcome = "PENDING"
	// OutcomeUnavailable means the bank could not be reached.
	OutcomeUnavailable Outcome = "UNAVAILABLE"
	// OutcomeInvalidQR means the QR text could not be read as a slip QR.
	OutcomeInvalidQR Outcome = "INVALID_QR"
	// OutcomeInvalidFile means the image could not be read.
	OutcomeInvalidFile Outcome = "INVALID_FILE"
	// OutcomeQRTampered means the QR text failed its integrity check.
	OutcomeQRTampered Outcome = "QR_TAMPERED"
	// OutcomeTooOld means the transfer is older than the bank lets us look
	// back (see SupportedBank.LookbackDays).
	OutcomeTooOld Outcome = "TOO_OLD"
)

// FreeReason says why a lookup used no credit.
type FreeReason string

// Free reasons.
const (
	FreeReasonNotFound    FreeReason = "NOT_FOUND"
	FreeReasonDuplicate   FreeReason = "DUPLICATE"
	FreeReasonPending     FreeReason = "PENDING"
	FreeReasonUnavailable FreeReason = "UNAVAILABLE"
	FreeReasonInvalidQR   FreeReason = "INVALID_QR"
	FreeReasonInvalidFile FreeReason = "INVALID_FILE"
	FreeReasonQRTampered  FreeReason = "QR_TAMPERED"
	FreeReasonTooOld      FreeReason = "TOO_OLD"
	FreeReasonNoCredit    FreeReason = "NO_CREDIT"
	FreeReasonHoldExpired FreeReason = "HOLD_EXPIRED"
	FreeReasonSystemError FreeReason = "SYSTEM_ERROR"
)

// ChargedFilter selects lookups by whether they used a credit.
type ChargedFilter string

// Charged filters for ListSlipsParams.Charged.
const (
	ChargedYes       ChargedFilter = "true"
	ChargedNo        ChargedFilter = "false"
	ChargedUnsettled ChargedFilter = "unsettled"
)

// ReceiverKind says what kind of account received a transfer.
type ReceiverKind string

// Receiver kinds.
const (
	ReceiverBankAccount    ReceiverKind = "BANK_ACCOUNT"
	ReceiverBillPayment    ReceiverKind = "BILL_PAYMENT"
	ReceiverNotBankAccount ReceiverKind = "NOT_BANK_ACCOUNT"
)

// Channel is where a request or action came from.
type Channel string

// Channels.
const (
	ChannelAPI       Channel = "API"
	ChannelDashboard Channel = "DASHBOARD"
	ChannelAssistant Channel = "ASSISTANT"
	ChannelStaff     Channel = "STAFF"
	ChannelSystem    Channel = "SYSTEM"
)

// ActorType is who performed an action in the audit log.
type ActorType string

// Actor types.
const (
	ActorUser      ActorType = "USER"
	ActorAPIKey    ActorType = "API_KEY"
	ActorAssistant ActorType = "ASSISTANT"
	ActorStaff     ActorType = "STAFF"
	ActorSystem    ActorType = "SYSTEM"
)

// EventType is the type of a webhook message.
type EventType string

// Webhook event types.
const (
	// EventSlipChecked is sent when a lookup finishes, including a
	// PENDING lookup that later resolves.
	EventSlipChecked EventType = "slip.checked"
	// EventWebhookTest is sent by WebhookEndpointsService.SendTest.
	EventWebhookTest EventType = "webhook.test"
)

// DeliveryStatus is the state of one webhook delivery.
type DeliveryStatus string

// Delivery statuses.
const (
	DeliveryPending   DeliveryStatus = "PENDING"
	DeliveryRetrying  DeliveryStatus = "RETRYING"
	DeliverySucceeded DeliveryStatus = "SUCCEEDED"
	// DeliveryFailed means 8 attempts over about 27.5 hours got no 2xx, or
	// your endpoint answered 410 Gone.
	DeliveryFailed    DeliveryStatus = "FAILED"
	DeliveryCancelled DeliveryStatus = "CANCELLED"
)

// AttemptOutcome is the result of one HTTP attempt of a webhook delivery.
type AttemptOutcome string

// Attempt outcomes.
const (
	AttemptSuccess             AttemptOutcome = "SUCCESS"
	AttemptHTTPError           AttemptOutcome = "HTTP_ERROR"
	AttemptTimeout             AttemptOutcome = "TIMEOUT"
	AttemptConnectError        AttemptOutcome = "CONNECT_ERROR"
	AttemptTLSError            AttemptOutcome = "TLS_ERROR"
	AttemptRedirectNotFollowed AttemptOutcome = "REDIRECT_NOT_FOLLOWED"
	AttemptDestinationBlocked  AttemptOutcome = "DESTINATION_BLOCKED"
)

// AttemptTrigger says what started a webhook attempt.
type AttemptTrigger string

// Attempt triggers.
const (
	TriggerSchedule  AttemptTrigger = "SCHEDULE"
	TriggerRedeliver AttemptTrigger = "REDELIVER"
	TriggerRecover   AttemptTrigger = "RECOVER"
)

// DisabledReason says why a webhook endpoint stopped receiving messages.
type DisabledReason string

// Disabled reasons.
const (
	// DisabledManual means someone set enabled to false.
	DisabledManual DisabledReason = "MANUAL"
	// DisabledGone means the endpoint answered 410 Gone.
	DisabledGone DisabledReason = "GONE"
	// DisabledFailing means every delivery failed for 5 days in a row.
	DisabledFailing DisabledReason = "FAILING"
)

// OrderStatus is the state of a credit order.
type OrderStatus string

// Order statuses.
const (
	OrderPending   OrderStatus = "PENDING"
	OrderPaid      OrderStatus = "PAID"
	OrderCancelled OrderStatus = "CANCELLED"
	OrderFlagged   OrderStatus = "FLAGGED"
)

// PaymentMethod is how an order was paid.
type PaymentMethod string

// Payment methods.
const (
	PaymentCard      PaymentMethod = "CARD"
	PaymentPromptPay PaymentMethod = "PROMPTPAY"
)

// CreditReason says why the credit balance changed.
type CreditReason string

// Credit reasons.
const (
	CreditTesterGrant    CreditReason = "TESTER_GRANT"
	CreditPurchase       CreditReason = "PURCHASE"
	CreditSlipCharge     CreditReason = "SLIP_CHARGE"
	CreditSlipFree       CreditReason = "SLIP_FREE"
	CreditExpire         CreditReason = "EXPIRE"
	CreditStaffAdjust    CreditReason = "STAFF_ADJUST"
	CreditAccountDeleted CreditReason = "ACCOUNT_DELETED"
	CreditDevGrant       CreditReason = "DEV_GRANT"
)

// ThresholdBasis says how Balance.Threshold was computed.
type ThresholdBasis string

// Threshold bases.
const (
	ThresholdAvg7Days          ThresholdBasis = "AVG_7_DAYS"
	ThresholdFallback10Percent ThresholdBasis = "FALLBACK_10_PERCENT"
	ThresholdNone              ThresholdBasis = "NONE"
)

// BuyerType says whether the buyer on a receipt is a person or a company.
type BuyerType string

// Buyer types.
const (
	BuyerPerson  BuyerType = "PERSON"
	BuyerCompany BuyerType = "COMPANY"
)

// UsageInterval is the bucket size of a usage report.
type UsageInterval string

// Usage intervals.
const (
	UsageDay   UsageInterval = "DAY"
	UsageMonth UsageInterval = "MONTH"
)

// CountBucket is the bucket size of SlipsService.Counts.
type CountBucket string

// Count buckets.
const (
	BucketMinute         CountBucket = "MINUTE"
	BucketFiveMinutes    CountBucket = "FIVE_MINUTE"
	BucketFifteenMinutes CountBucket = "FIFTEEN_MINUTE"
	BucketHour           CountBucket = "HOUR"
	BucketThreeHours     CountBucket = "THREE_HOUR"
	BucketSixHours       CountBucket = "SIX_HOUR"
	BucketTwelveHours    CountBucket = "TWELVE_HOUR"
	BucketDay            CountBucket = "DAY"
)

// ExportState is the state of a lookup history export or an evidence file.
type ExportState string

// Export states.
const (
	ExportQueued  ExportState = "QUEUED"
	ExportRunning ExportState = "RUNNING"
	ExportReady   ExportState = "READY"
	ExportFailed  ExportState = "FAILED"
	// ExportExpired means the file was deleted.
	ExportExpired ExportState = "EXPIRED"
)

// ExportMode says whether a lookup history export came back at once or runs
// as a background job.
type ExportMode string

// Export modes.
const (
	ExportDirect ExportMode = "DIRECT"
	ExportJob    ExportMode = "JOB"
)

// ExportColumn is one column of a lookup history CSV.
//
// latency_ms, upstream_ms and checked_elsewhere are deliberately absent:
// verify 0.61.0 takes them out of the contract and they are not offered here
// while that lands (checkslip/pm h-mw94dkac8a0, 9 Nov 2026).
type ExportColumn string

// Export columns.
const (
	ColumnRequestID               ExportColumn = "request_id"
	ColumnOutcome                 ExportColumn = "outcome"
	ColumnStatus                  ExportColumn = "status"
	ColumnCheckedAt               ExportColumn = "checked_at"
	ColumnChannel                 ExportColumn = "channel"
	ColumnAPIKeyID                ExportColumn = "api_key_id"
	ColumnClientReference         ExportColumn = "client_reference"
	ColumnDuplicateOfRequestID    ExportColumn = "duplicate_of.request_id"
	ColumnChargeConfirmed         ExportColumn = "charge_confirmed"
	ColumnCharged                 ExportColumn = "charged"
	ColumnFreeReason              ExportColumn = "free_reason"
	ColumnSlipTransRef            ExportColumn = "slip.trans_ref"
	ColumnSlipTransferredAt       ExportColumn = "slip.transferred_at"
	ColumnSlipAmountSatang        ExportColumn = "slip.amount.satang"
	ColumnSlipAmountCurrency      ExportColumn = "slip.amount.currency"
	ColumnSlipSenderBankCode      ExportColumn = "slip.sender.bank.code"
	ColumnSlipSenderBankBOTCode   ExportColumn = "slip.sender.bank.bot_code"
	ColumnSlipSenderNameTH        ExportColumn = "slip.sender.name_th"
	ColumnSlipSenderNameEN        ExportColumn = "slip.sender.name_en"
	ColumnSlipSenderAccount       ExportColumn = "slip.sender.account"
	ColumnSlipReceiverBankCode    ExportColumn = "slip.receiver.bank.code"
	ColumnSlipReceiverBankBOTCode ExportColumn = "slip.receiver.bank.bot_code"
	ColumnSlipReceiverNameTH      ExportColumn = "slip.receiver.name_th"
	ColumnSlipReceiverNameEN      ExportColumn = "slip.receiver.name_en"
	ColumnSlipReceiverAccount     ExportColumn = "slip.receiver.account"
)

// Stream is one of the two record streams kept for audit.
type Stream string

// Streams.
const (
	// StreamAPIRequests holds one record per API request.
	StreamAPIRequests Stream = "API_REQUESTS"
	// StreamAuditEvents holds one record per action on the account.
	StreamAuditEvents Stream = "AUDIT_EVENTS"
)

// AuditCategory groups audit events.
type AuditCategory string

// Audit categories.
const (
	AuditMoney         AuditCategory = "MONEY"
	AuditAccess        AuditCategory = "ACCESS"
	AuditDataChange    AuditCategory = "DATA_CHANGE"
	AuditSensitiveRead AuditCategory = "SENSITIVE_READ"
	AuditSecurity      AuditCategory = "SECURITY"
)

// AuditOutcome is how an audited action ended.
type AuditOutcome string

// Audit outcomes.
const (
	AuditSuccess AuditOutcome = "SUCCESS"
	AuditFailure AuditOutcome = "FAILURE"
	AuditDenied  AuditOutcome = "DENIED"
)
