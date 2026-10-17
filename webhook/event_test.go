package webhook_test

import (
	"encoding/json"
	"reflect"
	"testing"

	didit "github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/webhook"
)

// verifyPayload signs payload and returns the event Verify produces, after
// checking that VerifyParsed produces an event of the same type and fields.
func verifyPayload(t *testing.T, payload map[string]any) webhook.Event {
	t.Helper()
	body, h := signedBody(t, payload, testSecret)
	ev := mustVerify(t, body, h, testSecret, testNow)
	if !ev.Authenticated() {
		t.Fatal("not authenticated")
	}
	parsed, err := webhook.VerifyParsed(decodeUseNumber(t, body), h, testSecret, testNow)
	if err != nil {
		t.Fatalf("VerifyParsed: %v", err)
	}
	if reflect.TypeOf(parsed) != reflect.TypeOf(ev) {
		t.Fatalf("VerifyParsed returned %T, Verify %T", parsed, ev)
	}
	return ev
}

func checkEnvelope(t *testing.T, ev webhook.Event, typ webhook.Type) {
	t.Helper()
	env := ev.Envelope()
	want := webhook.EventEnvelope{
		EventID:       "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		WebhookType:   typ,
		Timestamp:     testTimestamp,
		CreatedAt:     testTimestamp - 6,
		ApplicationID: "0b5e8d2c-1f4a-4c3b-9d6e-2a7f8c1b3e5d",
		Environment:   webhook.EnvironmentLive,
	}
	got := *env
	if got.EventID != want.EventID || got.WebhookType != want.WebhookType || got.Timestamp != want.Timestamp ||
		got.CreatedAt != want.CreatedAt || got.ApplicationID != want.ApplicationID || got.Environment != want.Environment {
		t.Fatalf("envelope = %+v, want %+v", got, want)
	}
	if ev.Type() != typ {
		t.Fatalf("Type() = %q, want %q", ev.Type(), typ)
	}
}

var decisionJSON = map[string]any{
	"session_id":   "11111111-2222-3333-4444-555555555555",
	"session_kind": "user",
	"status":       "Approved",
	"features":     []any{"ID_VERIFICATION", "LIVENESS"},
	"liveness_checks": []any{
		map[string]any{"node_id": "liveness_1", "status": "Approved", "score": 95.4},
	},
	"created_at": "2026-05-17T08:54:25.443172Z",
}

func TestSessionEvents(t *testing.T) {
	for _, typ := range []webhook.Type{webhook.TypeStatusUpdated, webhook.TypeDataUpdated} {
		t.Run(string(typ), func(t *testing.T) {
			p := with(envelope(string(typ)),
				"session_id", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				"business_session_id", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				"session_kind", "business",
				"status", "Resubmitted",
				"workflow_id", "66666666-7777-8888-9999-000000000000",
				"workflow_version", 4,
				"vendor_data", "user_42",
				"metadata", map[string]any{"tier": "premium", "seats": 3},
				"decision", decisionJSON,
				"resubmit_info", map[string]any{
					"nodes_to_resubmit": []any{map[string]any{"node_id": "feature_poa", "feature": "PROOF_OF_ADDRESS"}},
					"reasons":           map[string]any{"feature_poa": "Poor document quality"},
				},
				"sandbox_scenario", nil,
			)
			ev := verifyPayload(t, p)
			checkEnvelope(t, ev, typ)

			var s webhook.SessionFields
			switch e := ev.(type) {
			case *webhook.StatusUpdatedEvent:
				s = e.SessionFields
			case *webhook.DataUpdatedEvent:
				s = e.SessionFields
			default:
				t.Fatalf("got %T", ev)
			}
			if s.SessionID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" || s.BusinessSessionID != s.SessionID ||
				s.SessionKind != webhook.SessionKindBusiness || s.Status != webhook.SessionStatusResubmitted ||
				s.WorkflowID != "66666666-7777-8888-9999-000000000000" || s.WorkflowVersion != 4 || s.VendorData != "user_42" {
				t.Fatalf("session fields = %+v", s)
			}
			if string(s.Metadata) != `{"seats":3,"tier":"premium"}` {
				t.Fatalf("Metadata = %s", s.Metadata)
			}
			if string(s.ResubmitInfo) != `{"nodes_to_resubmit":[{"feature":"PROOF_OF_ADDRESS","node_id":"feature_poa"}],"reasons":{"feature_poa":"Poor document quality"}}` {
				t.Fatalf("ResubmitInfo = %s", s.ResubmitInfo)
			}
			// The decision type is generated; compare with a direct decode
			// instead of reading its fields.
			raw, _ := json.Marshal(decisionJSON)
			want := new(didit.SessionsRetrieveDecisionResponse)
			if err := json.Unmarshal(raw, want); err != nil {
				t.Fatal(err)
			}
			if s.Decision == nil {
				t.Fatal("Decision is nil")
			}
			gotJSON, _ := json.Marshal(s.Decision)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("Decision = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestSessionEventWithoutOptionalFields(t *testing.T) {
	ev := verifyPayload(t, with(envelope("status.updated"), "session_id", "s", "status", "Approved", "decision", nil))
	e := ev.(*webhook.StatusUpdatedEvent)
	if e.Decision != nil || e.Metadata != nil || e.ResubmitInfo != nil || e.SessionKind != "" || e.WorkflowVersion != 0 {
		t.Fatalf("unexpected fields: %+v", e.SessionFields)
	}
}

func TestEntityEvents(t *testing.T) {
	for _, typ := range []webhook.Type{webhook.TypeUserStatusUpdated, webhook.TypeUserDataUpdated} {
		t.Run(string(typ), func(t *testing.T) {
			ev := verifyPayload(t, with(envelope(string(typ)),
				"vendor_user_id", "77777777-2222-3333-4444-555555555555",
				"vendor_data", "customer-1",
				"status", "BLOCKED",
				"previous_status", "ACTIVE",
			))
			checkEnvelope(t, ev, typ)
			var u webhook.UserFields
			switch e := ev.(type) {
			case *webhook.UserStatusUpdatedEvent:
				u = e.UserFields
			case *webhook.UserDataUpdatedEvent:
				u = e.UserFields
			default:
				t.Fatalf("got %T", ev)
			}
			want := webhook.UserFields{VendorUserID: "77777777-2222-3333-4444-555555555555", VendorData: "customer-1", Status: webhook.EntityStatusBlocked}
			if u != want {
				t.Fatalf("user fields = %+v", u)
			}
		})
	}
	for _, typ := range []webhook.Type{webhook.TypeBusinessStatusUpdated, webhook.TypeBusinessDataUpdated} {
		t.Run(string(typ), func(t *testing.T) {
			ev := verifyPayload(t, with(envelope(string(typ)),
				"vendor_business_id", "biz-9",
				"vendor_data", "company-1",
				"status", "FLAGGED",
			))
			checkEnvelope(t, ev, typ)
			var b webhook.BusinessFields
			switch e := ev.(type) {
			case *webhook.BusinessStatusUpdatedEvent:
				b = e.BusinessFields
			case *webhook.BusinessDataUpdatedEvent:
				b = e.BusinessFields
			default:
				t.Fatalf("got %T", ev)
			}
			want := webhook.BusinessFields{VendorBusinessID: "biz-9", VendorData: "company-1", Status: webhook.EntityStatusFlagged}
			if b != want {
				t.Fatalf("business fields = %+v", b)
			}
		})
	}
}

func TestTransactionEvents(t *testing.T) {
	for _, typ := range []webhook.Type{webhook.TypeTransactionCreated, webhook.TypeTransactionStatusUpdated} {
		t.Run(string(typ), func(t *testing.T) {
			ev := verifyPayload(t, with(envelope(string(typ)),
				"transaction_id", "22222222-3333-4444-5555-666666666666",
				"txn_id", "finance0001",
				"status", "IN_REVIEW",
				"score", 12.5,
				"severity", "HIGH",
				"amount", "1200.00",
				"currency", "EUR",
				"direction", "OUTBOUND",
				"action_required", map[string]any{"type": "wallet_ownership", "url": "https://example.com/w"},
			))
			checkEnvelope(t, ev, typ)
			var tx webhook.TransactionFields
			switch e := ev.(type) {
			case *webhook.TransactionCreatedEvent:
				tx = e.TransactionFields
			case *webhook.TransactionStatusUpdatedEvent:
				tx = e.TransactionFields
			default:
				t.Fatalf("got %T", ev)
			}
			if tx.TransactionID != "22222222-3333-4444-5555-666666666666" || tx.TxnID != "finance0001" ||
				tx.Status != webhook.TransactionStatusInReview || tx.Score != 12.5 || tx.Severity != webhook.TransactionSeverityHigh ||
				tx.Amount != "1200.00" || tx.Currency != "EUR" || tx.Direction != webhook.DirectionOutbound {
				t.Fatalf("transaction fields = %+v", tx)
			}
			if string(tx.ActionRequired) != `{"type":"wallet_ownership","url":"https://example.com/w"}` {
				t.Fatalf("ActionRequired = %s", tx.ActionRequired)
			}
		})
	}
}

func TestTransactionAmountAsNumberKeepsDigits(t *testing.T) {
	body := []byte(`{"webhook_type":"transaction.created","timestamp":1760702400,"amount":1500.10,"score":7,"action_required":false}`)
	ev := mustVerify(t, body, signHeaders(t, body, testSecret), testSecret, testNow)
	e := ev.(*webhook.TransactionCreatedEvent)
	if e.Amount != "1500.10" || e.Score != 7 || string(e.ActionRequired) != "false" {
		t.Fatalf("Amount = %q, Score = %v, ActionRequired = %s", e.Amount, e.Score, e.ActionRequired)
	}
}

func TestTravelRuleEvent(t *testing.T) {
	ev := verifyPayload(t, with(envelope("travel_rule.status.updated"),
		"transaction_id", "22222222-3333-4444-5555-666666666666",
		"txn_id", "trv-2026-07-05-001",
		"travel_rule_status", "COMPLETED",
		"rail", "INTERNAL",
		"direction", "INBOUND",
	))
	checkEnvelope(t, ev, webhook.TypeTravelRuleStatusUpdated)
	e, ok := ev.(*webhook.TravelRuleStatusUpdatedEvent)
	if !ok {
		t.Fatalf("got %T", ev)
	}
	if e.TransactionID != "22222222-3333-4444-5555-666666666666" || e.TxnID != "trv-2026-07-05-001" ||
		e.TravelRuleStatus != webhook.TravelRuleStatusCompleted || e.Rail != webhook.TravelRuleRailInternal || e.Direction != webhook.DirectionInbound {
		t.Fatalf("travel rule fields = %+v", e)
	}
}

func TestEnvelopeOnlyEvents(t *testing.T) {
	cases := map[webhook.Type]reflect.Type{
		webhook.TypeActivityCreated:           reflect.TypeOf(&webhook.ActivityCreatedEvent{}),
		webhook.TypeWorkflowComplianceChanged: reflect.TypeOf(&webhook.WorkflowComplianceChangedEvent{}),
	}
	for typ, want := range cases {
		t.Run(string(typ), func(t *testing.T) {
			ev := verifyPayload(t, with(envelope(string(typ)), "workflow_id", "w-1"))
			checkEnvelope(t, ev, typ)
			if reflect.TypeOf(ev) != want {
				t.Fatalf("got %T, want %v", ev, want)
			}
			var extra struct {
				WorkflowID string `json:"workflow_id"`
			}
			if err := json.Unmarshal(ev.Raw(), &extra); err != nil || extra.WorkflowID != "w-1" {
				t.Fatalf("Raw() does not carry the other fields: %v %+v", err, extra)
			}
		})
	}
}

func TestKYBRegistrySearchResolvedEvent(t *testing.T) {
	registry := map[string]any{"companies": []any{map[string]any{"name": "Tesco PLC"}}}
	ev := verifyPayload(t, with(envelope("kyb.registry_search.resolved"),
		"request_id", "5f2d8c1b-7e4a-4b9c-9f0d-3a6e8c2b1d4f",
		"vendor_data", "company-1234",
		"metadata", nil,
		"search_status", "resolved",
		"search_resolved", true,
		"kyb_registry", registry,
	))
	checkEnvelope(t, ev, webhook.TypeKYBRegistrySearchResolved)
	e, ok := ev.(*webhook.KYBRegistrySearchResolvedEvent)
	if !ok {
		t.Fatalf("got %T", ev)
	}
	if e.RequestID != "5f2d8c1b-7e4a-4b9c-9f0d-3a6e8c2b1d4f" || e.VendorData != "company-1234" || string(e.Metadata) != "null" ||
		e.SearchStatus != webhook.KYBSearchStatusResolved || !e.SearchResolved ||
		string(e.KYBRegistry) != `{"companies":[{"name":"Tesco PLC"}]}` {
		t.Fatalf("fields = %+v", e)
	}

	// The callback names its type event_type instead of webhook_type.
	p := with(envelope(""), "event_type", "kyb.registry_search.resolved", "search_status", "pending")
	delete(p, "webhook_type")
	ev = verifyPayload(t, p)
	if e, ok := ev.(*webhook.KYBRegistrySearchResolvedEvent); !ok || e.SearchStatus != webhook.KYBSearchStatusPending {
		t.Fatalf("got %T %+v", ev, ev)
	}
}

func TestUnknownEvents(t *testing.T) {
	for _, typ := range []string{"auth.user.created", "transaction_rule_suggestions.created", "status.updated.v2", ""} {
		t.Run(typ, func(t *testing.T) {
			ev := verifyPayload(t, with(envelope(typ), "user_id", "u-1", "count", json.Number("9007199254740993")))
			e, ok := ev.(*webhook.UnknownEvent)
			if !ok {
				t.Fatalf("got %T", ev)
			}
			checkEnvelope(t, ev, webhook.Type(typ))
			if e.Payload["user_id"] != "u-1" || e.Payload["count"] != json.Number("9007199254740993") {
				t.Fatalf("Payload = %v", e.Payload)
			}
		})
	}
	t.Run("no webhook_type", func(t *testing.T) {
		p := envelope("")
		delete(p, "webhook_type")
		if ev := verifyPayload(t, p); ev.Type() != "" {
			t.Fatalf("Type() = %q", ev.Type())
		}
	})
	t.Run("non-string webhook_type", func(t *testing.T) {
		ev := verifyPayload(t, with(envelope(""), "webhook_type", 5))
		if _, ok := ev.(*webhook.UnknownEvent); !ok || ev.Type() != "" {
			t.Fatalf("got %T with Type() %q", ev, ev.Type())
		}
	})
}

// Odd values in fields never make verification fail after the signature
// passed; they are left at their zero value or converted when unambiguous.
func TestLenientFields(t *testing.T) {
	t.Run("session", func(t *testing.T) {
		ev := verifyPayload(t, with(envelope("status.updated"),
			"event_id", 12345,
			"created_at", "2026-05-17T08:54:25Z",
			"environment", "staging",
			"session_id", []any{"x"},
			"status", nil,
			"workflow_version", "7",
			"vendor_data", 42.5,
			"metadata", "not an object",
			"decision", "not an object",
			"resubmit_info", []any{1, 2.0},
		))
		e := ev.(*webhook.StatusUpdatedEvent)
		if e.EventID != "12345" || e.CreatedAt != 1779008065 || e.Environment != "staging" {
			t.Fatalf("envelope = %+v", e.EventEnvelope)
		}
		if e.SessionID != "" || e.Status != "" || e.WorkflowVersion != 7 || e.VendorData != "42.5" ||
			string(e.Metadata) != `"not an object"` || e.Decision != nil || string(e.ResubmitInfo) != "[1,2]" {
			t.Fatalf("session fields = %+v", e.SessionFields)
		}
	})
	t.Run("decision with odd field types", func(t *testing.T) {
		ev := verifyPayload(t, with(envelope("status.updated"),
			"decision", map[string]any{"session_id": 5, "session_number": "x", "status": "Approved", "unknown": true},
		))
		if ev.(*webhook.StatusUpdatedEvent).Decision == nil {
			t.Fatal("Decision is nil")
		}
	})
	t.Run("transaction", func(t *testing.T) {
		ev := verifyPayload(t, with(envelope("transaction.created"),
			"score", "high",
			"amount", true,
			"workflow_version", 1.5,
			"created_at", map[string]any{},
		))
		e := ev.(*webhook.TransactionCreatedEvent)
		if e.Score != 0 || e.Amount != "" || e.CreatedAt != 0 {
			t.Fatalf("fields = %+v", e)
		}
	})
	t.Run("numeric strings and floats", func(t *testing.T) {
		ev := verifyPayload(t, with(envelope("transaction.created"), "score", "42.5", "created_at", 1.7607024e9))
		e := ev.(*webhook.TransactionCreatedEvent)
		if e.Score != 42.5 || e.CreatedAt != 1760702400 {
			t.Fatalf("Score = %v, CreatedAt = %d", e.Score, e.CreatedAt)
		}
		ev = verifyPayload(t, with(envelope("transaction.created"), "score", "NaN", "created_at", "1e3", "search_resolved", "true"))
		if e := ev.(*webhook.TransactionCreatedEvent); e.Score != 0 || e.CreatedAt != 0 {
			t.Fatalf("Score = %v, CreatedAt = %d", e.Score, e.CreatedAt)
		}
	})
	t.Run("out of range integers", func(t *testing.T) {
		ev := verifyPayload(t, with(envelope("status.updated"), "workflow_version", json.Number("99999999999999999999"), "created_at", 1e300))
		e := ev.(*webhook.StatusUpdatedEvent)
		if e.WorkflowVersion != 0 || e.CreatedAt != 0 {
			t.Fatalf("WorkflowVersion = %d, CreatedAt = %d", e.WorkflowVersion, e.CreatedAt)
		}
	})
	t.Run("parsed float64 values", func(t *testing.T) {
		body, h := signedBody(t, with(envelope("transaction.created"),
			"txn_id", 1234, "amount", 0.25, "score", 3, "transaction_id", 1e300), testSecret)
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		ev, err := webhook.VerifyParsed(m, h, testSecret, testNow)
		if err != nil {
			t.Fatal(err)
		}
		e := ev.(*webhook.TransactionCreatedEvent)
		if e.TxnID != "1234" || e.Amount != "0.25" || e.Score != 3 || e.TransactionID != "1e+300" || e.Timestamp != testTimestamp {
			t.Fatalf("fields = %+v", e)
		}
	})
}

func TestEventInterfaceOnEveryType(t *testing.T) {
	// Every concrete type satisfies Event through its embedded envelope.
	events := []webhook.Event{
		&webhook.StatusUpdatedEvent{}, &webhook.DataUpdatedEvent{},
		&webhook.UserStatusUpdatedEvent{}, &webhook.UserDataUpdatedEvent{},
		&webhook.BusinessStatusUpdatedEvent{}, &webhook.BusinessDataUpdatedEvent{},
		&webhook.ActivityCreatedEvent{}, &webhook.TransactionCreatedEvent{},
		&webhook.TransactionStatusUpdatedEvent{}, &webhook.TravelRuleStatusUpdatedEvent{},
		&webhook.WorkflowComplianceChangedEvent{}, &webhook.KYBRegistrySearchResolvedEvent{},
		&webhook.UnknownEvent{},
	}
	for _, ev := range events {
		if ev.Authenticated() || ev.Raw() != nil || ev.Type() != "" || ev.Envelope() == nil {
			t.Fatalf("%T: unexpected zero value behaviour", ev)
		}
	}
}
