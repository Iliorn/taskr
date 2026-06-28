package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"taskr/todo"
)

// ── Caches ────────────────────────────────────────────────────────────────────

// cacheState holds derived views recomputed by refreshCaches whenever the task
// set changes. Structural indexes (subtaskOf, runningTimers) live on the Store
// and are maintained incrementally — they're not rebuilt here.
type cacheState struct {
	dirty         bool
	filterDirty   bool
	overdueSet    map[string]bool
	active        []todo.Todo
	done          []todo.Todo
	tags          map[string]tagStats
	tagsSorted    []string
	tagsSortMode  tagSortMode
	untaggedTotal int
	untaggedDone  int
	learnings     []learningView
	projects      []string
	projectTasks  map[string][]todo.Todo
	tagRender     map[string]string
	taskTagRender map[string]string

	learningSearch string
	learningSort   learningSortMode
	projectSearch  string

	// Tasks-tab column-sizing metrics for the active list: the widest rendered
	// row content and the widest tag cell. Derived from the active set + overdue
	// set, so cached here rather than rescanned every frame (the scan called
	// subtaskProgress/hasOverdueDescendant per task and dominated the render).
	activeColContentMax int
	activeColTagsMax    int
}

// ── Cache management ──────────────────────────────────────────────────────────

func (m *model) refreshCaches() {
	m.frameTime = time.Now()

	all := m.allTodos()

	for k := range m.cache.overdueSet {
		delete(m.cache.overdueSet, k)
	}
	for i := range all {
		if all[i].IsOverdue() {
			m.cache.overdueSet[all[i].ID] = true
		}
	}

	m.cache.active, m.cache.done = selectActiveDone(all, m.searchQuery, m.focusFilter, m.taskSort)

	m.cache.tags = computeTagStats(all)
	m.rebuildSortedTagsFrom(all)

	// subtaskOf is maintained incrementally by Store.add / Store.remove, so
	// no rebuild is needed here.

	for k := range m.cache.projectTasks {
		delete(m.cache.projectTasks, k)
	}
	for i := range all {
		if p := all[i].Project; p != "" {
			m.cache.projectTasks[p] = append(m.cache.projectTasks[p], all[i])
		}
	}
	for p, tasks := range m.cache.projectTasks {
		m.cache.projectTasks[p] = sortTodosByStartDate(tasks)
	}

	m.cache.projects = nil
	m.cache.projectSearch = "\x00"

	m.cache.learningSearch = "\x00"

	m.refreshTagRenderCache()
	m.refreshTaskColMetrics()

	m.cache.dirty = false
	m.cache.filterDirty = false
}

// refreshTaskColMetrics recomputes the Tasks-tab column-sizing metrics for the
// active list: the widest rendered row content (title plus every indicator the
// row appends) and the widest tag cell. These depend only on the active set and
// the overdue set, so they're computed once per cache refresh instead of being
// rescanned on every frame — the per-frame scan was O(active) and dominated the
// render because it called subtaskProgress/hasOverdueDescendant for every task.
// It must mirror exactly the width every suffix/prefix renderTaskLineWithSet
// adds, or the longest row eats into the gap before the Score column.
func (m *model) refreshTaskColMetrics() {
	contentMax, tagsMax := 0, 0
	overdueSet := m.cache.overdueSet
	active := m.cache.active
	for i := range active {
		w := len([]rune(active[i].Title))
		if active[i].HasOverdueDependencyFast(overdueSet) {
			w += 2 // " !"
		}
		if active[i].Notes != "" {
			w += 2 // " ¶"
		}
		if active[i].IsRecurring() {
			w += 2 // " ↻"
		}
		if subDone, subTotal := m.subtaskProgress(active[i].ID); subTotal > 0 {
			w += len([]rune(fmt.Sprintf(" (%d/%d)", subDone, subTotal)))
			if m.hasOverdueDescendant(active[i].ID, overdueSet) {
				w++ // "‼"
			}
		}
		if active[i].IsTimerRunning() {
			w += 2 // "⏱ " prefix
		}
		if w > contentMax {
			contentMax = w
		}
		if tw := tagsRenderWidth(active[i].Tags); tw > tagsMax {
			tagsMax = tw
		}
	}
	m.cache.activeColContentMax = contentMax
	m.cache.activeColTagsMax = tagsMax
}

// refreshFilteredCaches rebuilds only the views that depend on the search/focus
// filter: the active/done split and the tag-render cache derived from it. The
// data-derived caches (overdue set, tag stats, sorted tags, per-project task
// lists) are left intact because none of them depend on the filter. This is the
// per-keystroke search path — a full refreshCaches would rescan and re-sort the
// entire task set on every keypress for no reason.
func (m *model) refreshFilteredCaches() {
	all := m.allTodos()
	m.cache.active, m.cache.done = selectActiveDone(all, m.searchQuery, m.focusFilter, m.taskSort)
	m.refreshTagRenderCache()
	m.refreshTaskColMetrics()
	m.cache.filterDirty = false
}

// rebuildSortedTagsFrom refreshes the cached unique, sorted tag list. The list
// is the expensive part of the Tags tab (a full scan + sort) and was previously
// recomputed on every render; cache it alongside tagStats and invalidate it
// the same way (on data change, and on sort-mode toggle via sortCachedTags).
func (m *model) rebuildSortedTagsFrom(todos []todo.Todo) {
	m.cache.tagsSorted, m.cache.untaggedTotal, m.cache.untaggedDone =
		selectSortedTags(todos, m.tagSort, m.cache.tags)
	m.cache.tagsSortMode = m.tagSort
}

// sortCachedTags re-sorts the cached tag list in place for the current sort
// mode without rescanning todos — used when only the sort mode changes.
func (m *model) sortCachedTags() {
	sortTags(m.cache.tagsSorted, m.tagSort, m.cache.tags)
	m.cache.tagsSortMode = m.tagSort
}

func sortTags(tags []string, mode tagSortMode, stats map[string]tagStats) {
	switch mode {
	case tagSortCount:
		sort.Slice(tags, func(i, j int) bool {
			ci := stats[tags[i]].total
			cj := stats[tags[j]].total
			if ci != cj {
				return ci > cj
			}
			return tags[i] < tags[j]
		})
	default:
		sort.Strings(tags)
	}
}

func (m *model) refreshTagRenderCache() {
	for k := range m.cache.tagRender {
		delete(m.cache.tagRender, k)
	}
	for k := range m.cache.taskTagRender {
		delete(m.cache.taskTagRender, k)
	}
	// tagRender dedups the expensive renderTagsPart by tag-set (joined key) so
	// it runs once per distinct tag set; taskTagRender then maps each task's ID
	// to that rendered string, so the row renderer can look tags up by ID with
	// no per-frame strings.Join. Iterate the two lists in place —
	// append(active, done...) would scribble into active's spare capacity.
	for _, list := range [2][]todo.Todo{m.cache.active, m.cache.done} {
		for _, t := range list {
			if len(t.Tags) == 0 {
				continue
			}
			key := strings.Join(t.Tags, ",")
			rendered, ok := m.cache.tagRender[key]
			if !ok {
				rendered = renderTagsPart(t.Tags)
				m.cache.tagRender[key] = rendered
			}
			m.cache.taskTagRender[t.ID] = rendered
		}
	}
}

func (m *model) refreshLearnings() {
	if m.cache.learningSearch == m.learningSearchQuery &&
		m.cache.learningSort == m.learningSort {
		return
	}
	m.cache.learnings = selectLearnings(m.allTodos(), m.learningSearchQuery, m.learningSort)
	m.cache.learningSearch = m.learningSearchQuery
	m.cache.learningSort = m.learningSort
}

func (m *model) refreshProjects() {
	if m.cache.projectSearch == m.searchQuery {
		return
	}
	m.cache.projects = selectProjects(m.allTodos(), m.searchQuery)
	m.cache.projectSearch = m.searchQuery
}

// ── Cache accessors ───────────────────────────────────────────────────────────

func (m *model) ensureCache() {
	switch {
	case m.cache.dirty:
		m.refreshCaches()
	case m.cache.filterDirty:
		m.refreshFilteredCaches()
	}
}

func (m model) activeTodos() []todo.Todo {
	return m.cache.active
}

func (m model) completedTodos() []todo.Todo {
	return m.cache.done
}

func (m *model) allLearnings() []learningView {
	m.refreshLearnings()
	return m.cache.learnings
}

func (m *model) allProjectsForList() []string {
	m.refreshProjects()
	return m.cache.projects
}

func (m model) getProjectTasks(project string) []todo.Todo {
	if tasks, ok := m.cache.projectTasks[project]; ok {
		return tasks
	}
	return nil
}

// getRenderedTagsForTask returns a task's rendered tags via the by-ID cache
// (populated by refreshTagRenderCache), avoiding the per-frame strings.Join that
// getRenderedTags pays to build its key. Falls back to rendering directly for a
// task not in the active/done lists.
func (m *model) getRenderedTagsForTask(t *todo.Todo) string {
	if len(t.Tags) == 0 {
		return ""
	}
	if r, ok := m.cache.taskTagRender[t.ID]; ok {
		return r
	}
	return renderTagsPart(t.Tags)
}

func (m model) getRenderedTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	key := strings.Join(tags, ",")
	if cached, ok := m.cache.tagRender[key]; ok {
		return cached
	}
	return renderTagsPart(tags)
}
