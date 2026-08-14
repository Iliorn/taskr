package main

import (
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
