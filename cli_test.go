package main

import (
	"strings"
	"testing"
	"time"

	"taskr/todo"
)

func TestFindByPrefix(t *testing.T) {
	a := todo.New("a")
	a.ID = "abc12345-aaaa"
	b := todo.New("b")
	b.ID = "abcdef99-bbbb"
	c := todo.New("c")
	c.ID = "fedcba00-cccc"
	todos := []todo.Todo{a, b, c}

	t.Run("unique short prefix", func(t *testing.T) {
		got, err := findByPrefix(todos, "fed")
		if err != nil || got.ID != c.ID {
			t.Fatalf("got=%v err=%v, want %s", got, err, c.ID)
		}
	})

	t.Run("ambiguous prefix errors", func(t *testing.T) {
		if _, err := findByPrefix(todos, "abc"); err == nil {
			t.Error("expected ambiguity error, got nil")
		}
	})

	t.Run("no match errors", func(t *testing.T) {
		if _, err := findByPrefix(todos, "zzz"); err == nil {
			t.Error("expected no-match error, got nil")
		}
	})

	t.Run("empty prefix rejected", func(t *testing.T) {
		if _, err := findByPrefix(todos, ""); err == nil {
			t.Error("expected empty-prefix error, got nil")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		got, err := findByPrefix(todos, "FED")
		if err != nil || got.ID != c.ID {
			t.Fatalf("got=%v err=%v, want %s", got, err, c.ID)
		}
	})

	t.Run("full id matches one", func(t *testing.T) {
		got, err := findByPrefix(todos, a.ID)
		if err != nil || got.ID != a.ID {
			t.Fatalf("got=%v err=%v, want %s", got, err, a.ID)
		}
	})
}

func TestSplitFlagsAndPositionals(t *testing.T) {
	valueFlags := map[string]bool{"due": true, "p": true, "size": true}

	cases := []struct {
		name      string
		in        []string
		wantFlags []string
		wantPos   []string
	}{
		{
			name:      "title first then flags",
			in:        []string{"Buy", "milk", "--size=s", "--p=h"},
			wantFlags: []string{"--size=s", "--p=h"},
			wantPos:   []string{"Buy", "milk"},
		},
		{
			name:      "interspersed flags consume next arg",
			in:        []string{"--p", "high", "Title", "here", "--size=s"},
			wantFlags: []string{"--p", "high", "--size=s"},
			wantPos:   []string{"Title", "here"},
		},
		{
			name:      "double-dash freezes the rest as positional",
			in:        []string{"--size=s", "--", "--p=h", "still", "title"},
			wantFlags: []string{"--size=s"},
			wantPos:   []string{"--p=h", "still", "title"},
		},
		{
			name:      "no flags",
			in:        []string{"Just", "a", "title"},
			wantFlags: nil,
			wantPos:   []string{"Just", "a", "title"},
		},
		{
			// `taskr comment <ref> -` uses bare dash for stdin; if the splitter
			// classifies it as a flag, the stdin path never fires.
			name:      "bare dash is positional, not a flag",
			in:        []string{"abc123", "-"},
			wantFlags: nil,
			wantPos:   []string{"abc123", "-"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotFlags, gotPos := splitFlagsAndPositionals(c.in, valueFlags)
			if !sliceEq(gotFlags, c.wantFlags) {
				t.Errorf("flags = %v, want %v", gotFlags, c.wantFlags)
			}
			if !sliceEq(gotPos, c.wantPos) {
				t.Errorf("positionals = %v, want %v", gotPos, c.wantPos)
			}
		})
	}
}

func sliceEq(a, b []string) bool {
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

// TestFindTaskByRefFallsBackToTitleSubstring covers the new ergonomic path:
// when no task ID matches the query, fall back to a case-insensitive title
// substring search. Without this, every CLI mutation requires looking up
// a UUID prefix from `taskr list` first — too much friction for daily use.
func TestFindTaskByRefFallsBackToTitleSubstring(t *testing.T) {
	a := todo.New("Buy milk")
	a.ID = "1aaaaa00-aaaa"
	b := todo.New("Call landlord")
	b.ID = "2bbbbb00-bbbb"
	c := todo.New("Read RFC")
	c.ID = "3ccccc00-cccc"
	todos := []todo.Todo{a, b, c}

	t.Run("substring matches one", func(t *testing.T) {
		got, err := findTaskByRef(todos, "milk")
		if err != nil || got.ID != a.ID {
			t.Fatalf("got=%v err=%v, want a", got, err)
		}
	})

	t.Run("substring case-insensitive", func(t *testing.T) {
		got, err := findTaskByRef(todos, "LANDLORD")
		if err != nil || got.ID != b.ID {
			t.Fatalf("got=%v err=%v, want b", got, err)
		}
	})

	t.Run("substring matches multiple errors", func(t *testing.T) {
		// Add a second task containing "milk" to force ambiguity.
		more := append(todos, todo.New("Buy more milk"))
		if _, err := findTaskByRef(more, "milk"); err == nil {
			t.Error("expected ambiguity error")
		}
	})

	t.Run("id-prefix wins over title containing same chars", func(t *testing.T) {
		// "1aa" matches a's id prefix; if a title contained "1aa" too, the
		// id path should still win for determinism. Construct that scenario.
		shared := todo.New("contains 1aa in title")
		shared.ID = "4ddddd00-dddd"
		got, err := findTaskByRef(append(todos, shared), "1aa")
		if err != nil || got.ID != a.ID {
			t.Fatalf("got=%v err=%v, want a (id-prefix path wins)", got, err)
		}
	})

	t.Run("no match anywhere", func(t *testing.T) {
		if _, err := findTaskByRef(todos, "wombat"); err == nil {
			t.Error("expected no-match error")
		}
	})

	t.Run("whitespace-only ref rejected", func(t *testing.T) {
		if _, err := findTaskByRef(todos, "   "); err == nil {
			t.Error("expected empty-ref error")
		}
	})
}

// TestShowAcceptsTrailingJSONFlag locks in the fix for the bug where
// `taskr show <ref> --json` failed because stdlib flag.Parse stops at the
// first non-flag token, leaving --json as an unexpected second positional.
// cliShow now routes through splitFlagsAndPositionals; this test guards that
// path at the helper level so a future refactor can't silently revert it.
func TestShowAcceptsTrailingJSONFlag(t *testing.T) {
	flags, positionals := splitFlagsAndPositionals([]string{"1ecdcc90", "--json"}, nil)
	if !sliceEq(flags, []string{"--json"}) {
		t.Errorf("flags = %v, want [--json]", flags)
	}
	if !sliceEq(positionals, []string{"1ecdcc90"}) {
		t.Errorf("positionals = %v, want [1ecdcc90]", positionals)
	}
}

// TestStartTimerOnRunningTaskRotatesEntry pins down the underlying todo
// behavior the cliStart guard relies on: an unguarded re-StartTimer call on
// an already-running task closes the existing entry and opens a new zero-gap
// one. The CLI now short-circuits before reaching this path; if todo's
// semantics ever change, the cliStart fix can be relaxed.
func TestStartTimerOnRunningTaskRotatesEntry(t *testing.T) {
	x := todo.New("track")
	x.StartTimer()
	if len(x.TimeEntries) != 1 {
		t.Fatalf("setup: want 1 entry, got %d", len(x.TimeEntries))
	}
	first := x.TimeEntries[0].ID
	x.StartTimer()
	if len(x.TimeEntries) != 2 {
		t.Fatalf("want 2 entries after re-start (proves the footgun), got %d", len(x.TimeEntries))
	}
	if x.TimeEntries[0].ID != first {
		t.Errorf("first entry id changed: was %s, now %s", first, x.TimeEntries[0].ID)
	}
	if x.TimeEntries[0].StoppedAt.IsZero() {
		t.Error("first entry should be stopped after second StartTimer")
	}
	if !x.TimeEntries[1].IsRunning() {
		t.Error("second entry should be the running one")
	}
}

func TestIsCLICommand(t *testing.T) {
	cases := map[string]bool{
		"add": true, "list": true, "ls": true, "done": true, "top": true,
		"show": true, "edit": true, "delete": true, "rm": true, "comment": true,
		"search": true, "tags": true, "projects": true,
		"help": true, "-h": true, "--help": true, "--version": true,
		"foo": false, "": false, "Add": false,
	}
	for in, want := range cases {
		if got := isCLICommand(in); got != want {
			t.Errorf("isCLICommand(%q)=%v, want %v", in, got, want)
		}
	}
}

// TestFilterTopLevel exercises every combination of filter the list / search
// verbs expose, so a future refactor of filterTopLevel can't silently change
// inclusion semantics.
func TestFilterTopLevel(t *testing.T) {
	mk := func(title, project string, tags []string, status todo.Status, due time.Time) todo.Todo {
		x := todo.New(title)
		x.Project = project
		for _, tag := range tags {
			x.AddTag(tag)
		}
		x.Status = status
		x.DueDate = due
		if status == todo.Done {
			x.CompletedAt = time.Now()
		}
		return x
	}
	yesterday := startOfDay(time.Now()).AddDate(0, 0, -1)
	today := startOfDay(time.Now())
	nextWeek := startOfDay(time.Now()).AddDate(0, 0, 7)

	a := mk("Buy milk", "groceries", []string{"shop", "urgent"}, todo.Pending, time.Time{})
	b := mk("Read RFC", "work", []string{"reading"}, todo.Pending, nextWeek)
	c := mk("Call landlord", "", []string{"urgent"}, todo.Pending, today)
	d := mk("Pay rent", "", []string{"urgent"}, todo.Pending, yesterday) // overdue
	e := mk("Old shopping", "groceries", []string{"shop"}, todo.Done, time.Time{})
	sub := mk("subtask", "", nil, todo.Pending, time.Time{})
	sub.ParentID = a.ID
	todos := []todo.Todo{a, b, c, d, e, sub}

	titlesOf := func(rows []todo.Todo) []string {
		out := make([]string, len(rows))
		for i := range rows {
			out[i] = rows[i].Title
		}
		return out
	}

	t.Run("default excludes done + subtasks", func(t *testing.T) {
		got := filterTopLevel(todos, listFilterOpts{})
		if len(got) != 4 {
			t.Fatalf("want 4 rows (a,b,c,d), got %d: %v", len(got), titlesOf(got))
		}
	})
	t.Run("includeDone surfaces completed", func(t *testing.T) {
		got := filterTopLevel(todos, listFilterOpts{includeDone: true})
		if len(got) != 5 {
			t.Errorf("want 5 with done included, got %d", len(got))
		}
	})
	t.Run("focus = overdue + today only", func(t *testing.T) {
		got := filterTopLevel(todos, listFilterOpts{focus: true})
		names := titlesOf(got)
		want := map[string]bool{"Call landlord": true, "Pay rent": true}
		if len(names) != len(want) {
			t.Fatalf("want 2 focus rows, got %d: %v", len(names), names)
		}
		for _, n := range names {
			if !want[n] {
				t.Errorf("unexpected focus row: %s", n)
			}
		}
	})
	t.Run("tag filter case-insensitive", func(t *testing.T) {
		got := filterTopLevel(todos, listFilterOpts{tag: "URGENT"})
		if len(got) != 3 {
			t.Errorf("want 3 urgent rows, got %d: %v", len(got), titlesOf(got))
		}
	})
	t.Run("project filter case-insensitive equality", func(t *testing.T) {
		got := filterTopLevel(todos, listFilterOpts{project: "Groceries"})
		if len(got) != 1 || got[0].Title != "Buy milk" {
			t.Errorf("want only 'Buy milk', got %v", titlesOf(got))
		}
	})
	t.Run("search filter case-insensitive substring", func(t *testing.T) {
		got := filterTopLevel(todos, listFilterOpts{search: "MILK"})
		if len(got) != 1 || got[0].Title != "Buy milk" {
			t.Errorf("want only 'Buy milk', got %v", titlesOf(got))
		}
	})
	t.Run("combined filters are AND", func(t *testing.T) {
		got := filterTopLevel(todos, listFilterOpts{tag: "urgent", project: "groceries"})
		if len(got) != 1 || got[0].Title != "Buy milk" {
			t.Errorf("want only 'Buy milk' (urgent ∩ groceries), got %v", titlesOf(got))
		}
	})
}

// TestResolveRefs covers the batch verb's ref-resolution contract: succeed on
// all refs or fail before any mutation; collapse duplicates silently so
// `done abc abc` is one done, not an error.
func TestResolveRefs(t *testing.T) {
	a := todo.New("Buy milk")
	a.ID = "aaaa1111-aaaa"
	b := todo.New("Call landlord")
	b.ID = "bbbb2222-bbbb"
	todos := []todo.Todo{a, b}

	t.Run("two refs", func(t *testing.T) {
		got, err := resolveRefs(todos, []string{"aaaa", "bbbb"})
		if err != nil || len(got) != 2 {
			t.Fatalf("want 2 targets, got %d err=%v", len(got), err)
		}
	})
	t.Run("duplicate refs collapse", func(t *testing.T) {
		got, err := resolveRefs(todos, []string{"aaaa", "aaaa", "milk"})
		if err != nil || len(got) != 1 {
			t.Fatalf("want 1 target after dedup, got %d err=%v", len(got), err)
		}
	})
	t.Run("first failure aborts the batch", func(t *testing.T) {
		if _, err := resolveRefs(todos, []string{"aaaa", "nope"}); err == nil {
			t.Error("want error from missing ref")
		}
	})
}

// TestTrackedTodayDuration covers the four overlap cases the stats one-liner
// has to get right: fully today, ends today (started yesterday), starts today
// (still running), and entirely outside today.
func TestTrackedTodayDuration(t *testing.T) {
	now := time.Date(2026, 6, 18, 14, 0, 0, 0, time.UTC)
	today := startOfDay(now)
	yesterday := today.AddDate(0, 0, -1)

	make := func(start, stop time.Time) todo.Todo {
		x := todo.New("t")
		x.TimeEntries = []todo.TimeEntry{{ID: "e", StartedAt: start, StoppedAt: stop}}
		return x
	}

	cases := []struct {
		name string
		t    todo.Todo
		want time.Duration
	}{
		{"fully today", make(today.Add(9*time.Hour), today.Add(11*time.Hour)), 2 * time.Hour},
		{"crosses midnight into today", make(yesterday.Add(23*time.Hour), today.Add(1*time.Hour)), 1 * time.Hour},
		{"running entry counts up to now", make(today.Add(12*time.Hour), time.Time{}), 2 * time.Hour},
		{"entirely yesterday", make(yesterday.Add(8*time.Hour), yesterday.Add(9*time.Hour)), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := trackedTodayDuration([]todo.Todo{c.t}, now)
			if got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}

	t.Run("sums across multiple todos and entries", func(t *testing.T) {
		x := todo.New("x")
		x.TimeEntries = []todo.TimeEntry{
			{StartedAt: today.Add(9 * time.Hour), StoppedAt: today.Add(10 * time.Hour)},
			{StartedAt: today.Add(13 * time.Hour), StoppedAt: time.Time{}}, // running, 1h so far
		}
		y := todo.New("y")
		y.TimeEntries = []todo.TimeEntry{
			{StartedAt: today.Add(11 * time.Hour), StoppedAt: today.Add(11*time.Hour + 30*time.Minute)},
		}
		got := trackedTodayDuration([]todo.Todo{x, y}, now)
		want := 1*time.Hour + 1*time.Hour + 30*time.Minute
		if got != want {
			t.Errorf("got %v want %v", got, want)
		}
	})
}

// TestDoneCommentSplitter verifies that `done <ref>... --comment="why"` keeps
// the refs as positionals and the comment as a flag value, with the comment
// surviving spaces. Guards the splitFlagsAndPositionals wiring on cliDone.
func TestDoneCommentSplitter(t *testing.T) {
	flags, positionals := splitFlagsAndPositionals(
		[]string{"abc", "def", "--comment=finished sprint"},
		map[string]bool{"comment": true},
	)
	if !sliceEq(flags, []string{"--comment=finished sprint"}) {
		t.Errorf("flags = %v, want [--comment=finished sprint]", flags)
	}
	if !sliceEq(positionals, []string{"abc", "def"}) {
		t.Errorf("positionals = %v, want [abc def]", positionals)
	}
	// Spaced form: --comment "why" — value consumes next arg.
	flags, positionals = splitFlagsAndPositionals(
		[]string{"abc", "--comment", "finished sprint", "def"},
		map[string]bool{"comment": true},
	)
	if !sliceEq(flags, []string{"--comment", "finished sprint"}) {
		t.Errorf("spaced form flags = %v", flags)
	}
	if !sliceEq(positionals, []string{"abc", "def"}) {
		t.Errorf("spaced form positionals = %v", positionals)
	}
}

// TestCommentTextFromPositionals: literal text path joins with spaces;
// descendantIDsInSlice must return rootID and every transitive subtask, so
// cliDelete can cascade the tombstone set and not strand subtasks with a
// parent_id pointing at a deleted parent (the DOGFOOD-child orphan bug).
func TestDescendantIDsInSliceCascadesSubtree(t *testing.T) {
	root := todo.New("root")
	root.ID = "root"
	child := todo.NewSubtask("child", "root")
	child.ID = "child"
	grand := todo.NewSubtask("grand", "child")
	grand.ID = "grand"
	sibling := todo.New("sibling") // unrelated, must not appear
	sibling.ID = "sibling"

	got := descendantIDsInSlice([]todo.Todo{root, child, grand, sibling}, "root")
	want := map[string]bool{"root": true, "child": true, "grand": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected id %q in descendants", id)
		}
	}
}

// the "-" sentinel reads stdin and trims one trailing newline (heredocs
// typically include one).
func TestCommentTextFromPositionals(t *testing.T) {
	got, err := commentTextFromPositionals([]string{"hello", "world"}, strings.NewReader(""))
	if err != nil || got != "hello world" {
		t.Errorf("literal path: got=%q err=%v", got, err)
	}
	got, err = commentTextFromPositionals([]string{"-"}, strings.NewReader("piped text\n"))
	if err != nil || got != "piped text" {
		t.Errorf("stdin path: got=%q err=%v", got, err)
	}
	got, err = commentTextFromPositionals([]string{"-"}, strings.NewReader("line one\nline two\n"))
	if err != nil || got != "line one\nline two" {
		t.Errorf("multiline stdin: got=%q err=%v", got, err)
	}
}
