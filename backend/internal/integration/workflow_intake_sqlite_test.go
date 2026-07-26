package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/trackerintake"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestWorkflowIntakeSQLiteRestartAndContinuationSmoke(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "WORKFLOW.md"), []byte(`---
tracker:
  kind: github
  provider:
    repo: acme/demo
    assignee: alice
  active_states: [open]
  terminal_states: [done, cancelled]
agent:
  max_concurrent_agents: 2
---
Implement {{ issue.identifier }}: {{ issue.title }}
Attempt {{ attempt }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	issue := domain.Issue{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#42"},
		Title:     "restart-safe intake",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}
	tracker := &integrationTracker{issue: issue}

	first, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.UpsertProject(ctx, domain.ProjectRecord{
		ID:            "demo",
		Path:          projectDir,
		RepoOriginURL: "https://github.com/acme/demo.git",
		RegisteredAt:  now,
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled:      true,
			WorkflowPath: "WORKFLOW.md",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	firstSpawner := &sqliteSpawner{store: first, now: func() time.Time { return now }}
	if err := trackerintake.New(
		trackerintake.SingleTrackerResolver{Provider: domain.TrackerProviderGitHub, Adapter: tracker},
		first,
		firstSpawner,
		trackerintake.Config{Clock: func() time.Time { return now }, Terminator: sqliteTerminator{store: first}},
	).Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if firstSpawner.calls != 1 {
		t.Fatalf("first daemon spawn calls = %d, want 1", firstSpawner.calls)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	secondSpawner := &sqliteSpawner{store: reopened, now: func() time.Time { return now }}
	observer := trackerintake.New(
		trackerintake.SingleTrackerResolver{Provider: domain.TrackerProviderGitHub, Adapter: tracker},
		reopened,
		secondSpawner,
		trackerintake.Config{Clock: func() time.Time { return now }, Terminator: sqliteTerminator{store: reopened}},
	)
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if secondSpawner.calls != 0 {
		t.Fatalf("reconstructed daemon spawned duplicate active attempt: %d", secondSpawner.calls)
	}

	sessions, err := reopened.ListAllSessions(ctx)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions after restart = %+v err=%v", sessions, err)
	}
	exited := sessions[0]
	exited.Activity.State = domain.ActivityExited
	exited.Activity.LastActivityAt = now
	exited.UpdatedAt = now
	if err := reopened.UpdateSession(ctx, exited); err != nil {
		t.Fatal(err)
	}
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	run, ok, err := reopened.GetWorkflowRun(ctx, "demo", trackerintake.CanonicalIssueID(issue.ID))
	if err != nil || !ok {
		t.Fatalf("retry run: ok=%v err=%v", ok, err)
	}
	if run.State != domain.WorkflowRunRetryQueued || run.Attempt != 1 ||
		!run.RetryDueAt.Equal(now.Add(time.Second)) {
		t.Fatalf("normal continuation retry = %+v", run)
	}

	now = now.Add(time.Second)
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if secondSpawner.calls != 1 {
		t.Fatalf("due continuation spawns = %d, want 1", secondSpawner.calls)
	}
	sessions, err = reopened.ListAllSessions(ctx)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions after continuation = %+v err=%v", sessions, err)
	}
	var active int
	for _, session := range sessions {
		if !session.IsTerminated {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active sessions after continuation = %d, want 1", active)
	}
	run, ok, err = reopened.GetWorkflowRun(ctx, "demo", trackerintake.CanonicalIssueID(issue.ID))
	if err != nil || !ok || run.State != domain.WorkflowRunRunning || run.Attempt != 1 {
		t.Fatalf("continuation run facts = %+v ok=%v err=%v", run, ok, err)
	}
	for _, session := range sessions {
		if !session.IsTerminated && session.Metadata.Prompt != "Implement acme/demo#42: restart-safe intake\nAttempt 1" {
			t.Fatalf("continuation prompt = %q", session.Metadata.Prompt)
		}
	}
}

type integrationTracker struct {
	issue domain.Issue
}

func (t *integrationTracker) Get(_ context.Context, id domain.TrackerID) (domain.Issue, error) {
	if id == t.issue.ID {
		return t.issue, nil
	}
	return domain.Issue{}, os.ErrNotExist
}

func (t *integrationTracker) List(context.Context, domain.TrackerRepo, domain.ListFilter) ([]domain.Issue, error) {
	return []domain.Issue{t.issue}, nil
}

func (t *integrationTracker) Preflight(context.Context) error { return nil }

type sqliteSpawner struct {
	store *sqlite.Store
	now   func() time.Time
	calls int
}

func (s *sqliteSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error) {
	s.calls++
	now := s.now()
	rec, err := s.store.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: cfg.ProjectID,
		IssueID:   cfg.IssueID,
		Kind:      cfg.Kind,
		Harness:   domain.HarnessFake,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
		Metadata:  domain.SessionMetadata{Prompt: cfg.Prompt},
		CreatedAt: now,
		UpdatedAt: now,
	})
	return domain.Session{SessionRecord: rec}, len(cfg.Prompt), 0, err
}

type sqliteTerminator struct {
	store *sqlite.Store
}

func (t sqliteTerminator) Kill(ctx context.Context, id domain.SessionID) (bool, error) {
	rec, ok, err := t.store.GetSession(ctx, id)
	if err != nil || !ok {
		return false, err
	}
	rec.IsTerminated = true
	rec.Activity.State = domain.ActivityExited
	rec.UpdatedAt = time.Now().UTC()
	if err := t.store.UpdateSession(ctx, rec); err != nil {
		return false, err
	}
	return true, nil
}
