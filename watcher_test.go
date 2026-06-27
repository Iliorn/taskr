package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// startWatcherOrSkip starts a watcher, skipping the test when the host can't
// initialize inotify (e.g. fs.inotify.max_user_instances exhausted on a busy
// box) — that's an environment limit, not a watcher bug, so a hard failure
// would just be noise. Returns the cleanup func to defer.
func startWatcherOrSkip(t *testing.T, state *watcherState, dir string) func() {
	t.Helper()
	cleanup, err := startWatcher(state, dir)
	if err != nil {
		if strings.Contains(err.Error(), "inotify") {
			t.Skipf("inotify unavailable on this host: %v", err)
		}
		t.Fatalf("startWatcher: %v", err)
	}
	return cleanup
}

// TestShouldReloadNowWhileTypingDefers covers the modal-mode rule: a fs
// event arriving while the user is mid-edit must not clobber the input.
// shouldReloadNow returns false AND records pendingExternalReload so the
// Update wrapper can drain it on mode exit.
func TestShouldReloadNowWhileTypingDefers(t *testing.T) {
	ws := newWatcherState()
	now := time.Now()

	if ws.shouldReloadNow(now, modeInput) {
		t.Error("modeInput should defer reload, got true")
	}
	if !ws.drainPending() {
		t.Error("pending reload flag should be set after deferred event")
	}
	if ws.drainPending() {
		t.Error("drainPending should clear the flag — second call must return false")
	}
}

// TestShouldReloadNowSuppressesOurOwnSave is the self-write loop guard:
// when a fs event arrives within selfWriteWindow of our own Save, it's
// our own write coming back through fsnotify. Skip without queueing.
func TestShouldReloadNowSuppressesOurOwnSave(t *testing.T) {
	ws := newWatcherState()
	ws.recordSelfSave()

	// Within the suppression window — should skip.
	if ws.shouldReloadNow(time.Now(), modeNormal) {
		t.Error("event within selfWriteWindow should be suppressed")
	}
	if ws.drainPending() {
		t.Error("self-write suppression must NOT queue a pending reload")
	}

	// Outside the window — should reload.
	wayLater := time.Now().Add(watcherSelfWriteWindow + 100*time.Millisecond)
	if !ws.shouldReloadNow(wayLater, modeNormal) {
		t.Error("event outside selfWriteWindow should trigger reload")
	}
}

// TestShouldReloadNowHappyPath confirms the simple case: idle TUI, external
// write, no recent save → reload immediately.
func TestShouldReloadNowHappyPath(t *testing.T) {
	ws := newWatcherState()
	// lastSelfSaveAt is the zero value → way in the past → not suppressed.
	if !ws.shouldReloadNow(time.Now(), modeNormal) {
		t.Error("idle TUI + external write should reload immediately")
	}
}

// TestStartWatcherFiresOnFileChange exercises the real fsnotify path: spin
// up a watcher on a temp dir, write to tasks.db, and confirm a dbChangedMsg
// arrives on the channel within a generous timeout. This validates the
// directory-watch + filename-filter + debounce wiring end-to-end without
// running the full TUI.
func TestStartWatcherFiresOnFileChange(t *testing.T) {
	dir := t.TempDir()
	state := newWatcherState()

	defer startWatcherOrSkip(t, state, dir)()

	// Write to tasks.db — this is the file the watcher is filtered to.
	dbPath := dir + "/tasks.db"
	if err := os.WriteFile(dbPath, []byte("hello"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Wait up to 2s for the debounce (200ms) + a generous buffer for slow CI.
	select {
	case <-state.ch:
		// got it
	case <-time.After(2 * time.Second):
		t.Fatal("expected dbChangedMsg within 2s of writing tasks.db, got nothing")
	}
}

// TestStartWatcherIgnoresUnrelatedFiles confirms the filename filter: a
// write to some-other-file.txt in the watched directory must not fire the
// channel, only writes to tasks.db / tasks.db-wal do.
func TestStartWatcherIgnoresUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	state := newWatcherState()

	defer startWatcherOrSkip(t, state, dir)()

	if err := os.WriteFile(dir+"/not-relevant.txt", []byte("noise"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 500ms is well past the debounce — if nothing arrives by then, the
	// filter works.
	select {
	case <-state.ch:
		t.Fatal("watcher fired on an unrelated file write")
	case <-time.After(500 * time.Millisecond):
	}
}

// TestRecordSelfSaveAdvancesWindow verifies recordSelfSave bumps the
// suppression deadline so back-to-back saves both stay protected.
func TestRecordSelfSaveAdvancesWindow(t *testing.T) {
	ws := newWatcherState()
	ws.recordSelfSave()
	firstStamp := ws.lastSelfSaveAt

	time.Sleep(5 * time.Millisecond)
	ws.recordSelfSave()
	if !ws.lastSelfSaveAt.After(firstStamp) {
		t.Errorf("second recordSelfSave didn't advance the stamp: first=%v second=%v",
			firstStamp, ws.lastSelfSaveAt)
	}
}
