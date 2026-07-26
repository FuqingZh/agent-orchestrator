package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestRenderPromptStrictVariables(t *testing.T) {
	description := "Implement strict workflow loading."
	priority := 1
	url := "https://github.com/FuqingZh/agent-orchestrator/issues/42"
	created := time.Date(2026, 7, 26, 1, 2, 3, 0, time.FixedZone("offset", 8*60*60))
	attempt := 2
	got, err := RenderPrompt(
		"{{ issue.identifier }} | {{ issue.title }} | {{ issue.description }} | {{ issue.priority }} | {{ issue.labels }} | {{ issue.url }} | {{ issue.created_at }} | {{ attempt }}",
		PromptData{
			Issue: Issue{
				Identifier:  "AO-42",
				Title:       "Workflow contract",
				Description: &description,
				Priority:    &priority,
				Labels:      []string{"symphony", "backend"},
				URL:         &url,
				CreatedAt:   &created,
			},
			Attempt: &attempt,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "AO-42 | Workflow contract | Implement strict workflow loading. | 1 | symphony, backend | https://github.com/FuqingZh/agent-orchestrator/issues/42 | 2026-07-25T17:02:03Z | 2"
	if got != want {
		t.Fatalf("rendered = %q, want %q", got, want)
	}
}

func TestRenderPromptFirstAttemptAndDefault(t *testing.T) {
	got, err := RenderPrompt("attempt={{ attempt }}", PromptData{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "attempt=" {
		t.Fatalf("first attempt = %q", got)
	}
	got, err = RenderPrompt(" \n", PromptData{})
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultPrompt {
		t.Fatalf("default = %q", got)
	}
}

func TestRenderPromptRejectsUnknownSyntax(t *testing.T) {
	tests := []struct {
		name     string
		template string
		kind     ErrorKind
	}{
		{name: "unknown variable", template: "{{ issue.missing }}", kind: ErrTemplateRender},
		{name: "unknown root", template: "{{ project.name }}", kind: ErrTemplateRender},
		{name: "unknown filter", template: "{{ issue.title | upcase }}", kind: ErrTemplateRender},
		{name: "tag", template: "{% if issue.title %}x{% endif %}", kind: ErrTemplateParse},
		{name: "unclosed", template: "{{ issue.title", kind: ErrTemplateParse},
		{name: "orphan close", template: "issue.title }}", kind: ErrTemplateParse},
		{name: "empty expression", template: "{{ }}", kind: ErrTemplateParse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenderPrompt(tt.template, PromptData{})
			assertErrorKind(t, err, tt.kind)
			if !strings.Contains(err.Error(), string(tt.kind)) {
				t.Fatalf("error %q does not expose kind %q", err, tt.kind)
			}
		})
	}
}
