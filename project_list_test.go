package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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

// TestProjectDrillUsesTaskRenderer guards the new drilled-in Projects view:
// pressing Enter on a project must switch to a task-list view that uses the
// same row renderer as the Tasks tab (checkbox, cursor marker, task title),
// with the Gantt chart in the right column as an always-on preview.
func TestProjectDrillUsesTaskRenderer(t *testing.T) {
	tasks := []todo.Todo{
		mkTodo("t1", "Alpha task", todo.Pending),
		mkTodo("t2", "Beta task", todo.Pending),
		mkTodo("t3", "Gamma task", todo.Done),
	}
	for i := range tasks {
		tasks[i].Project = "myproject"
	}
	m := modelWithTasks(t, tasks...)
	m.tab = tabProjects

	// Drill into the project.
	m = sendKey(t, m, "enter")
	if !m.projectTaskMode {
		t.Fatal("enter on project should set projectTaskMode = true")
	}

	// buildProjectListContent should return a side-by-side layout (two columns).
	w := m.termWidth - 6
	outerH := m.termHeight - 4
	out := m.buildProjectListContent(w, outerH)

	// The task titles should be visible in the left column.
	if !strings.Contains(out, "Alpha task") {
		t.Errorf("drilled-in view missing 'Alpha task':\n%s", out)
	}
	if !strings.Contains(out, "Beta task") {
		t.Errorf("drilled-in view missing 'Beta task':\n%s", out)
	}
	if !strings.Contains(out, "Gamma task") {
		t.Errorf("drilled-in view missing 'Gamma task':\n%s", out)
	}

	// The cursor marker must appear (cursor starts at 0 = Alpha task).
	if !strings.Contains(out, "▶") {
		t.Errorf("drilled-in view missing cursor marker ▶")
	}

	// The Gantt "Timeline" header should appear in the right column.
	if !strings.Contains(out, "Timeline") {
		t.Errorf("drilled-in view missing Gantt header 'Timeline'")
	}

	// No-wrap contract: the full View() output must not exceed termWidth.
	// TestNarrowNoWrap covers this via View(); verify it here too via the
	// full-screen render so the drilled-in Projects path is explicitly swept.
	for _, line := range strings.Split(m.View(), "\n") {
		if lw := ansi.StringWidth(line); lw > m.termWidth {
			t.Errorf("View() line %d cells exceeds termWidth %d: %q", lw, m.termWidth, line)
		}
	}
}

// TestProjectDrillCursorScrolls guards that the drilled-in task-list cursor
// and listOffset scroll together, so the cursor task stays in the visible
// window as the user navigates down.
func TestProjectDrillCursorScrolls(t *testing.T) {
	// Use a terminal small enough that the task list requires scrolling.
	// With termHeight=20 and the header/footer overhead, listVisible is
	// roughly 14 rows, so 30 tasks forces scrolling.
	const n = 30
	var tasks []todo.Todo
	for i := 0; i < n; i++ {
		tk := mkTodo(fmt.Sprintf("t%02d", i), fmt.Sprintf("Task %02d", i), todo.Pending)
		tk.Project = "bigproject"
		tasks = append(tasks, tk)
	}
	m := modelWithTasks(t, tasks...)
	m.tab = tabProjects
	m.termHeight = 20 // small enough to require scroll

	// Drill into the project.
	m = sendKey(t, m, "enter")
	if !m.projectTaskMode {
		t.Fatal("enter on project should set projectTaskMode = true")
	}

	visible := m.listVisible()
	if visible >= n {
		// Guard: if all rows fit the test can't verify scrolling — report a
		// useful message instead of a false negative.
		t.Skipf("listVisible=%d >= n=%d, no scrolling needed; increase n or reduce termHeight", visible, n)
	}

	// Navigate down past the visible window so scrolling is required.
	for i := 0; i < n-1; i++ {
		m = sendKey(t, m, "down")
	}
	if m.cursor != n-1 {
		t.Fatalf("cursor = %d, want %d after navigating to last task", m.cursor, n-1)
	}

	// The last task must appear in the full rendered view.
	last := fmt.Sprintf("Task %02d", n-1)
	fullView := m.View()
	if !strings.Contains(fullView, last) {
		t.Errorf("cursor task %q missing from drilled-in view after scrolling:\n%s", last, fullView)
	}
	// The first task must have scrolled out of the task-list (left column).
	// The Gantt preview (right column) legitimately shows all task labels
	// regardless of scroll position, so we check only the left column directly.
	projects := m.allProjectsForList()
	taskListLines := m.renderProjectDrillTaskList(m.getProjectTasks(projects[m.projectCursor]))
	leftCol := strings.Join(taskListLines, "\n")
	if strings.Contains(leftCol, "Task 00") {
		t.Errorf("first task 'Task 00' should have scrolled out of the task-list column, but it's still visible:\n%s", leftCol)
	}
}

// TestProjectDrillEnterOpensDetail guards that pressing Enter on a task inside
// the drilled-in project view opens the detail pane, and Esc backs out of it.
func TestProjectDrillEnterOpensDetail(t *testing.T) {
	tk := mkTodo("t1", "Detail target", todo.Pending)
	tk.Project = "proj"
	m := modelWithTasks(t, tk)
	m.tab = tabProjects

	// Drill into the project, then press Enter on the task.
	m = sendKey(t, m, "enter") // drill in
	if !m.projectTaskMode {
		t.Fatal("first enter should drill into project")
	}
	m = sendKey(t, m, "enter") // open task detail
	if m.pane != paneDetail {
		t.Errorf("second enter should open detail pane, got pane = %v", m.pane)
	}

	// Esc exits the detail pane back to the task list.
	m = sendKey(t, m, "esc")
	if m.pane != paneList {
		t.Errorf("esc should return to list pane, got pane = %v", m.pane)
	}
	if !m.projectTaskMode {
		t.Errorf("esc from detail should stay in projectTaskMode")
	}

	// Another Esc backs out of the drill to the project list.
	m = sendKey(t, m, "esc")
	if m.projectTaskMode {
		t.Errorf("second esc should exit projectTaskMode")
	}
}

// TestProjectDrillDetailShowsTaskNotGantt guards the regression where pressing
// Enter on a task inside the drilled-in view (pane == paneDetail) rendered an
// empty right column instead of the task's detail. The right column must show
// detail content (task title) and must NOT show the Gantt timeline header.
func TestProjectDrillDetailShowsTaskNotGantt(t *testing.T) {
	tk := mkTodo("t1", "Detail target", todo.Pending)
	tk.Project = "proj"
	m := modelWithTasks(t, tk)
	m.tab = tabProjects

	// Drill into the project, then open the task detail.
	m = sendKey(t, m, "enter") // drill in
	if !m.projectTaskMode {
		t.Fatal("first enter should drill into project")
	}
	m = sendKey(t, m, "enter") // open task detail
	if m.pane != paneDetail {
		t.Fatal("second enter should open detail pane")
	}

	// Render the inner content panel (same call as View() delegates to).
	w := m.termWidth - 6
	outerH := m.termHeight - 4
	out := m.buildProjectListContent(w, outerH)

	// The task title must appear in the right column (via buildDetailContent).
	if !strings.Contains(out, "Detail target") {
		t.Errorf("detail pane should show task title 'Detail target':\n%s", out)
	}

	// The Gantt timeline header must NOT appear — it belongs in the right
	// column when browsing (pane == paneList), not when the detail is open.
	if strings.Contains(out, "Timeline") {
		t.Errorf("detail pane should not show Gantt 'Timeline' header:\n%s", out)
	}
}
