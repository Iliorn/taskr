package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"taskr/todo"
)

func TestSyncStateRoundTrip(t *testing.T) {
	if err := writeSyncState(syncSummary{sent: 7, received: 9, conflicts: 2}); err != nil {
		t.Fatalf("write: %v", err)
	}
	st, ok, err := readSyncState()
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if st.Sent != 7 || st.Received != 9 || st.Conflicts != 2 {
		t.Errorf("state = %+v, want sent7/recv9/conf2", st)
	}
	if time.Since(st.LastSync) > time.Minute || st.LastSync.IsZero() {
		t.Errorf("LastSync = %v, want ~now", st.LastSync)
	}
}

func TestReadSyncStateAbsent(t *testing.T) {
	if err := os.Remove(syncStatePath()); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove: %v", err)
	}
	_, ok, err := readSyncState()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ok {
		t.Error("want ok=false when no state file exists")
	}
}

func TestPrintSyncStatus(t *testing.T) {
	// No state → "never".
	if err := os.Remove(syncStatePath()); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove: %v", err)
	}
	out := captureStdout(t, func() { printSyncStatus(syncConfig{}) })
	if !strings.Contains(out, "last sync: never") {
		t.Errorf("no-state status = %q, want it to say never", out)
	}
	if !strings.Contains(out, "(none configured)") {
		t.Errorf("no-url status = %q, want it to note no server", out)
	}
	// With state + a URL → shows the server and a summary line.
	if err := writeSyncState(syncSummary{sent: 3, received: 4, conflicts: 1}); err != nil {
		t.Fatalf("write: %v", err)
	}
	out = captureStdout(t, func() { printSyncStatus(syncConfig{URL: "http://example:8765"}) })
	if !strings.Contains(out, "http://example:8765") {
		t.Errorf("status = %q, want the configured URL", out)
	}
	if !strings.Contains(out, "sent 3, received 4, 1 conflict") {
		t.Errorf("status = %q, want the sync summary", out)
	}
}

// A local write landing while the sync round trip is in flight must survive
// the apply. Before the re-merge fix, runClientSync saved the server's merged
// set blind: a comment added after the client loaded its push set was missing
// from the response, so saveChildren tombstoned it as "vanished" — and that
// deletion then propagated. The test's server handler plays the concurrent
// writer: it mutates the client DB before responding.
func TestSyncConcurrentLocalWriteSurvives(t *testing.T) {
	ch := openTestDB(t)
	a := todo.New("task")
	a.ID = "a"
	saveTodos(t, ch, []todo.Todo{a})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sync", func(w http.ResponseWriter, r *http.Request) {
		var req syncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode push: %v", err)
		}
		// The concurrent local write: a comment added mid-flight, so it is
		// absent from both the push and the response.
		withComment := a
		withComment.AddComment("added mid-flight")
		saveTodos(t, ch, []todo.Todo{withComment})
		if err := json.NewEncoder(w).Encode(syncResponse(req)); err != nil {
			t.Errorf("encode response: %v", err)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	if _, err := runClientSync(ch, syncConfig{URL: ts.URL, Token: "tok"}, 5*time.Second); err != nil {
		t.Fatalf("client sync: %v", err)
	}

	live, err := loadTodosFromDB(ch)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, task := range live {
		if task.ID == "a" {
			if len(task.Comments) != 1 {
				t.Fatalf("comment added during the round trip was lost: %d live comments, want 1", len(task.Comments))
			}
			return
		}
	}
	t.Fatal("task a missing after sync")
}

func TestDroppedLocalEditsDeletionVsEdit(t *testing.T) {
	base := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	// A task the client still has live.
	live := todo.New("task")
	live.ModifiedAt = base

	// Case 1: another device deleted it AFTER our last edit → plain deletion,
	// not a conflict.
	delAfter := live
	delAfter.Deleted = true
	delAfter.DeletedAt = base.Add(time.Hour)
	if d := droppedLocalEdits([]todo.Todo{live}, []todo.Todo{delAfter}); len(d) != 0 {
		t.Errorf("remote deletion of an unedited live task should not be a conflict, got %d", len(d))
	}

	// Case 2: we edited it AFTER the deletion timestamp → a genuine edit that
	// lost to a delete → conflict.
	edited := live
	edited.ModifiedAt = base.Add(2 * time.Hour)
	delBefore := live
	delBefore.Deleted = true
	delBefore.DeletedAt = base.Add(time.Hour)
	if d := droppedLocalEdits([]todo.Todo{edited}, []todo.Todo{delBefore}); len(d) != 1 {
		t.Errorf("a local edit newer than the deletion should be a conflict, got %d", len(d))
	}

	// Case 3: both sides live, scalar fields differ → conflict (unchanged behavior).
	server := live
	server.Title = "server wording"
	if d := droppedLocalEdits([]todo.Todo{live}, []todo.Todo{server}); len(d) != 1 {
		t.Errorf("a contested live edit should still be a conflict, got %d", len(d))
	}
}

// TestSyncSaveIgnoresEnvOverlay: `sync --save` persists file values + explicit
// flags, never the TASKR_SYNC_URL/TOKEN env overlay — a one-off env var must
// not get baked into sync.json where it would silently outlive the shell.
func TestSyncSaveIgnoresEnvOverlay(t *testing.T) {
	t.Setenv("TASKR_SYNC_URL", "http://env-only:1")
	t.Setenv("TASKR_SYNC_TOKEN", "env-only-token")
	t.Cleanup(func() { _ = os.Remove(syncConfigPath()) })

	// URL from a flag, token only from env. The sync itself fails (dead
	// server, exit 1) but --save applies before the network round trip.
	if rc := cliSync([]string{"--url", "http://127.0.0.1:1", "--save", "--quiet"}); rc != 1 {
		t.Fatalf("sync against a dead server should fail with 1, got %d", rc)
	}

	saved := loadSyncConfigFile()
	if saved.URL != "http://127.0.0.1:1" {
		t.Errorf("--save should persist the flag URL, got %q", saved.URL)
	}
	if saved.Token != "" {
		t.Errorf("env token must not be baked into sync.json, got %q", saved.Token)
	}
	// The runtime view still sees the env overlay.
	if run := loadSyncConfig(); run.Token != "env-only-token" {
		t.Errorf("runtime config should overlay the env token, got %q", run.Token)
	}
}

// The insecure-URL warning must fire only for plain http to genuinely public
// hosts — private transports (Tailscale, LAN, loopback) and https stay quiet,
// so the warning keeps signal when it does appear.
func TestInsecureSyncURLWarning(t *testing.T) {
	quiet := []string{
		"",
		"https://tasks.example.com",
		"http://127.0.0.1:8765",
		"http://localhost:8765",
		"http://100.122.178.43:8765", // Tailscale CGNAT
		"http://192.168.1.10:8765",   // RFC1918
		"http://10.0.0.5:8765",
		"http://hoth.tail1234.ts.net:8765",
	}
	for _, u := range quiet {
		if w := insecureSyncURLWarning(u); w != "" {
			t.Errorf("unexpected warning for %q: %s", u, w)
		}
	}
	loud := []string{
		"http://203.0.113.7:8765",     // public IP
		"http://tasks.example.com:80", // public hostname
		"http://100.32.1.1:8765",      // 100.x but below CGNAT range
	}
	for _, u := range loud {
		if w := insecureSyncURLWarning(u); w == "" {
			t.Errorf("expected warning for %q, got none", u)
		}
	}
}
