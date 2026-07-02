package main

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// synclive.go is the client side of real-time push: a background listener that
// holds a Server-Sent Events stream to the sync server and, whenever the server
// signals the store changed, asks the TUI to run an immediate /v1/sync pull.
// That turns inbound propagation from "up to one syncTickInterval" into
// near-instant. The periodic tick stays as a fallback so a dropped stream still
// converges. No task data crosses the stream — it's a nudge; the existing
// authenticated /v1/sync carries the data.

// syncEventMsg reaches the Update loop when the server signalled a change. The
// handler re-arms the listener and triggers a background sync.
type syncEventMsg struct{}

// liveSyncState owns the listener goroutine. ch carries coalesced nudges into
// the Bubble Tea loop (1-buffered, like the watcher channel); stop ends it.
type liveSyncState struct {
	ch   chan struct{}
	stop chan struct{}
}

// startLiveSync launches the SSE listener for cfg, or returns nil if cfg isn't
// ready. The goroutine reconnects on its own with backoff, so the caller just
// arms waitForSyncEvent(state.ch).
func startLiveSync(cfg syncConfig) *liveSyncState {
	if !cfg.ready() {
		return nil
	}
	ls := &liveSyncState{ch: make(chan struct{}, 1), stop: make(chan struct{})}
	go ls.run(cfg)
	return ls
}

func (ls *liveSyncState) close() {
	select {
	case <-ls.stop:
	default:
		close(ls.stop)
	}
}

// run holds a stream open, reconnecting with capped exponential backoff. A
// healthy connection resets the backoff so a brief blip doesn't slow recovery.
func (ls *liveSyncState) run(cfg syncConfig) {
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for {
		select {
		case <-ls.stop:
			return
		default:
		}
		if ls.stream(cfg) {
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
func (ls *liveSyncState) stream(cfg syncConfig) bool {
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

	endpoint := strings.TrimRight(cfg.URL, "/") + "/v1/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
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
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "event:") && strings.TrimSpace(line[len("event:"):]) == "changed" {
			select {
			case ls.ch <- struct{}{}:
			default:
			}
		}
	}
	return true
}

// waitForSyncEvent bridges the listener channel into the Update loop, mirroring
// waitForDBChange: it blocks for the next nudge and is re-armed by the handler.
func waitForSyncEvent(ch chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return syncEventMsg{}
	}
}
