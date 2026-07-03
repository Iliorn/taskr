package tasksync

import (
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

// Hub fans a single "the store changed" signal out to all subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func NewHub() *Hub { return &Hub{subs: make(map[chan struct{}]struct{})} }

// Subscribe registers a client and returns its nudge channel, or nil when the
// hub is at capacity (the caller rejects the connection).
func (h *Hub) Subscribe() chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= sseMaxClients {
		return nil
	}
	ch := make(chan struct{}, sseClientBuffer)
	h.subs[ch] = struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(ch chan struct{}) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Broadcast nudges every subscriber without blocking on any of them. A client
// whose buffer is already full has a nudge pending, so skipping it loses
// nothing: one sync pulls every change accumulated since.
func (h *Hub) Broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
