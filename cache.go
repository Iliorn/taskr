package main

import (
	"sort"
	"strings"
	"time"

	"taskr/todo"
)

// ── Caches ────────────────────────────────────────────────────────────────────

type cacheState struct {
	dirty         bool
	todoIndex     map[string]int
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
	subtaskIndex  map[string][]int
	tagRender     map[string]string

	learningSearch string
	learningSort   learningSortMode
	projectSearch  string
}

// ── Cache management ──────────────────────────────────────────────────────────

func (m *model) refreshCaches() {
	m.frameTime = time.Now()

	for k := range m.cache.todoIndex {
		delete(m.cache.todoIndex, k)
	}
	for i := range m.todos {
		m.cache.todoIndex[m.todos[i].ID] = i
	}

	for k := range m.cache.overdueSet {
		delete(m.cache.overdueSet, k)
	}
	for i := range m.todos {
		if m.todos[i].IsOverdue() {
			m.cache.overdueSet[m.todos[i].ID] = true
		}
	}

	m.cache.active, m.cache.done = selectActiveDone(m.todos, m.searchQuery, m.focusFilter, m.taskSort)

	m.cache.tags = computeTagStats(m.todos)
	m.rebuildSortedTags()

	for k := range m.cache.subtaskIndex {
		delete(m.cache.subtaskIndex, k)
	}
	for i := range m.todos {
		if pid := m.todos[i].ParentID; pid != "" {
			m.cache.subtaskIndex[pid] = append(m.cache.subtaskIndex[pid], i)
		}
	}

	for k := range m.cache.projectTasks {
		delete(m.cache.projectTasks, k)
	}
	for i := range m.todos {
		if p := m.todos[i].Project; p != "" {
			m.cache.projectTasks[p] = append(m.cache.projectTasks[p], m.todos[i])
		}
	}
	for p, tasks := range m.cache.projectTasks {
		m.cache.projectTasks[p] = sortTodosByStartDate(tasks)
	}

	m.cache.projects = nil
	m.cache.projectSearch = "\x00"

	m.cache.learningSearch = "\x00"

	m.refreshTagRenderCache()

	m.cache.dirty = false
}

// rebuildSortedTags refreshes the cached unique, sorted tag list. The list is
// the expensive part of the Tags tab (a full scan + sort) and was previously
// recomputed on every render; cache it alongside tagStats and invalidate it
// the same way (on data change, and on sort-mode toggle via sortCachedTags).
func (m *model) rebuildSortedTags() {
	m.cache.tagsSorted, m.cache.untaggedTotal, m.cache.untaggedDone =
		selectSortedTags(m.todos, m.tagSort, m.cache.tags)
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
	seen := make(map[string]struct{}, len(m.cache.active))
	allTasks := append(m.cache.active, m.cache.done...)
	for _, t := range allTasks {
		if len(t.Tags) == 0 {
			continue
		}
		key := strings.Join(t.Tags, ",")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		m.cache.tagRender[key] = renderTagsPart(t.Tags)
	}
}

func (m *model) refreshLearnings() {
	if m.cache.learningSearch == m.learningSearchQuery &&
		m.cache.learningSort == m.learningSort {
		return
	}
	m.cache.learnings = selectLearnings(m.todos, m.learningSearchQuery, m.learningSort)
	m.cache.learningSearch = m.learningSearchQuery
	m.cache.learningSort = m.learningSort
}

func (m *model) refreshProjects() {
	if m.cache.projectSearch == m.searchQuery {
		return
	}
	m.cache.projects = selectProjects(m.todos, m.searchQuery)
	m.cache.projectSearch = m.searchQuery
}

// ── Cache accessors ───────────────────────────────────────────────────────────

func (m *model) ensureCache() {
	if m.cache.dirty {
		m.refreshCaches()
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
