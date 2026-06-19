package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// TestRenderGanttNarrowNoPanic guards the Gantt "today:" marker against an
// out-of-bounds write: when the localized label is wider than the (floored)
// chart, the insert position goes negative. It must clip rather than panic, in
// every language. See the insertPos clamp in renderGantt.
//
// The Gantt is a fixed two-panel chart with its own minimum width, so (like the
// Projects tab in TestNarrowNoWrapDanish) the no-wrap contract is only asserted
// at widths where it is designed to fit; the no-panic guarantee holds at every
// width down to the smallest terminals.
func TestRenderGanttNarrowNoPanic(t *testing.T) {
	for _, lang := range []language{langEN, langDA} {
		applyLang(string(lang))
		for _, width := range []int{16, 20, 24, 30, 40, 50, 70, 80, 120} {
			m := newTestModel()
			m.termWidth = width
			m.termHeight = 30
			// Tasks whose start/due straddle "today" so the marker is placed.
			tasks := []todo.Todo{
				todo.New("Task one"),
				todo.New("Task two"),
			}
			tasks[0].StartDate = m.frameTime.AddDate(0, 0, -3)
			tasks[0].DueDate = m.frameTime.AddDate(0, 0, 3)
			tasks[1].StartDate = m.frameTime.AddDate(0, 0, -1)
			tasks[1].DueDate = m.frameTime.AddDate(0, 0, 5)

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("lang=%s width=%d: renderGantt panicked: %v", lang, width, r)
					}
				}()
				out := m.renderGantt(tasks)
				if width < 70 {
					return // below the chart's minimum fit width
				}
				for _, line := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(line); w > width {
						t.Errorf("lang=%s width=%d: line %d cells exceeds width: %q", lang, width, w, line)
					}
				}
			}()
		}
	}
	applyLang(string(langEN))
}

// Backlog item ceea44fe: detail page 1 must surface the task's short ID so
// the user can read it (and pass it to the CLI) without leaving the TUI.
func TestRenderDetailPage1ShowsShortID(t *testing.T) {
	task := todo.New("show me my id")
	m := newTestModel()
	m.termWidth = 120
	m.termHeight = 40
	m.Store.add(task)
	m.ensureCache()
	m.cursor = 0
	m.pane = paneDetail

	got := m.renderDetailPage1(&task)
	short := shortID(task.ID)
	if !strings.Contains(got, short) {
		t.Errorf("detail page 1 missing short ID %q in output:\n%s", short, got)
	}
	if !strings.Contains(got, "ID:") {
		t.Errorf("detail page 1 missing 'ID:' label in output:\n%s", got)
	}
}
