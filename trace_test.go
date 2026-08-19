package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Tracing must be invisible unless asked for: no file, no channel, no cost.
func TestTraceOffByDefault(t *testing.T) {
	t.Setenv("TASKR_TRACE", "")
	if got := tracePath(); got != "" {
		t.Errorf("tracePath() = %q with TASKR_TRACE unset, want off", got)
	}
	stop := startTrace()
	defer stop()
	if traceCh != nil {
		t.Error("startTrace opened a channel with tracing off")
	}
	traceFrame("key j", time.Millisecond, time.Millisecond) // must not panic
}

// wantStateFile spells out where the state directory lands per platform,
// rather than asking paths.go and comparing its answer to itself. The XDG
// layout was hard-coded here, so this test failed on macOS — where state lives
// in Application Support — for a reason that had nothing to do with tracing.
func wantStateFile(home, name string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "Local", "taskr", name)
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "taskr", name)
	default:
		return filepath.Join(home, ".local", "state", "taskr", name)
	}
}

func TestTracePathForms(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	for _, off := range []string{"", "0", "false", "off", "  "} {
		t.Setenv("TASKR_TRACE", off)
		if got := tracePath(); got != "" {
			t.Errorf("TASKR_TRACE=%q → %q, want off", off, got)
		}
	}
	for _, on := range []string{"1", "true", "on"} {
		t.Setenv("TASKR_TRACE", on)
		if got, want := tracePath(), wantStateFile(home, "trace.log"); got != want {
			t.Errorf("TASKR_TRACE=%q → %q, want %q", on, got, want)
		}
	}
	t.Setenv("TASKR_TRACE", "/tmp/somewhere.log")
	if got := tracePath(); got != "/tmp/somewhere.log" {
		t.Errorf("an explicit path became %q", got)
	}
}

// A frame lands in the log with its timings, and stop() flushes what is buffered
// — a trace that only appears after a clean quit is useless for a hang.
func TestTraceWritesFrames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.log")
	t.Setenv("TASKR_TRACE", path)

	stop := startTrace()
	if traceCh == nil {
		t.Fatal("tracing did not start")
	}
	traceFrame("key down", 2*time.Millisecond, 3*time.Millisecond)
	traceFrame("main.reloadedMsg", 15*time.Millisecond, time.Millisecond)
	stop()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	body := string(data)
	for _, want := range []string{"key down", "reloadedMsg", "2.000", "15.000", "gap_ms",
		"summary over 2 frames", "update  p50", "p95", "max"} {
		if !strings.Contains(body, want) {
			t.Errorf("trace missing %q:\n%s", want, body)
		}
	}
	// Appending to an existing trace keeps the earlier session.
	stop2 := startTrace()
	traceFrame("key up", time.Millisecond, time.Millisecond)
	stop2()
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(again), "key down") || !strings.Contains(string(again), "key up") {
		t.Error("a second session replaced the first instead of appending")
	}
}

// Dropping is the contract when the writer falls behind: tracing must never
// block the event loop.
func TestTraceDoesNotBlockWhenFull(t *testing.T) {
	prev := traceCh
	defer func() { traceCh = prev }()
	traceCh = make(chan traceEntry) // unbuffered, nobody reading
	done := make(chan struct{})
	go func() {
		traceFrame("key j", time.Millisecond, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("traceFrame blocked on a full channel")
	}
}

// TASKR_NO_WATCH is the escape hatch for "input feels laggy": it must actually
// leave the watcher unstarted, since the point is to remove the app's only
// continuous OS interaction from the picture.
func TestNoWatchEnvDisablesTheWatcher(t *testing.T) {
	setTestHome(t, t.TempDir())
	for _, v := range []string{"1", "true", "yes"} {
		t.Setenv("TASKR_NO_WATCH", v)
		var m model
		startModelWatcher(&m)
		if m.watcher != nil {
			t.Errorf("TASKR_NO_WATCH=%q still started a watcher", v)
		}
	}
	for _, v := range []string{"", "0", "false"} {
		t.Setenv("TASKR_NO_WATCH", v)
		var m model
		startModelWatcher(&m)
		if m.watcher == nil {
			t.Errorf("TASKR_NO_WATCH=%q disabled live reload, want it on", v)
		}
		m.closeWatcher()
	}
}

// The doctor names the input path because on Windows there are two, and which
// one is in use is the difference between a keystroke waiting on a 16ms poll
// and one that doesn't. Off Windows there is only the one.
func TestInputPathIsNamedAndSwitchable(t *testing.T) {
	t.Setenv("TASKR_WIN_CONSOLE_INPUT", "")
	if consoleInputForced() {
		t.Error("the console-input override defaulted to on")
	}
	if inputPathName() == "" {
		t.Error("the doctor would print an empty input path")
	}
	for _, v := range []string{"1", "true", "yes"} {
		t.Setenv("TASKR_WIN_CONSOLE_INPUT", v)
		if !consoleInputForced() {
			t.Errorf("TASKR_WIN_CONSOLE_INPUT=%q did not take", v)
		}
	}
	for _, v := range []string{"", "0", "false"} {
		t.Setenv("TASKR_WIN_CONSOLE_INPUT", v)
		if consoleInputForced() {
			t.Errorf("TASKR_WIN_CONSOLE_INPUT=%q should mean off", v)
		}
	}
	if runtime.GOOS != "windows" && len(inputProgramOptions()) != 0 {
		t.Error("a non-Windows build should not change the input path")
	}
}
