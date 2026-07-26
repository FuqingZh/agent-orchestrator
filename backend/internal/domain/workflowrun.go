package domain

import "time"

// WorkflowRunState is the durable scheduler state for one workflow issue.
// Tracker state remains provider-owned; this state only prevents duplicate AO
// dispatch and records whether the issue is running, waiting to retry, or
// released.
type WorkflowRunState string

const (
	WorkflowRunClaimed     WorkflowRunState = "claimed"
	WorkflowRunRunning     WorkflowRunState = "running"
	WorkflowRunRetryQueued WorkflowRunState = "retry_queued"
	WorkflowRunReleased    WorkflowRunState = "released"
)

// WorkflowRunRecord is the restart-safe scheduling fact for one canonical
// issue within one project. SessionID is empty only during the short reservation
// window before the session service returns the newly created session.
type WorkflowRunRecord struct {
	ProjectID        ProjectID
	IssueID          IssueID
	IssueIdentifier  string
	SessionID        SessionID
	WorkflowRevision string
	State            WorkflowRunState
	Attempt          int64
	RetryDueAt       time.Time
	LastError        string
	TerminalReason   string
	UpdatedAt        time.Time
}
