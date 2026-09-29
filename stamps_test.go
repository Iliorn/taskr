package main

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/rank"
	"github.com/Iliorn/tjek/todo"
)

// stampSave saves tasks the way the app does, stamps and all, at now.
func stampSave(t *testing.T, h *sql.DB, now time.Time, tasks []todo.Todo, tombstones ...string) {
	t.Helper()
	ptrs := make([]*todo.Todo, len(tasks))
	for i := range tasks {
		ptrs[i] = &tasks[i]
	}
	var dead map[string]time.Time
	if len(tombstones) > 0 {
		dead = map[string]time.Time{}
		for _, id := range tombstones {
			dead[id] = now
		}
	}
	if err := saveStamped(h, ptrs, dead, rank.Default().Score, now); err != nil {
		t.Fatalf("saveStamped: %v", err)
	}
}

func storedStamps(t *testing.T, h *sql.DB, id string) map[string]hlc.Stamp {
	t.Helper()
	all, err := loadTodosForSync(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, x := range all {
		if x.ID == id {
			return x.Stamps
		}
	}
	t.Fatalf("task %s missing", id)
	return nil
}

var s0 = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// A new task has every unit stamped, all with the one stamp of its save.
func TestSaveStampsEveryUnitOfANewTask(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Pay rent")
	task.Tags = []string{"home"}
	stampSave(t, h, s0, []todo.Todo{task})

	st := storedStamps(t, h, task.ID)
	first := st["title"]
	if first == "" {
		t.Fatal("a new task was saved without stamps")
	}
	for _, f := range todo.Fields {
		if st[f.Key] != first {
			t.Errorf("unit %s stamped %q, want the save's %q", f.Key, st[f.Key], first)
		}
	}
	if st[todo.TagKey("home")] != first {
		t.Errorf("tag stamped %q, want %q", st[todo.TagKey("home")], first)
	}
}

// An edit stamps only what it changed: the other units keep their stamps.
func TestSaveStampsOnlyTheChangedUnits(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Pay rent")
	task.Tags = []string{"home", "money"}
	stampSave(t, h, s0, []todo.Todo{task})
	before := storedStamps(t, h, task.ID)

	task.Priority = todo.PriorityHigh
	task.Tags = []string{"home", "urgent"}
	stampSave(t, h, s0.Add(time.Minute), []todo.Todo{task})
	after := storedStamps(t, h, task.ID)

	for _, k := range []string{"priority", todo.TagKey("money"), todo.TagKey("urgent")} {
		if !after[k].After(before["title"]) {
			t.Errorf("%s stamped %q, want a stamp after the first save", k, after[k])
		}
	}
	for _, k := range []string{"title", "due", "done", todo.TagKey("home")} {
		if after[k] != before[k] {
			t.Errorf("%s restamped %q → %q though it did not change", k, before[k], after[k])
		}
	}
	if _, kept := after[todo.TagKey("money")]; !kept {
		t.Error("a removed tag lost its stamp, so an older add elsewhere would bring it back")
	}
}

// A deletion stamps the "deleted" unit and leaves the rest; restoring the
// task stamps it again, later, so the restore outranks the deletion.
func TestSaveStampsADeletionAndItsUndo(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Pay rent")
	stampSave(t, h, s0, []todo.Todo{task})
	before := storedStamps(t, h, task.ID)

	stampSave(t, h, s0.Add(time.Minute), nil, task.ID)
	deleted := storedStamps(t, h, task.ID)
	if !deleted["deleted"].After(before["deleted"]) || deleted["title"] != before["title"] {
		t.Errorf("deletion stamps %v; want only deleted restamped", deleted)
	}

	// Undo a second later on a clock an hour slow: still later.
	stampSave(t, h, s0.Add(-time.Hour), []todo.Todo{task})
	restored := storedStamps(t, h, task.ID)
	if !restored["deleted"].After(deleted["deleted"]) {
		t.Errorf("restore stamped %q, not after the deletion's %q", restored["deleted"], deleted["deleted"])
	}
}

// A task stored before stamps existed keeps, for each unit an edit leaves
// alone, the stamp its modification time gives it, written out so a later
// save cannot read a newer one off the edit's own modification time.
func TestSaveMaterializesTheStampsOfATaskFromBeforeStamps(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Pay rent")
	task.ModifiedAt = s0.Add(-24 * time.Hour)
	saveTodos(t, h, []todo.Todo{task}) // the writer without stamps

	task.Notes = "landlord changed account"
	task.ModifiedAt = s0
	stampSave(t, h, s0, []todo.Todo{task})
	st := storedStamps(t, h, task.ID)
	if want := hlc.At(s0.Add(-24 * time.Hour)); st["title"] != want {
		t.Errorf("title stamp %q, want the old modification time's %q", st["title"], want)
	}
	if !st["notes"].After(st["title"]) {
		t.Errorf("notes stamp %q, want the edit's, after %q", st["notes"], st["title"])
	}
}

// The clock lives in the store: one node for the device, and each save's
// stamp after the last, across saves and a clock that goes back.
func TestSaveKeepsOneClockAcrossSaves(t *testing.T) {
	h := openTestDB(t)
	a, b := todo.New("a"), todo.New("b")
	stampSave(t, h, s0, []todo.Todo{a})
	stampSave(t, h, s0.Add(-time.Hour), []todo.Todo{b})
	sa, sb := storedStamps(t, h, a.ID)["title"], storedStamps(t, h, b.ID)["title"]
	if !sb.After(sa) {
		t.Errorf("second save stamped %q, not after the first's %q", sb, sa)
	}
	var node, last string
	if err := h.QueryRow(`SELECT node, last FROM hlc_clock`).Scan(&node, &last); err != nil {
		t.Fatalf("clock row: %v", err)
	}
	if hlc.Stamp(last) != sb || len(node) != 8 {
		t.Errorf("clock row (%q, %q); want this device's node and the last stamp %q", node, last, sb)
	}
}
