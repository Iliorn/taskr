package tasksync

import (
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
	if StoreDigest(set1) != StoreDigest(set2) {
		t.Errorf("digest should ignore task/tag ordering")
	}

	// A real change → different digest.
	changed := a
	changed.Title = "alpha edited"
	if StoreDigest([]todo.Todo{a, b}) == StoreDigest([]todo.Todo{changed, b}) {
		t.Errorf("digest should change when a title changes")
	}
}

func TestStoreDigestDoesNotMutateInput(t *testing.T) {
	a := todo.New("alpha")
	a.Tags = []string{"z", "a"}
	_ = StoreDigest([]todo.Todo{a})
	if a.Tags[0] != "z" || a.Tags[1] != "a" {
		t.Errorf("storeDigest must not reorder the caller's slices, got %v", a.Tags)
	}
}

func TestSSEHubCapAndNonBlocking(t *testing.T) {
	h := NewHub()
	for i := 0; i < sseMaxClients; i++ {
		if h.Subscribe() == nil {
			t.Fatalf("subscribe %d should succeed under the cap", i)
		}
	}
	if h.Subscribe() != nil {
		t.Errorf("subscribe past the cap must return nil")
	}

	// Broadcast must not block even when every buffer is already full.
	done := make(chan struct{})
	go func() { h.Broadcast(); h.Broadcast(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on full subscriber buffers")
	}
}
