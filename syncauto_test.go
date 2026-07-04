package main

import (
	"errors"
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
