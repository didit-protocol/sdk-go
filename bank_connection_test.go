package didit_test

import (
	"context"
	"testing"

	"github.com/didit-protocol/sdk-go"
)

// A deleted connection is a minimal record: the API sends only the id, status,
// reason, dates and erasure block, and the other fields must not fail decoding.
func TestBankGetConnectionDeletedIsMinimal(t *testing.T) {
	const body = `{"id":"5e1d2a70-3b94-4c1f-8a6e-d2f07b9c1e35","status":"deleted","status_reason":"consent_expired","created_at":"2026-11-21T12:03:02Z","erasure":{"status":"erased","requested_at":"2026-11-21T15:10:00Z"}}`
	client, mock := newMockClient(t, 200, "application/json", body)

	res, err := client.Bank.GetConnection(context.Background(), "id-1")
	if err != nil {
		t.Fatal(err)
	}
	req := mock.single(t)
	req.expect(t, "GET", "/v3/bank/connections/id-1/")
	req.expectAPIKey(t)

	if res.ID != "5e1d2a70-3b94-4c1f-8a6e-d2f07b9c1e35" ||
		res.Status != didit.BankConnectionStatusDeleted ||
		res.StatusReason != didit.BankConnectionStatusReasonConsentExpired ||
		res.Erasure.Status != didit.BankConnectionErasureStatusErased {
		t.Fatalf("deleted connection = %+v", res)
	}
	if res.CreatedAt.IsZero() || res.Erasure.RequestedAt.IsZero() {
		t.Fatalf("dates = %v, %v", res.CreatedAt, res.Erasure.RequestedAt)
	}
	if len(res.Accounts) != 0 || res.Consent.Status != "" || res.Sync.Status != "" {
		t.Fatalf("omitted fields were not left empty: accounts=%d consent=%q sync=%q", len(res.Accounts), res.Consent.Status, res.Sync.Status)
	}
}
