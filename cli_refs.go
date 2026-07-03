package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"taskr/todo"
)

// loadForCLI opens the store with the user's persisted biases applied so any
// score-based output ranks the same way the TUI would.
func loadForCLI() (Repository, []todo.Todo, error) {
	settings, sErr := loadSettings()
	if sErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v (using defaults)\n", sErr)
	}
	applyBiases(biasesFromSettings(settings))
	repo := newSQLiteRepo()
	todos, err := repo.Load()
	return repo, todos, err
}

// findTaskByRef matches a task by either an id-prefix or a title substring,
// in that order. The id path takes precedence so scripts and aliases remain
// deterministic: a hex-shaped query that happens to appear in a task title
// won't ambiguously swap which task you operate on.
//
// Matching rules:
//   - ID prefix exactly matches one task            → return it
//   - ID prefix matches multiple tasks              → error with candidates
//     (no fallback — ambiguity is the user's call)
//   - ID prefix matches zero, title substring …
//     … matches one task                         → return it
//     … matches multiple                         → error with candidates
//     … matches zero                             → error "no task matches"
//
// Both comparisons are case-insensitive.
func findTaskByRef(todos []todo.Todo, ref string) (*todo.Todo, error) {
	t, _, err := findTaskByRefKind(todos, ref)
	return t, err
}

// refMatch reports which pass of findTaskByRefKind resolved the ref. Verbs
// with destructive semantics (delete) use it to require confirmation on the
// fuzzy title path while keeping exact id/prefix refs script-fast.
type refMatch int

const (
	refMatchID refMatch = iota
	refMatchTitle
)

func findTaskByRefKind(todos []todo.Todo, ref string) (*todo.Todo, refMatch, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, refMatchID, fmt.Errorf("empty task reference (need id-prefix or title substring)")
	}
	q := strings.ToLower(ref)

	// Pass 1: id-prefix.
	var idMatches []int
	for i := range todos {
		if strings.HasPrefix(strings.ToLower(todos[i].ID), q) {
			idMatches = append(idMatches, i)
		}
	}
	switch len(idMatches) {
	case 1:
		return &todos[idMatches[0]], refMatchID, nil
	case 0:
		// fall through to title-substring
	default:
		return nil, refMatchID, ambiguousMatchError("id prefix", ref, todos, idMatches)
	}

	// Pass 2: title substring (case-insensitive).
	var titleMatches []int
	for i := range todos {
		if strings.Contains(strings.ToLower(todos[i].Title), q) {
			titleMatches = append(titleMatches, i)
		}
	}
	switch len(titleMatches) {
	case 0:
		return nil, refMatchTitle, fmt.Errorf("no task matches %q (tried id-prefix and title substring)", ref)
	case 1:
		return &todos[titleMatches[0]], refMatchTitle, nil
	default:
		return nil, refMatchTitle, ambiguousMatchError("title", ref, todos, titleMatches)
	}
}

func ambiguousMatchError(kind, ref string, todos []todo.Todo, matches []int) error {
	lines := make([]string, len(matches))
	for i, m := range matches {
		lines[i] = fmt.Sprintf("    %s  %s", todos[m].ID[:8], todos[m].Title)
	}
	return fmt.Errorf("%s %q matches %d tasks:\n%s", kind, ref, len(matches), strings.Join(lines, "\n"))
}

// findByPrefix is preserved as a thin alias so existing call-sites (and
// tests) keep working while the name catches up everywhere.
func findByPrefix(todos []todo.Todo, ref string) (*todo.Todo, error) {
	return findTaskByRef(todos, ref)
}

// resolveRefs resolves N refs at once, failing fast on the first unresolvable
// or ambiguous one. Used by batch verbs (`done a b c`) so we can validate the
// whole set before mutating anything. Duplicate refs collapse to the first
// match — `done abc abc` is a single done, not an error.
func resolveRefs(todos []todo.Todo, refs []string) ([]*todo.Todo, error) {
	out := make([]*todo.Todo, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		t, err := findTaskByRef(todos, ref)
		if err != nil {
			return nil, err
		}
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out, nil
}

// listFilterOpts collects every filter `list` and `search` apply. Lifted into
// a struct so the filter logic is unit-testable separately from CLI parsing.
type listFilterOpts struct {
	includeDone bool
	focus       bool
	tag         string // matched case-insensitively after NormalizeTag
	project     string // matched case-insensitively for equality
	search      string // case-insensitive substring of title
}

func filterTopLevel(todos []todo.Todo, opts listFilterOpts) []todo.Todo {
	tagQ := todo.NormalizeTag(opts.tag)
	projQ := strings.ToLower(strings.TrimSpace(opts.project))
	searchQ := strings.ToLower(strings.TrimSpace(opts.search))

	rows := make([]todo.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ParentID != "" {
			continue
		}
		if !opts.includeDone && t.Status != todo.Pending {
			continue
		}
		if opts.focus && !(t.IsOverdue() || t.IsDueToday()) {
			continue
		}
		if tagQ != "" {
			found := false
			for _, tag := range t.Tags {
				if tag == tagQ {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if projQ != "" && strings.ToLower(t.Project) != projQ {
			continue
		}
		if searchQ != "" && !strings.Contains(strings.ToLower(t.Title), searchQ) {
			continue
		}
		rows = append(rows, t)
	}
	return rows
}

// trackedToday sums the portion of every TimeEntry across all todos that falls
// within today's local window. Running entries count up to `now`. Used by the
// stats one-liner.
func trackedTodayDuration(todos []todo.Todo, now time.Time) time.Duration {
	today := startOfDay(now)
	tomorrow := today.AddDate(0, 0, 1)
	var total time.Duration
	for _, t := range todos {
		for _, e := range t.TimeEntries {
			start := e.StartedAt
			end := e.StoppedAt
			if end.IsZero() {
				end = now
			}
			if !end.After(today) || !start.Before(tomorrow) {
				continue
			}
			if start.Before(today) {
				start = today
			}
			if end.After(tomorrow) {
				end = tomorrow
			}
			total += end.Sub(start)
		}
	}
	return total
}
