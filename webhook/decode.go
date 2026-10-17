package webhook

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strconv"
	"time"

	didit "github.com/didit-protocol/sdk-go"
)

// newEvent builds the typed event for a verified body. Every field is read
// leniently: a missing field or one with an unexpected JSON type is left at
// its zero value, because the signature already passed and the whole body
// stays available through Raw.
func newEvent(body map[string]any, raw []byte, authenticated bool) Event {
	typ, ok := body["webhook_type"].(string)
	if _, present := body["webhook_type"]; !present {
		// The kyb.registry_search.resolved callback names its type event_type.
		typ, ok = body["event_type"].(string)
	}
	if !ok {
		typ = ""
	}
	env := EventEnvelope{
		EventID:       stringField(body, "event_id"),
		WebhookType:   Type(typ),
		Timestamp:     intField(body, "timestamp"),
		CreatedAt:     unixField(body, "created_at"),
		ApplicationID: stringField(body, "application_id"),
		Environment:   Environment(stringField(body, "environment")),
		raw:           raw,
		authenticated: authenticated,
	}

	switch env.WebhookType {
	case TypeStatusUpdated:
		return &StatusUpdatedEvent{EventEnvelope: env, SessionFields: sessionFields(body)}
	case TypeDataUpdated:
		return &DataUpdatedEvent{EventEnvelope: env, SessionFields: sessionFields(body)}
	case TypeUserStatusUpdated:
		return &UserStatusUpdatedEvent{EventEnvelope: env, UserFields: userFields(body)}
	case TypeUserDataUpdated:
		return &UserDataUpdatedEvent{EventEnvelope: env, UserFields: userFields(body)}
	case TypeBusinessStatusUpdated:
		return &BusinessStatusUpdatedEvent{EventEnvelope: env, BusinessFields: businessFields(body)}
	case TypeBusinessDataUpdated:
		return &BusinessDataUpdatedEvent{EventEnvelope: env, BusinessFields: businessFields(body)}
	case TypeActivityCreated:
		return &ActivityCreatedEvent{EventEnvelope: env}
	case TypeTransactionCreated:
		return &TransactionCreatedEvent{EventEnvelope: env, TransactionFields: transactionFields(body)}
	case TypeTransactionStatusUpdated:
		return &TransactionStatusUpdatedEvent{EventEnvelope: env, TransactionFields: transactionFields(body)}
	case TypeTravelRuleStatusUpdated:
		return &TravelRuleStatusUpdatedEvent{
			EventEnvelope:    env,
			TransactionID:    stringField(body, "transaction_id"),
			TxnID:            stringField(body, "txn_id"),
			TravelRuleStatus: TravelRuleStatus(stringField(body, "travel_rule_status")),
			Rail:             TravelRuleRail(stringField(body, "rail")),
			Direction:        Direction(stringField(body, "direction")),
		}
	case TypeWorkflowComplianceChanged:
		return &WorkflowComplianceChangedEvent{EventEnvelope: env}
	case TypeKYBRegistrySearchResolved:
		return &KYBRegistrySearchResolvedEvent{
			EventEnvelope:  env,
			RequestID:      stringField(body, "request_id"),
			VendorData:     stringField(body, "vendor_data"),
			Metadata:       rawField(body, "metadata"),
			SearchStatus:   KYBSearchStatus(stringField(body, "search_status")),
			SearchResolved: boolField(body, "search_resolved"),
			KYBRegistry:    rawField(body, "kyb_registry"),
		}
	default:
		return &UnknownEvent{EventEnvelope: env, Payload: body}
	}
}

func sessionFields(body map[string]any) SessionFields {
	return SessionFields{
		SessionID:         stringField(body, "session_id"),
		BusinessSessionID: stringField(body, "business_session_id"),
		SessionKind:       SessionKind(stringField(body, "session_kind")),
		Status:            SessionStatus(stringField(body, "status")),
		WorkflowID:        stringField(body, "workflow_id"),
		WorkflowVersion:   intField(body, "workflow_version"),
		VendorData:        stringField(body, "vendor_data"),
		Metadata:          rawField(body, "metadata"),
		Decision:          decisionField(body),
		ResubmitInfo:      rawField(body, "resubmit_info"),
	}
}

func userFields(body map[string]any) UserFields {
	return UserFields{
		VendorUserID: stringField(body, "vendor_user_id"),
		VendorData:   stringField(body, "vendor_data"),
		Status:       EntityStatus(stringField(body, "status")),
	}
}

func businessFields(body map[string]any) BusinessFields {
	return BusinessFields{
		VendorBusinessID: stringField(body, "vendor_business_id"),
		VendorData:       stringField(body, "vendor_data"),
		Status:           EntityStatus(stringField(body, "status")),
	}
}

func transactionFields(body map[string]any) TransactionFields {
	return TransactionFields{
		TransactionID:  stringField(body, "transaction_id"),
		TxnID:          stringField(body, "txn_id"),
		Status:         TransactionStatus(stringField(body, "status")),
		Score:          floatField(body, "score"),
		Severity:       TransactionSeverity(stringField(body, "severity")),
		Amount:         stringField(body, "amount"),
		Currency:       stringField(body, "currency"),
		Direction:      Direction(stringField(body, "direction")),
		ActionRequired: rawField(body, "action_required"),
	}
}

// stringField returns a string field. A number is returned as its JSON text,
// so that an amount or id sent as a number is not lost; any other type
// gives "".
func stringField(body map[string]any, key string) string {
	switch v := body[key].(type) {
	case string:
		return v
	case json.Number:
		return string(v)
	case float64:
		if n, ok := integralFloat(v); ok && math.Abs(v) <= 1<<53 {
			return n.String()
		}
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return strconv.FormatFloat(v, 'g', -1, 64)
		}
	}
	return ""
}

// intField returns an integer field: a JSON integer, an integral float or a
// string holding an integer. Anything else, including values outside the
// int64 range, gives 0.
func intField(body map[string]any, key string) int64 {
	v := body[key]
	if s, ok := v.(string); ok {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	if n, ok := integerValue(v); ok && n.IsInt64() {
		return n.Int64()
	}
	if f, ok := floatValue(v); ok && f == math.Trunc(f) && math.Abs(f) < 1<<63 {
		return int64(f)
	}
	return 0
}

// floatField returns a numeric field: a JSON number or a string holding one.
// Anything else gives 0.
func floatField(body map[string]any, key string) float64 {
	f, _ := floatValue(body[key])
	return f
}

func floatValue(v any) (float64, bool) {
	var f float64
	var err error
	switch v := v.(type) {
	case json.Number:
		f, err = strconv.ParseFloat(string(v), 64)
	case string:
		f, err = strconv.ParseFloat(v, 64)
	case float64:
		f = v
	case float32:
		f = float64(v)
	default:
		n, ok := integerValue(v)
		if !ok {
			return 0, false
		}
		f, _ = new(big.Float).SetInt(n).Float64()
	}
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// unixField returns a time field as unix seconds: an integer, or an RFC 3339
// string. Anything else gives 0.
func unixField(body map[string]any, key string) int64 {
	if s, ok := body[key].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.Unix()
		}
	}
	return intField(body, key)
}

func boolField(body map[string]any, key string) bool {
	b, _ := body[key].(bool)
	return b
}

// rawField returns a field as JSON in canonical form, or nil when the field
// is absent.
func rawField(body map[string]any, key string) json.RawMessage {
	v, ok := body[key]
	if !ok {
		return nil
	}
	b, err := canonicalJSON(v)
	if err != nil {
		return nil
	}
	return b
}

// decisionField decodes the decision object into the generated decision type.
// A field of the decision with an unexpected type is left at its zero value
// (json.Unmarshal fills the others); a decision that is not an object gives
// nil.
func decisionField(body map[string]any) *didit.SessionsRetrieveDecisionResponse {
	obj, ok := body["decision"].(map[string]any)
	if !ok {
		return nil
	}
	b, err := canonicalJSON(obj)
	if err != nil {
		return nil
	}
	d := new(didit.SessionsRetrieveDecisionResponse)
	if err := json.Unmarshal(b, d); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return nil
		}
	}
	return d
}
