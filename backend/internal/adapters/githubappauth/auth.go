// Package githubappauth provides short-lived GitHub App installation tokens.
package githubappauth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
)

const (
	appIDEnv          = "AO_GITHUB_APP_ID"
	installationIDEnv = "AO_GITHUB_APP_INSTALLATION_ID"
	privateKeyFileEnv = "AO_GITHUB_APP_PRIVATE_KEY_FILE"
)

type tokenProvider interface {
	Token(context.Context) (string, error)
}

// Config identifies one GitHub App installation and its private key.
type Config struct {
	AppID          int64
	InstallationID int64
	PrivateKeyFile string
}

// Source refreshes short-lived installation tokens before they expire.
type Source struct {
	provider tokenProvider
}

// New validates cfg and constructs a lazy, concurrency-safe token source.
// No installation token is requested until Token is first called.
func New(cfg Config) (*Source, error) {
	if err := validate(cfg); err != nil {
		return nil, err
	}
	transport, err := ghinstallation.NewKeyFromFile(
		http.DefaultTransport,
		cfg.AppID,
		cfg.InstallationID,
		cfg.PrivateKeyFile,
	)
	if err != nil {
		return nil, fmt.Errorf("github app authentication: load private key: %w", err)
	}
	return &Source{provider: transport}, nil
}

// NewFromEnvironment returns a source when any GitHub App setting is present.
// A partially configured installation is an error so callers cannot silently
// fall back to a broader credential.
func NewFromEnvironment(getenv func(string) string) (*Source, bool, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	appIDText := strings.TrimSpace(getenv(appIDEnv))
	installationIDText := strings.TrimSpace(getenv(installationIDEnv))
	privateKeyFile := strings.TrimSpace(getenv(privateKeyFileEnv))
	if appIDText == "" && installationIDText == "" && privateKeyFile == "" {
		return nil, false, nil
	}
	if appIDText == "" || installationIDText == "" || privateKeyFile == "" {
		return nil, true, fmt.Errorf(
			"github app authentication: %s, %s, and %s must be configured together",
			appIDEnv,
			installationIDEnv,
			privateKeyFileEnv,
		)
	}
	appID, err := parsePositiveID(appIDEnv, appIDText)
	if err != nil {
		return nil, true, err
	}
	installationID, err := parsePositiveID(installationIDEnv, installationIDText)
	if err != nil {
		return nil, true, err
	}
	source, err := New(Config{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKeyFile: privateKeyFile,
	})
	if err != nil {
		return nil, true, err
	}
	return source, true, nil
}

// Token returns a current installation token, refreshing it when necessary.
func (s *Source) Token(ctx context.Context) (string, error) {
	if s == nil || s.provider == nil {
		return "", fmt.Errorf("github app authentication: token source is not initialized")
	}
	return s.provider.Token(ctx)
}

func parsePositiveID(name, value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("github app authentication: %s must be a positive integer", name)
	}
	return id, nil
}

func validate(cfg Config) error {
	if cfg.AppID <= 0 {
		return fmt.Errorf("github app authentication: app ID must be positive")
	}
	if cfg.InstallationID <= 0 {
		return fmt.Errorf("github app authentication: installation ID must be positive")
	}
	if !filepath.IsAbs(cfg.PrivateKeyFile) {
		return fmt.Errorf("github app authentication: private key path must be absolute")
	}
	info, err := os.Lstat(cfg.PrivateKeyFile)
	if err != nil {
		return fmt.Errorf("github app authentication: inspect private key: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("github app authentication: private key must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("github app authentication: private key must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("github app authentication: private key permissions must not allow group or other access")
	}
	return nil
}
