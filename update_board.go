package main

import (
	"fmt"

	"taskr/todo"
)

// update_board.go — Board tab interactions: column focus, card cursor, and
// moving cards between stages. Completion is shared with the Tasks tab's 'd'
// via closePendingTask, so a card moved into Done follows the exact same
// close path (timer stop, open-subtask confirm, rank capture, recurrence).

// closePendingTask runs the full pending→done mutation for t: stops its
// running timer, handles the open-subtask confirm/cascade, records undo,
// captures the sequence rank, toggles, spawns the next recurrence, and
// cascades done-ness to ancestors. Returns false when a confirm prompt was
// staged instead of mutating (open subtasks without auto-close) — the confirm
// flow completes the close on its own.
func (m *model) closePendingTask(t *todo.Todo) bool {
	// Closing a task while its timer is running would leave a dangling open
	// entry — and the runningTimers index would go stale. Stop first, then
	// toggle. Mirrors the CLI done path.
	if t.IsTimerRunning() {
		m.stopTimer(t.ID)
	}
	isSub := t.ParentID != ""
	// Pending parent with open subtasks: with auto-close-subtasks on, cascade
	// them closed; otherwise stage a confirm rather than silently close (and
	// hide) the open work.
	cascadeSubs := false
	if !isSub {
		if done, total := m.subtaskProgress(t.ID); total > 0 && done < total {
			if m.autoCloseSubtasks {
				cascadeSubs = true
			} else {
				m.pendingCloseParentID = t.ID
				m.mode = modeConfirm
				m.confirmOnYes = (*model).confirmCloseParent
				m.confirmMsg = fmt.Sprintf(tr("Close '%s' with %d open subtask(s)? (y/n)"), truncate(t.Title, 40), total-done)
				return false
			}
		}
	}
	// Full snapshot: ancestor cascade + recurrence spawn can touch arbitrary
	// IDs not knowable until mid-mutation, so capture all state for a clean
	// undo.
	if isSub || cascadeSubs || t.IsRecurring() {
		m.pushUndo("close task")
	} else {
		m.pushUndo("toggle done", t.ID)
	}
	captureSeqRankAtDone(m.allTodos(), t)
	t.Toggle()
	ids := []string{t.ID}
	if t.IsRecurring() {
		if newID := m.spawnNextRecurrence(t); newID != "" {
			ids = append(ids, newID)
		}
	}
	if isSub {
		ids = append(ids, m.autoCloseAncestorsIfAllDone(t.ID)...)
	}
	if cascadeSubs {
		ids = append(ids, m.closePendingSubtree(t.ID)...)
	}
	m.markModified(ids...)
	return true
}

// stageReopenConfirm stages the "move back to active?" prompt for a done
// task. Confirmed rather than immediate because reopening voids the
// completion-rank reading — same rule for a stray 'd' on a completed row and
// for a board card moved out of the Done column.
func (m *model) stageReopenConfirm(t *todo.Todo) {
	m.pendingReopenID = t.ID
	m.mode = modeConfirm
	m.confirmOnYes = (*model).confirmReopen
	m.confirmMsg = fmt.Sprintf(tr("Move '%s' to active? (y/n)"), truncate(t.Title, 40))
}

// boardMoveCursor moves the card cursor within the focused column, wrapping
// like the other list tabs.
func (m *model) boardMoveCursor(delta int) {
	cols := m.boardColumns()
	col, cursor := m.boardSelection(cols)
	n := len(cols[col])
	if n == 0 {
		return
	}
	m.board.col = col
	m.board.cursor = (cursor + delta + n) % n
}

// boardMoveColumn moves the focus between columns, clamping at the edges. The
// cursor keeps its row where possible (boardSelection clamps it to the new
// column's length at render time).
func (m *model) boardMoveColumn(delta int) {
	cols := m.boardColumns()
	col, cursor := m.boardSelection(cols)
	col += delta
	if col < 0 {
		col = 0
	}
	if col >= len(cols) {
		col = len(cols) - 1
	}
	m.board.col, m.board.cursor = col, cursor
}

// boardFollow points the board cursor at the card with the given ID inside
// column col, falling back to the top when it isn't there (filtered out, or
// hidden past the overflow cap).
func (m *model) boardFollow(col int, id string) {
	m.board.col, m.board.cursor = col, 0
	cols := m.boardColumns()
	if col < 0 || col >= len(cols) {
		return
	}
	for i := range cols[col] {
		if cols[col][i].ID == id {
			m.board.cursor = i
			return
		}
	}
}

// boardMoveCard moves the selected card one column left or right: between
// stages it's a stage edit (undoable), into the Done column it completes the
// task via the shared close path, and out of Done it stages the reopen
// confirm — the card then reappears in its stored stage.
func (m *model) boardMoveCard(dir int) {
	cols := m.boardColumns()
	col, _ := m.boardSelection(cols)
	t := m.boardSelectedTask()
	if t == nil {
		return
	}
	doneCol := len(activeStages)
	target := col + dir
	if target < 0 || target > doneCol {
		return
	}
	if col == doneCol {
		m.stageReopenConfirm(t)
		return
	}
	if target == doneCol {
		if m.closePendingTask(t) {
			m.boardFollow(doneCol, t.ID)
		}
		return
	}
	m.pushUndo("move stage", t.ID)
	t.SetStage(activeStages[target])
	m.markModified(t.ID)
	m.boardFollow(target, t.ID)
}
