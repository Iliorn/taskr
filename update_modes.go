package main

import (
	"fmt"
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
					if parsed.recurrence != "" {
						t.Recurrence = parsed.recurrence
					}
					// Resolve dep: refs before the add so `^` still points at
					// the previous task, not the one being created. A bad ref
					// doesn't block the add — the task lands, the toast says
					// which link didn't.
					var depErr error
					for _, ref := range parsed.deps {
						dep, err := resolveDepRef(m.allTodos(), ref)
						if err != nil {
							depErr = err
							continue
						}
						t.AddDependency(dep.ID)
					}
					m.pushUndo("add task", t.ID)
					m.add(t)
					m.markModified(t.ID)
					saveLastAddedID(t.ID)
					if depErr != nil {
						m.flashError(fmt.Sprintf("%s: %v", tr("Dependency not linked"), depErr))
						return m, clearErrAfter()
					}
				}
			} else if t := m.currentTodo(); t != nil {
				if m.detail.page == 2 {
					if val != "" {
						m.pushUndo("add comment", t.ID)
						t.AddComment(val)
						m.detail.commentCursor = len(t.Comments) - 1
						m.markModified(t.ID)
					}
				} else {
					switch m.detail.field {
					case fieldStartDate:
						if val == "" {
							m.pushUndo("clear start date", t.ID)
							t.StartDate = time.Time{}
						} else if d, err := parseDueDate(val); err == nil {
							m.pushUndo("set start date", t.ID)
							t.SetStartDate(d)
						} else {
							m.flashError(tr("Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'"))
							return m, clearErrAfter()
						}
					case fieldDueDate:
						if val == "" {
							m.pushUndo("clear due date", t.ID)
							t.DueDate = time.Time{}
						} else if d, err := parseDueDate(val); err == nil {
							// Subtask: extendParentDueIfNeeded may bump
							// ancestors, so capture full state instead of just
							// t.ID — partial would miss the ancestor pre-state.
							if t.ParentID != "" {
								m.pushUndo("set due date")
							} else {
								m.pushUndo("set due date", t.ID)
							}
							t.SetDueDate(d)
						} else {
							m.flashError(tr("Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'"))
							return m, clearErrAfter()
						}
					}
					ids := []string{t.ID}
					if m.detail.field == fieldDueDate && t.ParentID != "" {
						ids = append(ids, m.extendParentDueIfNeeded(t.ID)...)
					}
					m.markModified(ids...)
				}
			}
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		case "ctrl+e":
			// Escape hatch only for the comment-add input (the comments page);
			// other modeInput uses (quick-add, date fields) keep ctrl+e as the
			// text input's move-to-end.
			if m.pane != paneList && m.detail.page == 2 {
				return m, m.openEditorForInput()
			}
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
					// Build the subtask, push undo with its (not-yet-stored) ID
					// so undo will delete it (the ID is in entry.ids but has no
					// captured partial), then add to the store.
					sub := todo.NewSubtask(val, t.ID)
					sub.InheritContextFrom(m.get(t.ID))
					m.pushUndo("add subtask", sub.ID)
					m.add(sub)
					m.detail.subtaskCursor = m.subtaskCount(t.ID) - 1
					m.markModified(sub.ID)
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

// updateAddTimeEntry parses a duration ("45m", "1h30m") or a clock range
// ("10:00-10:30") and appends a TimeEntry to pendingEntryTaskID. Duration
// form anchors the entry at "now"; range form anchors it on today. Letting
// a user log time after the fact is useful for tracking work that wasn't
// captured by the live timer — and for back-dating, the entry can later be
// retimed via the existing modeEditTimeEntry flow.
func (m model) updateAddTimeEntry(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			t := m.findTodoByID(m.pendingEntryTaskID)
			if t == nil {
				m.mode = modeNormal
				return m, nil
			}
			start, stop, err := parseManualEntry(m.textInput.Value(), time.Now())
			if err != nil {
				m.flashError(err.Error())
				return m, clearErrAfter()
			}
			m.pushUndo("add time entry", t.ID)
			t.AddTimeEntry(start, stop)
			m.markModified(t.ID)
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

// updateEditSubtask renames the subtask whose index was captured in
// pendingSubtask when edit started. The index is captured up front so
// concurrent reorders/deletions can't accidentally rename a different child.
func (m model) updateEditSubtask(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			if val := strings.TrimSpace(m.textInput.Value()); val != "" {
				if t := m.currentTodo(); t != nil {
					ids := m.subtaskIDs(t.ID)
					if m.pendingSubtask < len(ids) {
						if sub := m.findTodoByID(ids[m.pendingSubtask]); sub != nil {
							m.pushUndo("rename subtask", sub.ID)
							sub.Title = todo.CapitalizeTitle(val)
							sub.ModifiedAt = time.Now()
							m.markModified(sub.ID)
						}
					}
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
					m.pushUndo("add learning", t.ID)
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
		case "ctrl+e":
			return m, m.openEditorForInput()
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
						// updateLearningByID walks every task and returns the
						// parent ID — we don't know which task to snapshot
						// ahead of time, so capture full state.
						m.pushUndo("edit learning")
						parentID := m.updateLearningByID(learnings[m.learningCursor].ID, newText)
						m.markModified(parentID)
					}
				} else if t := m.currentTodo(); t != nil && m.detail.learningCursor < len(t.Learnings) {
					m.pushUndo("edit learning", t.ID)
					t.UpdateLearning(m.detail.learningCursor, newText)
					m.markModified(t.ID)
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		case "ctrl+e":
			return m, m.openEditorForInput()
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
					m.pushUndo("rename task", t.ID)
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
					m.pushUndo("edit comment", t.ID)
					t.UpdateComment(m.pendingComment, val)
					m.markModified(t.ID)
				}
			}
			m.mode = modeNormal
			return m, nil
		case "esc":
			m.mode = modeNormal
			return m, nil
		case "ctrl+e":
			return m, m.openEditorForInput()
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
				// renameTagGlobally walks every task; the affected IDs aren't
				// known until after the walk, so capture full pre-state.
				m.pushUndo("rename tag")
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
				m.pushUndo("rename project")
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
				if m.learningSearchQuery != "" {
					m.pushFocus(stateLearningSearch)
				} else {
					m.dropFocus(stateLearningSearch)
				}
			} else {
				m.searchQuery = m.searchInput.Value()
				if m.searchQuery != "" {
					m.pushFocus(stateSearch)
				} else {
					m.dropFocus(stateSearch)
				}
				m.cursor = 0
				m.projectCursor = 0
				m.listOffset = 0
				m.markFilterDirty()
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
				m.markFilterDirty()
			}
			return m, nil
		}
	}
	if m.tab == tabLearnings {
		m.learningSearchInput, cmd = m.learningSearchInput.Update(msg)
		newQuery := m.learningSearchInput.Value()
		if newQuery != m.learningSearchQuery {
			m.learningSearchQuery = newQuery
			m.learningCursor = 0
		}
	} else {
		m.searchInput, cmd = m.searchInput.Update(msg)
		newQuery := m.searchInput.Value()
		if newQuery != m.searchQuery {
			// Only invalidate caches and reset cursor when the query
			// actually changed. Otherwise cursor-blink ticks would
			// rebuild caches on every frame, reshuffling tied done
			// tasks (see sortTodosBySequenceWithRollup tiebreakers).
			m.searchQuery = newQuery
			m.cursor = 0
			m.projectCursor = 0
			m.listOffset = 0
			m.markFilterDirty()
		}
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
			if m.tagTabSearchQuery != "" {
				m.pushFocus(stateTagSearch)
			} else {
				m.dropFocus(stateTagSearch)
			}
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
					m.pushUndo("add dependency", t.ID)
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
					m.pushUndo("add tag", t.ID)
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
					m.pushUndo("set project", t.ID)
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

// updateConfirm is the single handler for every modeConfirm prompt: y/enter runs
// the staged confirmOnYes action, n/esc cancels, and either way the prompt
// closes. The per-action bodies live in the confirm* methods below, staged at
// each trigger site — so prompt behavior (Enter-as-yes, cancel) is uniform by
// construction.
func (m model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y", "enter":
			var cmd tea.Cmd
			if m.confirmOnYes != nil {
				cmd = m.confirmOnYes(&m)
			}
			m.mode = modeNormal
			m.confirmOnYes = nil
			return m, cmd
		case "n", "esc":
			m.mode = modeNormal
			m.confirmOnYes = nil
		}
	}
	return m, nil
}

func (m *model) confirmDeleteTask() tea.Cmd {
	if id := m.pendingDeleteID; id != "" && m.get(id) != nil {
		ids := m.descendantIDs(id)
		m.pushUndo("delete task", ids...)
		for _, deleteID := range ids {
			m.markTombstone(deleteID)
			m.remove(deleteID)
		}
	}
	m.pendingDeleteID = ""
	// Tombstone already records what to persist; no other rows changed, so we
	// only need to schedule a save and refresh derived caches.
	m.dirty = true
	m.cache.dirty = true
	m.invalidateDetailCache()
	m.refreshCaches()
	var newLen int
	if m.showHistory {
		newLen = len(m.cache.done)
	} else {
		newLen = m.visibleActiveLen()
	}
	if m.cursor >= newLen && m.cursor > 0 {
		m.cursor--
	}
	return nil
}

// confirmReopen backs the "Move to active?" prompt staged by the Tasks-tab 'd'
// handler when the cursor is on a done task: it reopens the task (voiding the
// completion-rank reading via Toggle).
func (m *model) confirmReopen() tea.Cmd {
	if id := m.pendingReopenID; id != "" {
		if t := m.get(id); t != nil && t.Status == todo.Done {
			m.pushUndo("reopen task", t.ID)
			t.Toggle()
			m.markModified(t.ID)
			// The row leaves the done/history list, so the cursor would land on
			// the next row — decrement so it lands on the previous one instead.
			// Subtasks stay visible.
			if t.ParentID == "" && m.cursor > 0 {
				m.cursor--
			}
		}
	}
	m.pendingReopenID = ""
	return nil
}

// confirmCloseParent backs the "close parent with open subtasks?" prompt staged
// by the Tasks-tab 'd' handler: it closes the parent (and spawns next recurrence
// if the parent was recurring) but does NOT touch the open subtasks — the user
// opted to close just the parent, not cascade.
func (m *model) confirmCloseParent() tea.Cmd {
	if id := m.pendingCloseParentID; id != "" {
		if t := m.get(id); t != nil && t.Status == todo.Pending {
			// Full snapshot: spawnNextRecurrence creates a new task, and undo
			// must remove it plus restore t. Capturing all state is simpler than
			// tracking the new ID separately.
			m.pushUndo("close task")
			if t.IsTimerRunning() {
				m.stopTimer(t.ID)
			}
			captureSeqRankAtDone(m.allTodos(), t)
			t.Toggle()
			ids := []string{t.ID}
			if t.IsRecurring() {
				if newID := m.spawnNextRecurrence(t); newID != "" {
					ids = append(ids, newID)
				}
			}
			m.markModified(ids...)
			if m.cursor > 0 {
				m.cursor--
			}
		}
	}
	m.pendingCloseParentID = ""
	return nil
}

func (m *model) confirmDeleteComment() tea.Cmd {
	if t := m.currentTodo(); t != nil {
		m.pushUndo("delete comment", t.ID)
		t.DeleteComment(m.pendingComment)
		if m.detail.commentCursor >= len(t.Comments) && m.detail.commentCursor > 0 {
			m.detail.commentCursor--
		}
		m.markModified(t.ID)
	}
	return nil
}

func (m *model) confirmDeleteDep() tea.Cmd {
	if t := m.currentTodo(); t != nil && m.pendingDep < len(t.Dependencies) {
		m.pushUndo("remove dependency", t.ID)
		t.RemoveDependency(t.Dependencies[m.pendingDep])
		if m.detail.depCursor >= len(t.Dependencies) && m.detail.depCursor > 0 {
			m.detail.depCursor--
		}
		m.markModified(t.ID)
	}
	return nil
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
						m.flashError(err.Error())
						return m, clearErrAfter()
					}
					m.pushUndo("edit time entry", t.ID)
					e.StartedAt = start
					e.StoppedAt = stop
					e.ModifiedAt = time.Now()
					t.ModifiedAt = time.Now()
					m.markModified(t.ID)
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
				m.markModified(t.ID)
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
						m.markModified(t.ID)
						break
					}
				}
			}
			m.mode = modeNormal
		}
	}
	return m, nil
}

func (m *model) confirmDeleteTimeEntry() tea.Cmd {
	if t := m.findTodoByID(m.pendingEntryTaskID); t != nil {
		for i := range t.TimeEntries {
			if t.TimeEntries[i].ID == m.pendingEntryID {
				m.pushUndo("delete time entry", t.ID)
				t.DeleteTimeEntry(i)
				m.markModified(t.ID)
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
	return nil
}

func (m *model) confirmDeleteTag() tea.Cmd {
	if t := m.currentTodo(); t != nil && m.pendingTag < len(t.Tags) {
		m.pushUndo("remove tag", t.ID)
		t.RemoveTag(t.Tags[m.pendingTag])
		if m.detail.tagCursor >= len(t.Tags) && m.detail.tagCursor > 0 {
			m.detail.tagCursor--
		}
		m.markModified(t.ID)
	}
	return nil
}

func (m *model) confirmDeleteTagGlobal() tea.Cmd {
	if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) {
		m.pushUndo("delete tag globally")
		touched := m.deleteTagGlobally(tags[m.tagTabCursor])
		m.markModified(touched...)
		if remaining := m.getFilteredTagsForTab(); m.tagTabCursor >= len(remaining) && m.tagTabCursor > 0 {
			m.tagTabCursor--
		}
	}
	return nil
}

func (m *model) confirmDeleteProject() tea.Cmd {
	if t := m.currentTodo(); t != nil {
		m.pushUndo("remove project", t.ID)
		t.SetProject("")
		m.markModified(t.ID)
	}
	return nil
}

func (m *model) confirmDeleteLearning() tea.Cmd {
	if m.tab == tabLearnings {
		if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
			// We don't know the parent ID until deleteLearningByID returns it,
			// so capture the snapshot afterwards via the no-args fallback.
			m.pushUndo("delete learning")
			parentID := m.deleteLearningByID(learnings[m.learningCursor].ID)
			m.markModified(parentID)
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
		m.markModified(t.ID)
	}
	return nil
}

func (m *model) confirmDeleteSubtask() tea.Cmd {
	if t := m.currentTodo(); t != nil && m.pendingSubtask < m.subtaskCount(t.ID) {
		// Capture the parent (its subtask list will change) plus the subtask +
		// every transitive descendant (so undo can restore the whole tree).
		subID := ""
		if ids := m.subtaskIDs(t.ID); m.pendingSubtask < len(ids) {
			subID = ids[m.pendingSubtask]
		}
		toDelete := m.descendantIDs(subID)
		undoIDs := append([]string{t.ID}, toDelete...)
		m.pushUndo("delete subtask", undoIDs...)
		for _, id := range toDelete {
			m.markTombstone(id)
			m.remove(id)
		}
		if m.detail.subtaskCursor >= m.subtaskCount(t.ID) && m.detail.subtaskCursor > 0 {
			m.detail.subtaskCursor--
		}
		// Tombstone already records what to persist; nothing else changed.
		m.dirty = true
		m.cache.dirty = true
		m.invalidateDetailCache()
		m.refreshCaches()
	}
	return nil
}
