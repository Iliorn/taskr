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
