package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// withStages runs fn with the active stage list swapped, restoring the
// original after — the applyTheme/applyLang test pattern for globals.
func withStages(t *testing.T, stages []string, fn func()) {
	t.Helper()
	prev := activeStages
	applyStages(stages)
	defer applyStages(prev)
	fn()
}

func TestStagesFromSettings(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty falls back to defaults", nil, defaultStages()},
		{"all-blank falls back to defaults", []string{"", "  "}, defaultStages()},
		{"trims and keeps order", []string{" Todo ", "Doing"}, []string{"Todo", "Doing"}},
		{"dedupes case-insensitively onto first spelling", []string{"Todo", "todo", "Done-ish"}, []string{"Todo", "Done-ish"}},
	}
	for _, c := range cases {
		if got := stagesFromSettings(appSettings{Stages: c.in}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: stagesFromSettings(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestStageIndexAndCanonical(t *testing.T) {
	withStages(t, []string{"Todo", "Doing", "Waiting"}, func() {
		for stage, want := range map[string]int{
			"":        0, // fresh task → first column
			"Todo":    0,
			"doing":   1, // case-insensitive
			"Waiting": 2,
			"Review":  0, // renamed-away stage strands visibly in the first column
		} {
			if got := stageIndex(stage); got != want {
				t.Errorf("stageIndex(%q) = %d, want %d", stage, got, want)
			}
		}

		if name, ok := canonicalStage(" doing "); !ok || name != "Doing" {
			t.Errorf("canonicalStage(' doing ') = %q/%v, want Doing/true", name, ok)
		}
		if _, ok := canonicalStage("nope"); ok {
			t.Error("canonicalStage('nope') should not resolve")
		}
	})
}

func TestBoardColumnsSplitByStage(t *testing.T) {
	backlog := todo.New("fresh task") // empty stage → first column
	doing := todo.New("moving task")
	doing.Stage = "In progress"
	review := todo.New("almost there")
	review.Stage = "Review"
	sub := todo.New("subtask hidden from the board")
	sub.ParentID = backlog.ID
	finished := todo.New("shipped")
	finished.Status = todo.Done
	finished.CompletedAt = time.Now()

	m := newTagModel(backlog, doing, review, sub, finished)
	cols := m.boardColumns()

	if len(cols) != 4 {
		t.Fatalf("columns = %d, want 4 (3 stages + Done)", len(cols))
	}
	for i, want := range []string{"Fresh task", "Moving task", "Almost there", "Shipped"} {
		if len(cols[i]) != 1 || cols[i][0].Title != want {
			t.Errorf("column %d = %v, want single card %q", i, cols[i], want)
		}
	}
}

func TestBoardRenderWideAndStacked(t *testing.T) {
	a := todo.New("alpha")
	b := todo.New("beta")
	b.Stage = "Review"
	m := newTagModel(a, b)
	m.tab = tabBoard
	m.termWidth = 120
	m.termHeight = 30

	out := ansi.Strip(m.renderBoardList())
	for _, want := range []string{"Backlog (1)", "In progress (0)", "Review (1)", "Done (0)", "> Alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide board missing %q:\n%s", want, out)
		}
	}
	availW := m.termWidth - 8
	for _, line := range strings.Split(m.renderBoardList(), "\n") {
		if w := ansi.StringWidth(line); w > availW {
			t.Errorf("board line exceeds availW (%d > %d): %q", w, availW, ansi.Strip(line))
		}
	}

	// Narrow terminal degrades to the stacked, full-width form.
	m.termWidth = 50
	stacked := ansi.Strip(m.renderBoardList())
	if !strings.Contains(stacked, "Backlog (1)") || !strings.Contains(stacked, "Review (1)") {
		t.Errorf("stacked board missing headers:\n%s", stacked)
	}
}

func TestBoardSelectionClamps(t *testing.T) {
	a := todo.New("only card")
	m := newTagModel(a)
	m.board.col = 99
	m.board.cursor = 42
	cols := m.boardColumns()
	col, cursor := m.boardSelection(cols)
	if col != len(cols)-1 || cursor != 0 {
		t.Errorf("clamped selection = (%d,%d), want last column, cursor 0", col, cursor)
	}
	if sel := m.boardSelectedTask(); sel != nil {
		t.Errorf("selection on empty Done column should be nil, got %q", sel.Title)
	}
	m.board.col = 0
	if sel := m.boardSelectedTask(); sel == nil || sel.Title != "Only card" {
		t.Errorf("selected task = %v, want Only card", sel)
	}
}
