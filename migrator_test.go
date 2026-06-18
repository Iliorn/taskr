package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"taskr/todo"

	_ "modernc.org/sqlite"
)

// TestSnapshotInMemoryIsNoop confirms snapshotDBBeforeMigrations skips
// cleanly for :memory: databases (path is empty → nothing to back up).
// The migrator path runs against in-memory DBs in many tests; the snapshot
// must not break them.
func TestSnapshotInMemoryIsNoop(t *testing.T) {
	h := openTestDB(t)
	path, err := snapshotDBBeforeMigrations(h, 3)
	if err != nil {
		t.Fatalf("in-memory snapshot returned err: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path for in-memory DB, got %q", path)
	}
}

// TestSnapshotFileBackedProducesOpenableCopy is the real assertion: against
// a file-backed DB, snapshotDBBeforeMigrations writes a .bak file that can
// be reopened as an independent SQLite database with the same data.
func TestSnapshotFileBackedProducesOpenableCopy(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "tasks.db")

	// openStoreAt applies migrations and produces a normal taskr DB.
	h, err := openStoreAt(dbPath)
	if err != nil {
		t.Fatalf("openStoreAt: %v", err)
	}
	t.Cleanup(func() { h.Close() })

	// Seed one row through the normal save path so the backup has
	// recognisable contents.
	saveTodos(t, h, []todo.Todo{todo.New("backup target")})

	backupPath, err := snapshotDBBeforeMigrations(h, 4)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected a non-empty backup path for file-backed DB")
	}

	// Path naming contract: lives next to the source, encodes the
	// from-version, ends with `.bak`.
	matches, err := filepath.Glob(filepath.Join(tmp, "tasks.db-pre-migration-004-*.bak"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 || matches[0] != backupPath {
		t.Errorf("backup at unexpected path: glob=%v, returned=%s", matches, backupPath)
	}

	// Open the backup as a fresh independent SQLite handle and confirm the
	// row is there.
	bk, err := sql.Open("sqlite", backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer bk.Close()
	var count int
	if err := bk.QueryRow(`SELECT COUNT(*) FROM todos`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("backup has %d rows, want 1", count)
	}
}
