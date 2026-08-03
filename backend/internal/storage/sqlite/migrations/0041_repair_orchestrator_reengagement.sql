-- +goose Up
-- +goose StatementBegin
-- Compatibility repair for deployed calibration builds whose migration 0040
-- added sessions.launch_permissions. Those databases record version 40 as
-- applied, so Goose correctly skips 0040_orchestrator_reengagement.sql even
-- though the orchestrator_reengagements table is absent.
CREATE TABLE IF NOT EXISTS orchestrator_reengagements (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMP NOT NULL,
    last_attempt_at TIMESTAMP,
    progress_since_attempt BOOLEAN NOT NULL DEFAULT 0,
    attention_notified BOOLEAN NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'active'
        CHECK (state IN ('active', 'completed', 'exhausted')),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_orchestrator_reengagements_due
    ON orchestrator_reengagements(state, next_attempt_at)
    WHERE state = 'active';

CREATE INDEX IF NOT EXISTS idx_orchestrator_reengagements_attention
    ON orchestrator_reengagements(state, attention_notified)
    WHERE state = 'exhausted' AND attention_notified = 0;
-- +goose StatementEnd

-- +goose Down
-- Intentionally preserve the table: logical ownership remains with migration
-- 0040 on fresh databases, and a down migration cannot distinguish that case
-- from a repaired deployed database.
SELECT 1;
