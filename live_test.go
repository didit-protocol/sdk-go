package didit_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/didit-protocol/sdk-go"
	"github.com/didit-protocol/sdk-go/option"
)

// Live smoke tests against a sandbox application. They run only when
// DIDIT_SANDBOX_API_KEY is set (never in default CI) and only read data or
// create a sandbox session, which costs nothing.
//
//	DIDIT_SANDBOX_API_KEY=... DIDIT_SANDBOX_WORKFLOW_ID=... go test -run Live ./...
func liveClient(t *testing.T) *didit.Client {
	t.Helper()
	key := os.Getenv("DIDIT_SANDBOX_API_KEY")
	if key == "" {
		t.Skip("DIDIT_SANDBOX_API_KEY is not set")
	}
	opts := []option.RequestOption{option.WithAPIKey(key), option.WithTimeout(30 * time.Second)}
	if base := os.Getenv("DIDIT_SANDBOX_BASE_URL"); base != "" {
		opts = append(opts, option.WithBaseURL(base))
	}
	return didit.NewClient(opts...)
}

func TestLiveListSessions(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	page, err := client.Sessions.List(ctx, didit.SessionsListParams{Limit: didit.Int(2)})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d sessions, next page: %v", page.Count, page.HasNextPage())
	if page.HasNextPage() {
		next, err := page.GetNextPage(ctx)
		if err != nil {
			t.Fatalf("next page: %v", err)
		}
		t.Logf("second page: %d results", len(next.Results))
	}
}

func TestLiveWorkflowsAndBalance(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := client.Workflows.List(ctx, didit.WorkflowsListParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Billing.RetrieveBalance(ctx); err != nil {
		var denied *didit.PermissionDeniedError
		if !errors.As(err, &denied) {
			t.Fatal(err)
		}
		t.Log("the key cannot read the balance:", err)
	}
}

func TestLiveCreateSandboxSession(t *testing.T) {
	client := liveClient(t)
	workflowID := os.Getenv("DIDIT_SANDBOX_WORKFLOW_ID")
	if workflowID == "" {
		t.Skip("DIDIT_SANDBOX_WORKFLOW_ID is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	session, err := client.Sessions.Create(ctx, didit.SessionsCreateParams{
		WorkflowID: workflowID,
		VendorData: didit.String("sdk-go-live-test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.Sessions.RetrieveDecision(ctx, session.SessionID, didit.SessionsRetrieveDecisionParams{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("session %s status %s", decision.SessionID, decision.Status)
}
