package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	trackergithub "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/github"
	trackerlinear "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/linear"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	trackerintake "github.com/aoagents/agent-orchestrator/backend/internal/observe/trackerintake"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// startTrackerIntake wires the opt-in issue-intake loop. The observer
// always runs — Poll re-reads each project's config on every tick and skips
// projects with intake disabled, so a project enabling intake after daemon
// boot is picked up on the next tick without a restart. Provider adapters stay
// lazy so daemon readiness is not blocked by credential probing.
func startTrackerIntake(ctx context.Context, store *sqlite.Store, sessions *sessionsvc.Service, logger *slog.Logger) <-chan struct{} {
	resolver := trackerintake.MultiTrackerResolver{
		domain.TrackerProviderGitHub: newLazyGitHubTracker(logger),
		domain.TrackerProviderLinear: newLazyLinearTracker(logger),
	}
	return startTrackerIntakeObserver(ctx, resolver, store, sessions, trackerintake.Config{Logger: logger})
}

func startTrackerIntakeObserver(
	ctx context.Context,
	resolver trackerintake.TrackerResolver,
	store trackerintake.Store,
	spawner trackerintake.Spawner,
	cfg trackerintake.Config,
) <-chan struct{} {
	if cfg.Terminator == nil {
		if terminator, ok := spawner.(trackerintake.Terminator); ok {
			cfg.Terminator = terminator
		}
	}
	observer := trackerintake.New(resolver, store, spawner, cfg)
	return observer.Start(ctx)
}

// ---------------------------------------------------------------------------
// Linear lazy adapter (explicit AO-scoped API key or OAuth token)
// ---------------------------------------------------------------------------

type lazyLinearTracker struct {
	logger  *slog.Logger
	mu      sync.Mutex
	tracker ports.Tracker
}

func newLazyLinearTracker(logger *slog.Logger) *lazyLinearTracker {
	return &lazyLinearTracker{logger: logger}
}

func (t *lazyLinearTracker) Get(ctx context.Context, id domain.TrackerID) (domain.Issue, error) {
	tracker, err := t.resolve()
	if err != nil {
		return domain.Issue{}, err
	}
	return tracker.Get(ctx, id)
}

func (t *lazyLinearTracker) List(ctx context.Context, repo domain.TrackerRepo, filter domain.ListFilter) ([]domain.Issue, error) {
	tracker, err := t.resolve()
	if err != nil {
		return nil, err
	}
	return tracker.List(ctx, repo, filter)
}

func (t *lazyLinearTracker) Preflight(ctx context.Context) error {
	tracker, err := t.resolve()
	if err != nil {
		return err
	}
	return tracker.Preflight(ctx)
}

func (t *lazyLinearTracker) resolve() (ports.Tracker, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tracker != nil {
		return t.tracker, nil
	}
	tracker, err := trackerlinear.New(trackerlinear.Options{
		Authorization: trackerlinear.EnvironmentAuthorizationSource{Getenv: os.Getenv},
	})
	if err != nil {
		if errors.Is(err, trackerlinear.ErrNoCredential) && t.logger != nil {
			t.logger.Warn("tracker intake disabled: no usable Linear credential", "err", err)
		}
		return nil, err
	}
	t.tracker = tracker
	return tracker, nil
}

// ---------------------------------------------------------------------------
// GitHub lazy adapter (token sourced from env or gh CLI fallback)
// ---------------------------------------------------------------------------

type lazyGitHubTracker struct {
	logger  *slog.Logger
	tokens  *trackerTokenSource
	mu      sync.Mutex
	tracker ports.Tracker
}

func newLazyGitHubTracker(logger *slog.Logger) *lazyGitHubTracker {
	app, _, err := configuredGitHubAppTokenSource()
	return &lazyGitHubTracker{
		logger: logger,
		tokens: &trackerTokenSource{
			app:    app,
			appErr: err,
		},
	}
}

func (t *lazyGitHubTracker) Get(ctx context.Context, id domain.TrackerID) (domain.Issue, error) {
	tracker, err := t.resolve()
	if err != nil {
		return domain.Issue{}, err
	}
	return tracker.Get(ctx, id)
}

func (t *lazyGitHubTracker) List(ctx context.Context, repo domain.TrackerRepo, filter domain.ListFilter) ([]domain.Issue, error) {
	tracker, err := t.resolve()
	if err != nil {
		return nil, err
	}
	return tracker.List(ctx, repo, filter)
}

func (t *lazyGitHubTracker) Preflight(ctx context.Context) error {
	tracker, err := t.resolve()
	if err != nil {
		return err
	}
	return tracker.Preflight(ctx)
}

func (t *lazyGitHubTracker) resolve() (ports.Tracker, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tracker != nil {
		return t.tracker, nil
	}
	tracker, err := trackergithub.New(trackergithub.Options{Token: t.tokens})
	if err != nil {
		if errors.Is(err, trackergithub.ErrNoToken) && t.logger != nil {
			t.logger.Warn("tracker intake disabled: no usable GitHub token", "err", err)
		}
		return nil, err
	}
	t.tracker = tracker
	return tracker, nil
}

const (
	trackerTokenCacheTTL       = 5 * time.Minute
	trackerTokenCommandTimeout = 5 * time.Second
)

// trackerTokenSource mirrors the SCM credential precedence while returning the
// tracker adapter's own ErrNoToken sentinel.
type trackerTokenSource struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
	app       githubTokenSource
	appErr    error
}

func (s *trackerTokenSource) Token(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("AO_GITHUB_TOKEN")); token != "" {
		return token, nil
	}
	if s.appErr != nil {
		return "", s.appErr
	}
	if s.app != nil {
		return s.app.Token(ctx)
	}
	env := trackergithub.EnvTokenSource{EnvVars: []string{"AO_GITHUB_TOKEN"}}
	if tok, err := env.Token(ctx); err == nil {
		return tok, nil
	} else if !errors.Is(err, trackergithub.ErrNoToken) {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.token != "" && now.Before(s.expiresAt) {
		return s.token, nil
	}
	cmdCtx, cancel := context.WithTimeout(ctx, trackerTokenCommandTimeout)
	defer cancel()
	out, err := aoprocess.CommandContext(cmdCtx, "gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", trackergithub.ErrNoToken
	}
	s.token = token
	s.expiresAt = now.Add(trackerTokenCacheTTL)
	return token, nil
}
