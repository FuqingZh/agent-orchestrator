package store_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestWorkflowRunSingleDurableClaimUnderConcurrency(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "demo")
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	rec := domain.WorkflowRunRecord{
		ProjectID:        "demo",
		IssueID:          "github:acme/demo#42",
		IssueIdentifier:  "acme/demo#42",
		WorkflowRevision: "rev-1",
		UpdatedAt:        now,
	}

	var winners atomic.Int64
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.TryClaimWorkflowIssue(ctx, rec)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if ok {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := winners.Load(); got != 1 {
		t.Fatalf("claim winners = %d, want exactly 1", got)
	}
}

func TestWorkflowRunClaimSurvivesRestartUntilReleased(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	first, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, first, "demo")
	now := time.Now().UTC().Truncate(time.Second)
	rec := domain.WorkflowRunRecord{
		ProjectID:        "demo",
		IssueID:          "github:acme/demo#42",
		IssueIdentifier:  "acme/demo#42",
		WorkflowRevision: "rev-1",
		UpdatedAt:        now,
	}
	if ok, err := first.TryClaimWorkflowIssue(ctx, rec); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if ok, err := reopened.TryClaimWorkflowIssue(ctx, rec); err != nil || ok {
		t.Fatalf("claim after restart: ok=%v err=%v, want durable conflict", ok, err)
	}
	if ok, err := reopened.ReleaseWorkflowIssue(ctx, rec.ProjectID, rec.IssueID, "terminal", now.Add(time.Second)); err != nil || !ok {
		t.Fatalf("release: ok=%v err=%v", ok, err)
	}
	rec.WorkflowRevision = "rev-2"
	rec.Attempt = 2
	rec.UpdatedAt = now.Add(2 * time.Second)
	if ok, err := reopened.TryClaimWorkflowIssue(ctx, rec); err != nil || !ok {
		t.Fatalf("reclaim released issue: ok=%v err=%v", ok, err)
	}
}

func TestWorkflowRunRetryFactsAndCDC(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "demo")
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	issueID := domain.IssueID("github:acme/demo#42")
	claim := domain.WorkflowRunRecord{
		ProjectID:        "demo",
		IssueID:          issueID,
		IssueIdentifier:  "acme/demo#42",
		WorkflowRevision: "rev-1",
		UpdatedAt:        now,
	}
	if ok, err := s.TryClaimWorkflowIssue(ctx, claim); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	session := sampleRecord("demo")
	session.IssueID = issueID
	session, err := s.CreateSession(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.BindWorkflowIssueSession(ctx, "demo", issueID, session.ID, now.Add(time.Second)); err != nil || !ok {
		t.Fatalf("bind: ok=%v err=%v", ok, err)
	}
	due := now.Add(10 * time.Second)
	if ok, err := s.QueueWorkflowRetry(ctx, domain.WorkflowRunRecord{
		ProjectID:        "demo",
		IssueID:          issueID,
		WorkflowRevision: "rev-1",
		Attempt:          1,
		RetryDueAt:       due,
		LastError:        "agent process exited",
		UpdatedAt:        now.Add(2 * time.Second),
	}); err != nil || !ok {
		t.Fatalf("queue retry: ok=%v err=%v", ok, err)
	}

	got, ok, err := s.GetWorkflowRun(ctx, "demo", issueID)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.SessionID != session.ID || got.State != domain.WorkflowRunRetryQueued ||
		got.WorkflowRevision != "rev-1" || got.Attempt != 1 ||
		!got.RetryDueAt.Equal(due) || got.LastError != "agent process exited" {
		t.Fatalf("workflow retry facts = %+v", got)
	}
	dueRows, err := s.ListDueWorkflowRetries(ctx, due)
	if err != nil || len(dueRows) != 1 || dueRows[0].IssueID != issueID {
		t.Fatalf("due retries = %+v err=%v", dueRows, err)
	}
	retryClaim := claim
	retryClaim.Attempt = 1
	retryClaim.WorkflowRevision = "rev-2"
	retryClaim.UpdatedAt = due
	if claimed, err := s.TryClaimDueWorkflowRetry(ctx, retryClaim, due.Add(-time.Nanosecond)); err != nil || claimed {
		t.Fatalf("early retry claim: claimed=%v err=%v", claimed, err)
	}
	if claimed, err := s.TryClaimDueWorkflowRetry(ctx, retryClaim, due); err != nil || !claimed {
		t.Fatalf("due retry claim: claimed=%v err=%v", claimed, err)
	}
	got, ok, err = s.GetWorkflowRun(ctx, "demo", issueID)
	if err != nil || !ok || got.State != domain.WorkflowRunClaimed ||
		got.Attempt != 1 || got.WorkflowRevision != "rev-2" ||
		got.SessionID != "" || !got.RetryDueAt.IsZero() {
		t.Fatalf("claimed retry facts = %+v ok=%v err=%v", got, ok, err)
	}

	events, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var workflowEvents int
	for _, event := range events {
		if event.SessionID == string(session.ID) &&
			strings.Contains(string(event.Payload), `"workflowIssueId":"github:acme/demo#42"`) {
			workflowEvents++
		}
	}
	if workflowEvents != 2 {
		t.Fatalf("workflow CDC events = %d, want bind + retry transitions from triggers", workflowEvents)
	}
}
