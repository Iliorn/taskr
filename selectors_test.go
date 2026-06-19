package main

import (
	"testing"
	"time"

	"taskr/todo"
)

func mkTodo(id, title string, status todo.Status) todo.Todo {
	t := todo.New(title)
	t.ID = id
	t.Status = status
	return t
}

func ids(ts []todo.Todo) []string {
	r := make([]string, len(ts))
	for i, t := range ts {
		r[i] = t.ID
	}
	return r
}

func learnIDs(ls []learningView) []string {
	r := make([]string, len(ls))
	for i, l := range ls {
		r[i] = l.ID
	}
	return r
}

func TestSelectActiveDoneFilterAndSort(t *testing.T) {
	now := time.Now()
	a := mkTodo("a", "alpha", todo.Pending)
	a.DueDate = now.AddDate(0, 0, 2)
	b := mkTodo("b", "beta", todo.Pending)
	b.DueDate = now.AddDate(0, 0, 1)
	c := mkTodo("c", "done one", todo.Done)
	sub := mkTodo("s", "subtask", todo.Pending)
	sub.ParentID = "a" // subtasks are excluded from the top-level lists
	todos := []todo.Todo{a, b, c, sub}

	active, done := selectActiveDone(todos, "", false, taskSortDueDate)
	if got := ids(active); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("active = %v, want [b a] (sorted by due date, subtask excluded)", got)
	}
	if got := ids(done); len(got) != 1 || got[0] != "c" {
		t.Fatalf("done = %v, want [c]", got)
	}
}

func TestSelectActiveDoneSearch(t *testing.T) {
	p1 := mkTodo("a", "buy milk", todo.Pending)
	p2 := mkTodo("b", "walk dog", todo.Pending)
	active, _ := selectActiveDone([]todo.Todo{p1, p2}, "milk", false, taskSortDueDate)
	if got := ids(active); len(got) != 1 || got[0] != "a" {
		t.Fatalf("search active = %v, want [a]", got)
	}
}

// TestSelectActiveDoneStableUnderShuffle guards the no-shuffle contract:
// done tasks sharing identical sort keys (score 0 + CreatedAt) must come out
// in the same order regardless of the input slice order. The underlying bug
// was that Store.allTodos() returns map-iteration order, so the search-input
// cursor blink would dirty the cache, rebuild, and reshuffle tied done tasks
// on every frame. The fix uses ID as the absolute final sort tiebreaker.
func TestSelectActiveDoneStableUnderShuffle(t *testing.T) {
	mkDone := func(id string) todo.Todo {
		d := mkTodo(id, "task-"+id, todo.Done)
		// All ties on CreatedAt — the shape of the bug.
		d.CreatedAt = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		return d
	}
	base := []todo.Todo{mkDone("a"), mkDone("b"), mkDone("c"), mkDone("d")}

	for _, mode := range []taskSortMode{taskSortSequence, taskSortDueDate, taskSortSize} {
		_, want := selectActiveDone(base, "", false, mode)
		for shuffle, perm := range [][]int{{3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}} {
			in := make([]todo.Todo, len(base))
			for i, p := range perm {
				in[i] = base[p]
			}
			_, got := selectActiveDone(in, "", false, mode)
			if w, g := ids(want), ids(got); !equalStrings(w, g) {
				t.Errorf("mode=%v shuffle=%d: got %v, want %v", mode, shuffle, g, w)
			}
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSelectActiveDoneFocusFilter(t *testing.T) {
	now := time.Now()
	overdue := mkTodo("o", "overdue", todo.Pending)
	overdue.DueDate = now.AddDate(0, 0, -1)
	future := mkTodo("f", "future", todo.Pending)
	future.DueDate = now.AddDate(0, 0, 10)
	// focus filter keeps only overdue/due-today
	active, _ := selectActiveDone([]todo.Todo{overdue, future}, "", true, taskSortDueDate)
	if got := ids(active); len(got) != 1 || got[0] != "o" {
		t.Fatalf("focus active = %v, want [o]", got)
	}
}

func TestSelectSortedTags(t *testing.T) {
	t1 := mkTodo("a", "x", todo.Pending)
	t1.Tags = []string{"work", "urgent"}
	t2 := mkTodo("b", "y", todo.Pending)
	t2.Tags = []string{"work"}
	t3 := mkTodo("c", "z", todo.Done) // untagged + done
	todos := []todo.Todo{t1, t2, t3}
	stats := computeTagStats(todos)

	sorted, ut, ud := selectSortedTags(todos, tagSortAlpha, stats)
	if len(sorted) != 2 || sorted[0] != "urgent" || sorted[1] != "work" {
		t.Fatalf("alpha tags = %v, want [urgent work]", sorted)
	}
	if ut != 1 || ud != 1 {
		t.Fatalf("untagged total=%d done=%d, want 1/1", ut, ud)
	}

	byCount, _, _ := selectSortedTags(todos, tagSortCount, stats)
	if byCount[0] != "work" {
		t.Fatalf("count tags = %v, want work first (2 > 1)", byCount)
	}
}

// Regression: subtasks must not feed Tags-tab counts, because pressing Enter
// on a row switches to the Tasks tab whose list (selectActiveDone) excludes
// subtasks — counting them would leave rows pointing at empty results.
func TestSelectSortedTagsSkipsSubtasks(t *testing.T) {
	parent := mkTodo("p", "parent", todo.Pending)
	parent.Tags = []string{"work"}
	untaggedSub := mkTodo("s1", "untagged sub", todo.Pending)
	untaggedSub.ParentID = "p"
	subOnlyTag := mkTodo("s2", "sub-only tag", todo.Pending)
	subOnlyTag.ParentID = "p"
	subOnlyTag.Tags = []string{"orphan"}

	sorted, ut, ud := selectSortedTags(
		[]todo.Todo{parent, untaggedSub, subOnlyTag}, tagSortAlpha, nil)
	if ut != 0 || ud != 0 {
		t.Fatalf("untagged total=%d done=%d, want 0/0 (subtask must not count)", ut, ud)
	}
	for _, tg := range sorted {
		if tg == "orphan" {
			t.Fatalf("subtask-only tag surfaced in Tags tab: %v", sorted)
		}
	}

	stats := computeTagStats([]todo.Todo{parent, untaggedSub, subOnlyTag})
	if _, ok := stats["orphan"]; ok {
		t.Errorf("computeTagStats included subtask-only tag: %v", stats)
	}
	if stats["work"].total != 1 {
		t.Errorf("work total = %d, want 1 (subtask must not count)", stats["work"].total)
	}
}

func TestSelectProjects(t *testing.T) {
	a := mkTodo("a", "x", todo.Pending)
	a.Project = "zeta"
	b := mkTodo("b", "y", todo.Pending)
	b.Project = "alpha"
	c := mkTodo("c", "z", todo.Pending)
	c.Project = "alpha" // duplicate
	d := mkTodo("d", "w", todo.Pending)
	todos := []todo.Todo{a, b, c, d}

	got := selectProjects(todos, "")
	if len(got) != 2 || got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("projects = %v, want [alpha zeta]", got)
	}
	if f := selectProjects(todos, "alph"); len(f) != 1 || f[0] != "alpha" {
		t.Fatalf("search projects = %v, want [alpha]", f)
	}
}

func TestSelectLearnings(t *testing.T) {
	now := time.Now()
	// Tags now come from the PARENT task, not the learning. Two tasks with
	// different tags exercise the derivation and the #tag search.
	a := mkTodo("a", "task A", todo.Pending)
	a.Tags = []string{"go"}
	a.Learnings = []todo.Learning{{ID: "l1", Text: "learned go maps", CreatedAt: now.Add(-2 * time.Hour)}}
	b := mkTodo("b", "task B", todo.Pending)
	b.Tags = []string{"go", "test"}
	b.Learnings = []todo.Learning{{ID: "l2", Text: "learned testing", CreatedAt: now.Add(-1 * time.Hour)}}
	todos := []todo.Todo{a, b}

	if got := learnIDs(selectLearnings(todos, "", learningSortDate)); len(got) != 2 || got[0] != "l2" {
		t.Fatalf("date sort = %v, want [l2 l1] (newest first)", got)
	}

	// Tags are derived from each learning's parent task.
	for _, l := range selectLearnings(todos, "", learningSortDate) {
		switch l.ID {
		case "l1":
			if len(l.Tags) != 1 || l.Tags[0] != "go" {
				t.Errorf("l1 tags = %v, want [go] (from parent A)", l.Tags)
			}
		case "l2":
			if len(l.Tags) != 2 {
				t.Errorf("l2 tags = %v, want [go test] (from parent B)", l.Tags)
			}
		}
	}

	if got := learnIDs(selectLearnings(todos, "maps", learningSortDate)); len(got) != 1 || got[0] != "l1" {
		t.Fatalf("text search = %v, want [l1]", got)
	}
	// #test matches only l2, whose parent (B) carries the 'test' tag.
	if got := learnIDs(selectLearnings(todos, "#test", learningSortDate)); len(got) != 1 || got[0] != "l2" {
		t.Fatalf("tag search = %v, want [l2]", got)
	}
	if got := selectLearnings(todos, "", learningSortAlpha); got[0].Text != "learned go maps" {
		t.Fatalf("alpha sort first = %q, want 'learned go maps'", got[0].Text)
	}
}

func TestSubtaskDerivation(t *testing.T) {
	now := time.Now()
	parent := mkTodo("p", "parent", todo.Pending)
	c1 := mkTodo("c1", "child one", todo.Pending)
	c1.ParentID = "p"
	c1.CreatedAt = now.Add(1 * time.Minute)
	c2 := mkTodo("c2", "child two", todo.Pending)
	c2.ParentID = "p"
	c2.CreatedAt = now.Add(2 * time.Minute)
	other := mkTodo("o", "unrelated", todo.Pending)

	m := newTestModel()
	// Insertion order is deliberately scrambled to prove the derivation orders
	// by CreatedAt, not map iteration order.
	for _, t := range []todo.Todo{c2, parent, other, c1} {
		m.add(t)
	}
	m.refreshCaches()

	if n := m.subtaskCount("p"); n != 2 {
		t.Fatalf("subtaskCount(p) = %d, want 2", n)
	}
	if got := m.subtaskIDs("p"); len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("subtaskIDs(p) = %v, want [c1 c2] (CreatedAt order)", got)
	}
	if m.subtaskCount("none") != 0 || len(m.subtaskIDs("none")) != 0 {
		t.Fatalf("a parent with no children should give count 0 / no ids")
	}
}

// descendantIDs walks the subtask tree depth-first so cascade-delete can
// tombstone every node under a root in one pass.
func TestDescendantIDsCollectsTransitiveSubtasks(t *testing.T) {
	now := time.Now()
	parent := mkTodo("p", "parent", todo.Pending)
	parent.CreatedAt = now
	c1 := mkTodo("c1", "child one", todo.Pending)
	c1.ParentID = "p"
	c1.CreatedAt = now.Add(1 * time.Minute)
	c2 := mkTodo("c2", "child two", todo.Pending)
	c2.ParentID = "p"
	c2.CreatedAt = now.Add(2 * time.Minute)
	g1 := mkTodo("g1", "grandchild", todo.Pending)
	g1.ParentID = "c1"
	g1.CreatedAt = now.Add(3 * time.Minute)
	other := mkTodo("o", "unrelated", todo.Pending)

	m := newTestModel()
	for _, td := range []todo.Todo{parent, c1, c2, g1, other} {
		m.add(td)
	}

	got := m.descendantIDs("p")
	want := map[string]bool{"p": true, "c1": true, "c2": true, "g1": true}
	if len(got) != len(want) {
		t.Fatalf("descendantIDs(p) = %v, want 4 ids", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id %q in descendantIDs(p) = %v", id, got)
		}
	}
	if got[0] != "p" {
		t.Errorf("descendantIDs should put the root first; got %v", got)
	}
	if leaf := m.descendantIDs("o"); len(leaf) != 1 || leaf[0] != "o" {
		t.Errorf("descendantIDs on a leaf should be [self]; got %v", leaf)
	}
}

// visibleActiveTasks interleaves the subtasks of an expanded parent inline
// with the active list, so the Tasks-tab cursor can land on them. Collapsed
// parents must hide their subtasks.
func TestVisibleActiveTasksFlatten(t *testing.T) {
	now := time.Now()
	parent := mkTodo("p", "parent", todo.Pending)
	parent.CreatedAt = now
	c1 := mkTodo("c1", "child one", todo.Pending)
	c1.ParentID = "p"
	c1.CreatedAt = now.Add(1 * time.Minute)
	c2 := mkTodo("c2", "child two", todo.Pending)
	c2.ParentID = "p"
	c2.CreatedAt = now.Add(2 * time.Minute)
	other := mkTodo("o", "unrelated", todo.Pending)
	other.CreatedAt = now.Add(3 * time.Minute)

	m := newTestModel()
	for _, td := range []todo.Todo{parent, c1, c2, other} {
		m.add(td)
	}
	m.refreshCaches()

	// Collapsed → only top-level rows.
	got := idsOf(m.visibleActiveTasks())
	if len(got) != 2 || got[0] != "p" || got[1] != "o" {
		t.Fatalf("collapsed visible = %v, want [p o]", got)
	}

	// Expanded → subtasks interleaved beneath their parent.
	m.expandedTasks["p"] = true
	got = idsOf(m.visibleActiveTasks())
	if len(got) != 4 || got[0] != "p" || got[1] != "c1" || got[2] != "c2" || got[3] != "o" {
		t.Fatalf("expanded visible = %v, want [p c1 c2 o]", got)
	}

	// currentTodo follows the flat cursor onto a subtask.
	m.tab = tabTasks
	m.cursor = 2
	if cur := m.currentTodo(); cur == nil || cur.ID != "c2" {
		t.Fatalf("currentTodo at flat cursor=2 = %v, want c2", cur)
	}
}

func idsOf(ts []todo.Todo) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}

func TestTodoMatchesSearch(t *testing.T) {
	x := mkTodo("a", "Buy Milk", todo.Pending)
	x.Tags = []string{"shopping"}
	cases := []struct {
		q    string
		want bool
	}{
		{"", true},
		{"milk", true}, // case-insensitive title match
		{"bread", false},
		{"#shop", true}, // tag prefix match
		{"#xyz", false},
		{untaggedKey, false}, // x has tags
	}
	for _, c := range cases {
		if got := todoMatchesSearch(x, c.q); got != c.want {
			t.Errorf("todoMatchesSearch(%q) = %v, want %v", c.q, got, c.want)
		}
	}
	if !todoMatchesSearch(mkTodo("b", "no tags", todo.Pending), untaggedKey) {
		t.Error("untagged task should match untaggedKey")
	}
}
