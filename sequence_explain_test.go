package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// sequence_explain_test.go guards the promise the explain view makes: that what
// it says about a score is the score. An explanation that drifts from the
// arithmetic it describes is worse than no explanation — it teaches a model of
// the ranking that is wrong.

func balancedBiases() biases {
	return biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true}
}

// The five factors must add up to the number the sort used. Any future
// dimension that forgets to appear here shows up as a gap.
func TestExplainFactorsSumToTheScore(t *testing.T) {
	for _, b := range []biases{
		balancedBiases(),
		{Deadline: biasIntense, Priority: biasRelaxed, Momentum: biasIntense, Aging: true},
		{Deadline: biasRelaxed, Priority: biasRelaxed, Momentum: biasRelaxed, Aging: false},
	} {
		tt := todo.New("sum check")
		tt.Priority = todo.PriorityHigh
		tt.Size = todo.SizeSmall
		tt.Project = "hot"
		tt.DueDate = fixedNow.AddDate(0, 0, 3)
		tt.CreatedAt = fixedNow.AddDate(0, 0, -40)
		heat := hotHeat(fixedNow, nil, []string{"hot"}, nil)

		want := sequenceComponentsAt(fixedNow, &tt, b, heat).Total
		var sum float64
		for _, f := range seqFactorsAt(fixedNow, &tt, b, heat) {
			sum += f.Weighted
		}
		if !approxEq(sum, want) {
			t.Errorf("biases %+v: factors sum to %v, score is %v", b, sum, want)
		}
	}
}

// Each factor's sentence must describe the branch the number came from. The
// deadline axis has the most branches and the most room to disagree.
func TestExplainDeadlineReasonMatchesTheNumber(t *testing.T) {
	cases := []struct {
		name   string
		due    time.Time
		reason seqReason
		n      int
	}{
		{"no date", time.Time{}, reasonNoDue, 0},
		{"overdue", startOfDay(fixedNow).AddDate(0, 0, -4), reasonOverdue, 4},
		{"today", fixedNow.Add(2 * time.Hour), reasonDueToday, 0},
		{"tomorrow", startOfDay(fixedNow).AddDate(0, 0, 1), reasonDueTomorrow, 1},
		{"inside the ramp", startOfDay(fixedNow).AddDate(0, 0, 5), reasonDueInDays, 5},
		{"past the ramp", startOfDay(fixedNow).AddDate(0, 0, 20), reasonDueBeyondRamp, 20},
	}
	for _, c := range cases {
		tt := todo.New(c.name)
		tt.DueDate = c.due
		f := seqFactorsAt(fixedNow, &tt, balancedBiases(), activityHeat{})[0]
		if f.Reason != c.reason || f.N != c.n {
			t.Errorf("%s: reason %v/%d, want %v/%d", c.name, f.Reason, f.N, c.reason, c.n)
		}
		// The sentence and the points have to agree about whether the deadline
		// contributed anything at all.
		scores := f.Weighted > 0
		says := c.reason != reasonNoDue && c.reason != reasonDueBeyondRamp
		if scores != says {
			t.Errorf("%s: scored %v but the reason (%v) reads otherwise", c.name, f.Weighted, c.reason)
		}
	}
}

func TestExplainMomentumNamesItsSource(t *testing.T) {
	heat := hotHeat(fixedNow, []string{"self"}, []string{"alpha"}, []string{"go"})
	mk := func(id, project string, tags ...string) *todo.Todo {
		tt := todo.New("x")
		tt.ID, tt.Project, tt.Tags = id, project, tags
		return &tt
	}
	cases := []struct {
		name   string
		t      *todo.Todo
		reason seqReason
		word   string
	}{
		{"own work", mk("self", ""), reasonMomentumTask, ""},
		{"project", mk("other", "alpha"), reasonMomentumProj, "alpha"},
		{"tag", mk("other", "cold", "go"), reasonMomentumTag, "go"},
		{"cold", mk("other", "cold", "rust"), reasonMomentumCold, ""},
	}
	for _, c := range cases {
		f := seqFactorsAt(fixedNow, c.t, balancedBiases(), heat)[2]
		if f.Reason != c.reason || f.Word != c.word {
			t.Errorf("%s: got %v/%q, want %v/%q", c.name, f.Reason, f.Word, c.reason, c.word)
		}
	}
}

// The margins have to be the real ones: the gap to the row above is what the
// task would have to gain to pass it, measured on the scores the sort used.
func TestExplainQuotesRealMargins(t *testing.T) {
	high := todo.New("high priority")
	high.ID = "high"
	high.Priority = todo.PriorityHigh
	mid := todo.New("medium priority")
	mid.ID = "mid"
	mid.Priority = todo.PriorityMedium
	low := todo.New("low priority")
	low.ID = "low"
	low.Priority = todo.PriorityLow
	for _, tt := range []*todo.Todo{&high, &mid, &low} {
		tt.CreatedAt = fixedNow
	}
	all := []*todo.Todo{&high, &mid, &low}

	e := explainSequenceAt(fixedNow, &mid, all, balancedBiases(), activityHeat{})
	if e.Pos != 2 || e.Of != 3 {
		t.Fatalf("rank = #%d of %d, want #2 of 3", e.Pos, e.Of)
	}
	if e.Above == nil || e.Above.Title != high.Title || !approxEq(e.Above.Gap, 5) {
		t.Errorf("above = %+v, want the high-priority task 5.0 ahead", e.Above)
	}
	if e.Below == nil || e.Below.Title != low.Title || !approxEq(e.Below.Gap, 5) {
		t.Errorf("below = %+v, want the low-priority task 5.0 behind", e.Below)
	}
}

// A parent ranked on a subtask's score must say so rather than showing its own
// number next to a position that number does not explain.
func TestExplainFlagsARollupBoost(t *testing.T) {
	parent := todo.New("calm parent")
	parent.ID = "parent"
	parent.Priority = todo.PriorityLow
	parent.CreatedAt = fixedNow
	child := todo.New("urgent child")
	child.ID = "child"
	child.ParentID = "parent"
	child.Priority = todo.PriorityHigh
	child.CreatedAt = fixedNow
	all := []*todo.Todo{&parent, &child}

	e := explainSequenceAt(fixedNow, &parent, all, balancedBiases(), activityHeat{})
	if !e.Boosted {
		t.Fatalf("parent lifted by its subtask did not report a boost (own %.1f, ranked %.1f)", e.Total, e.Ranked)
	}
	if e.Ranked <= e.Total {
		t.Errorf("ranked on %.1f, own score %.1f — a boost must rank higher", e.Ranked, e.Total)
	}
}

// The forecast is the whole point: the two things that reshuffle a list with
// nobody touching it are the midnight deadline step and momentum expiring.
func TestForecastReportsTheMidnightDeadlineStep(t *testing.T) {
	tt := todo.New("due in three days")
	tt.ID = "due"
	tt.CreatedAt = fixedNow
	tt.DueDate = startOfDay(fixedNow).AddDate(0, 0, 3)

	shifts := forecastSequence(fixedNow, &tt, []*todo.Todo{&tt}, balancedBiases(), activityHeat{})
	if len(shifts) == 0 {
		t.Fatal("a task inside the deadline ramp reported no upcoming change")
	}
	first := shifts[0]
	if first.Cause != shiftMidnight {
		t.Errorf("first shift cause = %v, want the midnight step", first.Cause)
	}
	if !first.At.Equal(startOfDay(fixedNow).AddDate(0, 0, 1)) {
		t.Errorf("first shift at %v, want the next midnight", first.At)
	}
	if first.Delta <= 0 {
		t.Errorf("a day closer to the deadline scored %+.2f, want a gain", first.Delta)
	}
}

func TestForecastReportsMomentumRunningOut(t *testing.T) {
	// Warm only through the project, with the signal 40h old: it expires 8h
	// from now, well before any deadline effect could explain the drop.
	signal := fixedNow.Add(-40 * time.Hour)
	tt := todo.New("carried by a warm project")
	tt.ID = "warm"
	tt.Project = "alpha"
	tt.CreatedAt = fixedNow
	heat := hotHeat(signal, nil, []string{"alpha"}, nil)

	shifts := forecastSequence(fixedNow, &tt, []*todo.Todo{&tt}, balancedBiases(), heat)
	var found *seqShift
	for i := range shifts {
		if shifts[i].Cause == shiftMomentum {
			found = &shifts[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("momentum expiry was not forecast; shifts = %+v", shifts)
	}
	if want := signal.Add(momentumWindow); !found.At.Equal(want) {
		t.Errorf("momentum expiry at %v, want %v (signal + 48h)", found.At, want)
	}
	// Age keeps accruing across the eight hours, so the drop is the whole
	// Balanced momentum axis less that day's fraction — not exactly -10.
	if found.Delta > -9.9 || found.Delta < -10.1 {
		t.Errorf("momentum expiry delta = %+.2f, want ~-10 (the whole Balanced axis)", found.Delta)
	}
}

// A stable task must say it is stable rather than inventing a change: age
// drift alone moves 0.1/day and reorders nothing.
func TestForecastStaysQuietForAStableTask(t *testing.T) {
	tt := todo.New("no deadline, no heat")
	tt.ID = "quiet"
	tt.CreatedAt = fixedNow.AddDate(0, 0, -3)

	if shifts := forecastSequence(fixedNow, &tt, []*todo.Todo{&tt}, balancedBiases(), activityHeat{}); len(shifts) != 0 {
		t.Errorf("a task with no deadline and no heat forecast %d change(s): %+v", len(shifts), shifts)
	}
}

// Done tasks score 0 by rule; the explanation must say that instead of
// printing five zeros and a total nobody can act on.
func TestExplainDoneTaskSaysSo(t *testing.T) {
	tt := todo.New("finished")
	tt.Status = todo.Done
	tt.Priority = todo.PriorityHigh
	e := explainSequenceAt(fixedNow, &tt, []*todo.Todo{&tt}, balancedBiases(), activityHeat{})
	if !e.Done || len(e.Factors) != 0 {
		t.Errorf("done task: Done=%v with %d factors, want the done headline and no breakdown", e.Done, len(e.Factors))
	}
	if !strings.Contains(seqHeadline(e), "done") {
		t.Errorf("headline %q does not mention that the task is done", seqHeadline(e))
	}
}

// ── The overlay ───────────────────────────────────────────────────────────────

func explainModel(t *testing.T) model {
	t.Helper()
	overdue := todo.New("Fix the boiler in the basement before the inspection")
	overdue.Priority = todo.PriorityHigh
	overdue.Project = "House"
	overdue.AddTag("home")
	overdue.DueDate = time.Now().Add(-72 * time.Hour)
	soon := todo.New("Write the quarterly memo")
	soon.DueDate = time.Now().Add(48 * time.Hour)
	plain := todo.New("Buy filters")
	return modelWithTasks(t, overdue, soon, plain)
}

// w opens the overlay on the task under the cursor and pins it by ID, so a
// re-sort underneath cannot swap out what is being explained.
func TestWhyKeyOpensTheOverlayOnTheCurrentTask(t *testing.T) {
	m := explainModel(t)
	want := m.currentTodo()
	if want == nil {
		t.Fatal("no task under the cursor")
	}
	m = sendKey(t, m, "w")
	if m.mode != modeExplain {
		t.Fatalf("mode = %v after w, want modeExplain", m.mode)
	}
	if m.explainTaskID != want.ID {
		t.Errorf("overlay pinned %q, want the task under the cursor (%q)", m.explainTaskID, want.ID)
	}
	if body := m.View(); !strings.Contains(body, "Why this rank") {
		t.Error("the overlay did not render its title")
	}
	m = sendKey(t, m, "esc")
	if m.mode != modeNormal || m.explainTaskID != "" {
		t.Errorf("esc left mode=%v pinned=%q, want a closed overlay", m.mode, m.explainTaskID)
	}
}

// The overlay says the same things the CLI prints — one score, one story.
func TestOverlayAndCLIAgree(t *testing.T) {
	m := explainModel(t)
	tt := m.currentTodo()
	e := explainSequenceFor(tt, m.allTodos())

	plain := strings.Join(explainPlainLines(e), "\n")
	if !strings.Contains(plain, tt.Title) {
		t.Errorf("plain output does not name the task:\n%s", plain)
	}
	for _, name := range seqDimNames {
		if !strings.Contains(plain, name) {
			t.Errorf("plain output is missing the %s row:\n%s", name, plain)
		}
	}
	styled := strings.Join(m.explainBodyLines(e, 100), "\n")
	if !strings.Contains(ansi.Strip(styled), tt.Title) {
		t.Error("the overlay body does not name the task")
	}
}

// The no-wrap contract, swept across widths and languages — German is the
// width stress case, and a translated sentence is exactly what would push a
// hand-budgeted column over the edge.
func TestOverlayHonoursTheWidthBudget(t *testing.T) {
	defer applyLang("en")
	m := explainModel(t)
	e := explainSequenceFor(m.currentTodo(), m.allTodos())
	for _, lang := range availableLanguages {
		applyLang(string(lang))
		for _, w := range []int{0, 1, 8, 20, 40, 60, 80, 100, 160} {
			for _, line := range m.explainBodyLines(e, w) {
				if got := ansi.StringWidth(line); got > w && w >= 8 {
					t.Errorf("%s at width %d: line is %d wide: %q", lang, w, got, ansi.Strip(line))
				}
			}
		}
	}
}

// The overlay must survive being asked about a task that is not in the
// ranking at all — a subtask, or a row whose ID went stale.
func TestOverlayHandlesUnrankedAndMissingTasks(t *testing.T) {
	parent := todo.New("parent")
	parent.ID = "p"
	sub := todo.New("subtask")
	sub.ID = "s"
	sub.ParentID = "p"
	m := modelWithTasks(t, parent, sub)

	e := explainSequenceFor(m.get("s"), m.allTodos())
	if e.Pos != 0 {
		t.Errorf("a subtask reported rank #%d, want unranked", e.Pos)
	}
	if len(m.explainBodyLines(e, 80)) == 0 {
		t.Error("an unranked task rendered no body")
	}

	m.mode = modeExplain
	m.explainTaskID = "gone"
	if got := m.View(); got == "" {
		t.Error("a stale explain ID rendered nothing")
	}
}
