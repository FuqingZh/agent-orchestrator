// Package workflow loads and validates repository-owned Symphony workflow
// contracts without starting trackers, sessions, or runtime processes.
package workflow

import "time"

// Compatibility and default values define AO's supported Symphony subset.
const (
	CompatibilityProfile          = "symphony-subset-v1"
	DefaultPrompt                 = "You are working on an issue from the configured tracker."
	DefaultPollingInterval        = 30 * time.Second
	DefaultHookTimeout            = 60 * time.Second
	DefaultMaxConcurrentAgents    = 10
	DefaultMaxTurns               = 20
	DefaultMaxRetryBackoff        = 5 * time.Minute
	DefaultCodexCommand           = "codex app-server"
	DefaultCodexTurnTimeout       = time.Hour
	DefaultCodexReadTimeout       = 5 * time.Second
	DefaultCodexStallTimeout      = 5 * time.Minute
	DefaultWorkspaceDirectoryName = "symphony_workspaces"
)

// Definition is the parsed, provider-neutral WORKFLOW.md payload.
type Definition struct {
	Config         map[string]any
	PromptTemplate string
}

// Workflow is one validated workflow revision and its effective typed config.
type Workflow struct {
	Profile    string
	Path       string
	Revision   string
	Definition Definition
	Config     Config
}

// Config is the typed view of workflow front matter after defaults and path
// resolution are applied.
type Config struct {
	Tracker   TrackerConfig
	Polling   PollingConfig
	Workspace WorkspaceConfig
	Hooks     HooksConfig
	Agent     AgentConfig
	Codex     CodexConfig
}

// TrackerConfig selects the tracker adapter and issue eligibility rules.
type TrackerConfig struct {
	Kind           string
	Provider       map[string]any
	RequiredLabels []string
	ActiveStates   []string
	TerminalStates []string
}

// PollingConfig controls how often the workflow tracker is reconciled.
type PollingConfig struct {
	Interval time.Duration
}

// WorkspaceConfig identifies the root for isolated issue workspaces.
type WorkspaceConfig struct {
	Root string
}

// HooksConfig contains lifecycle commands and their shared timeout.
type HooksConfig struct {
	AfterCreate  string
	BeforeRun    string
	AfterRun     string
	BeforeRemove string
	Timeout      time.Duration
}

// AgentConfig contains global and state-specific dispatch limits and retry
// bounds.
type AgentConfig struct {
	MaxConcurrentAgents        int
	MaxTurns                   int
	MaxRetryBackoff            time.Duration
	MaxConcurrentAgentsByState map[string]int
}

// CodexConfig contains the app-server command and per-thread runtime policies.
type CodexConfig struct {
	Command           string
	ApprovalPolicy    any
	ThreadSandbox     any
	TurnSandboxPolicy any
	TurnTimeout       time.Duration
	ReadTimeout       time.Duration
	StallTimeout      time.Duration
}

// Issue is the normalized prompt input defined by the Symphony contract.
// Provider-specific, non-secret identifiers stay opaque in NativeRef.
type Issue struct {
	ID           string
	NativeRef    map[string]any
	Identifier   string
	Title        string
	Description  *string
	Priority     *int
	State        string
	BranchName   *string
	URL          *string
	AssigneeID   *string
	Labels       []string
	BlockedBy    []BlockerRef
	Dispatchable bool
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

// BlockerRef is the provider-neutral identity and state of a blocking issue.
type BlockerRef struct {
	ID         *string `json:"id"`
	Identifier *string `json:"identifier"`
	State      *string `json:"state"`
}

// PromptData is the strict template input. Attempt is nil on the first run.
type PromptData struct {
	Issue   Issue
	Attempt *int
}
