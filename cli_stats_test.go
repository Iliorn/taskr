package main

import (
	"testing"
	"time"

	"taskr/todo"
)

// TestComputeStatsBuckets covers the four pending buckets + the two done
// buckets simultaneously. Uses a fixed `now` so the date math is
// deterministic regardless of when CI runs.
func TestComputeStatsBuckets(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	today := startOfDay(now)

	build := func(title string, st todo.Status, due time.Time, completedAt time.Time, parentID string) todo.Todo {
		x := todo.New(title)
		x.Status = st
		x.DueDate = due
		x.CompletedAt = completedAt
		x.ParentID = parentID
		return x
	}

	todos := []todo.Todo{
		build("overdue 1", todo.Pending, today.AddDate(0, 0, -5), time.Time{}, ""),
		build("overdue 2", todo.Pending, today.AddDate(0, 0, -1), time.Time{}, ""),
		build("due today", todo.Pending, today.Add(8*time.Hour), time.Time{}, ""),
		build("due this week", todo.Pending, today.AddDate(0, 0, 3), time.Time{}, ""),
		build("far future, no bucket", todo.Pending, today.AddDate(0, 0, 30), time.Time{}, ""),
		build("no date pending", todo.Pending, time.Time{}, time.Time{}, ""),
		build("done today", todo.Done, time.Time{}, today.Add(2*time.Hour), ""),
		build("done yesterday", todo.Done, time.Time{}, today.AddDate(0, 0, -1), ""),
		// Subtasks should be ignored by the top-level filter.
		build("subtask of something", todo.Pending, today, time.Time{}, "some-parent-id"),
	}

	got := computeStats(todos, now)

	// Active = every non-subtask pending row, regardless of due bucket:
	//   2 overdue + 1 due today + 1 due this week + 1 far-future + 1 no-date
	want := statsSummary{
		Active:       6,
		Overdue:      2,
		DueToday:     1,
		DueThisWeek:  1,
		DoneToday:    1,
		DoneThisWeek: 2,
	}
	if got != want {
		t.Errorf("computeStats() = %+v\nwant %+v", got, want)
	}
}

// TestComputeStatsIgnoresSubtasksAndCompletedWithoutTimestamp ensures the
// filter is robust to malformed input — a Done task with a zero CompletedAt
// shouldn't count toward "done today".
func TestComputeStatsIgnoresEdgeCases(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	todos := []todo.Todo{
		{ParentID: "p", Status: todo.Pending},           // subtask → skipped
		{Status: todo.Done, CompletedAt: time.Time{}},   // done with no timestamp → skipped from done buckets
		{Status: todo.Done, CompletedAt: time.Time{}},   // ditto
	}
	got := computeStats(todos, now)
	if got.Active != 0 || got.DoneToday != 0 || got.DoneThisWeek != 0 {
		t.Errorf("expected all zero, got %+v", got)
	}
}
