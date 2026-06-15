package main

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"taskr/todo"
)

// ── Input handlers ────────────────────────────────────────────────────────────

func (m model) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			val := m.textInput.Value()
			m.mode = modeNormal
			if m.pane == paneList {
				if val != "" {
					parsed := parseQuickAdd(val)
					t := todo.New(parsed.title)
					t.Priority = parsed.priority
					if !parsed.dueDate.IsZero() {
						t.DueDate = parsed.dueDate
					}
					if parsed.project != "" {
						t.Project = parsed.project
					}
					for _, tg := range parsed.tags {
						t.AddTag(tg)
					}
					m.todos = append(m.todos, t)
					m.markModified()
				}
			} else if idx := m.currentTodoIndex(); idx >= 0 {
				if m.detail.page == 2 {
					if val != "" {
						m.todos[idx].AddComment(val)
						m.detail.commentCursor = len(m.todos[idx].Comments) - 1
						m.markModified()
					}
				} else {
					switch m.detail.field {
					case fieldStartDate:
						if val == "" {
							m.todos[idx].StartDate = time.Time{}
						} else if d, err := parseDueDate(val); err == nil {
							m.todos[idx].SetStartDate(d)
						} else {
							m.err = "Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'"
							return m, clearErrAfter()
						}
					case fieldDueDate:
						if val == "" {
							m.todos[idx].DueDate = time.Time{}
						} else if d, err := parseDueDate(val); err == nil {
							m.todos[idx].SetDueDate(d)
						} else {
							m.err = "Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'"
							return m, clearErrAfter()
						}
					}
					m.markModified()
				}
			}
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateAddSubtask(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if val := strings.TrimSpace(m.textInput.Value()); val != "" {
				if idx := m.currentTodoIndex(); idx >= 0 {
					m.addSubtask(idx, val)
					m.detail.subtaskCursor = len(m.todos[idx].SubtaskIDs) - 1
					m.markModified()
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateAddLearning(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if val := strings.TrimSpace(m.textInput.Value()); val != "" {
				if idx := m.currentTodoIndex(); idx >= 0 {
					m.todos[idx].AddLearning(val)
					m.detail.learningCursor = len(m.todos[idx].Learnings) - 1
					m.markModified()
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateEditLearning(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if newText := strings.TrimSpace(m.textInput.Value()); newText != "" {
				if m.tab == tabLearnings {
					if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
						m.updateLearningByID(learnings[m.learningCursor].ID, newText)
						m.markModified()
					}
				} else if idx := m.currentTodoIndex(); idx >= 0 && m.detail.learningCursor < len(m.todos[idx].Learnings) {
					m.todos[idx].UpdateLearning(m.detail.learningCursor, newText)
					m.markModified()
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateEditTitle(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if newTitle := strings.TrimSpace(m.textInput.Value()); newTitle != "" {
				if idx := m.currentTodoIndex(); idx >= 0 {
					m.todos[idx].Title = newTitle
					m.todos[idx].ModifiedAt = time.Now()
					m.markModified()
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateEditComment(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if idx := m.currentTodoIndex(); idx >= 0 {
				if val := m.textInput.Value(); val != "" {
					m.todos[idx].UpdateComment(m.pendingComment, val)
					m.markModified()
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateEditTag(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if newName := strings.TrimSpace(m.textInput.Value()); newName != "" && newName != m.editingTagName {
				m.renameTagGlobally(m.editingTagName, newName)
				m.markModified()
			}
			m.mode = modeNormal
			m.editingTagName = ""
			return m, nil
		case "esc":
			m.mode = modeNormal
			m.editingTagName = ""
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateEditProjectInline(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if newName := strings.TrimSpace(m.textInput.Value()); newName != "" && newName != m.editingProjectName {
				m.renameProjectGlobally(m.editingProjectName, newName)
				m.markModified()
			}
			m.mode = modeNormal
			m.editingProjectName = ""
			return m, nil
		case "esc":
			m.mode = modeNormal
			m.editingProjectName = ""
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// ── Search handlers ───────────────────────────────────────────────────────────

func (m model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			m.mode = modeNormal
			if m.tab == tabLearnings {
				m.learningSearchQuery = m.learningSearchInput.Value()
				m.learningCursor = 0
			} else {
				m.searchQuery = m.searchInput.Value()
				m.cursor = 0
				m.projectCursor = 0
				m.listOffset = 0
				m.markCacheDirty()
			}
			return m, nil
		case "esc":
			// Cancel: discard the query and restore the unfiltered list.
			m.mode = modeNormal
			if m.tab == tabLearnings {
				m.learningSearchInput.SetValue("")
				m.learningSearchQuery = ""
				m.learningCursor = 0
			} else {
				m.searchInput.SetValue("")
				m.searchQuery = ""
				m.cursor = 0
				m.projectCursor = 0
				m.listOffset = 0
				m.markCacheDirty()
			}
			return m, nil
		}
	}
	if m.tab == tabLearnings {
		m.learningSearchInput, cmd = m.learningSearchInput.Update(msg)
		m.learningSearchQuery = m.learningSearchInput.Value()
		m.learningCursor = 0
	} else {
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.searchQuery = m.searchInput.Value()
		m.cursor = 0
		m.projectCursor = 0
		m.listOffset = 0
		m.markCacheDirty()
	}
	return m, cmd
}

func (m model) updateSearchTagTab(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			m.mode = modeNormal
			m.tagTabCursor = 0
			return m, nil
		case "esc":
			// Cancel: discard the filter and restore the full tag list.
			m.mode = modeNormal
			m.tagTabSearchInput.SetValue("")
			m.tagTabSearchQuery = ""
			m.tagTabCursor = 0
			return m, nil
		}
	}
	m.tagTabSearchInput, cmd = m.tagTabSearchInput.Update(msg)
	m.tagTabSearchQuery = m.tagTabSearchInput.Value()
	m.tagTabCursor = 0
	return m, cmd
}

func (m model) updateSearchDep(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			results := m.depSearchResults()
			if m.depSearch.cursor < len(results) {
				if idx := m.currentTodoIndex(); idx >= 0 {
					m.todos[idx].AddDependency(results[m.depSearch.cursor].ID)
					m.markModified()
				}
			}
			m.mode = modeNormal
			m.depSearch = searchState{}
			return m, nil
		case "up", "k":
			if m.depSearch.cursor > 0 {
				m.depSearch.cursor--
			}
			return m, nil
		case "down", "j":
			if results := m.depSearchResults(); m.depSearch.cursor < len(results)-1 {
				m.depSearch.cursor++
			}
			return m, nil
		case "esc":
			m.mode = modeNormal
			m.depSearch = searchState{}
			return m, nil
		}
	}
	oldQuery := m.depSearch.query
	m.depSearchInput, cmd = m.depSearchInput.Update(msg)
	m.depSearch.query = m.depSearchInput.Value()
	if m.depSearch.query != oldQuery {
		m.depSearch.cursor = 0
	}
	return m, cmd
}

func (m model) updateSearchTag(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if idx := m.currentTodoIndex(); idx >= 0 {
				results := m.tagSearchResults()
				var tagToAdd string
				if m.tagSearch.cursor < len(results) {
					tagToAdd = results[m.tagSearch.cursor]
				} else if m.tagSearch.query != "" {
					tagToAdd = m.tagSearch.query
				}
				if tagToAdd != "" {
					m.todos[idx].AddTag(tagToAdd)
					m.markModified()
				}
			}
			m.mode = modeNormal
			m.tagSearch = searchState{}
			return m, nil
		case "up", "k":
			if m.tagSearch.cursor > 0 {
				m.tagSearch.cursor--
			}
			return m, nil
		case "down", "j":
			if results := m.tagSearchResults(); m.tagSearch.cursor < len(results)-1 {
				m.tagSearch.cursor++
			}
			return m, nil
		case "esc":
			m.mode = modeNormal
			m.tagSearch = searchState{}
			return m, nil
		}
	}
	oldQuery := m.tagSearch.query
	m.tagSearchInput, cmd = m.tagSearchInput.Update(msg)
	m.tagSearch.query = m.tagSearchInput.Value()
	if m.tagSearch.query != oldQuery {
		m.tagSearch.cursor = 0
	}
	return m, cmd
}

func (m model) updateSearchProject(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if idx := m.currentTodoIndex(); idx >= 0 {
				results := m.projSearchResults()
				var projToSet string
				if m.projSearch.cursor < len(results) {
					projToSet = results[m.projSearch.cursor]
				} else if m.projSearch.query != "" {
					projToSet = m.projSearch.query
				}
				if projToSet != "" {
					m.todos[idx].SetProject(projToSet)
					m.markModified()
				}
			}
			m.mode = modeNormal
			m.projSearch = searchState{}
			return m, nil
		case "up", "k":
			if m.projSearch.cursor > 0 {
				m.projSearch.cursor--
			}
			return m, nil
		case "down", "j":
			if results := m.projSearchResults(); m.projSearch.cursor < len(results)-1 {
				m.projSearch.cursor++
			}
			return m, nil
		case "esc":
			m.mode = modeNormal
			m.projSearch = searchState{}
			return m, nil
		}
	}
	oldQuery := m.projSearch.query
	m.projSearchInput, cmd = m.projSearchInput.Update(msg)
	m.projSearch.query = m.projSearchInput.Value()
	if m.projSearch.query != oldQuery {
		m.projSearch.cursor = 0
	}
	return m, cmd
}

// ── Confirm-delete handlers ───────────────────────────────────────────────────

func (m model) updateConfirmDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			var list []todo.Todo
			if m.showHistory {
				list = m.cache.done
			} else {
				list = m.cache.active
			}
			if m.pendingDelete < len(list) {
				id := list[m.pendingDelete].ID
				m.pushUndo("delete task")
				for i, t := range m.todos {
					if t.ID == id {
						m.todos = append(m.todos[:i], m.todos[i+1:]...)
						break
					}
				}
			}
			m.markModifiedNoUndo()
			var newLen int
			if m.showHistory {
				newLen = len(m.cache.done)
			} else {
				newLen = len(m.cache.active)
			}
			if m.cursor >= newLen && m.cursor > 0 {
				m.cursor--
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteComment(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if idx := m.currentTodoIndex(); idx >= 0 {
				m.pushUndo("delete comment")
				m.todos[idx].DeleteComment(m.pendingComment)
				if m.detail.commentCursor >= len(m.todos[idx].Comments) && m.detail.commentCursor > 0 {
					m.detail.commentCursor--
				}
				m.markModifiedNoUndo()
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteDep(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if idx := m.currentTodoIndex(); idx >= 0 && m.pendingDep < len(m.todos[idx].Dependencies) {
				m.pushUndo("remove dependency")
				m.todos[idx].RemoveDependency(m.todos[idx].Dependencies[m.pendingDep])
				if m.detail.depCursor >= len(m.todos[idx].Dependencies) && m.detail.depCursor > 0 {
					m.detail.depCursor--
				}
				m.markModifiedNoUndo()
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateEditTimeEntry(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			t := m.findTodoByID(m.pendingEntryTaskID)
			if t == nil {
				m.mode = modeNormal
				return m, nil
			}
			for i := range t.TimeEntries {
				if t.TimeEntries[i].ID == m.pendingEntryID {
					e := &t.TimeEntries[i]
					start, stop, err := parseEntryEdit(m.textInput.Value(), e.StartedAt, e.IsRunning())
					if err != nil {
						m.err = err.Error()
						return m, clearErrAfter()
					}
					m.pushUndo("edit time entry")
					e.StartedAt = start
					e.StoppedAt = stop
					t.ModifiedAt = time.Now()
					m.markModifiedNoUndo()
					break
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateIdlePrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "k", "esc", "enter":
			m.mode = modeNormal
		case "s":
			if t := m.findTodoByID(m.pendingEntryTaskID); t != nil && t.IsTimerRunning() {
				m.pushUndo("stop timer")
				t.StopTimer()
				m.markModifiedNoUndo()
			}
			m.mode = modeNormal
		case "e":
			return m, m.startEditTimeEntry(m.pendingEntryTaskID, m.pendingEntryID)
		case "d":
			if t := m.findTodoByID(m.pendingEntryTaskID); t != nil {
				for i := range t.TimeEntries {
					if t.TimeEntries[i].ID == m.pendingEntryID {
						m.pushUndo("discard time entry")
						t.DeleteTimeEntry(i)
						m.markModifiedNoUndo()
						break
					}
				}
			}
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteTimeEntry(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if t := m.findTodoByID(m.pendingEntryTaskID); t != nil {
				for i := range t.TimeEntries {
					if t.TimeEntries[i].ID == m.pendingEntryID {
						m.pushUndo("delete time entry")
						t.DeleteTimeEntry(i)
						m.markModifiedNoUndo()
						break
					}
				}
			}
			acts := m.activitiesForDay(m.calendar.selected)
			if len(acts) == 0 {
				m.calendar.focusTimeline = false
				m.calendar.entryCursor = 0
			} else if m.calendar.entryCursor >= len(acts) {
				m.calendar.entryCursor = len(acts) - 1
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteTag(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if idx := m.currentTodoIndex(); idx >= 0 && m.pendingTag < len(m.todos[idx].Tags) {
				m.pushUndo("remove tag")
				m.todos[idx].RemoveTag(m.todos[idx].Tags[m.pendingTag])
				if m.detail.tagCursor >= len(m.todos[idx].Tags) && m.detail.tagCursor > 0 {
					m.detail.tagCursor--
				}
				m.markModifiedNoUndo()
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteTagGlobal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) {
				m.pushUndo("delete tag globally")
				m.deleteTagGlobally(tags[m.tagTabCursor])
				m.markModifiedNoUndo()
				if remaining := m.getFilteredTagsForTab(); m.tagTabCursor >= len(remaining) && m.tagTabCursor > 0 {
					m.tagTabCursor--
				}
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteProject(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if idx := m.currentTodoIndex(); idx >= 0 {
				m.pushUndo("remove project")
				m.todos[idx].SetProject("")
				m.markModifiedNoUndo()
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteLearning(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if m.tab == tabLearnings {
				if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
					m.pushUndo("delete learning")
					m.deleteLearningByID(learnings[m.learningCursor].ID)
					m.markModifiedNoUndo()
					if remaining := m.allLearnings(); m.learningCursor >= len(remaining) && m.learningCursor > 0 {
						m.learningCursor--
					}
				}
			} else if idx := m.currentTodoIndex(); idx >= 0 && m.detail.learningCursor < len(m.todos[idx].Learnings) {
				m.pushUndo("delete learning")
				m.todos[idx].DeleteLearning(m.detail.learningCursor)
				if m.detail.learningCursor >= len(m.todos[idx].Learnings) && m.detail.learningCursor > 0 {
					m.detail.learningCursor--
				}
				m.markModifiedNoUndo()
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m model) updateConfirmDeleteSubtask(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y":
			if idx := m.currentTodoIndex(); idx >= 0 && m.pendingSubtask < len(m.todos[idx].SubtaskIDs) {
				m.pushUndo("delete subtask")
				m.deleteSubtask(idx, m.pendingSubtask)
				if m.detail.subtaskCursor >= len(m.todos[idx].SubtaskIDs) && m.detail.subtaskCursor > 0 {
					m.detail.subtaskCursor--
				}
				m.markModifiedNoUndo()
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}
