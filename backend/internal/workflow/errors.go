package workflow

import "fmt"

type ErrorKind string

const (
	ErrMissingWorkflowFile       ErrorKind = "missing_workflow_file"
	ErrWorkflowParse             ErrorKind = "workflow_parse_error"
	ErrWorkflowFrontMatterNotMap ErrorKind = "workflow_front_matter_not_a_map"
	ErrWorkflowValidation        ErrorKind = "workflow_validation_error"
	ErrTemplateParse             ErrorKind = "template_parse_error"
	ErrTemplateRender            ErrorKind = "template_render_error"
)

// Error gives callers a stable operator-facing error class while preserving
// the underlying filesystem, YAML, validation, or template error.
type Error struct {
	Kind ErrorKind
	Path string
	Err  error
}

func (e *Error) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("%s: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("%s %q: %v", e.Kind, e.Path, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func errorf(kind ErrorKind, path, format string, args ...any) error {
	return &Error{Kind: kind, Path: path, Err: fmt.Errorf(format, args...)}
}
