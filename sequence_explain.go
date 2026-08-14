package main

import (
	"math"
	"sort"
	"time"

	"taskr/todo"
)

// sequence_explain.go answers the two questions the score column raises but
// cannot answer on its own: why is this task *here*, and what is going to move
// it. A ranking nobody can predict is a ranking nobody trusts — and this engine
// has two ways of reordering a list with no edit from the user at all: the
// Deadline term steps every midnight, and the Momentum term expires exactly
// momentumWindow after the last signal. Both are computable in advance, so the
// explain view states them as facts rather than leaving the reshuffle to be
// discovered the next morning.
//
// Everything here is pure and locale-free, like the rest of the scoring code:
// reasons are returned as codes (seqReason) and become sentences at the view
// layer (trSeqReason, lang.go).

// ── Reasons ───────────────────────────────────────────────────────────────────

// seqReason identifies the sentence behind one factor's value.
type seqReason int

const (
	reasonNone seqReason = iota
	reasonNoDue
	reasonOverdue       // N = whole days past the due date
	reasonDueToday      //
	reasonDueTomorrow   //
	reasonDueInDays     // N = days out, inside the 7-day ramp
	reasonDueBeyondRamp // N = days out, past the ramp (scores 0)
	reasonPriority      // Word = the English priority word
	reasonMomentumTask  // this task itself was worked on
	reasonMomentumProj  // Word = the project that saw the activity
	reasonMomentumTag   // Word = the tag that saw the activity
	reasonMomentumCold  //
	reasonSize          // Word = the English size word
	reasonAgeDays       // N = days since creation
	reasonAgeToday      //
	reasonAgeOff        // aging switched off in Settings
)

// seqFactor is one row of the breakdown: the raw 0–10 axis reading, the bias it
// was multiplied by, the points that reached the total, and why the axis reads
// what it does.
type seqFactor struct {
	Name     string // one of seqDimNames
	Raw      float64
	Weight   float64 // the bias multiplier; 1 for the unweighted terms
	Weighted float64 // Raw × Weight — the points in Total
	Bias     biasLevel
	Knobbed  bool // Deadline/Priority/Momentum have a Settings knob; Size/Age don't
	Reason   seqReason
	N        int
	Word     string
}

// ── Shifts (what moves on its own) ────────────────────────────────────────────

type seqShiftCause int

const (
	shiftMidnight seqShiftCause = iota // the Deadline ramp steps a day closer
	shiftMomentum                      // an activity signal ages out of the window
)

// seqShift is a predicted future state of this task: at `At` the score becomes
// Total and the task sits at rank Pos, with nothing required from the user.
type seqShift struct {
	At    time.Time
	Cause seqShiftCause
	Total float64
	Delta float64 // change from the previously reported state
	Pos   int     // rank at that moment, 0 if it drops out of the ranking
}

// seqNeighbour is the row directly above or below in the ranking, with the
// margin that separates it. Gap is always positive: the points this task would
// have to gain to pass Above, or the cushion it holds over Below.
type seqNeighbour struct {
	Pos   int
	Title string
	Score float64
	Gap   float64
}

// seqExplain is the whole answer for one task.
type seqExplain struct {
	Now     time.Time
	Title   string
	Done    bool    // done tasks score 0 by rule; the breakdown would be zeros
	Total   float64 // this task's own score
	Ranked  float64 // the score the ranking actually sorted it by
	Boosted bool    // Ranked > Total — a subtask or a blocked dependent lifted it
	Pos     int     // 1-based rank among top-level pending tasks, 0 if unranked
	Of      int
	Factors []seqFactor
	Above   *seqNeighbour
	Below   *seqNeighbour
	Shifts  []seqShift
}

// ── Ranking with its effective scores ─────────────────────────────────────────

// seqRanking is the ordering `taskr top` and the Sequence sort produce, paired
// with the score each row was actually sorted by. That effective score is
// max(own score, rollup) — a parent lifted by a subtask, or a blocker lifted by
// what it blocks, ranks on the inherited number, so quoting the own-score as
// the margin to a neighbour would be quoting the wrong one.
func seqRanking(todos []*todo.Todo, score func(*todo.Todo) float64) ([]todo.Todo, map[string]float64) {
	rollup := descendantScoreRollupWith(todos, score)
	rollup = dependencyScoreRollupWith(todos, rollup, score)
	rows := make([]todo.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ParentID == "" && t.Status == todo.Pending {
			rows = append(rows, *t)
		}
	}
	sortTodosBySequenceWithRollupBy(rows, rollup, score)
	eff := make(map[string]float64, len(rows))
	for i := range rows {
		s := score(&rows[i])
		if boost, ok := rollup[rows[i].ID]; ok && boost > s {
			s = boost
		}
		eff[rows[i].ID] = s
	}
	return rows, eff
}

// ── Factors ───────────────────────────────────────────────────────────────────

// seqFactorsAt breaks the score into its five rows for the given moment. The
// arithmetic is deliberately the same call the live scorer makes, so what the
// explanation shows and what the sort used cannot drift.
func seqFactorsAt(now time.Time, t *todo.Todo, b biases, heat activityHeat) []seqFactor {
	u, i, m, size, age := dimensionsAt(now, t, heat)
	if !b.Aging {
		age = 0
	}
	dueReason, dueN := deadlineReason(now, t.DueDate)
	momReason, momWord := momentumReason(t, heat)
	ageReason, ageN := agingReason(now, t.CreatedAt, b.Aging)

	knob := func(name string, raw float64, level biasLevel, reason seqReason, n int, word string) seqFactor {
		return seqFactor{
			Name: name, Raw: raw, Weight: level.weight(), Weighted: raw * level.weight(),
			Bias: level, Knobbed: true, Reason: reason, N: n, Word: word,
		}
	}
	flat := func(name string, v float64, reason seqReason, n int, word string) seqFactor {
		return seqFactor{Name: name, Raw: v, Weight: 1, Weighted: v, Bias: biasBalanced, Reason: reason, N: n, Word: word}
	}
	return []seqFactor{
		knob(seqDimNames[0], u, b.Deadline, dueReason, dueN, ""),
		knob(seqDimNames[1], i, b.Priority, reasonPriority, 0, t.Priority.String()),
		knob(seqDimNames[2], m, b.Momentum, momReason, 0, momWord),
		flat(seqDimNames[3], size, reasonSize, 0, t.Size.String()),
		flat(seqDimNames[4], age, ageReason, ageN, ""),
	}
}

// deadlineReason names the branch of urgencyDim the task landed in. It counts
// days exactly the way urgencyDim does — start-of-day to start-of-day — so the
// sentence and the number can never disagree.
func deadlineReason(now, due time.Time) (seqReason, int) {
	if due.IsZero() {
		return reasonNoDue, 0
	}
	days := int(startOfDay(due).Sub(startOfDay(now)).Hours() / 24)
	switch {
	case days < 0:
		return reasonOverdue, -days
	case days == 0:
		return reasonDueToday, 0
	case days == 1:
		return reasonDueTomorrow, 1
	case days <= 7:
		return reasonDueInDays, days
	default:
		return reasonDueBeyondRamp, days
	}
}

// momentumReason names which tier of momentumDim fired, and what warmed it —
// "the project you were in" is a far more useful answer than "10".
func momentumReason(t *todo.Todo, heat activityHeat) (seqReason, string) {
	if hot(heat.tasks, t.ID) {
		return reasonMomentumTask, ""
	}
	if hot(heat.projects, t.Project) {
		return reasonMomentumProj, t.Project
	}
	for _, tag := range t.Tags {
		if hot(heat.tags, tag) {
			return reasonMomentumTag, tag
		}
	}
	return reasonMomentumCold, ""
}

func agingReason(now, created time.Time, aging bool) (seqReason, int) {
	if !aging {
		return reasonAgeOff, 0
	}
	days := int(now.Sub(created).Hours() / 24)
	if created.IsZero() || days < 1 {
		return reasonAgeToday, 0
	}
	return reasonAgeDays, days
}

// ── Forecast ──────────────────────────────────────────────────────────────────

// seqShiftHorizon bounds how far ahead the forecast looks. Both self-moving
// terms act inside two days (the next midnight, and momentumWindow from the
// newest signal), so nothing useful lives past it.
const seqShiftHorizon = 3 * 24 * time.Hour

// seqShiftMinDelta is the score change worth a line on its own. Age alone
// drifts 0.1–0.2 a day, which moves nothing anyone can see; a rank change is
// reported regardless of size.
const seqShiftMinDelta = 0.5

// seqShiftMax caps the forecast so the overlay stays a screen.
const seqShiftMax = 3

// forecastSequence predicts the moments this task's standing changes with no
// user action: each midnight inside the horizon (the Deadline ramp steps) and
// each instant a signal feeding its Momentum ages out. Every candidate is
// scored by re-running the real scorer against the heat snapshot as it will
// stand then, and re-ranked against the real ranking, so a predicted position
// is the position the list will actually show.
func forecastSequence(now time.Time, t *todo.Todo, all []*todo.Todo, b biases, heat activityHeat) []seqShift {
	if t == nil || t.Status == todo.Done || t.ParentID != "" {
		return nil
	}
	type candidate struct {
		at    time.Time
		cause seqShiftCause
	}
	var cands []candidate
	for day := startOfDay(now).AddDate(0, 0, 1); day.Sub(now) < seqShiftHorizon; day = day.AddDate(0, 0, 1) {
		cands = append(cands, candidate{day, shiftMidnight})
	}
	for _, at := range heatExpiries(t, heat, now) {
		if at.Sub(now) < seqShiftHorizon {
			cands = append(cands, candidate{at, shiftMomentum})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].at.Before(cands[j].at) })

	prevTotal := sequenceComponentsAt(now, t, b, heat).Total
	prevPos := rankOf(seqRankingAt(now, all, b, heat))(t.ID)
	var out []seqShift
	for _, c := range cands {
		aged := heat.expire(c.at)
		total := sequenceComponentsAt(c.at, t, b, aged).Total
		pos := rankOf(seqRankingAt(c.at, all, b, aged))(t.ID)
		delta := total - prevTotal
		if pos == prevPos && math.Abs(delta) < seqShiftMinDelta {
			continue
		}
		out = append(out, seqShift{At: c.at, Cause: c.cause, Total: total, Delta: delta, Pos: pos})
		prevTotal, prevPos = total, pos
		if len(out) == seqShiftMax {
			break
		}
	}
	return out
}

// seqRankingAt ranks the whole set with an explicit clock and heat snapshot —
// the same ordering the Tasks tab shows, evaluated at another moment.
func seqRankingAt(now time.Time, all []*todo.Todo, b biases, heat activityHeat) []todo.Todo {
	rows, _ := seqRanking(all, func(t *todo.Todo) float64 {
		return sequenceComponentsAt(now, t, b, heat).Total
	})
	return rows
}

// rankOf returns a lookup from task ID to 1-based position (0 = not ranked).
func rankOf(rows []todo.Todo) func(string) int {
	return func(id string) int {
		for i := range rows {
			if rows[i].ID == id {
				return i + 1
			}
		}
		return 0
	}
}

// ── Assembly ──────────────────────────────────────────────────────────────────

// explainSequenceAt is the pure, testable whole: breakdown, position in the
// ranking, the margins to the neighbours on either side, and the forecast.
func explainSequenceAt(now time.Time, t *todo.Todo, all []*todo.Todo, b biases, heat activityHeat) seqExplain {
	if t == nil {
		return seqExplain{Now: now}
	}
	e := seqExplain{Now: now, Title: t.Title, Done: t.Status == todo.Done}
	e.Total = sequenceComponentsAt(now, t, b, heat).Total
	e.Ranked = e.Total
	if e.Done {
		return e
	}
	e.Factors = seqFactorsAt(now, t, b, heat)

	rows, eff := seqRanking(all, func(x *todo.Todo) float64 {
		return sequenceComponentsAt(now, x, b, heat).Total
	})
	e.Of = len(rows)
	for i := range rows {
		if rows[i].ID != t.ID {
			continue
		}
		e.Pos = i + 1
		e.Ranked = eff[t.ID]
		e.Boosted = e.Ranked > e.Total+0.0005
		if i > 0 {
			above := rows[i-1]
			e.Above = &seqNeighbour{Pos: i, Title: above.Title, Score: eff[above.ID], Gap: eff[above.ID] - e.Ranked}
		}
		if i+1 < len(rows) {
			below := rows[i+1]
			e.Below = &seqNeighbour{Pos: i + 2, Title: below.Title, Score: eff[below.ID], Gap: e.Ranked - eff[below.ID]}
		}
		break
	}
	e.Shifts = forecastSequence(now, t, all, b, heat)
	return e
}

// explainSequenceFor is the live form: current clock, active biases, active
// heat — what the TUI overlay and `taskr why` both call.
func explainSequenceFor(t *todo.Todo, all []*todo.Todo) seqExplain {
	return explainSequenceAt(time.Now(), t, all, activeBiases, activeHeat)
}
