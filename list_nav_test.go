package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

func tenTaskModel(t *testing.T) model {
	t.Helper()
	var todos []todo.Todo
	for i := 0; i < 10; i++ {
		todos = append(todos, todo.New(fmt.Sprintf("task %02d", i)))
	}
	m := modelWithTasks(t, todos...)
	m.tab = tabTasks
	return m
}

// Home/End jump to the ends; PgUp/PgDn move by a page and clamp at the bounds.
func TestListJumpAndPageNavigation(t *testing.T) {
	m := tenTaskModel(t)
	if got := m.currentTaskListLen(); got != 10 {
		t.Fatalf("expected 10 rows, got %d", got)
	}

	m.listJumpBottom()
	if m.cursor != 9 {
		t.Errorf("End: cursor = %d, want 9", m.cursor)
	}
	m.listJumpTop()
	if m.cursor != 0 {
		t.Errorf("Home: cursor = %d, want 0", m.cursor)
	}

	// PgDn advances by a page step and never past the last row.
	step := m.listPageStep()
	if step < 1 {
		t.Fatalf("page step should be >= 1, got %d", step)
	}
	m.listPage(1)
	if want := min(step, 9); m.cursor != want {
		t.Errorf("PgDn from top: cursor = %d, want %d", m.cursor, want)
	}
	m.cursor = 9
	m.listPage(1) // already at the bottom → clamped, no wrap
	if m.cursor != 9 {
		t.Errorf("PgDn at bottom: cursor = %d, want 9 (no wrap)", m.cursor)
	}
	m.cursor = 0
	m.listPage(-1) // already at the top → clamped, no wrap
	if m.cursor != 0 {
		t.Errorf("PgUp at top: cursor = %d, want 0 (no wrap)", m.cursor)
	}

	// Out-of-range targets clamp.
	m.moveListCursorTo(999)
	if m.cursor != 9 {
		t.Errorf("moveListCursorTo(999): cursor = %d, want 9", m.cursor)
	}
}

// The Tasks header carries a right-aligned cursor/total position indicator.
func TestTaskListPositionIndicator(t *testing.T) {
	m := tenTaskModel(t)

	m.cursor = 0
	if got := ansi.Strip(m.renderTaskList()); !strings.Contains(got, "1/10") {
		t.Errorf("header should show 1/10 at the top; got first line:\n%s", firstLine(got))
	}
	m.cursor = 9
	if got := ansi.Strip(m.renderTaskList()); !strings.Contains(got, "10/10") {
		t.Errorf("header should show 10/10 at the bottom; got first line:\n%s", firstLine(got))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
