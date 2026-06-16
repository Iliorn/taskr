package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
			m.todos = append(m.todos, task)
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
			m.todos = append(m.todos, task)
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
