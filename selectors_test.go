package main

import (
	"reflect"
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

	active, done := selectActiveDone(todos, "", false, taskSortDueDate, historySortCompleted)
	if got := ids(active); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("active = %v, want [b a] (sorted by due date, subtask excluded)", got)
	}
	if got := ids(done); len(got) != 1 || got[0] != "c" {
		t.Fatalf("done = %v, want [c]", got)
	}
}

// The done list has its own sort, independent of the active taskSort: by
// completion time (most recent first) or title (A→Z). taskSort must not leak
// into history ordering.
func TestSelectActiveDoneHistorySort(t *testing.T) {
	mk := func(id, title string, completed time.Time) todo.Todo {
		d := mkTodo(id, title, todo.Done)
		d.CompletedAt = completed
		return d
	}
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	a := mk("a", "zebra", base)                  // oldest, last alphabetically
	b := mk("b", "apple", base.AddDate(0, 0, 1)) // middle
	c := mk("c", "mango", base.AddDate(0, 0, 2)) // newest
	todos := []todo.Todo{a, b, c}

	// Completed mode: most recent first, regardless of the active sort mode.
	_, done := selectActiveDone(todos, "", false, taskSortSequence, historySortCompleted)
	if got := ids(done); len(got) != 3 || got[0] != "c" || got[1] != "b" || got[2] != "a" {
		t.Fatalf("history completed = %v, want [c b a] (most recent first)", got)
	}

	// Alpha mode: title A→Z.
	_, done = selectActiveDone(todos, "", false, taskSortSize, historySortAlpha)
	if got := ids(done); len(got) != 3 || got[0] != "b" || got[1] != "c" || got[2] != "a" {
		t.Fatalf("history alpha = %v, want [b c a] (apple, mango, zebra)", got)
	}
}

// A trivial blocker (no due date) should be lifted above the urgent task that
// depends on it, so the prerequisite surfaces right before the work it gates.
func TestDependencyBoostLiftsBlockerAboveDependent(t *testing.T) {
	now := time.Now()
	blocker := mkTodo("a", "get sign-off", todo.Pending) // low score on its own
	urgent := mkTodo("b", "deploy release", todo.Pending)
	urgent.DueDate = now // due today → high urgency
	urgent.Priority = todo.PriorityHigh
	urgent.Dependencies = []string{"a"}

	// Without the boost, urgent's score dwarfs the blocker's.
	if sequenceScore(&blocker) >= sequenceScore(&urgent) {
		t.Fatalf("precondition: blocker raw score should be below urgent")
	}

	active, _ := selectActiveDone([]todo.Todo{blocker, urgent}, "", false, taskSortSequence, historySortCompleted)
	if got := ids(active); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("active = %v, want [a b] (blocker lifted above its dependent)", got)
	}
}

// A→B→C chain (C depends on B depends on A): an urgent C should lift the whole
// prerequisite chain, in dependency order, above itself.
func TestDependencyBoostTransitiveChain(t *testing.T) {
	now := time.Now()
	a := mkTodo("a", "a", todo.Pending)
	b := mkTodo("b", "b", todo.Pending)
	b.Dependencies = []string{"a"}
	c := mkTodo("c", "c", todo.Pending)
	c.DueDate = now
	c.Priority = todo.PriorityHigh
	c.Dependencies = []string{"b"}

	active, _ := selectActiveDone([]todo.Todo{a, b, c}, "", false, taskSortSequence, historySortCompleted)
	if got := ids(active); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("active = %v, want [a b c] (chain lifted in dependency order)", got)
	}
}

// A dependency cycle must terminate (and not panic) rather than recurse forever.
func TestDependencyBoostCycleSafe(t *testing.T) {
	a := mkTodo("a", "a", todo.Pending)
	a.Dependencies = []string{"b"}
	b := mkTodo("b", "b", todo.Pending)
	b.Dependencies = []string{"a"}
	// Just assert it returns; a non-terminating walk would hang the test.
	selectActiveDone([]todo.Todo{a, b}, "", false, taskSortSequence, historySortCompleted)
}

// The dependency picker must hide tasks that would close a loop: the current
// task itself and anything that already depends on it (transitively). Unrelated
// tasks stay selectable, and a pre-existing cycle elsewhere must not hang.
func TestLoopingDepCandidates(t *testing.T) {
	a := mkTodo("a", "a", todo.Pending)
	b := mkTodo("b", "b", todo.Pending)
	b.Dependencies = []string{"a"} // b depends on a
	c := mkTodo("c", "c", todo.Pending)
	c.Dependencies = []string{"b"} // c -> b -> a, so c depends on a transitively
	free := mkTodo("free", "free", todo.Pending)
	// A pre-existing cycle unrelated to a — proves the walk terminates.
	d := mkTodo("d", "d", todo.Pending)
	d.Dependencies = []string{"e"}
	e := mkTodo("e", "e", todo.Pending)
	e.Dependencies = []string{"d"}

	tasks := map[string]*todo.Todo{
		"a": &a, "b": &b, "c": &c, "free": &free, "d": &d, "e": &e,
	}
	ex := loopingDepCandidates(tasks, "a")

	for _, id := range []string{"a", "b", "c"} {
		if !ex[id] {
			t.Errorf("want %q excluded (would loop with a), but it wasn't", id)
		}
	}
	for _, id := range []string{"free", "d", "e"} {
		if ex[id] {
			t.Errorf("want %q selectable as a dependency of a, but it was excluded", id)
		}
	}
}

func TestSelectActiveDoneSearch(t *testing.T) {
	p1 := mkTodo("a", "buy milk", todo.Pending)
	p2 := mkTodo("b", "walk dog", todo.Pending)
	active, _ := selectActiveDone([]todo.Todo{p1, p2}, "milk", false, taskSortDueDate, historySortCompleted)
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
		_, want := selectActiveDone(base, "", false, mode, historySortCompleted)
		for shuffle, perm := range [][]int{{3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}} {
			in := make([]todo.Todo, len(base))
			for i, p := range perm {
				in[i] = base[p]
			}
			_, got := selectActiveDone(in, "", false, mode, historySortCompleted)
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
	active, _ := selectActiveDone([]todo.Todo{overdue, future}, "", true, taskSortDueDate, historySortCompleted)
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

// The non-allocating active-list helpers (length, index-of, at, window) must
// agree exactly with the materialized visibleActiveTasks they replaced on the
// hot path, in both collapsed and expanded states.
func TestVisibleActiveHelpersMatchFull(t *testing.T) {
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

	check := func(label string) {
		full := m.visibleActiveTasks()
		if got := m.visibleActiveLen(); got != len(full) {
			t.Fatalf("%s: visibleActiveLen=%d, want %d", label, got, len(full))
		}
		for i, want := range full {
			if at := m.visibleActiveAt(i); at == nil || at.ID != want.ID {
				t.Fatalf("%s: visibleActiveAt(%d)=%v, want %s", label, i, at, want.ID)
			}
			if idx := m.visibleActiveIndexOf(want.ID); idx != i {
				t.Fatalf("%s: visibleActiveIndexOf(%s)=%d, want %d", label, want.ID, idx, i)
			}
		}
		if m.visibleActiveAt(len(full)) != nil {
			t.Fatalf("%s: visibleActiveAt past end should be nil", label)
		}
		if idx := m.visibleActiveIndexOf("nope"); idx != -1 {
			t.Fatalf("%s: visibleActiveIndexOf(missing)=%d, want -1", label, idx)
		}
		// Every sub-window must equal the corresponding slice of the full list.
		for start := 0; start <= len(full); start++ {
			for end := start; end <= len(full); end++ {
				win := m.visibleActiveWindow(start, end)
				if !reflect.DeepEqual(idsOf(win), idsOf(full[start:end])) {
					t.Fatalf("%s: window[%d:%d]=%v, want %v",
						label, start, end, idsOf(win), idsOf(full[start:end]))
				}
			}
		}
	}

	check("collapsed")
	m.expandedTasks["p"] = true
	check("expanded")
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
		{"bymlk", true}, // fuzzy subsequence: B-uy M-i-L-K
		{"bmm", false},  // subsequence still ordered: no second 'm' after milk
		{"#shop", true}, // tag substring match
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

// TestCompileSearchFields covers the field-token query surface: project, priority,
// due-date comparisons, the overdue keyword, and an ANDed field+title combo.
func TestCompileSearchFields(t *testing.T) {
	yesterday := startOfDay(time.Now().AddDate(0, 0, -1))
	tomorrow := startOfDay(time.Now().AddDate(0, 0, 1))

	deploy := mkTodo("a", "Deploy release", todo.Pending)
	deploy.Project = "Work"
	deploy.Priority = todo.PriorityHigh
	deploy.DueDate = tomorrow

	taxes := mkTodo("b", "File taxes", todo.Pending)
	taxes.Project = "Personal"
	taxes.Priority = todo.PriorityLow
	taxes.DueDate = yesterday // overdue

	cases := []struct {
		q    string
		a, b bool // expected match for deploy, taxes
		desc string
	}{
		{"@work", true, false, "project substring"},
		{"p:high", true, false, "priority high"},
		{"p:low", false, true, "priority low"},
		{"overdue", false, true, "overdue keyword"},
		{"due:<today", false, true, "due before today"},
		{"due:>today", true, false, "due after today"},
		{"@work p:high", true, false, "project AND priority"},
		{"@work dply", true, false, "field AND fuzzy title"},
		{"@work taxes", false, false, "field AND title both required"},
		{"p:bogus", false, false, "unparseable priority falls back to title word"},
	}
	for _, c := range cases {
		match := compileSearch(c.q)
		if got := match(deploy); got != c.a {
			t.Errorf("%s: compileSearch(%q)(deploy) = %v, want %v", c.desc, c.q, got, c.a)
		}
		if got := match(taxes); got != c.b {
			t.Errorf("%s: compileSearch(%q)(taxes) = %v, want %v", c.desc, c.q, got, c.b)
		}
	}
}
