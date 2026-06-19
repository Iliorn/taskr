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
	if search == "" {
		return true
	}
	if search == untaggedKey {
		return len(t.Tags) == 0
	}
	if strings.HasPrefix(search, "#") {
		tagQuery := strings.ToLower(strings.TrimPrefix(search, "#"))
		for _, tag := range t.Tags {
			if strings.Contains(strings.ToLower(tag), tagQuery) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(t.Title), strings.ToLower(search))
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
// only, then sorts each by the given mode.
func selectActiveDone(todos []todo.Todo, search string, focus bool, sortMode taskSortMode) (active, done []todo.Todo) {
	for _, t := range todos {
		if t.ParentID != "" {
			continue
		}
		switch {
		case t.Status == todo.Pending && todoMatchesSearch(t, search) && todoMatchesFocus(t, focus):
			active = append(active, t)
		case t.Status == todo.Done && todoMatchesSearch(t, search):
			done = append(done, t)
		}
	}
	sortTodosByMode(active, sortMode)
	sortTodosByMode(done, sortMode)
	return active, done
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
