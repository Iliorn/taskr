package main

import "github.com/Iliorn/taskr/todo"

// ── Detail scroll estimation ──────────────────────────────────────────────────

// estimateDetailCursorLine is which line of the rendered detail document the
// field cursor sits on. It has to agree with what view_detail.go actually
// emits, section for section: the scroll window is placed from this number, so
// a section left out here scrolls the cursor off the pane by exactly the rows
// it forgot.
func (m model) estimateDetailCursorLine() int {
	t := m.currentTodo()
	if t == nil {
		return 0
	}
	// Title moved to the border; content starts at the first field. The Stage
	// row is conditional, so the rows below it shift — walk the render order
	// rather than hard-coding an offset per field.
	rows := []detailField{fieldStartDate, fieldDueDate, fieldRecurrence, fieldPriority, fieldSize}
	if stageFieldVisible(t) {
		rows = append(rows, fieldStage)
	}
	rows = append(rows, fieldProject, fieldNotes)
	for i, f := range rows {
		if m.detail.field == f {
			return i
		}
	}
	if m.detail.field == fieldTags {
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
	}

	// Time entries: blank after the relations block, label first.
	teStart := relStart + m.detailRelationsHeight(t) + 1
	if m.detail.field == fieldTimeEntries {
		return teStart + 1 + m.detail.timeEntryCursor
	}

	// Comments close the document, straight after the time-entry block's own
	// trailing blank. Comments wrap, so sum the rendered line counts of
	// everything above the cursor — counting one line per comment undershoots
	// in narrow columns and the window loses the selected comment off the
	// bottom.
	line := teStart + m.detailTimeEntriesHeight(t) + 1
	available := m.termWidth - 32
	if available < 10 {
		available = 10
	}
	for i := 0; i < m.detail.commentCursor && i < len(t.Comments); i++ {
		line += commentLineCount(t.Comments[i].Text, available)
	}
	return line
}

// ── Detail scroll window ──────────────────────────────────────────────────────

// detailScrollWindow places the detail viewport: keep the offset where it is
// unless the cursor would come within detailScrollMargin of an edge, and then
// move by the least that takes. This is the whole of "scroll gradually" — the
// pane holds still while the cursor travels through it, and follows a line at
// a time once the cursor reaches the margin. Pure, so the model can pace the
// offset against its estimated geometry and the renderer can re-derive it
// against the lines it actually produced; running it twice changes nothing.
func detailScrollWindow(offset, cursor, visible, total int) int {
	if visible >= total {
		return 0
	}
	// A pane too short to hold both margins would have the two clamps cross and
	// push the cursor out of the window it is supposed to keep it in, so shrink
	// the margin to what fits: the window then centres on the cursor, which is
	// the best a four-line pane can do.
	margin := detailScrollMargin
	if fits := (visible - 1) / 2; margin > fits {
		margin = fits
	}
	if bottom := cursor - visible + 1 + margin; offset < bottom {
		offset = bottom
	}
	if top := cursor - margin; offset > top {
		offset = top
	}
	if max := total - visible; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// detailViewportHeight is how many rendered lines of the detail document the
// pane shows — the stacked panel's percentage cap, or the full column height
// when the detail sits beside the list. An estimate: the exact figure comes
// from panel geometry View computes. It only paces the stored offset, since
// applyDetailScrollN re-runs detailScrollWindow against the real line count,
// so an estimate that is off by a line costs a slightly larger step once and
// never a cursor scrolled out of sight.
func (m model) detailViewportHeight() int {
	h := m.termHeight*detailMaxHeightPct/100 - 2
	if m.sideBySide() || (m.tab == tabProjects && m.projectTaskMode) {
		// A full-height column beside the list: the window less the fixed
		// header and footer, less the panel's two borders and the blank row
		// under its border title. Deliberately not listVisible(), which
		// additionally subtracts a row per active filter — those render into
		// the one fixed status line and cost the columns nothing.
		h = m.termHeight - minHeaderLines - footerHeight - m.extraOverheadLines() - 3
	}
	if h < 3 {
		return 3
	}
	return h
}

// clampDetailScroll keeps the stored offset a window that contains the field
// cursor, so the scroll position survives between keystrokes instead of being
// recomputed from the cursor every frame. Called from clampCursors with the
// other cursors: the document shrinks under the offset just as often as a list
// shrinks under its cursor — a deleted comment, a closed subtask, a task
// switched to underneath the pane.
func (m *model) clampDetailScroll() {
	if m.currentTodo() == nil {
		m.detail.scroll = 0
		return
	}
	m.detail.scroll = detailScrollWindow(
		m.detail.scroll, m.estimateDetailCursorLine(), m.detailViewportHeight(), m.detailContentHeight())
}

// ── List offset clamping ──────────────────────────────────────────────────────

func (m *model) clampListOffset(listLen int) {
	m.clampListOffsetFor(m.cursor, listLen)
}

// clampListOffsetFor scrolls m.listOffset so the given cursor row stays within
// the visible window. The Tasks/Projects lists track m.cursor; the Tags and
// Lists that keep their own cursor pass it in here.
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
// tabs with a selected-item detail — Tasks, Tags — share the shape;
// below the width threshold each falls back to its stacked layout
// (enter-to-open for Tasks, always-on below the list for Tags).
func (m model) sideBySide() bool {
	return (m.tab == tabTasks || m.tab == tabTags) &&
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
	case tabTasks:
		return m.pane == paneDetail && !m.sideBySide()
	case tabTags:
		// Always-on preview: stacked panel below the threshold, right column above.
		return !m.sideBySide()
	case tabProjects:
		// When drilled in, buildProjectDrillContent's right column shows either the
		// Gantt (paneList) or the task detail (paneDetail), so no stacked panel is
		// needed. Outside drill mode, show the stacked panel when pane == paneDetail.
		return m.pane == paneDetail && !m.projectTaskMode
	case tabSettings, tabBoard:
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
		// Content plus the detail pane's borders, title spacing row, and the
		// separator used when it is stacked below the list.
		detailTotal = contentH + 5
	}
	// Header/footer chrome plus the list pane's blank title-spacing row.
	fixedLines := 5
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
		detailH = 13
	}
	available := m.termHeight - headerH - footerHeight - detailH - 3
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

// projectDrillTaskVisibleRows is the number of task rows shown in the left
// column of the drilled-in Projects view. The left panel inner height equals
// estimateListHeight() (same formula as buildListContent's content height,
// including the shared blank row below the border title),
// and the renderer emits one header line above the task rows, so the task row
// count is estimateListHeight()-1. Both renderProjectDrillTaskList and the
// projectTaskMode offset clamp read this helper, so the two windows agree
// exactly and no off-by-one is possible.
func (m model) projectDrillTaskVisibleRows() int {
	rows := m.estimateListHeight() - 1
	if rows < 1 {
		rows = 1
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
// section: the fields block, blank, tags label, tag rows. (The task title has
// moved to the top border of the panel and is no longer counted here.)
func (m model) detailMainHeight(t *todo.Todo) int {
	h := 0 // title is on the border now; content starts at the first field
	h += 9 // start, due, recurrence, priority, size, project, notes, created, id
	if stageFieldVisible(t) {
		h++
	}
	// Modified is drawn only when it differs from Created (see
	// renderDetailPage1) — an untouched task has nothing to say there.
	if !t.ModifiedAt.Equal(t.CreatedAt) {
		h++
	}
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
// relations section, starting at its first label row.
func (m model) detailRelationsHeight(t *todo.Todo) int {
	rows := func(n int) int {
		if n == 0 {
			return 1
		}
		return n
	}
	subH := 1 + rows(m.subtaskCount(t.ID))
	return subH + 1 + 1 + m.detailDepRows(t)
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

// detailTimeEntriesHeight is the rendered height of the time-entries section:
// label, one row per entry (or the one-line empty hint), and the blank row
// that separates it from the comments below.
func (m model) detailTimeEntriesHeight(t *todo.Todo) int {
	rows := len(t.TimeEntries)
	if rows == 0 {
		rows = 1
	}
	return 1 + rows + 1
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

// detailContentHeight is the full single-column detail document height. The
// two bare +1s are the blank rows buildDetailContent puts between its three
// pages; the time-entry block carries its own trailing blank.
func (m model) detailContentHeight() int {
	t := m.currentTodo()
	if t == nil {
		return 1
	}
	return m.detailMainHeight(t) + 1 +
		m.detailRelationsHeight(t) + 1 +
		m.detailTimeEntriesHeight(t) +
		m.detailCommentsHeight(t)
}

func (m model) extraOverheadLines() int {
	switch m.mode {
	case modeInput, modeEditComment, modeEditTag, modeEditTitle, modeEditDue,
		modeSearch, modeAddSubtask,
		modeEditSubtask, modeEditProjectInline, modeEditTimeEntry,
		modeAddTimeEntry, modeEditSyncURL, modeEditSyncToken,
		modeEditServerListen, modeEditServerToken, modeEditStages:
		return 3
	case modeSearchDep, modeSearchTag, modeSearchProject:
		return 8
	case modePalette:
		return 3 + maxPaletteResults
	case modeSearchTagTab:
		return 3
	case modeConfirm, modeConfirmUpdate, modeIdlePrompt:
		return 1
	}
	return 0
}
