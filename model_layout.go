package main

import "taskr/todo"

// ── Detail scroll estimation ──────────────────────────────────────────────────

func (m model) estimateDetailCursorLine() int {
	t := m.currentTodo()
	if t == nil {
		return 0
	}
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
	case fieldTags:
		// Tags label sits below the fields block; +1 skips the label row.
		return m.detailMainHeight(t) - m.detailTagsRows(t) + m.detail.tagCursor
	}

	// Relations section: one blank line after the main block, label first.
	relStart := m.detailMainHeight(t) + 1
	subRows := m.subtaskCount(t.ID)
	if subRows == 0 {
		subRows = 1
	}
	switch m.detail.field {
	case fieldSubtasks:
		return relStart + 1 + m.detail.subtaskCursor
	case fieldDependencies:
		return relStart + 1 + subRows + 2 + m.detail.depCursor
	case fieldLearnings:
		return relStart + 1 + subRows + 2 + m.detailDepRows(t) + 2 + m.detail.learningCursor
	}

	// Comments section: blank after the relations block, label first.
	// Comments wrap, so sum the rendered line counts of everything above the
	// cursor — counting one line per comment undershoots in narrow columns
	// and the scroll window loses the selected comment off the bottom.
	comStart := relStart + m.detailRelationsHeight(t) + 1
	line = comStart + 1
	available := m.termWidth - 32
	if available < 10 {
		available = 10
	}
	for i := 0; i < m.detail.commentCursor && i < len(t.Comments); i++ {
		line += commentLineCount(t.Comments[i].Text, available)
	}
	return line
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

// sideBySide reports whether the current tab renders list and detail as two
// columns (list full-height left, always-on detail preview right). The list
// tabs with a selected-item detail — Tasks, Learnings, Tags — share the shape;
// below the width threshold each falls back to its stacked layout
// (enter-to-open for Tasks/Learnings, always-on below the list for Tags).
func (m model) sideBySide() bool {
	return (m.tab == tabTasks || m.tab == tabLearnings || m.tab == tabTags) &&
		m.termWidth >= sideBySideMinWidth
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
	case tabTasks, tabLearnings:
		return m.pane == paneDetail && !m.sideBySide()
	case tabTags:
		// Always-on preview: stacked panel below the threshold, right column above.
		return !m.sideBySide()
	case tabProjects:
		return m.pane == paneDetail
	case tabSettings:
		return false
	}
	return true
}

func (m model) listVisible() int {
	detailTotal := 0
	if m.detailVisible() {
		contentH := m.detailContentHeight()
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

// detailTagsRows is the number of rows below the tags label: the tag list,
// or the one-line "no tags" hint.
func (m model) detailTagsRows(t *todo.Todo) int {
	if len(t.Tags) == 0 {
		return 1
	}
	return len(t.Tags)
}

// detailMainHeight is the rendered height of the detail column's first
// section: title, blank, the fields block, blank, tags label, tag rows.
func (m model) detailMainHeight(t *todo.Todo) int {
	h := 2  // title + blank
	h += 10 // start, due, recurrence, priority, size, project, notes, id, created, modified
	if len(t.TimeEntries) > 0 || m.descendantTimeSpent(t.ID) > 0 {
		h++
	}
	if t.Status == todo.Done && !t.CompletedAt.IsZero() {
		h++
	}
	if t.Status == todo.Pending {
		h++ // score
	}
	h += 2 // blank + tags label
	return h + m.detailTagsRows(t)
}

// detailRelationsHeight is the rendered height of the subtasks/dependencies/
// learnings section, starting at its first label row.
func (m model) detailRelationsHeight(t *todo.Todo) int {
	rows := func(n int) int {
		if n == 0 {
			return 1
		}
		return n
	}
	subH := 1 + rows(m.subtaskCount(t.ID))
	return subH + 1 + 1 + m.detailDepRows(t) + 1 + 1 + rows(len(t.Learnings))
}

// detailDepRows is the number of rows under the Dependencies label: outbound
// ↧ rows, then inbound ↥ rows, or the one-line hint when there are neither.
func (m model) detailDepRows(t *todo.Todo) int {
	out := len(t.Dependencies)
	in := len(dependentsOf(m.allTodos(), t.ID))
	if out == 0 && in == 0 {
		return 1
	}
	return out + in
}

// detailCommentsHeight is the rendered height of the comments section:
// label plus wrapped comment lines (or the one-line empty hint).
func (m model) detailCommentsHeight(t *todo.Todo) int {
	lines := 1 // label
	if len(t.Comments) == 0 {
		return lines + 1
	}
	available := m.termWidth - 32
	if available < 10 {
		available = 10
	}
	for _, c := range t.Comments {
		lines += commentLineCount(c.Text, available)
	}
	return lines
}

// detailContentHeight is the full single-column detail document height.
func (m model) detailContentHeight() int {
	t := m.currentTodo()
	if t == nil {
		return 1
	}
	return m.detailMainHeight(t) + 1 +
		m.detailRelationsHeight(t) + 1 +
		m.detailCommentsHeight(t)
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
