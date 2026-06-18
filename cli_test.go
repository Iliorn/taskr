package main

import (
	"testing"

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

func TestIsCLICommand(t *testing.T) {
	cases := map[string]bool{
		"add": true, "list": true, "ls": true, "done": true, "top": true,
		"show": true, "edit": true, "delete": true, "rm": true, "comment": true,
		"help": true, "-h": true, "--help": true, "--version": true,
		"foo": false, "": false, "Add": false,
	}
	for in, want := range cases {
		if got := isCLICommand(in); got != want {
			t.Errorf("isCLICommand(%q)=%v, want %v", in, got, want)
		}
	}
}
