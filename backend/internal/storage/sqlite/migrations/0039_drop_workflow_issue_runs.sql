-- +goose Up
-- The upstream-first tracker intake path uses sessions.issue_id as its only
-- durable deduplication fact. Remove the fork-only scheduler state machine.
-- +goose StatementBegin
DROP TRIGGER IF EXISTS workflow_issue_runs_cdc_update;
DROP TRIGGER IF EXISTS workflow_issue_runs_cdc_insert;
DROP INDEX IF EXISTS idx_workflow_issue_runs_due;
DROP TABLE IF EXISTS workflow_issue_runs;
-- +goose StatementEnd

-- +goose Down
-- The removed workflow scheduler is intentionally not recreated. Restoring it
-- requires rolling back to a binary that owns its full contract.
SELECT 1;
