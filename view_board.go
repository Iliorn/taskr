package main

import (
	"fmt"
	"strings"

	"taskr/todo"
)

// view_board.go renders the Board tab: one kanban column per configured stage
// (settings.json "stages") plus a trailing Done column. The board is a
// different projection of the same filtered lists the Tasks tab shows — cards
// inherit the active list's sequence order and the done list's recency order,
// and the active search filter applies unchanged.

const (
	boardColGap  = 2
	boardMinColW = 16 // below this per-column width the board degrades to a stacked list
	// The Done column reads the (potentially long) history list; anything past
	// this many cards can never be visible, so don't build rows for it.
	boardDoneCards = 50
)

// buildBoardColumns splits the filtered active/done lists into per-column
// card lists: index i < len(activeStages) is stage i, the last index is Done
// (capped at boardDoneCards). Pure so refreshCaches can derive it and tests
// can drive it directly.
func buildBoardColumns(active, done []todo.Todo) [][]todo.Todo {
	cols := make([][]todo.Todo, len(activeStages)+1)
	for i := range active {
		cols[stageIndex(active[i].Stage)] = append(cols[stageIndex(active[i].Stage)], active[i])
	}
	if len(done) > boardDoneCards {
		done = done[:boardDoneCards]
	}
	cols[len(activeStages)] = append([]todo.Todo(nil), done...)
	return cols
}

// boardColumns returns the cached per-column card lists (rebuilt by
// refreshCaches/refreshFilteredCaches alongside the active/done split — the
// column split copies task values, and doing that per frame is exactly the
// per-frame O(active) work cacheState exists to prevent). The nil-cache
// fallback builds directly so a bare model (tests) stays correct.
func (m model) boardColumns() [][]todo.Todo {
	if m.cache.boardCols != nil {
		return m.cache.boardCols
	}
	return buildBoardColumns(m.cache.active, m.cache.done)
}

// boardColTitles returns the column headers: the stage names plus Done.
func boardColTitles() []string {
	titles := append([]string(nil), activeStages...)
	return append(titles, tr("Done"))
}

// boardSelection clamps the stored board cursor against the current columns,
// so a stale position (task completed elsewhere, stage list edited) degrades
// to the nearest valid card instead of pointing past the end.
func (m model) boardSelection(cols [][]todo.Todo) (col, cursor int) {
	col, cursor = m.board.col, m.board.cursor
	if col < 0 {
		col = 0
	}
	if col >= len(cols) {
		col = len(cols) - 1
	}
	if n := len(cols[col]); cursor >= n {
		cursor = n - 1 // -1 on an empty column = no selected card
	}
	if cursor < 0 {
		cursor = 0
	}
	return col, cursor
}

// boardSelectedTask returns the task under the board cursor, or nil on an
// empty column.
func (m model) boardSelectedTask() *todo.Todo {
	cols := m.boardColumns()
	col, cursor := m.boardSelection(cols)
	if len(cols[col]) == 0 {
		return nil
	}
	return m.get(cols[col][cursor].ID)
}

func (m model) renderBoardList() string {
	cols := m.boardColumns()
	titles := boardColTitles()
	n := len(cols)
	availW := m.termWidth - 8
	colW := (availW - (n-1)*boardColGap) / n
	if colW < boardMinColW {
		return m.renderBoardStacked(cols, titles)
	}
	// Rows available inside the list panel: buildListContent subtracts the two
	// border lines from the outer height, mirrored here so per-column clipping
	// and the "+N more" marker line up with what actually fits.
	budget := m.listVisible() - 2
	if budget < 4 {
		budget = 4
	}
	selCol, selCursor := m.boardSelection(cols)
	rendered := make([][]string, n)
	for c := range cols {
		cursor := -1
		if c == selCol {
			cursor = selCursor
		}
		rendered[c] = m.renderBoardColumn(cols[c], titles[c], c == len(cols)-1, cursor, colW, budget)
	}
	return zipColumns(colW, boardColGap, rendered...)
}

// renderBoardColumn builds one column's lines: header with count, a rule, then
// one card per row, clipped to the row budget with a "+N more" marker. cursor
// is the selected card index, or -1 when the column isn't focused. doneCol
// renders its cards dim — they're history, not work.
func (m model) renderBoardColumn(cards []todo.Todo, title string, doneCol bool, cursor, colW, budget int) []string {
	lines := make([]string, 0, budget)
	header := truncate(fmt.Sprintf("%s (%d)", title, len(cards)), colW)
	lines = append(lines, statsHeaderStyle.Render(header))
	// The focused column is marked by an accented rule under its header — the
	// header text itself keeps the standard style so it stays legible.
	rule := strings.Repeat("─", colW)
	if cursor != -1 {
		lines = append(lines, selectedStyle.Render(rule))
	} else {
		lines = append(lines, dimStyle.Render(rule))
	}
	if len(cards) == 0 {
		lines = append(lines, dimStyle.Render("  "+tr("empty")))
		return lines
	}
	maxCards := budget - len(lines)
	overflow := 0
	if len(cards) > maxCards {
		overflow = len(cards) - (maxCards - 1) // reserve the last row for the marker
	}
	for i := range cards {
		if overflow > 0 && i == maxCards-1 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  +%d %s", overflow, tr("more"))))
			break
		}
		lines = append(lines, m.renderBoardCard(&cards[i], doneCol, i == cursor, colW))
	}
	return lines
}

// renderBoardCard renders one card row: cursor marker, truncated title, and
// the high-priority "!" the task list uses. Selected cards get the selection
// style; Done-column cards are dim.
func (m model) renderBoardCard(t *todo.Todo, doneCol, selected bool, colW int) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	suffix := ""
	if !doneCol && t.Priority == todo.PriorityHigh {
		suffix = " !"
	}
	title := truncate(t.Title, colW-len([]rune(marker))-len([]rune(suffix)))
	switch {
	case selected:
		return selectedStyle.Render(marker + title + suffix)
	case doneCol:
		return dimStyle.Render(marker + title + suffix)
	default:
		return normalStyle.Render(marker+title) + overdueStyle.Render(suffix)
	}
}

// renderBoardStacked is the narrow-terminal fallback: stages as full-width
// sections instead of side-by-side columns. Height clipping is left to
// buildListContent, matching the other full-width tabs.
func (m model) renderBoardStacked(cols [][]todo.Todo, titles []string) string {
	var sb strings.Builder
	availW := m.termWidth - 8
	selCol, selCursor := m.boardSelection(cols)
	for c := range cols {
		if c > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(statsHeaderStyle.Render(truncate(fmt.Sprintf("%s (%d)", titles[c], len(cols[c])), availW)) + "\n")
		if len(cols[c]) == 0 {
			sb.WriteString(dimStyle.Render("  "+tr("empty")) + "\n")
			continue
		}
		for i := range cols[c] {
			cursor := -1
			if c == selCol {
				cursor = selCursor
			}
			sb.WriteString(m.renderBoardCard(&cols[c][i], c == len(cols)-1, i == cursor, availW) + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
