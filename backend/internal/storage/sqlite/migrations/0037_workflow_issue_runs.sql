-- +goose Up
-- workflow_issue_runs is AO's durable extension to Symphony's scheduler state.
-- The primary key is the claim boundary: at most one non-released owner can
-- exist for a canonical issue within a project, including across daemon
-- restarts. Released rows are retained as an audit/debug fact and can be
-- atomically reclaimed by the store.
-- +goose StatementBegin
CREATE TABLE workflow_issue_runs (
    project_id        TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    issue_id          TEXT NOT NULL,
    issue_identifier  TEXT NOT NULL DEFAULT '',
    session_id        TEXT REFERENCES sessions (id) ON DELETE SET NULL,
    workflow_revision TEXT NOT NULL,
    state             TEXT NOT NULL
        CHECK (state IN ('claimed', 'running', 'retry_queued', 'released')),
    attempt           INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    retry_due_at      TIMESTAMP,
    last_error        TEXT NOT NULL DEFAULT '',
    terminal_reason   TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMP NOT NULL,
    PRIMARY KEY (project_id, issue_id)
);
CREATE INDEX idx_workflow_issue_runs_due
    ON workflow_issue_runs (state, retry_due_at);
-- +goose StatementEnd

-- Reuse the existing session invalidation event for workflow facts that already
-- own a session. This keeps the CDC enum stable: clients refetch the owning
-- session, while reservation-only rows with no session remain internal.
-- +goose StatementBegin
CREATE TRIGGER workflow_issue_runs_cdc_insert
AFTER INSERT ON workflow_issue_runs
WHEN NEW.session_id IS NOT NULL
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.session_id, 'session_updated',
        json_object('id', NEW.session_id, 'workflowIssueId', NEW.issue_id),
        NEW.updated_at);
END;
-- +goose StatementEnd

-- Retry bookkeeping is operator-visible state, so every meaningful transition
-- is captured by a database trigger rather than application emit code.
-- +goose StatementBegin
CREATE TRIGGER workflow_issue_runs_cdc_update
AFTER UPDATE ON workflow_issue_runs
WHEN NEW.session_id IS NOT NULL
    AND (
        OLD.session_id IS NOT NEW.session_id
        OR OLD.state <> NEW.state
        OR OLD.attempt <> NEW.attempt
        OR OLD.retry_due_at IS NOT NEW.retry_due_at
        OR OLD.last_error <> NEW.last_error
        OR OLD.terminal_reason <> NEW.terminal_reason
        OR OLD.workflow_revision <> NEW.workflow_revision
    )
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.session_id, 'session_updated',
        json_object('id', NEW.session_id, 'workflowIssueId', NEW.issue_id),
        NEW.updated_at);
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS workflow_issue_runs_cdc_update;
DROP TRIGGER IF EXISTS workflow_issue_runs_cdc_insert;
DROP TABLE IF EXISTS workflow_issue_runs;
-- +goose StatementEnd
