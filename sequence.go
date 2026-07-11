package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"taskr/todo"
)

// sequence.go is the sequencing engine: the rule that decides the "Sequence"
// sort order and the value persisted in the todos.sequence column on every save.
//
// The score is the design's Normalized Power Scale: three 0–10 dimensions,
// each multiplied by a user-tunable bias (Relaxed=0.5, Balanced=1.0,
// Intense=2.0), plus two small unweighted terms — Size and Age — so quick
// wins edge ahead of equal peers and old tasks always eventually surface for
// cleanup or completion.
//
//	Score = U·Wd + I·Wp + M·Wm + Size + Age
//
//	U  Urgency    closeness to deadline (0..10+)
//	I  Importance priority bucket (0/5/10)
//	M  Momentum   activity heat: 10 when the task or its project saw activity
//	              (completion, timer, comment) inside momentumWindow, 5 when
//	              only one of its tags did, 0 cold
//	Size          quick-win nudge (S=2, M=1, L=0)
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
	// Aging gates the per-day Age contribution. true (default) keeps the rot
	// guard on; toggling off zeros the Age term so a brand-new task and a
	// year-old task with the same Deadline/Priority/Momentum score identically.
	Aging bool
}

// defaultBiases is the all-Balanced, aging-on configuration that the engine
// boots into before settings.json is read.
func defaultBiases() biases {
	return biases{Aging: true}
}

// activeBiases is the package-level setting the score functions read at the
// time they're called. Settings.go's load/save path sets it via applyBiases on
// startup and whenever the user cycles a bias, matching the pattern already in
// use for themes (applyTheme) and language (applyLang). It boots into the
// neutral default (all Balanced, aging on); settings.json may overwrite later.
var activeBiases = defaultBiases()

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
// All-uniform presets get their named personality. If exactly one axis
// deviates from Balanced (e.g. Momentum=Intense while the other two stay
// Balanced), name that axis's flavor — the user picked a single dimension to
// emphasize and the label should reflect it, not collapse to "Custom".
// Anything more entangled still shows as "Custom".
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
		return "Copilot", "Balanced: equally weighs priorities, deadlines, and recent activity."
	}
	if name, descr, ok := singleAxisPersonality(b); ok {
		return name, descr
	}
	return "Custom", "Mixed biases — score reflects your tuned weights."
}

// singleAxisPersonality names the configuration when exactly one of the three
// biases has been moved off Balanced. Returns ok=false otherwise so personality
// falls back to "Custom".
func singleAxisPersonality(b biases) (name, descr string, ok bool) {
	count := 0
	if b.Deadline != biasBalanced {
		count++
	}
	if b.Priority != biasBalanced {
		count++
	}
	if b.Momentum != biasBalanced {
		count++
	}
	if count != 1 {
		return "", "", false
	}
	switch {
	case b.Deadline == biasIntense:
		return "Deadline Hawk", "Tasks closest to their due date dominate the ranking.", true
	case b.Deadline == biasRelaxed:
		return "Deadline Cruise", "Due dates barely move the ranking.", true
	case b.Priority == biasIntense:
		return "Importance First", "High priorities outweigh everything else.", true
	case b.Priority == biasRelaxed:
		return "Importance Casual", "Priority is treated as a hint, not a driver.", true
	case b.Momentum == biasIntense:
		return "Flow State", "Projects with recent activity dominate — ride the streak.", true
	case b.Momentum == biasRelaxed:
		return "Fresh Eyes", "Recent activity barely moves the ranking; cold projects get equal footing.", true
	}
	return "", "", false
}

// ── Activity heat (the Momentum signal) ──────────────────────────────────────

// momentumWindow is how far back an activity signal still counts as "recent".
const momentumWindow = 48 * time.Hour

// activityHeat is the recent-activity snapshot the Momentum dimension reads:
// which tasks, projects, and tags saw a completion, a time entry, or a comment
// inside momentumWindow. The zero value means everything is cold (momentum 0),
// which is what a process that never computes heat — the sync server
// persisting merged rows — correctly falls back to; every user-facing surface
// recomputes it on load or cache refresh.
type activityHeat struct {
	tasks    map[string]bool
	projects map[string]bool
	tags     map[string]bool
}

// computeActivityHeat scans the full task set (done tasks included — their
// completions are the strongest signal) and marks the task, its project, and
// its tags hot when any signal lands inside the window ending at `now`.
func computeActivityHeat(now time.Time, todos []todo.Todo) activityHeat {
	cutoff := now.Add(-momentumWindow)
	recent := func(ts time.Time) bool { return !ts.IsZero() && ts.After(cutoff) }
	return scanHeat(todos, func(t *todo.Todo) bool {
		if recent(t.CompletedAt) {
			return true
		}
		for _, c := range t.Comments {
			if c.DeletedAt.IsZero() && (recent(c.CreatedAt) || recent(c.ModifiedAt)) {
				return true
			}
		}
		for _, e := range t.TimeEntries {
			if e.IsRunning() || recent(e.StartedAt) || recent(e.StoppedAt) {
				return true
			}
		}
		return false
	})
}

// computeActivityHeatAt reconstructs the heat snapshot as it stood at a past
// moment `at` — used by the stats --seq miss analysis to re-score a completion
// with the momentum signal its rank stamp actually saw. Unlike the live
// computeActivityHeat it bounds signals STRICTLY before `at`: the completion
// being analyzed lands at exactly `at`, and captureSeqRankAtDone stamps the
// rank before Toggle flips the status, so the task's own completion must not
// count toward its own momentum. The live path keeps its open upper edge on
// purpose — cross-device clock skew after a sync can put a legitimate hot
// signal slightly in the future, and dropping it there would be wrong.
func computeActivityHeatAt(at time.Time, todos []todo.Todo) activityHeat {
	cutoff := at.Add(-momentumWindow)
	inWindow := func(ts time.Time) bool {
		return !ts.IsZero() && ts.After(cutoff) && ts.Before(at)
	}
	return scanHeat(todos, func(t *todo.Todo) bool {
		if inWindow(t.CompletedAt) {
			return true
		}
		for _, c := range t.Comments {
			if c.DeletedAt.IsZero() && (inWindow(c.CreatedAt) || inWindow(c.ModifiedAt)) {
				return true
			}
		}
		for _, e := range t.TimeEntries {
			// A time entry is a signal if it overlapped the window at all:
			// started before `at` and not stopped before the window opened.
			// (IsRunning is a *current* fact, meaningless for a past moment.)
			if !e.StartedAt.IsZero() && e.StartedAt.Before(at) &&
				(e.StoppedAt.IsZero() || e.StoppedAt.After(cutoff)) {
				return true
			}
		}
		return false
	})
}

// scanHeat builds an activityHeat by testing every live task against `hot`
// and marking the task, its project, and its tags when it fires. The two
// heat builders above share it; only their notion of "recent" differs.
func scanHeat(todos []todo.Todo, hot func(*todo.Todo) bool) activityHeat {
	h := activityHeat{
		tasks:    make(map[string]bool),
		projects: make(map[string]bool),
		tags:     make(map[string]bool),
	}
	for i := range todos {
		t := &todos[i]
		if t.Deleted || !hot(t) {
			continue
		}
		h.tasks[t.ID] = true
		if t.Project != "" {
			h.projects[t.Project] = true
		}
		for _, tag := range t.Tags {
			h.tags[tag] = true
		}
	}
	return h
}

// activeHeat is the package-level snapshot the live score functions read,
// following the applyTheme/applyLang/applyBiases pattern. Refreshed by
// refreshCaches (TUI) and loadForCLI (CLI) so both surfaces rank identically.
var activeHeat activityHeat

func applyActivityHeat(h activityHeat) { activeHeat = h }

// ── Per-dimension contributions ──────────────────────────────────────────────

// dimensionsAt is the pure core of the formula: given `now`, a task, and the
// activity-heat snapshot, return the five un-weighted dimension scores.
// Splitting `now` and `heat` out lets tests pin both without monkey-patching.
func dimensionsAt(now time.Time, t *todo.Todo, heat activityHeat) (u, i, m, size, age float64) {
	if t == nil || t.Status == todo.Done {
		return 0, 0, 0, 0, 0
	}
	return rawDimensionsAt(now, t, heat)
}

// rawDimensionsAt is dimensionsAt without the Done guard. The stats --seq
// miss analysis re-scores *completed* tasks as of their completion moment,
// where the live guard (Done scores 0) would erase exactly the data it needs.
// Live scoring must keep going through dimensionsAt.
func rawDimensionsAt(now time.Time, t *todo.Todo, heat activityHeat) (u, i, m, size, age float64) {
	u = urgencyDim(now, t.DueDate)
	i = importanceDim(t.Priority)
	m = momentumDim(t, heat)
	size = sizeDim(t.Size)
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

// momentumDim is the activity-heat lookup: 10 when the task itself or its
// project was touched (completion, timer, comment) inside momentumWindow,
// 5 when only one of its tags was, 0 cold. "The thing you're already deep in
// comes next" — the informal ordering that dependency edges encode explicitly,
// available even when no edges were recorded.
func momentumDim(t *todo.Todo, heat activityHeat) float64 {
	if heat.tasks[t.ID] || (t.Project != "" && heat.projects[t.Project]) {
		return 10
	}
	for _, tag := range t.Tags {
		if heat.tags[tag] {
			return 5
		}
	}
	return 0
}

// sizeDim is the quick-win nudge that survived the momentum rework: Small=2,
// Medium=1, Large=0, added unweighted. Size was never momentum — it's a
// static property — so it stopped claiming that axis and became a small
// tie-flavored bonus instead: quick wins still edge ahead of equal peers
// without drowning the real signals.
func sizeDim(s todo.Size) float64 {
	switch s {
	case todo.SizeSmall:
		return 2
	case todo.SizeLarge:
		return 0
	default:
		return 1
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
// already weighted (i.e. multiplied by its bias) so the five values sum to
// Total — the user sees the actual contributions, not the raw 0..10 axes.
type sequenceComponents struct {
	Urgency    float64
	Importance float64
	Momentum   float64
	Size       float64
	Age        float64
	Total      float64
}

// sequenceComponentsAt is the testable assembly: pure, takes `now`, biases,
// and the heat snapshot explicitly.
func sequenceComponentsAt(now time.Time, t *todo.Todo, b biases, heat activityHeat) sequenceComponents {
	u, i, m, size, age := dimensionsAt(now, t, heat)
	if !b.Aging {
		age = 0
	}
	out := sequenceComponents{
		Urgency:    u * b.Deadline.weight(),
		Importance: i * b.Priority.weight(),
		Momentum:   m * b.Momentum.weight(),
		Size:       size,
		Age:        age,
	}
	out.Total = out.Urgency + out.Importance + out.Momentum + out.Size + out.Age
	return out
}

// sequenceComponentsFor is the live form used by callers that want the
// breakdown for display (detail view). Reads activeBiases, activeHeat, and
// time.Now.
func sequenceComponentsFor(t *todo.Todo) sequenceComponents {
	return sequenceComponentsAt(time.Now(), t, activeBiases, activeHeat)
}

// sequenceScore is the total persisted score: written into todos.sequence on
// every save and read by the Sequence sort. Live callers (render loop, sort)
// invoke this directly; the per-dimension breakdown is exposed via
// sequenceComponentsFor for the detail view.
func sequenceScore(t *todo.Todo) float64 {
	return sequenceComponentsAt(time.Now(), t, activeBiases, activeHeat).Total
}

// rankTopBySequenceWith is the pure, testable form of rankTopBySequence: it
// accepts explicit biases, a heat snapshot, and a clock so callers can compute
// a preview ranking without touching the activeBiases / activeHeat globals.
// The result is the same critical-path ordering (subtask + dependency rollups
// applied) as the live path — only the scoring inputs differ.
func rankTopBySequenceWith(todos []todo.Todo, b biases, heat activityHeat, now time.Time) []todo.Todo {
	return rankTopBySequenceBy(todos, func(t *todo.Todo) float64 {
		return sequenceComponentsAt(now, t, b, heat).Total
	})
}

// ── Sequence hit rate ─────────────────────────────────────────────────────────

const (
	seqHitWindow = 50 // completions the hit-rate stat looks back over
	seqHitTopN   = 5  // a "hit" closed while ranked in the top N
)

// captureSeqRankAtDone stamps t.SeqRankAtDone with the task's 1-based
// position in the ranking `taskr top` would have shown at this moment. The
// user-initiated close paths (CLI done, TUI toggle, confirm-close-parent)
// call it just before Toggle flips the status; auto-closed parents and
// recurrence spawns don't, so the metric only reads deliberate picks —
// "when you finished something, was it what the engine suggested".
func captureSeqRankAtDone(todos []todo.Todo, t *todo.Todo) {
	t.SeqRankAtDone = 0
	if t.ParentID != "" {
		return
	}
	for i, row := range rankTopBySequence(todos) {
		if row.ID == t.ID {
			t.SeqRankAtDone = i + 1
			return
		}
	}
}

// ratedCompletions returns the rank-stamped completions the hit-rate metric
// reads — done, top-level, stamped, timestamped — most recent first, truncated
// to `window`. Shared by sequenceHitStats and analyzeSeqMisses so the two
// always agree on which completions count.
func ratedCompletions(todos []todo.Todo, window int) []*todo.Todo {
	var recent []*todo.Todo
	for i := range todos {
		t := &todos[i]
		if t.Status != todo.Done || t.ParentID != "" || t.SeqRankAtDone <= 0 || t.CompletedAt.IsZero() {
			continue
		}
		recent = append(recent, t)
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].CompletedAt.After(recent[j].CompletedAt) })
	if len(recent) > window {
		recent = recent[:window]
	}
	return recent
}

// sequenceHitStats reports, over the `window` most recent rank-stamped
// completions, how many closed inside the top seqHitTopN. rated counts the
// completions considered, so callers can render "39/50" and hide the stat
// entirely while no history exists.
func sequenceHitStats(todos []todo.Todo, window int) (hits, rated int) {
	for _, t := range ratedCompletions(todos, window) {
		rated++
		if t.SeqRankAtDone <= seqHitTopN {
			hits++
		}
	}
	return hits, rated
}

// ── Sequence miss analysis (stats --seq) ─────────────────────────────────────
//
// The hit rate says how often a finished task was a top-N pick; this section
// says WHY the misses weren't. For every rated completion the five score
// dimensions are recomputed as of its CompletedAt — activity heat rebuilt from
// the historical record via computeActivityHeatAt — then averaged separately
// for hits and misses. A dimension where misses lag hits is one the engine
// values more than the user's actual picking behaviour does (they finished
// those tasks anyway), so its bias knob is a Relaxed candidate; a dimension
// where misses *beat* hits is followed more than the engine weights it —
// an Intense candidate.
//
// Known approximation, deliberate: dimensions are recomputed from each task's
// CURRENT fields (a due date or priority edited after completion skews that
// reading) and weighted by the CURRENT biases. Exact readings would need the
// components stamped at done-time — a schema migration not worth taking until
// this reconstruction proves its keep.

// seqDimCount / seqDimNames fix the dimension order used by every [seqDimCount]
// array below: Deadline, Priority, Momentum, Size, Age. The first three are
// the knobbed dimensions (they have a Settings bias); Size and Age are shown
// in the table but never suggested on.
const seqDimCount = 5

var seqDimNames = [seqDimCount]string{"Deadline", "Priority", "Momentum", "Size", "Age"}

type seqMissRow struct {
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Rank        int                  `json:"rank"`
	CompletedAt time.Time            `json:"completed_at"`
	Dims        [seqDimCount]float64 `json:"dims"`
	// Weakest is the dimension where this miss fell furthest below the hit
	// average — the single best answer to "what buried it". Empty when there
	// are no hits to compare against.
	Weakest string `json:"weakest,omitempty"`
}

type seqAnalysis struct {
	Hits       int                  `json:"hits"`
	Rated      int                  `json:"rated"`
	TopN       int                  `json:"top_n"`
	Window     int                  `json:"window"`
	Dimensions [seqDimCount]string  `json:"dimensions"` // names the array order for JSON consumers
	HitAvg     [seqDimCount]float64 `json:"hit_avg"`
	MissAvg    [seqDimCount]float64 `json:"miss_avg"`
	Gap        [seqDimCount]float64 `json:"gap"` // MissAvg − HitAvg
	Misses     []seqMissRow         `json:"misses"`
}

// analyzeSeqMisses is the pure fold behind stats --seq: re-score every rated
// completion at its own CompletedAt, split hits from misses, and aggregate the
// weighted per-dimension contributions. Misses come back most recent first
// (ratedCompletions' order). heatSource is the task set momentum is
// reconstructed from — pass the FULL set even when `todos` is a filtered
// stats scope, or completions outside the filter stop warming their
// project/tags and the momentum readings go colder than the rank stamp saw.
func analyzeSeqMisses(todos, heatSource []todo.Todo, window int, b biases) seqAnalysis {
	a := seqAnalysis{TopN: seqHitTopN, Window: window, Dimensions: seqDimNames}
	type scored struct {
		t    *todo.Todo
		dims [seqDimCount]float64
	}
	var missRows []scored
	var hitSum, missSum [seqDimCount]float64
	for _, t := range ratedCompletions(todos, window) {
		heat := computeActivityHeatAt(t.CompletedAt, heatSource)
		u, i, m, size, age := rawDimensionsAt(t.CompletedAt, t, heat)
		if !b.Aging {
			age = 0
		}
		dims := [seqDimCount]float64{
			u * b.Deadline.weight(),
			i * b.Priority.weight(),
			m * b.Momentum.weight(),
			size,
			age,
		}
		a.Rated++
		if t.SeqRankAtDone <= seqHitTopN {
			a.Hits++
			for d := range dims {
				hitSum[d] += dims[d]
			}
			continue
		}
		missRows = append(missRows, scored{t, dims})
		for d := range dims {
			missSum[d] += dims[d]
		}
	}
	misses := a.Rated - a.Hits
	for d := 0; d < seqDimCount; d++ {
		if a.Hits > 0 {
			a.HitAvg[d] = hitSum[d] / float64(a.Hits)
		}
		if misses > 0 {
			a.MissAvg[d] = missSum[d] / float64(misses)
		}
		a.Gap[d] = a.MissAvg[d] - a.HitAvg[d]
	}
	for _, r := range missRows {
		row := seqMissRow{
			ID:          r.t.ID,
			Title:       r.t.Title,
			Rank:        r.t.SeqRankAtDone,
			CompletedAt: r.t.CompletedAt,
			Dims:        r.dims,
		}
		if a.Hits > 0 {
			worst, worstIdx := 0.0, -1
			for d := range r.dims {
				if deficit := a.HitAvg[d] - r.dims[d]; deficit > worst {
					worst, worstIdx = deficit, d
				}
			}
			if worstIdx >= 0 {
				row.Weakest = seqDimNames[worstIdx]
			}
		}
		a.Misses = append(a.Misses, row)
	}
	return a
}

// seqSuggestionMinMisses / seqSuggestionMinGap gate the bias hint: with fewer
// misses than the floor any pattern is noise, and a gap under the floor (in
// weighted score points) isn't worth moving a knob over.
const (
	seqSuggestionMinMisses = 3
	seqSuggestionMinGap    = 1.0
)

// seqSuggestion turns the gap table into at most one actionable line: the
// knobbed dimension (Deadline/Priority/Momentum) with the largest |gap|, and
// which way to move its bias. Empty when there isn't enough signal to say
// anything; an explicit "looks calibrated" when there is signal but no
// dominant pattern.
func seqSuggestion(a seqAnalysis, b biases) string {
	misses := a.Rated - a.Hits
	if a.Hits == 0 || misses < seqSuggestionMinMisses {
		return ""
	}
	knobs := [3]biasLevel{b.Deadline, b.Priority, b.Momentum}
	best, bestGap := -1, 0.0
	for d := 0; d < len(knobs); d++ {
		if g := a.Gap[d]; math.Abs(g) > math.Abs(bestGap) {
			best, bestGap = d, g
		}
	}
	if best < 0 || math.Abs(bestGap) < seqSuggestionMinGap {
		return "No dominant pattern in the misses — the biases look calibrated."
	}
	name := seqDimNames[best]
	if bestGap < 0 {
		if knobs[best] == biasRelaxed {
			return fmt.Sprintf("Misses were weakest on %s; your %s: relaxed setting already leans that way.", name, name)
		}
		return fmt.Sprintf("Misses were weakest on %s — you finish tasks the engine buried for scoring low there. Consider %s: relaxed (Settings).", name, name)
	}
	if knobs[best] == biasIntense {
		return fmt.Sprintf("Misses scored higher on %s than hits; your %s: intense setting already leans that way.", name, name)
	}
	return fmt.Sprintf("Misses scored higher on %s than hits — you follow it more than the engine weights it. Consider %s: intense (Settings).", name, name)
}
