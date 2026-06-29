package main

import (
	"sort"
	"strings"

	"taskr/todo"
)

// Selectors are pure functions that derive a view from the source of truth (the
// task set) plus the relevant UI parameters (search text, sort mode, …). They
// hold no state and touch no model fields, which makes each one independently
// testable and impossible to leave "stale". The cache currently memoizes their
// results; the selectors are the single definition of each derivation.

// ── Predicates ────────────────────────────────────────────────────────────────

func todoMatchesSearch(t todo.Todo, search string) bool {
	return compileSearch(search)(t)
}

// compileSearch lowers the query once and returns a per-task predicate, so the
// active/done scan doesn't re-lower the (constant) search string for every task.
// todoMatchesSearch is the single-shot form; both share this one definition.
//
// The query is tokenised on whitespace and the tokens are ANDed together,
// reusing the quick-add vocabulary: `#tag` (tag substring), `@project` (project
// substring), `p:high|med|low`, `due:<date` / `due:>date` / `due:date`
// (comparison, "<"/">"/"<="/">=" or exact day), and the bare keyword `overdue`.
// Any leftover bare words are joined and fuzzy-matched against the title
// (subsequence, so "grcry" finds "Buy groceries"). The two click-driven sentinels
// — empty (match all) and untaggedKey (no tags) — keep their exact-string meaning.
func compileSearch(search string) func(todo.Todo) bool {
	switch search {
	case "":
		return func(todo.Todo) bool { return true }
	case untaggedKey:
		return func(t todo.Todo) bool { return len(t.Tags) == 0 }
	}

	var preds []func(todo.Todo) bool
	var titleWords []string

	for _, tok := range strings.Fields(search) {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(tok, "#") && len(tok) > 1:
			q := strings.ToLower(tok[1:])
			preds = append(preds, func(t todo.Todo) bool {
				for _, tag := range t.Tags {
					if strings.Contains(strings.ToLower(tag), q) {
						return true
					}
				}
				return false
			})
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			q := strings.ToLower(tok[1:])
			preds = append(preds, func(t todo.Todo) bool {
				return strings.Contains(strings.ToLower(t.Project), q)
			})
		case strings.HasPrefix(lower, "p:"):
			if p, ok := parsePriorityFilter(strings.TrimPrefix(lower, "p:")); ok {
				preds = append(preds, func(t todo.Todo) bool { return t.Priority == p })
			} else {
				titleWords = append(titleWords, tok)
			}
		case strings.HasPrefix(lower, "due:"):
			if f, ok := parseDueFilter(strings.TrimPrefix(lower, "due:")); ok {
				preds = append(preds, f)
			} else {
				titleWords = append(titleWords, tok)
			}
		case lower == "overdue":
			preds = append(preds, func(t todo.Todo) bool { return t.IsOverdue() })
		default:
			titleWords = append(titleWords, tok)
		}
	}

	if len(titleWords) > 0 {
		titleQuery := strings.ToLower(strings.Join(titleWords, " "))
		preds = append(preds, func(t todo.Todo) bool {
			return subsequenceFold(t.Title, titleQuery)
		})
	}

	if len(preds) == 0 {
		return func(todo.Todo) bool { return true }
	}
	return func(t todo.Todo) bool {
		for _, p := range preds {
			if !p(t) {
				return false
			}
		}
		return true
	}
}

// subsequenceFold reports whether every rune of needle appears in haystack in
// order (not necessarily contiguous), case-insensitively. Empty needle matches.
// This is the fuzzy form of strings.Contains: "grcry" matches "Buy groceries".
func subsequenceFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	n := []rune(strings.ToLower(needle))
	i := 0
	for _, r := range strings.ToLower(haystack) {
		if r == n[i] {
			if i++; i == len(n) {
				return true
			}
		}
	}
	return false
}

// parsePriorityFilter maps a `p:` filter value to a priority, reusing the
// quick-add spellings. The bool is false for anything unrecognised so the caller
// can fall back to treating the token as a literal title word.
func parsePriorityFilter(s string) (todo.Priority, bool) {
	switch s {
	case "high", "h":
		return todo.PriorityHigh, true
	case "medium", "med", "m":
		return todo.PriorityMedium, true
	case "low", "l":
		return todo.PriorityLow, true
	}
	return 0, false
}

// parseDueFilter builds a due-date predicate from a `due:` filter value. An
// optional leading "<", ">", "<=" or ">=" sets the comparison (else exact day);
// the rest is parsed with the same parseDueDate the quick-add path uses. Tasks
// with no due date never match a due: filter. Returns false if the date is
// unparseable so the caller can fall back to a literal title word.
func parseDueFilter(spec string) (func(todo.Todo) bool, bool) {
	op, rest := "=", spec
	switch {
	case strings.HasPrefix(spec, "<="):
		op, rest = "<=", spec[2:]
	case strings.HasPrefix(spec, ">="):
		op, rest = ">=", spec[2:]
	case strings.HasPrefix(spec, "<"):
		op, rest = "<", spec[1:]
	case strings.HasPrefix(spec, ">"):
		op, rest = ">", spec[1:]
	}
	d, err := parseDueDate(rest)
	if err != nil {
		return nil, false
	}
	day := startOfDay(d)
	return func(t todo.Todo) bool {
		if t.DueDate.IsZero() {
			return false
		}
		td := startOfDay(t.DueDate)
		switch op {
		case "<":
			return td.Before(day)
		case "<=":
			return !td.After(day)
		case ">":
			return td.After(day)
		case ">=":
			return !td.Before(day)
		default:
			return td.Equal(day)
		}
	}, true
}

func todoMatchesFocus(t todo.Todo, focus bool) bool {
	if !focus {
		return true
	}
	return t.IsOverdue() || t.IsDueToday()
}

// ── View selectors ────────────────────────────────────────────────────────────

// selectActiveDone splits the top-level (non-subtask) tasks into the active and
// done lists, applying the search filter to both and the focus filter to active
// only, then sorts each by the given mode. In Sequence mode, parents inherit
// the max score of their descendants for ranking only — the displayed score
// stays the parent's own — so a high-priority subtask pulls its parent up
// rather than hiding beneath a calmer one.
func selectActiveDone(todos []todo.Todo, search string, focus bool, sortMode taskSortMode) (active, done []todo.Todo) {
	match := compileSearch(search)
	for _, t := range todos {
		if t.ParentID != "" {
			continue
		}
		switch {
		case t.Status == todo.Pending && match(t) && todoMatchesFocus(t, focus):
			active = append(active, t)
		case t.Status == todo.Done && match(t):
			done = append(done, t)
		}
	}
	if sortMode == taskSortSequence {
		rollup := descendantScoreRollup(todos)
		rollup = dependencyScoreRollup(todos, rollup)
		sortTodosBySequenceWithRollup(active, rollup)
		sortTodosBySequenceWithRollup(done, rollup)
	} else {
		sortTodosByMode(active, sortMode)
		sortTodosByMode(done, sortMode)
	}
	return active, done
}

// descendantScoreRollup walks the full task slice and returns, per top-level
// ID, the max sequenceScore observed across all of its transitive subtasks.
// Pure: builds its own parent index in one pass and follows ParentID chains
// instead of relying on the model's subtaskOf cache. Tasks without subtasks
// don't appear in the map.
func descendantScoreRollup(todos []todo.Todo) map[string]float64 {
	if len(todos) == 0 {
		return nil
	}
	idx := make(map[string]int, len(todos))
	for i := range todos {
		idx[todos[i].ID] = i
	}
	rollup := make(map[string]float64, len(todos))
	for i := range todos {
		if todos[i].ParentID == "" {
			continue
		}
		// Walk up to the top-level ancestor, lifting the boost at every
		// level so a deeply-nested high-pri grandchild reaches the root.
		score := sequenceScore(&todos[i])
		cur := todos[i].ParentID
		for cur != "" {
			if rollup[cur] < score {
				rollup[cur] = score
			}
			pi, ok := idx[cur]
			if !ok {
				break
			}
			cur = todos[pi].ParentID
		}
	}
	return rollup
}

// dependencyScoreRollup augments base (the subtask rollup) with dependency
// boosts: a still-pending task that another pending task depends on inherits
// that dependent's urgency, so a blocker can't sort below the work it's holding
// up — the prerequisite for an urgent task surfaces right above it (critical-path
// behaviour). Propagation is transitive (a chain lifts end-to-end) and cycle-safe.
// effBase is max(own score, subtask rollup), so subtask and dependency boosts
// compose. Returns base unchanged when no task depends on a pending one.
// depBoostEpsilon is the per-edge nudge that keeps a boosted blocker strictly
// ahead of the dependent it inherited from.
const depBoostEpsilon = 0.001

func dependencyScoreRollup(todos []todo.Todo, base map[string]float64) map[string]float64 {
	if len(todos) == 0 {
		return base
	}
	pending := make(map[string]bool, len(todos))
	for i := range todos {
		if todos[i].Status != todo.Done {
			pending[todos[i].ID] = true
		}
	}
	// dependents maps a pending task to the pending tasks that depend on it.
	dependents := make(map[string][]string)
	for i := range todos {
		if todos[i].Status == todo.Done {
			continue
		}
		for _, depID := range todos[i].Dependencies {
			if pending[depID] {
				dependents[depID] = append(dependents[depID], todos[i].ID)
			}
		}
	}
	if len(dependents) == 0 {
		return base
	}
	idx := make(map[string]int, len(todos))
	for i := range todos {
		idx[todos[i].ID] = i
	}
	effBase := func(id string) float64 {
		s := sequenceScore(&todos[idx[id]])
		if b, ok := base[id]; ok && b > s {
			s = b
		}
		return s
	}
	// eff(id) = max(effBase(id), max eff over its dependents). Memoised DFS;
	// visiting guards back-edges so a dependency cycle terminates.
	eff := make(map[string]float64, len(dependents))
	visiting := make(map[string]bool)
	var compute func(id string) float64
	compute = func(id string) float64 {
		if v, ok := eff[id]; ok {
			return v
		}
		best := effBase(id)
		if visiting[id] {
			return best
		}
		visiting[id] = true
		for _, dep := range dependents[id] {
			// + epsilon so a blocker sorts strictly above its dependent rather
			// than merely tying (the score-tie backstop is CreatedAt/ID, which
			// could otherwise place the dependent first). Compounds per chain
			// hop; far below the %.1f the score column rounds to, so invisible.
			if s := compute(dep) + depBoostEpsilon; s > best {
				best = s
			}
		}
		visiting[id] = false
		eff[id] = best
		return best
	}
	out := make(map[string]float64, len(base)+len(dependents))
	for k, v := range base {
		out[k] = v
	}
	for id := range dependents {
		if s := compute(id); s > out[id] {
			out[id] = s
		}
	}
	return out
}

// selectSortedTags returns the unique tags across all tasks (sorted by mode)
// plus the count of untagged tasks (total and done) shown as a virtual row.
func selectSortedTags(todos []todo.Todo, mode tagSortMode, stats map[string]tagStats) (sorted []string, untaggedTotal, untaggedDone int) {
	seen := make(map[string]struct{}, len(stats))
	for i := range todos {
		// The Tasks tab list excludes subtasks, so counting them here
		// would inflate row counts (or surface a tag only present on
		// subtasks) and leave pressing Enter on the row showing an
		// empty list.
		if todos[i].ParentID != "" {
			continue
		}
		if len(todos[i].Tags) == 0 {
			untaggedTotal++
			if todos[i].Status == todo.Done {
				untaggedDone++
			}
			continue
		}
		for _, tag := range todos[i].Tags {
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			sorted = append(sorted, tag)
		}
	}
	sortTags(sorted, mode, stats)
	return sorted, untaggedTotal, untaggedDone
}

// selectProjects returns the distinct non-empty project names (sorted), filtered
// by the search query.
func selectProjects(todos []todo.Todo, search string) []string {
	seen := make(map[string]struct{})
	var projects []string
	for i := range todos {
		p := todos[i].Project
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		projects = append(projects, p)
	}
	sort.Strings(projects)

	if search != "" {
		q := strings.ToLower(search)
		filtered := projects[:0]
		for _, p := range projects {
			if strings.Contains(strings.ToLower(p), q) {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}
	return projects
}

// learningView pairs a learning with its parent task's current tags. The tags
// are derived here (not stored on todo.Learning), so they always reflect the
// task. Fields of the embedded Learning (ID, Text, CreatedAt) promote, so the
// view is a drop-in for the old []todo.Learning at call sites.
type learningView struct {
	todo.Learning
	Tags []string
}

// selectLearnings gathers every task's learnings (tagged with the parent's
// tags), filters by the search query (a leading '#' searches those tags), and
// sorts by mode.
func selectLearnings(todos []todo.Todo, search string, sortMode learningSortMode) []learningView {
	var result []learningView
	for i := range todos {
		for _, l := range todos[i].Learnings {
			result = append(result, learningView{Learning: l, Tags: todos[i].Tags})
		}
	}

	if search != "" {
		filtered := result[:0]
		q := strings.ToLower(search)
		isTagSearch := strings.HasPrefix(q, "#")
		tagQuery := strings.TrimPrefix(q, "#")
		for _, l := range result {
			if isTagSearch {
				for _, tag := range l.Tags {
					if strings.Contains(strings.ToLower(tag), tagQuery) {
						filtered = append(filtered, l)
						break
					}
				}
			} else if strings.Contains(strings.ToLower(l.Text), q) {
				filtered = append(filtered, l)
			}
		}
		result = filtered
	}

	switch sortMode {
	case learningSortAlpha:
		sort.SliceStable(result, func(i, j int) bool {
			return strings.ToLower(result[i].Text) < strings.ToLower(result[j].Text)
		})
	default:
		sort.SliceStable(result, func(i, j int) bool {
			return result[i].CreatedAt.After(result[j].CreatedAt)
		})
	}
	return result
}
