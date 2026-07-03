package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"taskr/todo"

	_ "modernc.org/sqlite"
)

// openTestDB returns an isolated in-memory database with the schema applied.
// MaxOpenConns(1) keeps the single shared :memory: connection alive for the
// whole test (each new connection would otherwise get a fresh empty database).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	h, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	h.SetMaxOpenConns(1)
	if err := runMigrations(h); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

// saveTodos upserts todos and tombstones the explicit IDs in tombstones,
// mirroring the differential adapter contract.
func saveTodos(t *testing.T, h *sql.DB, todos []todo.Todo, tombstones ...string) {
	t.Helper()
	ptrs := make([]*todo.Todo, len(todos))
	for i := range todos {
		ptrs[i] = &todos[i]
	}
	if err := saveNormalized(h, ptrs, tombstones); err != nil {
		t.Fatalf("saveNormalized: %v", err)
	}
}

// TestSaveDependentBeforeDependency guards the write-ordering fix: a batch that
// lists a task ahead of the task it depends on must still save, because
// saveNormalizedIn defers foreign-key enforcement to COMMIT. Before the fix,
// the task_dependencies insert for the dependent fired while the dependency's
// todos row didn't yet exist and failed with SQLITE_CONSTRAINT_FOREIGNKEY (787).
// foreign_keys is forced ON here (openTestDB doesn't set it) so the constraint
// is actually enforced — otherwise the test couldn't observe the bug.
func TestSaveDependentBeforeDependency(t *testing.T) {
	h := openTestDB(t)
	if _, err := h.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign_keys: %v", err)
	}

	dependency := todo.New("Update TTRPG ruleset")
	dependent := todo.New("Send the new version")
	dependent.AddDependency(dependency.ID)

	// Dependent first: the hazardous order the map/slice iteration can produce.
	saveTodos(t, h, []todo.Todo{dependent, dependency})

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byID := make(map[string]todo.Todo, len(got))
	for _, g := range got {
		byID[g.ID] = g
	}
	d, ok := byID[dependent.ID]
	if !ok {
		t.Fatal("dependent task not saved")
	}
	if len(d.Dependencies) != 1 || d.Dependencies[0] != dependency.ID {
		t.Fatalf("dependency link not persisted: got %v", d.Dependencies)
	}
}

// TestSQLiteRoundTrip saves todos with nested data and confirms a load
// reconstructs them losslessly from the JSON blob.
func TestSQLiteRoundTrip(t *testing.T) {
	h := openTestDB(t)

	a := todo.New("write tests")
	a.AddTag("work")
	a.AddComment("first pass")
	a.SetPriority(todo.PriorityHigh)
	b := todo.New("ship it")

	saveTodos(t, h, []todo.Todo{a, b})

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d todos, want 2", len(got))
	}

	byID := map[string]todo.Todo{got[0].ID: got[0], got[1].ID: got[1]}
	ra, ok := byID[a.ID]
	if !ok {
		t.Fatalf("todo %s missing after round-trip", a.ID)
	}
	if ra.Title != "Write tests" || ra.Priority != todo.PriorityHigh {
		t.Errorf("scalar fields lost: %+v", ra)
	}
	if len(ra.Tags) != 1 || ra.Tags[0] != "work" {
		t.Errorf("nested tags lost: %v", ra.Tags)
	}
	if len(ra.Comments) != 1 || ra.Comments[0].Text != "first pass" {
		t.Errorf("nested comments lost: %v", ra.Comments)
	}
}

// TestSQLiteColumnsMirrorBlob verifies the queryable columns are populated from
// the todo (not just the JSON blob), so Phase-2 filtering can use SQL.
func TestSQLiteColumnsMirrorBlob(t *testing.T) {
	h := openTestDB(t)

	due := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	x := todo.New("with due")
	x.SetDueDate(due)
	x.SetProject("alpha")
	saveTodos(t, h, []todo.Todo{x})

	var project, dueCol string
	var status, priority int
	err := h.QueryRow(
		`SELECT project, due_date, status, priority FROM todos WHERE id=?`, x.ID,
	).Scan(&project, &dueCol, &status, &priority)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	if project != "alpha" {
		t.Errorf("project column = %q, want alpha", project)
	}
	if dueCol != due.Format(time.RFC3339) {
		t.Errorf("due_date column = %q, want %s", dueCol, due.Format(time.RFC3339))
	}
	if status != int(todo.Pending) || priority != int(todo.PriorityMedium) {
		t.Errorf("status/priority columns = %d/%d", status, priority)
	}
}

// TestDueDateRoundTripKeepsCalendarDay pins the process zone east of UTC and
// asserts a local-midnight due date still formats to the same calendar day
// after a save+load round-trip: fmtTime stores UTC, so without parseTime
// rehydrating in local time the UTC form renders the previous day.
func TestDueDateRoundTripKeepsCalendarDay(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	oldLocal := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = oldLocal })

	h := openTestDB(t)
	due := time.Date(2030, 5, 20, 0, 0, 0, 0, time.Local)
	x := todo.New("tz round trip")
	x.SetDueDate(due)
	saveTodos(t, h, []todo.Todo{x})

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d todos, want 1", len(got))
	}
	if !got[0].DueDate.Equal(due) {
		t.Errorf("due instant changed: %s vs %s", got[0].DueDate, due)
	}
	if g, w := got[0].DueDate.Format("02-01-06"), due.Format("02-01-06"); g != w {
		t.Errorf("due renders as %s after round-trip, want %s", g, w)
	}
}

// TestSQLiteSizeRoundTrip confirms the size column populated by migration 003
// round-trips: a Small task saves as 1 and loads back as todo.SizeSmall, and a
// task with no explicit size loads as the zero value (SizeMedium) so existing
// rows are unaffected.
func TestSQLiteSizeRoundTrip(t *testing.T) {
	h := openTestDB(t)

	small := todo.New("quick win")
	small.SetSize(todo.SizeSmall)
	plain := todo.New("default sized")

	saveTodos(t, h, []todo.Todo{small, plain})

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byID := map[string]todo.Todo{}
	for _, x := range got {
		byID[x.ID] = x
	}
	if byID[small.ID].Size != todo.SizeSmall {
		t.Errorf("Small task loaded as %v, want SizeSmall", byID[small.ID].Size)
	}
	if byID[plain.ID].Size != todo.SizeMedium {
		t.Errorf("default-sized task loaded as %v, want SizeMedium (zero value)", byID[plain.ID].Size)
	}

	var sizeCol int
	if err := h.QueryRow(`SELECT size FROM todos WHERE id=?`, small.ID).Scan(&sizeCol); err != nil {
		t.Fatalf("query size: %v", err)
	}
	if sizeCol != int(todo.SizeSmall) {
		t.Errorf("size column = %d, want %d", sizeCol, int(todo.SizeSmall))
	}
}

// TestSQLiteTombstone confirms a task dropped from the saved set is soft-deleted
// (kept with deleted=1 for sync) rather than removed, and no longer loads.
func TestSQLiteTombstone(t *testing.T) {
	h := openTestDB(t)

	a := todo.New("keep")
	b := todo.New("delete me")
	saveTodos(t, h, []todo.Todo{a, b})

	// Differential save: explicitly tombstone b.
	saveTodos(t, h, []todo.Todo{a}, b.ID)

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("live load = %+v, want only %s", got, a.ID)
	}

	// b must still exist as a tombstone with a deleted_at stamp.
	var deleted int
	var deletedAt string
	if err := h.QueryRow(
		`SELECT deleted, deleted_at FROM todos WHERE id=?`, b.ID,
	).Scan(&deleted, &deletedAt); err != nil {
		t.Fatalf("tombstone row gone: %v", err)
	}
	if deleted != 1 || deletedAt == "" {
		t.Errorf("b not tombstoned: deleted=%d deleted_at=%q", deleted, deletedAt)
	}
}

// TestSQLiteTombstoneRevive confirms re-saving a previously tombstoned id clears
// the tombstone (deleted back to 0), so the row participates in load again.
func TestSQLiteTombstoneRevive(t *testing.T) {
	h := openTestDB(t)

	a := todo.New("on and off")
	saveTodos(t, h, []todo.Todo{a})
	saveTodos(t, h, nil, a.ID)      // tombstone
	saveTodos(t, h, []todo.Todo{a}) // revive (upsert clears deleted)

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("revived load = %+v, want %s", got, a.ID)
	}
	var deleted int
	if err := h.QueryRow(`SELECT deleted FROM todos WHERE id=?`, a.ID).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("revived row still tombstoned (deleted=%d)", deleted)
	}
}

// TestImportFromJSONSeedsFreshDB confirms the legacy-JSON import populates a
// fresh database, then loads back through the SQLite path.
func TestImportFromJSONSeedsFreshDB(t *testing.T) {
	// Point storage at a temp HOME so loadTodosJSON reads our fixture.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	seed := []todo.Todo{todo.New("imported one"), todo.New("imported two")}
	if err := os.MkdirAll(filepath.Dir(getStoragePath()), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := marshalTodos(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(getStoragePath(), data, 0644); err != nil {
		t.Fatal(err)
	}

	h := openTestDB(t)
	if err := importFromJSON(h); err != nil {
		t.Fatalf("importFromJSON: %v", err)
	}

	got, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("imported %d todos, want 2", len(got))
	}
}

// TestFileBackedRoundTrip exercises the real on-disk path (a temp file, WAL,
// schema) and confirms data — including a tombstone — survives reopening with a
// fresh connection. Uses openStoreAt directly to avoid the package-level
// singleton.
func TestFileBackedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")

	h1, err := openStoreAt(path)
	if err != nil {
		t.Fatalf("openStoreAt: %v", err)
	}
	a := todo.New("persist me")
	b := todo.New("delete me")
	saveTodos(t, h1, []todo.Todo{a, b})
	saveTodos(t, h1, []todo.Todo{a}, b.ID) // tombstone b
	h1.Close()

	// Reopen: the on-disk data must come back, and b must stay tombstoned.
	h2, err := openStoreAt(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer h2.Close()
	got, err := loadTodosFromDB(h2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("reopened load = %+v, want only %s", got, a.ID)
	}
}

// TestImportGatedOnFreshDB is the zombie-prevention guard: legacy JSON is
// imported only when the database is created. After importing, deleting every
// task (tombstoning all rows) and reopening must NOT re-import the still-present
// JSON, or deleted tasks would rise from the dead.
func TestImportGatedOnFreshDB(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := ensureStorageDir(); err != nil {
		t.Fatal(err)
	}
	data, err := marshalTodos([]todo.Todo{todo.New("one"), todo.New("two")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(getStoragePath(), data, 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tasks.db")

	// Fresh DB imports the two legacy tasks.
	h1, err := openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loadTodosFromDB(h1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("fresh import got %d, want 2", len(got))
	}
	allIDs := make([]string, 0, len(got))
	for _, td := range got {
		allIDs = append(allIDs, td.ID)
	}
	saveTodos(t, h1, nil, allIDs...) // delete everything → all tombstoned
	h1.Close()

	// Reopen the existing DB: must not re-import despite tasks.json still there.
	h2, err := openStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()
	got, err = loadTodosFromDB(h2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("reopen re-imported deleted tasks (got %d, want 0) — zombie bug", len(got))
	}
}

// TestPruneOldTombstones: task and child tombstones older than the retention
// window are hard-deleted (row plus every child row); fresh tombstones and
// live data are untouched.
func TestPruneOldTombstones(t *testing.T) {
	h := openTestDB(t)
	now := time.Now()
	ancient := now.Add(-tombstoneRetention - 24*time.Hour)

	oldDead := todo.New("pruned long ago")
	oldDead.AddTag("x")
	oldDead.AddComment("gone with me")
	oldDead.Deleted = true
	oldDead.DeletedAt = ancient

	freshDead := todo.New("deleted yesterday")
	freshDead.Deleted = true
	freshDead.DeletedAt = now.Add(-24 * time.Hour)

	live := todo.New("alive")
	live.Comments = []todo.Comment{
		{ID: "c-old", Text: "old deleted comment", CreatedAt: ancient, DeletedAt: ancient},
		{ID: "c-fresh", Text: "freshly deleted", CreatedAt: now, DeletedAt: now.Add(-time.Hour)},
		{ID: "c-live", Text: "still here", CreatedAt: now},
	}

	saveTodos(t, h, []todo.Todo{oldDead, freshDead, live})
	if err := pruneOldTombstones(h, now); err != nil {
		t.Fatalf("prune: %v", err)
	}

	got, err := loadTodosForSync(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byID := make(map[string]todo.Todo, len(got))
	for _, td := range got {
		byID[td.ID] = td
	}
	if _, ok := byID[oldDead.ID]; ok {
		t.Errorf("tombstone older than retention should be pruned")
	}
	if _, ok := byID[freshDead.ID]; !ok {
		t.Errorf("fresh tombstone must be retained so the deletion keeps propagating")
	}
	gotLive, ok := byID[live.ID]
	if !ok {
		t.Fatalf("live task must survive pruning")
	}
	commentIDs := make(map[string]bool, len(gotLive.Comments))
	for _, c := range gotLive.Comments {
		commentIDs[c.ID] = true
	}
	if commentIDs["c-old"] {
		t.Errorf("old child tombstone should be pruned")
	}
	if !commentIDs["c-fresh"] || !commentIDs["c-live"] {
		t.Errorf("fresh child tombstone and live comment must survive; got %v", commentIDs)
	}

	// The pruned task's child rows must go with it — no orphans.
	for _, table := range []string{"task_tags", "task_comments"} {
		var n int
		if err := h.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE task_id = ?`, oldDead.ID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still holds %d row(s) for the pruned task", table, n)
		}
	}
}
