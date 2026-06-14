package main

import (
	"sort"
	"strings"
	"time"

	"taskr/todo"
)

// ── Caches ────────────────────────────────────────────────────────────────────

type cacheState struct {
	dirty        bool
	todoIndex    map[string]int
	overdueSet   map[string]bool
	active       []todo.Todo
	done         []todo.Todo
	tags         map[string]tagStats
	learnings    []todo.Learning
	projects     []string
	projectTasks map[string][]todo.Todo
	subtaskIndex map[string][]int
	tagRender    map[string]string

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

	m.cache.active = m.cache.active[:0]
	m.cache.done = m.cache.done[:0]
	for _, t := range m.todos {
		if t.ParentID != "" {
			continue
		}
		if t.Status == todo.Pending && m.matchesSearch(t) && m.matchesFocusFilter(t) {
			m.cache.active = append(m.cache.active, t)
		} else if t.Status == todo.Done && m.matchesSearch(t) {
			m.cache.done = append(m.cache.done, t)
		}
	}
	sortTodosByMode(m.cache.active, m.taskSort)
	sortTodosByMode(m.cache.done, m.taskSort)

	m.cache.tags = computeTagStats(m.todos)

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

	var result []todo.Learning
	for i := range m.todos {
		if len(m.todos[i].Learnings) > 0 {
			result = append(result, m.todos[i].Learnings...)
		}
	}

	if m.learningSearchQuery != "" {
		filtered := result[:0]
		q := strings.ToLower(m.learningSearchQuery)
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

	switch m.learningSort {
	case learningSortAlpha:
		sort.SliceStable(result, func(i, j int) bool {
			return strings.ToLower(result[i].Text) < strings.ToLower(result[j].Text)
		})
	default:
		sort.SliceStable(result, func(i, j int) bool {
			return result[i].CreatedAt.After(result[j].CreatedAt)
		})
	}

	m.cache.learnings = result
	m.cache.learningSearch = m.learningSearchQuery
	m.cache.learningSort = m.learningSort
}

func (m *model) refreshProjects() {
	if m.cache.projectSearch == m.searchQuery {
		return
	}

	projects := make([]string, 0, len(m.cache.projectTasks))
	for p := range m.cache.projectTasks {
		projects = append(projects, p)
	}
	sort.Strings(projects)

	if m.searchQuery != "" {
		q := strings.ToLower(m.searchQuery)
		filtered := projects[:0]
		for _, p := range projects {
			if strings.Contains(strings.ToLower(p), q) {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}

	m.cache.projects = projects
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

func (m *model) allLearnings() []todo.Learning {
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
