package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

func statsTodos() []todo.Todo {
	now := time.Now()
	var todos []todo.Todo
	for i := 0; i < 6; i++ {
		td := todo.New(fmt.Sprintf("active task %d with a longish title", i))
		td.CreatedAt = now.AddDate(0, 0, -i*2)
		if i == 0 {
			td.Priority = todo.PriorityHigh
			td.DueDate = now.AddDate(0, 0, -2)
		}
		todos = append(todos, td)
	}
	d := todo.New("done one")
	d.Status = todo.Done
	d.CompletedAt = now
	d.CreatedAt = now.AddDate(0, 0, -3)
	return append(todos, d)
}

// TestStatsTwoColumnLayout checks the stats page collapses into two columns on a
// wide terminal (shorter) and falls back to one column when narrow.
func TestStatsTwoColumnLayout(t *testing.T) {
	todos := statsTodos()

	wide := newTagModel(todos...)
	wide.termWidth = 120
	wide.tab = tabStats
	wide.refreshCaches()
	wideLines := strings.Split(strings.TrimRight(wide.renderStatsList(), "\n"), "\n")

	narrow := newTagModel(todos...)
	narrow.termWidth = 70
	narrow.tab = tabStats
	narrow.refreshCaches()
	narrowLines := strings.Split(strings.TrimRight(narrow.renderStatsList(), "\n"), "\n")

	if len(wideLines) >= len(narrowLines) {
		t.Errorf("two-column layout (%d lines) should be shorter than single column (%d)",
			len(wideLines), len(narrowLines))
	}

	pairFound := false
	for _, l := range wideLines {
		if strings.Contains(l, "Workload") && strings.Contains(l, "Throughput") {
			pairFound = true
		}
	}
	if !pairFound {
		t.Errorf("expected a side-by-side section row in two-column layout, got:\n%s",
			strings.Join(wideLines, "\n"))
	}

	// Both Flow windows are present (symmetric 3-per-side layout).
	joined := strings.Join(wideLines, "\n")
	for _, want := range []string{"Flow (last 7 days)", "Flow (last 30 days)", "vs prior 30d"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in stats page, got:\n%s", want, joined)
		}
	}

	// Column content may not exceed the inner width, or it would wrap/clip and
	// lose data. (The divider is a deliberate full-bleed rule, excluded here.)
	for _, l := range wideLines {
		if strings.Contains(l, "─") {
			continue
		}
		if w := ansi.StringWidth(l); w > wide.termWidth-8 {
			t.Errorf("stats line exceeds inner width %d (%d cells): %q", wide.termWidth-8, w, l)
		}
	}
}

// TestStatsNarrowNoWrap renders the whole Stats page across widths and asserts no
// line wraps the terminal — covering both the two-column and fallback paths.
func TestStatsNarrowNoWrap(t *testing.T) {
	todos := statsTodos()
	for _, w := range []int{60, 84, 100, 140} {
		m := newTagModel(todos...)
		m.termWidth = w
		m.termHeight = 40
		m.tab = tabStats
		m.refreshCaches()
		for n, line := range strings.Split(m.View(), "\n") {
			if cw := ansi.StringWidth(line); cw > w {
				t.Errorf("width=%d: line %d is %d cells wide: %q", w, n, cw, line)
			}
		}
	}
}
