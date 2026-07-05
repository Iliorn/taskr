package main

import "taskr/todo"

// ── Detail scroll estimation ──────────────────────────────────────────────────

func (m model) estimateDetailCursorLine() int {
	t := m.currentTodo()
	if t == nil {
		return 0
	}
	twoCol := (m.termWidth - 8) >= twoColumnDetailMinWidth
	switch m.detail.page {
	case 0:
		line := 2 // title + blank
		switch m.detail.field {
		case fieldStartDate:
			return line
		case fieldDueDate:
			return line + 1
		case fieldRecurrence:
			return line + 2
		case fieldPriority:
			return line + 3
		case fieldSize:
			return line + 4
		case fieldProject:
			return line + 5
		case fieldNotes:
			return line + 6
		default: // fieldTags
			if twoCol {
				// Two-col mode: left has 7 rows (start..notes); right has
				// 3 + optional (time, completed|score) rows. Tags label sits
				// below the longer of the two.
				leftRows := 7
				rightRows := 3 // id, created, modified
				if len(t.TimeEntries) > 0 || m.descendantTimeSpent(t.ID) > 0 {
					rightRows++
				}
				if t.Status == todo.Done && !t.CompletedAt.IsZero() {
					rightRows++
				}
				if t.Status == todo.Pending {
					rightRows++ // score
				}
				rows := leftRows
				if rightRows > rows {
					rows = rightRows
				}
				line += rows + 2 // block + blank + tags label
				return line + m.detail.tagCursor
			}
			// Single column: fields stack as before.
			line += 10 // start, due, recurrence, priority, size, project, notes, id, created, modified
			if len(t.TimeEntries) > 0 || m.descendantTimeSpent(t.ID) > 0 {
				line++
			}
			if t.Status == todo.Done && !t.CompletedAt.IsZero() {
				line++
			}
			if t.Status == todo.Pending {
				line++ // score
			}
			line += 2 // blank + tags label
			return line + m.detail.tagCursor
		}
	case 1:
		line := 3 // title + blank + subtasks label
		switch m.detail.field {
		case fieldSubtasks:
			return line + m.detail.subtaskCursor
		case fieldDependencies:
			if twoCol {
				// In two-col mode subtasks (left) and deps (right) share the
				// same top, so the deps cursor sits next to its own list head.
				return line + m.detail.depCursor
			}
			if m.subtaskCount(t.ID) == 0 {
				line++
			} else {
				line += m.subtaskCount(t.ID)
			}
			line += 2 // blank + deps label
			return line + m.detail.depCursor
		default: // fieldLearnings
			if twoCol {
				// Right column = deps + blank + learnings. Learnings label
				// sits right below deps, regardless of subtasks count.
				if len(t.Dependencies) == 0 {
					line++
				} else {
					line += len(t.Dependencies)
				}
				line += 2 // blank + learnings label
				return line + m.detail.learningCursor
			}
			if m.subtaskCount(t.ID) == 0 {
				line++
			} else {
				line += m.subtaskCount(t.ID)
			}
			line++
			if len(t.Dependencies) == 0 {
				line++
			} else {
				line += len(t.Dependencies)
			}
			line += 2 // blank + learnings label
			return line + m.detail.learningCursor
		}
	case 2:
		return 3 + m.detail.commentCursor // title + blank + comments label
	}
	return 0
}

// ── List offset clamping ──────────────────────────────────────────────────────

func (m *model) clampListOffset(listLen int) {
	m.clampListOffsetFor(m.cursor, listLen)
}

// clampListOffsetFor scrolls m.listOffset so the given cursor row stays within
// the visible window. The Tasks/Projects lists track m.cursor; the Tags and
// Learnings lists keep their own cursor, so they pass it in here.
func (m *model) clampListOffsetFor(cursor, listLen int) {
	m.clampListOffsetVisible(cursor, listLen, m.listVisible())
}

// clampListOffsetVisible keeps listOffset so `cursor` stays within the next
// `visible` rows. Most tabs fill the whole list area (visible = listVisible),
// but the Projects tab's list shares space with the Gantt preview, so it passes
// its own smaller count via projectListVisibleRows.
func (m *model) clampListOffsetVisible(cursor, listLen, visible int) {
	if visible < 1 {
		visible = 1
	}
	if cursor < m.listOffset {
		m.listOffset = cursor
	}
	if cursor >= m.listOffset+visible {
		m.listOffset = cursor - visible + 1
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
	if max := listLen - visible; m.listOffset > max {
		if max < 0 {
			m.listOffset = 0
		} else {
			m.listOffset = max
		}
	}
}

// sideBySide reports whether the Tasks tab renders list and detail as two
// columns (list full-height left, always-on detail preview right). Below the
// width threshold the tab falls back to the stacked enter-to-open layout.
func (m model) sideBySide() bool {
	return m.tab == tabTasks && m.termWidth >= sideBySideMinWidth
}

// detailVisible reports whether the detail pane will be rendered as its own
// stacked panel for the current tab/mode/pane. Mirrors the showDetail
// decision in view.View so the list-height math matches what the renderer
// actually emits. In side-by-side mode the detail lives inside the list
// region's right column, so it costs no list rows and reports false here.
func (m model) detailVisible() bool {
	if m.mode != modeNormal {
		return false
	}
	switch m.tab {
	case tabTasks:
		return m.pane == paneDetail && !m.sideBySide()
	case tabProjects, tabLearnings:
		return m.pane == paneDetail
	case tabSettings:
		return false
	}
	return true
}

func (m model) listVisible() int {
	detailTotal := 0
	if m.detailVisible() {
		var contentH int
		switch m.detail.page {
		case 1:
			contentH = m.detailPage2ContentHeight()
		case 2:
			contentH = m.detailPage3ContentHeight()
		default:
			contentH = m.detailPage1ContentHeight()
		}
		if maxH := m.maxDetailHeight(); contentH > maxH {
			contentH = maxH
		}
		detailTotal = contentH + 4
	}
	fixedLines := 4
	if m.err != "" {
		fixedLines++
	}
	if m.searchQuery != "" {
		fixedLines++
	}
	if m.focusFilter {
		fixedLines++
	}
	if m.anyTimerRunning() {
		fixedLines++ // live timer line above the key hints
	}
	fixedLines += m.extraOverheadLines()
	if available := m.termHeight - fixedLines - detailTotal; available >= minListHeight {
		return available
	}
	return minListHeight
}

func (m model) estimateListHeight() int {
	headerH := minHeaderLines
	if m.err != "" {
		headerH++
	}
	if m.focusFilter {
		headerH++
	}
	if m.searchQuery != "" {
		headerH++
	}
	if m.anyTimerRunning() {
		headerH++ // live timer line above the key hints
	}
	detailH := 0
	if m.detailVisible() && m.tab != tabStats {
		detailH = 12
	}
	available := m.termHeight - headerH - footerHeight - detailH - 2
	if available < minListHeight {
		return minListHeight
	}
	return available
}

// projectListVisibleRows is how many project rows the Projects tab shows. The
// list panel gets a third of the list area (the Gantt preview takes the rest),
// less one line for the header. Both the render window (renderProjectListContent)
// and the offset clamp read this, so the project cursor can't scroll below the
// visible rows. The Projects tab hides the task detail pane, so estimateListHeight
// (detailH = 0 there) stays at or below the layout's actual list height, which
// keeps the rendered window from being clipped by the panel's own height cap.
func (m model) projectListVisibleRows() int {
	rows := m.estimateListHeight()/3 - 1
	if rows < minListPanelLines-1 {
		rows = minListPanelLines - 1
	}
	return rows
}

func (m model) maxDetailHeight() int {
	available := m.termHeight - minHeaderLines - footerHeight - detailBorderLines - minListPanelLines
	if available < minDetailHeight {
		return minDetailHeight
	}
	return available
}

func (m model) detailPage1ContentHeight() int {
	t := m.currentTodo()
	if t == nil {
		return 1
	}
	lines := 12 // 10 fixed + Size row + ID row
	if t.Status == todo.Pending {
		lines++ // Score breakdown row, rendered only for pending tasks
	}
	if len(t.Tags) == 0 {
		lines += 2
	} else {
		lines += 1 + len(t.Tags)
	}
	if len(t.TimeEntries) > 0 {
		lines++
	}
	if t.Status == todo.Done && !t.CompletedAt.IsZero() {
		lines++
	}
	return lines
}

func (m model) detailPage2ContentHeight() int {
	t := m.currentTodo()
	if t == nil {
		return 1
	}
	lines := 3 // title + blank + subtasks label
	if m.subtaskCount(t.ID) == 0 {
		lines += 2
	} else {
		lines += 1 + m.subtaskCount(t.ID)
	}
	lines++ // blank
	if len(t.Dependencies) == 0 {
		lines += 2
	} else {
		lines += 1 + len(t.Dependencies)
	}
	lines++ // blank
	if len(t.Learnings) == 0 {
		lines += 2
	} else {
		lines += 1 + len(t.Learnings)
	}
	return lines
}

func (m model) detailPage3ContentHeight() int {
	t := m.currentTodo()
	if t == nil {
		return 1
	}
	lines := 3
	if len(t.Comments) == 0 {
		lines++
	} else {
		available := m.termWidth - 32
		if available < 10 {
			available = 10
		}
		for _, c := range t.Comments {
			lines += commentLineCount(c.Text, available)
		}
	}
	return lines
}

func (m model) extraOverheadLines() int {
	switch m.mode {
	case modeInput, modeEditComment, modeEditTag, modeEditTitle,
		modeSearch, modeAddLearning, modeEditLearning, modeAddSubtask,
		modeEditSubtask, modeEditProjectInline, modeEditTimeEntry,
		modeAddTimeEntry, modeEditSyncURL, modeEditSyncToken,
		modeEditServerListen, modeEditServerToken:
		return 3
	case modeSearchDep, modeSearchTag, modeSearchProject:
		return 8
	case modeSearchTagTab:
		return 3
	case modeConfirm, modeConfirmUpdate, modeIdlePrompt:
		return 1
	}
	return 0
}
