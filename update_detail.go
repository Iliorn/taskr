package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"taskr/todo"
)

// ── Detail pane ───────────────────────────────────────────────────────────────

func (m model) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.tab == tabLearnings {
		return m.updateLearningsDetail(msg)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "?":
		m.mode = modeHelp
		return m, nil
	case "u":
		return m, m.performUndo()
	case "n":
		if m.currentTodo() != nil {
			return m, m.openEditorForNotes()
		}
		return m, nil
	case "T":
		// Manual time entry (mirror of the list-view shortcut).
		if t := m.currentTodo(); t != nil {
			m.pendingEntryTaskID = t.ID
			m.mode = modeAddTimeEntry
			m.textInput.SetValue("")
			m.textInput.Placeholder = tr("Time spent (45m, 1h30m) or HH:MM-HH:MM…")
			m.textInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case "esc":
		m.pane = paneList
		m.detail = detailState{field: fieldStartDate}
		m.invalidateDetailCache()

	case "tab":
		m.switchTab((m.tab + 1) % numTabs)
		return m, nil

	case "left":
		if m.detail.page > 0 {
			m.detail.page--
			if m.detail.page == 0 {
				m.detail.field = fieldStartDate
			} else {
				m.detail.field = fieldSubtasks
			}
			m.invalidateDetailCache()
		}
	case "right", "l":
		if m.detail.page < 2 {
			m.detail.page++
			if m.detail.page == 1 {
				m.detail.field = fieldSubtasks
				m.detail.subtaskCursor = 0
			} else {
				m.detail.commentCursor = 0
			}
			m.invalidateDetailCache()
		}

	case "up", "k":
		m.detailCursorUp()
	case "down", "j":
		m.detailCursorDown()

	case "enter":
		return m.startEditing()

	case "d":
		if m.detail.page == 1 && m.detail.field == fieldSubtasks {
			if t := m.currentTodo(); t != nil {
				if m.detail.subtaskCursor < m.subtaskCount(t.ID) {
					// Full snapshot: toggleSubtask cascades up through
					// ancestors and may spawn new recurrence tasks —
					// neither is knowable until after the call.
					m.pushUndo("toggle subtask")
					ids := m.toggleSubtask(t.ID, m.detail.subtaskCursor)
					if len(ids) > 0 {
						m.markModified(ids...)
					}
				}
			}
		}

	case "t":
		// Toggle the timer on the selected subtask. Mirrors the
		// top-level `t` handler: done + no running timer = no-op,
		// idle threshold opens the runaway prompt, otherwise
		// toggleTimer enforces single-task tracking.
		if m.detail.page == 1 && m.detail.field == fieldSubtasks {
			if parent := m.currentTodo(); parent != nil {
				ids := m.subtaskIDs(parent.ID)
				if m.detail.subtaskCursor < len(ids) {
					sub := m.get(ids[m.detail.subtaskCursor])
					if sub == nil {
						return m, nil
					}
					if sub.Status == todo.Done && !sub.IsTimerRunning() {
						return m, nil
					}
					if e := sub.RunningEntry(); e != nil && time.Since(e.StartedAt) > idleThreshold {
						m.openIdlePrompt(sub)
						return m, nil
					}
					undoIDs := []string{sub.ID}
					if !sub.IsTimerRunning() {
						for otherID := range m.runningTimers {
							if otherID != sub.ID {
								undoIDs = append(undoIDs, otherID)
							}
						}
					}
					m.pushUndo("toggle timer", undoIDs...)
					m.toggleTimer(sub)
					m.markModified(sub.ID)
					if !m.timerTickOn && m.anyTimerRunning() {
						m.timerTickOn = true
						return m, timerTick()
					}
				}
			}
		}

	case "a":
		return m.detailAdd()

	case "x", "delete":
		return m.detailDelete()
	}
	return m, nil
}

func (m *model) detailCursorUp() {
	m.invalidateDetailCache()
	if m.detail.page == 0 {
		t := m.currentTodo()
		switch m.detail.field {
		case fieldStartDate:
			// Wrap to the bottom of the page: last tag, or fieldTags
			// itself if there are no tags.
			m.detail.field = fieldTags
			m.detail.tagCursor = 0
			if t != nil && len(t.Tags) > 0 {
				m.detail.tagCursor = len(t.Tags) - 1
			}
		case fieldDueDate:
			m.detail.field = fieldStartDate
		case fieldRecurrence:
			m.detail.field = fieldDueDate
		case fieldPriority:
			m.detail.field = fieldRecurrence
		case fieldSize:
			m.detail.field = fieldPriority
		case fieldProject:
			m.detail.field = fieldSize
		case fieldNotes:
			m.detail.field = fieldProject
		case fieldTags:
			if m.detail.tagCursor > 0 {
				m.detail.tagCursor--
			} else {
				m.detail.field = fieldNotes
			}
		}
	} else if m.detail.page == 1 {
		t := m.currentTodo()
		switch m.detail.field {
		case fieldSubtasks:
			if m.detail.subtaskCursor > 0 {
				m.detail.subtaskCursor--
			} else {
				// Wrap to the last learning on this page.
				m.detail.field = fieldLearnings
				m.detail.learningCursor = 0
				if t != nil && len(t.Learnings) > 0 {
					m.detail.learningCursor = len(t.Learnings) - 1
				}
			}
		case fieldDependencies:
			if m.detail.depCursor > 0 {
				m.detail.depCursor--
			} else {
				m.detail.field = fieldSubtasks
				if t != nil && m.subtaskCount(t.ID) > 0 {
					m.detail.subtaskCursor = m.subtaskCount(t.ID) - 1
				}
			}
		case fieldLearnings:
			if m.detail.learningCursor > 0 {
				m.detail.learningCursor--
			} else {
				m.detail.field = fieldDependencies
				if t != nil && len(t.Dependencies) > 0 {
					m.detail.depCursor = len(t.Dependencies) - 1
				}
			}
		}
	} else if m.detail.commentCursor > 0 {
		m.detail.commentCursor--
	} else if t := m.currentTodo(); t != nil && len(t.Comments) > 0 {
		// Wrap to the last comment.
		m.detail.commentCursor = len(t.Comments) - 1
	}
}

func (m *model) detailCursorDown() {
	m.invalidateDetailCache()
	if m.detail.page == 0 {
		t := m.currentTodo()
		switch m.detail.field {
		case fieldStartDate:
			m.detail.field = fieldDueDate
		case fieldDueDate:
			m.detail.field = fieldRecurrence
		case fieldRecurrence:
			m.detail.field = fieldPriority
		case fieldPriority:
			m.detail.field = fieldSize
		case fieldSize:
			m.detail.field = fieldProject
		case fieldProject:
			m.detail.field = fieldNotes
		case fieldNotes:
			m.detail.field = fieldTags
			m.detail.tagCursor = 0
		case fieldTags:
			if t != nil && m.detail.tagCursor < len(t.Tags)-1 {
				m.detail.tagCursor++
			} else {
				// Wrap to the top of the page.
				m.detail.field = fieldStartDate
				m.detail.tagCursor = 0
			}
		}
	} else if m.detail.page == 1 {
		t := m.currentTodo()
		switch m.detail.field {
		case fieldSubtasks:
			if t != nil && m.detail.subtaskCursor < m.subtaskCount(t.ID)-1 {
				m.detail.subtaskCursor++
			} else {
				m.detail.field = fieldDependencies
				m.detail.depCursor = 0
			}
		case fieldDependencies:
			if t != nil && m.detail.depCursor < len(t.Dependencies)-1 {
				m.detail.depCursor++
			} else {
				m.detail.field = fieldLearnings
				m.detail.learningCursor = 0
			}
		case fieldLearnings:
			if t != nil && m.detail.learningCursor < len(t.Learnings)-1 {
				m.detail.learningCursor++
			} else {
				// Wrap to the top of the page.
				m.detail.field = fieldSubtasks
				m.detail.subtaskCursor = 0
			}
		}
	} else if t := m.currentTodo(); t != nil && m.detail.commentCursor < len(t.Comments)-1 {
		m.detail.commentCursor++
	} else {
		// Wrap to the first comment.
		m.detail.commentCursor = 0
	}
}

func (m model) detailAdd() (tea.Model, tea.Cmd) {
	if m.detail.page == 2 {
		m.mode = modeInput
		m.textInput.SetValue("")
		m.textInput.Placeholder = tr("Add comment...")
		m.textInput.Focus()
		return m, textinput.Blink
	}
	switch m.detail.field {
	case fieldDependencies:
		m.mode = modeSearchDep
		m.depSearchInput.SetValue("")
		m.depSearch = searchState{}
		m.depSearchInput.Focus()
		return m, textinput.Blink
	case fieldTags:
		m.mode = modeSearchTag
		m.tagSearchInput.SetValue("")
		m.tagSearch = searchState{}
		m.tagSearchInput.Focus()
		return m, textinput.Blink
	case fieldProject:
		m.mode = modeSearchProject
		m.projSearchInput.SetValue("")
		m.projSearch = searchState{}
		m.projSearchInput.Focus()
		return m, textinput.Blink
	case fieldLearnings:
		m.mode = modeAddLearning
		m.textInput.SetValue("")
		m.textInput.Placeholder = tr("Add learning...")
		m.textInput.Focus()
		return m, textinput.Blink
	case fieldSubtasks:
		m.mode = modeAddSubtask
		m.textInput.SetValue("")
		m.textInput.Placeholder = tr("Add subtask...")
		m.textInput.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m model) detailDelete() (tea.Model, tea.Cmd) {
	t := m.currentTodo()
	if t == nil {
		return m, nil
	}
	if m.detail.page == 2 {
		if len(t.Comments) > 0 {
			m.mode = modeConfirmDeleteComment
			m.pendingComment = m.detail.commentCursor
			m.confirmMsg = tr("Delete this comment? (y/n)")
		}
		return m, nil
	}
	switch m.detail.field {
	case fieldProject:
		if t.Project != "" {
			m.mode = modeConfirmDeleteProject
			m.confirmMsg = fmt.Sprintf(tr("Remove project '%s' from this task? (y/n)"), t.Project)
		}
	case fieldNotes:
		if t.Notes != "" {
			m.pushUndo("clear notes", t.ID)
			t.SetNotes("")
			m.markModified(t.ID)
		}
	case fieldTags:
		if len(t.Tags) > 0 && m.detail.tagCursor < len(t.Tags) {
			m.mode = modeConfirmDeleteTag
			m.pendingTag = m.detail.tagCursor
			m.confirmMsg = fmt.Sprintf(tr("Remove tag '#%s' from this task? (y/n)"), t.Tags[m.detail.tagCursor])
		}
	case fieldDependencies:
		if len(t.Dependencies) > 0 && m.detail.depCursor < len(t.Dependencies) {
			m.mode = modeConfirmDeleteDep
			m.pendingDep = m.detail.depCursor
			m.confirmMsg = tr("Remove this dependency? (y/n)")
		}
	case fieldLearnings:
		if len(t.Learnings) > 0 && m.detail.learningCursor < len(t.Learnings) {
			m.mode = modeConfirmDeleteLearning
			m.pendingLearning = m.detail.learningCursor
			m.confirmMsg = fmt.Sprintf(tr("Delete learning '%s'? (y/n)"), truncate(t.Learnings[m.detail.learningCursor].Text, 40))
		}
	case fieldSubtasks:
		if m.subtaskCount(t.ID) > 0 && m.detail.subtaskCursor < m.subtaskCount(t.ID) {
			subID := m.subtaskIDs(t.ID)[m.detail.subtaskCursor]
			subTitle := subID
			if sub := m.findTodoByID(subID); sub != nil {
				subTitle = sub.Title
			}
			m.mode = modeConfirmDeleteSubtask
			m.pendingSubtask = m.detail.subtaskCursor
			m.confirmMsg = fmt.Sprintf(tr("Delete subtask '%s'? (y/n)"), truncate(subTitle, 40))
		}
	}
	return m, nil
}

func (m model) updateLearningsDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "?":
		m.mode = modeHelp
	case "u":
		return m, m.performUndo()
	case "esc":
		m.pane = paneList
	case "tab":
		m.switchTab((m.tab + 1) % numTabs)
	case "x", "delete":
		if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
			m.mode = modeConfirmDeleteLearning
			m.pendingLearning = m.learningCursor
			m.confirmMsg = fmt.Sprintf(tr("Delete learning '%s'? (y/n)"), truncate(learnings[m.learningCursor].Text, 40))
		}
	case "r":
		if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
			m.mode = modeEditLearning
			m.textInput.SetValue(learnings[m.learningCursor].Text)
			m.textInput.Placeholder = tr("Edit learning...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m model) startEditing() (tea.Model, tea.Cmd) {
	t := m.currentTodo()
	if t == nil {
		return m, nil
	}

	if m.detail.page == 2 {
		if len(t.Comments) > 0 {
			m.mode = modeEditComment
			m.pendingComment = m.detail.commentCursor
			m.textInput.SetValue(t.Comments[m.detail.commentCursor].Text)
			m.textInput.Placeholder = tr("Edit comment...")
			m.textInput.Focus()
		}
		return m, textinput.Blink
	}

	switch m.detail.field {
	case fieldStartDate:
		m.mode = modeInput
		if !t.StartDate.IsZero() {
			m.textInput.SetValue(t.StartDate.Format("02-01-06"))
		} else {
			m.textInput.SetValue("")
		}
		m.textInput.Placeholder = tr("Start date (dd-mm-yy, 'today', 'next week', '+3d')...")
		m.textInput.Focus()
	case fieldDueDate:
		m.mode = modeInput
		if !t.DueDate.IsZero() {
			m.textInput.SetValue(t.DueDate.Format("02-01-06"))
		} else {
			m.textInput.SetValue("")
		}
		m.textInput.Placeholder = tr("Due date (dd-mm-yy, 'today', 'next week', '+3d')...")
		m.textInput.Focus()
	case fieldRecurrence:
		// Cycle through canonical rules. Custom "every:Nd|w|m|y" rules are
		// returned to "none" by the next press; users can re-enter the
		// custom form via quick-add (r:Nd).
		m.pushUndo("cycle recurrence", t.ID)
		switch t.Recurrence {
		case "":
			t.SetRecurrence("daily")
		case "daily":
			t.SetRecurrence("weekly")
		case "weekly":
			t.SetRecurrence("monthly")
		case "monthly":
			t.SetRecurrence("yearly")
		case "yearly":
			t.SetRecurrence("weekdays")
		default:
			t.ClearRecurrence()
		}
		m.markModified(t.ID)
		return m, nil
	case fieldPriority:
		m.pushUndo("cycle priority", t.ID)
		switch t.Priority {
		case todo.PriorityLow:
			t.SetPriority(todo.PriorityMedium)
		case todo.PriorityMedium:
			t.SetPriority(todo.PriorityHigh)
		default:
			t.SetPriority(todo.PriorityLow)
		}
		m.markModified(t.ID)
		return m, nil
	case fieldSize:
		// Cycle Medium → Small → Large → Medium. Starts at Medium so the first
		// press moves toward "Small" (the small-task floor) — the direction
		// users will most often want.
		m.pushUndo("cycle size", t.ID)
		switch t.Size {
		case todo.SizeMedium:
			t.SetSize(todo.SizeSmall)
		case todo.SizeSmall:
			t.SetSize(todo.SizeLarge)
		default:
			t.SetSize(todo.SizeMedium)
		}
		m.markModified(t.ID)
		return m, nil
	case fieldProject:
		m.mode = modeSearchProject
		m.projSearchInput.SetValue(t.Project)
		m.projSearch = searchState{query: t.Project}
		m.projSearchInput.Focus()
		return m, textinput.Blink
	case fieldNotes:
		return m, m.openEditorForNotes()
	case fieldTags:
		m.mode = modeSearchTag
		m.tagSearchInput.SetValue("")
		m.tagSearch = searchState{}
		m.tagSearchInput.Focus()
		return m, textinput.Blink
	case fieldDependencies:
		if len(t.Dependencies) > 0 && m.detail.depCursor < len(t.Dependencies) {
			depID := t.Dependencies[m.detail.depCursor]
			for i, candidate := range m.cache.active {
				if candidate.ID == depID {
					m.pane = paneList
					m.cursor = i
					m.tab = tabTasks
					m.invalidateDetailCache()
					return m, nil
				}
			}
		}
		return m, nil
	case fieldLearnings:
		if len(t.Learnings) > 0 && m.detail.learningCursor < len(t.Learnings) {
			m.mode = modeEditLearning
			m.textInput.SetValue(t.Learnings[m.detail.learningCursor].Text)
			m.textInput.Placeholder = tr("Edit learning...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	case fieldSubtasks:
		// Enter on a subtask edits the title (matches comments + learnings).
		// 'd' still toggles done/pending.
		if m.subtaskCount(t.ID) > 0 && m.detail.subtaskCursor < m.subtaskCount(t.ID) {
			ids := m.subtaskIDs(t.ID)
			sub := m.findTodoByID(ids[m.detail.subtaskCursor])
			if sub == nil {
				return m, nil
			}
			m.mode = modeEditSubtask
			m.pendingSubtask = m.detail.subtaskCursor
			m.textInput.SetValue(sub.Title)
			m.textInput.Placeholder = tr("Edit subtask title...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	}
	return m, textinput.Blink
}
