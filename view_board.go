package main

import (
	"fmt"
	"strings"

	"github.com/Iliorn/taskr/todo"
)

// view_board.go renders the Board tab: one kanban column per configured stage
// (settings.json "stages"), the last of which is the Done column. The board is
// a different projection of the same filtered lists the Tasks tab shows —
// cards inherit the active list's sequence order and the done list's recency
// order, and the active search filter applies unchanged.

const (
	boardColGap  = 2
	boardMinColW = 16 // below this per-column width the board degrades to a stacked list
	// boardMinWindowCols is the fewest columns worth scrolling between. A board
	// showing one or two columns of eleven has stopped being a board — the
	// stacked list shows every stage at once and is the better answer on a
	// genuinely narrow window. Scrolling is for the case the stacked list
	// cannot help with: a wide enough terminal, too many stages.
	boardMinWindowCols = 3
	// The Done column reads the (potentially long) history list; anything past
	// this many cards can never be visible, so don't build rows for it.
	boardDoneCards = 50
)

// buildBoardColumns splits the filtered active/done lists into per-column
// card lists: one column per entry of activeStages, with the last one holding
// the done tasks (capped at boardDoneCards) rather than a stage. Pure so
// refreshCaches can derive it and tests can drive it directly.
func buildBoardColumns(active, done []todo.Todo) [][]todo.Todo {
	cols := make([][]todo.Todo, len(activeStages))
	for i := range active {
		cols[stageIndex(active[i].Stage)] = append(cols[stageIndex(active[i].Stage)], active[i])
	}
	if len(done) > boardDoneCards {
		done = done[:boardDoneCards]
	}
	cols[doneColumn()] = append([]todo.Todo(nil), done...)
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

// boardColTitles returns the column headers — the configured names verbatim,
// the last of which heads the Done column. They are user text, so they are not
// translated: a board whose columns you named is shown the way you named them.
func boardColTitles() []string {
	return append([]string(nil), activeStages...)
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

// boardWindow decides which columns are on screen: the first visible index,
// how many are visible, and their width. Splitting the width across every
// stage is what a three-stage board wants and what a ten-stage one cannot
// survive — eleven columns need a 204-column terminal before each is even
// boardMinColW wide. So the columns that fit are shown at a readable width and
// the rest are scrolled to, one column at a time.
//
// count == 0 means not even one column fits; the caller falls back to the
// stacked layout, which is still the right answer on a genuinely narrow window.
func boardWindow(n, offset, availW int) (start, count, colW int) {
	if n <= 0 {
		return 0, 0, 0
	}
	fit := 0
	for k := 1; k <= n; k++ {
		if (availW-(k-1)*boardColGap)/k < boardMinColW {
			break
		}
		fit = k
	}
	// Too few columns to still read as a board: let the caller stack instead.
	// Capped at n, so a two-stage board is never told it needs three.
	need := boardMinWindowCols
	if need > n {
		need = n
	}
	if fit < need {
		return 0, 0, 0
	}
	count = fit
	start = offset
	if max := n - count; start > max {
		start = max
	}
	if start < 0 {
		start = 0
	}
	return start, count, (availW - (count-1)*boardColGap) / count
}

func (m model) renderBoardList() string {
	cols := m.boardColumns()
	titles := boardColTitles()
	n := len(cols)
	availW := m.termWidth - 8
	start, count, colW := boardWindow(n, m.board.colOffset, availW)
	if count == 0 {
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
	widths := boardColWidths(cols[start:start+count], titles[start:start+count], colW*count+(count-1)*boardColGap)
	rendered := make([][]string, 0, count)
	for c := start; c < start+count; c++ {
		cursor := -1
		if c == selCol {
			cursor = selCursor
		}
		rendered = append(rendered,
			m.renderBoardColumn(cols[c], titles[c], c == n-1, cursor, widths[c-start], budget))
	}
	return zipColumnsW(widths, boardColGap, rendered...)
}

// boardColWidths splits the board's width across the visible columns in
// proportion to what each one has to show, instead of giving every column an
// equal share. An equal split is what made a board of one busy stage and three
// empty ones clip its only real cards at twenty characters while three columns
// of blanks sat beside them at the same width.
//
// Every column keeps boardMinColW so an empty stage is still a place a card can
// be moved to and its heading stays readable; the surplus above that floor goes
// to the columns that can use it, and no column grows past what it actually
// wants. This is the same rule the Tasks tab's title column follows
// (contentFitWidth) — hug the content, cap at a share of the pane — so the
// board reflows on a resize the way the rest of the app does.
func boardColWidths(cols [][]todo.Todo, titles []string, availW int) []int {
	n := len(cols)
	widths := make([]int, n)
	if n == 0 {
		return widths
	}
	budget := availW - (n-1)*boardColGap
	// want is the width that would clip nothing: the widest card (plus the
	// cursor marker and the priority "!") or the heading with its count,
	// whichever is longer.
	want := make([]int, n)
	surplus := budget
	for i := range cols {
		want[i] = len([]rune(fmt.Sprintf("%s (%d)", titles[i], len(cols[i]))))
		for j := range cols[i] {
			w := len([]rune(cols[i][j].Title)) + len([]rune(cursorGap)) + 2 // marker + " !"
			if w > want[i] {
				want[i] = w
			}
		}
		if want[i] < boardMinColW {
			want[i] = boardMinColW
		}
		widths[i] = boardMinColW
		surplus -= boardMinColW
	}
	if surplus <= 0 {
		// Nothing to share out: fall back to the equal split the caller sized
		// its window with, which is what boardWindow already guaranteed fits.
		even := budget / n
		for i := range widths {
			widths[i] = even
		}
		widths[n-1] += budget - even*n
		return widths
	}
	// Hand out the surplus in proportion to how much each column still wants,
	// then give any rounding residue to the hungriest column so the row fills
	// the pane exactly rather than leaving a ragged edge.
	totalWant := 0
	for i := range want {
		totalWant += want[i] - boardMinColW
	}
	left, hungriest := surplus, 0
	for i := range widths {
		if totalWant > 0 {
			grow := surplus * (want[i] - boardMinColW) / totalWant
			if grow > want[i]-boardMinColW {
				grow = want[i] - boardMinColW
			}
			widths[i] += grow
			left -= grow
		}
		if want[i]-widths[i] > want[hungriest]-widths[hungriest] {
			hungriest = i
		}
	}
	if left > 0 {
		widths[hungriest] += left
	}
	return widths
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
	marker := cursorGap
	if selected {
		marker = cursorMark
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
