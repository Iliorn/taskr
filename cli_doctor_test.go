package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The doctor has to say whether live reload is actually off, because "did the
// variable reach the program" is otherwise indistinguishable from "the watcher
// wasn't the problem" when someone is bisecting input lag.
func TestDoctorReportsLiveReloadState(t *testing.T) {
	setTestHome(t, t.TempDir())

	t.Setenv("TASKR_NO_WATCH", "")
	if got := diagnoseLiveReload(); got.Value != "on" {
		t.Errorf("live reload = %q with the variable unset, want on", got.Value)
	}
	t.Setenv("TASKR_NO_WATCH", "1")
	got := diagnoseLiveReload()
	if !strings.Contains(got.Value, "off") || !strings.Contains(got.Value, "TASKR_NO_WATCH") {
		t.Errorf("live reload = %q, want it to name the variable that turned it off", got.Value)
	}
}

// sync.log lives in the state directory, which is not the data directory on a
// fresh install; the doctor has to look where the sync writes it.
func TestDoctorFindsTheSyncLogInTheStateDir(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("TASKR_SYNC_URL", "http://127.0.0.1:8765")
	t.Setenv("TASKR_SYNC_TOKEN", "a-test-token-that-is-long-enough")
	if err := os.MkdirAll(filepath.Dir(syncLogPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(syncLogPath(), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range diagnoseSync() {
		if d.Name == "sync.log" {
			return
		}
	}
	t.Errorf("the doctor did not report the sync.log at %s", syncLogPath())
}
