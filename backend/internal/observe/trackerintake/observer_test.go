package trackerintake

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestPollSpawnsWorkerForEligibleIssue(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled: true, Assignee: "alice",
		}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Fix login",
		Body:      "The login form submits twice.",
		State:     domain.IssueOpen,
		URL:       "https://github.com/acme/demo/issues/12",
		Labels:    []string{"agent-ready"},
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn calls = %d, want 1", len(spawner.calls))
	}
	call := spawner.calls[0]
	if call.ProjectID != "demo" || call.Kind != domain.KindWorker || call.IssueID != "github:acme/demo#12" {
		t.Fatalf("spawn config = %+v", call)
	}
	if !strings.Contains(call.Prompt, "Fix login") || !strings.Contains(call.Prompt, "The login form submits twice.") {
		t.Fatalf("prompt missing issue context:\n%s", call.Prompt)
	}
	if got := tracker.filters[0]; got.State != domain.ListOpen || got.Assignee != "alice" || len(got.Labels) != 0 {
		t.Fatalf("tracker filter = %+v", got)
	}
}

func TestPollSkipsExistingIssueSessionsAfterRestart(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{ID: "demo-1", ProjectID: "demo", IssueID: "github:acme/demo#12"}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Already running",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0", len(spawner.calls))
	}
}

func TestPollRespawnsIssueAfterTerminatedSession(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{
			ID: "demo-1", ProjectID: "demo", IssueID: "github:acme/demo#12", IsTerminated: true,
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Killed session should respawn",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#12" {
		t.Fatalf("spawn calls = %+v", spawner.calls)
	}
}

func TestSeenIssueIDsExcludesTerminatedSessions(t *testing.T) {
	sessions := []domain.SessionRecord{
		{ID: "demo-1", IssueID: "github:acme/demo#12", IsTerminated: true},
		{ID: "demo-2", IssueID: "github:acme/demo#12"},
	}
	seen := seenIssueIDs(sessions)
	if !seen["github:acme/demo#12"] || len(seen) != 1 {
		t.Fatalf("seen = %+v, want one live issue", seen)
	}
	if seenIssueIDs(sessions[:1])["github:acme/demo#12"] {
		t.Fatal("terminated-only issue should not be seen")
	}
}

func TestPollSkipsSessionScanWhenIntakeDisabled(t *testing.T) {
	store := &fakeStore{
		projects:    []domain.ProjectRecord{{ID: "demo"}},
		sessionsErr: errors.New("session scan should not run"),
	}
	if err := New(singleResolver(&fakeTracker{}), store, &fakeSpawner{}, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v, want nil", err)
	}
}

func TestPollSkipsIneligibleAndInvalidProjects(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{
		{ID: "off", RepoOriginURL: "https://github.com/acme/off.git"},
		{ID: "broad", RepoOriginURL: "https://github.com/acme/broad.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true}}},
		{ID: "missing-origin", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
		{ID: "linear-missing-project", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled: true, Provider: domain.TrackerProviderLinear, Assignee: "alice",
		}}},
	}}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/off#1"}, State: domain.IssueOpen,
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(tracker.repos) != 0 || len(spawner.calls) != 0 {
		t.Fatalf("unexpected tracker calls=%+v spawn calls=%+v", tracker.repos, spawner.calls)
	}
}

func TestPollContinuesAfterTrackerAndSpawnFailures(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{
		{ID: "bad", RepoOriginURL: "https://github.com/acme/bad.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
		{ID: "good", RepoOriginURL: "https://github.com/acme/good.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
	}}
	tracker := &fakeTracker{
		failRepos: map[string]error{"acme/bad": errors.New("rate limited")},
		issuesByRepo: map[string][]domain.Issue{"acme/good": {
			{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/good#1"}, State: domain.IssueOpen, Assignees: []string{"alice"}},
			{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/good#2"}, State: domain.IssueOpen, Assignees: []string{"alice"}},
		}},
	}
	spawner := &fakeSpawner{failIssue: "github:acme/good#1"}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 2 || spawner.calls[1].IssueID != "github:acme/good#2" {
		t.Fatalf("spawn calls = %+v", spawner.calls)
	}
}

func TestPollBacksOffProjectAfterFailure(t *testing.T) {
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{failRepos: map[string]error{"acme/demo": errors.New("rate limited")}}
	observer := New(singleResolver(tracker), store, &fakeSpawner{}, Config{
		Clock: func() time.Time { return now }, FailureBackoff: time.Minute, Logger: discardLogger(),
	})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tracker.repos) != 1 {
		t.Fatalf("tracker calls during backoff = %d, want 1", len(tracker.repos))
	}
	now = now.Add(time.Minute + time.Nanosecond)
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tracker.repos) != 2 {
		t.Fatalf("tracker calls after backoff = %d, want 2", len(tracker.repos))
	}
}

func TestPollSkipsNonOpenIssueStates(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"}, State: domain.IssueInProgress, Assignees: []string{"alice"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#2"}, State: domain.IssueOpen, Assignees: []string{"alice"}},
	}}
	spawner := &fakeSpawner{}
	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#2" {
		t.Fatalf("spawn calls = %+v", spawner.calls)
	}
}

func TestPollAppliesLocalEligibilityFilter(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"}, State: domain.IssueOpen},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#2"}, State: domain.IssueOpen, Assignees: []string{"bob"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#3"}, State: domain.IssueOpen, Assignees: []string{"Alice"}},
	}}
	spawner := &fakeSpawner{}
	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#3" {
		t.Fatalf("spawn calls = %+v", spawner.calls)
	}
}

func TestIssueMatchesConfigAssigneeSpecialValues(t *testing.T) {
	assigned := domain.Issue{Assignees: []string{"alice"}}
	unassigned := domain.Issue{}
	if !issueMatchesConfig(assigned, domain.TrackerIntakeConfig{Assignee: "*"}) ||
		issueMatchesConfig(unassigned, domain.TrackerIntakeConfig{Assignee: "*"}) ||
		!issueMatchesConfig(unassigned, domain.TrackerIntakeConfig{Assignee: "none"}) ||
		issueMatchesConfig(assigned, domain.TrackerIntakeConfig{Assignee: "none"}) {
		t.Fatal("assignee special-value matching failed")
	}
}

func TestBuildIssuePromptCapsLargeIssueBody(t *testing.T) {
	prompt := BuildIssuePrompt(domain.Issue{
		ID:         domain.TrackerID{Provider: domain.TrackerProviderLinear, Native: "issue-uuid"},
		Identifier: "FUQ-12",
		Title:      "Large issue",
		URL:        "https://linear.app/example/issue/FUQ-12",
		Body:       strings.Repeat("body ", 2000),
	})
	if len(prompt) > maxIntakePromptLen || !strings.Contains(prompt, "Issue content truncated") ||
		!strings.Contains(prompt, "Identifier: FUQ-12") || !strings.HasSuffix(prompt, intakePromptFooter) {
		t.Fatalf("unexpected prompt:\n%s", prompt)
	}
}

func TestTrackerRepoUsesConfiguredScopes(t *testing.T) {
	github, ok := trackerRepo(domain.ProjectRecord{
		RepoOriginURL: "https://github.com/wrong/repo.git",
	}, domain.TrackerIntakeConfig{Enabled: true, Repo: "acme/demo", Assignee: "alice"}.WithDefaults())
	if !ok || github != (domain.TrackerRepo{Provider: domain.TrackerProviderGitHub, Native: "acme/demo"}) {
		t.Fatalf("github scope = %+v, ok=%v", github, ok)
	}
	linear, ok := trackerRepo(domain.ProjectRecord{}, domain.TrackerIntakeConfig{
		Enabled: true, Provider: domain.TrackerProviderLinear, Repo: "project-uuid", Assignee: "alice",
	}.WithDefaults())
	if !ok || linear != (domain.TrackerRepo{Provider: domain.TrackerProviderLinear, Native: "project-uuid"}) {
		t.Fatalf("linear scope = %+v, ok=%v", linear, ok)
	}
}

func TestPollRoutesLinearProjectWithoutWorkflowState(t *testing.T) {
	project := domain.ProjectRecord{
		ID: "linear-demo",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled: true, Provider: domain.TrackerProviderLinear, Repo: "project-uuid", Assignee: "alice@example.com",
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:         domain.TrackerID{Provider: domain.TrackerProviderLinear, Native: "issue-uuid"},
		Identifier: "FUQ-12",
		Title:      "Ship the service",
		State:      domain.IssueOpen,
		Assignees:  []string{"alice@example.com"},
	}}}
	store := &fakeStore{projects: []domain.ProjectRecord{project}}
	spawner := &fakeSpawner{}
	observer := New(MultiTrackerResolver{domain.TrackerProviderLinear: tracker}, store, spawner, Config{Logger: discardLogger()})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tracker.repos) != 1 || tracker.repos[0] != (domain.TrackerRepo{
		Provider: domain.TrackerProviderLinear, Native: "project-uuid",
	}) {
		t.Fatalf("repos = %#v", tracker.repos)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "linear:issue-uuid" ||
		!strings.Contains(spawner.calls[0].Prompt, "Identifier: FUQ-12") {
		t.Fatalf("spawn calls = %#v", spawner.calls)
	}
}

func singleResolver(tracker ports.Tracker) TrackerResolver {
	return SingleTrackerResolver{Provider: domain.TrackerProviderGitHub, Adapter: tracker}
}

type fakeStore struct {
	projects    []domain.ProjectRecord
	sessions    []domain.SessionRecord
	sessionsErr error
}

func (f *fakeStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return append([]domain.ProjectRecord(nil), f.projects...), nil
}

func (f *fakeStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return append([]domain.SessionRecord(nil), f.sessions...), f.sessionsErr
}

type fakeTracker struct {
	issues       []domain.Issue
	issuesByRepo map[string][]domain.Issue
	failRepos    map[string]error
	repos        []domain.TrackerRepo
	filters      []domain.ListFilter
}

func (f *fakeTracker) Get(_ context.Context, id domain.TrackerID) (domain.Issue, error) {
	for _, issue := range f.issues {
		if issue.ID == id {
			return issue, nil
		}
	}
	for _, issues := range f.issuesByRepo {
		for _, issue := range issues {
			if issue.ID == id {
				return issue, nil
			}
		}
	}
	return domain.Issue{}, errors.New("issue not found")
}

func (f *fakeTracker) List(_ context.Context, repo domain.TrackerRepo, filter domain.ListFilter) ([]domain.Issue, error) {
	f.repos = append(f.repos, repo)
	f.filters = append(f.filters, filter)
	if err := f.failRepos[repo.Native]; err != nil {
		return nil, err
	}
	if f.issuesByRepo != nil {
		return append([]domain.Issue(nil), f.issuesByRepo[repo.Native]...), nil
	}
	return append([]domain.Issue(nil), f.issues...), nil
}

func (f *fakeTracker) Preflight(context.Context) error { return nil }

type fakeSpawner struct {
	calls     []ports.SpawnConfig
	failIssue domain.IssueID
}

func (f *fakeSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error) {
	f.calls = append(f.calls, cfg)
	if cfg.IssueID == f.failIssue {
		return domain.Session{}, 0, 0, errors.New("spawn failed")
	}
	return domain.Session{
		SessionRecord: domain.SessionRecord{
			ID: domain.SessionID(string(cfg.ProjectID) + "-1"), ProjectID: cfg.ProjectID, IssueID: cfg.IssueID, Kind: cfg.Kind,
		},
	}, len(cfg.Prompt), 0, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
