package main

import "testing"

// Switching away from a tab and back restores its cursor, scroll, and search;
// tab-private state (history mode) persists too.
func TestSwitchTabRetainsPerTabState(t *testing.T) {
	m := tenTaskModel(t)
	m.cursor = 4
	m.listOffset = 2
	m.searchQuery = "task 0"
	m.showHistory = true

	m.switchTab(tabCalendar)
	// The Tasks cursor is now the calendar tab's own (fresh) value.
	if m.cursor != 0 {
		t.Errorf("calendar tab should have its own cursor, got %d", m.cursor)
	}

	m.switchTab(tabTasks)
	if m.cursor != 4 {
		t.Errorf("cursor not retained: got %d, want 4", m.cursor)
	}
	if m.listOffset != 2 {
		t.Errorf("listOffset not retained: got %d, want 2", m.listOffset)
	}
	if m.searchQuery != "task 0" {
		t.Errorf("search not retained: got %q", m.searchQuery)
	}
	if !m.showHistory {
		t.Error("history mode (tab-private) should persist across a tab detour")
	}
}

// Tasks and Projects both read m.searchQuery, so each must keep its own —
// a Tasks search must not leak into Projects.
func TestSwitchTabSearchIsPerTab(t *testing.T) {
	m := tenTaskModel(t)
	m.searchQuery = "milk"

	m.switchTab(tabProjects)
	if m.searchQuery != "" {
		t.Errorf("Projects should start with its own empty search, got %q", m.searchQuery)
	}
	m.searchQuery = "widget"

	m.switchTab(tabTasks)
	if m.searchQuery != "milk" {
		t.Errorf("Tasks search not restored: got %q, want milk", m.searchQuery)
	}
	m.switchTab(tabProjects)
	if m.searchQuery != "widget" {
		t.Errorf("Projects search not restored: got %q, want widget", m.searchQuery)
	}
}

// Re-selecting the current tab is a no-op — it must not wipe state.
func TestSwitchTabToSameTabIsNoOp(t *testing.T) {
	m := tenTaskModel(t)
	m.cursor = 5
	m.searchQuery = "keep me"

	m.switchTab(tabTasks)
	if m.cursor != 5 || m.searchQuery != "keep me" {
		t.Errorf("same-tab switch should not reset state: cursor=%d search=%q", m.cursor, m.searchQuery)
	}
}
