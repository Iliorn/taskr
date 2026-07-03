package tasksync

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"time"
)

// synclive.go is the client side of real-time push: a background listener that
// holds a Server-Sent Events stream to the sync server and, whenever the server
// signals the store changed, asks the TUI to run an immediate /v1/sync pull.
// That turns inbound propagation from "up to one syncTickInterval" into
// near-instant. The periodic tick stays as a fallback so a dropped stream still
// converges. No task data crosses the stream — it's a nudge; the existing
// authenticated /v1/sync carries the data.

// Listener owns the SSE listener goroutine. C carries coalesced change nudges
// (1-buffered — the consumer needs "something changed", not a count); Close
// ends the goroutine.
type Listener struct {
	C    chan struct{}
	stop chan struct{}
}

// StartListener launches the SSE listener against the server at url with the
// shared bearer token. The goroutine reconnects on its own with backoff, so
// the caller just reads C.
func StartListener(url, token string) *Listener {
	ls := &Listener{C: make(chan struct{}, 1), stop: make(chan struct{})}
	go ls.run(url, token)
	return ls
}

// Done is closed once Close has been called — for teardown ordering and
// tests; the listener goroutine may still be mid-return when it fires.
func (ls *Listener) Done() <-chan struct{} { return ls.stop }

func (ls *Listener) Close() {
	select {
	case <-ls.stop:
	default:
		close(ls.stop)
	}
}

// run holds a stream open, reconnecting with capped exponential backoff. A
// healthy connection resets the backoff so a brief blip doesn't slow recovery.
func (ls *Listener) run(url, token string) {
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for {
		select {
		case <-ls.stop:
			return
		default:
		}
		if ls.stream(url, token) {
			backoff = time.Second
		}
		select {
		case <-ls.stop:
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// stream holds one SSE connection until it drops or stop fires, nudging ch on
// every "changed" event. Returns true if the connection was established (so the
// caller can reset its backoff).
func (ls *Listener) stream(url, token string) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel the in-flight request when stop fires so a blocked read unblocks.
	go func() {
		select {
		case <-ls.stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	endpoint := strings.TrimRight(url, "/") + "/v1/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")

	// No client timeout: the stream is long-lived and kept alive by heartbeats;
	// ctx cancellation (stop) is what ends it. The shared transport still bounds
	// the dial, so an unreachable server fails into the backoff loop quickly.
	resp, err := (&http.Client{Transport: syncTransport}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	// Stall watchdog: the server writes a heartbeat comment every sseHeartbeat,
	// so a read silence of several beats means the connection died without a
	// FIN/RST (server power loss, network partition) and the blocked read would
	// otherwise hang forever — live push silently gone for the session. Cancel
	// the request so Scan unblocks and run() reconnects. Every received line
	// (heartbeats included) re-arms it.
	const stallAfter = 4 * sseHeartbeat
	watchdog := time.AfterFunc(stallAfter, cancel)
	defer watchdog.Stop()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		watchdog.Reset(stallAfter)
		line := sc.Text()
		if strings.HasPrefix(line, "event:") && strings.TrimSpace(line[len("event:"):]) == "changed" {
			select {
			case ls.C <- struct{}{}:
			default:
			}
		}
	}
	return true
}
