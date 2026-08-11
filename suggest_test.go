package main

import (
	"reflect"
	"testing"
	"time"

	"taskr/todo"
)

func TestQuickAddToken(t *testing.T) {
	cases := []struct {
		name         string
		value        string
		pos          int
		wantSigil    string
		wantQuery    string
		wantOK       bool
		wantSt, wnEn int
	}{
		{"bare sigil", "buy milk #", 10, "#", "", true, 9, 10},
		{"partial tag", "buy milk #ho", 12, "#", "ho", true, 9, 12},
		{"project token", "buy milk @ho", 12, "@", "ho", true, 9, 12},
		{"caret inside the token completes the whole token", "buy #home now", 7, "#", "home", true, 4, 9},
		{"plain word is not a completion", "buy milk", 8, "", "", false, 0, 0},
		{"caret parked before a token has nothing typed yet", "buy #home", 4, "", "", false, 0, 0},
		{"empty input", "", 0, "", "", false, 0, 0},
		// "købé " is five runes, so the token runs 5..8 in rune space — the
		// byte offsets would be 6..10 and would slice mid-rune.
		{"multibyte before the token", "købé #hø", 8, "#", "hø", true, 5, 8},
	}
	for _, c := range cases {
		sigil, query, start, end, ok := quickAddToken(c.value, c.pos)
		if ok != c.wantOK || sigil != c.wantSigil || query != c.wantQuery {
			t.Errorf("%s: quickAddToken(%q, %d) = %q/%q/%v, want %q/%q/%v",
				c.name, c.value, c.pos, sigil, query, ok, c.wantSigil, c.wantQuery, c.wantOK)
			continue
		}
		if ok && (start != c.wantSt || end != c.wnEn) {
			t.Errorf("%s: bounds = %d..%d, want %d..%d", c.name, start, end, c.wantSt, c.wnEn)
		}
	}
}

// A caret position past the end of the string (or below zero) must clamp
// rather than slice out of range — Update can hand either shape through.
func TestQuickAddTokenClampsCaret(t *testing.T) {
	if _, _, _, _, ok := quickAddToken("buy #home", 99); !ok {
		t.Error("caret past the end should clamp to the end and still complete the last token")
	}
	if _, _, _, _, ok := quickAddToken("buy #home", -3); ok {
		t.Error("negative caret should clamp to 0, where nothing is typed yet")
	}
}

// suggestModel stamps ModifiedAt explicitly: the suggestion order is
// recency-first, and two todo.New calls in the same test would otherwise be
// separated by nanoseconds — enough to pass, not enough to trust.
func suggestModel(t *testing.T) model {
	t.Helper()
	now := time.Now()
	home := todo.New("Fix boiler")
	home.AddTag("home")
	home.AddTag("house-stuff")
	home.Project = "House"
	home.ModifiedAt = now.Add(-2 * time.Hour)
	work := todo.New("Write memo")
	work.AddTag("work")
	work.AddTag("writing")
	work.Project = "Q3"
	work.ModifiedAt = now
	spaced := todo.New("Redo kitchen")
	spaced.Project = "Home renovation"
	spaced.ModifiedAt = now.Add(-time.Hour)
	return modelWithTasks(t, home, work, spaced)
}

func TestQuickAddSuggestions(t *testing.T) {
	m := suggestModel(t)

	sigil, got := m.quickAddSuggestions("buy milk #", 10)
	if sigil != "#" {
		t.Fatalf("sigil = %q, want #", sigil)
	}
	if len(got) != 4 {
		t.Fatalf("bare '#' = %v, want every tag offered", got)
	}

	// Prefix matches come first; a plain substring match still shows up.
	_, got = m.quickAddSuggestions("buy milk #o", 11)
	if want := []string{"work", "home", "house-stuff"}; !reflect.DeepEqual(got, want) {
		t.Errorf("'#o' = %v, want %v (substring matches, recency order)", got, want)
	}
	_, got = m.quickAddSuggestions("buy milk #w", 11)
	if len(got) == 0 || got[0] != "work" {
		t.Errorf("'#w' = %v, want the prefix match first", got)
	}

	// A tag already on the line is not worth offering again — but the token
	// being typed must not exclude itself.
	_, got = m.quickAddSuggestions("buy milk #work #w", 17)
	for _, g := range got {
		if g == "work" {
			t.Errorf("'#work #w' = %v, want the already-typed tag left out", got)
		}
	}
	_, got = m.quickAddSuggestions("buy milk #work", 14)
	for _, g := range got {
		if g == "work" {
			t.Errorf("'#work' = %v, want the fully typed tag left out", got)
		}
	}
}

func TestQuickAddSuggestionsProjects(t *testing.T) {
	m := suggestModel(t)
	sigil, got := m.quickAddSuggestions("buy milk @", 10)
	if sigil != "@" {
		t.Fatalf("sigil = %q, want @", sigil)
	}
	// "Home renovation" carries a space, which quick-add's whitespace
	// tokenisation cannot express, so it must not be offered.
	if want := []string{"Q3", "House"}; !reflect.DeepEqual(got, want) {
		t.Errorf("'@' = %v, want %v (space-free projects, recency order)", got, want)
	}
}

func TestQuickAddSuggestionsCapped(t *testing.T) {
	task := todo.New("Tagged")
	for _, tag := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7"} {
		task.AddTag(tag)
	}
	m := modelWithTasks(t, task)
	if _, got := m.quickAddSuggestions("x #a", 4); len(got) != maxQuickAddSuggestions {
		t.Errorf("got %d suggestions, want the cap of %d", len(got), maxQuickAddSuggestions)
	}
}

func TestAcceptQuickAddSuggestion(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		pos      int
		choice   string
		want     string
		wantPos  int
		wantDone bool
	}{
		{"completes and leaves room for the next token", "buy milk #ho", 12, "home", "buy milk #home ", 15, true},
		// The caret lands at the end of the completed token; the space that was
		// already there is left alone rather than duplicated.
		{"does not double an existing space", "buy #ho milk", 7, "home", "buy #home milk", 9, true},
		{"project keeps its sigil", "buy @ho", 7, "House", "buy @House ", 11, true},
		{"nothing to complete", "buy milk", 8, "home", "buy milk", 8, false},
	}
	for _, c := range cases {
		got, pos, ok := acceptQuickAddSuggestion(c.value, c.pos, c.choice)
		if got != c.want || pos != c.wantPos || ok != c.wantDone {
			t.Errorf("%s: accept(%q, %d, %q) = %q/%d/%v, want %q/%d/%v",
				c.name, c.value, c.pos, c.choice, got, pos, ok, c.want, c.wantPos, c.wantDone)
		}
	}
}
