package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// TestRenderTimelineSubSkipsBareEntries asserts the sub-line is skipped when
// the entry has neither project nor tags (so bare entries stay 1-line), and
// that the project + tags appear in that order otherwise.
func TestRenderTimelineSubSkipsBareEntries(t *testing.T) {
	m := newTestModel()

	bare := dayActivity{title: "bare"}
	if got := m.renderTimelineSub(bare, 80, false); got != "" {
		t.Errorf("bare entry: want empty sub line, got %q", got)
	}

	withProj := dayActivity{title: "x", project: "alpha"}
	if got := m.renderTimelineSub(withProj, 80, false); !strings.Contains(got, "[alpha]") {
		t.Errorf("project-only: missing [alpha] in %q", got)
	}

	withTags := dayActivity{title: "x", tags: []string{"a", "b"}}
	if got := m.renderTimelineSub(withTags, 80, false); !strings.Contains(got, "#a") || !strings.Contains(got, "#b") {
		t.Errorf("tag-only: missing tags in %q", got)
	}

	both := dayActivity{title: "x", project: "alpha", tags: []string{"a"}}
	got := m.renderTimelineSub(both, 80, false)
	pIdx := strings.Index(got, "[alpha]")
	tIdx := strings.Index(got, "#a")
	if pIdx < 0 || tIdx < 0 || pIdx > tIdx {
		t.Errorf("project must precede tags: pIdx=%d tIdx=%d in %q", pIdx, tIdx, got)
	}
}

// TestRenderTimelineSubDropsTagsOnNarrow asserts the sub-line drops tags
// before project when the combined width would overflow innerW. This keeps
// the no-wrap contract from the renderTimelineLines panel honest.
func TestRenderTimelineSubDropsTagsOnNarrow(t *testing.T) {
	m := newTestModel()
	long := dayActivity{
		title:   "x",
		project: "shorty",
		tags:    []string{"a-very-long-tag-name-that-eats-the-width"},
	}
	got := m.renderTimelineSub(long, 16, false) // 16 - 4 indent = 12 avail
	if !strings.Contains(got, "[shorty]") {
		t.Errorf("project should survive narrow width: %q", got)
	}
	if strings.Contains(got, "a-very-long-tag-name") {
		t.Errorf("tag should be dropped when it doesn't fit: %q", got)
	}
}

// TestRenderTimelineSubShowsParent asserts a subtask activity renders a
// "↳ parent" reference even when it has no project or tags (so a done subtask's
// short title gets parent context in the day timeline), and that the parent ref
// is truncated rather than overflowing a narrow panel.
func TestRenderTimelineSubShowsParent(t *testing.T) {
	m := newTestModel()

	sub := dayActivity{title: "fix", completed: true, parentTitle: "Big parent task"}
	got := m.renderTimelineSub(sub, 80, false)
	if !strings.Contains(got, "↳ Big parent task") {
		t.Errorf("want parent reference in sub line, got %q", got)
	}

	// Parent reference must not overflow the inner width once styled
	// (indent + truncated content stays within innerW).
	narrow := dayActivity{title: "fix", parentTitle: "A parent title that is far too long to fit"}
	got = m.renderTimelineSub(narrow, 16, false)
	if w := ansi.StringWidth(got); w > 16 {
		t.Errorf("parent ref overflowed innerW=16: width=%d %q", w, got)
	}
}

// localMidnight returns midnight of today shifted by deltaDays, matching how
// due dates are stored (local midnight, date-only).
func localMidnight(deltaDays int) time.Time {
	now := time.Now()
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return d.AddDate(0, 0, deltaDays)
}

// TestActivitiesForDaySurfacesDueTasks asserts an unfinished task with a future
// DueDate shows up as a "due" activity on its due day, and that a completed task
// with the same due date does not (its deadline is moot).
func TestActivitiesForDaySurfacesDueTasks(t *testing.T) {
	m := newTestModel()
	day := localMidnight(5)

	pending := mkTodo("p", "ship release", todo.Pending)
	pending.DueDate = day
	done := mkTodo("d", "already shipped", todo.Done)
	done.DueDate = day
	m.add(pending)
	m.add(done)
	m.refreshCaches()

	acts := m.activitiesForDay(day)
	if len(acts) != 1 {
		t.Fatalf("activitiesForDay = %d activities, want 1 (done task must not surface)", len(acts))
	}
	if a := acts[0]; !a.due || a.taskID != "p" {
		t.Fatalf("got %+v, want due event for task p", a)
	}

	line := ansi.Strip(m.renderTimelineEntry(acts[0], 0, 80))
	if !strings.Contains(line, "⧗ due") || !strings.Contains(line, "Ship release") {
		t.Fatalf("timeline entry = %q, want due marker + title", line)
	}
}

// TestRenderTimelineEntryOverdue asserts a due event whose day is already past
// renders as "⧗ overdue" rather than "⧗ due".
func TestRenderTimelineEntryOverdue(t *testing.T) {
	m := newTestModel()
	past := localMidnight(-3)
	a := dayActivity{title: "file taxes", start: past, stop: past, due: true}

	line := ansi.Strip(m.renderTimelineEntry(a, 0, 80))
	if !strings.Contains(line, "⧗ overdue") {
		t.Fatalf("timeline entry = %q, want overdue marker", line)
	}
}
