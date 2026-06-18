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
					t.Size = parsed.size
					if !parsed.dueDate.IsZero() {
						t.DueDate = parsed.dueDate
					}
					if parsed.project != "" {
						t.Project = parsed.project
					}
					for _, tg := range parsed.tags {
						t.AddTag(tg)
					}
					m.add(t)
					m.markModified(t.ID)
				}
			} else if t := m.currentTodo(); t != nil {
				if m.detail.page == 2 {
					if val != "" {
						t.AddComment(val)
						m.detail.commentCursor = len(t.Comments) - 1
						m.markModified(t.ID)
					}
				} else {
					switch m.detail.field {
					case fieldStartDate:
						if val == "" {
							t.StartDate = time.Time{}
						} else if d, err := parseDueDate(val); err == nil {
							t.SetStartDate(d)
						} else {
							m.err = tr("Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'")
							return m, clearErrAfter()
						}
					case fieldDueDate:
						if val == "" {
							t.DueDate = time.Time{}
						} else if d, err := parseDueDate(val); err == nil {
							t.SetDueDate(d)
						} else {
							m.err = tr("Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'")
							return m, clearErrAfter()
						}
					}
					m.markModified(t.ID)
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
				if t := m.currentTodo(); t != nil {
					newID := m.addSubtask(t.ID, val)
					m.detail.subtaskCursor = m.subtaskCount(t.ID) - 1
					m.markModified(newID)
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
				if t := m.currentTodo(); t != nil {
					t.AddLearning(val)
					m.detail.learningCursor = len(t.Learnings) - 1
					m.markModified(t.ID)
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
						parentID := m.updateLearningByID(learnings[m.learningCursor].ID, newText)
						m.markModified(parentID)
					}
				} else if t := m.currentTodo(); t != nil && m.detail.learningCursor < len(t.Learnings) {
					t.UpdateLearning(m.detail.learningCursor, newText)
					m.markModified(t.ID)
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
				if t := m.currentTodo(); t != nil {
					t.Title = todo.CapitalizeTitle(newTitle)
					t.ModifiedAt = time.Now()
					m.markModified(t.ID)
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
			if t := m.currentTodo(); t != nil {
				if val := m.textInput.Value(); val != "" {
					t.UpdateComment(m.pendingComment, val)
					m.markModified(t.ID)
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
				touched := m.renameTagGlobally(m.editingTagName, newName)
				m.markModified(touched...)
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
				touched := m.renameProjectGlobally(m.editingProjectName, newName)
				m.markModified(touched...)
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
				if t := m.currentTodo(); t != nil {
					t.AddDependency(results[m.depSearch.cursor].ID)
					m.markModified(t.ID)
				}
			}
			m.mode = modeNormal
			m.depSearch = searchState{}
			return m, nil
		case "up":
			if m.depSearch.cursor > 0 {
				m.depSearch.cursor--
			}
			return m, nil
		case "down":
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
			if t := m.currentTodo(); t != nil {
				results := m.tagSearchResults()
				var tagToAdd string
				if m.tagSearch.cursor < len(results) {
					tagToAdd = results[m.tagSearch.cursor]
				} else if m.tagSearch.query != "" {
					tagToAdd = m.tagSearch.query
				}
				if tagToAdd != "" {
					t.AddTag(tagToAdd)
					m.markModified(t.ID)
				}
			}
			m.mode = modeNormal
			m.tagSearch = searchState{}
			return m, nil
		case "up":
			if m.tagSearch.cursor > 0 {
				m.tagSearch.cursor--
			}
			return m, nil
		case "down":
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
			if t := m.currentTodo(); t != nil {
				results := m.projSearchResults()
				var projToSet string
				if m.projSearch.cursor < len(results) {
					projToSet = results[m.projSearch.cursor]
				} else if m.projSearch.query != "" {
					projToSet = m.projSearch.query
				}
				if projToSet != "" {
					t.SetProject(projToSet)
					m.markModified(t.ID)
				}
			}
			m.mode = modeNormal
			m.projSearch = searchState{}
			return m, nil
		case "up":
			if m.projSearch.cursor > 0 {
				m.projSearch.cursor--
			}
			return m, nil
		case "down":
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
				m.pushUndo("delete task", id)
				m.markTombstone(id)
				m.remove(id)
			}
			// Tombstone already records what to persist; no other rows changed,
			// so we only need to schedule a save and refresh derived caches.
			m.dirty = true
			m.cache.dirty = true
			m.invalidateDetailCache()
			m.refreshCaches()
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
			if t := m.currentTodo(); t != nil {
				m.pushUndo("delete comment", t.ID)
				t.DeleteComment(m.pendingComment)
				if m.detail.commentCursor >= len(t.Comments) && m.detail.commentCursor > 0 {
					m.detail.commentCursor--
				}
				m.markModifiedNoUndo(t.ID)
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
			if t := m.currentTodo(); t != nil && m.pendingDep < len(t.Dependencies) {
				m.pushUndo("remove dependency", t.ID)
				t.RemoveDependency(t.Dependencies[m.pendingDep])
				if m.detail.depCursor >= len(t.Dependencies) && m.detail.depCursor > 0 {
					m.detail.depCursor--
				}
				m.markModifiedNoUndo(t.ID)
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
					m.pushUndo("edit time entry", t.ID)
					e.StartedAt = start
					e.StoppedAt = stop
					t.ModifiedAt = time.Now()
					m.markModifiedNoUndo(t.ID)
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
				m.pushUndo("stop timer", t.ID)
				t.StopTimer()
				m.markModifiedNoUndo(t.ID)
			}
			m.mode = modeNormal
		case "e":
			return m, m.startEditTimeEntry(m.pendingEntryTaskID, m.pendingEntryID)
		case "d":
			if t := m.findTodoByID(m.pendingEntryTaskID); t != nil {
				for i := range t.TimeEntries {
					if t.TimeEntries[i].ID == m.pendingEntryID {
						m.pushUndo("discard time entry", t.ID)
						t.DeleteTimeEntry(i)
						m.markModifiedNoUndo(t.ID)
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
						m.pushUndo("delete time entry", t.ID)
						t.DeleteTimeEntry(i)
						m.markModifiedNoUndo(t.ID)
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
			if t := m.currentTodo(); t != nil && m.pendingTag < len(t.Tags) {
				m.pushUndo("remove tag", t.ID)
				t.RemoveTag(t.Tags[m.pendingTag])
				if m.detail.tagCursor >= len(t.Tags) && m.detail.tagCursor > 0 {
					m.detail.tagCursor--
				}
				m.markModifiedNoUndo(t.ID)
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
				touched := m.deleteTagGlobally(tags[m.tagTabCursor])
				m.markModifiedNoUndo(touched...)
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
			if t := m.currentTodo(); t != nil {
				m.pushUndo("remove project", t.ID)
				t.SetProject("")
				m.markModifiedNoUndo(t.ID)
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
					// We don't know the parent ID until deleteLearningByID
					// returns it, so capture the snapshot afterwards via the
					// no-args fallback.
					m.pushUndo("delete learning")
					parentID := m.deleteLearningByID(learnings[m.learningCursor].ID)
					m.markModifiedNoUndo(parentID)
					if remaining := m.allLearnings(); m.learningCursor >= len(remaining) && m.learningCursor > 0 {
						m.learningCursor--
					}
				}
			} else if t := m.currentTodo(); t != nil && m.detail.learningCursor < len(t.Learnings) {
				m.pushUndo("delete learning", t.ID)
				t.DeleteLearning(m.detail.learningCursor)
				if m.detail.learningCursor >= len(t.Learnings) && m.detail.learningCursor > 0 {
					m.detail.learningCursor--
				}
				m.markModifiedNoUndo(t.ID)
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
			if t := m.currentTodo(); t != nil && m.pendingSubtask < m.subtaskCount(t.ID) {
				// Capture both the parent (its subtask list will change) and
				// the subtask itself (so undo can restore it).
				subID := ""
				if ids := m.subtaskIDs(t.ID); m.pendingSubtask < len(ids) {
					subID = ids[m.pendingSubtask]
				}
				m.pushUndo("delete subtask", t.ID, subID)
				subID = m.deleteSubtask(t.ID, m.pendingSubtask)
				m.markTombstone(subID)
				if m.detail.subtaskCursor >= m.subtaskCount(t.ID) && m.detail.subtaskCursor > 0 {
					m.detail.subtaskCursor--
				}
				// Tombstone already records what to persist; nothing else changed.
				m.dirty = true
				m.cache.dirty = true
				m.invalidateDetailCache()
				m.refreshCaches()
			}
			m.mode = modeNormal
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}
