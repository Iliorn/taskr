package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"taskr/todo"
)

// modelWithTasks builds a fakeRepo-backed model pre-seeded with `tasks` so
// tests can exercise list-pane behavior without touching real storage. Sets
// a sane terminal size so layout-dependent paths (cursor clamping, list
// height) have realistic bounds.
func modelWithTasks(t *testing.T, tasks ...todo.Todo) model {
	t.Helper()
	m := initialModel(&fakeRepo{todos: tasks})
	m.termWidth = 120
	m.termHeight = 40
	m.ensureCache()
	return m
}

// sendKey is the test analogue of "the user pressed this key". Tea's
// KeyMsg uses Runes for printable chars and a typed Type for special keys
// (Tab, Enter, arrows, …) — wrap both shapes here so test bodies stay
// readable.
func sendKey(t *testing.T, m model, k string) model {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.Update(msg)
	return next.(model)
}

// ── Sort cycle ────────────────────────────────────────────────────────────────

func TestSortCycleVisitsAllThreeModes(t *testing.T) {
	m := modelWithTasks(t)

	if m.taskSort != taskSortSequence {
		t.Fatalf("initial sort = %v, want Sequence", m.taskSort)
	}
	m = sendKey(t, m, "s")
	if m.taskSort != taskSortDueDate {
		t.Errorf("after 1×s: sort = %v, want DueDate", m.taskSort)
	}
	m = sendKey(t, m, "s")
	if m.taskSort != taskSortSize {
		t.Errorf("after 2×s: sort = %v, want Size", m.taskSort)
	}
	m = sendKey(t, m, "s")
	if m.taskSort != taskSortSequence {
		t.Errorf("after 3×s: sort = %v, want Sequence (wrap)", m.taskSort)
	}
}

func TestSortCycleResetsCursorAndOffset(t *testing.T) {
	m := modelWithTasks(t, todo.New("a"), todo.New("b"), todo.New("c"))
	m.cursor = 2
	m.listOffset = 1

	m = sendKey(t, m, "s")
	if m.cursor != 0 || m.listOffset != 0 {
		t.Errorf("sort cycle should reset cursor/offset, got cursor=%d offset=%d",
			m.cursor, m.listOffset)
	}
}

// ── Tab switching ────────────────────────────────────────────────────────────

func TestTabSwitchByNumberKey(t *testing.T) {
	m := modelWithTasks(t)

	cases := []struct {
		key  string
		want tab
	}{
		{"1", tabTasks},
		{"2", tabCalendar},
		{"3", tabProjects},
		{"4", tabTags},
		{"5", tabLearnings},
		{"6", tabStats},
		{"7", tabSettings},
	}
	for _, c := range cases {
		m = sendKey(t, m, c.key)
		if m.tab != c.want {
			t.Errorf("key %q: tab = %v, want %v", c.key, m.tab, c.want)
		}
	}
}

func TestTabKeyAdvancesThroughTabs(t *testing.T) {
	m := modelWithTasks(t)
	if m.tab != tabTasks {
		t.Fatalf("initial tab = %v, want Tasks", m.tab)
	}
	m = sendKey(t, m, "tab")
	if m.tab != tabCalendar {
		t.Errorf("after 1 tab: %v, want Calendar", m.tab)
	}
	// numTabs presses from Tasks bring us back to Tasks (full wrap). We've
	// already sent one, so send numTabs-1 more.
	for i := 0; i < numTabs-1; i++ {
		m = sendKey(t, m, "tab")
	}
	if m.tab != tabTasks {
		t.Errorf("after %d tabs: %v, want Tasks (full wrap)", numTabs, m.tab)
	}
}

// ── Cursor navigation ────────────────────────────────────────────────────────

func TestCursorJKMovesCursor(t *testing.T) {
	m := modelWithTasks(t, todo.New("a"), todo.New("b"), todo.New("c"))

	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m = sendKey(t, m, "j")
	if m.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.cursor)
	}
	m = sendKey(t, m, "j")
	if m.cursor != 2 {
		t.Errorf("after 2×j: cursor = %d, want 2", m.cursor)
	}
	m = sendKey(t, m, "k")
	if m.cursor != 1 {
		t.Errorf("after k: cursor = %d, want 1", m.cursor)
	}
}

func TestCursorClampsToListBounds(t *testing.T) {
	m := modelWithTasks(t, todo.New("a"), todo.New("b"))

	// k at top: cursor should not go negative.
	m = sendKey(t, m, "k")
	if m.cursor != 0 {
		t.Errorf("k at top: cursor = %d, want 0 (no negative)", m.cursor)
	}
	// j past bottom: cursor should clamp to last index (len-1).
	m = sendKey(t, m, "j")
	m = sendKey(t, m, "j")
	m = sendKey(t, m, "j") // would go to 3, but only 2 items
	if m.cursor != 1 {
		t.Errorf("j past bottom: cursor = %d, want 1 (clamped)", m.cursor)
	}
}

// ── Toggle done ──────────────────────────────────────────────────────────────

func TestDKeyTogglesDone(t *testing.T) {
	task := todo.New("flip me")
	m := modelWithTasks(t, task)

	m = sendKey(t, m, "d")
	got := m.get(task.ID)
	if got == nil {
		t.Fatal("task vanished after d")
	}
	if got.Status != todo.Done {
		t.Errorf("after d: status = %v, want Done", got.Status)
	}
}

// ── Priority cycle ───────────────────────────────────────────────────────────

func TestPKeyCyclesPriority(t *testing.T) {
	task := todo.New("priority dance")
	// task.New defaults Priority to Medium.
	m := modelWithTasks(t, task)

	m = sendKey(t, m, "p")
	if got := m.get(task.ID); got == nil || got.Priority != todo.PriorityHigh {
		t.Errorf("after p (Medium → High): got %v", got)
	}
	m = sendKey(t, m, "p")
	if got := m.get(task.ID); got == nil || got.Priority != todo.PriorityLow {
		t.Errorf("after 2×p (High → Low): got %v", got)
	}
	m = sendKey(t, m, "p")
	if got := m.get(task.ID); got == nil || got.Priority != todo.PriorityMedium {
		t.Errorf("after 3×p (Low → Medium, wrap): got %v", got)
	}
}

// ── Bias cycle on Settings tab ───────────────────────────────────────────────

func TestBiasCycleOnSettingsTab(t *testing.T) {
	// Reset to a known starting point so prior tests' bias mutations don't
	// leak in. applyBiases is global state — explicit reset is the safest
	// guard for parallel-safety even with -race off.
	applyBiases(biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true})
	defer applyBiases(biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true})

	m := modelWithTasks(t)
	m.tab = tabSettings
	m.settingsCursor = settingBiasDeadline

	m = sendKey(t, m, "right")
	if activeBiases.Deadline != biasIntense {
		t.Errorf("after right on Deadline row: %v, want Intense (Balanced → next)", activeBiases.Deadline)
	}
	m = sendKey(t, m, "left")
	if activeBiases.Deadline != biasBalanced {
		t.Errorf("after left: %v, want Balanced", activeBiases.Deadline)
	}

	// Other rows are not touched by this row's cycle.
	if activeBiases.Priority != biasBalanced || activeBiases.Momentum != biasBalanced {
		t.Errorf("siblings should be untouched: Priority=%v Momentum=%v",
			activeBiases.Priority, activeBiases.Momentum)
	}

	// Move cursor to the Momentum row and confirm the cycle hits that one.
	m.settingsCursor = settingBiasMomentum
	m = sendKey(t, m, "right")
	if activeBiases.Momentum != biasIntense {
		t.Errorf("after right on Momentum row: %v, want Intense", activeBiases.Momentum)
	}
}
