package main

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"taskr/todo"
)

func TestStoreDigestOrderIndependent(t *testing.T) {
	a := todo.New("alpha")
	a.Tags = []string{"x", "y"}
	b := todo.New("beta")

	// Same content, different task order and different tag order → same digest.
	a2 := a
	a2.Tags = []string{"y", "x"}
	set1 := []todo.Todo{a, b}
	set2 := []todo.Todo{b, a2}
	if storeDigest(set1) != storeDigest(set2) {
		t.Errorf("digest should ignore task/tag ordering")
	}

	// A real change → different digest.
	changed := a
	changed.Title = "alpha edited"
	if storeDigest([]todo.Todo{a, b}) == storeDigest([]todo.Todo{changed, b}) {
		t.Errorf("digest should change when a title changes")
	}
}

func TestStoreDigestDoesNotMutateInput(t *testing.T) {
	a := todo.New("alpha")
	a.Tags = []string{"z", "a"}
	_ = storeDigest([]todo.Todo{a})
	if a.Tags[0] != "z" || a.Tags[1] != "a" {
		t.Errorf("storeDigest must not reorder the caller's slices, got %v", a.Tags)
	}
}

func TestSSEHubCapAndNonBlocking(t *testing.T) {
	h := newSSEHub()
	for i := 0; i < sseMaxClients; i++ {
		if h.subscribe() == nil {
			t.Fatalf("subscribe %d should succeed under the cap", i)
		}
	}
	if h.subscribe() != nil {
		t.Errorf("subscribe past the cap must return nil")
	}

	// Broadcast must not block even when every buffer is already full.
	done := make(chan struct{})
	go func() { h.broadcast(); h.broadcast(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on full subscriber buffers")
	}
}

func TestSSEEventsRequireToken(t *testing.T) {
	srv := &syncServer{db: openTestDB(t), token: "tok", hub: newSSEHub()}
	w := httptest.NewRecorder()
	srv.handleEvents(w, httptest.NewRequest(http.MethodGet, "/v1/events", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated events stream: want 401, got %d", w.Code)
	}
}

// TestSSEPushOnChange is the end-to-end contract: a subscriber connected to
// /v1/events is nudged when a sync changes the store, and is NOT nudged by a
// no-op sync (the skip-write guard).
func TestSSEPushOnChange(t *testing.T) {
	srv := &syncServer{db: openTestDB(t), token: "tok", hub: newSSEHub()}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	events := make(chan string, 8)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open events stream: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events stream: want 200, got %d", resp.StatusCode)
	}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "event:") {
				events <- strings.TrimSpace(line[len("event:"):])
			}
		}
	}()

	// Give the subscriber a moment to register before the first broadcast.
	waitFor(t, func() bool { return srv.hub.subscriberCount() == 1 })

	// A real change → exactly one "changed" event.
	if _, err := srv.sync([]todo.Todo{todo.New("brand new")}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	select {
	case ev := <-events:
		if ev != "changed" {
			t.Errorf("unexpected event %q", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a change event, got none")
	}

	// A no-op sync (client already in sync) must not nudge.
	if _, err := srv.sync(nil); err != nil {
		t.Fatalf("no-op sync: %v", err)
	}
	select {
	case ev := <-events:
		t.Errorf("no-op sync should not push, got %q", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
