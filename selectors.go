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
func compileSearch(search string) func(todo.Todo) bool {
	switch {
	case search == "":
		return func(todo.Todo) bool { return true }
	case search == untaggedKey:
		return func(t todo.Todo) bool { return len(t.Tags) == 0 }
	case strings.HasPrefix(search, "#"):
		tagQuery := strings.ToLower(strings.TrimPrefix(search, "#"))
		return func(t todo.Todo) bool {
			for _, tag := range t.Tags {
				if strings.Contains(strings.ToLower(tag), tagQuery) {
					return true
				}
			}
			return false
		}
	default:
		titleQuery := strings.ToLower(search)
		return func(t todo.Todo) bool {
			return strings.Contains(strings.ToLower(t.Title), titleQuery)
		}
	}
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
