package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	yaml "gopkg.in/yaml.v3"
)

type Options struct {
	Getenv  func(string) string
	HomeDir string
	TempDir string
}

type rawConfig struct {
	Tracker struct {
		Kind           string         `yaml:"kind"`
		Provider       map[string]any `yaml:"provider"`
		RequiredLabels []string       `yaml:"required_labels"`
		ActiveStates   []string       `yaml:"active_states"`
		TerminalStates []string       `yaml:"terminal_states"`
	} `yaml:"tracker"`
	Polling struct {
		IntervalMS *int64 `yaml:"interval_ms"`
	} `yaml:"polling"`
	Workspace struct {
		Root string `yaml:"root"`
	} `yaml:"workspace"`
	Hooks struct {
		AfterCreate  string `yaml:"after_create"`
		BeforeRun    string `yaml:"before_run"`
		AfterRun     string `yaml:"after_run"`
		BeforeRemove string `yaml:"before_remove"`
		TimeoutMS    *int64 `yaml:"timeout_ms"`
	} `yaml:"hooks"`
	Agent struct {
		MaxConcurrentAgents        *int           `yaml:"max_concurrent_agents"`
		MaxTurns                   *int           `yaml:"max_turns"`
		MaxRetryBackoffMS          *int64         `yaml:"max_retry_backoff_ms"`
		MaxConcurrentAgentsByState map[string]any `yaml:"max_concurrent_agents_by_state"`
	} `yaml:"agent"`
	Codex struct {
		Command           *string `yaml:"command"`
		ApprovalPolicy    any     `yaml:"approval_policy"`
		ThreadSandbox     any     `yaml:"thread_sandbox"`
		TurnSandboxPolicy any     `yaml:"turn_sandbox_policy"`
		TurnTimeoutMS     *int64  `yaml:"turn_timeout_ms"`
		ReadTimeoutMS     *int64  `yaml:"read_timeout_ms"`
		StallTimeoutMS    *int64  `yaml:"stall_timeout_ms"`
	} `yaml:"codex"`
}

// ResolvePath applies the Symphony workflow path precedence.
func ResolvePath(explicit, cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	selected := strings.TrimSpace(explicit)
	if selected == "" {
		selected = "WORKFLOW.md"
	}
	if !filepath.IsAbs(selected) {
		selected = filepath.Join(cwd, selected)
	}
	return filepath.Abs(selected)
}

// Load reads, parses, defaults, and resolves one workflow revision.
func Load(path string, opts Options) (Workflow, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Workflow{}, errorf(ErrWorkflowParse, path, "resolve path: %v", err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Workflow{}, &Error{Kind: ErrMissingWorkflowFile, Path: absPath, Err: err}
		}
		return Workflow{}, errorf(ErrWorkflowParse, absPath, "read: %v", err)
	}
	definition, raw, err := parse(absPath, content)
	if err != nil {
		return Workflow{}, err
	}
	cfg, err := effectiveConfig(absPath, raw, opts)
	if err != nil {
		return Workflow{}, err
	}
	sum := sha256.Sum256(content)
	return Workflow{
		Profile:    CompatibilityProfile,
		Path:       absPath,
		Revision:   hex.EncodeToString(sum[:]),
		Definition: definition,
		Config:     cfg,
	}, nil
}

func parse(path string, content []byte) (Definition, rawConfig, error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	frontMatter := ""
	body := text
	if firstLine(text) == "---" {
		lines := strings.Split(text, "\n")
		end := -1
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				end = i
				break
			}
		}
		if end < 0 {
			return Definition{}, rawConfig{}, errorf(ErrWorkflowParse, path, "front matter has no closing delimiter")
		}
		frontMatter = strings.Join(lines[1:end], "\n")
		body = strings.Join(lines[end+1:], "\n")
	}

	configMap := map[string]any{}
	var raw rawConfig
	if strings.TrimSpace(frontMatter) != "" {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(frontMatter), &node); err != nil {
			return Definition{}, rawConfig{}, errorf(ErrWorkflowParse, path, "decode front matter: %v", err)
		}
		if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
			return Definition{}, rawConfig{}, errorf(ErrWorkflowFrontMatterNotMap, path, "front matter must decode to an object")
		}
		if err := yaml.Unmarshal([]byte(frontMatter), &configMap); err != nil {
			return Definition{}, rawConfig{}, errorf(ErrWorkflowParse, path, "decode config map: %v", err)
		}
		if err := yaml.Unmarshal([]byte(frontMatter), &raw); err != nil {
			return Definition{}, rawConfig{}, errorf(ErrWorkflowParse, path, "decode typed config: %v", err)
		}
	}
	return Definition{Config: configMap, PromptTemplate: strings.TrimSpace(body)}, raw, nil
}

func firstLine(text string) string {
	if before, _, ok := strings.Cut(text, "\n"); ok {
		return before
	}
	return text
}

func effectiveConfig(path string, raw rawConfig, opts Options) (Config, error) {
	cfg := Config{
		Tracker: TrackerConfig{
			Kind:           strings.ToLower(strings.TrimSpace(raw.Tracker.Kind)),
			Provider:       cloneMap(raw.Tracker.Provider),
			RequiredLabels: normalizeStrings(raw.Tracker.RequiredLabels),
			ActiveStates:   normalizeStrings(raw.Tracker.ActiveStates),
			TerminalStates: normalizeStrings(raw.Tracker.TerminalStates),
		},
		Polling: PollingConfig{Interval: DefaultPollingInterval},
		Hooks: HooksConfig{
			AfterCreate:  raw.Hooks.AfterCreate,
			BeforeRun:    raw.Hooks.BeforeRun,
			AfterRun:     raw.Hooks.AfterRun,
			BeforeRemove: raw.Hooks.BeforeRemove,
			Timeout:      DefaultHookTimeout,
		},
		Agent: AgentConfig{
			MaxConcurrentAgents:        DefaultMaxConcurrentAgents,
			MaxTurns:                   DefaultMaxTurns,
			MaxRetryBackoff:            DefaultMaxRetryBackoff,
			MaxConcurrentAgentsByState: normalizeConcurrency(raw.Agent.MaxConcurrentAgentsByState),
		},
		Codex: CodexConfig{
			Command:           DefaultCodexCommand,
			ApprovalPolicy:    raw.Codex.ApprovalPolicy,
			ThreadSandbox:     raw.Codex.ThreadSandbox,
			TurnSandboxPolicy: raw.Codex.TurnSandboxPolicy,
			TurnTimeout:       DefaultCodexTurnTimeout,
			ReadTimeout:       DefaultCodexReadTimeout,
			StallTimeout:      DefaultCodexStallTimeout,
		},
	}

	if err := applyPositiveDuration(path, "polling.interval_ms", raw.Polling.IntervalMS, &cfg.Polling.Interval); err != nil {
		return Config{}, err
	}
	if err := applyPositiveDuration(path, "hooks.timeout_ms", raw.Hooks.TimeoutMS, &cfg.Hooks.Timeout); err != nil {
		return Config{}, err
	}
	if err := applyPositiveInt(path, "agent.max_concurrent_agents", raw.Agent.MaxConcurrentAgents, &cfg.Agent.MaxConcurrentAgents); err != nil {
		return Config{}, err
	}
	if err := applyPositiveInt(path, "agent.max_turns", raw.Agent.MaxTurns, &cfg.Agent.MaxTurns); err != nil {
		return Config{}, err
	}
	if err := applyPositiveDuration(path, "agent.max_retry_backoff_ms", raw.Agent.MaxRetryBackoffMS, &cfg.Agent.MaxRetryBackoff); err != nil {
		return Config{}, err
	}
	if raw.Codex.Command != nil {
		cfg.Codex.Command = *raw.Codex.Command
	}
	if err := applyPositiveDuration(path, "codex.turn_timeout_ms", raw.Codex.TurnTimeoutMS, &cfg.Codex.TurnTimeout); err != nil {
		return Config{}, err
	}
	if err := applyPositiveDuration(path, "codex.read_timeout_ms", raw.Codex.ReadTimeoutMS, &cfg.Codex.ReadTimeout); err != nil {
		return Config{}, err
	}
	if raw.Codex.StallTimeoutMS != nil {
		cfg.Codex.StallTimeout = time.Duration(*raw.Codex.StallTimeoutMS) * time.Millisecond
	}

	root := strings.TrimSpace(raw.Workspace.Root)
	if root == "" {
		tempDir := opts.TempDir
		if tempDir == "" {
			tempDir = os.TempDir()
		}
		root = filepath.Join(tempDir, DefaultWorkspaceDirectoryName)
	} else {
		getenv := opts.Getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		var missing []string
		root = os.Expand(root, func(name string) string {
			value := getenv(name)
			if value == "" {
				missing = append(missing, name)
			}
			return value
		})
		if len(missing) > 0 {
			return Config{}, errorf(ErrWorkflowValidation, path, "workspace.root references missing environment variable %q", missing[0])
		}
		if root == "" {
			return Config{}, errorf(ErrWorkflowValidation, path, "workspace.root resolved to an empty path")
		}
		home := opts.HomeDir
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		switch {
		case root == "~":
			root = home
		case strings.HasPrefix(root, "~/"):
			root = filepath.Join(home, strings.TrimPrefix(root, "~/"))
		}
		if !filepath.IsAbs(root) {
			root = filepath.Join(filepath.Dir(path), root)
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Config{}, errorf(ErrWorkflowValidation, path, "resolve workspace.root: %v", err)
	}
	cfg.Workspace.Root = filepath.Clean(root)
	return cfg, nil
}

func applyPositiveDuration(path, field string, raw *int64, dst *time.Duration) error {
	if raw == nil {
		return nil
	}
	if *raw <= 0 {
		return errorf(ErrWorkflowValidation, path, "%s must be positive", field)
	}
	*dst = time.Duration(*raw) * time.Millisecond
	return nil
}

func applyPositiveInt(path, field string, raw *int, dst *int) error {
	if raw == nil {
		return nil
	}
	if *raw <= 0 {
		return errorf(ErrWorkflowValidation, path, "%s must be positive", field)
	}
	*dst = *raw
	return nil
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.ToLower(strings.TrimSpace(value)))
	}
	return out
}

func normalizeConcurrency(values map[string]any) map[string]int {
	out := make(map[string]int)
	for state, raw := range values {
		state = strings.ToLower(strings.TrimSpace(state))
		if state == "" {
			continue
		}
		var value int
		switch n := raw.(type) {
		case int:
			value = n
		case int64:
			value = int(n)
		case uint64:
			if uint64(int(n)) != n {
				continue
			}
			value = int(n)
		default:
			continue
		}
		if value > 0 {
			out[state] = value
		}
	}
	return out
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}

// ValidateForDispatch applies the runtime preflight checks that are independent
// of a provider adapter. supportedKinds is the set available in this build.
func ValidateForDispatch(workflow Workflow, supportedKinds map[string]bool) error {
	kind := strings.ToLower(strings.TrimSpace(workflow.Config.Tracker.Kind))
	if kind == "" {
		return errorf(ErrWorkflowValidation, workflow.Path, "tracker.kind is required for dispatch")
	}
	if !supportedKinds[kind] {
		return errorf(ErrWorkflowValidation, workflow.Path, "tracker.kind %q is not supported", kind)
	}
	if strings.TrimSpace(workflow.Config.Codex.Command) == "" {
		return errorf(ErrWorkflowValidation, workflow.Path, "codex.command must not be empty")
	}
	return nil
}

// Reloader retains the last valid workflow when a later filesystem revision
// cannot be parsed or validated. Runtime owners call Reload defensively at
// their tick/dispatch boundary.
type Reloader struct {
	path     string
	options  Options
	validate []func(Workflow) error

	mu      sync.RWMutex
	current *Workflow
}

func NewReloader(path string, opts Options, validators ...func(Workflow) error) *Reloader {
	return &Reloader{path: path, options: opts, validate: validators}
}

// Reload returns the effective workflow, whether its revision changed, and
// any error from the attempted revision. After the first success, an error is
// returned alongside the last known good workflow.
func (r *Reloader) Reload() (Workflow, bool, error) {
	next, err := Load(r.path, r.options)
	for _, validate := range r.validate {
		if err == nil && validate != nil {
			err = validate(next)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		if r.current != nil {
			return *r.current, false, err
		}
		return Workflow{}, false, err
	}
	if r.current != nil && r.current.Revision == next.Revision {
		return *r.current, false, nil
	}
	r.current = &next
	return next, true, nil
}

func (r *Reloader) Current() (Workflow, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current == nil {
		return Workflow{}, false
	}
	return *r.current, true
}
