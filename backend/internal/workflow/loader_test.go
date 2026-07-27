package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResolvePathPrecedence(t *testing.T) {
	cwd := t.TempDir()
	got, err := ResolvePath("", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cwd, "WORKFLOW.md"); got != want {
		t.Fatalf("default path = %q, want %q", got, want)
	}
	got, err = ResolvePath("config/custom.md", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cwd, "config", "custom.md"); got != want {
		t.Fatalf("explicit path = %q, want %q", got, want)
	}
}

func TestLoadWithoutFrontMatterUsesDefaults(t *testing.T) {
	path := filepath.Join("testdata", "minimal", "WORKFLOW.md")
	got, err := Load(path, Options{TempDir: "/tmp/spec-temp"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Definition.PromptTemplate != "Work on {{ issue.identifier }}: {{ issue.title }}" {
		t.Fatalf("prompt = %q", got.Definition.PromptTemplate)
	}
	if len(got.Definition.Config) != 0 {
		t.Fatalf("config = %#v, want empty", got.Definition.Config)
	}
	if got.Config.Polling.Interval != DefaultPollingInterval ||
		got.Config.Hooks.Timeout != DefaultHookTimeout ||
		got.Config.Agent.MaxConcurrentAgents != DefaultMaxConcurrentAgents ||
		got.Config.Agent.MaxTurns != DefaultMaxTurns ||
		got.Config.Agent.MaxRetryBackoff != DefaultMaxRetryBackoff ||
		got.Config.Codex.Command != DefaultCodexCommand ||
		got.Config.Codex.TurnTimeout != DefaultCodexTurnTimeout ||
		got.Config.Codex.ReadTimeout != DefaultCodexReadTimeout ||
		got.Config.Codex.StallTimeout != DefaultCodexStallTimeout {
		t.Fatalf("defaults not applied: %#v", got.Config)
	}
	if want := "/tmp/spec-temp/symphony_workspaces"; got.Config.Workspace.Root != want {
		t.Fatalf("workspace root = %q, want %q", got.Config.Workspace.Root, want)
	}
	if len(got.Revision) != 64 {
		t.Fatalf("revision = %q", got.Revision)
	}
	if got.Profile != CompatibilityProfile {
		t.Fatalf("profile = %q, want %q", got.Profile, CompatibilityProfile)
	}
}

func TestLoadFullConfigAndPreserveExtensions(t *testing.T) {
	path := filepath.Join("testdata", "full", "WORKFLOW.md")
	got, err := Load(path, Options{
		Getenv: func(name string) string {
			if name == "WORKSPACE_ROOT" {
				return "/tmp/ao-workflow-fixture"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Tracker.Kind != "github" {
		t.Fatalf("tracker kind = %q", got.Config.Tracker.Kind)
	}
	if got.Config.Tracker.Provider["repo"] != "FuqingZh/agent-orchestrator" ||
		got.Config.Tracker.Provider["token"] != "$AO_GITHUB_TOKEN" {
		t.Fatalf("provider config was not preserved: %#v", got.Config.Tracker.Provider)
	}
	if _, ok := got.Definition.Config["extension"]; !ok {
		t.Fatalf("unknown top-level extension was not preserved: %#v", got.Definition.Config)
	}
	if want := []string{"symphony"}; !reflect.DeepEqual(got.Config.Tracker.RequiredLabels, want) {
		t.Fatalf("required labels = %#v, want %#v", got.Config.Tracker.RequiredLabels, want)
	}
	if got.Config.Polling.Interval != 15*time.Second ||
		got.Config.Hooks.Timeout != 45*time.Second ||
		got.Config.Agent.MaxConcurrentAgents != 4 ||
		got.Config.Agent.MaxTurns != 12 ||
		got.Config.Agent.MaxRetryBackoff != 2*time.Minute {
		t.Fatalf("typed config mismatch: %#v", got.Config)
	}
	if want := map[string]int{"in progress": 2, "review": 1}; !reflect.DeepEqual(got.Config.Agent.MaxConcurrentAgentsByState, want) {
		t.Fatalf("state concurrency = %#v, want %#v", got.Config.Agent.MaxConcurrentAgentsByState, want)
	}
	if got.Config.Workspace.Root != "/tmp/ao-workflow-fixture" {
		t.Fatalf("workspace root = %q", got.Config.Workspace.Root)
	}
}

func TestLoadResolvesRelativeAndHomeWorkspaceRoots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflow(t, path, "---\nworkspace:\n  root: workspaces\n---\nPrompt\n")
	got, err := Load(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "workspaces"); got.Config.Workspace.Root != want {
		t.Fatalf("relative root = %q, want %q", got.Config.Workspace.Root, want)
	}

	writeWorkflow(t, path, "---\nworkspace:\n  root: ~/ao-workspaces\n---\nPrompt\n")
	got, err = Load(path, Options{HomeDir: "/home/tester"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/tester/ao-workspaces"; got.Config.Workspace.Root != want {
		t.Fatalf("home root = %q, want %q", got.Config.Workspace.Root, want)
	}
}

func TestLoadRejectsMissingWorkspaceEnvironmentVariable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflow(t, path, "---\nworkspace:\n  root: $MISSING_ROOT/workspaces\n---\nPrompt\n")
	_, err := Load(path, Options{Getenv: func(string) string { return "" }})
	assertErrorKind(t, err, ErrWorkflowValidation)
}

func TestLoadTypedErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		content *string
		kind    ErrorKind
	}{
		{name: "missing", kind: ErrMissingWorkflowFile},
		{name: "invalid yaml", content: ptr("---\ntracker: [\n---\nPrompt\n"), kind: ErrWorkflowParse},
		{name: "non map", content: ptr("---\n- tracker\n---\nPrompt\n"), kind: ErrWorkflowFrontMatterNotMap},
		{name: "missing delimiter", content: ptr("---\ntracker:\n  kind: github\n"), kind: ErrWorkflowParse},
		{name: "invalid positive duration", content: ptr("---\npolling:\n  interval_ms: 0\n---\nPrompt\n"), kind: ErrWorkflowValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".md")
			if tt.content != nil {
				writeWorkflow(t, path, *tt.content)
			}
			_, err := Load(path, Options{})
			assertErrorKind(t, err, tt.kind)
		})
	}
}

func TestValidateForDispatch(t *testing.T) {
	path := filepath.Join("testdata", "full", "WORKFLOW.md")
	got, err := Load(path, Options{Getenv: func(string) string { return "/tmp/work" }})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateForDispatch(got, map[string]bool{"github": true}); err != nil {
		t.Fatalf("valid dispatch config: %v", err)
	}
	if err := ValidateForDispatch(got, map[string]bool{"linear": true}); err == nil {
		t.Fatal("unsupported tracker unexpectedly validated")
	} else {
		assertErrorKind(t, err, ErrWorkflowValidation)
	}
}

func TestValidateForDispatchRejectsUnknownNormalizedTrackerStates(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		states []string
	}{
		{name: "active", field: "tracker.active_states", states: []string{"open", "in_review"}},
		{name: "terminal", field: "tracker.terminal_states", states: []string{"done", "closed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := Workflow{
				Path: "WORKFLOW.md",
				Config: Config{
					Tracker: TrackerConfig{Kind: "linear"},
					Codex:   CodexConfig{Command: DefaultCodexCommand},
				},
			}
			if tt.name == "active" {
				candidate.Config.Tracker.ActiveStates = tt.states
			} else {
				candidate.Config.Tracker.TerminalStates = tt.states
			}
			err := ValidateForDispatch(candidate, map[string]bool{"linear": true})
			assertErrorKind(t, err, ErrWorkflowValidation)
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("error = %v, want field %q", err, tt.field)
			}
		})
	}
}

func TestReloaderRetainsLastKnownGood(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflow(t, path, "First {{ issue.identifier }}\n")
	reloader := NewReloader(path, Options{})

	first, changed, err := reloader.Reload()
	if err != nil || !changed {
		t.Fatalf("first reload = changed %v err %v", changed, err)
	}
	again, changed, err := reloader.Reload()
	if err != nil || changed || again.Revision != first.Revision {
		t.Fatalf("unchanged reload = %#v changed %v err %v", again, changed, err)
	}

	writeWorkflow(t, path, "---\ntracker: [\n---\nBroken\n")
	retained, changed, err := reloader.Reload()
	if err == nil || changed {
		t.Fatalf("invalid reload = changed %v err %v", changed, err)
	}
	if retained.Revision != first.Revision || retained.Definition.PromptTemplate != first.Definition.PromptTemplate {
		t.Fatalf("did not retain last known good: %#v", retained)
	}

	writeWorkflow(t, path, "Second {{ issue.identifier }}\n")
	second, changed, err := reloader.Reload()
	if err != nil || !changed || second.Revision == first.Revision {
		t.Fatalf("fixed reload = %#v changed %v err %v", second, changed, err)
	}
}

func TestReloaderRetainsLastKnownGoodAfterDispatchValidationFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflow(t, path, "---\ntracker:\n  kind: github\n---\nPrompt\n")
	validate := func(candidate Workflow) error {
		return ValidateForDispatch(candidate, map[string]bool{"github": true})
	}
	reloader := NewReloader(path, Options{}, validate)
	first, changed, err := reloader.Reload()
	if err != nil || !changed {
		t.Fatalf("first reload = changed %v err %v", changed, err)
	}

	writeWorkflow(t, path, "---\ntracker:\n  kind: unsupported\n---\nPrompt\n")
	retained, changed, err := reloader.Reload()
	if err == nil || changed {
		t.Fatalf("invalid reload = changed %v err %v", changed, err)
	}
	if retained.Revision != first.Revision {
		t.Fatalf("revision = %q, want retained %q", retained.Revision, first.Revision)
	}
}

func writeWorkflow(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertErrorKind(t *testing.T, err error, want ErrorKind) {
	t.Helper()
	var workflowErr *Error
	if !errors.As(err, &workflowErr) {
		t.Fatalf("error = %v, want workflow Error", err)
	}
	if workflowErr.Kind != want {
		t.Fatalf("kind = %q, want %q", workflowErr.Kind, want)
	}
}

func ptr(value string) *string {
	return &value
}
