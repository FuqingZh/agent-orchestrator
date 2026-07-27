package daemon

import (
	"context"
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/githubappauth"
	trackergithub "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/github"
)

type githubTokenSource interface {
	Token(context.Context) (string, error)
}

// configuredGitHubAppTokenSource preserves an explicitly scoped static token
// for compatibility. Otherwise, any App configuration is authoritative:
// invalid or partial settings fail closed instead of falling back to a broader
// host credential.
func configuredGitHubAppTokenSource() (githubTokenSource, bool, error) {
	if strings.TrimSpace(os.Getenv("AO_GITHUB_TOKEN")) != "" {
		return nil, false, nil
	}
	source, configured, err := githubappauth.NewFromEnvironment(os.Getenv)
	if source == nil {
		return nil, configured, err
	}
	return source, configured, err
}

type trackerPromptTokenSource struct {
	app    githubTokenSource
	appErr error
}

func newTrackerPromptTokenSource() trackergithub.TokenSource {
	app, _, err := configuredGitHubAppTokenSource()
	return &trackerPromptTokenSource{app: app, appErr: err}
}

func (s *trackerPromptTokenSource) Token(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("AO_GITHUB_TOKEN")); token != "" {
		return token, nil
	}
	if s.appErr != nil {
		return "", s.appErr
	}
	if s.app != nil {
		return s.app.Token(ctx)
	}
	return trackergithub.EnvTokenSource{EnvVars: []string{"AO_GITHUB_TOKEN"}}.Token(ctx)
}
