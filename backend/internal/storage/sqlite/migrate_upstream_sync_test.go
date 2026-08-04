package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestCurrentUpstreamMainMigrationCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name         string
		startVersion int64
	}{
		{name: "fresh database"},
		{name: "upstream v0.11.1 database", startVersion: 36},
		{name: "fork database through 0039", startVersion: 39},
		{name: "fork database through 0041", startVersion: 41},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ao.db")+pragmas)
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })

			if tc.startVersion > 0 {
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
			assertColumnExists(t, db, "notifications", "resolved_at", true)

			var current int64
			if err := db.QueryRow(`
SELECT MAX(version_id)
FROM goose_db_version
WHERE is_applied = 1
`).Scan(&current); err != nil {
				t.Fatalf("read current migration version: %v", err)
			}
			if current != 44 {
				t.Fatalf("current migration version = %d, want 44", current)
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
	if current != 44 {
		t.Fatalf("repaired migration version = %d, want 44", current)
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
