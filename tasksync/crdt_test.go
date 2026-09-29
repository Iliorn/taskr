package tasksync

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/todo"
)

// crdt_test.go checks the three laws that let every device end with the same
// task set however syncs are ordered, repeated or interleaved: Merge is
// commutative, associative and idempotent. The replicas are random edits of
// one origin, made by devices with their own clocks, some of them older
// builds that stamp nothing and bump the modification time instead.

// replica is one device's edits of a task set.
type replica struct {
	clock  *hlc.Clock
	legacy bool // an older build: no stamps, whole-task times
	now    time.Time
	tasks  map[string]todo.Todo
}

var crdtIDs = []string{"a", "b", "c", "d"}

func origin(r *rand.Rand, t0 time.Time) map[string]todo.Todo {
	out := map[string]todo.Todo{}
	for i, id := range crdtIDs {
		if r.Intn(4) == 0 {
			continue // not every task exists before the devices diverge
		}
		t := todo.Todo{ID: id, Title: "task " + id, Priority: todo.PriorityMedium,
			CreatedAt: t0, ModifiedAt: t0.Add(time.Duration(i) * time.Millisecond)}
		if r.Intn(2) == 0 {
			t.Tags = []string{"home"}
		}
		out[id] = t
	}
	return out
}

func (rp *replica) edit(r *rand.Rand) {
	rp.now = rp.now.Add(time.Duration(r.Intn(3)) * time.Millisecond)
	id := crdtIDs[r.Intn(len(crdtIDs))]
	t, ok := rp.tasks[id]
	if !ok {
		t = todo.Todo{ID: id, Title: "made here", CreatedAt: rp.now}
	}
	before := t
	before.Stamps = t.Stamps
	switch r.Intn(10) {
	case 0:
		t.Title = fmt.Sprintf("title %d", r.Intn(3))
	case 1:
		t.Priority = todo.Priority(r.Intn(3))
	case 2:
		t.DueDate = rp.now.Add(time.Duration(r.Intn(3)) * 24 * time.Hour)
	case 3:
		if t.Status == todo.Done {
			t.Status, t.CompletedAt = todo.Pending, time.Time{}
		} else {
			t.Status, t.CompletedAt = todo.Done, rp.now
		}
	case 4:
		tag := []string{"home", "work", "urgent"}[r.Intn(3)]
		if t.HasMember(todo.TagKey(tag)) {
			var kept []string
			for _, x := range t.Tags {
				if x != tag {
					kept = append(kept, x)
				}
			}
			t.Tags = kept
		} else {
			t.Tags = append(append([]string(nil), t.Tags...), tag)
		}
	case 5:
		dep := crdtIDs[r.Intn(len(crdtIDs))]
		if t.HasMember(todo.DepKey(dep)) {
			t.Dependencies = nil
		} else {
			t.Dependencies = append(append([]string(nil), t.Dependencies...), dep)
		}
	case 6:
		t.Deleted = !t.Deleted
		t.DeletedAt = time.Time{}
		if t.Deleted {
			t.DeletedAt = rp.now
		}
	case 7:
		t.ParentID = crdtIDs[r.Intn(len(crdtIDs))]
	case 8:
		c := todo.Comment{ID: fmt.Sprintf("%s-%d", id, r.Intn(3)), Text: "note", CreatedAt: rp.now, ModifiedAt: rp.now}
		if r.Intn(3) == 0 {
			c.DeletedAt = rp.now
		}
		t.Comments = append(append([]todo.Comment(nil), t.Comments...), c)
	case 9:
		t.Notes = fmt.Sprintf("notes %d", r.Intn(3))
	}
	t.ModifiedAt = todo.StampAt(rp.now, before.ModifiedAt)
	if !rp.legacy {
		t.Stamps = stampLike(before, t, ok, rp.clock, rp.now)
	}
	rp.tasks[id] = t
}

// stampLike stamps t against before the way the app's save does
// (stampEdit): a fresh stamp for each unit that changed, the old stamp for
// the rest, a baseline for a new task.
func stampLike(before, t todo.Todo, existed bool, clock *hlc.Clock, now time.Time) map[string]hlc.Stamp {
	fresh := clock.Now(now)
	out := map[string]hlc.Stamp{}
	if !existed {
		for _, f := range todo.Fields {
			out[f.Key] = fresh
		}
		for _, k := range t.SetKeys() {
			out[k] = fresh
		}
		out[todo.SetsKey] = fresh
		return out
	}
	for _, f := range todo.Fields {
		if f.Same(&before, &t) {
			out[f.Key] = before.Stamp(f.Key)
		} else {
			out[f.Key] = fresh
		}
	}
	keys := append(before.SetKeys(), t.SetKeys()...)
	for _, k := range keys {
		if before.HasMember(k) == t.HasMember(k) {
			if s := before.Stamp(k); s != "" {
				out[k] = s
			}
		} else {
			out[k] = fresh
		}
	}
	if s := before.Stamp(todo.SetsKey); s != "" {
		out[todo.SetsKey] = s
	}
	return out
}

func (rp *replica) list() []todo.Todo {
	var out []todo.Todo
	for _, t := range rp.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// canon is a set's order-insensitive form: each task's canonical JSON, by ID.
func canon(ts []todo.Todo) string {
	sorted := append([]todo.Todo(nil), ts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var b bytes.Buffer
	for _, t := range sorted {
		b.Write(CanonicalJSON(t))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestMergeIsACRDT(t *testing.T) {
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for seed := int64(1); seed <= 2000; seed++ {
		r := rand.New(rand.NewSource(seed))
		base := origin(r, t0)
		var reps []*replica
		for i, node := range []string{"x", "y", "z"} {
			tasks := map[string]todo.Todo{}
			for id, task := range base {
				tasks[id] = task
			}
			reps = append(reps, &replica{
				clock: hlc.New(node, ""), legacy: r.Intn(4) == 0,
				// Clocks disagree by up to a second, either way.
				now:   t0.Add(time.Duration(r.Intn(2000)-1000+i) * time.Millisecond),
				tasks: tasks,
			})
		}
		for i := 0; i < 12; i++ {
			reps[r.Intn(3)].edit(r)
		}
		x, y, z := reps[0].list(), reps[1].list(), reps[2].list()

		fail := func(law string) {
			t.Fatalf("seed %d: Merge is not %s", seed, law)
		}
		if canon(Merge(x, y)) != canon(Merge(y, x)) {
			fail("commutative")
		}
		if canon(Merge(Merge(x, y), z)) != canon(Merge(x, Merge(y, z))) {
			fail("associative")
		}
		if canon(Merge(x, x)) != canon(x) {
			fail("idempotent on one set")
		}
		xy := Merge(x, y)
		if canon(Merge(xy, y)) != canon(xy) || canon(Merge(x, xy)) != canon(xy) {
			fail("idempotent on what it already merged")
		}
	}
}

// With stamps, an edit to one field survives any other device's edits to
// other fields of the same task, in every sync order.
func TestMergeKeepsEditsToDifferentFieldsInEveryOrder(t *testing.T) {
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	origin := todo.Todo{ID: "a", Title: "Pay rent", CreatedAt: t0, ModifiedAt: t0}
	edits := []struct {
		node   string
		change func(*todo.Todo)
		check  func(todo.Todo) bool
	}{
		{"x", func(t *todo.Todo) { t.DueDate = t0.Add(48 * time.Hour) }, func(t todo.Todo) bool { return t.DueDate.Equal(t0.Add(48 * time.Hour)) }},
		{"y", func(t *todo.Todo) { t.Priority = todo.PriorityHigh }, func(t todo.Todo) bool { return t.Priority == todo.PriorityHigh }},
		{"z", func(t *todo.Todo) { t.Tags = []string{"urgent"} }, func(t todo.Todo) bool { return len(t.Tags) == 1 && t.Tags[0] == "urgent" }},
	}
	var versions [][]todo.Todo
	for i, e := range edits {
		v := origin
		e.change(&v)
		now := t0.Add(time.Duration(i+1) * time.Second)
		v.ModifiedAt = now
		v.Stamps = stampLike(origin, v, true, hlc.New(e.node, ""), now)
		versions = append(versions, []todo.Todo{v})
	}
	for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		got := Merge(Merge(versions[order[0]], versions[order[1]]), versions[order[2]])[0]
		for i, e := range edits {
			if !e.check(got) {
				t.Errorf("order %v: edit %d (%s) lost", order, i, e.node)
			}
		}
	}
}
