package main

import (
	"time"

	"taskr/todo"
)

// sequence.go is the sequencing engine: the rule that decides the "Sequence"
// sort order and the value persisted in the todos.sequence column on every save.
//
// The score is the design's Normalized Power Scale: four dimensions, each on a
// 0–10 axis, three of them multiplied by a user-tunable bias (Relaxed=0.5,
// Balanced=1.0, Intense=2.0). The fourth — Age — is added unweighted so old
// tasks always eventually surface for cleanup or completion.
//
//	Score = U·Wd + I·Wp + M·Wm + Age
//
//	U  Urgency    closeness to deadline (0..10+)
//	I  Importance priority bucket (0/5/10)
//	M  Momentum   size bucket (S=10, M=5, L=0)
//	Age           rot-guard: +0.1/day, +0.2/day past 30
//	Wd Wp Wm      Deadline / Priority / Momentum bias multipliers
//
// Done tasks score 0.

// ── Bias level ────────────────────────────────────────────────────────────────

// biasLevel is the three-state user-facing knob exposed in Settings. The
// numbers are deliberately symmetric around Balanced so cycling left/right
// doubles or halves the dimension's voting weight.
type biasLevel int

const (
	biasBalanced biasLevel = iota // weight 1.0 — the design's "neutral middleground"
	biasRelaxed                   // weight 0.5
	biasIntense                   // weight 2.0
)

func (b biasLevel) weight() float64 {
	switch b {
	case biasRelaxed:
		return 0.5
	case biasIntense:
		return 2.0
	default:
		return 1.0
	}
}

func (b biasLevel) String() string {
	switch b {
	case biasRelaxed:
		return "relaxed"
	case biasIntense:
		return "intense"
	default:
		return "balanced"
	}
}

// next cycles Relaxed → Balanced → Intense → Relaxed.
func (b biasLevel) next() biasLevel {
	switch b {
	case biasRelaxed:
		return biasBalanced
	case biasBalanced:
		return biasIntense
	default:
		return biasRelaxed
	}
}

// prev cycles in the opposite direction so ←/→ in Settings are symmetric.
func (b biasLevel) prev() biasLevel {
	switch b {
	case biasIntense:
		return biasBalanced
	case biasBalanced:
		return biasRelaxed
	default:
		return biasIntense
	}
}

// ── Biases (the user setting) ─────────────────────────────────────────────────

type biases struct {
	Deadline biasLevel
	Priority biasLevel
	Momentum biasLevel
}

// activeBiases is the package-level setting the score functions read at the
// time they're called. Settings.go's load/save path sets it via applyBiases on
// startup and whenever the user cycles a bias, matching the pattern already in
// use for themes (applyTheme) and language (applyLang). The zero value is all
// Balanced — the neutral default exactly as the design specifies.
var activeBiases biases

func applyBiases(b biases) { activeBiases = b }

// cycleBias is the user-facing knob bound to ←/→ on a Settings bias row.
// Direction +1 cycles Relaxed→Balanced→Intense (next), -1 the other way. After
// mutating activeBiases the caller is responsible for invalidating the task
// cache so the new weights take effect on the next render.
func cycleBiasLevel(b biasLevel, direction int) biasLevel {
	if direction < 0 {
		return b.prev()
	}
	return b.next()
}

// personality is the Sequence "feel" tagline shown in the Settings footer.
// Three named personalities match the design's all-Intense / all-Relaxed /
// all-Balanced presets; any other combination shows as "Custom" — keeps the
// labels meaningful instead of inventing a new name per permutation.
func personality(b biases) (name, descr string) {
	all := func(l biasLevel) bool {
		return b.Deadline == l && b.Priority == l && b.Momentum == l
	}
	switch {
	case all(biasIntense):
		return "Drill Sergeant", "High-Reactive: expect frequent shuffling as deadlines approach."
	case all(biasRelaxed):
		return "Zen Garden", "Stable: tasks stay mostly in the order they were created."
	case all(biasBalanced):
		return "Copilot", "Balanced: equally weighs priorities, deadlines, and quick wins."
	default:
		return "Custom", "Mixed biases — score reflects your tuned weights."
	}
}

// ── Per-dimension contributions ──────────────────────────────────────────────

// dimensionsAt is the pure core of the formula: given `now` and a task, return
// the four un-weighted dimension scores. Splitting `now` out lets tests pin
// time without monkey-patching.
func dimensionsAt(now time.Time, t *todo.Todo) (u, i, m, age float64) {
	if t == nil || t.Status == todo.Done {
		return 0, 0, 0, 0
	}
	u = urgencyDim(now, t.DueDate)
	i = importanceDim(t.Priority)
	m = momentumDim(t.Size)
	age = ageDim(now, t.CreatedAt)
	return
}

// urgencyDim implements the Deadline rule from the design:
//
//   - No due date          → 0
//   - Overdue or due today → 10 + 0.5 × (whole days past)
//   - 1..7 days out        → linear decay 10 (day 0) → 2 (day 7)
//   - >7 days out          → 0 (Age takes over for the long tail)
//
// Days are measured between start-of-day(now) and start-of-day(due), so a task
// due any time today scores exactly 10 regardless of the wall clock.
func urgencyDim(now, due time.Time) float64 {
	if due.IsZero() {
		return 0
	}
	today := startOfDay(now)
	dueDay := startOfDay(due)
	days := int(dueDay.Sub(today).Hours() / 24)
	switch {
	case days <= 0:
		return 10.0 + 0.5*float64(-days)
	case days <= 7:
		return 10.0 - (8.0*float64(days))/7.0
	default:
		return 0
	}
}

// importanceDim is the flat priority lookup: High=10, Medium=5, Low=0.
func importanceDim(p todo.Priority) float64 {
	switch p {
	case todo.PriorityHigh:
		return 10
	case todo.PriorityMedium:
		return 5
	default:
		return 0
	}
}

// momentumDim is the "small-task floor": Small=10 so quick wins outrank
// untyped Mediums and unaddressed Larges, Medium=5 stays neutral, Large=0
// concedes the floor to the smaller tasks.
func momentumDim(s todo.Size) float64 {
	switch s {
	case todo.SizeSmall:
		return 10
	case todo.SizeLarge:
		return 0
	default:
		return 5
	}
}

// ageDim is the unbounded rot-guard: 0.1/day until day 30, then 0.2/day. A
// 30-day-old task with no other signals scores 3.0; after 60 days, 9.0. The
// rule is intentionally unbounded so anything truly forgotten eventually
// floats to the top to be finished or deleted.
func ageDim(now, created time.Time) float64 {
	if created.IsZero() {
		return 0
	}
	days := now.Sub(created).Hours() / 24
	if days <= 0 {
		return 0
	}
	if days <= 30 {
		return 0.1 * days
	}
	return 0.1*30 + 0.2*(days-30)
}

// ── Score assembly ────────────────────────────────────────────────────────────

// sequenceComponents is the breakdown shown in the detail view. Each field is
// already weighted (i.e. multiplied by its bias) so the four values sum to
// Total — the user sees the actual contributions, not the raw 0..10 axes.
type sequenceComponents struct {
	Urgency    float64
	Importance float64
	Momentum   float64
	Age        float64
	Total      float64
}

// sequenceComponentsAt is the testable assembly: pure, takes `now` and biases
// explicitly.
func sequenceComponentsAt(now time.Time, t *todo.Todo, b biases) sequenceComponents {
	u, i, m, age := dimensionsAt(now, t)
	out := sequenceComponents{
		Urgency:    u * b.Deadline.weight(),
		Importance: i * b.Priority.weight(),
		Momentum:   m * b.Momentum.weight(),
		Age:        age,
	}
	out.Total = out.Urgency + out.Importance + out.Momentum + out.Age
	return out
}

// sequenceComponentsFor is the live form used by callers that want the
// breakdown for display (detail view). Reads activeBiases and time.Now.
func sequenceComponentsFor(t *todo.Todo) sequenceComponents {
	return sequenceComponentsAt(time.Now(), t, activeBiases)
}

// sequenceScore is the total persisted score: written into todos.sequence on
// every save and read by the Sequence sort. Live callers (render loop, sort)
// invoke this directly; the per-dimension breakdown is exposed via
// sequenceComponentsFor for the detail view.
func sequenceScore(t *todo.Todo) float64 {
	return sequenceComponentsAt(time.Now(), t, activeBiases).Total
}
