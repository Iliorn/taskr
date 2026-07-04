package main

import (
	"fmt"
	"strings"
	"testing"

	"taskr/todo"
)

// TestProjectListWindowFollowsCursor guards fd8502d1: the Projects tab renderer
// used to draw from index 0 and the offset was clamped against m.cursor (the
// Tasks-tab cursor) instead of m.projectCursor, so navigating down past the
// visible rows walked the project cursor off-screen. The rendered window must
// follow m.projectCursor like the other list tabs.
func TestProjectListWindowFollowsCursor(t *testing.T) {
	const n = 40
	var tasks []todo.Todo
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("p%02d", i) // selectProjects sorts these p00..p39
		tk := mkTodo(p, "task "+p, todo.Pending)
		tk.Project = p
		tasks = append(tasks, tk)
	}
	m := modelWithTasks(t, tasks...)
	m.tab = tabProjects

	visible := m.projectListVisibleRows()
	if visible < 1 || visible >= n {
		t.Fatalf("need 1 <= visible < %d for a meaningful window, got %d", n, visible)
	}

	// Walk the cursor to the last project via real key handling (down = j),
	// which re-runs the offset clamp at the end of updateList each step.
	for i := 0; i < n-1; i++ {
		m = sendKey(t, m, "down")
	}
	if m.projectCursor != n-1 {
		t.Fatalf("projectCursor = %d, want %d", m.projectCursor, n-1)
	}

	out := m.renderProjectListContent(m.allProjectsForList())
	if !strings.Contains(out, "p39") {
		t.Errorf("cursor project p39 missing from rendered window:\n%s", out)
	}
	if !strings.Contains(out, "▶") {
		t.Errorf("cursor marker missing from rendered window:\n%s", out)
	}
	if strings.Contains(out, "p00") {
		t.Errorf("top project p00 should have scrolled out of the window:\n%s", out)
	}
}
