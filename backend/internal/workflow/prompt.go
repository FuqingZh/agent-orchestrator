package workflow

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// RenderPrompt renders the supported strict Liquid-style interpolation subset.
// P0 intentionally supports variables but no filters or tags; unknown
// variables, filters, and tags fail instead of silently producing bad prompts.
func RenderPrompt(template string, data PromptData) (string, error) {
	if strings.TrimSpace(template) == "" {
		return DefaultPrompt, nil
	}
	if strings.Contains(template, "{%") || strings.Contains(template, "%}") {
		return "", errorf(ErrTemplateParse, "", "template tags are not supported by symphony-subset-v1")
	}

	var rendered strings.Builder
	remaining := template
	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			if strings.Contains(remaining, "}}") {
				return "", errorf(ErrTemplateParse, "", "closing interpolation without an opening delimiter")
			}
			rendered.WriteString(remaining)
			break
		}
		rendered.WriteString(remaining[:start])
		remaining = remaining[start+2:]
		end := strings.Index(remaining, "}}")
		if end < 0 {
			return "", errorf(ErrTemplateParse, "", "interpolation has no closing delimiter")
		}
		expression := strings.TrimSpace(remaining[:end])
		if expression == "" {
			return "", errorf(ErrTemplateParse, "", "interpolation expression is empty")
		}
		if before, after, ok := strings.Cut(expression, "|"); ok {
			filter := strings.TrimSpace(after)
			return "", errorf(ErrTemplateRender, "", "unknown filter %q in %q", filter, strings.TrimSpace(before))
		}
		value, err := resolveVariable(expression, data)
		if err != nil {
			return "", err
		}
		rendered.WriteString(value)
		remaining = remaining[end+2:]
	}
	return rendered.String(), nil
}

func resolveVariable(name string, data PromptData) (string, error) {
	if name == "attempt" {
		if data.Attempt == nil {
			return "", nil
		}
		return strconv.Itoa(*data.Attempt), nil
	}
	if !strings.HasPrefix(name, "issue.") {
		return "", errorf(ErrTemplateRender, "", "unknown variable %q", name)
	}
	switch strings.TrimPrefix(name, "issue.") {
	case "id":
		return data.Issue.ID, nil
	case "native_ref":
		return encodeJSON(data.Issue.NativeRef)
	case "identifier":
		return data.Issue.Identifier, nil
	case "title":
		return data.Issue.Title, nil
	case "description":
		return optionalString(data.Issue.Description), nil
	case "priority":
		return optionalInt(data.Issue.Priority), nil
	case "state":
		return data.Issue.State, nil
	case "branch_name":
		return optionalString(data.Issue.BranchName), nil
	case "url":
		return optionalString(data.Issue.URL), nil
	case "assignee_id":
		return optionalString(data.Issue.AssigneeID), nil
	case "labels":
		return strings.Join(data.Issue.Labels, ", "), nil
	case "blocked_by":
		return encodeJSON(data.Issue.BlockedBy)
	case "dispatchable":
		return strconv.FormatBool(data.Issue.Dispatchable), nil
	case "created_at":
		return optionalTime(data.Issue.CreatedAt), nil
	case "updated_at":
		return optionalTime(data.Issue.UpdatedAt), nil
	default:
		return "", errorf(ErrTemplateRender, "", "unknown variable %q", name)
	}
}

func encodeJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errorf(ErrTemplateRender, "", "encode template value: %v", err)
	}
	return string(encoded), nil
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
