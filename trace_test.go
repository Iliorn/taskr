package main

import (
	"os"
	"path/filepath"
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
		if got, want := tracePath(), filepath.Join(home, ".taskr", "trace.log"); got != want {
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
