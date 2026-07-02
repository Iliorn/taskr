package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"taskr/todo"
)

// syncclient.go is the `taskr sync` side: it pushes the local task set (including
// tombstones) to a `taskr serve` endpoint, applies the authoritative merged set
// that comes back, and logs any local edit that lost a conflict so it stays
// recoverable. It is fail-soft — a network/server error leaves the local store
// untouched.

type syncConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	// AutoSync gates the automatic syncs (TUI launch/periodic/exit and after CLI
	// mutations). nil means "default on" — set "auto_sync": false in sync.json to
	// keep sync manual-only.
	AutoSync *bool `json:"auto_sync,omitempty"`

	// Server side: this machine acting as a sync hub. ServerOn runs the endpoint
	// in-process while the TUI is open (the always-on case still uses the
	// headless `taskr serve`). ServerListen/ServerToken are its bind address and
	// the token clients must present.
	ServerListen string `json:"server_listen,omitempty"`
	ServerToken  string `json:"server_token,omitempty"`
	ServerOn     bool   `json:"server_on,omitempty"`
}

func (c syncConfig) ready() bool { return c.URL != "" && c.Token != "" }

// autoSyncEnabled reports whether automatic syncs should fire: only when a
// server is configured, and not explicitly disabled.
func autoSyncEnabled(c syncConfig) bool {
	return c.ready() && (c.AutoSync == nil || *c.AutoSync)
}

// maybeAutoSyncCLI runs one fail-soft sync after a mutating CLI command, so a
// shell edit (taskr add/done/…) propagates without the TUI being open. Silent
// on failure — a network blip must not fail the command the user actually ran.
func maybeAutoSyncCLI() {
	cfg := loadSyncConfig()
	if !autoSyncEnabled(cfg) {
		return
	}
	if err := openStore(); err != nil {
		return
	}
	_, _ = runClientSync(db, cfg, 10*time.Second)
}

func syncConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "sync.json")
}

// loadSyncConfig reads ~/.taskr/sync.json, then overlays TASKR_SYNC_URL /
// TASKR_SYNC_TOKEN when set. Either source may be absent.
func loadSyncConfig() syncConfig {
	var c syncConfig
	if b, err := os.ReadFile(syncConfigPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if v := os.Getenv("TASKR_SYNC_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("TASKR_SYNC_TOKEN"); v != "" {
		c.Token = v
	}
	return c
}

func saveSyncConfig(c syncConfig) error {
	if err := ensureStorageDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(syncConfigPath(), b, 0600)
}

// syncState is the outcome of the last successful sync, persisted so `taskr sync
// --status` can report it without touching the network. Only successful syncs
// update it, so a failed attempt never erases the last-known-good timestamp.
type syncState struct {
	LastSync  time.Time `json:"last_sync"`
	Sent      int       `json:"sent"`
	Received  int       `json:"received"`
	Conflicts int       `json:"conflicts"`
}

func syncStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "sync-state.json")
}

// writeSyncState records the outcome of a successful sync. Best-effort — callers
// ignore its error, since failing to note status must never fail the sync.
func writeSyncState(sum syncSummary) error {
	if err := ensureStorageDir(); err != nil {
		return err
	}
	st := syncState{
		LastSync:  time.Now().UTC(),
		Sent:      sum.sent,
		Received:  sum.received,
		Conflicts: sum.conflicts,
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(syncStatePath(), b, 0600)
}

// readSyncState returns the last recorded sync outcome. ok is false (with a nil
// error) when no sync has ever been recorded.
func readSyncState() (st syncState, ok bool, err error) {
	b, err := os.ReadFile(syncStatePath())
	if os.IsNotExist(err) {
		return syncState{}, false, nil
	}
	if err != nil {
		return syncState{}, false, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return syncState{}, false, err
	}
	return st, true, nil
}

// printSyncStatus reports the configured server and the last successful sync,
// reading only local state (no network). Returns a process exit code.
func printSyncStatus(cfg syncConfig) int {
	if cfg.URL != "" {
		fmt.Printf("server: %s\n", cfg.URL)
	} else {
		fmt.Println("server: (none configured)")
	}
	st, ok, err := readSyncState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr sync: read state: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Println("last sync: never")
		return 0
	}
	fmt.Printf("last sync: %s (%s ago) — sent %d, received %d, %d conflict(s)\n",
		st.LastSync.Local().Format("2006-01-02 15:04"), shortDur(time.Since(st.LastSync)),
		st.Sent, st.Received, st.Conflicts)
	return 0
}

func cliSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	url := fs.String("url", "", "sync server URL, e.g. http://100.x.y.z:8765 (or set TASKR_SYNC_URL)")
	token := fs.String("token", "", "shared bearer token (or set TASKR_SYNC_TOKEN)")
	save := fs.Bool("save", false, "persist --url/--token to ~/.taskr/sync.json for future syncs")
	quiet := fs.Bool("quiet", false, "print nothing on success")
	status := fs.Bool("status", false, "print the last sync time/result and exit (local only, no network)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg := loadSyncConfig()
	if *url != "" {
		cfg.URL = *url
	}
	if *token != "" {
		cfg.Token = *token
	}
	// --status is a local read: report and exit before any config-required or
	// network path, so it works even when no server is configured yet.
	if *status {
		return printSyncStatus(cfg)
	}
	if *save {
		if err := saveSyncConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "taskr sync: save config: %v\n", err)
			return 1
		}
	}
	if !cfg.ready() {
		fmt.Fprintln(os.Stderr, "taskr sync: missing url/token — pass --url/--token (optionally --save), or set TASKR_SYNC_URL/TASKR_SYNC_TOKEN")
		return 2
	}
	if err := openStore(); err != nil {
		fmt.Fprintf(os.Stderr, "taskr sync: open store: %v\n", err)
		return 1
	}
	sum, err := runClientSync(db, cfg, 30*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr sync: %v\n", err)
		return 1
	}
	if !*quiet {
		hint := ""
		if sum.conflicts > 0 {
			hint = " (dropped versions logged to ~/.taskr/sync.log)"
		}
		fmt.Printf("synced: sent %d, received %d, %d conflict(s) resolved%s\n",
			sum.sent, sum.received, sum.conflicts, hint)
	}
	return 0
}

type syncSummary struct {
	sent, received, conflicts int
}

// runClientSync pushes the local full task set (including tombstones) to the
// server, persists the merged set it returns, and logs any local edit that lost
// a conflict. On error nothing is applied locally, so the local store is left
// untouched.
func runClientSync(h *sql.DB, cfg syncConfig, timeout time.Duration) (syncSummary, error) {
	local, err := loadTodosForSync(h)
	if err != nil {
		return syncSummary{}, err
	}
	merged, err := postSync(cfg, local, timeout)
	if err != nil {
		return syncSummary{}, err
	}
	// Record dropped local edits before we overwrite, for the recovery log.
	dropped := droppedLocalEdits(local, merged)
	if err := logDroppedEdits(dropped); err != nil {
		fmt.Fprintf(os.Stderr, "taskr sync: warning: could not write sync log: %v\n", err)
	}
	// The round trip can take seconds, and anything written locally meanwhile
	// (the TUI's debounced save, another CLI command) is missing from `merged`.
	// Saving that blind would overwrite those rows — worse, saveChildren would
	// tombstone a just-added comment as "vanished", and that deletion would
	// then propagate to every device. Re-merge against the store as it is NOW
	// so concurrent local writes survive; Merge is idempotent, and whatever the
	// server hasn't seen yet goes out on the next sync. (A writer squeezing in
	// between this load and the commit can still lose — but the window is now
	// microseconds of local I/O, not a network round trip.)
	current, err := loadTodosForSync(h)
	if err != nil {
		return syncSummary{}, err
	}
	final := Merge(current, merged)
	// No-op guard, mirroring the server: if the merge changed nothing locally,
	// skip the write so the fs watcher isn't woken into a pointless TUI reload
	// on every periodic sync.
	if storeDigest(current) != storeDigest(final) {
		ptrs := make([]*todo.Todo, len(final))
		for i := range final {
			ptrs[i] = &final[i]
		}
		if err := saveNormalized(h, ptrs, nil); err != nil {
			return syncSummary{}, err
		}
	}
	// Count live tasks only: the wire sets include every tombstone ever made,
	// so raw lengths would overstate forever ("received 400" on a no-op sync).
	sum := syncSummary{sent: countLive(local), received: countLive(merged), conflicts: len(dropped)}
	// Record status for `taskr sync --status`. Best-effort: a write failure here
	// must not fail an otherwise-successful sync.
	_ = writeSyncState(sum)
	return sum, nil
}

func countLive(ts []todo.Todo) int {
	n := 0
	for i := range ts {
		if !ts[i].Deleted {
			n++
		}
	}
	return n
}

func postSync(cfg syncConfig, tasks []todo.Todo, timeout time.Duration) ([]todo.Todo, error) {
	body, err := json.Marshal(syncRequest{Tasks: tasks})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(cfg.URL, "/") + "/v1/sync"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var out syncResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
}

// droppedLocalEdits returns the local versions of tasks whose scalar fields were
// overwritten by the merge — a local edit that lost last-writer-wins. Only tasks
// the client had live are considered.
func droppedLocalEdits(local, merged []todo.Todo) []todo.Todo {
	mergedByID := make(map[string]todo.Todo, len(merged))
	for _, t := range merged {
		mergedByID[t.ID] = t
	}
	var dropped []todo.Todo
	for _, l := range local {
		if l.Deleted {
			continue
		}
		m, ok := mergedByID[l.ID]
		if !ok {
			continue
		}
		if m.Deleted {
			// The authoritative version is a tombstone while we still had it
			// live: another device deleted it. That's only a genuine dropped
			// edit if our copy was modified *after* the deletion (an edit that
			// lost to a delete). A plain deletion propagating to us is not a
			// conflict — surfacing it as one nags on every remote delete.
			if l.ModifiedAt.After(m.DeletedAt) {
				dropped = append(dropped, l)
			}
			continue
		}
		if scalarHash(l) != scalarHash(m) {
			dropped = append(dropped, l)
		}
	}
	return dropped
}

// scalarHash hashes only the conflict-relevant scalar fields of a task (not
// children, tags or deps, which merge independently) so droppedLocalEdits can
// tell whether the authoritative version replaced the local one.
func scalarHash(t todo.Todo) [32]byte {
	key := struct {
		Title      string
		Status     todo.Status
		Priority   todo.Priority
		Size       todo.Size
		Project    string
		Notes      string
		ParentID   string
		Recurrence string
		Due        time.Time
		Start      time.Time
		Completed  time.Time
		Deleted    bool
	}{t.Title, t.Status, t.Priority, t.Size, t.Project, t.Notes, t.ParentID,
		t.Recurrence, t.DueDate, t.StartDate, t.CompletedAt, t.Deleted}
	b, _ := json.Marshal(key)
	return sha256.Sum256(b)
}

func syncLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "sync.log")
}

// logDroppedEdits appends one JSON line per dropped local edit to
// ~/.taskr/sync.log so a wrongly-overwritten edit can be recovered.
func logDroppedEdits(dropped []todo.Todo) error {
	if len(dropped) == 0 {
		return nil
	}
	if err := ensureStorageDir(); err != nil {
		return err
	}
	f, err := os.OpenFile(syncLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, t := range dropped {
		line, err := json.Marshal(struct {
			At      string    `json:"at"`
			Note    string    `json:"note"`
			Dropped todo.Todo `json:"dropped"`
		}{now, "local edit superseded by sync (last-writer-wins)", t})
		if err != nil {
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}
