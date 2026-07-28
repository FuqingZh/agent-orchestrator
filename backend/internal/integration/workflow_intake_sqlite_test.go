package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
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

func TestWorkflowIntakeContinuationTimingSmoke(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "WORKFLOW.md"), []byte(`---
tracker:
  kind: github
  provider:
    repo: acme/demo
    assignee: alice
  active_states: [open]
polling:
  interval_ms: 20
---
Continue {{ issue.identifier }} attempt {{ attempt }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	issue := domain.Issue{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#77"},
		Title:     "timed continuation",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}
	if err := store.UpsertProject(ctx, domain.ProjectRecord{
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
	issueID := trackerintake.CanonicalIssueID(issue.ID)
	session, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "demo",
		IssueID:   issueID,
		Kind:      domain.KindWorker,
		Harness:   domain.HarnessFake,
		Activity:  domain.Activity{State: domain.ActivityExited, LastActivityAt: now},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := domain.WorkflowRunRecord{
		ProjectID:        "demo",
		IssueID:          issueID,
		IssueIdentifier:  issue.ID.Native,
		WorkflowRevision: "initial",
		UpdatedAt:        now,
	}
	if ok, err := store.TryClaimWorkflowIssue(ctx, claim); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if ok, err := store.BindWorkflowIssueSession(ctx, "demo", issueID, session.ID, now); err != nil || !ok {
		t.Fatalf("bind: ok=%v err=%v", ok, err)
	}

	spawner := &timingSpawner{store: store, calls: make(chan time.Time, 1)}
	observer := trackerintake.New(
		trackerintake.SingleTrackerResolver{
			Provider: domain.TrackerProviderGitHub,
			Adapter:  &integrationTracker{issue: issue},
		},
		store,
		spawner,
		trackerintake.Config{Terminator: sqliteTerminator{store: store}},
	)
	started := time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	done := observer.Start(runCtx)
	t.Cleanup(func() {
		cancel()
		<-done
	})

	select {
	case spawnedAt := <-spawner.calls:
		elapsed := spawnedAt.Sub(started)
		if elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
			t.Fatalf("continuation elapsed = %s, want near one-second retry", elapsed)
		}
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("continuation was not dispatched near its one-second deadline")
	}
}

func TestWorkflowIntakeLinearCancellationIsTerminalAndSurvivesLatePRMerge(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "WORKFLOW.md"), []byte(`---
tracker:
  kind: linear
  provider:
    project: linear-project
  active_states: [open, in_progress, review]
  terminal_states: [done, cancelled]
---
Implement {{ issue.identifier }}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	if err := store.UpsertProject(ctx, domain.ProjectRecord{
		ID:           "demo",
		Path:         projectDir,
		RegisteredAt: now,
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled:      true,
			WorkflowPath: "WORKFLOW.md",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	issue := domain.Issue{
		ID:         domain.TrackerID{Provider: domain.TrackerProviderLinear, Native: "issue-uuid"},
		Identifier: "FUQ-17",
		State:      domain.IssueCancelled,
	}
	issueID := trackerintake.CanonicalIssueID(issue.ID)
	session, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "demo",
		IssueID:   issueID,
		Kind:      domain.KindWorker,
		Harness:   domain.HarnessFake,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := domain.WorkflowRunRecord{
		ProjectID:        "demo",
		IssueID:          issueID,
		IssueIdentifier:  issue.Identifier,
		WorkflowRevision: "rev-1",
		UpdatedAt:        now,
	}
	if ok, err := store.TryClaimWorkflowIssue(ctx, claim); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if ok, err := store.BindWorkflowIssueSession(ctx, "demo", issueID, session.ID, now); err != nil || !ok {
		t.Fatalf("bind: ok=%v err=%v", ok, err)
	}
	observer := trackerintake.New(
		trackerintake.SingleTrackerResolver{
			Provider: domain.TrackerProviderLinear,
			Adapter:  &integrationTracker{issue: issue},
		},
		store,
		&sqliteSpawner{store: store, now: func() time.Time { return now }},
		trackerintake.Config{
			Clock:      func() time.Time { return now },
			Terminator: sqliteTerminator{store: store},
		},
	)
	if err := observer.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	run, ok, err := store.GetWorkflowRun(ctx, "demo", issueID)
	if err != nil || !ok {
		t.Fatalf("released run: ok=%v err=%v", ok, err)
	}
	if run.State != domain.WorkflowRunReleased || run.TerminalReason != "terminal:cancelled" {
		t.Fatalf("released run = %+v", run)
	}
	rec, ok, err := store.GetSession(ctx, session.ID)
	if err != nil || !ok || !rec.IsTerminated {
		t.Fatalf("terminated session = %+v ok=%v err=%v", rec, ok, err)
	}

	// A merge observation is owned by the SCM/session lifecycle. It must not
	// reopen or rewrite the already-released issue-intake record.
	if err := lifecycle.New(store, nil).ApplyPRObservation(ctx, session.ID, ports.PRObservation{
		Fetched: true,
		URL:     "https://github.com/acme/demo/pull/17",
		Merged:  true,
	}); err != nil {
		t.Fatal(err)
	}
	afterMerge, ok, err := store.GetWorkflowRun(ctx, "demo", issueID)
	if err != nil || !ok {
		t.Fatalf("run after merge: ok=%v err=%v", ok, err)
	}
	if afterMerge.State != domain.WorkflowRunReleased ||
		afterMerge.TerminalReason != "terminal:cancelled" {
		t.Fatalf("late merge rewrote released run: before=%+v after=%+v", run, afterMerge)
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

type timingSpawner struct {
	store *sqlite.Store
	calls chan time.Time
}

func (s *timingSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error) {
	now := time.Now().UTC()
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
	if err == nil {
		s.calls <- now
	}
	return domain.Session{SessionRecord: rec}, len(cfg.Prompt), 0, err
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
