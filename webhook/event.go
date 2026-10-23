package webhook

import (
	"encoding/json"

	didit "github.com/didit-protocol/sdk-go"
)

// Event is a verified webhook. Its dynamic type is one of the *...Event types
// of this package, chosen by the body's webhook_type; use a type switch:
//
//	switch e := ev.(type) {
//	case *webhook.StatusUpdatedEvent:
//		// e.SessionID, e.Status, e.Decision, ...
//	case *webhook.TransactionStatusUpdatedEvent:
//		// e.TransactionID, e.Status, e.Score, ...
//	case *webhook.UnknownEvent:
//		// a webhook type this version of the SDK does not know: e.Payload
//	}
//
// Every event embeds an [EventEnvelope], so the envelope fields (EventID,
// Timestamp, ...) are available on every concrete type, and through the
// interface with Envelope.
type Event interface {
	// Type returns the event's webhook_type, for example "status.updated".
	Type() Type
	// Envelope returns the fields every event carries.
	Envelope() *EventEnvelope
	// Raw returns the verified body. For Verify it is the exact body that was
	// received; for VerifyParsed it is the canonical JSON that
	// X-Signature-V2 signs. Every field Didit sent, including fields this
	// package does not model, can be decoded from it with json.Unmarshal.
	// The returned slice must not be modified.
	Raw() []byte
	// Authenticated reports whether a full-body signature (X-Signature or
	// X-Signature-V2, or their -Previous variants) matched. It is false only
	// when the event was accepted through X-Signature-Simple with
	// WithAllowSimple: that signature covers the envelope only, so the body
	// must not be trusted and the record should be re-fetched from the API.
	Authenticated() bool

	isEvent()
}

// Type is a webhook type, the body's webhook_type field. It is an open set:
// webhook types this package does not know arrive as an *UnknownEvent with
// their Type preserved.
type Type string

// Webhook types with a typed event (docs/DESIGN.md section 9.4).
const (
	TypeStatusUpdated             Type = "status.updated"
	TypeDataUpdated               Type = "data.updated"
	TypeUserStatusUpdated         Type = "user.status.updated"
	TypeUserDataUpdated           Type = "user.data.updated"
	TypeBusinessStatusUpdated     Type = "business.status.updated"
	TypeBusinessDataUpdated       Type = "business.data.updated"
	TypeActivityCreated           Type = "activity.created"
	TypeTransactionCreated        Type = "transaction.created"
	TypeTransactionStatusUpdated  Type = "transaction.status.updated"
	TypeTravelRuleStatusUpdated   Type = "travel_rule.status.updated"
	TypeWorkflowComplianceChanged Type = "workflow.compliance_changed"
	TypeKYBRegistrySearchResolved Type = "kyb.registry_search.resolved"
)

// Environment is the environment of the application that sent the webhook.
// Values other than the constants below are kept as sent.
type Environment string

// Known environments.
const (
	EnvironmentLive    Environment = "live"
	EnvironmentSandbox Environment = "sandbox"
)

// EventEnvelope holds the fields every webhook carries. Every event type
// embeds it.
//
// Fields are decoded leniently: a field that is missing or has an unexpected
// JSON type is left at its zero value instead of failing verification. Raw
// always holds the complete body.
type EventEnvelope struct {
	// EventID identifies the event. It stays the same across retries and
	// destinations, so use it to deduplicate deliveries.
	EventID string
	// WebhookType is the body's webhook_type (or event_type when the body has
	// no webhook_type).
	WebhookType Type
	// Timestamp is the signed delivery time, in unix seconds. Verification
	// checked it against the tolerance.
	Timestamp int64
	// CreatedAt is when the event was created, in unix seconds (0 when
	// absent).
	CreatedAt int64
	// ApplicationID is the Didit application the event belongs to.
	ApplicationID string
	// Environment is "live" or "sandbox".
	Environment Environment

	raw           []byte
	authenticated bool
}

// Type returns e.WebhookType.
func (e *EventEnvelope) Type() Type { return e.WebhookType }

// Envelope returns e. It lets every Event expose its envelope.
func (e *EventEnvelope) Envelope() *EventEnvelope { return e }

// Raw returns the verified body; see Event.
func (e *EventEnvelope) Raw() []byte { return e.raw }

// Authenticated reports whether a full-body signature matched; see Event.
func (e *EventEnvelope) Authenticated() bool { return e.authenticated }

func (*EventEnvelope) isEvent() {}

// SessionKind tells user (KYC) sessions from business (KYB) sessions.
type SessionKind string

// Known session kinds.
const (
	SessionKindUser     SessionKind = "user"
	SessionKindBusiness SessionKind = "business"
)

// SessionStatus is the status of a verification session.
type SessionStatus string

// Known session statuses.
const (
	SessionStatusNotStarted   SessionStatus = "Not Started"
	SessionStatusInProgress   SessionStatus = "In Progress"
	SessionStatusAwaitingUser SessionStatus = "Awaiting User"
	SessionStatusInReview     SessionStatus = "In Review"
	SessionStatusApproved     SessionStatus = "Approved"
	SessionStatusDeclined     SessionStatus = "Declined"
	SessionStatusResubmitted  SessionStatus = "Resubmitted"
	SessionStatusExpired      SessionStatus = "Expired"
	SessionStatusKYCExpired   SessionStatus = "Kyc Expired"
	SessionStatusAbandoned    SessionStatus = "Abandoned"
)

// SessionFields are the fields of the session events, status.updated and
// data.updated. Both event types embed it, so code that handles either can
// take a SessionFields.
type SessionFields struct {
	// SessionID is the session, a user (KYC) or business (KYB) session.
	SessionID string
	// BusinessSessionID is set on business sessions, where it equals
	// SessionID.
	BusinessSessionID string
	// SessionKind is "business" on business sessions. User sessions may omit
	// it, leaving it empty.
	SessionKind SessionKind
	// Status is the session status.
	Status SessionStatus
	// WorkflowID is the workflow the session was created with.
	WorkflowID string
	// WorkflowVersion is the workflow version the session runs (0 when
	// absent).
	WorkflowVersion int64
	// VendorData is the identifier you attached to the session.
	VendorData string
	// Metadata is the metadata you attached to the session, as JSON (nil when
	// absent).
	Metadata json.RawMessage
	// Decision is the session decision, the same shape as the response of
	// Sessions.RetrieveDecision. It is nil when the body has no decision
	// object.
	Decision *didit.SessionDecision
	// ResubmitInfo lists the steps the user must redo and why, as JSON. It is
	// present when Status is Resubmitted.
	ResubmitInfo json.RawMessage
}

// StatusUpdatedEvent is a status.updated webhook: a session changed status.
type StatusUpdatedEvent struct {
	EventEnvelope
	SessionFields
}

// DataUpdatedEvent is a data.updated webhook: the data of a session changed.
type DataUpdatedEvent struct {
	EventEnvelope
	SessionFields
}

// EntityStatus is the status of a user or business entity.
type EntityStatus string

// Known entity statuses.
const (
	EntityStatusActive  EntityStatus = "ACTIVE"
	EntityStatusFlagged EntityStatus = "FLAGGED"
	EntityStatusBlocked EntityStatus = "BLOCKED"
)

// UserFields are the fields of user.status.updated and user.data.updated.
type UserFields struct {
	// VendorUserID is the user's identifier.
	VendorUserID string
	// VendorData is the identifier you attached to the user.
	VendorData string
	// Status is the user's status.
	Status EntityStatus
}

// UserStatusUpdatedEvent is a user.status.updated webhook.
type UserStatusUpdatedEvent struct {
	EventEnvelope
	UserFields
}

// UserDataUpdatedEvent is a user.data.updated webhook.
type UserDataUpdatedEvent struct {
	EventEnvelope
	UserFields
}

// BusinessFields are the fields of business.status.updated and
// business.data.updated.
type BusinessFields struct {
	// VendorBusinessID is the business's identifier.
	VendorBusinessID string
	// VendorData is the identifier you attached to the business.
	VendorData string
	// Status is the business's status.
	Status EntityStatus
}

// BusinessStatusUpdatedEvent is a business.status.updated webhook.
type BusinessStatusUpdatedEvent struct {
	EventEnvelope
	BusinessFields
}

// BusinessDataUpdatedEvent is a business.data.updated webhook.
type BusinessDataUpdatedEvent struct {
	EventEnvelope
	BusinessFields
}

// ActivityCreatedEvent is an activity.created webhook. Didit has not
// published its payload beyond the envelope; decode Raw for other fields.
type ActivityCreatedEvent struct {
	EventEnvelope
}

// TransactionStatus is the monitoring verdict of a transaction.
type TransactionStatus string

// Known transaction statuses.
const (
	TransactionStatusApproved     TransactionStatus = "APPROVED"
	TransactionStatusInReview     TransactionStatus = "IN_REVIEW"
	TransactionStatusDeclined     TransactionStatus = "DECLINED"
	TransactionStatusAwaitingUser TransactionStatus = "AWAITING_USER"
)

// TransactionSeverity is the risk severity of a transaction.
type TransactionSeverity string

// Known transaction severities.
const (
	TransactionSeverityUnknown  TransactionSeverity = "UNKNOWN"
	TransactionSeverityLow      TransactionSeverity = "LOW"
	TransactionSeverityMedium   TransactionSeverity = "MEDIUM"
	TransactionSeverityHigh     TransactionSeverity = "HIGH"
	TransactionSeverityCritical TransactionSeverity = "CRITICAL"
)

// Direction is the direction of a transaction relative to its subject.
type Direction string

// Known directions.
const (
	DirectionInbound  Direction = "INBOUND"
	DirectionOutbound Direction = "OUTBOUND"
)

// TransactionFields are the fields of transaction.created and
// transaction.status.updated.
type TransactionFields struct {
	// TransactionID is Didit's transaction id.
	TransactionID string
	// TxnID is the transaction id you supplied.
	TxnID string
	// Status is the monitoring verdict.
	Status TransactionStatus
	// Score is the risk score accumulated by rules.
	Score float64
	// Severity is the risk severity, empty until something sets it.
	Severity TransactionSeverity
	// Amount is the amount as a decimal string, exactly as sent.
	Amount string
	// Currency is the currency code or crypto asset symbol of Amount.
	Currency string
	// Direction is the direction relative to the subject.
	Direction Direction
	// ActionRequired is the pending end-user action, as JSON (nil when
	// absent).
	ActionRequired json.RawMessage
}

// TransactionCreatedEvent is a transaction.created webhook.
type TransactionCreatedEvent struct {
	EventEnvelope
	TransactionFields
}

// TransactionStatusUpdatedEvent is a transaction.status.updated webhook.
type TransactionStatusUpdatedEvent struct {
	EventEnvelope
	TransactionFields
}

// TravelRuleStatus is the status of a Travel Rule exchange.
type TravelRuleStatus string

// Known Travel Rule statuses.
const (
	TravelRuleStatusAwaitingCounterparty           TravelRuleStatus = "AWAITING_COUNTERPARTY"
	TravelRuleStatusOnHold                         TravelRuleStatus = "ON_HOLD"
	TravelRuleStatusCompleted                      TravelRuleStatus = "COMPLETED"
	TravelRuleStatusFinished                       TravelRuleStatus = "FINISHED"
	TravelRuleStatusCancelled                      TravelRuleStatus = "CANCELLED"
	TravelRuleStatusExpired                        TravelRuleStatus = "EXPIRED"
	TravelRuleStatusCounterpartyMismatchedData     TravelRuleStatus = "COUNTERPARTY_MISMATCHED_DATA"
	TravelRuleStatusCounterpartyVASPNotFound       TravelRuleStatus = "COUNTERPARTY_VASP_NOT_FOUND"
	TravelRuleStatusCounterpartyVASPNotReachable   TravelRuleStatus = "COUNTERPARTY_VASP_NOT_REACHABLE"
	TravelRuleStatusUnconfirmedOwnership           TravelRuleStatus = "UNCONFIRMED_OWNERSHIP"
	TravelRuleStatusCounterpartyVASPGeneralDecline TravelRuleStatus = "COUNTERPARTY_VASP_GENERAL_DECLINE"
	TravelRuleStatusNotEnoughCounterpartyData      TravelRuleStatus = "NOT_ENOUGH_COUNTERPARTY_DATA"
	TravelRuleStatusNotApplicable                  TravelRuleStatus = "NOT_APPLICABLE"
)

// TravelRuleRail is the protocol a Travel Rule exchange runs on.
type TravelRuleRail string

// Known Travel Rule rails.
const (
	TravelRuleRailInternal TravelRuleRail = "INTERNAL"
	TravelRuleRailTRP      TravelRuleRail = "TRP"
	TravelRuleRailGTR      TravelRuleRail = "GTR"
	TravelRuleRailEmail    TravelRuleRail = "EMAIL"
)

// TravelRuleStatusUpdatedEvent is a travel_rule.status.updated webhook.
type TravelRuleStatusUpdatedEvent struct {
	EventEnvelope
	// TransactionID is Didit's transaction id.
	TransactionID string
	// TxnID is the transaction id you supplied.
	TxnID string
	// TravelRuleStatus is the status of the exchange.
	TravelRuleStatus TravelRuleStatus
	// Rail is the protocol of the exchange, empty when none was chosen.
	Rail TravelRuleRail
	// Direction is the direction relative to the subject.
	Direction Direction
}

// WorkflowComplianceChangedEvent is a workflow.compliance_changed webhook.
// Didit has not published its payload beyond the envelope; decode Raw for
// other fields.
type WorkflowComplianceChangedEvent struct {
	EventEnvelope
}

// KYBSearchStatus is the state of a business registry search.
type KYBSearchStatus string

// Known registry search states.
const (
	KYBSearchStatusPending  KYBSearchStatus = "pending"
	KYBSearchStatusResolved KYBSearchStatus = "resolved"
)

// KYBRegistrySearchResolvedEvent is a kyb.registry_search.resolved webhook:
// an asynchronous business registry search finished.
type KYBRegistrySearchResolvedEvent struct {
	EventEnvelope
	// RequestID is the search request, as returned by KYB.Search.
	RequestID string
	// VendorData is the identifier you attached to the search.
	VendorData string
	// Metadata is the metadata you attached to the search, as JSON (nil when
	// absent).
	Metadata json.RawMessage
	// SearchStatus is the state of the search.
	SearchStatus KYBSearchStatus
	// SearchResolved reports whether the search resolved.
	SearchResolved bool
	// KYBRegistry is the registry result with the candidate companies, as
	// JSON (nil when absent).
	KYBRegistry json.RawMessage
}

// UnknownEvent is a webhook whose type this package has no typed event for.
// Payload keeps the whole body so nothing is lost; Type returns its
// webhook_type.
type UnknownEvent struct {
	EventEnvelope
	// Payload is the parsed body. For Verify, numbers are json.Number values;
	// for VerifyParsed it is the map that was passed in.
	Payload map[string]any
}
