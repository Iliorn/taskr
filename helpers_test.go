package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// ── clamp ─────────────────────────────────────────────────────────────────────

func TestClamp(t *testing.T) {
	tests := []struct {
		name          string
		val, min, max int
		want          int
	}{
		{"in range", 5, 0, 10, 5},
		{"at min", 0, 0, 10, 0},
		{"at max", 10, 0, 10, 10},
		{"below min", -5, 0, 10, 0},
		{"above max", 99, 0, 10, 10},
		{"all equal", 5, 5, 5, 5},
		{"negative range", -3, -10, -1, -3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clamp(tt.val, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("clamp(%d, %d, %d) = %d, want %d",
					tt.val, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

// ── truncate ──────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"short unchanged", "hi", 10, "hi"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello(…)"},
		{"max 3 no ellipsis", "hello", 3, "hel"},
		{"max 4 with ellipsis", "hello", 4, "h(…)"},
		{"empty string", "", 5, ""},
		{"max 1", "hello", 1, "h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q",
					tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// ── padRight ──────────────────────────────────────────────────────────────────

func TestPadRight(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  string
	}{
		{"needs padding", "hi", 5, "hi   "},
		{"exact width", "hello", 5, "hello"},
		{"longer than width", "hello world", 5, "hello"},
		{"empty string", "", 3, "   "},
		{"width zero", "hi", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padRight(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("padRight(%q, %d) = %q, want %q",
					tt.input, tt.width, got, tt.want)
			}
		})
	}
}

// ── wrapText ──────────────────────────────────────────────────────────────────

func TestWrapText(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		width     int
		wantLines int
	}{
		{"short text", "hello", 80, 1},
		{"exact width", "hello", 5, 1},
		{"wraps once", "hello world", 6, 2},
		{"long no spaces", "abcdefghij", 4, 3},
		{"empty string", "", 10, 0},
		{"width 0 uses 1", "hi", 0, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.input, tt.width)
			if len(got) != tt.wantLines {
				t.Errorf("wrapText(%q, %d) = %d lines %v, want %d lines",
					tt.input, tt.width, len(got), got, tt.wantLines)
			}
		})
	}
}

// ── commentLineCount ──────────────────────────────────────────────────────────

func TestCommentLineCount(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		available int
		want      int
	}{
		{"empty", "", 80, 1},
		{"short text", "hello", 80, 1},
		{"exact fit", "hello", 5, 1},
		{"wraps to 2", "hello world", 6, 2},
		{"wraps to 3", "this is a longer sentence here", 10, 3},
		{"single char available", "hello", 1, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commentLineCount(tt.text, tt.available)
			if got != tt.want {
				t.Errorf("commentLineCount(%q, %d) = %d, want %d",
					tt.text, tt.available, got, tt.want)
			}
		})
	}
}

// ── formatDuration ────────────────────────────────────────────────────────────

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name  string
		input time.Duration
		want  string
	}{
		{"seconds", 30 * time.Second, "30s"},
		{"minutes", 5 * time.Minute, "5m"},
		{"hours and minutes", 2*time.Hour + 30*time.Minute, "2h 30m"},
		{"over a day", 25 * time.Hour, "1d 1h"},
		{"multiple days", 50 * time.Hour, "2d 2h"},
		{"exactly one minute", 60 * time.Second, "1m"},
		{"exactly one hour", 60 * time.Minute, "1h 0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.input)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q",
					tt.input, got, tt.want)
			}
		})
	}
}

// ── formatDueShort ────────────────────────────────────────────────────────────

func TestFormatDueShort(t *testing.T) {
	applyLang(string(langEN))
	now := time.Date(2026, 7, 3, 8, 30, 0, 0, time.Local)
	cases := []struct {
		days int
		want string
	}{
		{0, "today"},
		{1, "1d"},
		{6, "6d"},
		{28, "28d"},
		{-2, "-2d"},
	}
	for _, c := range cases {
		due := now.AddDate(0, 0, c.days)
		if got := formatDueShort(due, now); got != c.want {
			t.Errorf("%+d days: got %q, want %q", c.days, got, c.want)
		}
	}
	// Far dates in the same year show only dd-mm (no year) to save space.
	farSameYear := now.AddDate(0, 0, 40) // still 2026
	if got := formatDueShort(farSameYear, now); got != farSameYear.Format("02-01") {
		t.Errorf("far date same year: got %q, want %q", got, farSameYear.Format("02-01"))
	}
	// Far dates in a different year include the 2-digit year.
	farOtherYear := time.Date(2027, 3, 15, 0, 0, 0, 0, time.Local)
	if got := formatDueShort(farOtherYear, now); got != farOtherYear.Format("02-01-06") {
		t.Errorf("far date other year: got %q, want %q", got, farOtherYear.Format("02-01-06"))
	}
	// Same applies to far overdue dates in a past year.
	pastOtherYear := time.Date(2025, 1, 10, 0, 0, 0, 0, time.Local)
	if got := formatDueShort(pastOtherYear, now); got != pastOtherYear.Format("02-01-06") {
		t.Errorf("far past date other year: got %q, want %q", got, pastOtherYear.Format("02-01-06"))
	}
}

// ── parseQuickAdd ─────────────────────────────────────────────────────────────

func TestParseQuickAdd(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTitle string
		wantTags  []string
		wantProj  string
		wantPrio  todo.Priority
		wantSize  todo.Size
		wantDue   bool
	}{
		{
			name:      "plain title",
			input:     "Buy groceries",
			wantTitle: "Buy groceries",
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "with tags",
			input:     "Fix bug #backend #urgent",
			wantTitle: "Fix bug",
			wantTags:  []string{"backend", "urgent"},
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "priority high",
			input:     "Deploy release p:high",
			wantTitle: "Deploy release",
			wantPrio:  todo.PriorityHigh,
		},
		{
			name:      "priority medium short",
			input:     "Review PR p:m",
			wantTitle: "Review PR",
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "priority low explicit",
			input:     "Chill task p:low",
			wantTitle: "Chill task",
			wantPrio:  todo.PriorityLow,
		},
		{
			name:      "with project",
			input:     "Write docs @taskr",
			wantTitle: "Write docs",
			wantProj:  "taskr",
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "with due date",
			input:     "Submit report due:tomorrow",
			wantTitle: "Submit report",
			wantDue:   true,
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "all together",
			input:     "Refactor cache #performance @taskr p:high due:+3d",
			wantTitle: "Refactor cache",
			wantTags:  []string{"performance"},
			wantProj:  "taskr",
			wantPrio:  todo.PriorityHigh,
			wantDue:   true,
		},
		{
			name:      "invalid due becomes title",
			input:     "Check due:gibberish",
			wantTitle: "Check due:gibberish",
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "invalid priority becomes title",
			input:     "Task p:extreme",
			wantTitle: "Task p:extreme",
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "empty tag ignored",
			input:     "Test # thing",
			wantTitle: "Test thing",
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "empty input",
			input:     "",
			wantTitle: "",
			wantPrio:  todo.PriorityMedium,
		},
		{
			name:      "size small",
			input:     "Sweep porch size:s",
			wantTitle: "Sweep porch",
			wantPrio:  todo.PriorityMedium,
			wantSize:  todo.SizeSmall,
		},
		{
			name:      "size shortcut s:l",
			input:     "Rewrite scheduler s:l",
			wantTitle: "Rewrite scheduler",
			wantPrio:  todo.PriorityMedium,
			wantSize:  todo.SizeLarge,
		},
		{
			name:      "size large word",
			input:     "Rewrite scheduler size:large",
			wantTitle: "Rewrite scheduler",
			wantPrio:  todo.PriorityMedium,
			wantSize:  todo.SizeLarge,
		},
		{
			name:      "invalid size becomes title",
			input:     "Task size:huge",
			wantTitle: "Task size:huge",
			wantPrio:  todo.PriorityMedium,
			wantSize:  todo.SizeMedium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseQuickAdd(tt.input)

			if got.title != tt.wantTitle {
				t.Errorf("title = %q, want %q", got.title, tt.wantTitle)
			}
			if got.priority != tt.wantPrio {
				t.Errorf("priority = %v, want %v", got.priority, tt.wantPrio)
			}
			if got.size != tt.wantSize {
				t.Errorf("size = %v, want %v", got.size, tt.wantSize)
			}
			if tt.wantProj != "" && got.project != tt.wantProj {
				t.Errorf("project = %q, want %q", got.project, tt.wantProj)
			}
			if tt.wantDue && got.dueDate.IsZero() {
				t.Error("expected due date to be set, got zero")
			}
			if !tt.wantDue && !got.dueDate.IsZero() {
				t.Errorf("expected no due date, got %v", got.dueDate)
			}
			if len(tt.wantTags) > 0 {
				if len(got.tags) != len(tt.wantTags) {
					t.Fatalf("tags = %v, want %v", got.tags, tt.wantTags)
				}
				for i, tag := range tt.wantTags {
					if got.tags[i] != tag {
						t.Errorf("tag[%d] = %q, want %q", i, got.tags[i], tag)
					}
				}
			}
		})
	}
}

func TestParseQuickAddRecurrence(t *testing.T) {
	cases := []struct {
		input     string
		wantTitle string
		wantRecur string
	}{
		{"Water plants r:daily", "Water plants", "daily"},
		{"Standup r:weekly @team", "Standup", "weekly"},
		{"Rent recur:monthly", "Rent", "monthly"},
		{"Birthday card r:yearly", "Birthday card", "yearly"},
		{"Run r:weekdays", "Run", "weekdays"},
		{"Backup r:3d", "Backup", "every:3d"},
		{"Bogus r:every-other-tuesday", "Bogus r:every-other-tuesday", ""},
		{"Plain task", "Plain task", ""},
	}
	for _, c := range cases {
		got := parseQuickAdd(c.input)
		if got.title != c.wantTitle {
			t.Errorf("input %q: title = %q, want %q", c.input, got.title, c.wantTitle)
		}
		if got.recurrence != c.wantRecur {
			t.Errorf("input %q: recurrence = %q, want %q", c.input, got.recurrence, c.wantRecur)
		}
	}
}

func TestParseQuickAddDeps(t *testing.T) {
	cases := []struct {
		input     string
		wantTitle string
		wantDeps  []string
	}{
		{"Deploy dep:^", "Deploy", []string{"^"}},
		{"Deploy dep:fd8502d1", "Deploy", []string{"fd8502d1"}},
		{"Deploy dep:a1b2 dep:^ #ops", "Deploy", []string{"a1b2", "^"}},
		{"Keep dep: in title", "Keep dep: in title", nil},
		{"No deps here", "No deps here", nil},
	}
	for _, c := range cases {
		got := parseQuickAdd(c.input)
		if got.title != c.wantTitle {
			t.Errorf("input %q: title = %q, want %q", c.input, got.title, c.wantTitle)
		}
		if len(got.deps) != len(c.wantDeps) {
			t.Errorf("input %q: deps = %v, want %v", c.input, got.deps, c.wantDeps)
			continue
		}
		for i := range c.wantDeps {
			if got.deps[i] != c.wantDeps[i] {
				t.Errorf("input %q: deps[%d] = %q, want %q", c.input, i, got.deps[i], c.wantDeps[i])
			}
		}
	}
}

// ── nameColWidth ──────────────────────────────────────────────────────────────

func TestNameColWidth(t *testing.T) {
	tests := []struct {
		name      string
		termWidth int
	}{
		{"narrow terminal", 60},
		{"medium terminal", 120},
		{"wide terminal", 200},
		{"very narrow", 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nameColWidth(tt.termWidth)
			if got < nameColMinWidth {
				t.Errorf("nameColWidth(%d) = %d, below minimum %d",
					tt.termWidth, got, nameColMinWidth)
			}
			if got > nameColMaxWidth {
				t.Errorf("nameColWidth(%d) = %d, above maximum %d",
					tt.termWidth, got, nameColMaxWidth)
			}
			// Within the clamp range it is a straight percentage of the width.
			if want := tt.termWidth * nameColWidthPct / 100; want >= nameColMinWidth && want <= nameColMaxWidth && got != want {
				t.Errorf("nameColWidth(%d) = %d, want %d", tt.termWidth, got, want)
			}
		})
	}
}

// ── taskListCols title growth ───────────────────────────────────────────────

func TestTaskListColsTitleGrowsOnWideTerminal(t *testing.T) {
	// shownColsW reconstructs the fixed-column budget from a laid-out listCols so
	// the no-overflow check below doesn't depend on taskListCols internals.
	shownColsW := func(c listCols) int {
		w := 0
		if c.showSize {
			w += sizeColW
		}
		if c.showDue {
			w += dueColW
		}
		if c.showLast {
			w += scoreColW
		}
		if c.showProject {
			w += projectColW
		}
		return w
	}

	tests := []struct {
		name       string
		termWidth  int
		contentMax int
		tagsMax    int
	}{
		{"wide + long titles", 200, 120, 0},
		{"wide + short titles", 200, 18, 0},
		{"medium + long titles", 120, 90, 0},
		{"narrow + long titles", 60, 90, 0},
		{"wide + long titles + tags", 200, 120, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := taskListCols(tt.termWidth, false, tt.contentMax, tt.tagsMax, true)

			// Never wider than the longest title needs (+gap), but at least the
			// header label — growth must not produce an empty padded column.
			floor := len([]rune(tr("Active tasks")))
			want := tt.contentMax + 4
			if want < floor {
				want = floor
			}
			if c.titleW > want {
				t.Errorf("titleW = %d, exceeds content need %d", c.titleW, want)
			}

			// No-wrap contract: title + fixed cells + the widest row's tags
			// (a leading space + tagsMax) must fit the inner width.
			const fixed = 6
			inner := tt.termWidth - 8
			tagsReserve := 0
			if tt.tagsMax > 0 {
				tagsReserve = 1 + tt.tagsMax
			}
			if total := c.titleW + fixed + shownColsW(c) + tagsReserve; total > inner {
				t.Errorf("titleW=%d + fixed + cols + tags = %d overflows inner %d",
					c.titleW, total, inner)
			}
		})
	}

	// On a wide terminal a long title must grow past the flat nameColMaxWidth
	// cap, filling slack the old hard cap left empty.
	if c := taskListCols(200, false, 120, 0, true); c.titleW <= nameColMaxWidth {
		t.Errorf("wide terminal titleW = %d, want > flat cap %d (should absorb slack)",
			c.titleW, nameColMaxWidth)
	}
	// But a short title still hugs its content — no needless sprawl.
	if c := taskListCols(200, false, 18, 0, true); c.titleW != 18+4 {
		t.Errorf("short title titleW = %d, want %d (hug content)", c.titleW, 18+4)
	}
	// Reserving tag room must shrink the grown title vs. the no-tags case, so
	// the tags column survives.
	noTags := taskListCols(200, false, 120, 0, true)
	withTags := taskListCols(200, false, 120, 40, true)
	if !(withTags.titleW < noTags.titleW) {
		t.Errorf("titleW with tags reserve = %d, want < no-reserve %d",
			withTags.titleW, noTags.titleW)
	}
}

// ── Due-column collapse ───────────────────────────────────────────────────────

// TestDueColumnCollapseWhenNoDueDates verifies that when no visible task has a
// due date the Due column is omitted entirely (showDue == false) and the header
// carries no "Due" label either.
func TestDueColumnCollapseWhenNoDueDates(t *testing.T) {
	// hasDue=false: column must be absent regardless of terminal width.
	c := taskListCols(120, false, 20, 0, false)
	if c.showDue {
		t.Error("showDue should be false when hasDue=false")
	}

	// hasDue=true: column must appear on a reasonably wide terminal.
	c = taskListCols(120, false, 20, 0, true)
	if !c.showDue {
		t.Error("showDue should be true when hasDue=true and there is room")
	}
}

// TestDueColumnAppearsWhenAtLeastOneDue verifies the end-to-end path: a list
// with no due dates shows no "Due" column header; adding one due date causes
// it to appear. Uses renderListHeader directly so we don't have to parse a
// full View() that may include "Due date:" in the detail panel.
func TestDueColumnAppearsWhenAtLeastOneDue(t *testing.T) {
	var b strings.Builder

	// No due dates: header must not contain the "Due" column label.
	noDueCols := taskListCols(120, false, 20, 0, false)
	b.Reset()
	renderListHeader(&b, 120, false, noDueCols, "")
	hdrNoDue := b.String()
	// The header label is "Due" padded to dueColW; when absent it must not appear.
	if strings.Contains(hdrNoDue, tr("Due")) {
		t.Errorf("no due dates: list header should not show 'Due', got: %q", hdrNoDue)
	}

	// With a due date: header must now contain "Due".
	withDueCols := taskListCols(120, false, 20, 0, true)
	b.Reset()
	renderListHeader(&b, 120, false, withDueCols, "")
	hdrWithDue := b.String()
	if !strings.Contains(hdrWithDue, tr("Due")) {
		t.Errorf("with due dates: list header should show 'Due', got: %q", hdrWithDue)
	}
}

// TestDueColumnNoWrapContractWithCollapse verifies the no-wrap contract holds
// when the Due column is collapsed: every rendered line must fit within the
// terminal width.
func TestDueColumnNoWrapContractWithCollapse(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120} {
		m := modelWithTasks(t, todo.New("alpha"), todo.New("beta"), todo.New("gamma"))
		m.termWidth = width
		m.termHeight = 30
		m.refreshCaches()
		out := m.View()
		for n, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width=%d: line %d is %d cells wide (Due collapsed): %q",
					width, n, w, line)
			}
		}
	}
}

// ── computeTagStats ───────────────────────────────────────────────────────────

func TestComputeTagStats(t *testing.T) {
	todos := []todo.Todo{
		{Tags: []string{"work", "urgent"}, Status: todo.Pending},
		{Tags: []string{"work"}, Status: todo.Done},
		{Tags: []string{"personal"}, Status: todo.Pending},
	}

	stats := computeTagStats(todos)

	if stats["work"].total != 2 {
		t.Errorf("work total = %d, want 2", stats["work"].total)
	}
	if stats["work"].done != 1 {
		t.Errorf("work done = %d, want 1", stats["work"].done)
	}
	if stats["urgent"].total != 1 {
		t.Errorf("urgent total = %d, want 1", stats["urgent"].total)
	}
	if stats["urgent"].done != 0 {
		t.Errorf("urgent done = %d, want 0", stats["urgent"].done)
	}
	if stats["personal"].total != 1 {
		t.Errorf("personal total = %d, want 1", stats["personal"].total)
	}
	if stats["personal"].done != 0 {
		t.Errorf("personal done = %d, want 0", stats["personal"].done)
	}

	// Empty list
	empty := computeTagStats([]todo.Todo{})
	if len(empty) != 0 {
		t.Errorf("expected empty stats, got %d entries", len(empty))
	}

	// No tags
	noTags := computeTagStats([]todo.Todo{{Title: "No tags"}})
	if len(noTags) != 0 {
		t.Errorf("expected empty stats for tagless todos, got %d", len(noTags))
	}
}

// ── copyTodos ─────────────────────────────────────────────────────────────────

func TestCopyTodos(t *testing.T) {
	original := []todo.Todo{
		{
			ID:           "task-1",
			Title:        "Original",
			Tags:         []string{"work", "urgent"},
			Dependencies: []string{"dep-1", "dep-2"},
			Comments:     []todo.Comment{{ID: "c1", Text: "hello"}},
			Learnings:    []todo.Learning{{ID: "l1", Text: "learned"}},
		},
	}

	cp := copyTodos(original)

	// Verify values copied correctly
	if cp[0].ID != "task-1" {
		t.Errorf("ID = %q, want %q", cp[0].ID, "task-1")
	}
	if cp[0].Title != "Original" {
		t.Errorf("Title = %q, want %q", cp[0].Title, "Original")
	}
	if len(cp[0].Tags) != 2 {
		t.Fatalf("Tags len = %d, want 2", len(cp[0].Tags))
	}

	// Modify original — copy should be unaffected
	original[0].Title = "Modified"
	original[0].Tags[0] = "changed"
	original[0].Dependencies[0] = "changed-dep"
	original[0].Comments[0].Text = "changed-comment"
	original[0].Learnings[0].Text = "changed-learning"

	if cp[0].Title != "Original" {
		t.Error("copy title was affected by original mutation")
	}
	if cp[0].Tags[0] != "work" {
		t.Error("copy tags were affected by original mutation")
	}
	if cp[0].Dependencies[0] != "dep-1" {
		t.Error("copy dependencies were affected by original mutation")
	}
	if cp[0].Comments[0].Text != "hello" {
		t.Error("copy comments were affected by original mutation")
	}
	if cp[0].Learnings[0].Text != "learned" {
		t.Error("copy learnings text was affected by original mutation")
	}
}

func TestCopyTodosEmpty(t *testing.T) {
	cp := copyTodos([]todo.Todo{})
	if len(cp) != 0 {
		t.Errorf("expected empty copy, got %d", len(cp))
	}
}

func TestCopyTodosNilSlices(t *testing.T) {
	// Task with no slices — should not panic
	original := []todo.Todo{
		{ID: "bare", Title: "Bare task"},
	}
	cp := copyTodos(original)
	if cp[0].ID != "bare" {
		t.Errorf("ID = %q, want %q", cp[0].ID, "bare")
	}
}

// parseManualEntry backs both the TUI 'T' shortcut and `taskr log`: bare
// durations anchor the entry to END now (the "I just spent 45m" reading),
// clock ranges are literal on today.
func TestParseManualEntry(t *testing.T) {
	now := time.Date(2026, 7, 3, 14, 0, 0, 0, time.Local)

	start, stop, err := parseManualEntry("45m", now)
	if err != nil {
		t.Fatalf("duration form: %v", err)
	}
	if !stop.Equal(now) || !start.Equal(now.Add(-45*time.Minute)) {
		t.Errorf("45m → [%v, %v], want [now-45m, now]", start, stop)
	}

	start, stop, err = parseManualEntry("10:00-11:30", now)
	if err != nil {
		t.Fatalf("range form: %v", err)
	}
	wantStart := time.Date(2026, 7, 3, 10, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || stop.Sub(start) != 90*time.Minute {
		t.Errorf("range → [%v, %v], want literal 10:00 + 90m", start, stop)
	}

	if _, _, err := parseManualEntry("banana", now); err == nil {
		t.Error("expected error for junk input")
	}
}
