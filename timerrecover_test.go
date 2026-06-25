package main

import (
	"testing"
	"time"

	"taskr/todo"
)

// runningEntry builds a task carrying one running time entry (no stop time).
func taskWithTimer(title string, e todo.TimeEntry) todo.Todo {
	t := todo.New(title)
	t.TimeEntries = []todo.TimeEntry{e}
	return t
}

func TestReconcileStopsAbandonedTimer(t *testing.T) {
	h := openTestDB(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	// Started 5h ago, never heartbeated (CLI/agent orphan).
	task := taskWithTimer("deploy thing", todo.TimeEntry{ID: "te1", StartedAt: now.Add(-5 * time.Hour)})
	saveTodos(t, h, []todo.Todo{task})

	rec, err := reconcileStaleTimers(h, now, 4*time.Hour)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(rec) != 1 || rec[0].Title != "Deploy thing" {
		t.Fatalf("expected 1 recovered timer for the task, got %+v", rec)
	}
	// No heartbeat → stopped at start → ~0 logged (honest: we never saw the work).
	if rec[0].Logged != 0 {
		t.Errorf("orphan with no heartbeat should log ~0, got %s", rec[0].Logged)
	}
	// And it's no longer running in the store.
	got, _ := loadTodosFromDB(h)
	if got[0].TimeEntries[0].IsRunning() {
		t.Errorf("timer should be stopped after recovery")
	}
}

func TestReconcileKeepsFreshlyHeartbeatedTimer(t *testing.T) {
	h := openTestDB(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	// Long-running but a live TUI heartbeated it 1 minute ago → must NOT be touched.
	task := taskWithTimer("focus block", todo.TimeEntry{
		ID: "te1", StartedAt: now.Add(-10 * time.Hour), LastSeen: now.Add(-1 * time.Minute),
	})
	saveTodos(t, h, []todo.Todo{task})

	rec, err := reconcileStaleTimers(h, now, 4*time.Hour)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(rec) != 0 {
		t.Fatalf("a freshly-heartbeated timer must be kept, got %+v", rec)
	}
	got, _ := loadTodosFromDB(h)
	if !got[0].TimeEntries[0].IsRunning() {
		t.Errorf("live timer should still be running")
	}
}

func TestReconcileStopsAtLastSeen(t *testing.T) {
	h := openTestDB(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	// Heartbeated until 5h ago, then went silent (TUI killed): recover at last_seen.
	task := taskWithTimer("write report", todo.TimeEntry{
		ID: "te1", StartedAt: now.Add(-6 * time.Hour), LastSeen: now.Add(-5 * time.Hour),
	})
	saveTodos(t, h, []todo.Todo{task})

	rec, err := reconcileStaleTimers(h, now, 4*time.Hour)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(rec) != 1 {
		t.Fatalf("expected the silent timer to be recovered, got %+v", rec)
	}
	// Stopped at last_seen → logged ≈ last_seen - started = 1h.
	if rec[0].Logged != time.Hour {
		t.Errorf("should log up to last heartbeat (1h), got %s", rec[0].Logged)
	}
}

func TestHeartbeatTouchesOnlyRunningEntries(t *testing.T) {
	h := openTestDB(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	running := taskWithTimer("running", todo.TimeEntry{ID: "run", StartedAt: now.Add(-time.Hour)})
	stopped := taskWithTimer("stopped", todo.TimeEntry{
		ID: "stop", StartedAt: now.Add(-2 * time.Hour), StoppedAt: now.Add(-time.Hour),
	})
	saveTodos(t, h, []todo.Todo{running, stopped})

	if err := heartbeatRunningTimers(h, now); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	got, _ := loadTodosFromDB(h)
	byTitle := map[string]todo.TimeEntry{}
	for _, tk := range got {
		if len(tk.TimeEntries) > 0 {
			byTitle[tk.Title] = tk.TimeEntries[0]
		}
	}
	if byTitle["Running"].LastSeen.IsZero() {
		t.Errorf("running entry should have last_seen stamped")
	}
	if !byTitle["Stopped"].LastSeen.IsZero() {
		t.Errorf("stopped entry must not be heartbeated")
	}
}
