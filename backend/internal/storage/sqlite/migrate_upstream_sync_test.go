package sqlite

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestUpstreamBaselineMigrationCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name         string
		startVersion int64
	}{
		{name: "fresh database"},
		{name: "upstream v0.11.1 database", startVersion: 36},
		{name: "fork database through 0039", startVersion: 39},
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

			assertTableExists(t, db, "orchestrator_reengagements", true)
			assertTableExists(t, db, "workflow_issue_runs", false)
			assertTableExists(t, db, "worker_idle_outbox", false)

			var current int64
			if err := db.QueryRow(`
SELECT MAX(version_id)
FROM goose_db_version
WHERE is_applied = 1
`).Scan(&current); err != nil {
				t.Fatalf("read current migration version: %v", err)
			}
			if current != 40 {
				t.Fatalf("current migration version = %d, want 40", current)
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
