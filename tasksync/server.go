package tasksync

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"taskr/todo"
)

// Store is the doorway the sync engine needs into task storage — the only
// thing this package asks of the application. Keeping it to one method keeps
// the package free of SQL, file paths and app config.
type Store interface {
	// MergeIn folds incoming into the store atomically and returns the merged
	// authoritative set (tombstones included). changed=false means the store
	// already contained the result and nothing was written — callers use it to
	// skip change broadcasts so idle syncs can't feed back.
	MergeIn(incoming []todo.Todo) (merged []todo.Todo, changed bool, err error)
}

// Request and Response are the /v1/sync wire format: the client pushes its
// full task set (tombstones included) and receives the merged authoritative
// set in one round trip.
type Request struct {
	Tasks []todo.Todo `json:"tasks"`
}

type Response struct {
	Tasks []todo.Todo `json:"tasks"`
	// ServerTime lets the client detect a skewed local clock (see
	// ClockSkewWarning): the LWW merge runs on wall-clock timestamps, so a
	// device with a bad clock silently loses or wrongly wins conflicts, and
	// nothing else in the protocol would ever tell the user. Zero when the
	// server predates the field; clients skip the check then.
	ServerTime time.Time `json:"server_time,omitempty"`
}

// Server is the sync endpoint: POST /v1/sync does push+pull in one round trip,
// GET /v1/events streams change nudges. It is single-owner by design — one
// shared bearer token, not multi-tenant. Transport (Tailscale IP, localhost
// behind a proxy, LAN) is the caller's deployment choice.
type Server struct {
	Token string
	Store Store
	// Hub, when non-nil, is nudged after every merge that changed the store so
	// connected clients pull immediately.
	Hub *Hub
	// OnClientSync, when non-nil, is told the time of every authenticated sync
	// (the app records it for `sync --status`). Called under the sync lock —
	// keep it fast; throttling is the callback's business.
	OnClientSync func(time.Time)

	mu sync.Mutex
}

// Handler builds the route set. Shared by every entry point (headless serve,
// in-process TUI server, tests) so they never drift.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/sync", s.handleSync)
	mux.HandleFunc("/v1/events", s.handleEvents)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimPrefix(h, prefix)
	// Constant-time compare so the token can't be guessed by timing.
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	merged, err := s.Sync(req.Tasks)
	if err != nil {
		http.Error(w, "merge failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(Response{Tasks: merged, ServerTime: time.Now().UTC()}); err != nil {
		log.Printf("taskr serve: encode response: %v", err)
	}
}

// Sync merges the client's tasks into the authoritative set and returns it.
// Serialized by mu so concurrent HTTP syncs can't interleave; atomicity
// against writers in OTHER processes is the Store's job (MergeIn).
func (s *Server) Sync(clientTasks []todo.Todo) ([]todo.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clampFutureEventTimes(clientTasks, time.Now())
	if s.OnClientSync != nil {
		s.OnClientSync(time.Now())
	}
	merged, changed, err := s.Store.MergeIn(clientTasks)
	if err != nil {
		return nil, err
	}
	// Nudge every connected client to pull — only when something was actually
	// written (a no-op pull must not broadcast, or syncs would feed back).
	if changed && s.Hub != nil {
		s.Hub.Broadcast()
	}
	return merged, nil
}

// handleEvents streams change notifications to a client as Server-Sent Events.
// It blocks for the life of the connection (its own request goroutine) and
// returns — closing the stream — when the client disconnects or the request
// context is cancelled.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if s.Hub == nil {
		http.Error(w, "events unavailable", http.StatusServiceUnavailable)
		return
	}
	ch := s.Hub.Subscribe()
	if ch == nil {
		http.Error(w, "too many subscribers", http.StatusServiceUnavailable)
		return
	}
	defer s.Hub.Unsubscribe(ch)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// An initial comment opens the stream so the client (and any proxy) flushes
	// headers and treats the connection as live immediately.
	fmt.Fprint(w, ": ok\n\n")
	flusher.Flush()

	beat := time.NewTicker(sseHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "event: changed\ndata: 1\n\n")
			flusher.Flush()
		case <-beat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
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

// StoreDigest is an order-independent fingerprint of a task set: identical
// content hashes identically regardless of slice ordering. It must have no
// false positives — a stable order is imposed on the task slice and every
// child/tag slice before hashing.
func StoreDigest(ts []todo.Todo) [32]byte {
	cp := make([]todo.Todo, len(ts))
	copy(cp, ts)
	for i := range cp {
		canonicalizeForDigest(&cp[i])
	}
	sort.Slice(cp, func(i, j int) bool { return cp[i].ID < cp[j].ID })
	b, _ := json.Marshal(cp)
	return sha256.Sum256(b)
}

// CanonicalJSON is a single task's order-insensitive fingerprint source: a
// value copy with its unordered slices sorted, marshalled. The app's
// changed-row detection compares these to decide what a merge must write.
func CanonicalJSON(t todo.Todo) []byte {
	canonicalizeForDigest(&t)
	b, _ := json.Marshal(t)
	return b
}

// canonicalizeForDigest replaces a task's order-insensitive slices with sorted
// copies — never mutating the caller's backing arrays — so digests are stable
// across the reordering a merge may introduce. Timestamps are normalized to
// UTC: json.Marshal embeds the zone offset, and times are compared as
// instants, so two loads of the same store must hash identically even when
// one rehydrated in a different zone (parseTime returns local time).
func canonicalizeForDigest(t *todo.Todo) {
	t.Tags = sortedStrings(t.Tags)
	t.Dependencies = sortedStrings(t.Dependencies)
	t.CreatedAt = t.CreatedAt.UTC()
	t.ModifiedAt = t.ModifiedAt.UTC()
	t.CompletedAt = t.CompletedAt.UTC()
	t.StartDate = t.StartDate.UTC()
	t.DueDate = t.DueDate.UTC()
	t.DeletedAt = t.DeletedAt.UTC()
	if len(t.Comments) > 0 {
		c := append([]todo.Comment(nil), t.Comments...)
		sort.Slice(c, func(i, j int) bool { return c[i].ID < c[j].ID })
		for i := range c {
			c[i].CreatedAt = c[i].CreatedAt.UTC()
			c[i].ModifiedAt = c[i].ModifiedAt.UTC()
			c[i].DeletedAt = c[i].DeletedAt.UTC()
		}
		t.Comments = c
	}
	if len(t.Learnings) > 0 {
		l := append([]todo.Learning(nil), t.Learnings...)
		sort.Slice(l, func(i, j int) bool { return l[i].ID < l[j].ID })
		for i := range l {
			l[i].CreatedAt = l[i].CreatedAt.UTC()
			l[i].ModifiedAt = l[i].ModifiedAt.UTC()
			l[i].DeletedAt = l[i].DeletedAt.UTC()
		}
		t.Learnings = l
	}
	if len(t.TimeEntries) > 0 {
		e := append([]todo.TimeEntry(nil), t.TimeEntries...)
		sort.Slice(e, func(i, j int) bool { return e[i].ID < e[j].ID })
		for i := range e {
			e[i].StartedAt = e[i].StartedAt.UTC()
			e[i].StoppedAt = e[i].StoppedAt.UTC()
			e[i].ModifiedAt = e[i].ModifiedAt.UTC()
			e[i].DeletedAt = e[i].DeletedAt.UTC()
			e[i].LastSeen = e[i].LastSeen.UTC()
		}
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
