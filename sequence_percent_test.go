package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// sequence_percent_test.go covers the display scale: the raw score is unbounded
// upward, so on screen it reads as a share of the current field. The properties
// worth pinning are that the top of the field is 100%, that nothing exceeds it,
// and that the scale is a pure rescaling — it must not reorder anything.

func TestPercentIsRelativeToTheTopOfTheField(t *testing.T) {
	cases := []struct {
		score, max float64
		want       int
	}{
		{24.4, 24.4, 100},
		{12.2, 24.4, 50},
		{0, 24.4, 0},
		{24.4, 0, 0},    // no field yet — 0%, not a divide by zero
		{30, 24.4, 100}, // clamped: a stale maximum must not print 123%
		{-5, 24.4, 0},   // a negative score cannot read as a negative percent
	}
	for _, c := range cases {
		if got := percentOfField(c.score, c.max); got != c.want {
			t.Errorf("percentOfField(%v, %v) = %d, want %d", c.score, c.max, got, c.want)
		}
	}
}

// The highest-scoring pending task is the 100% mark, and done tasks and
// tombstones are not part of the field.
func TestFieldMaximumIgnoresDoneAndDeleted(t *testing.T) {
	live := todo.New("live")
	live.Priority = todo.PriorityMedium
	live.CreatedAt = fixedNow

	done := todo.New("done but high priority")
	done.Priority = todo.PriorityHigh
	done.Status = todo.Done
	done.CreatedAt = fixedNow

	gone := todo.New("deleted but high priority")
	gone.Priority = todo.PriorityHigh
	gone.Deleted = true
	gone.CreatedAt = fixedNow

	all := []*todo.Todo{&live, &done, &gone}
	score := func(t *todo.Todo) float64 {
		return sequenceComponentsAt(fixedNow, t, balancedBiases(), activityHeat{}).Total
	}
	max := maxSequenceScoreWith(all, score)
	if want := score(&live); !approxEq(max, want) {
		t.Errorf("field maximum = %v, want the only live task's score %v", max, want)
	}
}

// The percentage is a rescaling, not a reranking: whatever order the scores
// were in, the percentages must be in the same order.
func TestPercentPreservesTheOrdering(t *testing.T) {
	m := explainModel(t)
	rows := m.cache.active
	if len(rows) < 2 {
		t.Fatal("need at least two active tasks")
	}
	prev := 101
	for i := range rows {
		got := sequencePercent(sequenceScore(m.get(rows[i].ID)))
		if got > prev {
			t.Errorf("row %d reads %d%% after a row reading %d%% — the scale reordered the list", i, got, prev)
		}
		prev = got
	}
	// The list is sorted by sequence, so its first row is the top of the field.
	if top := sequencePercent(sequenceScore(m.get(rows[0].ID))); top != 100 {
		t.Errorf("the top row reads %d%%, want 100%%", top)
	}
}

// The overlay's percentage and the list column's must agree about one task —
// they are two readings of the same scale and are computed by different paths.
func TestOverlayPercentMatchesTheListColumn(t *testing.T) {
	m := explainModel(t)
	for i := range m.cache.active {
		task := m.get(m.cache.active[i].ID)
		e := explainSequenceFor(task, m.allTodos())
		want := formatSequencePercent(sequenceScore(task))
		if got := formatPercentOf(e.Total, e.FieldMax); got != want {
			t.Errorf("%q: overlay says %s, the list column says %s", task.Title, got, want)
		}
	}
}

// The scale moves when the field does — that is the trade normalizing against
// the live field makes, so the overlay has to state what 100% currently costs.
func TestOverlayStatesWhatFullScaleCosts(t *testing.T) {
	m := explainModel(t)
	e := explainSequenceFor(m.currentTodo(), m.allTodos())
	line := seqScaleLine(e)
	if !strings.Contains(line, formatPercentOf(e.Total, e.FieldMax)) {
		t.Errorf("the scale line %q does not name this task's percentage", line)
	}
	body := ansi.Strip(strings.Join(m.explainBodyLines(e, 100), "\n"))
	if !strings.Contains(body, line) {
		t.Errorf("the overlay does not show the scale line:\n%s", body)
	}
}

// ── Board filtering ───────────────────────────────────────────────────────────

func boardFilterModel(t *testing.T) model {
	t.Helper()
	home := todo.New("Fix the boiler")
	home.AddTag("home")
	home.Project = "House"
	work := todo.New("Write the memo")
	work.AddTag("work")
	work.Project = "Q3"
	workDone := todo.New("Ship the release")
	workDone.AddTag("work")
	workDone.Project = "Q3"
	workDone.Status = todo.Done
	workDone.CompletedAt = time.Now()
	m := modelWithTasks(t, home, work, workDone)
	m.switchTab(tabBoard)
	return m
}

func boardTitles(m model) []string {
	var out []string
	for _, col := range m.boardColumns() {
		for _, card := range col {
			out = append(out, card.Title)
		}
	}
	return out
}

// / on the Board opens the same search the Tasks tab uses, and the columns —
// which are a projection of the same filtered lists — narrow with it.
func TestBoardFiltersByTagAndProject(t *testing.T) {
	m := boardFilterModel(t)
	if got := len(boardTitles(m)); got != 3 {
		t.Fatalf("unfiltered board shows %d cards, want 3", got)
	}

	m = sendKey(t, m, "/")
	if m.mode != modeSearch {
		t.Fatalf("/ on the Board left mode %v, want modeSearch", m.mode)
	}

	for _, c := range []struct {
		query string
		want  []string
	}{
		{"#home", []string{"Fix the boiler"}},
		{"#work", []string{"Write the memo", "Ship the release"}},
		{"@House", []string{"Fix the boiler"}},
		{"@Q3", []string{"Write the memo", "Ship the release"}},
	} {
		m.searchQuery = c.query
		m.markFilterDirty()
		m.ensureCache()
		got := boardTitles(m)
		if len(got) != len(c.want) {
			t.Errorf("query %q → %v, want %v", c.query, got, c.want)
			continue
		}
		for _, title := range c.want {
			found := false
			for _, g := range got {
				if g == title {
					found = true
				}
			}
			if !found {
				t.Errorf("query %q → %v, missing %q", c.query, got, title)
			}
		}
	}
}

// A filter that empties a column must not strand the board cursor on a card
// that is no longer there.
func TestBoardCursorSurvivesAFilterThatEmptiesIt(t *testing.T) {
	m := boardFilterModel(t)
	m.searchQuery = "#nothing-matches-this"
	m.markFilterDirty()
	m.ensureCache()
	if got := boardTitles(m); len(got) != 0 {
		t.Fatalf("a filter matching nothing left %v on the board", got)
	}
	if task := m.boardSelectedTask(); task != nil {
		t.Errorf("board selection = %q on an empty board, want none", task.Title)
	}
	if got := m.View(); got == "" {
		t.Error("an empty filtered board rendered nothing")
	}
}
