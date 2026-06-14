package main

import (
	"testing"
	"time"

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
		{"needs truncation", "hello world", 8, "hello..."},
		{"max 3 no ellipsis", "hello", 3, "hel"},
		{"max 4 with ellipsis", "hello", 4, "h..."},
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

// ── parseQuickAdd ─────────────────────────────────────────────────────────────

func TestParseQuickAdd(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTitle string
		wantTags  []string
		wantProj  string
		wantPrio  todo.Priority
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

// ── titleColWidth ─────────────────────────────────────────────────────────────

func TestTitleColWidth(t *testing.T) {
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
			got := titleColWidth(tt.termWidth)
			if got < minTitleColWidth {
				t.Errorf("titleColWidth(%d) = %d, below minimum %d",
					tt.termWidth, got, minTitleColWidth)
			}
			// Only check max when terminal is wide enough for it to make sense
			max := tt.termWidth * titleColMaxWidthPct / 100
			if max >= minTitleColWidth && got > max {
				t.Errorf("titleColWidth(%d) = %d, above max %d",
					tt.termWidth, got, max)
			}
		})
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
			Learnings:    []todo.Learning{{ID: "l1", Text: "learned", Tags: []string{"go"}}},
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
	original[0].Learnings[0].Tags[0] = "changed-tag"

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
	if cp[0].Learnings[0].Tags[0] != "go" {
		t.Error("copy learning tags were affected by original mutation")
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
