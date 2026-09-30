package app

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/Iliorn/tjek/todo"
)

// invariants_test.go drives the app with randomized key sequences (fixed seeds,
// so a failure is reproducible) and checks the store's structural invariants
// after every key. The undo property test already covers op/undo round trips;
// this covers the other half — that no *sequence* of ordinary keys can leave
// the store, its maintained indexes, or the cursors in an impossible state.

// checkInvariants asserts everything that must hold after any key, in any mode.
func checkInvariants(t *testing.T, m model, trail string) {
	t.Helper()

	// The subtaskOf index and the ParentID fields are two spellings of the same
	// relation; a mutation that updates one and not the other silently loses or
	// duplicates subtasks.
	for parent, children := range m.subtaskOf {
		for _, id := range children {
			child := m.tasks[id]
			if child == nil {
				t.Fatalf("%s: subtaskOf[%s] lists %s, which is not in the store", trail, parent[:6], id[:6])
			}
			if child.ParentID != parent {
				t.Fatalf("%s: subtaskOf[%s] lists %s whose ParentID is %q",
					trail, parent[:6], id[:6], child.ParentID)
			}
		}
	}
	for id, task := range m.tasks {
		if task.ParentID == "" {
			continue
		}
		found := false
		for _, sib := range m.subtaskOf[task.ParentID] {
			if sib == id {
				found = true
				break
			}
		}
		if !found && m.tasks[task.ParentID] != nil {
			t.Fatalf("%s: %s has ParentID %s but is missing from that parent's index",
				trail, id[:6], task.ParentID[:6])
		}
	}

	// runningTimers drives the footer, the timer tick and the runaway guard, so
	// it must agree with the actual open time entries.
	for id := range m.runningTimers {
		task := m.tasks[id]
		if task == nil {
			t.Fatalf("%s: runningTimers holds %s, which is not in the store", trail, id[:6])
		}
		if !task.IsTimerRunning() {
			t.Fatalf("%s: runningTimers holds %s (%q) but it has no open entry", trail, id[:6], task.Title)
		}
	}
	for id, task := range m.tasks {
		if task.IsTimerRunning() {
			if _, ok := m.runningTimers[id]; !ok {
				t.Fatalf("%s: %s (%q) has an open time entry but is not in runningTimers",
					trail, id[:6], task.Title)
			}
		}
	}

	// A task may never depend on itself; that would make it permanently blocked.
	for id, task := range m.tasks {
		for _, dep := range task.Dependencies {
			if dep == id {
				t.Fatalf("%s: %s (%q) depends on itself", trail, id[:6], task.Title)
			}
		}
	}

	// Cursors address rows that exist. An out-of-range cursor selects nothing,
	// which turns the next key into a silent no-op.
	if n := m.currentTaskListLen(); m.tab == tabTasks && m.mode == modeNormal && n > 0 && m.cursor >= n {
		t.Fatalf("%s: Tasks cursor %d past the end of a %d-row list", trail, m.cursor, n)
	}
	if tags := m.getFilteredTagsForTab(); len(tags) > 0 && m.tagTabCursor >= len(tags) {
		t.Fatalf("%s: tag cursor %d past the end of %d tags", trail, m.tagTabCursor, len(tags))
	}
	if tasks, drilled := m.drillTaskList(); drilled && len(tasks) > 0 && m.cursor >= len(tasks) {
		t.Fatalf("%s: drill cursor %d past the end of %d tasks", trail, m.cursor, len(tasks))
	}
}

func monkeyModel(t *testing.T) model {
	t.Helper()
	parent := todo.New("Fix the boiler")
	parent.AddTag("home")
	parent.Project = "House"
	parent.DueDate = time.Now().Add(-24 * time.Hour)
	sub := todo.NewSubtask("Find the receipt", parent.ID)
	other := todo.New("Write the memo")
	other.AddTag("work")
	other.Project = "Q3"
	other.AddDependency(parent.ID)
	recurring := todo.New("Water the plants")
	recurring.Recurrence = "weekly"
	recurring.AddTag("home")
	done := todo.New("Old thing")
	done.Status = todo.Done
	m := modelWithTasks(t, parent, sub, other, recurring, done)
	m.termWidth, m.termHeight = 100, 30
	return m
}

// monkeyKeys is the alphabet: every normal-mode key plus the ones that answer a
// modal, so sequences wander in and out of modes on their own.
var monkeyKeys = []string{
	"up", "down", "left", "right", "home", "end", "pgup", "pgdown",
	"enter", "esc", "tab", "1", "2", "3", "4", "5", "6", "7",
	"a", "d", "t", "T", "p", "r", "x", "n", "f", "h", "s", "m", "u", "y", "?",
	"H", "L", "[", "]", "#", "@", "/", "milk", "#home", "@House", "45m", "backspace",
}

// Seeds are fixed so a failure reproduces; the count is what keeps the suite
// fast. Widen both temporarily when hunting (300 × 400 was the sweep that found
// the stale-cursor bug the clamp in clampCursors now prevents).
func TestMonkeyKeysPreserveInvariants(t *testing.T) {
	for seed := int64(1); seed <= 12; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			m := monkeyModel(t)
			trail := ""
			for step := 0; step < 250; step++ {
				k := monkeyKeys[rng.Intn(len(monkeyKeys))]
				trail = fmt.Sprintf("seed %d step %d (…%s)", seed, step, trail)
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s key %q: panic: %v", trail, k, r)
						}
					}()
					m = sendKey(t, m, k)
					_ = m.View()
				}()
				checkInvariants(t, m, fmt.Sprintf("%s key %q", trail, k))
				trail = k
			}
		})
	}
}

// The bug the monkey found, as a readable case: delete the task the cursor sits
// on and the list gets shorter under it, so without a clamp the cursor points
// past the end — nothing is selected and the next key silently does nothing.
func TestCursorSurvivesTheListShrinking(t *testing.T) {
	m := modelWithTasks(t, todo.New("Alpha"), todo.New("Beta"), todo.New("Gamma"))
	m.tab = tabTasks
	m = script(t, m, "end") // onto the last row
	last := m.currentTodo()
	if last == nil {
		t.Fatal("end did not select the last task")
	}

	m = script(t, m, "x", "y") // delete it
	if m.get(last.ID) != nil {
		t.Fatal("the task was not deleted")
	}
	if got := m.currentTodo(); got == nil {
		t.Fatalf("cursor %d selects nothing after the list shrank", m.cursor)
	}
	// …and the next key reaches the task now under the cursor.
	before := m.currentTodo().ID
	m = sendKey(t, m, "d")
	if got := m.get(before); got == nil || got.Status != todo.Done {
		t.Error("the key after the delete did not reach the selected task")
	}
}

// Undo is the other way the list shrinks under the cursor: undoing a create
// removes the task the cursor was left on.
func TestCursorSurvivesUndoOfACreate(t *testing.T) {
	m := modelWithTasks(t, todo.New("Alpha"))
	m.tab = tabTasks
	m = script(t, m, "a", "beta", "enter", "esc")
	if m.len() != 2 {
		t.Fatalf("store has %d tasks, want 2", m.len())
	}
	m = script(t, m, "end", "u")
	if m.len() != 1 {
		t.Fatalf("after undo: store has %d tasks, want 1", m.len())
	}
	if got := m.currentTodo(); got == nil {
		t.Fatalf("cursor %d selects nothing after undo removed the last task", m.cursor)
	}
}

// A tab switch restores that tab's search, and the cursor is clamped against
// the list that search gives, not the one the tab left behind: close a card on
// the unfiltered Board while the Tasks list is filtered and sits on its last
// row, and back on Tasks the cursor must still be on a row.
func TestTasksCursorSurvivesACloseOnAnotherTab(t *testing.T) {
	var tasks []todo.Todo
	for _, title := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		x := todo.New(title)
		x.Project = "Kitchen"
		x.SetPriority(todo.PriorityHigh) // so the Board's first card is one of these
		tasks = append(tasks, x)
	}
	elsewhere := todo.New("Elsewhere")
	elsewhere.SetPriority(todo.PriorityLow)
	m := modelWithTasks(t, append(tasks, elsewhere)...)
	m.termWidth, m.termHeight = 120, 30
	m = script(t, m, "/", "@Kitchen", "enter", "end")
	m = script(t, m, "5", "d")
	if closed := m.get(tasks[0].ID); closed == nil {
		t.Fatal("setup: the task store lost a task")
	}
	m = script(t, m, "1")
	_ = m.View() // the frame is what refreshes the filtered list
	if n := m.currentTaskListLen(); m.cursor >= n {
		t.Errorf("back on Tasks the cursor is %d on a %d-row list", m.cursor, n)
	}
}
