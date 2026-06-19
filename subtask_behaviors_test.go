package main

import (
	"testing"
	"time"

	"taskr/todo"
)

// makeSub is a tiny constructor used across the subtask-behavior tests so the
// fixtures stay readable. Stamps a deterministic CreatedAt offset so order is
// reproducible.
func makeSub(id, title, parentID string, offset time.Duration) todo.Todo {
	t := todo.New(title)
	t.ID = id
	t.ParentID = parentID
	t.CreatedAt = time.Now().Add(offset)
	return t
}

// ── Badges ───────────────────────────────────────────────────────────────────

func TestSubtaskProgressCountsDirectChildren(t *testing.T) {
	parent := makeSub("p", "parent", "", 0)
	c1 := makeSub("c1", "c1", "p", time.Second)
	c2 := makeSub("c2", "c2", "p", 2*time.Second)
	c2.Status = todo.Done
	grand := makeSub("g", "g", "c1", 3*time.Second) // shouldn't count toward p

	m := modelWithTasks(t, parent, c1, c2, grand)
	done, total := m.subtaskProgress("p")
	if total != 2 {
		t.Errorf("total = %d, want 2 (direct children only)", total)
	}
	if done != 1 {
		t.Errorf("done = %d, want 1", done)
	}
}

func TestHasOverdueDescendantRecurses(t *testing.T) {
	parent := makeSub("p", "parent", "", 0)
	mid := makeSub("m", "mid", "p", time.Second)
	leaf := makeSub("l", "leaf", "m", 2*time.Second)
	leaf.DueDate = time.Now().Add(-48 * time.Hour) // overdue

	m := modelWithTasks(t, parent, mid, leaf)
	overdue := map[string]bool{"l": true}
	if !m.hasOverdueDescendant("p", overdue) {
		t.Error("parent should report overdue grandchild")
	}
	if m.hasOverdueDescendant("m", overdue) == false {
		t.Error("mid should also report overdue child")
	}
	// A leaf has no descendants — should be false even though it's overdue.
	if m.hasOverdueDescendant("l", overdue) {
		t.Error("leaf has no descendants, should return false")
	}
}

// ── Parent due-date extension ────────────────────────────────────────────────

func TestExtendParentDueBumpsAncestorsOnlyForward(t *testing.T) {
	now := time.Now()
	parent := makeSub("p", "parent", "", 0)
	parent.DueDate = now.AddDate(0, 0, 5)
	child := makeSub("c", "child", "p", time.Second)
	child.DueDate = now.AddDate(0, 0, 10) // later than parent

	m := modelWithTasks(t, parent, child)
	bumped := m.extendParentDueIfNeeded("c")
	if len(bumped) != 1 || bumped[0] != "p" {
		t.Fatalf("bumped = %v, want [p]", bumped)
	}
	gotP := m.get("p")
	if !gotP.DueDate.Equal(child.DueDate) {
		t.Errorf("parent due = %v, want %v", gotP.DueDate, child.DueDate)
	}
}

func TestExtendParentDueDoesNotShrink(t *testing.T) {
	now := time.Now()
	parent := makeSub("p", "parent", "", 0)
	parent.DueDate = now.AddDate(0, 0, 10)
	child := makeSub("c", "child", "p", time.Second)
	child.DueDate = now.AddDate(0, 0, 5) // earlier than parent

	m := modelWithTasks(t, parent, child)
	bumped := m.extendParentDueIfNeeded("c")
	if len(bumped) != 0 {
		t.Errorf("bumped = %v, want none (child earlier than parent)", bumped)
	}
}

// ── Auto-close parent ───────────────────────────────────────────────────────

func TestAutoCloseOffDoesNothing(t *testing.T) {
	parent := makeSub("p", "parent", "", 0)
	c := makeSub("c", "c", "p", time.Second)
	c.Status = todo.Done

	m := modelWithTasks(t, parent, c)
	m.autoCloseParent = false
	closed := m.autoCloseAncestorsIfAllDone("c")
	if len(closed) != 0 {
		t.Errorf("setting off, closed = %v, want none", closed)
	}
	if m.get("p").Status != todo.Pending {
		t.Error("parent should remain Pending when auto-close is off")
	}
}

func TestAutoCloseOnClosesParentAndAncestors(t *testing.T) {
	gp := makeSub("gp", "grand", "", 0)
	p := makeSub("p", "parent", "gp", time.Second)
	c := makeSub("c", "child", "p", 2*time.Second)
	c.Status = todo.Done

	m := modelWithTasks(t, gp, p, c)
	m.autoCloseParent = true
	closed := m.autoCloseAncestorsIfAllDone("c")
	if len(closed) != 2 {
		t.Fatalf("closed = %v, want both p and gp", closed)
	}
	if m.get("p").Status != todo.Done {
		t.Error("parent should be auto-closed")
	}
	if m.get("gp").Status != todo.Done {
		t.Error("grandparent should be auto-closed (cascades up)")
	}
}

func TestAutoCloseStopsAtAncestorWithOpenSibling(t *testing.T) {
	gp := makeSub("gp", "grand", "", 0)
	p := makeSub("p", "parent", "gp", time.Second)
	c := makeSub("c", "child", "p", 2*time.Second)
	c.Status = todo.Done
	sibling := makeSub("s", "open sibling under gp", "gp", 3*time.Second) // still open

	m := modelWithTasks(t, gp, p, c, sibling)
	m.autoCloseParent = true
	closed := m.autoCloseAncestorsIfAllDone("c")
	if len(closed) != 1 || closed[0] != "p" {
		t.Fatalf("closed = %v, want [p] only (gp has an open sibling)", closed)
	}
	if m.get("gp").Status != todo.Pending {
		t.Error("grandparent should stay Pending because of the open sibling")
	}
}

// ── Recurring parent: clone subtree reset ────────────────────────────────────

func TestSpawnRecurrenceClonesSubtreeReset(t *testing.T) {
	now := time.Now()
	parent := makeSub("p", "weekly review", "", 0)
	parent.Recurrence = "weekly"
	parent.DueDate = now
	c1 := makeSub("c1", "follow up", "p", time.Second)
	c1.Status = todo.Done
	c1.TimeEntries = []todo.TimeEntry{{StartedAt: now, StoppedAt: now.Add(time.Hour)}}
	c2 := makeSub("c2", "send report", "p", 2*time.Second)
	grand := makeSub("g", "verify sent", "c1", 3*time.Second)
	grand.Status = todo.Done

	m := modelWithTasks(t, parent, c1, c2, grand)
	newID := m.spawnNextRecurrence(m.get("p"))
	if newID == "" {
		t.Fatal("spawnNextRecurrence returned empty ID")
	}

	cloneChildren := m.subtaskIDs(newID)
	if len(cloneChildren) != 2 {
		t.Fatalf("new parent has %d children, want 2", len(cloneChildren))
	}
	// Every cloned descendant must be Pending and history-free, regardless of
	// the source's status. That's the contract from the user — recurring task
	// respawns with all children un-done.
	all := append([]string{}, cloneChildren...)
	for i := 0; i < len(all); i++ {
		all = append(all, m.subtaskIDs(all[i])...)
	}
	if len(all) != 3 {
		t.Errorf("cloned descendants = %d, want 3 (c1+c2+grand-equivalents)", len(all))
	}
	for _, id := range all {
		c := m.get(id)
		if c == nil {
			t.Fatalf("clone %s missing", id)
		}
		if c.Status != todo.Pending {
			t.Errorf("clone %s status = %v, want Pending", c.Title, c.Status)
		}
		if len(c.TimeEntries) != 0 {
			t.Errorf("clone %s carried %d time entries, want 0", c.Title, len(c.TimeEntries))
		}
	}
}

// ── Score rollup ─────────────────────────────────────────────────────────────

func TestDescendantScoreRollupLiftsChildScore(t *testing.T) {
	parent := makeSub("p", "parent", "", 0)
	parent.Priority = todo.PriorityLow
	child := makeSub("c", "child", "p", time.Second)
	child.Priority = todo.PriorityHigh
	child.DueDate = time.Now().Add(-24 * time.Hour) // overdue + high priority

	rollup := descendantScoreRollup([]todo.Todo{parent, child})
	if rollup["p"] == 0 {
		t.Error("rollup should report a non-zero score for parent (child is overdue+high)")
	}
	parentOwn := sequenceScore(&parent)
	if rollup["p"] <= parentOwn {
		t.Errorf("rollup score %.2f should exceed parent's own %.2f", rollup["p"], parentOwn)
	}
}
