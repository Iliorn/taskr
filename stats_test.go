package main

import (
	"fmt"
	"regexp"
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

// TestStatsThreeColumns verifies the stats list uses three columns on a wide
// terminal, keeping the page short while showing both Flow blocks in full.
func TestStatsThreeColumns(t *testing.T) {
	todos := statsTodos()

	wide := newTagModel(todos...)
	wide.termWidth = 140
	wide.tab = tabStats
	wide.refreshCaches()
	wideLines := strings.Split(strings.TrimRight(wide.renderStatsList(), "\n"), "\n")

	two := newTagModel(todos...)
	two.termWidth = 100
	two.tab = tabStats
	two.refreshCaches()
	twoLines := strings.Split(strings.TrimRight(two.renderStatsList(), "\n"), "\n")

	if len(wideLines) >= len(twoLines) {
		t.Errorf("three columns (%d lines) should be shorter than two (%d)", len(wideLines), len(twoLines))
	}

	threeWide := false
	for _, l := range wideLines {
		if strings.Contains(l, "Workload") && strings.Contains(l, "Flow (last 7 days)") {
			threeWide = true
		}
	}
	if !threeWide {
		t.Errorf("expected a 3-column header row at width 140:\n%s", strings.Join(wideLines, "\n"))
	}

	joined := strings.Join(wideLines, "\n")
	if !strings.Contains(joined, "Flow (last 30 days)") || !strings.Contains(joined, "vs prior 30d") {
		t.Errorf("Flow (last 30 days) must remain fully present in 3-col:\n%s", joined)
	}
	for _, l := range wideLines {
		if strings.Contains(l, "─") {
			continue
		}
		if w := ansi.StringWidth(l); w > wide.termWidth-8 {
			t.Errorf("3-col line exceeds inner width %d (%d): %q", wide.termWidth-8, w, l)
		}
	}
}

// TestStatsHistogram checks the activity histogram for every range: the right
// title shows, nothing exceeds the pane's inner width (which would wrap), and
// the empty state is handled.
func TestStatsHistogram(t *testing.T) {
	now := time.Now()
	var todos []todo.Todo
	for i := 0; i < 40; i++ {
		td := todo.New(fmt.Sprintf("done %d", i))
		td.Status = todo.Done
		td.CompletedAt = now.AddDate(0, 0, -(i * 4)) // spread over ~160 days
		todos = append(todos, td)
	}

	ranges := map[statsRangeMode]string{
		statsRange7Days:   "Last 7 days",
		statsRange30Days:  "Last 30 days",
		statsRange6Months: "Last 26 weeks",
	}
	for _, w := range []int{40, 60, 100, 140} {
		for r, title := range ranges {
			m := newTagModel(todos...)
			m.termWidth = w
			m.termHeight = 44
			m.tab = tabStats
			m.statsRange = r
			m.refreshCaches()
			out := m.renderStatsDetail()
			if !strings.Contains(out, title) {
				t.Errorf("w=%d range=%d: missing title %q in:\n%s", w, r, title, out)
			}
			for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
				if cw := ansi.StringWidth(line); cw > w-8 {
					t.Errorf("w=%d range=%d: line %d cells exceeds inner width %d: %q",
						w, r, cw, w-8, line)
				}
			}
		}
	}

	// The 6-month view labels the axis with week numbers (wNN).
	weekLabel := regexp.MustCompile(`w\d`)
	wide := newTagModel(todos...)
	wide.termWidth = 120
	wide.tab = tabStats
	wide.statsRange = statsRange6Months
	wide.refreshCaches()
	wideOut := wide.renderStatsDetail()
	if !weekLabel.MatchString(wideOut) {
		t.Errorf("expected week-number axis labels in 6-month view, got:\n%s", wideOut)
	}
	// On a wide terminal the legend fits in the header's top-right.
	if !strings.Contains(wideOut, "1 block = 1 completed task") {
		t.Errorf("expected legend in header, got:\n%s", wideOut)
	}

	// The 30-day view draws dotted separators between weeks.
	d30 := newTagModel(todos...)
	d30.termWidth = 120
	d30.tab = tabStats
	d30.statsRange = statsRange30Days
	d30.refreshCaches()
	if out := d30.renderStatsDetail(); !strings.Contains(out, "·") {
		t.Errorf("expected dotted week separators in 30-day view, got:\n%s", out)
	}

	// Empty state.
	empty := newTagModel()
	empty.termWidth = 100
	empty.tab = tabStats
	empty.refreshCaches()
	if out := empty.renderStatsDetail(); !strings.Contains(out, "No completions") {
		t.Errorf("expected empty-state message, got:\n%s", out)
	}
}

// TestStatsHistogramOverflow checks that a bar with more tasks than the height
// limit is capped with a "+" overflow marker.
func TestStatsHistogramOverflow(t *testing.T) {
	now := time.Now()
	var todos []todo.Todo
	for i := 0; i < 30; i++ { // 30 completions, all today — well past any limit
		td := todo.New("x")
		td.Status = todo.Done
		td.CompletedAt = now
		todos = append(todos, td)
	}
	m := newTagModel(todos...)
	m.termWidth = 100
	m.termHeight = 30
	m.tab = tabStats
	m.statsRange = statsRange7Days
	m.refreshCaches()
	if out := m.renderStatsDetail(); !strings.Contains(out, "+") {
		t.Errorf("expected a '+' overflow marker for an over-limit bar, got:\n%s", out)
	}
}

// TestStatsWeekdayLabels checks the 7-day histogram spells weekday names out
// when wide and shrinks them when narrow.
func TestStatsWeekdayLabels(t *testing.T) {
	now := time.Now()
	var todos []todo.Todo
	for d := 0; d < 7; d++ {
		td := todo.New("x")
		td.Status = todo.Done
		td.CompletedAt = now.AddDate(0, 0, -d)
		todos = append(todos, td)
	}
	weekdays := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

	wide := newTagModel(todos...)
	wide.termWidth = 110
	wide.termHeight = 40
	wide.tab = tabStats
	wide.statsRange = statsRange7Days
	wide.refreshCaches()
	outWide := wide.renderStatsDetail()
	full := false
	for _, d := range weekdays {
		if strings.Contains(outWide, d) {
			full = true
		}
	}
	if !full {
		t.Errorf("expected full weekday names on a wide 7-day histogram:\n%s", outWide)
	}

	narrow := newTagModel(todos...)
	narrow.termWidth = 44
	narrow.termHeight = 40
	narrow.tab = tabStats
	narrow.statsRange = statsRange7Days
	narrow.refreshCaches()
	outNarrow := narrow.renderStatsDetail()
	for _, d := range weekdays {
		if strings.Contains(outNarrow, d) {
			t.Errorf("did not expect full weekday name %q on a narrow histogram:\n%s", d, outNarrow)
		}
	}
}

// TestStatsRangeCycles confirms Enter on the Stats tab advances the range and
// wraps back to the start.
func TestStatsRangeCycles(t *testing.T) {
	m := newTagModel()
	m.tab = tabStats
	want := []statsRangeMode{statsRange30Days, statsRange6Months, statsRange7Days}
	for i, exp := range want {
		nm, _ := m.handleListEnter()
		m = nm.(model)
		if m.statsRange != exp {
			t.Fatalf("after %d Enter(s): statsRange = %d, want %d", i+1, m.statsRange, exp)
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
