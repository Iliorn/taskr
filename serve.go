package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"taskr/tasksync"
	"taskr/todo"
)

// defaultServerListen is the bind address used when none is configured —
// localhost only, so an accidental "Server: On" never exposes tasks beyond this
// machine until the user deliberately sets a reachable address.
const defaultServerListen = "127.0.0.1:8765"

// startSyncServer launches the sync endpoint in a background goroutine and
// returns the running server so the caller can stop it. net.Listen runs
// synchronously so a bind failure (e.g. address already in use) is reported now
// rather than vanishing into the goroutine. A token is mandatory — the endpoint
// must never serve unauthenticated.
func startSyncServer(listen, token string) (*http.Server, func(), error) {
	if token == "" {
		return nil, nil, fmt.Errorf("a server token is required")
	}
	if listen == "" {
		listen = defaultServerListen
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, nil, err
	}
	srv := newAppSyncServer(token)
	// Watch the store so out-of-process writes (a CLI taskr add on this host)
	// also push to clients. Non-fatal if it can't start.
	stopWatch := func() {}
	if stop, werr := startChangeWatcher(srv.Hub, taskrDir()); werr == nil {
		stopWatch = stop
	}
	// Addr is informational here (Serve uses ln); it reflects the actually-bound
	// address, which matters when the configured port was 0 (OS-assigned).
	httpServer := &http.Server{Addr: ln.Addr().String(), Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		// Serve blocks until the listener fails or Shutdown/Close is called;
		// ErrServerClosed is the expected stop signal, anything else is a real
		// failure that would otherwise vanish into this goroutine.
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("taskr serve: %v", err)
		}
	}()
	return httpServer, stopWatch, nil
}

// serve.go implements `taskr serve`: a small self-hosted HTTP endpoint that
// merges task sets pushed by `taskr sync` clients. It is taskr in another mode —
// it reuses the exact storage and merge code of the app and persists to its own
// ~/.taskr/tasks.db. One endpoint, POST /v1/sync, does push+pull in a single
// round trip: the client sends its full task set (tombstones included), the
// server merges it into the authoritative set, persists the result, and returns
// the merged set for the client to apply.
//
// It is single-owner by design: one shared bearer token, not multi-tenant.
// Anyone can run their own instance; the transport (Tailscale IP, localhost
// behind a reverse proxy, LAN) is a deployment choice via --listen.

func cliServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:8765",
		"address to bind (e.g. a Tailscale IP like 100.x.y.z:8765, or 127.0.0.1:8765 behind a reverse proxy)")
	token := fs.String("token", os.Getenv("TASKR_SYNC_TOKEN"),
		"shared bearer token clients must present (or set TASKR_SYNC_TOKEN)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "taskr serve: a token is required (--token or TASKR_SYNC_TOKEN); refusing to run unauthenticated")
		return 2
	}
	if err := openStore(); err != nil {
		fmt.Fprintf(os.Stderr, "taskr serve: open store: %v\n", err)
		return 1
	}
	srv := newAppSyncServer(*token)
	// Watch the store so out-of-process writes (a CLI taskr add on this host)
	// also push to clients in real time, not just client-initiated merges.
	if stop, werr := startChangeWatcher(srv.Hub, taskrDir()); werr != nil {
		fmt.Fprintf(os.Stderr, "taskr serve: change watcher unavailable (%v); out-of-process writes won't push in real time\n", werr)
	} else {
		defer stop()
	}

	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "taskr serve: listening on %s (POST /v1/sync)\n", *listen)
	if err := httpServer.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "taskr serve: %v\n", err)
		return 1
	}
	return 0
}

// dbStore adapts the app's SQLite store to tasksync.Store: MergeIn is the
// transactional load+merge+save (mergeIntoStore), the one write path a sync
// is allowed to use.
type dbStore struct{ h *sql.DB }

func (d dbStore) MergeIn(incoming []todo.Todo) ([]todo.Todo, bool, error) {
	return mergeIntoStore(d.h, incoming)
}

// newAppSyncServer wires a tasksync.Server to this app: the shared SQLite
// store, a fresh SSE hub, and the serve-state file (throttled) so
// `taskr sync --status` on this host can report the last client contact.
func newAppSyncServer(token string) *tasksync.Server {
	return &tasksync.Server{
		Token:        token,
		Store:        dbStore{db},
		Hub:          tasksync.NewHub(),
		OnClientSync: noteClientSync,
	}
}

// noteClientSync records an inbound client sync for `sync --status`,
// throttled to once a minute. Best-effort — a write failure must never fail
// the sync that triggered it.
var (
	serveStateMu        sync.Mutex
	lastServeStateWrite time.Time
)

func noteClientSync(now time.Time) {
	serveStateMu.Lock()
	defer serveStateMu.Unlock()
	if now.Sub(lastServeStateWrite) < time.Minute {
		return
	}
	lastServeStateWrite = now
	_ = writeServeState(now)
}

// serveState records hub-side sync facts, currently just the last time any
// authenticated client completed a /v1/sync against this host. Written by the
// serve process (headless or in-process), read by `taskr sync --status`.
type serveState struct {
	LastClientSync time.Time `json:"last_client_sync"`
}

func serveStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "serve-state.json")
}

func writeServeState(now time.Time) error {
	if err := ensureStorageDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(serveState{LastClientSync: now.UTC()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(serveStatePath(), b, 0600)
}

// readServeState returns the recorded state; ok is false when no client has
// ever synced (or the file is unreadable/corrupt — treated the same, since
// the only consumer is a status line).
func readServeState() (serveState, bool) {
	b, err := os.ReadFile(serveStatePath())
	if err != nil {
		return serveState{}, false
	}
	var st serveState
	if err := json.Unmarshal(b, &st); err != nil || st.LastClientSync.IsZero() {
		return serveState{}, false
	}
	return st, true
}
