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
	go httpServer.Serve(ln)
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
type syncServer struct {
	db    *sql.DB
	token string
	hub   *sseHub
	mu    sync.Mutex
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

	server, err := loadTodosForSync(s.db)
	if err != nil {
		return nil, err
	}
	merged := Merge(server, clientTasks)
	// A no-op pull (client already in sync) must not write: the write would wake
	// the change watcher into a pointless broadcast and churn the DB. Idempotent
	// merge plus this guard guarantee convergence with no feedback loop.
	if storeDigest(server) == storeDigest(merged) {
		return merged, nil
	}
	ptrs := make([]*todo.Todo, len(merged))
	for i := range merged {
		ptrs[i] = &merged[i]
	}
	if err := saveNormalized(s.db, ptrs, nil); err != nil {
		return nil, err
	}
	// Nudge every connected client to pull. The change watcher would also catch
	// this write, but broadcasting here makes client→client propagation immediate
	// and independent of fsnotify being available.
	if s.hub != nil {
		s.hub.broadcast()
	}
	return merged, nil
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
