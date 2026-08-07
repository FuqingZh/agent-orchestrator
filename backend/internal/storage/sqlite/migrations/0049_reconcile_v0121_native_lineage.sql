-- +goose Up
-- +goose StatementBegin
-- Native v0.12.1 databases already ran the equivalent review, pinning, batch,
-- and model-catalog migrations under different version numbers. The startup
-- lineage bridge marks those remapped versions applied; this migration keeps
-- the final fork state explicit and idempotent for both lineages.
UPDATE review_run SET batch_id = id WHERE batch_id = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_review_run_session_pr_sha_harness
    ON review_run (session_id, pr_url, target_sha, harness)
    WHERE target_sha != ''
        AND status NOT IN ('failed', 'cancelled')
        AND (status = 'running' OR verdict NOT IN ('', 'changes_requested'));

CREATE TABLE IF NOT EXISTS agent_model_catalog (
    agent_id TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    binary_version TEXT NOT NULL DEFAULT '',
    catalog_json TEXT NOT NULL,
    source TEXT NOT NULL,
    fetched_at TIMESTAMP NOT NULL,
    PRIMARY KEY (agent_id, project_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
