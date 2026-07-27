// Package trackerintake implements the opt-in issue-intake observer. It polls a
// project's configured tracker for eligible issues and starts one worker session
// per issue, leaving PR/lifecycle handling to the existing observers.
package trackerintake

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/workflow"
)

const (
	// DefaultTickInterval is intentionally slower than runtime liveness checks:
	// intake is a backlog sweep, not an interactive status surface.
	DefaultTickInterval = time.Minute
	// DefaultFailureBackoff suppresses repeated polls for a project after an
	// intake failure. The observer retries automatically after this window.
	DefaultFailureBackoff = 5 * time.Minute
	// DefaultMaxWorkflowSessions bounds sessions spawned by the Symphony lane
	// across all projects. Legacy tracker intake remains unchanged.
	DefaultMaxWorkflowSessions = 10
	// maxIntakePromptLen mirrors the session HTTP prompt limit. Intake uses the
	// session service directly, so it must enforce the same boundary itself.
	maxIntakePromptLen = 4096

	intakePromptTruncationNotice = "\n\n[Issue content truncated to fit the session prompt limit. Open the linked issue for the full details.]\n"
	intakePromptFooter           = "\nImplement the requested change in this repository, run the relevant checks, and open or update a pull request when ready."
)

// Store is the durable read surface the observer needs.
type Store interface {
	ListProjects(ctx context.Context) ([]domain.ProjectRecord, error)
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
}

// Spawner is the session creation surface used by intake.
type Spawner interface {
	Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error)
}

// TrackerResolver picks the tracker adapter for a project's configured
// provider.
type TrackerResolver interface {
	Resolve(provider domain.TrackerProvider) (ports.Tracker, error)
}

// SingleTrackerResolver returns the same tracker for one specific provider and
// refuses every other provider. It exists so single-provider deployments don't
// need to construct a map.
type SingleTrackerResolver struct {
	Provider domain.TrackerProvider
	Adapter  ports.Tracker
}

// Resolve returns the wrapped adapter when the requested provider matches, or
// when the resolver was constructed without a provider pin.
func (s SingleTrackerResolver) Resolve(provider domain.TrackerProvider) (ports.Tracker, error) {
	if s.Adapter == nil {
		return nil, fmt.Errorf("tracker intake: no adapter for provider %q", provider)
	}
	if s.Provider == "" || provider == "" || provider == s.Provider {
		return s.Adapter, nil
	}
	return nil, fmt.Errorf("tracker intake: no adapter for provider %q", provider)
}

// Config holds optional observer knobs. Zero values use production defaults.
type Config struct {
	Tick                time.Duration
	FailureBackoff      time.Duration
	MaxWorkflowSessions int
	WorkflowOptions     workflow.Options
	Clock               func() time.Time
	Logger              *slog.Logger
}

// Observer polls configured projects and starts sessions for eligible issues.
type Observer struct {
	resolver       TrackerResolver
	store          Store
	spawner        Spawner
	tick           time.Duration
	failureBackoff time.Duration
	clock          func() time.Time
	logger         *slog.Logger
	backoffUntil   map[string]time.Time
	maxWorkflow    int
	workflowOpts   workflow.Options
	reloaders      map[string]*workflow.Reloader
	nextInterval   time.Duration
}

// New constructs an Observer with safe defaults.
func New(resolver TrackerResolver, store Store, spawner Spawner, cfg Config) *Observer {
	o := &Observer{
		resolver:       resolver,
		store:          store,
		spawner:        spawner,
		tick:           cfg.Tick,
		failureBackoff: cfg.FailureBackoff,
		maxWorkflow:    cfg.MaxWorkflowSessions,
		workflowOpts:   cfg.WorkflowOptions,
		clock:          cfg.Clock,
		logger:         cfg.Logger,
		backoffUntil:   map[string]time.Time{},
		reloaders:      map[string]*workflow.Reloader{},
	}
	if o.tick <= 0 {
		o.tick = DefaultTickInterval
	}
	if o.failureBackoff <= 0 {
		o.failureBackoff = DefaultFailureBackoff
	}
	if o.maxWorkflow <= 0 {
		o.maxWorkflow = DefaultMaxWorkflowSessions
	}
	if o.clock == nil {
		o.clock = time.Now
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	return o
}

// Start launches the observer loop. The first poll runs immediately inside the
// goroutine, keeping daemon startup non-blocking. Later polls use the fastest
// active workflow interval, while legacy intake keeps the daemon fallback.
func (o *Observer) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if err := o.Poll(ctx); err != nil && ctx.Err() == nil {
				o.logger.Error("tracker intake: poll failed", "err", err)
			}
			timer := time.NewTimer(o.effectiveTick())
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return done
}

// Poll runs one synchronous intake pass. Store discovery failures are returned
// because they prevent the pass from knowing the current world; provider and
// spawn failures are logged and skipped so one bad issue/project does not block
// the rest of the daemon.
func (o *Observer) Poll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.resolver == nil || o.store == nil || o.spawner == nil {
		return nil
	}
	now := o.clock().UTC()
	projects, err := o.store.ListProjects(ctx)
	if err != nil {
		return err
	}
	enabledProjects := make([]domain.ProjectRecord, 0, len(projects))
	for _, project := range projects {
		if project.Config.TrackerIntake.Enabled {
			enabledProjects = append(enabledProjects, project)
		}
	}
	o.nextInterval = 0
	if len(enabledProjects) == 0 {
		return nil
	}
	sessions, err := o.store.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	seen := seenIssueIDs(sessions)
	budget := newDispatchBudget(enabledProjects, sessions, o.maxWorkflow)
	for _, project := range enabledProjects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if until, ok := o.backoffUntil[project.ID]; ok && now.Before(until) {
			o.logger.Debug("tracker intake: project in failure backoff", "project", project.ID, "until", until)
			continue
		}
		if failed := o.pollProject(ctx, project, seen, budget); failed {
			o.backoffUntil[project.ID] = now.Add(o.failureBackoff)
		} else {
			delete(o.backoffUntil, project.ID)
		}
	}
	return nil
}

func (o *Observer) includeInterval(interval time.Duration) {
	if interval > 0 && (o.nextInterval == 0 || interval < o.nextInterval) {
		o.nextInterval = interval
	}
}

func (o *Observer) effectiveTick() time.Duration {
	if o.nextInterval > 0 {
		return o.nextInterval
	}
	return o.tick
}

// pollProject returns failed=true for conditions that should be retried after a
// backoff window rather than logged on every poll.
func (o *Observer) pollProject(ctx context.Context, project domain.ProjectRecord, seen map[domain.IssueID]bool, budget *dispatchBudget) (failed bool) {
	cfg := project.Config.TrackerIntake.WithDefaults()
	if !cfg.Enabled {
		return false
	}
	if err := cfg.Validate(); err != nil {
		o.logger.Warn("tracker intake: skipping project with invalid config", "project", project.ID, "err", err)
		return true
	}
	workflowConfig, workflowEnabled, workflowErr := o.workflowForProject(project, cfg)
	if workflowErr != nil {
		o.logger.Warn("tracker intake: workflow reload failed", "project", project.ID, "err", workflowErr)
		if !workflowEnabled {
			o.includeInterval(o.tick)
			return true
		}
	}
	if workflowEnabled {
		o.includeInterval(workflowConfig.Config.Polling.Interval)
		var err error
		cfg, err = intakeConfigFromWorkflow(cfg, workflowConfig)
		if err != nil {
			o.logger.Warn("tracker intake: invalid GitHub workflow profile", "project", project.ID, "err", err)
			return true
		}
		if !budget.projectSlotAvailable(project.ID, workflowConfig.Config.Agent.MaxConcurrentAgents) {
			return false
		}
	} else {
		o.includeInterval(o.tick)
	}
	repo, ok := trackerRepo(project, cfg)
	if !ok {
		o.logger.Warn("tracker intake: skipping project without tracker scope", "project", project.ID, "provider", cfg.Provider, "origin", project.RepoOriginURL)
		return true
	}
	tracker, err := o.resolver.Resolve(cfg.Provider)
	if err != nil {
		o.logger.Warn("tracker intake: no adapter for provider", "project", project.ID, "provider", cfg.Provider, "err", err)
		return true
	}
	issues, err := tracker.List(ctx, repo, domain.ListFilter{
		State:    domain.ListOpen,
		Labels:   workflowLabels(workflowConfig, workflowEnabled),
		Assignee: cfg.Assignee,
	})
	if err != nil {
		o.logger.Error("tracker intake: list issues failed", "project", project.ID, "repo", repo.Native, "err", err)
		return true
	}
	var spawnFailed bool
	for _, issue := range issues {
		if ctx.Err() != nil {
			return true
		}
		if !issueMatchesConfig(issue, cfg) {
			continue
		}
		if workflowEnabled {
			state := normalizedState(issue.State)
			if !workflowIssueEligible(issue, workflowConfig) {
				continue
			}
			if !budget.slotAvailable(project.ID, state, workflowConfig.Config.Agent) {
				continue
			}
		} else if issue.State != domain.IssueOpen {
			continue
		}
		issueID := CanonicalIssueID(issue.ID)
		if issueID == "" || seen[issueID] {
			continue
		}
		prompt := BuildIssuePrompt(issue)
		if workflowEnabled {
			prompt, err = renderWorkflowPrompt(workflowConfig, issue)
			if err != nil {
				o.logger.Error("tracker intake: render workflow prompt failed", "project", project.ID, "issue", issueID, "err", err)
				spawnFailed = true
				continue
			}
		}
		if _, _, _, err := o.spawner.Spawn(ctx, ports.SpawnConfig{
			ProjectID: domain.ProjectID(project.ID),
			IssueID:   issueID,
			Kind:      domain.KindWorker,
			Prompt:    prompt,
		}); err != nil {
			o.logger.Error("tracker intake: spawn issue session failed", "project", project.ID, "issue", issueID, "err", err)
			spawnFailed = true
			continue
		}
		seen[issueID] = true
		if workflowEnabled {
			budget.recordSpawn(project.ID, normalizedState(issue.State))
		}
	}
	return spawnFailed
}

type dispatchBudget struct {
	globalLimit   int
	globalActive  int
	projectActive map[string]int
	stateSpawned  map[string]map[string]int
}

func newDispatchBudget(projects []domain.ProjectRecord, sessions []domain.SessionRecord, globalLimit int) *dispatchBudget {
	workflowProjects := make(map[string]bool)
	for _, project := range projects {
		cfg := project.Config.TrackerIntake
		if cfg.Enabled && cfg.WorkflowPath != "" {
			workflowProjects[project.ID] = true
		}
	}
	budget := &dispatchBudget{
		globalLimit:   globalLimit,
		projectActive: make(map[string]int),
		stateSpawned:  make(map[string]map[string]int),
	}
	for _, session := range sessions {
		projectID := string(session.ProjectID)
		if session.IsTerminated || session.IssueID == "" || !workflowProjects[projectID] {
			continue
		}
		budget.globalActive++
		budget.projectActive[projectID]++
	}
	return budget
}

func (b *dispatchBudget) projectSlotAvailable(projectID string, projectLimit int) bool {
	return b.globalActive < b.globalLimit && b.projectActive[projectID] < projectLimit
}

func (b *dispatchBudget) slotAvailable(projectID, state string, cfg workflow.AgentConfig) bool {
	if !b.projectSlotAvailable(projectID, cfg.MaxConcurrentAgents) {
		return false
	}
	limit, limited := cfg.MaxConcurrentAgentsByState[state]
	if !limited {
		return true
	}
	return b.stateSpawned[projectID][state] < limit
}

func (b *dispatchBudget) recordSpawn(projectID, state string) {
	b.globalActive++
	b.projectActive[projectID]++
	if b.stateSpawned[projectID] == nil {
		b.stateSpawned[projectID] = make(map[string]int)
	}
	b.stateSpawned[projectID][state]++
}

func (o *Observer) workflowForProject(project domain.ProjectRecord, cfg domain.TrackerIntakeConfig) (workflow.Workflow, bool, error) {
	if cfg.WorkflowPath == "" {
		return workflow.Workflow{}, false, nil
	}
	path := filepath.Join(project.Path, cfg.WorkflowPath)
	relative, err := filepath.Rel(project.Path, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return workflow.Workflow{}, false, fmt.Errorf("tracker intake: workflow path escapes project root")
	}
	reloaderKey := project.ID + "\x00" + path
	reloader := o.reloaders[reloaderKey]
	if reloader == nil {
		validate := func(candidate workflow.Workflow) error {
			return workflow.ValidateForDispatch(candidate, map[string]bool{"github": true})
		}
		reloader = workflow.NewReloader(path, o.workflowOpts, validate)
		o.reloaders[reloaderKey] = reloader
	}
	candidate, _, err := reloader.Reload()
	return candidate, candidate.Revision != "", err
}

func intakeConfigFromWorkflow(base domain.TrackerIntakeConfig, candidate workflow.Workflow) (domain.TrackerIntakeConfig, error) {
	assignee, err := providerString(candidate.Config.Tracker.Provider, "assignee")
	if err != nil {
		return base, err
	}
	if assignee == "" {
		return base, fmt.Errorf("tracker.provider.assignee is required for GitHub dispatch")
	}
	repo, err := providerString(candidate.Config.Tracker.Provider, "repo")
	if err != nil {
		return base, err
	}
	base.Assignee = assignee
	if repo != "" {
		base.Repo = repo
	}
	return base, nil
}

func providerString(provider map[string]any, key string) (string, error) {
	value, ok := provider[key]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("tracker.provider.%s must be a string", key)
	}
	return strings.TrimSpace(text), nil
}

func workflowLabels(candidate workflow.Workflow, enabled bool) []string {
	if !enabled {
		return nil
	}
	return candidate.Config.Tracker.RequiredLabels
}

func workflowIssueEligible(issue domain.Issue, candidate workflow.Workflow) bool {
	active := candidate.Config.Tracker.ActiveStates
	if len(active) == 0 {
		active = []string{string(domain.IssueOpen)}
	}
	if !containsFold(active, normalizedState(issue.State)) {
		return false
	}
	for _, required := range candidate.Config.Tracker.RequiredLabels {
		if strings.TrimSpace(required) == "" || !containsFold(issue.Labels, required) {
			return false
		}
	}
	return true
}

func normalizedState(state domain.NormalizedIssueState) string {
	return strings.ToLower(strings.TrimSpace(string(state)))
}

func renderWorkflowPrompt(candidate workflow.Workflow, issue domain.Issue) (string, error) {
	description := issue.Body
	url := issue.URL
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		labels = append(labels, strings.ToLower(strings.TrimSpace(label)))
	}
	rendered, err := workflow.RenderPrompt(candidate.Definition.PromptTemplate, workflow.PromptData{
		Issue: workflow.Issue{
			ID:           issue.ID.Native,
			Identifier:   issue.ID.Native,
			Title:        issue.Title,
			Description:  &description,
			State:        normalizedState(issue.State),
			URL:          &url,
			Labels:       labels,
			Dispatchable: true,
		},
	})
	if err != nil {
		return "", err
	}
	return capIntakePrompt(rendered), nil
}

func issueMatchesConfig(issue domain.Issue, cfg domain.TrackerIntakeConfig) bool {
	assignee := strings.TrimSpace(cfg.Assignee)
	switch {
	case assignee == "":
		return true
	case assignee == "*":
		return len(issue.Assignees) > 0
	case strings.EqualFold(assignee, "none"):
		return len(issue.Assignees) == 0
	default:
		return containsFold(issue.Assignees, assignee)
	}
}

func containsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

func seenIssueIDs(sessions []domain.SessionRecord) map[domain.IssueID]bool {
	seen := make(map[domain.IssueID]bool, len(sessions))
	for _, sess := range sessions {
		if sess.IssueID != "" && !sess.IsTerminated {
			seen[sess.IssueID] = true
		}
	}
	return seen
}

// CanonicalIssueID stores tracker issue ids in sessions.issue_id with the
// provider included, so future providers cannot collide on native ids.
func CanonicalIssueID(id domain.TrackerID) domain.IssueID {
	provider := id.Provider
	if provider == "" {
		provider = domain.TrackerProviderGitHub
	}
	native := strings.TrimSpace(id.Native)
	if native == "" {
		return ""
	}
	return domain.IssueID(string(provider) + ":" + native)
}

// BuildIssuePrompt turns normalized issue facts into the worker's initial task.
func BuildIssuePrompt(issue domain.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Work on tracker issue %s.\n\n", CanonicalIssueID(issue.ID))
	if issue.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", issue.Title)
	}
	if issue.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", issue.URL)
	}
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	if len(issue.Assignees) > 0 {
		fmt.Fprintf(&b, "Assignees: %s\n", strings.Join(issue.Assignees, ", "))
	}
	body := strings.TrimSpace(issue.Body)
	if body != "" {
		fmt.Fprintf(&b, "\nBody:\n%s\n", body)
	}
	b.WriteString(intakePromptFooter)
	return capIntakePrompt(b.String())
}

func capIntakePrompt(prompt string) string {
	if len(prompt) <= maxIntakePromptLen {
		return prompt
	}
	prefix := strings.TrimSuffix(prompt, intakePromptFooter)
	prefixBudget := maxIntakePromptLen - len(intakePromptTruncationNotice) - len(intakePromptFooter)
	if prefixBudget <= 0 {
		return truncateUTF8(prompt, maxIntakePromptLen)
	}
	return truncateUTF8(prefix, prefixBudget) + intakePromptTruncationNotice + intakePromptFooter
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		cut = i
	}
	return s[:cut]
}

func trackerRepo(project domain.ProjectRecord, cfg domain.TrackerIntakeConfig) (domain.TrackerRepo, bool) {
	provider := cfg.Provider
	if provider == "" {
		provider = domain.TrackerProviderGitHub
	}
	if provider != domain.TrackerProviderGitHub {
		return domain.TrackerRepo{}, false
	}
	native := strings.TrimSpace(cfg.Repo)
	if native == "" {
		native = parseGitHubRepoNative(project.RepoOriginURL)
	}
	if native == "" {
		return domain.TrackerRepo{}, false
	}
	return domain.TrackerRepo{Provider: provider, Native: native}, true
}

func parseGitHubRepoNative(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "git@") {
		if _, rest, ok := strings.Cut(remote, ":"); ok {
			return cleanRepoPath(rest)
		}
	}
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		if host == "github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".ghe.io") {
			return cleanRepoPath(u.Path)
		}
		return ""
	}
	return cleanRepoPath(remote)
}

func cleanRepoPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	owner := strings.TrimSpace(parts[len(parts)-2])
	repo := strings.TrimSpace(parts[len(parts)-1])
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}
