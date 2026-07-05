package main

import (
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
