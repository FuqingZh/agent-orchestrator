package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TryClaimWorkflowIssue atomically reserves one project/issue pair. An existing
// claimed, running, or retry-queued row wins; a released row may be reclaimed.
func (s *Store) TryClaimWorkflowIssue(ctx context.Context, rec domain.WorkflowRunRecord) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, err := s.writeDB.ExecContext(ctx, `
INSERT INTO workflow_issue_runs (
    project_id, issue_id, issue_identifier, session_id, workflow_revision,
    state, attempt, retry_due_at, last_error, terminal_reason, updated_at
) VALUES (?, ?, ?, NULL, ?, 'claimed', ?, NULL, '', '', ?)
ON CONFLICT(project_id, issue_id) DO UPDATE SET
    issue_identifier = excluded.issue_identifier,
    session_id = NULL,
    workflow_revision = excluded.workflow_revision,
    state = 'claimed',
    attempt = excluded.attempt,
    retry_due_at = NULL,
    last_error = '',
    terminal_reason = '',
    updated_at = excluded.updated_at
WHERE workflow_issue_runs.state = 'released'`,
		rec.ProjectID, rec.IssueID, rec.IssueIdentifier, rec.WorkflowRevision,
		rec.Attempt, rec.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("claim workflow issue %s/%s: %w", rec.ProjectID, rec.IssueID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim workflow issue %s/%s rows affected: %w", rec.ProjectID, rec.IssueID, err)
	}
	return n == 1, nil
}

// TryClaimDueWorkflowRetry atomically transitions one due retry back to a
// reservation. It leaves a queued retry untouched when its deadline has not
// arrived or another observer already claimed it.
func (s *Store) TryClaimDueWorkflowRetry(ctx context.Context, rec domain.WorkflowRunRecord, dueAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, err := s.writeDB.ExecContext(ctx, `
UPDATE workflow_issue_runs
SET issue_identifier = ?, session_id = NULL, workflow_revision = ?,
    state = 'claimed', attempt = ?, retry_due_at = NULL, last_error = '',
    terminal_reason = '', updated_at = ?
WHERE project_id = ? AND issue_id = ? AND state = 'retry_queued'
    AND retry_due_at <= ?`,
		rec.IssueIdentifier, rec.WorkflowRevision, rec.Attempt, rec.UpdatedAt,
		rec.ProjectID, rec.IssueID, dueAt,
	)
	if err != nil {
		return false, fmt.Errorf("claim due workflow retry %s/%s: %w", rec.ProjectID, rec.IssueID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim due workflow retry %s/%s rows affected: %w", rec.ProjectID, rec.IssueID, err)
	}
	return n == 1, nil
}

// BindWorkflowIssueSession transitions a reservation to running after Spawn
// returns the durable AO session identity.
func (s *Store) BindWorkflowIssueSession(ctx context.Context, projectID domain.ProjectID, issueID domain.IssueID, sessionID domain.SessionID, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, err := s.writeDB.ExecContext(ctx, `
UPDATE workflow_issue_runs
SET session_id = ?, state = 'running', retry_due_at = NULL,
    last_error = '', terminal_reason = '', updated_at = ?
WHERE project_id = ? AND issue_id = ? AND state = 'claimed'`,
		sessionID, updatedAt, projectID, issueID,
	)
	if err != nil {
		return false, fmt.Errorf("bind workflow issue %s/%s to session %s: %w", projectID, issueID, sessionID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("bind workflow issue %s/%s rows affected: %w", projectID, issueID, err)
	}
	return n == 1, nil
}

// QueueWorkflowRetry records a restart-safe retry while retaining the owning
// session and workflow revision for operator/debug surfaces.
func (s *Store) QueueWorkflowRetry(ctx context.Context, rec domain.WorkflowRunRecord) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, err := s.writeDB.ExecContext(ctx, `
UPDATE workflow_issue_runs
SET state = 'retry_queued', attempt = ?, retry_due_at = ?, last_error = ?,
    terminal_reason = '', workflow_revision = ?, updated_at = ?
WHERE project_id = ? AND issue_id = ? AND state <> 'released'`,
		rec.Attempt, rec.RetryDueAt, rec.LastError, rec.WorkflowRevision,
		rec.UpdatedAt, rec.ProjectID, rec.IssueID,
	)
	if err != nil {
		return false, fmt.Errorf("queue workflow retry %s/%s: %w", rec.ProjectID, rec.IssueID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("queue workflow retry %s/%s rows affected: %w", rec.ProjectID, rec.IssueID, err)
	}
	return n == 1, nil
}

// ReleaseWorkflowIssue removes the active claim without deleting its last
// session/retry facts.
func (s *Store) ReleaseWorkflowIssue(ctx context.Context, projectID domain.ProjectID, issueID domain.IssueID, reason string, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, err := s.writeDB.ExecContext(ctx, `
UPDATE workflow_issue_runs
SET state = 'released', retry_due_at = NULL, terminal_reason = ?, updated_at = ?
WHERE project_id = ? AND issue_id = ? AND state <> 'released'`,
		reason, updatedAt, projectID, issueID,
	)
	if err != nil {
		return false, fmt.Errorf("release workflow issue %s/%s: %w", projectID, issueID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("release workflow issue %s/%s rows affected: %w", projectID, issueID, err)
	}
	return n == 1, nil
}

// GetWorkflowRun returns the durable scheduler row for one project/issue.
func (s *Store) GetWorkflowRun(ctx context.Context, projectID domain.ProjectID, issueID domain.IssueID) (domain.WorkflowRunRecord, bool, error) {
	row := s.readDB.QueryRowContext(ctx, workflowRunSelect+`
WHERE project_id = ? AND issue_id = ?`, projectID, issueID)
	rec, err := scanWorkflowRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkflowRunRecord{}, false, nil
	}
	if err != nil {
		return domain.WorkflowRunRecord{}, false, fmt.Errorf("get workflow run %s/%s: %w", projectID, issueID, err)
	}
	return rec, true, nil
}

// ListDueWorkflowRetries returns retry-queued rows due at or before now.
func (s *Store) ListDueWorkflowRetries(ctx context.Context, now time.Time) ([]domain.WorkflowRunRecord, error) {
	rows, err := s.readDB.QueryContext(ctx, workflowRunSelect+`
WHERE state = 'retry_queued' AND retry_due_at <= ?
ORDER BY retry_due_at, project_id, issue_id`, now)
	if err != nil {
		return nil, fmt.Errorf("list due workflow retries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.WorkflowRunRecord
	for rows.Next() {
		rec, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan due workflow retry: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due workflow retries: %w", err)
	}
	return out, nil
}

// ListActiveWorkflowRuns returns every claimed/running/retry-queued row so a
// reconstructed observer can reconcile durable scheduler state before dispatch.
func (s *Store) ListActiveWorkflowRuns(ctx context.Context) ([]domain.WorkflowRunRecord, error) {
	rows, err := s.readDB.QueryContext(ctx, workflowRunSelect+`
WHERE state <> 'released'
ORDER BY project_id, issue_id`)
	if err != nil {
		return nil, fmt.Errorf("list active workflow runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.WorkflowRunRecord
	for rows.Next() {
		rec, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan active workflow run: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active workflow runs: %w", err)
	}
	return out, nil
}

const workflowRunSelect = `
SELECT project_id, issue_id, issue_identifier, COALESCE(session_id, ''),
       workflow_revision, state, attempt, retry_due_at, last_error,
       terminal_reason, updated_at
FROM workflow_issue_runs
`

type workflowRunScanner interface {
	Scan(dest ...any) error
}

func scanWorkflowRun(row workflowRunScanner) (domain.WorkflowRunRecord, error) {
	var rec domain.WorkflowRunRecord
	var retryDueAt sql.NullTime
	err := row.Scan(
		&rec.ProjectID, &rec.IssueID, &rec.IssueIdentifier, &rec.SessionID,
		&rec.WorkflowRevision, &rec.State, &rec.Attempt, &retryDueAt,
		&rec.LastError, &rec.TerminalReason, &rec.UpdatedAt,
	)
	if retryDueAt.Valid {
		rec.RetryDueAt = retryDueAt.Time
	}
	return rec, err
}
