package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	srv := &syncServer{db: db, token: token, hub: newSSEHub()}
	// Watch the store so out-of-process writes (a CLI taskr add on this host)
	// also push to clients. Non-fatal if it can't start.
	stopWatch := func() {}
	if stop, werr := startChangeWatcher(srv.hub, taskrDir()); werr == nil {
		stopWatch = stop
	}
	// Addr is informational here (Serve uses ln); it reflects the actually-bound
	// address, which matters when the configured port was 0 (OS-assigned).
	httpServer := &http.Server{Addr: ln.Addr().String(), Handler: srv.handler(), ReadHeaderTimeout: 10 * time.Second}
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

type syncRequest struct {
	Tasks []todo.Todo `json:"tasks"`
}

type syncResponse struct {
	Tasks []todo.Todo `json:"tasks"`
}

// syncServer holds the store handle and serializes merges so concurrent client
// syncs can't interleave load→merge→save and drop a write. hub fans real-time
// change notifications out to subscribed clients (see ssehub.go).
// lastStateWrite (guarded by mu) throttles the serve-state file writes.
type syncServer struct {
	db             *sql.DB
	token          string
	hub            *sseHub
	mu             sync.Mutex
	lastStateWrite time.Time
}

// handler builds the route set. Shared by both entry points (headless cliServe
// and the in-process startSyncServer) so they never drift.
func (s *syncServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/sync", s.handleSync)
	mux.HandleFunc("/v1/events", s.handleEvents)
	return mux
}

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
	srv := &syncServer{db: db, token: *token, hub: newSSEHub()}
	// Watch the store so out-of-process writes (a CLI taskr add on this host)
	// also push to clients in real time, not just client-initiated merges.
	if stop, werr := startChangeWatcher(srv.hub, taskrDir()); werr != nil {
		fmt.Fprintf(os.Stderr, "taskr serve: change watcher unavailable (%v); out-of-process writes won't push in real time\n", werr)
	} else {
		defer stop()
	}

	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           srv.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "taskr serve: listening on %s (POST /v1/sync)\n", *listen)
	if err := httpServer.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "taskr serve: %v\n", err)
		return 1
	}
	return 0
}

func (s *syncServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func (s *syncServer) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimPrefix(h, prefix)
	// Constant-time compare so the token can't be guessed by timing.
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func (s *syncServer) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req syncRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	merged, err := s.sync(req.Tasks)
	if err != nil {
		http.Error(w, "merge failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(syncResponse{Tasks: merged}); err != nil {
		log.Printf("taskr serve: encode response: %v", err)
	}
}

// sync merges the client's tasks into the authoritative set, persists the
// result, and returns it. Serialized by mu so concurrent syncs are atomic.
func (s *syncServer) sync(clientTasks []todo.Todo) ([]todo.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clampFutureEventTimes(clientTasks, time.Now())
	// Note the client contact for `sync --status` on this host: a hub with no
	// client URL of its own otherwise reports "last sync: never", reading as
	// broken while it serves every other device daily. Throttled + best-effort.
	if now := time.Now(); now.Sub(s.lastStateWrite) > time.Minute {
		s.lastStateWrite = now
		_ = writeServeState(now)
	}
	// Load, merge and save run inside one transaction (mergeIntoStore): mu only
	// serializes HTTP-level syncs, and a CLI write on this host is a different
	// process that mu cannot see — transactionality is what keeps such a write
	// from being overwritten (or its fresh comment tombstoned) mid-merge.
	merged, changed, err := mergeIntoStore(s.db, clientTasks)
	if err != nil {
		return nil, err
	}
	// Nudge every connected client to pull — only when something was actually
	// written (a no-op pull must not broadcast, or syncs would feed back). The
	// change watcher would also catch the write, but broadcasting here makes
	// client→client propagation immediate and independent of fsnotify.
	if changed && s.hub != nil {
		s.hub.broadcast()
	}
	return merged, nil
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

// maxClientClockSkew bounds how far ahead of the server's clock a client's
// merge-ordering timestamps may run. The merge is last-writer-wins by
// ModifiedAt/DeletedAt, all stamped from device wall clocks — a device with a
// clock hours in the future would win every conflict it touches until real
// time catches up (and its edits would be unbeatable by devices with correct
// clocks). Five minutes tolerates ordinary NTP drift without letting a broken
// clock own the store.
const maxClientClockSkew = 5 * time.Minute

// clampFutureEventTimes pulls any merge-ordering timestamp (task and child
// ModifiedAt/DeletedAt) that is more than maxClientClockSkew ahead of now back
// to now, in place. Domain dates (DueDate, StartDate, time-entry bounds) are
// deliberately untouched — a future due date is data, not clock skew.
func clampFutureEventTimes(tasks []todo.Todo, now time.Time) {
	limit := now.Add(maxClientClockSkew)
	clamp := func(t *time.Time) {
		if t.After(limit) {
			*t = now
		}
	}
	for i := range tasks {
		t := &tasks[i]
		clamp(&t.ModifiedAt)
		clamp(&t.DeletedAt)
		for j := range t.Comments {
			clamp(&t.Comments[j].ModifiedAt)
			clamp(&t.Comments[j].DeletedAt)
		}
		for j := range t.Learnings {
			clamp(&t.Learnings[j].ModifiedAt)
			clamp(&t.Learnings[j].DeletedAt)
		}
		for j := range t.TimeEntries {
			clamp(&t.TimeEntries[j].ModifiedAt)
			clamp(&t.TimeEntries[j].DeletedAt)
		}
	}
}

// storeDigest is an order-independent fingerprint of a task set: identical
// content hashes identically regardless of slice ordering, so sync() can tell
// whether a merge actually changed the store. It must have no false positives —
// a stable order is imposed on the task slice and every child/tag slice before
// hashing — or the no-op-write guard could loop.
func storeDigest(ts []todo.Todo) [32]byte {
	cp := make([]todo.Todo, len(ts))
	copy(cp, ts)
	for i := range cp {
		canonicalizeForDigest(&cp[i])
	}
	sort.Slice(cp, func(i, j int) bool { return cp[i].ID < cp[j].ID })
	b, _ := json.Marshal(cp)
	return sha256.Sum256(b)
}

// canonicalizeForDigest replaces a task's order-insensitive slices with sorted
// copies — never mutating the caller's backing arrays — so storeDigest is stable
// across the reordering a merge may introduce.
func canonicalizeForDigest(t *todo.Todo) {
	t.Tags = sortedStrings(t.Tags)
	t.Dependencies = sortedStrings(t.Dependencies)
	if len(t.Comments) > 1 {
		c := append([]todo.Comment(nil), t.Comments...)
		sort.Slice(c, func(i, j int) bool { return c[i].ID < c[j].ID })
		t.Comments = c
	}
	if len(t.Learnings) > 1 {
		l := append([]todo.Learning(nil), t.Learnings...)
		sort.Slice(l, func(i, j int) bool { return l[i].ID < l[j].ID })
		t.Learnings = l
	}
	if len(t.TimeEntries) > 1 {
		e := append([]todo.TimeEntry(nil), t.TimeEntries...)
		sort.Slice(e, func(i, j int) bool { return e[i].ID < e[j].ID })
		t.TimeEntries = e
	}
}

func sortedStrings(s []string) []string {
	if len(s) < 2 {
		return s
	}
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}
