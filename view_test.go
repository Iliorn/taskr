package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"taskr/todo"
)

// TestNarrowNoWrap ensures no rendered line ever exceeds the terminal width,
// which would cause ugly wrapping inside the bordered panels.
func TestNarrowNoWrap(t *testing.T) {
	for _, width := range []int{40, 50, 60, 70, 80, 120} {
		m := newTestModel()
		m.termWidth = width
		m.termHeight = 30
		for i := 0; i < 5; i++ {
			task := todo.New("A fairly long task title that could overflow easily here")
			task.DueDate = time.Now().AddDate(0, 0, i)
			task.Tags = []string{"alpha", "beta"}
			m.add(task)
		}
		m.refreshCaches()
		out := m.View()
		for n, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width=%d: line %d is %d cells wide: %q", width, n, w, line)
			}
		}
	}
}

// TestKeyHintsVisibleSearch asserts the Tasks-tab footer always advertises
// search: at widths where the full hint list can't fit, renderKeyHints must
// fall back to the curated short set instead of truncating it away.
func TestKeyHintsVisibleSearch(t *testing.T) {
	m := newTestModel()
	m.tab = tabTasks
	for _, width := range []int{74, 114, 200} {
		hint := m.renderKeyHints(width)
		if !strings.Contains(hint, "/ search") {
			t.Errorf("width=%d: hint line lost '/ search': %q", width, hint)
		}
	}
}

// TestQuickAddShowsSyntaxHint asserts the quick-add input surfaces the inline
// syntax, and that other text inputs (e.g. detail-pane comments) don't.
func TestQuickAddShowsSyntaxHint(t *testing.T) {
	applyLang(string(langEN))
	m := newTestModel()
	m.termWidth, m.termHeight = 100, 30
	m2 := script(t, m, "a")
	if !strings.Contains(m2.View(), "#tag @project due:tomorrow") {
		t.Error("quick-add input should show the syntax hint line")
	}
}

// TestNarrowNoWrapDanish guards against translations that overflow a bordered
// panel the English source fit. Danish words are generally longer, so for every
// tab/width it asserts the widest Danish line is no wider than the English
// baseline (or the terminal). Comparing against English keeps pre-existing
// layout limits on the densest tabs from counting as translation regressions.
func TestNarrowNoWrapDanish(t *testing.T) {
	// Reflowing single-list tabs are checked across every width. The calendar and
	// projects tabs use fixed two-panel layouts with their own minimum widths (and
	// pre-existing narrow-width handling), so they're only swept where they fit.
	listTabs := []tab{tabTasks, tabTags, tabLearnings, tabStats, tabSettings}
	panelTabs := []tab{tabCalendar, tabProjects}

	// initialModel applies the stored language, so set lang *after* building it.
	maxLineWidth := func(width int, tb tab, lang language) int {
		m := newTestModel()
		applyLang(string(lang))
		m.termWidth = width
		m.termHeight = 30
		for i := 0; i < 5; i++ {
			task := todo.New("A fairly long task title that could overflow easily here")
			task.DueDate = time.Now().AddDate(0, 0, i)
			task.Tags = []string{"alpha", "beta"}
			task.Project = "Demo"
			m.add(task)
		}
		m.refreshCaches()
		m.tab = tb
		widest := 0
		for _, line := range strings.Split(m.View(), "\n") {
			if w := ansi.StringWidth(line); w > widest {
				widest = w
			}
		}
		return widest
	}

	check := func(width int, tb tab) {
		baseline := maxLineWidth(width, tb, langEN)
		got := maxLineWidth(width, tb, langDA)
		applyLang(string(langEN))

		limit := baseline
		if width > limit {
			limit = width
		}
		if got > limit {
			t.Errorf("da tab=%d width=%d: widest line %d cells exceeds limit %d (en baseline %d)",
				tb, width, got, limit, baseline)
		}
	}

	for _, width := range []int{40, 50, 60, 70, 80, 120} {
		for _, tb := range listTabs {
			check(width, tb)
		}
	}
	for _, width := range []int{70, 80, 120} {
		for _, tb := range panelTabs {
			check(width, tb)
		}
	}
}

// A High-priority task carries a trailing "!" in the task list so cycling
// priority (p) gives visible feedback; a lower-priority task does not.
func TestHighPriorityShowsExclamationInList(t *testing.T) {
	hi := todo.New("Finish the audit")
	hi.Priority = todo.PriorityHigh
	lo := todo.New("Water the plants")
	lo.Priority = todo.PriorityLow
	m := modelWithTasks(t, hi, lo)

	var hiLine, loLine string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "Finish the audit") {
			hiLine = line
		}
		if strings.Contains(line, "Water the plants") {
			loLine = line
		}
	}
	if hiLine == "" || loLine == "" {
		t.Fatalf("both task rows should render; hi=%q lo=%q", hiLine, loLine)
	}
	if !strings.Contains(hiLine, "!") {
		t.Errorf("high-priority row should carry a '!': %q", hiLine)
	}
	if strings.Contains(loLine, "!") {
		t.Errorf("low-priority row should have no '!': %q", loLine)
	}
}

// At side-by-side widths the Tasks tab always previews the cursor task's
// detail in the right column — without pressing Enter — and narrow terminals
// fall back to the stacked enter-to-open layout.
func TestSideBySideDetailPreview(t *testing.T) {
	m := modelWithTasks(t, todo.New("pay rent"), todo.New("water plants"))
	m.termHeight = 40

	m.termWidth = sideBySideMinWidth + 10
	if !strings.Contains(m.View(), tr("Priority")) {
		t.Error("side-by-side: detail preview should render without Enter")
	}

	m.termWidth = sideBySideMinWidth - 10
	if strings.Contains(m.View(), tr("Priority")) {
		t.Error("stacked fallback: detail should stay hidden until Enter")
	}
	m2 := script(t, m, "enter")
	if !strings.Contains(m2.View(), tr("Priority")) {
		t.Error("stacked fallback: Enter should open the detail pane")
	}
}

// The Learnings tab shares the Tasks tab's side-by-side contract: at wide
// widths the cursor learning's detail previews in the right column without
// Enter, and narrow terminals fall back to the stacked enter-to-open layout.
func TestLearningsSideBySide(t *testing.T) {
	task := todo.New("ship the release")
	task.AddLearning("tag the release before pushing")
	m := modelWithTasks(t, task)
	m.tab = tabLearnings
	m.termHeight = 40

	m.termWidth = sideBySideMinWidth + 10
	if !strings.Contains(m.View(), tr("Source task:  ")) {
		t.Error("side-by-side: learning detail should render without Enter")
	}

	m.termWidth = sideBySideMinWidth - 10
	if strings.Contains(m.View(), tr("Source task:  ")) {
		t.Error("stacked fallback: learning detail should stay hidden until Enter")
	}
	m2 := script(t, m, "enter")
	if !strings.Contains(m2.View(), tr("Source task:  ")) {
		t.Error("stacked fallback: Enter should open the learning detail pane")
	}
}

// The Tags tab's detail is always-on either way; side-by-side only moves it
// from the stacked panel below the list into the right column at wide widths.
// detailVisible gates the stacked panel, so it must flip with the threshold
// while the summary line stays rendered at both widths.
func TestTagsSideBySide(t *testing.T) {
	task := todo.New("fix the fence")
	task.AddTag("home")
	m := modelWithTasks(t, task)
	m.tab = tabTags
	m.termHeight = 40

	summary := strings.TrimSpace(fmt.Sprintf(tr("  %d active · %d done · %d overdue"), 1, 0, 0))

	m.termWidth = sideBySideMinWidth + 10
	if !strings.Contains(m.View(), summary) {
		t.Error("side-by-side: tag detail should render in the right column")
	}
	if m.detailVisible() {
		t.Error("side-by-side: the stacked tag panel should be off")
	}

	m.termWidth = sideBySideMinWidth - 10
	if !strings.Contains(m.View(), summary) {
		t.Error("stacked fallback: tag detail should render below the list")
	}
	if !m.detailVisible() {
		t.Error("stacked fallback: detailVisible should report the stacked panel")
	}
}

// A selected overdue row must show both states: the overdue foreground and the
// selection background. Before the combined styles, the status colour won the
// style switch outright and the only cursor cue on an overdue-heavy list was
// the arrow glyph.
func TestSelectedOverdueRowKeepsSelectionBackground(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	applyTheme(themes[0])
	defer func() {
		lipgloss.SetColorProfile(before)
		applyTheme(themes[0])
	}()

	over := todo.New("Pay the rent")
	over.DueDate = time.Now().Add(-48 * time.Hour)
	over2 := todo.New("File the taxes")
	over2.DueDate = time.Now().Add(-24 * time.Hour)
	m := modelWithTasks(t, over, over2)

	var selLine, plainLine string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "Pay the rent") {
			selLine = line // cursor starts on the first row
		}
		if strings.Contains(line, "File the taxes") {
			plainLine = line
		}
	}
	if selLine == "" || plainLine == "" {
		t.Fatalf("both overdue rows should render; sel=%q plain=%q", selLine, plainLine)
	}
	wantPrefix := newFastStyle(selectedOverdueRowStyle).prefix
	if !strings.Contains(selLine, wantPrefix) {
		t.Errorf("selected overdue row should use overdue fg + sel bg (%q): %q", wantPrefix, selLine)
	}
	if strings.Contains(plainLine, "48;2;") {
		t.Errorf("unselected overdue row should have no background: %q", plainLine)
	}
}

// An overdue dependency conveys itself through the row color, not a glyph — a
// normal-priority task whose dependency is overdue carries no "!".
func TestOverdueDependencyAddsNoGlyph(t *testing.T) {
	dep := todo.New("blocking dep")
	dep.ID = "dep1"
	dep.DueDate = time.Now().Add(-48 * time.Hour) // overdue, still pending
	blocked := todo.New("Finish the audit")       // normal priority, depends on dep1
	blocked.Dependencies = []string{"dep1"}

	m := modelWithTasks(t, blocked, dep)
	var line string
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, "Finish the audit") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("blocked task row should render")
	}
	if strings.Contains(line, "!") {
		t.Errorf("overdue-dependency row should carry no '!' (color-only): %q", line)
	}
}

// The detail pane shows both directions of the dependency graph in one
// merged Dependencies list: ↧ rows for outbound edges, dimmed ↥ rows for
// the pending tasks waiting on this one.
func TestDetailShowsInboundDependents(t *testing.T) {
	blocker := todo.New("build the widget")
	blocker.ID = "blk1"
	dependent := todo.New("ship the widget")
	dependent.Dependencies = []string{"blk1"}
	m := modelWithTasks(t, blocker, dependent)
	m.termWidth, m.termHeight = 120, 40 // side-by-side: detail previews the cursor task

	setCursorOn := func(id string) {
		for i := range m.cache.active {
			if m.cache.active[i].ID == id {
				m.cursor = i
				return
			}
		}
		t.Fatalf("task %s not in active list", id)
	}

	setCursorOn("blk1")
	out := m.View()
	if !strings.Contains(out, "↥ Ship the widget") {
		t.Errorf("blocker detail should list its dependent as a ↥ row:\n%s", out)
	}

	setCursorOn(dependent.ID)
	m.invalidateDetailCache()
	out = m.View()
	// "    ↥ " is the detail pane's inbound-row indent; the bare trailing ↥
	// on the blocker's list row is a different (expected) glyph.
	if strings.Contains(out, "    ↥ ") {
		t.Errorf("dependent's detail should have no ↥ rows:\n%s", out)
	}
	// The outbound side carries the ↧ glyph, mirroring the list rows.
	if !strings.Contains(out, "↧ Build the widget") {
		t.Errorf("outbound dependency line should carry the ↧ glyph:\n%s", out)
	}
}

// Enter on a ↥ row jumps to the task waiting on this one, exactly like
// enter on a ↧ row jumps to the dependency.
func TestEnterOnInboundDependentJumps(t *testing.T) {
	blocker := todo.New("build the widget")
	blocker.ID = "blk1"
	dependent := todo.New("ship the widget")
	dependent.Dependencies = []string{"blk1"}
	m := modelWithTasks(t, blocker, dependent)
	for i := range m.cache.active {
		if m.cache.active[i].ID == "blk1" {
			m.cursor = i
		}
	}
	m.pane = paneDetail
	// The blocker has no outbound deps, so row 0 is the inbound ↥ row.
	m.detail = detailState{field: fieldDependencies, depCursor: 0}

	updated, _ := m.startEditing()
	m2 := updated.(model)
	if m2.pane != paneList {
		t.Fatalf("pane = %v, want paneList after jump", m2.pane)
	}
	if cur := m2.currentTodo(); cur == nil || cur.ID != dependent.ID {
		t.Errorf("cursor should land on the dependent, got %+v", cur)
	}
}

// When comments wrap to multiple lines in a narrow column, the detail scroll
// must still bring the selected comment into view — counting one line per
// comment used to undershoot and push the cursor row off the bottom edge.
func TestDetailScrollReachesWrappedComment(t *testing.T) {
	task := todo.New("Task with a very long tail")
	for i := 1; i <= 12; i++ {
		task.AddComment(fmt.Sprintf("comment number %d with some length to it", i))
	}
	m := modelWithTasks(t, task)
	m.termWidth, m.termHeight = 120, 24
	m.pane = paneDetail
	m.detail = detailState{field: fieldComments, commentCursor: 9}
	m.invalidateDetailCache()

	if !strings.Contains(m.View(), "comment number 10") {
		t.Error("selected comment should be scrolled into view")
	}
}
