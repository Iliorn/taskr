package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ssehub.go adds real-time change notifications to the sync server. It is a
// doorbell, not a data channel: when the authoritative store changes the hub
// nudges every connected client over Server-Sent Events, and each client reacts
// by running its ordinary /v1/sync pull. No task data ever crosses this stream,
// so it adds no data-exposure surface beyond the already-authenticated
// /v1/sync. The only genuinely new attack surface is holding connections open,
// which is bounded explicitly:
//
//   - the same Bearer token as /v1/sync, checked before the stream starts;
//   - a hard cap on concurrent subscribers (sseMaxClients);
//   - a per-client buffered channel written with a non-blocking send, so one
//     stalled client can never block a broadcast (and thus never a merge);
//   - a periodic heartbeat comment so dead peers are reaped and intermediaries
//     don't time the idle stream out.

const (
	sseMaxClients   = 64
	sseHeartbeat    = 25 * time.Second
	sseClientBuffer = 1 // one pending nudge suffices — changes coalesce
)

// sseHub fans a single "the store changed" signal out to all subscribers.
type sseHub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newSSEHub() *sseHub { return &sseHub{subs: make(map[chan struct{}]struct{})} }

// subscribe registers a client and returns its nudge channel, or nil when the
// hub is at capacity (the caller rejects the connection).
func (h *sseHub) subscribe() chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= sseMaxClients {
		return nil
	}
	ch := make(chan struct{}, sseClientBuffer)
	h.subs[ch] = struct{}{}
	return ch
}

func (h *sseHub) unsubscribe(ch chan struct{}) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *sseHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// broadcast nudges every subscriber without blocking on any of them. A client
// whose buffer is already full has a nudge pending, so skipping it loses
// nothing: one sync pulls every change accumulated since.
func (h *sseHub) broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// handleEvents streams change notifications to a client as Server-Sent Events.
// It blocks for the life of the connection (its own request goroutine) and
// returns — closing the stream — when the client disconnects or the request
// context is cancelled.
func (s *syncServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := s.hub.subscribe()
	if ch == nil {
		http.Error(w, "too many subscribers", http.StatusServiceUnavailable)
		return
	}
	defer s.hub.unsubscribe(ch)

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

// startChangeWatcher watches the storage directory and nudges the hub whenever
// tasks.db changes, so a direct CLI write on the server host (taskr add/done…)
// reaches connected clients in real time — not only client-initiated merges. It
// reuses the same fsnotify plumbing the TUI uses for live reload. Returns a stop
// func; if the watcher can't start the server still works, it just loses
// real-time push for out-of-process writes (client syncs still nudge directly).
func startChangeWatcher(hub *sseHub, dir string) (stop func(), err error) {
	state := newWatcherState()
	cleanup, err := startWatcher(state, dir)
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-state.ch:
				hub.broadcast()
			}
		}
	}()
	return func() { close(done); cleanup() }, nil
}
