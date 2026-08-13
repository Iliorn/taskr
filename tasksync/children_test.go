package tasksync

import (
	"reflect"
	"testing"
	"time"

	"taskr/todo"
)

// children_test.go covers the child collections — learnings especially, which
// had almost no coverage despite being the one child type with no other way
// back once lost: a learning exists only inside its task.

func learning(id, text string, modified time.Time) todo.Learning {
	return todo.Learning{ID: id, Text: text, CreatedAt: modified, ModifiedAt: modified}
}

func TestMergeLearnings(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	later := base.Add(time.Hour)

	t.Run("union by id, both sides kept", func(t *testing.T) {
		a := []todo.Learning{learning("1", "from the laptop", base)}
		b := []todo.Learning{learning("2", "from the phone", base)}
		got := mergeLearnings(a, b)
		if len(got) != 2 {
			t.Fatalf("merged %d learnings, want both", len(got))
		}
	})

	t.Run("the later edit of the same learning wins", func(t *testing.T) {
		a := []todo.Learning{learning("1", "first draft", base)}
		b := []todo.Learning{learning("1", "reworded", later)}
		if got := mergeLearnings(a, b); got[0].Text != "reworded" {
			t.Errorf("kept %q, want the later edit", got[0].Text)
		}
		// Symmetric: argument order must not decide the winner.
		if got := mergeLearnings(b, a); got[0].Text != "reworded" {
			t.Errorf("kept %q with the arguments swapped", got[0].Text)
		}
	})

	t.Run("a deletion is sticky and keeps its tombstone", func(t *testing.T) {
		live := learning("1", "still here", later)
		tomb := learning("1", "gone", base)
		tomb.DeletedAt = base.Add(30 * time.Minute)

		for _, got := range [][]todo.Learning{
			mergeLearnings([]todo.Learning{live}, []todo.Learning{tomb}),
			mergeLearnings([]todo.Learning{tomb}, []todo.Learning{live}),
		} {
			if len(got) != 1 {
				t.Fatalf("merged to %d learnings, want the tombstone alone", len(got))
			}
			if got[0].DeletedAt.IsZero() {
				t.Error("the tombstone lost its DeletedAt — the deletion would resurrect on the next sync")
			}
		}
	})

	t.Run("two tombstones resolve independently of order", func(t *testing.T) {
		first := learning("1", "a", base)
		first.DeletedAt = base
		second := learning("1", "b", base)
		second.DeletedAt = later

		ab := mergeLearnings([]todo.Learning{first}, []todo.Learning{second})
		ba := mergeLearnings([]todo.Learning{second}, []todo.Learning{first})
		if !reflect.DeepEqual(ab, ba) {
			t.Fatalf("order changed the result: %+v vs %+v — the two stores would ping-pong forever", ab, ba)
		}
		if !ab[0].DeletedAt.Equal(later) {
			t.Errorf("kept DeletedAt %v, want the later deletion", ab[0].DeletedAt)
		}
	})

	t.Run("equal timestamps resolve by hash, not by argument order", func(t *testing.T) {
		a := []todo.Learning{learning("1", "version a", base)}
		b := []todo.Learning{learning("1", "version b", base)}
		if !reflect.DeepEqual(mergeLearnings(a, b), mergeLearnings(b, a)) {
			t.Error("a tie resolved differently depending on argument order")
		}
	})

	t.Run("nil inputs", func(t *testing.T) {
		if got := mergeLearnings(nil, nil); len(got) != 0 {
			t.Errorf("merging nothing produced %+v", got)
		}
		only := []todo.Learning{learning("1", "solo", base)}
		if got := mergeLearnings(nil, only); len(got) != 1 {
			t.Errorf("merging against nil dropped the learning")
		}
	})
}

// A whole-task merge has to carry the learnings through, not just the scalars.
func TestMergeCarriesLearningsAcrossDevices(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	server := todo.New("write the postmortem")
	server.ID = "a"
	server.ModifiedAt = base
	server.Learnings = []todo.Learning{learning("1", "check the disk first", base)}

	client := server
	client.Learnings = []todo.Learning{learning("2", "and the clock", base)}

	merged := Merge([]todo.Todo{server}, []todo.Todo{client})
	if len(merged) != 1 {
		t.Fatalf("merged to %d tasks", len(merged))
	}
	if len(merged[0].Learnings) != 2 {
		t.Errorf("task kept %d learnings, want both devices'", len(merged[0].Learnings))
	}
}

// The digest decides whether a sync "changed" anything, which gates both the
// write and the broadcast. It has to ignore orderings the merge may introduce
// and the zone a load happened to rehydrate in.
func TestStoreDigestIgnoresOrderAndZone(t *testing.T) {
	base := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	task := todo.New("digest me")
	task.ID = "a"
	task.ModifiedAt = base
	task.Tags = []string{"work", "home"}
	task.Dependencies = []string{"b", "c"}
	task.Comments = []todo.Comment{{ID: "2", Text: "second", CreatedAt: base}, {ID: "1", Text: "first", CreatedAt: base}}
	task.Learnings = []todo.Learning{{ID: "2", Text: "b", CreatedAt: base}, {ID: "1", Text: "a", CreatedAt: base}}
	task.TimeEntries = []todo.TimeEntry{{ID: "2", StartedAt: base}, {ID: "1", StartedAt: base}}

	shuffled := task
	shuffled.Tags = []string{"home", "work"}
	shuffled.Dependencies = []string{"c", "b"}
	shuffled.Comments = []todo.Comment{task.Comments[1], task.Comments[0]}
	shuffled.Learnings = []todo.Learning{task.Learnings[1], task.Learnings[0]}
	shuffled.TimeEntries = []todo.TimeEntry{task.TimeEntries[1], task.TimeEntries[0]}

	if StoreDigest([]todo.Todo{task}) != StoreDigest([]todo.Todo{shuffled}) {
		t.Error("reordering children changed the digest — every sync would look like a change")
	}

	// Same instants, different zone: a store loaded in another timezone must
	// hash the same, or two devices would write to each other forever.
	elsewhere := task
	zone := time.FixedZone("UTC+7", 7*3600)
	elsewhere.ModifiedAt = task.ModifiedAt.In(zone)
	elsewhere.CreatedAt = task.CreatedAt.In(zone)
	if StoreDigest([]todo.Todo{task}) != StoreDigest([]todo.Todo{elsewhere}) {
		t.Error("the same instant in another zone changed the digest")
	}

	// And a real change must still register.
	changed := task
	changed.Title = "something else"
	if StoreDigest([]todo.Todo{task}) == StoreDigest([]todo.Todo{changed}) {
		t.Error("an edited title did not change the digest")
	}

	// Canonicalization must not scribble on the caller's slices.
	before := append([]string(nil), task.Tags...)
	_ = CanonicalJSON(task)
	if !reflect.DeepEqual(task.Tags, before) {
		t.Errorf("CanonicalJSON reordered the caller's tags: %v", task.Tags)
	}
}
