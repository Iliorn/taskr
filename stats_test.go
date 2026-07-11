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

// When a single bucket exceeds chartH, the histogram doubles its scale so a
// busy day collapses to half-height rather than instantly capping with `+`.
// Odd counts render with a half-block (▄) on top so 9 stays visibly shorter
// than 10; only past 2×chartH does `+` re-appear.
func TestStatsHistogramScalesBeforeOverflow(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
	mk := func(n int) []todo.Todo {
		var todos []todo.Todo
		for i := 0; i < n; i++ {
			td := todo.New(fmt.Sprintf("done %d", i))
			td.Status = todo.Done
			td.CompletedAt = today
			todos = append(todos, td)
		}
		return todos
	}
	render := func(todos []todo.Todo) string {
		m := newTagModel(todos...)
		m.termWidth = 100
		m.termHeight = 30
		m.tab = tabStats
		m.statsRange = statsRange7Days
		m.refreshCaches()
		return m.renderStatsDetail()
	}

	// 9 tasks at scale=2 → 4 paired-half rows (▀) plus a ▄ cap on the 5th
	// row (the 9th task), no `+`.
	out := render(mk(9))
	if !strings.Contains(out, "▄") {
		t.Errorf("9 tasks should render ▄ for the odd top task, got:\n%s", out)
	}
	if !strings.Contains(out, "▀") {
		t.Errorf("9 tasks should render ▀ for stacked-half-block rows, got:\n%s", out)
	}
	if strings.Contains(out, "+") {
		t.Errorf("scale=2 should fit 9 tasks without `+`, got:\n%s", out)
	}

	// 10 tasks: 5 full ▀-rows (top/bottom both filled), still no `+`.
	out10 := render(mk(10))
	if strings.Contains(out10, "+") {
		t.Errorf("10 tasks should fit without `+`, got:\n%s", out10)
	}
	if strings.Contains(out10, "▄") {
		t.Errorf("10 tasks is even, no orphan ▄, got:\n%s", out10)
	}

	// 12 tasks: scale=2 still overflows chartH=5, so `+` reappears.
	out2 := render(mk(12))
	if !strings.Contains(out2, "+") {
		t.Errorf("12 tasks should overflow scale=2, expected `+`, got:\n%s", out2)
	}
}

// Regression: tasks stored UTC (as SQLite does) and rendered with a frameTime
// in a +N timezone were being skipped from today's bucket because the bucket
// day was reconstructed from CompletedAt's UTC date parts, putting "midnight"
// past local midnight and tripping the `d.After(today)` guard.
func TestStatsHistogramTimezoneBucket(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("Asia/Tokyo tzdata not available: " + err.Error())
	}
	td := todo.New("done in Tokyo")
	td.Status = todo.Done
	td.CompletedAt = time.Date(2026, 6, 18, 10, 0, 0, 0, tokyo).UTC()

	m := newTagModel(td)
	m.termWidth = 100
	m.termHeight = 44
	m.tab = tabStats
	m.statsRange = statsRange7Days
	m.refreshCaches()
	m.frameTime = time.Date(2026, 6, 18, 12, 0, 0, 0, tokyo)

	out := m.renderStatsDetail()
	if strings.Contains(out, "No completions in this range") {
		t.Fatalf("today's completion (stored UTC) dropped from 7-day window:\n%s", out)
	}
	if !strings.Contains(out, "1 done") {
		t.Errorf("expected '1 done' in header, got:\n%s", out)
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

// TestStatsRespectsSearchFilter pins the Stats-tab scoping contract: an active
// search query (set via / on the Tasks, Projects, or Stats tab) narrows every
// stat block to the matching top-level tasks.
func TestStatsRespectsSearchFilter(t *testing.T) {
	now := time.Now()
	mk := func(title string, tags ...string) todo.Todo {
		td := todo.New(title)
		td.CreatedAt = now.AddDate(0, 0, -1)
		td.Tags = tags
		return td
	}
	tagged1 := mk("tagged one", "work")
	tagged2 := mk("tagged two", "work")
	plain1 := mk("plain one")
	plain2 := mk("plain two")
	doneTagged := mk("tagged done", "work")
	doneTagged.Status = todo.Done
	doneTagged.CompletedAt = now

	m := newTagModel(tagged1, tagged2, plain1, plain2, doneTagged)
	m.termWidth = 120
	m.tab = tabStats
	m.frameTime = now

	activeTotal := regexp.MustCompile(`Active total\s+(\d+)`)

	plainOut := ansi.Strip(m.renderStatsList())
	if got := activeTotal.FindStringSubmatch(plainOut); got == nil || got[1] != "4" {
		t.Fatalf("unfiltered Active total = %v, want 4\n%s", got, plainOut)
	}

	m.searchQuery = "#work"
	filteredOut := ansi.Strip(m.renderStatsList())
	if got := activeTotal.FindStringSubmatch(filteredOut); got == nil || got[1] != "2" {
		t.Fatalf("filtered Active total = %v, want 2\n%s", got, filteredOut)
	}
	if !regexp.MustCompile(`Today\s+1`).MatchString(filteredOut) {
		t.Errorf("filtered velocity should still count the tagged completion:\n%s", filteredOut)
	}
}
