package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/Iliorn/taskr/todo"
)

func TestWeekIndexBucketsByMondayWeeks(t *testing.T) {
	// Thursday 24 Sep 2026; its week starts Monday 21 Sep.
	now := time.Date(2026, 9, 24, 15, 0, 0, 0, time.Local)
	start := startOfWeek(now)
	if want := time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local); !start.Equal(want) {
		t.Fatalf("startOfWeek = %v, want %v", start, want)
	}
	cases := []struct {
		at   time.Time
		want int
	}{
		{time.Date(2026, 9, 24, 9, 0, 0, 0, time.Local), groupWeeks - 1},  // today
		{time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local), groupWeeks - 1},  // Monday midnight
		{time.Date(2026, 9, 20, 23, 0, 0, 0, time.Local), groupWeeks - 2}, // Sunday before
		{time.Date(2026, 9, 14, 0, 0, 0, 0, time.Local), groupWeeks - 2},  // the week before
		{time.Date(2026, 8, 3, 0, 0, 0, 0, time.Local), 0},                // the oldest week kept
		{time.Date(2026, 8, 2, 23, 0, 0, 0, time.Local), -1},              // the Sunday before it
	}
	for _, c := range cases {
		if got := weekIndex(c.at, start); got != c.want {
			t.Errorf("weekIndex(%v) = %d, want %d", c.at.Format("Mon 02 Jan"), got, c.want)
		}
	}
}

func TestFormatSince(t *testing.T) {
	applyLang(string(langEN))
	now := time.Date(2026, 9, 24, 15, 0, 0, 0, time.Local)
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Time{}, "—"},
		{now.Add(-time.Hour), "today"},
		{now.AddDate(0, 0, -1), "1d"},
		{now.AddDate(0, 0, -13), "13d"},
		{now.AddDate(0, 0, -14), "2w"},
		{now.AddDate(0, 0, -69), "9w"},
		{now.AddDate(0, 0, -95), "3mo"},
	}
	for _, c := range cases {
		if got := formatSince(c.at, now); got != c.want {
			t.Errorf("formatSince(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

// One pass builds every group's counts. An untagged subtask belongs to its
// parent and so to no tag group, and next-up is the best-scored open task.
func TestSummarizeGroups(t *testing.T) {
	now := time.Now()
	parent := mkTodo("p", "parent", todo.Pending)
	parent.Tags = []string{"home"}
	child := mkTodo("c", "child", todo.Pending)
	child.ParentID = "p"
	late := mkTodo("l", "late", todo.Pending)
	late.Tags = []string{"home"}
	late.DueDate = now.AddDate(0, 0, -2)
	done := mkTodo("d", "done", todo.Done)
	done.Tags = []string{"home"}
	done.CompletedAt = now
	bare := mkTodo("b", "bare", todo.Pending)
	all := todoPtrs([]todo.Todo{parent, child, late, done, bare})

	score := map[string]float64{"p": 1, "l": 5, "c": 9, "b": 2}
	sums := summarizeGroups(all, tagGroupKeys, func(t *todo.Todo) float64 { return score[t.ID] }, now)

	home := sums["home"]
	if home.open != 2 || home.overdue != 1 || home.done != 1 {
		t.Errorf("home = %d open, %d overdue, %d done; want 2, 1, 1", home.open, home.overdue, home.done)
	}
	if home.nextID != "l" {
		t.Errorf("home next = %q, want the best-scored open task l", home.nextID)
	}
	if home.weekly[groupWeeks-1] != 1 {
		t.Errorf("home weekly = %v, want this week's completion counted", home.weekly)
	}
	if u := sums[untaggedKey]; u == nil || u.open != 1 || u.nextID != "b" {
		t.Errorf("untagged = %+v, want only the top-level bare task", u)
	}
}

func TestVisibleGroupsHidesFinishedButKeepsThePinnedOne(t *testing.T) {
	now := time.Now()
	sums := map[string]*groupSummary{
		"busy":      {open: 3, last: now.Add(-time.Hour)},
		"quiet":     {open: 1, last: now},
		"finished":  {done: 4, last: now},
		untaggedKey: {open: 1},
	}
	all := func(string) bool { return true }

	if got, want := visibleGroups(sums, groupSortOpen, false, "", all), []string{untaggedKey, "busy", "quiet"}; !reflect.DeepEqual(got, want) {
		t.Errorf("by open work = %v, want %v", got, want)
	}
	if got, want := visibleGroups(sums, groupSortRecent, false, "", all), []string{untaggedKey, "quiet", "busy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("by recent = %v, want %v", got, want)
	}
	if got, want := visibleGroups(sums, groupSortName, true, "", all), []string{untaggedKey, "busy", "finished", "quiet"}; !reflect.DeepEqual(got, want) {
		t.Errorf("by name, all shown = %v, want %v", got, want)
	}
	if got := visibleGroups(sums, groupSortName, false, "finished", all); !reflect.DeepEqual(got, []string{untaggedKey, "busy", "finished", "quiet"}) {
		t.Errorf("the pinned group must stay listed, got %v", got)
	}
	if n := hiddenFinishedGroups(sums, false, all); n != 1 {
		t.Errorf("hidden = %d, want 1", n)
	}
}

func TestHasDatedOpenTask(t *testing.T) {
	undated := mkTodo("a", "a", todo.Pending)
	doneDated := mkTodo("b", "b", todo.Done)
	doneDated.DueDate = time.Now()
	if hasDatedOpenTask([]todo.Todo{undated, doneDated}) {
		t.Error("only a done task has a date; the timeline has nothing ahead to draw")
	}
	started := mkTodo("c", "c", todo.Pending)
	started.StartDate = time.Now()
	if !hasDatedOpenTask([]todo.Todo{undated, started}) {
		t.Error("an open task with a start date belongs on a timeline")
	}
}

func TestWeeklySparklineScalesToTheBusiestWeek(t *testing.T) {
	var w [groupWeeks]int
	w[0], w[3], w[groupWeeks-1] = 1, 8, 4
	if got, want := weeklySparkline(w), "▁··█···▄"; got != want {
		t.Errorf("sparkline = %q, want %q", got, want)
	}
}
