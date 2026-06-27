package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// newTagModel returns a model with a known, hermetic set of todos (initialModel
// loads from disk, so we replace its tasks wholesale). initialModel also applies
// the developer's stored language; pin English so these tests, which assert
// English labels, are deterministic regardless of ~/.taskr/settings.json.
func newTagModel(todos ...todo.Todo) model {
	m := newTestModel()
	applyLang(string(langEN))
	for _, td := range todos {
		m.add(td)
	}
	m.termWidth = 80
	m.termHeight = 30
	m.refreshCaches()
	return m
}

func TestRenameTagMerges(t *testing.T) {
	task := todo.New("merge")
	task.Tags = []string{"a", "b"}
	m := newTagModel(task)

	m.renameTagGlobally("a", "b")

	got := m.get(task.ID).Tags
	if len(got) != 1 || got[0] != "b" {
		t.Errorf("after merge rename, tags = %v, want [b]", got)
	}
}

func TestRenameTagMergesMixedCase(t *testing.T) {
	// Legacy data: a capitalized tag stored before normalization, alongside
	// the lowercase form. Merging the two must collapse to one.
	capped := todo.New("capped")
	capped.Tags = []string{"Work"}
	lower := todo.New("lower")
	lower.Tags = []string{"work"}
	m := newTagModel(capped, lower)

	m.renameTagGlobally("Work", "work")

	if got := m.get(capped.ID).Tags; len(got) != 1 || got[0] != "work" {
		t.Errorf("capped task tags = %v, want [work]", got)
	}
	if got := m.get(lower.ID).Tags; len(got) != 1 || got[0] != "work" {
		t.Errorf("lower task tags = %v, want [work]", got)
	}
}

func TestRenameTagNonColliding(t *testing.T) {
	task := todo.New("rename")
	task.Tags = []string{"a", "b"}
	m := newTagModel(task)

	m.renameTagGlobally("a", "C") // also exercises normalization

	got := m.get(task.ID).Tags
	if len(got) != 2 {
		t.Fatalf("tags = %v, want 2 tags", got)
	}
	has := map[string]bool{}
	for _, tg := range got {
		has[tg] = true
	}
	if !has["b"] || !has["c"] {
		t.Errorf("tags = %v, want b and normalized c", got)
	}
}

func TestUntaggedRowAndFilter(t *testing.T) {
	tagged := todo.New("tagged")
	tagged.Tags = []string{"x"}
	m := newTagModel(tagged, todo.New("bare one"), todo.New("bare two"))

	if m.cache.untaggedTotal != 2 {
		t.Fatalf("untaggedTotal = %d, want 2", m.cache.untaggedTotal)
	}

	tags := m.getFilteredTagsForTab()
	if len(tags) == 0 || tags[0] != untaggedKey {
		t.Fatalf("expected untagged row first, got %v", tags)
	}

	m.searchQuery = untaggedKey
	matched := 0
	for _, td := range m.tasks {
		if m.matchesSearch(*td) {
			matched++
		}
	}
	if matched != 2 {
		t.Errorf("untagged filter matched %d tasks, want 2", matched)
	}
}

func TestUntaggedRowHiddenWhenAllTagged(t *testing.T) {
	tagged := todo.New("tagged")
	tagged.Tags = []string{"x"}
	m := newTagModel(tagged)

	for _, tg := range m.getFilteredTagsForTab() {
		if tg == untaggedKey {
			t.Fatal("untagged row should not appear when no tasks are untagged")
		}
	}
}

// TestTagDetailCapsAndOrders verifies the detail pane stays within its height
// cap (no opaque overflow) and surfaces overdue/active tasks before done ones.
func TestTagDetailCapsAndOrders(t *testing.T) {
	mk := func(title string, overdue, done bool) todo.Todo {
		td := todo.New(title)
		switch {
		case done:
			td.Status = todo.Done
		case overdue:
			td.DueDate = time.Now().AddDate(0, 0, -3)
		}
		return td
	}
	var todos []todo.Todo
	for i := 0; i < 30; i++ {
		todos = append(todos, mk(fmt.Sprintf("done-%02d", i), false, true))
	}
	todos = append(todos, mk("zeta-active", false, false))
	todos = append(todos, mk("alpha-overdue", true, false))

	m := newTagModel(todos...)
	m.tab = tabTags
	m.termHeight = 16
	m.refreshCaches()
	m.tagTabCursor = 0 // the untagged row

	lines := m.buildTagDetailLines()
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	got := len(strings.Split(content, "\n"))
	maxVisible := m.termHeight*detailMaxHeightPct/100 - 2
	if got > maxVisible {
		t.Errorf("detail produced %d lines, exceeds cap %d:\n%s", got, maxVisible, content)
	}
	if !strings.Contains(content, "more") {
		t.Errorf("expected an 'and N more' notice, got:\n%s", content)
	}
	// Overdue/active come first; with a tight budget the done-NN tasks must not
	// crowd them out.
	if !strings.Contains(content, "Alpha-overdue") {
		t.Errorf("overdue task should be shown, missing from:\n%s", content)
	}
	if strings.Contains(content, "Done-00") {
		t.Errorf("done tasks should sort after overdue/active, but a done task displaced them:\n%s", content)
	}
}

func TestTagStatsAgeAndTracked(t *testing.T) {
	a := todo.New("old open")
	a.Tags = []string{"work"}
	a.CreatedAt = time.Now().AddDate(0, 0, -12)
	a.TimeEntries = []todo.TimeEntry{{
		StartedAt: time.Now().Add(-90 * time.Minute),
		StoppedAt: time.Now(),
	}}
	b := todo.New("recent open")
	b.Tags = []string{"work"}
	b.CreatedAt = time.Now().AddDate(0, 0, -2)
	c := todo.New("done")
	c.Tags = []string{"work"}
	c.Status = todo.Done
	c.CreatedAt = time.Now().AddDate(0, 0, -100) // done → excluded from avg age

	stats := computeTagStats([]todo.Todo{a, b, c})
	s := stats["work"]
	if s.openCount != 2 {
		t.Fatalf("openCount = %d, want 2 (done task excluded)", s.openCount)
	}
	avgDays := (s.ageSum / time.Duration(s.openCount)).Hours() / 24
	if avgDays < 6.5 || avgDays > 7.5 {
		t.Errorf("avg open age = %.2f days, want ~7 (12+2)/2", avgDays)
	}
	if s.tracked < 89*time.Minute || s.tracked > 91*time.Minute {
		t.Errorf("tracked = %v, want ~90m", s.tracked)
	}

	m := newTagModel(a, b, c)
	m.termWidth = 120
	m.tab = tabTags
	m.refreshCaches()
	out := m.renderTagList()
	for _, want := range []string{"avg age", "⏱ time spent", " · "} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in tag row, got:\n%s", want, out)
		}
	}
}

// TestTagsTabNarrowNoWrap renders the Tags tab (list + detail) across widths and
// for both a real tag and the untagged row, asserting nothing wraps.
func TestTagsTabNarrowNoWrap(t *testing.T) {
	for _, width := range []int{40, 50, 60, 80} {
		tagged := todo.New("A long task title that would overflow a slim detail pane easily")
		tagged.Tags = []string{"alpha"}
		tagged.Project = "someproject"
		tagged.DueDate = time.Now().AddDate(0, 0, -2)

		m := newTagModel(tagged, todo.New("An untagged task with a similarly long title to test wrapping"))
		m.termWidth = width
		m.tab = tabTags
		m.refreshCaches()

		for _, cursor := range []int{0, 1} { // untagged row, then #alpha
			m.tagTabCursor = cursor
			out := m.View()
			for n, line := range strings.Split(out, "\n") {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("width=%d cursor=%d: line %d is %d cells: %q", width, cursor, n, w, line)
				}
			}
		}
	}
}
