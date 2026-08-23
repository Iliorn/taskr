package main

import (
	"strings"
	"testing"

	"github.com/Iliorn/taskr/todo"
	"github.com/charmbracelet/x/ansi"
)

// board_ux_test.go covers the three things that make a many-stage board
// usable: a column window that scrolls, a Stage field in the detail pane, and
// a switch that removes the whole surface for people who never use it.

// manyStages installs a board of n columns — n-1 working ones plus the Done
// column that always ends the list — and restores the previous list. It must
// be called *after* the model is built: initialModel re-applies settings.json,
// which would reset activeStages to the defaults underneath it.
func manyStages(t *testing.T, n int) func() {
	t.Helper()
	prev := activeStages
	stages := make([]string, 0, n)
	for _, name := range []string{"Active", "On hold", "Awaiting", "Review", "Blocked",
		"Cancelled", "Later", "Icebox", "Triage", "Ready"} {
		if len(stages) == n-1 {
			break
		}
		stages = append(stages, name)
	}
	applyStages(append(stages, "Done"))
	return func() { applyStages(prev) }
}

// ── The column window ─────────────────────────────────────────────────────────

// Eleven columns do not fit on any ordinary terminal, and shrinking them all to
// four characters serves nobody. The window shows what fits and scrolls.
func TestBoardWindowShowsWhatFitsAndScrolls(t *testing.T) {
	// Everything fits: no window, and the offset is pinned at zero.
	if start, count, _ := boardWindow(4, 0, 112); start != 0 || count != 4 {
		t.Errorf("4 columns in 112: start %d count %d, want the whole board", start, count)
	}
	// Too many: a window, and it honours the offset.
	start, count, colW := boardWindow(11, 3, 112)
	if count >= 11 || count < boardMinWindowCols {
		t.Fatalf("11 columns in 112: count %d, want a readable window", count)
	}
	if start != 3 {
		t.Errorf("start = %d, want the requested offset", start)
	}
	if colW < boardMinColW {
		t.Errorf("colW = %d, below the readable floor %d", colW, boardMinColW)
	}
	// The offset cannot scroll past the end.
	if start, _, _ := boardWindow(11, 99, 112); start != 11-count {
		t.Errorf("start = %d for a runaway offset, want the last full window", start)
	}
	// Too narrow for even a few columns: the caller stacks instead.
	if _, count, _ := boardWindow(11, 0, 42); count != 0 {
		t.Errorf("count = %d on a narrow window, want the stacked fallback", count)
	}
}

// The arrows move focus; the window follows only when focus would leave it.
// That is what makes it scroll a column at a time rather than jump.
func TestBoardWindowFollowsTheFocusedColumn(t *testing.T) {
	m := modelWithTasks(t, todo.New("a card"))
	defer manyStages(t, 11)()
	m.termWidth, m.termHeight = 120, 30
	m.switchTab(tabBoard)
	m.markCacheDirty()
	m.ensureCache()

	cols := m.boardColumns()
	_, count, _ := boardWindow(len(cols), 0, m.termWidth-8)
	if count == 0 || count >= len(cols) {
		t.Fatalf("test needs a scrolling board: %d of %d columns visible", count, len(cols))
	}

	// Walking right stays put until the focus reaches the edge, then scrolls
	// one column per step.
	for i := 0; i < count-1; i++ {
		m = sendKey(t, m, "right")
		if m.board.colOffset != 0 {
			t.Fatalf("offset moved to %d while the focus was still on screen", m.board.colOffset)
		}
	}
	m = sendKey(t, m, "right")
	if m.board.colOffset != 1 {
		t.Errorf("offset = %d after stepping past the edge, want 1", m.board.colOffset)
	}
	// And back again.
	for i := 0; i < count; i++ {
		m = sendKey(t, m, "left")
	}
	if m.board.colOffset != 0 {
		t.Errorf("offset = %d after walking back, want 0", m.board.colOffset)
	}
}

// A scrolled board must say so, or it just looks like a board missing columns.
func TestBoardTitleNamesTheVisibleSlice(t *testing.T) {
	m := modelWithTasks(t, todo.New("a card"))
	defer manyStages(t, 11)()
	m.termWidth, m.termHeight = 120, 30
	m.switchTab(tabBoard)
	m.markCacheDirty()
	m.ensureCache()
	if title := m.listPanelTitle(); !strings.Contains(title, "/11") {
		t.Errorf("panel title %q does not say which slice of 11 columns is shown", title)
	}
}

// ── The Stage field ───────────────────────────────────────────────────────────

func stageFieldModel(t *testing.T) model {
	t.Helper()
	m := modelWithTasks(t, todo.New("Fix the boiler"))
	m.switchTab(tabTasks)
	m.pane = paneDetail
	if task := m.currentTodo(); task != nil {
		m.detailTaskID = task.ID
	}
	m.detail.field = fieldStage
	return m
}

// ←/→ change the value on the Stage row, the way they do on a Settings row.
func TestStageFieldCyclesWithTheArrows(t *testing.T) {
	m := stageFieldModel(t)
	task := m.currentTodo()
	if !stageFieldVisible(task) {
		t.Fatal("a pending top-level task has no Stage field")
	}
	start := stageIndex(task.Stage)

	m = sendKey(t, m, "right")
	if got := stageIndex(m.get(task.ID).Stage); got != (start+1)%len(pendingStages()) {
		t.Errorf("→ moved to stage %d, want %d", got, (start+1)%len(pendingStages()))
	}
	m = sendKey(t, m, "left")
	if got := stageIndex(m.get(task.ID).Stage); got != start {
		t.Errorf("← did not undo →: stage %d, want %d", got, start)
	}
	// It wraps rather than stopping at the ends, and never reaches the last
	// column — completing a task has one path, and it is not this one.
	for i := 0; i < len(activeStages)+2; i++ {
		m = sendKey(t, m, "right")
		if m.get(task.ID).Status != todo.Pending {
			t.Fatal("cycling the stage completed the task")
		}
	}
}

// Everywhere else in the pane the arrows still jump section.
func TestArrowsStillJumpSectionOffTheStageRow(t *testing.T) {
	m := stageFieldModel(t)
	m.detail.field = fieldPriority
	m = sendKey(t, m, "right")
	if m.detail.field == fieldPriority {
		t.Error("→ on a non-Stage field did not jump section")
	}
}

// A subtask never reaches the board and Done is a status, not a stage.
func TestStageFieldHidesWhereItWouldLie(t *testing.T) {
	parent := todo.New("parent")
	parent.ID = "p"
	sub := todo.New("subtask")
	sub.ID = "s"
	sub.ParentID = "p"
	done := todo.New("finished")
	done.ID = "d"
	done.Status = todo.Done
	m := modelWithTasks(t, parent, sub, done)

	if !stageFieldVisible(m.get("p")) {
		t.Error("a pending top-level task should have the field")
	}
	if stageFieldVisible(m.get("s")) {
		t.Error("a subtask has no stage — it never reaches the board")
	}
	if stageFieldVisible(m.get("d")) {
		t.Error("a done task has no stage — Done is a status")
	}
}

// Up/down must step over the row when it is not there, or the cursor lands on
// a field the pane is not drawing.
func TestDetailNavigationSkipsAHiddenStageRow(t *testing.T) {
	defer applyShowBoard(true)
	m := stageFieldModel(t)
	applyShowBoard(false)

	m.detail.field = fieldSize
	m = sendKey(t, m, "down")
	if m.detail.field == fieldStage {
		t.Error("↓ landed on the Stage row while the board is off")
	}
	m.detail.field = fieldProject
	m = sendKey(t, m, "up")
	if m.detail.field == fieldStage {
		t.Error("↑ landed on the Stage row while the board is off")
	}
}

// ── Turning the board off ─────────────────────────────────────────────────────

// Off means gone: out of the bar, out of tab cycling, off its digit, and not
// somewhere the cursor can be left standing.
func TestTurningTheBoardOffRemovesTheWholeSurface(t *testing.T) {
	defer applyShowBoard(true)
	// toggleShowBoard persists, so this needs its own home — writing the shared
	// one would leave every later model in the binary loading a settings.json
	// with the board off.
	setTestHome(t, t.TempDir())
	if err := ensureStorageDir(); err != nil {
		t.Fatal(err)
	}
	m := modelWithTasks(t, todo.New("a card"))
	m.termWidth, m.termHeight = 120, 30
	m.switchTab(tabBoard)

	m.toggleShowBoard()
	if showBoard {
		t.Fatal("the toggle did not turn the board off")
	}
	if m.tab == tabBoard {
		t.Error("the cursor was left standing on a tab that is no longer shown")
	}
	if tabVisible(tabBoard) {
		t.Error("tabVisible still reports the board")
	}
	if _, ok := tabForNumberKey("5"); ok {
		t.Error("5 still lands on the hidden board")
	}
	// Cycling must step over it rather than stopping there.
	seen := map[tab]bool{}
	cur := tabTasks
	for i := 0; i < numTabs*2; i++ {
		cur = nextTab(cur, 1)
		seen[cur] = true
	}
	if seen[tabBoard] {
		t.Error("tab cycling still stops on the board")
	}
	if bar := ansi.Strip(m.renderTabs(200)); strings.Contains(bar, "Board") {
		t.Errorf("the tab bar still shows the board: %q", bar)
	}
	// The numbers do not renumber — Stats stays 6 whether or not the board is on.
	if got, ok := tabForNumberKey("6"); !ok || got != tabStats {
		t.Error("hiding the board renumbered the other tabs")
	}
}

// The setting has to survive a restart, or the tab comes back on next launch.
func TestBoardVisibilityPersists(t *testing.T) {
	defer applyShowBoard(true)
	m := settingsModel(t)
	m.settingsCursor = settingShowBoard
	m = sendKey(t, m, "enter")

	if showBoard {
		t.Fatal("enter on the row did not toggle it")
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !got.BoardDisabled {
		t.Error("settings.json did not record that the board is off")
	}
	// And the flag is negative, so an untouched file leaves the board on.
	applyShowBoard(!appSettings{}.BoardDisabled)
	if !showBoard {
		t.Error("a settings file with no board key should leave the board on")
	}
}

// ── Column widths ────────────────────────────────────────────────────────────

// An equal split gave a board of one busy stage and three empty ones four
// columns of the same width — clipping the only real cards while three columns
// of blanks sat beside them.
func TestBoardColumnsAreWidthedByWhatTheyHold(t *testing.T) {
	busy := []todo.Todo{todo.New("A card whose title is a good deal longer than the others")}
	titles := []string{"Backlog", "In progress", "Review", "Done"}
	cols := [][]todo.Todo{busy, nil, nil, nil}

	const availW = 100
	got := boardColWidths(cols, titles, availW)
	if len(got) != 4 {
		t.Fatalf("want a width per column, got %d", len(got))
	}
	for i, w := range got {
		if w < boardMinColW {
			t.Errorf("column %d width %d is below the floor %d — an empty stage is still a place to drop a card",
				i, w, boardMinColW)
		}
	}
	if !(got[0] > got[1]) {
		t.Errorf("the column with the long card should be wider than the empty ones: %v", got)
	}
	// The row fills the pane exactly: no ragged edge, no overflow.
	sum := (len(cols) - 1) * boardColGap
	for _, w := range got {
		sum += w
	}
	if sum != availW {
		t.Errorf("columns + gaps = %d, want exactly the available %d: %v", sum, availW, got)
	}
}

// When there is not enough width to give every column its floor, the split
// falls back to even columns rather than handing some column a negative width.
func TestBoardColumnWidthsFallBackWhenCrowded(t *testing.T) {
	titles := []string{"A", "B", "C", "D"}
	cols := make([][]todo.Todo, 4)
	availW := boardMinColW*2 + 3*boardColGap
	got := boardColWidths(cols, titles, availW)
	sum := 3 * boardColGap
	for _, w := range got {
		if w <= 0 {
			t.Fatalf("crowded board produced a non-positive width: %v", got)
		}
		sum += w
	}
	if sum != availW {
		t.Errorf("columns + gaps = %d, want %d: %v", sum, availW, got)
	}
}

// A board whose columns are a different width every time a card is added reads
// as clutter even when no row is clipped, so the grid is even until a column
// would actually be clipped.
func TestBoardColumnsAreAnEvenGridWhenNothingClips(t *testing.T) {
	titles := []string{"Backlog", "In progress", "Review", "Done"}
	cols := [][]todo.Todo{{todo.New("short")}, {todo.New("also short")}, nil, nil}

	const availW = 120
	got := boardColWidths(cols, titles, availW)
	lo, hi := got[0], got[0]
	for _, w := range got {
		if w < lo {
			lo = w
		}
		if w > hi {
			hi = w
		}
	}
	if hi-lo > 1 {
		t.Errorf("columns %v differ by %d; nothing needs the extra width, so the grid should be even", got, hi-lo)
	}
	sum := (len(cols) - 1) * boardColGap
	for _, w := range got {
		sum += w
	}
	if sum != availW {
		t.Errorf("columns + gaps = %d, want exactly the available %d: %v", sum, availW, got)
	}
}

// The heading sits over its own cards. The card marker column is blank on every
// row but the selected one, so a flush-left heading hung two characters to the
// left of every title under it.
func TestBoardHeadingsLineUpWithTheirCards(t *testing.T) {
	a := todo.New("alpha")
	b := todo.New("beta")
	b.Stage = "In progress"
	m := newTagModel(a, b)
	m.tab = tabBoard
	m.termWidth, m.termHeight = 120, 30
	m.refreshCaches()

	lines := strings.Split(ansi.Strip(m.renderBoardList()), "\n")
	if len(lines) < 3 {
		t.Fatalf("board rendered %d lines, want a header, a rule and cards", len(lines))
	}
	header, cards := lines[0], lines[2]
	for _, title := range []string{"In progress", "Beta"} {
		if !strings.Contains(header+cards, title) {
			t.Fatalf("board is missing %q:\n%s", title, strings.Join(lines, "\n"))
		}
	}
	// Display columns, not byte offsets: the card row carries the ▶ marker and
	// the column dividers, which are three bytes each.
	col := func(line, sub string) int { return ansi.StringWidth(line[:strings.Index(line, sub)]) }
	if h, c := col(header, "In progress"), col(cards, "Beta"); h != c {
		t.Errorf("heading starts at column %d but its card at %d:\n%s", h, c, strings.Join(lines, "\n"))
	}
}

// The columns are separated by a divider that runs the height of the pane, so
// an empty stage still holds its place in the grid.
func TestBoardDividersRunTheFullPane(t *testing.T) {
	a := todo.New("alpha")
	m := newTagModel(a)
	m.tab = tabBoard
	m.termWidth, m.termHeight = 120, 30
	m.refreshCaches()

	lines := strings.Split(ansi.Strip(m.renderBoardList()), "\n")
	want := strings.Count(lines[2], boardColSep)
	if want != len(activeStages)-1 {
		t.Fatalf("card row has %d dividers, want one between each of %d columns", want, len(activeStages))
	}
	if n := len(lines); n < m.listVisible()-2 {
		t.Errorf("board drew %d rows, want the pane's %d so the dividers reach the bottom", n, m.listVisible()-2)
	}
	for i, line := range lines[2:] {
		if got := strings.Count(line, boardColSep); got != want {
			t.Errorf("row %d has %d dividers, want %d: %q", i+2, got, want, line)
		}
	}
}
