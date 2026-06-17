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

func TestIsCLICommand(t *testing.T) {
	cases := map[string]bool{
		"add": true, "list": true, "ls": true, "done": true, "top": true,
		"help": true, "-h": true, "--help": true, "--version": true,
		"foo": false, "": false, "Add": false,
	}
	for in, want := range cases {
		if got := isCLICommand(in); got != want {
			t.Errorf("isCLICommand(%q)=%v, want %v", in, got, want)
		}
	}
}
