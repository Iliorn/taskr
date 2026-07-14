package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

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

// TestPrunePreMigrationBackups pins the retention rule: newest `keep` stay,
// older go, unrelated files are untouched.
func TestPrunePreMigrationBackups(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tasks.db")
	names := []string{
		"tasks.db-pre-migration-005-20260624-162950.bak",
		"tasks.db-pre-migration-006-20260625-065315.bak",
		"tasks.db-pre-migration-007-20260702-085628.bak",
		"tasks.db-pre-migration-008-20260703-191525.bak",
		"tasks.db-pre-migration-009-20260712-110537.bak",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := filepath.Join(dir, "tasks.db-wal")
	if err := os.WriteFile(unrelated, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	prunePreMigrationBackups(dbPath, 3)

	for i, n := range names {
		_, err := os.Stat(filepath.Join(dir, n))
		if i < 2 && !os.IsNotExist(err) {
			t.Errorf("%s should have been pruned", n)
		}
		if i >= 2 && err != nil {
			t.Errorf("%s should survive: %v", n, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated sidecar removed: %v", err)
	}
}

func TestNormalizeStoredTagsMigration(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("legacy spaced tags")
	task.ModifiedAt = time.Now().Add(-time.Hour)
	before := task.ModifiedAt
	// Bypass AddTag to model rows written by an older taskr version.
	task.Tags = []string{"Deep Work", "deep-work", "Personal   Admin"}
	saveTodos(t, h, []todo.Todo{task})

	tx, err := h.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := normalizeStoredTags(tx); err != nil {
		tx.Rollback()
		t.Fatalf("normalizeStoredTags: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load normalized tags: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d tasks, want 1", len(got))
	}
	sort.Strings(got[0].Tags)
	want := []string{"deep-work", "personal-admin"}
	if len(got[0].Tags) != len(want) || got[0].Tags[0] != want[0] || got[0].Tags[1] != want[1] {
		t.Errorf("normalized tags = %v, want %v", got[0].Tags, want)
	}
	if !got[0].ModifiedAt.After(before) {
		t.Errorf("migration ModifiedAt = %v, want after %v so sync propagates it", got[0].ModifiedAt, before)
	}
}
