package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestV0121MigrationCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name         string
		startVersion int64
		seed         func(*testing.T, *sql.DB)
	}{
		{name: "fresh database"},
		{name: "upstream v0.11.2 database", seed: seedNativeV0112Database},
		{name: "native upstream v0.12.1 database", seed: seedNativeV0121Database},
		{name: "fork database through 0039", startVersion: 39},
		{name: "fork database through 0041", startVersion: 41},
		{name: "fork database through 0044", startVersion: 44},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			if tc.seed != nil {
				tc.seed(t, db)
			} else if tc.startVersion > 0 {
				upTo(t, db, tc.startVersion)
			}
			if err := migrate(db); err != nil {
				t.Fatalf("migrate from version %d: %v", tc.startVersion, err)
			}

			assertTableExists(t, db, "orchestrator_reengagements", false)
			assertTableExists(t, db, "workflow_issue_runs", false)
			assertTableExists(t, db, "worker_idle_events", false)
			assertColumnExists(t, db, "sessions", "diff_base_sha", true)
			assertColumnExists(t, db, "sessions", "diff_base_ref", true)
			assertColumnExists(t, db, "sessions", "reviewer_harness", true)
			assertColumnExists(t, db, "sessions", "is_pinned", true)
			assertColumnExists(t, db, "sessions", "pinned_at", true)
			assertColumnExists(t, db, "notifications", "resolved_at", true)
			assertTableExists(t, db, "agent_model_catalog", true)
			assertSchemaObjectExists(t, db, "index", "idx_review_run_session_pr_sha_harness", true)
			assertSchemaObjectExists(t, db, "trigger", "sessions_cdc_update", true)

			schemaBefore := sqliteSchemaSnapshot(t, db)

			var current int64
			if err := db.QueryRow(`
SELECT MAX(version_id)
FROM goose_db_version
WHERE is_applied = 1
`).Scan(&current); err != nil {
				t.Fatalf("read current migration version: %v", err)
			}
			if current != 82 {
				t.Fatalf("current migration version = %d, want 82", current)
			}

			var appliedBefore int
			if err := db.QueryRow(`
SELECT COUNT(*)
FROM goose_db_version
WHERE is_applied = 1
`).Scan(&appliedBefore); err != nil {
				t.Fatalf("count applied migrations: %v", err)
			}
			if err := migrate(db); err != nil {
				t.Fatalf("repeat migrate: %v", err)
			}
			var appliedAfter int
			if err := db.QueryRow(`
SELECT COUNT(*)
FROM goose_db_version
WHERE is_applied = 1
`).Scan(&appliedAfter); err != nil {
				t.Fatalf("count migrations after repeat: %v", err)
			}
			if appliedAfter != appliedBefore {
				t.Fatalf("repeat migrate changed applied count: before=%d after=%d", appliedBefore, appliedAfter)
			}
			if schemaAfter := sqliteSchemaSnapshot(t, db); schemaAfter != schemaBefore {
				t.Fatalf("repeat migrate changed schema:\nbefore:\n%s\nafter:\n%s", schemaBefore, schemaAfter)
			}
		})
	}
}

func TestMigrationRepairsDeployedCalibrationVersion40Collision(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	upTo(t, db, 36)
	if _, err := db.Exec(`
DROP TABLE worker_idle_events;
ALTER TABLE sessions ADD COLUMN launch_permissions TEXT NOT NULL DEFAULT ''
    CHECK (launch_permissions IN ('', 'default', 'accept-edits', 'auto', 'bypass-permissions'));
INSERT INTO goose_db_version (version_id, is_applied) VALUES
    (37, 1), (38, 1), (39, 1), (40, 1);
`); err != nil {
		t.Fatalf("seed deployed calibration version 40 history: %v", err)
	}

	assertTableExists(t, db, "orchestrator_reengagements", false)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate deployed calibration version 40 database: %v", err)
	}
	assertTableExists(t, db, "orchestrator_reengagements", false)
	assertTableExists(t, db, "worker_idle_events", false)
	assertColumnExists(t, db, "sessions", "diff_base_sha", true)
	assertColumnExists(t, db, "sessions", "diff_base_ref", true)
	assertColumnExists(t, db, "notifications", "resolved_at", true)

	var launchPermissionsColumns int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM pragma_table_info('sessions')
WHERE name = 'launch_permissions'
`).Scan(&launchPermissionsColumns); err != nil {
		t.Fatalf("inspect launch_permissions column: %v", err)
	}
	if launchPermissionsColumns != 1 {
		t.Fatalf("launch_permissions columns = %d, want 1", launchPermissionsColumns)
	}

	var current int64
	if err := db.QueryRow(`
SELECT MAX(version_id)
FROM goose_db_version
WHERE is_applied = 1
`).Scan(&current); err != nil {
		t.Fatalf("read repaired migration version: %v", err)
	}
	if current != 82 {
		t.Fatalf("repaired migration version = %d, want 82", current)
	}
}

func TestCurrentForkMigrationPreservesRepresentativeData(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedRepresentativeRows(t, db, "current")

	schemaBefore := sqliteSchemaSnapshot(t, db)
	dataBefore := representativeDataSnapshot(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("repeat migrate: %v", err)
	}
	if schemaAfter := sqliteSchemaSnapshot(t, db); schemaAfter != schemaBefore {
		t.Fatalf("repeat migrate changed schema:\nbefore:\n%s\nafter:\n%s", schemaBefore, schemaAfter)
	}
	if dataAfter := representativeDataSnapshot(t, db); dataAfter != dataBefore {
		t.Fatalf("repeat migrate changed representative data: before=%q after=%q", dataBefore, dataAfter)
	}
}

func TestMappedUpstreamMigrationsKeepRecordedContent(t *testing.T) {
	for _, tc := range []struct {
		path string
		want string
	}{
		{
			path: "migrations/0038_drop_worker_idle_outbox.sql",
			want: "abf41789032c9a9bc25e21364d07d3c19dc3ad5e76a6e327e195a87f32947342",
		},
		{
			path: "migrations/0040_orchestrator_reengagement.sql",
			want: "f2b14364b6abad489e941dac5c9e40e2678c8f5054ddfcd734ff7d09ad6db3ca",
		},
		{
			path: "migrations/0042_drop_orchestrator_reengagement.sql",
			want: "01b2baa49b6fcc0c461f05e8b8bcf07a7f971ff8fcaee80425b53d0c8b752cf4",
		},
		{
			path: "migrations/0043_add_session_diff_base.sql",
			want: "1b1001d774bcb30aec24de8803bac0090b12c1fa3252d8f9b45ed74e0f9596f9",
		},
		{
			path: "migrations/0044_notification_resolution.sql",
			want: "4aed8877163cd39674716564262376449a3a61a5959468e6c33d2b308c34e112",
		},
		{
			path: "migrations/0045_review_run_unique_per_harness.sql",
			want: "679218370fde19c9f534395037055f74acd84877b119bc4d9d9c46471a405074",
		},
		{
			path: "migrations/0046_add_session_pinned.sql",
			want: "bfb32829e648bf051051d88b85fd8c459b95cd87b485d2b7f211043936427a68",
		},
		{
			path: "migrations/0047_backfill_review_run_batch_id.sql",
			want: "e8263c0522e9c1946fc550cc0cc4223abf87268882ee512d06b18c9c80361598",
		},
		{
			path: "migrations/0048_agent_model_catalog.sql",
			want: "cafb59d968d0941e04219dfabefc7e8440cbfb51ef4695be9904361e2bb9c4da",
		},
		{
			path: "migrations/0050_review_agent_session_id.sql",
			want: "440eac3ebdd773e4fa70824d5137ac3b1704d706618d7d947da88407c6d23a7e",
		},
		{
			path: "migrations/0051_review_per_harness.sql",
			want: "720fb0658b813368d3ae9aab914634c85638582207f6f506fa9139a21527ff1a",
		},
	} {
		t.Run(tc.path, func(t *testing.T) {
			contents, err := migrationsFS.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read mapped migration: %v", err)
			}
			got := fmt.Sprintf("%x", sha256.Sum256(contents))
			if got != tc.want {
				t.Fatalf("SHA-256 = %s, want %s", got, tc.want)
			}
		})
	}
}

func seedNativeV0112Database(t *testing.T, db *sql.DB) {
	t.Helper()
	upTo(t, db, 36)
	seedRepresentativeRows(t, db, "v0112")
	if _, err := db.Exec(`
DROP INDEX IF EXISTS idx_worker_idle_pending_project;
DROP INDEX IF EXISTS idx_worker_idle_pending_worker;
DROP TABLE IF EXISTS worker_idle_events;
CREATE TABLE orchestrator_reengagements (
    session_id TEXT PRIMARY KEY REFERENCES sessions (id) ON DELETE CASCADE,
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
CREATE INDEX idx_orchestrator_reengagements_due
    ON orchestrator_reengagements(state, next_attempt_at)
    WHERE state = 'active';
CREATE INDEX idx_orchestrator_reengagements_attention
    ON orchestrator_reengagements(state, attention_notified)
    WHERE state = 'exhausted' AND attention_notified = 0;
INSERT INTO goose_db_version (version_id, is_applied) VALUES (37, 1), (38, 1);
`); err != nil {
		t.Fatalf("seed native v0.11.2 database: %v", err)
	}
}

func seedNativeV0121Database(t *testing.T, db *sql.DB) {
	t.Helper()
	upTo(t, db, 36)
	seedRepresentativeRows(t, db, "native")
	if _, err := db.Exec(`
DROP INDEX IF EXISTS idx_worker_idle_pending_project;
DROP INDEX IF EXISTS idx_worker_idle_pending_worker;
DROP TABLE IF EXISTS worker_idle_events;

CREATE TABLE orchestrator_reengagements (
    session_id TEXT PRIMARY KEY REFERENCES sessions (id) ON DELETE CASCADE,
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
CREATE INDEX idx_orchestrator_reengagements_due
    ON orchestrator_reengagements(state, next_attempt_at)
    WHERE state = 'active';
CREATE INDEX idx_orchestrator_reengagements_attention
    ON orchestrator_reengagements(state, attention_notified)
    WHERE state = 'exhausted' AND attention_notified = 0;
DROP INDEX IF EXISTS idx_orchestrator_reengagements_due;
DROP INDEX IF EXISTS idx_orchestrator_reengagements_attention;
DROP TABLE orchestrator_reengagements;

ALTER TABLE sessions ADD COLUMN diff_base_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN diff_base_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN resolved_at TIMESTAMP;
UPDATE notifications SET resolved_at = created_at WHERE status = 'read';
DROP INDEX IF EXISTS idx_notifications_unread_dedupe;
CREATE UNIQUE INDEX idx_notifications_open_dedupe
    ON notifications(session_id, type, pr_url)
    WHERE status = 'unread' OR resolved_at IS NULL;
CREATE INDEX idx_notifications_unresolved
    ON notifications(resolved_at, created_at DESC, id DESC);

ALTER TABLE sessions ADD COLUMN reviewer_harness TEXT NOT NULL DEFAULT '';
DROP INDEX idx_review_run_session_pr_sha;
CREATE UNIQUE INDEX idx_review_run_session_pr_sha_harness
    ON review_run (session_id, pr_url, target_sha, harness)
    WHERE target_sha != ''
        AND status NOT IN ('failed', 'cancelled')
        AND (status = 'running' OR verdict NOT IN ('', 'changes_requested'));

ALTER TABLE sessions ADD COLUMN is_pinned BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN pinned_at DATETIME;
DROP TRIGGER IF EXISTS sessions_cdc_update;
CREATE TRIGGER sessions_cdc_update
AFTER UPDATE ON sessions
WHEN OLD.activity_state <> NEW.activity_state
    OR OLD.is_terminated <> NEW.is_terminated
    OR (OLD.first_signal_at IS NULL AND NEW.first_signal_at IS NOT NULL)
    OR OLD.preview_url <> NEW.preview_url
    OR OLD.preview_revision <> NEW.preview_revision
    OR OLD.display_name <> NEW.display_name
    OR OLD.terminate_on_pr_merge <> NEW.terminate_on_pr_merge
    OR OLD.is_pinned <> NEW.is_pinned
    OR OLD.pinned_at <> NEW.pinned_at
    OR (OLD.pinned_at IS NULL AND NEW.pinned_at IS NOT NULL)
    OR (OLD.pinned_at IS NOT NULL AND NEW.pinned_at IS NULL)
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_updated',
        json_object(
            'id', NEW.id,
            'activity', NEW.activity_state,
            'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END),
            'terminateOnPrMerge', json(CASE WHEN NEW.terminate_on_pr_merge THEN 'true' ELSE 'false' END),
            'previewUrl', NEW.preview_url,
            'previewRevision', NEW.preview_revision,
            'isPinned', json(CASE WHEN NEW.is_pinned THEN 'true' ELSE 'false' END)
        ),
        NEW.updated_at);
END;

UPDATE review_run SET batch_id = id WHERE batch_id = '';
CREATE TABLE agent_model_catalog (
    agent_id TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    binary_version TEXT NOT NULL DEFAULT '',
    catalog_json TEXT NOT NULL,
    source TEXT NOT NULL,
    fetched_at TIMESTAMP NOT NULL,
    PRIMARY KEY (agent_id, project_id)
);
INSERT INTO goose_db_version (version_id, is_applied) VALUES
    (37, 1), (38, 1), (39, 1), (40, 1), (41, 1),
    (42, 1), (43, 1), (44, 1), (47, 1);
`); err != nil {
		t.Fatalf("seed native v0.12.1 database: %v", err)
	}
	var batchID string
	if err := db.QueryRow(`SELECT batch_id FROM review_run WHERE id = 'native-run'`).Scan(&batchID); err != nil {
		t.Fatalf("read native backfill seed: %v", err)
	}
	if batchID != "native-run" {
		t.Fatalf("native batch_id = %q, want native-run", batchID)
	}
	var resolved int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE id = 'native-notification' AND resolved_at IS NOT NULL`).Scan(&resolved); err != nil {
		t.Fatalf("read native notification seed: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("native notification resolved rows = %d, want 1", resolved)
	}
}

func seedRepresentativeRows(t *testing.T, db *sql.DB, suffix string) {
	t.Helper()
	projectID := "project-" + suffix
	sessionID := "session-" + suffix
	reviewID := "review-" + suffix
	runID := "native-run"
	notificationID := "native-notification"
	if suffix == "current" {
		runID = "current-run"
		notificationID = "current-notification"
	}
	if _, err := db.Exec(`
INSERT INTO projects (id, path, repo_origin_url, display_name, registered_at, config, kind)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, projectID, "/tmp/"+projectID, "https://example.com/"+projectID, projectID, "2026-08-06T00:00:00Z", `{}`, "single_repo"); err != nil {
		t.Fatalf("seed project %s: %v", suffix, err)
	}
	if _, err := db.Exec(`
INSERT INTO sessions (id, project_id, num, activity_last_at, created_at, updated_at)
VALUES (?, ?, 1, ?, ?, ?)
`, sessionID, projectID, "2026-08-06T00:00:00Z", "2026-08-06T00:00:00Z", "2026-08-06T00:00:00Z"); err != nil {
		t.Fatalf("seed session %s: %v", suffix, err)
	}
	if _, err := db.Exec(`
INSERT INTO review (id, session_id, project_id, harness, created_at, updated_at)
VALUES (?, ?, ?, 'claude-code', ?, ?)
`, reviewID, sessionID, projectID, "2026-08-06T00:00:00Z", "2026-08-06T00:00:00Z"); err != nil {
		t.Fatalf("seed review %s: %v", suffix, err)
	}
	if _, err := db.Exec(`
INSERT INTO review_run (id, review_id, session_id, batch_id, harness, pr_url, target_sha, status, verdict, body, github_review_id, created_at)
VALUES (?, ?, ?, '', 'claude-code', 'https://example.com/pr/1', 'sha-native', 'running', '', '', '', ?)
`, runID, reviewID, sessionID, "2026-08-06T00:00:00Z"); err != nil {
		t.Fatalf("seed review run %s: %v", suffix, err)
	}
	if _, err := db.Exec(`
INSERT INTO notifications (id, session_id, project_id, pr_url, type, title, body, status, created_at)
VALUES (?, ?, ?, 'https://example.com/pr/1', 'needs_input', 'title', 'body', 'read', ?)
`, notificationID, sessionID, projectID, "2026-08-06T00:00:00Z"); err != nil {
		t.Fatalf("seed notification %s: %v", suffix, err)
	}
	if suffix == "current" {
		if _, err := db.Exec(`
INSERT INTO agent_model_catalog (agent_id, project_id, binary_version, catalog_json, source, fetched_at)
VALUES ('codex', ?, '1.0.0', '{"models":["gpt-5"]}', 'test', ?)
`, projectID, "2026-08-06T00:00:00Z"); err != nil {
			t.Fatalf("seed model catalog: %v", err)
		}
	}
}

func sqliteSchemaSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`
SELECT type, name, COALESCE(sql, '')
FROM sqlite_master
WHERE type IN ('table', 'index', 'trigger')
ORDER BY type, name
`)
	if err != nil {
		t.Fatalf("read sqlite schema: %v", err)
	}
	defer rows.Close()
	var snapshot strings.Builder
	for rows.Next() {
		var kind, name, sqlText string
		if err := rows.Scan(&kind, &name, &sqlText); err != nil {
			t.Fatalf("scan sqlite schema: %v", err)
		}
		fmt.Fprintf(&snapshot, "%s|%s|%s\n", kind, name, sqlText)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read sqlite schema rows: %v", err)
	}
	return snapshot.String()
}

func representativeDataSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var reviewRun, notification, session, catalog string
	if err := db.QueryRow(`SELECT id || ':' || COALESCE(batch_id, '') FROM review_run ORDER BY id LIMIT 1`).Scan(&reviewRun); err != nil {
		t.Fatalf("read review data: %v", err)
	}
	if err := db.QueryRow(`SELECT id || ':' || COALESCE(CAST(resolved_at AS TEXT), '') FROM notifications ORDER BY id LIMIT 1`).Scan(&notification); err != nil {
		t.Fatalf("read notification data: %v", err)
	}
	if err := db.QueryRow(`SELECT id || ':' || CAST(is_pinned AS TEXT) || ':' || COALESCE(reviewer_harness, '') FROM sessions ORDER BY id LIMIT 1`).Scan(&session); err != nil {
		t.Fatalf("read session data: %v", err)
	}
	if err := db.QueryRow(`SELECT agent_id || ':' || project_id || ':' || catalog_json FROM agent_model_catalog ORDER BY agent_id, project_id LIMIT 1`).Scan(&catalog); err != nil {
		t.Fatalf("read model catalog data: %v", err)
	}
	return strings.Join([]string{reviewRun, notification, session, catalog}, "|")
}

func assertTableExists(t *testing.T, db *sql.DB, name string, want bool) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name,
	).Scan(&count); err != nil {
		t.Fatalf("inspect table %s: %v", name, err)
	}
	if got := count == 1; got != want {
		t.Fatalf("table %s exists = %v, want %v", name, got, want)
	}
}

func assertColumnExists(t *testing.T, db *sql.DB, table, column string, want bool) {
	t.Helper()
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info(%q) WHERE name = ?", table)
	if err := db.QueryRow(query, column).Scan(&count); err != nil {
		t.Fatalf("inspect column %s.%s: %v", table, column, err)
	}
	if got := count == 1; got != want {
		t.Fatalf("column %s.%s exists = %v, want %v", table, column, got, want)
	}
}

func assertSchemaObjectExists(t *testing.T, db *sql.DB, kind, name string, want bool) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, kind, name,
	).Scan(&count); err != nil {
		t.Fatalf("inspect %s %s: %v", kind, name, err)
	}
	if got := count == 1; got != want {
		t.Fatalf("%s %s exists = %v, want %v", kind, name, got, want)
	}
}
