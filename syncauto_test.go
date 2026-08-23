package main

import (
	"errors"
	"strings"
	"testing"
)

// handleSyncDone should toast exactly once when sync crosses from healthy to
// failing, then stay quiet on repeated failures (the header glyph and Settings
// footer carry the ongoing outage). A later success clears the failed flag so
// the next failure toasts again.
func TestHandleSyncDoneFirstFailureTogglesToast(t *testing.T) {
	m := modelWithTasks(t)

	// First failure after a healthy run: toast fires, glyph flips to failed.
	next, cmd := m.handleSyncDone(syncDoneMsg{err: errors.New("dial tcp: timeout")})
	m = next.(model)
	if !m.lastSyncFailed {
		t.Fatal("lastSyncFailed should be set after a failed sync")
	}
	if m.err == "" {
		t.Fatal("first failure should raise a toast on m.err")
	}
	if cmd == nil {
		t.Fatal("first failure should return clearErrAfter to expire the toast")
	}

	// Simulate the toast expiring, then a second consecutive failure.
	m.err = ""
	next, cmd = m.handleSyncDone(syncDoneMsg{err: errors.New("dial tcp: timeout")})
	m = next.(model)
	if m.err != "" {
		t.Fatal("repeated failure should stay quiet on the toast line")
	}
	if cmd != nil {
		t.Fatal("repeated failure should not schedule another toast clear")
	}

	// A success clears the failed flag; the next failure toasts again.
	next, _ = m.handleSyncDone(syncDoneMsg{})
	m = next.(model)
	if m.lastSyncFailed {
		t.Fatal("a successful sync should clear lastSyncFailed")
	}
	next, _ = m.handleSyncDone(syncDoneMsg{err: errors.New("dial tcp: timeout")})
	m = next.(model)
	if m.err == "" {
		t.Fatal("failure after recovery should toast again")
	}
}

// The Settings footer is where a failed sync explains itself, and the
// explanation is in the tail — a fixed-width cut here once left a user reading
// "server returned 500 Internal Server Error" for a week while the sentence
// naming the stale server sat just past the cut.
func TestSyncFailureKeepsTheWholeExplanation(t *testing.T) {
	m := modelWithTasks(t)
	detail := "sync server runs taskr v1.25.0, this device runs v1.33.1 — restart the sync server (it answered 500 Internal Server Error: merge failed: no such table: task_learnings)"

	next, _ := m.handleSyncDone(syncDoneMsg{err: errors.New(detail)})
	m = next.(model)
	if !strings.Contains(m.syncStatus, detail) {
		t.Errorf("syncStatus = %q, want the whole error", m.syncStatus)
	}
}

// A version gap does not fail the sync — which is exactly why the successful
// case has to mention it. An older server against a migrated store keeps
// answering 200 while dropping whatever it has no column for.
func TestSyncSuccessReportsAVersionGap(t *testing.T) {
	m := modelWithTasks(t)
	gap := "sync server runs taskr v1.25.0, this device runs v1.33.1"

	next, _ := m.handleSyncDone(syncDoneMsg{summary: syncSummary{sent: 2, received: 0, versionGap: gap}})
	m = next.(model)
	if !strings.Contains(m.syncStatus, gap) {
		t.Errorf("syncStatus = %q, want the version gap named", m.syncStatus)
	}
	if m.lastSyncFailed {
		t.Error("a version gap must not mark the sync as failed")
	}
}
