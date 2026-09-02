package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Iliorn/taskr/todo"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// pickerWindowStart computes the scroll offset for the detail-pane search
// pickers (dep / tag / project). The pickers render a fixed `max`-row viewport
// and keep no persistent offset state; the window is derived purely from the
// cursor each frame so no offset field is needed.
//
// When there are results below the visible window the caller must reserve the
// last slot for a "… N more below" indicator, which means the cursor must sit
// at most at slot max−2 (not max−1). This function handles that by pulling
// start forward when needed so the cursor never lands on the indicator slot.
// Similarly, when start > 0 the first slot becomes a "… N more above"
// indicator, so the cursor must sit at slot ≥ 1; start is adjusted backward
// when needed.
//
// The caller renders exactly max lines and the cursor is always on a result
// row, never on an indicator row.
func pickerWindowStart(cursor, total, max int) (start int, hasAbove, hasBelow bool) {
	if max < 1 {
		max = 1
	}
	if total <= 0 {
		return 0, false, false
	}
	// First pass: anchor cursor at the bottom of the window.
	start = cursor - (max - 1)
	if start < 0 {
		start = 0
	}
	maxStart := total - max
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}

	hasAbove = start > 0
	hasBelow = start+max < total

	// Second pass: if the cursor would land on an indicator slot, shift start.
	//
	// hasBelow reserves the last slot (index max−1) for the below-indicator.
	// If cursor == start+max−1 (last slot), pull start forward by 1 so the
	// cursor moves to slot max−2, and recompute.
	if hasBelow && cursor == start+max-1 {
		start++
		if start > maxStart {
			start = maxStart
		}
		hasAbove = start > 0
		hasBelow = start+max < total
	}

	// hasAbove reserves the first slot (index 0) for the above-indicator.
	// If cursor == start (first slot), pull start backward by 1 so the cursor
	// moves to slot 1, and recompute.
	if hasAbove && cursor == start {
		start--
		if start < 0 {
			start = 0
		}
		hasAbove = start > 0
		hasBelow = start+max < total
		// A backward shift may again cause cursor == start+max−1 (hasBelow
		// conflict) only if max==1, which is prevented by the guard above.
	}

	return start, hasAbove, hasBelow
}

// truncateLines ANSI-aware-truncates every line to maxW display cells so
// over-long lines can never wrap inside a bordered panel.
//
// The cut carries the ellipsis marker. Cutting silently is what produced
// detail-pane rows like "No dependencies. Press 'a' to add o" — a sentence that
// stops mid-word and gives the reader no way to tell a clipped line from one
// that simply ends there.
func truncateLines(lines []string, maxW int) {
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, maxW, ellipsis)
	}
}

// panelContentHeight returns the rows available to pane content after the top
// and bottom borders and the shared blank row below the border title.
func panelContentHeight(outerH int) int {
	h := outerH - 3
	if h < 1 {
		return 1
	}
	return h
}

// withBorderTitle rewrites the top border line of a lipgloss-rendered
// rounded-border box to embed the title text, producing the standard TUI look:
//
//	╭─ Title ──────────────────╮
//	│ content …               │
//	╰──────────────────────────╯
//
// boxW is the Width() argument that was passed to the panel's .Render call
// (the content width, excluding borders and padding). focused controls which
// border color is used (accent when true, dim when false). title is plain text;
// it is ANSI-truncated with "…" so the box corners always survive.
// If the box is too narrow to embed any title the function returns rendered
// unchanged.
func withBorderTitle(rendered, title string, boxW int, focused bool) string {
	if rendered == "" {
		return rendered
	}

	// style.Width(w) with RoundedBorder produces a top line:
	//   ╭ + w dashes + ╮   (total box width w+2, excluding the 2-space margin)
	//
	// With an embedded title we replace the dash run:
	//   ╭─ <title> <fill>╮
	//   3(╭─ ) + T(title) + 1( ) + F(fill) + 1(╮) = T+F+5 = w+2
	//   → F = w - T - 3
	//
	// Require at least 1 fill dash (F≥1) → max title = w - 4 where w = boxW.
	maxTitle := boxW - 4
	if maxTitle <= 0 {
		return rendered // box too narrow for any title
	}
	title = ansi.Truncate(title, maxTitle, "…")
	titleW := ansi.StringWidth(title)
	fillW := boxW - titleW - 3
	if fillW < 1 {
		fillW = 1
	}

	borderFg := currentTheme.dim
	if focused {
		borderFg = currentTheme.accent
	}
	borderSty := lipgloss.NewStyle().Foreground(borderFg)
	titleSty := lipgloss.NewStyle().Bold(true).Foreground(currentTheme.accent)

	margin := "  " // MarginLeft(2) from detailPanelStyle / listPanelStyle
	topLine := margin +
		borderSty.Render("╭─ ") +
		titleSty.Render(title) +
		borderSty.Render(" "+strings.Repeat("─", fillW)+"╮")

	// Replace only the first line of rendered (everything up to the first \n).
	idx := strings.IndexByte(rendered, '\n')
	if idx < 0 {
		return rendered // no newline — shouldn't happen for a bordered box
	}
	return topLine + rendered[idx:]
}

// detailPanelTitle returns a short label for the detail panel's border title
// given the current tab and selected item.
func (m model) detailPanelTitle() string {
	switch m.tab {
	case tabTags:
		if m.pane == paneDetail {
			if t := m.currentTodo(); t != nil {
				return t.Title
			}
		}
		tags := m.getFilteredTagsForTab()
		if m.tagTabCursor < len(tags) {
			tag := tags[m.tagTabCursor]
			if tag == untaggedKey {
				return tr("(untagged)")
			}
			return "#" + tag
		}
		return tr("Tag")
	case tabStats:
		return m.statsPanelTitle()
	default:
		if t := m.currentTodo(); t != nil {
			// A drilled-in subtask is not a top-level task; prefix a chevron
			// so the border makes clear you're inside a subtask, not viewing
			// the parent. withBorderTitle truncates the title, not the marker.
			if len(m.detailStack) > 0 {
				return "↳ " + t.Title
			}
			return t.Title
		}
		return tr("Detail")
	}
}

// listPanelTitle names the primary content box without merely repeating the
// selected tab. Context-sensitive variants make the border explain what is in
// the pane (active work versus history, for example).
func (m model) listPanelTitle() string {
	switch m.tab {
	case tabTasks:
		title := tr("Overview")
		if m.showHistory {
			title = tr("History")
		}

		total := m.visibleActiveLen()
		if m.showHistory {
			total = len(m.completedTodos())
		}
		if pos := listPosLabel(m.cursor, total); pos != "" {
			title += " [" + pos + "]"
		}
		return title + " [" + tr("sort:") + " " + m.sortLabel() + "]"
	case tabTags:
		return tr("Overview")
	case tabBoard:
		title := tr("Workflow")
		if cols := m.boardColumns(); len(cols) > 0 {
			start, count, _ := boardWindow(len(cols), m.board.colOffset, m.termWidth-8)
			if count > 0 && count < len(cols) {
				// Which slice of the board this is, and that ←/→ reaches the
				// rest. Without it a scrolled board just looks like a board
				// that lost its columns.
				title += fmt.Sprintf("  ‹ %d–%d/%d ›", start+1, start+count, len(cols))
			}
		}
		return title
	case tabStats:
		return tr("Summary")
	case tabSettings:
		return tr("Preferences")
	}
	return tr("Overview")
}

func projectTimelineTitle(project string) string {
	if project == "" {
		return tr("Timeline")
	}
	return tr("Timeline") + " · " + project
}

func projectTasksTitle(project string) string {
	if project == "" {
		return tr("Overview")
	}
	return tr("Overview") + " · @" + project
}

// ── Top-level View ────────────────────────────────────────────────────────────

func (m model) View() string {
	defer m.crashGuard("view", nil)
	// One trace line per frame, pairing this render with the Update that
	// produced it (see trace.go). Nil channel = tracing off = one branch.
	if traceCh != nil {
		t0 := time.Now()
		defer func() { traceFrame(lastUpdateKind, lastUpdate, time.Since(t0)) }()
	}
	m.ensureCache()
	if m.mode == modeHelp {
		return m.renderHelpFullscreen()
	}
	if m.mode == modeExplain {
		return m.renderExplainFullscreen()
	}

	out := getBuilder()
	defer putBuilder(out)

	w := m.termWidth - 6

	// ── HEADER ───────────────────────────────────────────────────────────
	// One bare "?" is everything the header says about getting unstuck. It is
	// the universal key for it, the overlay it opens lists ctrl+k among the
	// shortcuts, and the palette can find the overlay back — so one character
	// reaches the whole app, and the columns a sentence would cost go to the
	// tab labels instead.
	shortcutHint := helpStyle.Render("?")
	title := titleStyle.Render("taskr")
	// Right margin of the line, then the blank columns the hint is held off the
	// tab bar by. The bar is budgeted against both, so a bar that exactly fills
	// its budget still leaves the gap standing rather than costing the hint.
	budget := m.termWidth - 4
	avail := budget - ansi.StringWidth(title) - 2 - ansi.StringWidth(shortcutHint) - headerHintGap
	tabsStr := title + "  " + m.renderTabs(avail)
	padW := budget - ansi.StringWidth(tabsStr) - ansi.StringWidth(shortcutHint)
	if padW < headerHintGap {
		// A window too narrow for both. The tabs are the navigation and the
		// hint is a courtesy, so the hint goes — whole, rather than as the
		// fragment the line's truncate would otherwise leave standing.
		shortcutHint, padW = "", 1
	}
	out.WriteString(ansi.Truncate(tabsStr+strings.Repeat(" ", padW)+shortcutHint, m.termWidth-2, "") + "\n")
	// One fixed status line replaces the old stack of banner rows, so filters
	// and toasts never reflow the list below (see renderStatusLine).
	out.WriteString(m.renderStatusLine() + "\n")

	// ── FOOTER ───────────────────────────────────────────────────────────
	footerContent := m.buildFooterContent(w)
	footerLines := 0
	if footerContent != "" {
		footerLines = strings.Count(footerContent, "\n") + 1
	}

	// ── DETAIL (with caching) ────────────────────────────────────────────
	var detailContent string
	detailLineCount := 0
	showDetail := m.mode == modeNormal
	// For tabs that open the detail on enter / close on esc, the detail
	// panel is hidden until the user explicitly opens it. In side-by-side
	// mode the Tasks detail renders inside buildListContent's right column
	// instead of as a stacked panel.
	switch m.tab {
	case tabTasks:
		showDetail = showDetail && m.pane == paneDetail && !m.sideBySide()
	case tabTags:
		showDetail = showDetail && !m.sideBySide()
	case tabProjects:
		// When drilled into a project, the right column of buildProjectDrillContent
		// handles both browsing (Gantt) and the open-task case (task detail), so no
		// stacked panel is needed. Outside drill mode, a stacked panel is shown when
		// the user has pressed Enter (pane == paneDetail).
		showDetail = showDetail && m.pane == paneDetail && !m.projectTaskMode
	}

	if showDetail {
		switch {
		case m.tab == tabSettings, m.tab == tabBoard:
			detailContent = "" // settings and board tabs have no detail pane
		case m.tab == tabTags || m.tab == tabStats:
			detailContent = m.buildDetailContent()
		default:
			detailContent = m.getCachedDetailContent()
		}

		if detailContent != "" {
			// The stacked detail only exists while it owns keystrokes on the
			// enter-to-open tabs; the always-on previews (Tags/Stats) never do
			// — except a drilled-into tag, whose task list the cursor is in.
			focused := m.pane == paneDetail || (m.tab == tabTags && m.tagTaskMode)
			dst := detailPanelStyle
			if focused {
				dst = detailPanelFocusedStyle
			}
			// Clip to the panel's inner width first (w covers the two padding
			// columns). lipgloss cannot break a long unbroken token, so an
			// unclipped line pushes the whole box past the terminal edge on a
			// narrow window — every other pane clips for the same reason.
			detailBody := strings.Split(m.applyDetailScroll(detailContent), "\n")
			truncateLines(detailBody, w-2)
			detailContent = dst.Width(w).Render(strings.Join(detailBody, "\n"))
			detailContent = withBorderTitle(detailContent, m.detailPanelTitle(), w, focused)
			detailSplit := strings.Split(detailContent, "\n")
			for len(detailSplit) > 0 && strings.TrimSpace(detailSplit[len(detailSplit)-1]) == "" {
				detailSplit = detailSplit[:len(detailSplit)-1]
			}
			detailContent = strings.Join(detailSplit, "\n")
			detailLineCount = len(detailSplit)
		}
	}

	// ── LAYOUT ───────────────────────────────────────────────────────────
	li := computeLayout(layoutInput{
		termW:       m.termWidth,
		termH:       m.termHeight,
		mode:        m.mode,
		tab:         m.tab,
		detailLines: detailLineCount,
	})

	// ── LIST ─────────────────────────────────────────────────────────────
	target := m.termHeight
	availableForList := target - li.headerH - detailLineCount - footerLines
	if availableForList < minListHeight {
		availableForList = minListHeight
	}
	listContent := m.buildListContent(w, availableForList)
	listSplit := strings.Split(listContent, "\n")
	for len(listSplit) > 0 && strings.TrimSpace(listSplit[len(listSplit)-1]) == "" {
		listSplit = listSplit[:len(listSplit)-1]
	}

	// ── ASSEMBLE ─────────────────────────────────────────────────────────
	// Remove from second-to-last so the bottom border is always preserved.
	for len(listSplit) > availableForList {
		n := len(listSplit)
		listSplit = append(listSplit[:n-2], listSplit[n-1:]...)
	}
	for len(listSplit) < availableForList {
		listSplit = append(listSplit, "")
	}
	for _, line := range listSplit {
		out.WriteString(line + "\n")
	}
	if detailContent != "" {
		out.WriteString(detailContent + "\n")
	}
	if footerContent != "" {
		out.WriteString(footerContent)
	}
	result := out.String()
	resultLines := strings.Split(result, "\n")
	for len(resultLines) < target {
		resultLines = append(resultLines, "")
	}
	if len(resultLines) > target {
		resultLines = resultLines[:target]
	}

	for i, line := range resultLines {
		resultLines[i] = " " + line
	}
	return strings.Join(resultLines, "\n")

}

// ── Status line ────────────────────────────────────────────────────────────────

// renderStatusLine builds the single fixed header status line under the tab
// bar: filter chips on the left, the Tags-tab sort label and sync-health glyph
// on the right. The Tasks-tab sort label lives beside its cursor/total counter
// in the Overview or History panel title. A toast (m.err) overlays the whole line for its lifetime
// instead of claiming its own row, so filters and toasts coming and going never
// reflow the list below.
func (m model) renderStatusLine() string {
	width := m.termWidth - 2
	if width < 1 {
		width = 1
	}
	if m.err != "" {
		style := toastErrorStyle
		switch m.errKind {
		case toastSuccess:
			style = toastSuccessStyle
		case toastInfo:
			style = toastInfoStyle
		}
		return ansi.Truncate(style.Render(m.err), width, "")
	}

	var chips []string
	if m.focusFilter {
		chips = append(chips, focusChipStyle.Render(tr("FOCUS")))
	}
	if m.searchQuery != "" {
		label := m.searchQuery
		if label == untaggedKey {
			label = tr("(untagged)")
		}
		chips = append(chips, searchChipStyle.Render("/"+label))
	}
	if m.tab == tabTags && m.tagTabSearchQuery != "" {
		chips = append(chips, searchChipStyle.Render("/"+m.tagTabSearchQuery))
	}
	left := strings.Join(chips, " ")

	var right []string
	if m.tab == tabTags {
		right = append(right, statusSortStyle.Render(tr("sort:")+" "+m.tagSortLabel()))
	}
	if g := m.syncGlyph(); g != "" {
		right = append(right, g)
	}

	return statusLineJoin(left, strings.Join(right, "  "), width)
}

// statusLineJoin left-aligns left, right-aligns right, and fills the gap so the
// result is exactly width display cells. When both can't fit, the left chips
// win (the filter you just toggled is the more urgent cue) and the line is
// truncated.
func statusLineJoin(left, right string, width int) string {
	if right == "" {
		return ansi.Truncate(left, width, "")
	}
	lw := ansi.StringWidth(left)
	rw := ansi.StringWidth(right)
	if lw+1+rw > width {
		return ansi.Truncate(left, width, "")
	}
	return left + strings.Repeat(" ", width-lw-rw) + right
}

// sortLabel names the ordering currently applied to the visible task list.
// History mode has its own two sorts.
func (m model) sortLabel() string {
	if m.showHistory {
		if m.historySort == historySortAlpha {
			return tr("alpha")
		}
		return tr("completed")
	}
	switch m.taskSort {
	case taskSortDueDate:
		return tr("due")
	case taskSortSize:
		return tr("size")
	default:
		return tr("score")
	}
}

// tagSortLabel names the ordering currently applied to the Tags-tab list.
func (m model) tagSortLabel() string {
	switch m.tagSort {
	case tagSortCount:
		return tr("count")
	case tagSortProgress:
		return tr("progress")
	case tagSortRecent:
		return tr("recent")
	default:
		return tr("alpha")
	}
}

// syncGlyph reports background-sync health for the status line: a red mark
// after a failure, and nothing otherwise.
//
// Healthy sync says nothing on purpose. It used to show a dim ✓, which spent
// the status line's best corner on "there is nothing wrong" — in a symbol with
// nowhere to look it up, so the one question it reliably provoked was what it
// meant. The steady-state answer belongs in Settings, which carries it in
// words ("Last sync: sent 3, received 1") next to the rows that configure it,
// and which Init populates with a launch sync.
//
// The failure keeps its place here: sync failing means this device is drifting
// away from the others, which is the one sync fact a user must not have to go
// looking for. So the corner speaks only when something is wrong, and what it
// says carries the word "sync".
func (m model) syncGlyph() string {
	if !m.autoSync || !m.lastSyncFailed {
		return ""
	}
	return syncFailStyle.Render(tr("✕ sync"))
}

// ── Detail scroll ────────────────────────────────────────────────────────────

func (m model) applyDetailScroll(content string) string {
	maxVisible := m.termHeight*detailMaxHeightPct/100 - 2
	if maxVisible < 3 {
		maxVisible = 3
	}
	return m.applyDetailScrollN(content, maxVisible)
}

// applyDetailScrollN is applyDetailScroll with an explicit viewport height —
// the side-by-side detail column scrolls within the full list height rather
// than the stacked panel's percentage cap.
func (m model) applyDetailScrollN(content string, maxVisible int) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) <= maxVisible {
		return strings.Join(lines, "\n")
	}

	cursorLine := m.estimateDetailCursorLine()
	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}

	// The stored offset is where the pane was; this moves it only as far as the
	// cursor demands, against the lines actually rendered rather than the
	// model's estimate of them.
	scrollStart := detailScrollWindow(m.detail.scroll, cursorLine, maxVisible, len(lines))
	// Within a margin of the top there is nothing to gain by hiding the first
	// rows behind a marker that costs one of them.
	if scrollStart <= detailScrollMargin {
		scrollStart = 0
	}
	end := scrollStart + maxVisible
	if end > len(lines) {
		end = len(lines)
	}

	visible := make([]string, end-scrollStart)
	copy(visible, lines[scrollStart:end])

	// The markers carry the count. A bare ellipsis said only "there is more",
	// which is the one thing the reader could already infer; what they cannot
	// see is whether one line is hidden or thirty, and that is what decides
	// whether scrolling is worth it. The list pane has said "[3/47]" in its
	// border all along — this is the detail pane's version of the same fact.
	if scrollStart > 0 {
		// The marker replaces the first visible line, so that line is hidden
		// too and counts toward the total above.
		visible[0] = dimStyle.Render(fmt.Sprintf(tr("  ↑ %d more"), scrollStart+1))
	}
	if end < len(lines) {
		visible[len(visible)-1] = dimStyle.Render(fmt.Sprintf(tr("  ↓ %d more"), len(lines)-end+1))
	}

	return strings.Join(visible, "\n")
}

// ── Footer builder ────────────────────────────────────────────────────────────

// buildFooterContent renders the footer for the current mode and clips it to
// the window. The clip lives here rather than in each branch because every
// footer variant has a minimum width of its own — the text input's box, the
// picker rows, a confirm sentence — and on a narrow window any of them would
// otherwise push past the terminal edge. View pads every line with one leading
// space, so the budget is one column less than the terminal.
func (m model) buildFooterContent(w int) string {
	out := m.footerContentFor(w)
	if out == "" || m.termWidth <= 1 {
		return out
	}
	lines := strings.Split(out, "\n")
	truncateLines(lines, m.termWidth-1)
	return strings.Join(lines, "\n")
}

func (m model) footerContentFor(w int) string {
	switch m.mode {
	case modeNormal:
		hints := m.renderKeyHints(w)
		if t := m.runningTask(); t != nil {
			elapsed := ""
			if e := t.RunningEntry(); e != nil {
				elapsed = formatDurationLive(time.Since(e.StartedAt))
			}
			timerLine := timerStyle.Render("    ◉ "+truncate(t.Title, w/2)) +
				normalStyle.Render(" · "+elapsed) +
				helpStyle.Render(tr(" · t to stop"))
			return ansi.Truncate(timerLine, w, "") + "\n" + hints
		}
		return hints
	case modeInput, modeEditComment, modeEditTag, modeEditTitle, modeEditDue,
		modeAddSubtask, modeEditSubtask,
		modeEditProjectInline, modeEditTimeEntry, modeAddTimeEntry,
		modeEditSyncURL, modeEditSyncToken,
		modeEditServerListen, modeEditServerToken, modeEditStages:
		field := inputStyle.Width(w).Render(m.textInput.View())
		if m.mode == modeInput && m.pane == paneList {
			// Quick-add: on a blank input show the syntax reference (the keywords
			// stay English in every language — parsing is locale-free — so only
			// the example words are translated); once typing, replace it with a
			// live preview of the parsed fields so a mistyped token is visible.
			// While the caret sits in a #tag / @project token, the completion
			// row is the useful feedback — the parse preview comes back the
			// moment the token is finished.
			if sigil, matches := m.completionMatches(); len(matches) > 0 {
				return field + "\n" + renderQuickAddSuggestions(sigil, matches, m.suggestIndex(len(matches)), w)
			}
			if strings.TrimSpace(m.textInput.Value()) == "" {
				return field + "\n" +
					helpStyle.Render("    "+truncate(quickAddHint(), w))
			}
			return field + "\n" + renderQuickAddPreview(m.textInput.Value(), w)
		}
		if m.mode == modeEditStages {
			// The last column holds the completed tasks whatever it is called,
			// so say which one that is — otherwise renaming it looks like it
			// might have added a fifth column, or lost the done cards.
			return field + "\n" + helpStyle.Render("    "+truncate(tr("Comma-separated column names · the last one holds completed tasks"), w))
		}
		// The single-line comment inputs get a ctrl+e escape hatch to compose
		// in $EDITOR; advertise it under the field.
		switch {
		case m.mode == modeEditComment,
			m.mode == modeInput && m.pane != paneList && m.detail.field == fieldComments:
			return field + "\n" + helpStyle.Render("    "+tr("ctrl+e  edit in $EDITOR"))
		}
		return field
	case modeIdlePrompt, modeConfirmUpdate:
		// Same 4-space gutter as the key hints these prompts replace, so a
		// prompt appears where the line it stands in for was.
		return calTodayStyle.Render("    " + m.confirmMsg)
	case modeSearch:
		field := searchStyle.Width(w).Render(m.searchInput.View())
		// Same two-stage footer as quick-add: while the caret sits in a #tag /
		// @project token the completion row is the useful feedback, and the
		// parse preview comes back the moment the token is finished.
		if sigil, matches := m.completionMatches(); len(matches) > 0 {
			return field + "\n" + renderQuickAddSuggestions(sigil, matches, m.suggestIndex(len(matches)), w)
		}
		// Both are gated on the tab actually running the token grammar — on
		// Projects the query is a plain name substring, and a chip preview
		// would describe a filter that is not the one in effect.
		if m.searchUsesTokenGrammar() {
			if val := m.searchInput.Value(); strings.TrimSpace(val) != "" {
				return field + "\n" + renderSearchPreview(val, w)
			}
		}
		return field
	case modeSearchTagTab:
		return searchStyle.Width(w).Render(m.tagTabSearchInput.View())
	case modeSearchDep:
		b := getBuilder()
		defer putBuilder(b)
		b.WriteString(searchStyle.Width(w).Render(m.depSearchInput.View()))
		results := m.depSearchResults()
		shown := 0
		start, hasAbove, hasBelow := pickerWindowStart(m.depSearch.cursor, len(results), maxDepSearchResults)
		for slot := 0; slot < maxDepSearchResults; slot++ {
			idx := start + slot
			switch {
			case hasAbove && slot == 0:
				b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more above", start+1)))
				shown++
			case hasBelow && slot == maxDepSearchResults-1:
				below := len(results) - (start + slot)
				b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more below", below)))
				shown++
			case idx < len(results):
				r := results[idx]
				if idx == m.depSearch.cursor {
					b.WriteString("\n" + selectedStyle.Render(cursorGap+cursorMark+r.Title))
				} else {
					b.WriteString("\n" + normalStyle.Render("    "+r.Title))
				}
				shown++
			default:
				b.WriteString("\n")
				shown++
			}
		}
		return b.String()
	case modeSearchTag:
		b := getBuilder()
		defer putBuilder(b)
		b.WriteString(searchStyle.Width(w).Render(m.tagSearchInput.View()))
		results := m.tagSearchResults()
		shown := 0
		if len(results) == 0 && m.tagSearch.query != "" {
			b.WriteString("\n" + dimStyle.Render(cursorGap+cursorMark+tr("create new tag: ")) + tagStyle.Render(m.tagSearch.query))
			shown++
		} else {
			start, hasAbove, hasBelow := pickerWindowStart(m.tagSearch.cursor, len(results), maxTagSearchResults)
			for slot := 0; slot < maxTagSearchResults; slot++ {
				idx := start + slot
				switch {
				case hasAbove && slot == 0:
					b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more above", start+1)))
					shown++
				case hasBelow && slot == maxTagSearchResults-1:
					below := len(results) - (start + slot)
					b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more below", below)))
					shown++
				case idx < len(results):
					r := results[idx]
					if idx == m.tagSearch.cursor {
						b.WriteString("\n" + selectedStyle.Render(cursorGap+cursorMark+"#"+r))
					} else {
						b.WriteString("\n" + normalStyle.Render("    #"+r))
					}
					shown++
				default:
					b.WriteString("\n")
					shown++
				}
			}
		}
		for shown < maxTagSearchResults {
			b.WriteString("\n")
			shown++
		}
		return b.String()
	case modeSearchProject:
		b := getBuilder()
		defer putBuilder(b)
		b.WriteString(searchStyle.Width(w).Render(m.projSearchInput.View()))
		results := m.projSearchResults()
		shown := 0
		if len(results) == 0 && m.projSearch.query != "" {
			b.WriteString("\n" + dimStyle.Render(cursorGap+cursorMark+tr("create new project: ")) + selectedStyle.Render(m.projSearch.query))
			shown++
		} else {
			start, hasAbove, hasBelow := pickerWindowStart(m.projSearch.cursor, len(results), maxProjSearchResults)
			for slot := 0; slot < maxProjSearchResults; slot++ {
				idx := start + slot
				switch {
				case hasAbove && slot == 0:
					b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more above", start+1)))
					shown++
				case hasBelow && slot == maxProjSearchResults-1:
					below := len(results) - (start + slot)
					b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more below", below)))
					shown++
				case idx < len(results):
					r := results[idx]
					if idx == m.projSearch.cursor {
						b.WriteString("\n" + selectedStyle.Render(cursorGap+cursorMark+r))
					} else {
						b.WriteString("\n" + normalStyle.Render("    "+r))
					}
					shown++
				default:
					b.WriteString("\n")
					shown++
				}
			}
		}
		for shown < maxProjSearchResults {
			b.WriteString("\n")
			shown++
		}
		return b.String()
	case modePalette:
		return m.renderPalette(w)
	case modeConfirm:
		return confirmStyle.Render("    " + m.confirmMsg)
	}
	return ""
}

// renderPalette draws the command palette: the query field, then the matching
// commands with the key each one presses and the tab it belongs to. Sized to
// maxPaletteResults so the block above it never reflows as the list narrows.
func (m model) renderPalette(w int) string {
	b := getBuilder()
	defer putBuilder(b)
	b.WriteString(searchStyle.Width(w).Render(m.paletteInput.View()))

	results := paletteResults(m.paletteInput.Value())
	sel := m.paletteSelection(len(results))
	if len(results) == 0 {
		b.WriteString("\n" + dimStyle.Render("    "+tr("No command matches that.")))
		for i := 1; i < maxPaletteResults; i++ {
			b.WriteString("\n")
		}
		return b.String()
	}
	start, hasAbove, hasBelow := pickerWindowStart(sel, len(results), maxPaletteResults)
	for slot := 0; slot < maxPaletteResults; slot++ {
		idx := start + slot
		switch {
		case hasAbove && slot == 0:
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more above", start+1)))
		case hasBelow && slot == maxPaletteResults-1:
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d more below", len(results)-(start+slot))))
		case idx < len(results):
			c := results[idx]
			// Right-align the key + section so the labels form a readable
			// column on the left, the way the list tabs do.
			meta := c.key
			if c.section != "" {
				meta += "  " + c.section
			}
			label := c.label
			// The row occupies the field's own text columns — a 4-cell gutter
			// for the cursor (margin 2 + border + padding), then the field's
			// inner width — so the palette reads as one block with the box
			// above it rather than a list shifted out from under it.
			inner := w - 2
			gap := inner - len([]rune(label)) - len([]rune(meta))
			if gap < 2 {
				gap = 2
				label = truncate(label, inner-2-len([]rune(meta)))
			}
			row := label + strings.Repeat(" ", gap) + meta
			if idx == sel {
				b.WriteString("\n" + selectedStyle.Render(cursorGap+cursorMark+row))
			} else {
				b.WriteString("\n" + normalStyle.Render("    "+row))
			}
		default:
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ── Key hints ─────────────────────────────────────────────────────────────────

// hintLabelOverrides names what the toggle keys would do to the currently
// selected task, rather than what they do in general. A footer that says
// "t track" over a task that is already being tracked is not a hint, it is a
// wrong answer; the keys toggle, so the label has to as well.
func (m model) hintLabelOverrides() map[string]string {
	t := m.currentTodo()
	if t == nil {
		return nil
	}
	var over map[string]string
	set := func(action, label string) {
		if over == nil {
			over = make(map[string]string, 2)
		}
		over[action] = label
	}
	if t.IsTimerRunning() {
		set("track", "stop")
	}
	if t.Status == todo.Done {
		set("done", "reopen")
	}
	return over
}

func (m model) renderKeyHints(w int) string {
	// Both the hint line and the help overlay are generated from the keymap
	// registry (keymap.go), so they can't drift from each other or from
	// dispatch.
	ctx := m.currentKeyCtx()
	over := m.hintLabelOverrides()
	hints := hintString(ctx, false, over)
	// Prefer the full hint line; when it can't fit, fall back to the curated
	// short (primary-only) set instead of truncating mid-list — plain
	// truncation always cut the same trailing keys (e.g. / search on the Tasks
	// tab), hiding them at common terminal widths. hints is pre-Render plain
	// text, so rune length is the display width.
	if short := hintString(ctx, true, over); short != "" && len([]rune(hints)) > w {
		hints = short
	}
	// 4-space indent aligns the hint under the box's inner content (margin 2 +
	// border 1 + padding 1) — so it begins at the same column as the task rows.
	return helpStyle.Render("    " + truncate(hints, w))
}

// ── Detail content ────────────────────────────────────────────────────────────

func (m model) buildDetailContent() string {
	switch {
	// A task opened out of the tag drill takes the pane over — same as the
	// Projects tab, where the right column swaps the Gantt for the task
	// detail. Without this the pane would keep showing the tag summary while
	// the detail keyset was live.
	case m.tab == tabTags && m.pane == paneDetail:
		if t := m.currentTodo(); t != nil {
			return m.renderDetailPage1(t) + "\n" +
				m.renderDetailPage2(t) + "\n" +
				m.renderDetailPage3(t)
		}
		return strings.Join(m.buildTagDetailLines(), "\n")
	case m.tab == tabTags:
		lines := m.buildTagDetailLines()
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n")
	case m.tab == tabStats:
		return m.renderStatsDetail()
	default:
		t := m.currentTodo()
		if t == nil {
			return dimStyle.Render("  No task selected.")
		}
		// One continuous column: fields+tags, relations, comments. Sections
		// scroll as a single document; left/right jump between section heads.
		return m.renderDetailPage1(t) + "\n" +
			m.renderDetailPage2(t) + "\n" +
			m.renderDetailPage3(t)
	}
}

// ── List content builder ──────────────────────────────────────────────────────

func (m model) buildListContent(w, outerH int) string {
	if m.tab == tabProjects {
		return m.buildProjectListContent(w, outerH)
	}
	if m.tab == tabCalendar {
		return m.buildCalendarContent(w, outerH)
	}
	if m.tab == tabSettings {
		return m.buildSettingsContent(w, outerH)
	}
	if m.sideBySide() {
		return m.buildSideBySide(w, outerH)
	}

	innerH := panelContentHeight(outerH)
	rawList := m.buildListLines()
	for len(rawList) < innerH {
		rawList = append(rawList, "")
	}
	if len(rawList) > innerH {
		rawList = rawList[:innerH]
	}
	truncateLines(rawList, w-2)
	panel := listPanelStyle.Width(w).Render(strings.Join(rawList, "\n"))
	return withBorderTitle(panel, m.listPanelTitle(), w, false)
}

// buildSideBySide renders the side-by-side list tabs (Tasks/Tags) as
// two columns: the list keeps full height on the left and the detail pane is an
// always-on preview of the cursor item on the right. Mirrors buildCalendarContent's
// approach — each
// column is rendered through a model copy whose termWidth is the column's
// share, so the existing width math (list columns, tag fitting, the no-wrap
// contract, the detail's own two-column threshold) applies per column
// unchanged. The focused pane carries the accent border.
func (m model) buildSideBySide(w, outerH int) string {
	innerH := panelContentHeight(outerH)
	detailW := w * sideDetailColPct / 100
	if detailW < sideDetailColMin {
		detailW = sideDetailColMin
	}
	if detailW > sideDetailColMax {
		detailW = sideDetailColMax
	}
	listW := w - detailW - 4
	if listW < minInnerWidth {
		listW = minInnerWidth
	}

	lm := m
	lm.termWidth = listW + 6 // View hands buildListContent w = termWidth-6
	// The narrowed copy is only for responsive column sizing. If the detail
	// column owns focus, leaving paneDetail set makes the list-height helpers
	// interpret this now-narrow model as the stacked layout and reserve rows
	// for a second detail panel below the list. The real detail is already in
	// the right column, so size the list copy as the list pane.
	lm.pane = paneList
	listLines := lm.buildListLines()

	dm := m
	dm.termWidth = detailW + 6
	var detailLines []string
	if m.tab == tabTasks && m.currentTodo() == nil {
		detailLines = []string{"", dimStyle.Render(tr("  No task selected."))}
	} else {
		detailLines = strings.Split(dm.applyDetailScrollN(dm.buildDetailContent(), innerH), "\n")
	}

	fitLines := func(lines []string, h, contentW int) []string {
		if len(lines) > h {
			lines = lines[:h]
		}
		for len(lines) < h {
			lines = append(lines, "")
		}
		truncateLines(lines, contentW)
		return lines
	}
	listLines = fitLines(listLines, innerH, listW-2)
	detailLines = fitLines(detailLines, innerH, detailW-2)

	listStyle, detailStyle := listPanelFocusedStyle, detailPanelStyle
	// Drilling into a tag moves the cursor into the right column, so the accent
	// border has to move with it.
	detailFocused := m.pane == paneDetail || (m.tab == tabTags && m.tagTaskMode)
	if detailFocused {
		listStyle, detailStyle = listPanelStyle, detailPanelFocusedStyle
	}
	listPanel := listStyle.Width(listW).Render(strings.Join(listLines, "\n"))
	detailPanel := detailStyle.Width(detailW).Render(strings.Join(detailLines, "\n"))
	listPanel = withBorderTitle(listPanel, m.listPanelTitle(), listW, !detailFocused)
	detailPanel = withBorderTitle(detailPanel, m.detailPanelTitle(), detailW, detailFocused)
	// Only the order changes with the placement: both columns are already
	// sized and clipped, so mirroring the layout is one swap rather than a
	// second set of width math that could drift from this one.
	if m.detailPos == detailLeft {
		return lipgloss.JoinHorizontal(lipgloss.Top, detailPanel, listPanel)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, listPanel, detailPanel)
}

func (m model) buildProjectListContent(w, listH int) string {
	projects := m.allProjectsForList()
	if len(projects) == 0 {
		empty := normalStyle.Render(tr("  No projects yet. Add a project to a task first.")) + "\n" +
			dimStyle.Render(tr("  A project groups its tasks into a timeline on this tab."))
		if m.searchQuery != "" {
			empty = normalStyle.Render(tr("  No projects match your search."))
		}
		innerH := panelContentHeight(listH)
		emptyLines := strings.Split(empty, "\n")
		for len(emptyLines) < innerH {
			emptyLines = append(emptyLines, "")
		}
		if len(emptyLines) > innerH {
			emptyLines = emptyLines[:innerH]
		}
		panel := listPanelStyle.Width(w).Render(strings.Join(emptyLines, "\n"))
		return withBorderTitle(panel, tr("Overview"), w, false)
	}

	// ── Drilled-in view: task list (left) + right column (right) ────────────
	// When the user has pressed Enter to drill into a project, render the
	// same list+detail side-by-side contract as the Tasks/Tags tabs:
	// left column = task rows (same renderer as the Tasks tab), right column =
	// Gantt chart when browsing (pane == paneList) or the task detail when the
	// user has pressed Enter on a task (pane == paneDetail).
	if m.projectTaskMode {
		return m.buildProjectDrillContent(projects, w, listH)
	}

	// ── Project list + Gantt preview (stacked, original layout) ──────────────
	projMaxH := listH / 3
	if projMaxH < minListPanelLines {
		projMaxH = minListPanelLines
	}
	projLines := strings.Split(m.renderProjectListContent(projects), "\n")
	projEnd := len(projLines)
	for projEnd > 0 && strings.TrimSpace(projLines[projEnd-1]) == "" {
		projEnd--
	}
	projLines = projLines[:projEnd]
	if len(projLines) > projMaxH {
		projLines = projLines[:projMaxH]
	}
	for len(projLines) < projMaxH {
		projLines = append(projLines, "")
	}
	truncateLines(projLines, w-2)
	projRendered := listPanelStyle.Width(w).Render(strings.Join(projLines, "\n"))
	projRendered = withBorderTitle(projRendered, tr("Overview"), w, false)

	projRenderedLines := strings.Split(projRendered, "\n")
	ganttOuterH := listH - len(projRenderedLines)
	if ganttOuterH < minListPanelLines+3 {
		ganttOuterH = minListPanelLines + 3
	}
	ganttInnerH := panelContentHeight(ganttOuterH)

	var ganttLines []string
	if m.projectCursor < len(projects) {
		tasks := m.getProjectTasks(projects[m.projectCursor])
		ganttContent := m.renderGantt(tasks)
		ganttLines = strings.Split(ganttContent, "\n")
		ganttEnd := len(ganttLines)
		for ganttEnd > 0 && strings.TrimSpace(ganttLines[ganttEnd-1]) == "" {
			ganttEnd--
		}
		ganttLines = ganttLines[:ganttEnd]
	}
	if len(ganttLines) > ganttInnerH {
		ganttLines = ganttLines[:ganttInnerH]
	}
	for len(ganttLines) < ganttInnerH {
		ganttLines = append(ganttLines, "")
	}
	truncateLines(ganttLines, w-2)
	ganttRendered := listPanelStyle.Width(w).Render(strings.Join(ganttLines, "\n"))
	project := ""
	if m.projectCursor < len(projects) {
		project = projects[m.projectCursor]
	}
	ganttRendered = withBorderTitle(ganttRendered, projectTimelineTitle(project), w, false)

	b := getBuilder()
	defer putBuilder(b)
	b.WriteString(projRendered)
	b.WriteString("\n")
	b.WriteString(ganttRendered)
	return b.String()
}

// buildProjectDrillContent renders the drilled-in project view as two columns:
// the task list (left, using the same row renderer as the Tasks tab) and,
// in the right column, either the timeline strip (when browsing the list,
// pane == paneList; see renderGanttStrip — bars only, aligned row-for-row with
// the list) or the task detail (when the user has pressed Enter on a task,
// pane == paneDetail). Mirrors buildSideBySide's contract — each column
// is rendered through a model copy whose termWidth is the column's share, and
// the focused pane carries the accent border.
// buildProjectDrillNarrow is the single-column drilled-in project view for
// windows too small to carry the Gantt beside the task list: the list (or the
// open task's detail) takes the whole width, with no floor to overflow past.
func (m model) buildProjectDrillNarrow(projects []string, w, innerH int) string {
	if w < 0 {
		w = 0 // borders only; a floor here would push the box past the window
	}
	cm := m
	cm.termWidth = w + 6 // the column renderers take termWidth-6 as their width
	var lines []string
	title := projectTasksTitle("")
	if m.projectCursor < len(projects) {
		title = projectTasksTitle(projects[m.projectCursor])
	}
	if m.pane == paneDetail {
		lines = strings.Split(cm.applyDetailScrollN(cm.buildDetailContent(), innerH), "\n")
		title = cm.detailPanelTitle()
	} else {
		var tasks []todo.Todo
		if m.projectCursor < len(projects) {
			tasks = m.getProjectTasks(projects[m.projectCursor])
		}
		lines = cm.renderProjectDrillTaskList(tasks)
	}
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	truncateLines(lines, w-2)
	panel := listPanelFocusedStyle.Width(w).Render(strings.Join(lines, "\n"))
	return withBorderTitle(panel, title, w, true)
}

func (m model) buildProjectDrillContent(projects []string, w, outerH int) string {
	innerH := panelContentHeight(outerH)
	if w < projDrillMinWidth {
		return m.buildProjectDrillNarrow(projects, w, innerH)
	}

	// Column widths: Gantt needs a reasonable minimum to be legible; the task
	// list takes the remainder. Mirror the sideDetailCol constants but keep the
	// Gantt wider since the bar chart needs more horizontal room than text detail.
	ganttW := w * sideDetailColPct / 100
	if ganttW < sideDetailColMin {
		ganttW = sideDetailColMin
	}
	if ganttW > sideDetailColMax {
		ganttW = sideDetailColMax
	}
	listW := w - ganttW - 4 // 4 = inter-panel gap absorbed by the border chars
	if listW < minInnerWidth {
		listW = minInnerWidth
	}

	// Task list — rendered through a model copy sized to the left column so
	// taskListCols and renderTaskLineWithSet see the correct terminal width.
	lm := m
	lm.termWidth = listW + 6 // View hands buildListContent w = termWidth-6
	var tasks []todo.Todo
	if m.projectCursor < len(projects) {
		tasks = m.getProjectTasks(projects[m.projectCursor])
	}
	listLines := lm.renderProjectDrillTaskList(tasks)

	// Right column: task detail when the user has opened a task (pane ==
	// paneDetail), Gantt chart otherwise (pane == paneList, always-on preview).
	dm := m
	dm.termWidth = ganttW + 6
	var rightLines []string
	if m.pane == paneDetail {
		if m.currentTodo() != nil {
			rightLines = strings.Split(dm.applyDetailScrollN(dm.buildDetailContent(), innerH), "\n")
		} else {
			rightLines = []string{"", dimStyle.Render(tr("  No task selected."))}
		}
	} else {
		// Beside the list the timeline is a strip: no label column, and windowed
		// to the rows the list is showing, so the two columns read as one table
		// split by a border instead of two panes listing the same tasks.
		rightLines = dm.renderGanttStrip(tasks, ganttW-2, m.listOffset, m.projectDrillTaskVisibleRows())
	}

	fitLines := func(lines []string, h, contentW int) []string {
		if len(lines) > h {
			lines = lines[:h]
		}
		for len(lines) < h {
			lines = append(lines, "")
		}
		truncateLines(lines, contentW)
		return lines
	}
	listLines = fitLines(listLines, innerH, listW-2)
	rightLines = fitLines(rightLines, innerH, ganttW-2)

	// Focused-pane accent border: list gets the accent when browsing; the right
	// column gets it when the user is viewing a task's detail.
	listStyle := listPanelFocusedStyle
	ganttStyle := detailPanelStyle
	if m.pane == paneDetail {
		listStyle = listPanelStyle
		ganttStyle = detailPanelFocusedStyle
	}

	listPanel := listStyle.Width(listW).Render(strings.Join(listLines, "\n"))
	rightPanel := ganttStyle.Width(ganttW).Render(strings.Join(rightLines, "\n"))
	project := ""
	if m.projectCursor < len(projects) {
		project = projects[m.projectCursor]
	}
	listPanel = withBorderTitle(listPanel, projectTasksTitle(project), listW, m.pane == paneList)
	// The right border names either the opened task or the selected project's
	// timeline, matching the contextual title on the left task pane.
	if m.pane == paneDetail {
		rightPanel = withBorderTitle(rightPanel, m.detailPanelTitle(), ganttW, true)
	} else {
		rightPanel = withBorderTitle(rightPanel, projectTimelineTitle(project), ganttW, false)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, listPanel, rightPanel)
}

// ── Help ──────────────────────────────────────────────────────────────────────

// helpChromeLines is the number of fixed rows renderHelpFullscreen spends on
// the title block and footer around the scrolling body. Kept in sync with the
// literal writes below so helpViewportH can size the body to never push the
// footer off-screen (the final pad/truncate then lands the footer at the
// bottom exactly).
const helpChromeLines = 7

// helpBodyLines renders the scrollable body of the help overlay — every key
// section plus the date-input reference — as one styled line per slice entry,
// with a blank line between sections. Title and footer are chrome and live in
// renderHelpFullscreen. Shared with the scroll clamp so both agree on length.
// helpSec is one titled block of the help overlay: a section name and its
// key/description rows.
type helpSec struct {
	title string
	keys  [][2]string
}

// filterHelpSections keeps the rows matching query — case-insensitively, in the
// key, the description or the section title, so both "x" and "delete" and
// "board" find something — and drops sections left with no rows. An empty query
// keeps everything.
func filterHelpSections(sections []helpSec, query string) []helpSec {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return sections
	}
	out := make([]helpSec, 0, len(sections))
	for _, sec := range sections {
		if strings.Contains(strings.ToLower(sec.title), q) {
			out = append(out, sec)
			continue
		}
		var kept [][2]string
		for _, kv := range sec.keys {
			if strings.Contains(strings.ToLower(kv[0]), q) || strings.Contains(strings.ToLower(kv[1]), q) {
				kept = append(kept, kv)
			}
		}
		if len(kept) > 0 {
			out = append(out, helpSec{sec.title, kept})
		}
	}
	return out
}

func (m model) helpBodyLines() []string {
	// Key sections are generated from the keymap registry (keymap.go), so the
	// help overlay can't drift from the footer hints or from dispatch.
	var sections []helpSec
	for _, title := range helpSectionOrder {
		var keys [][2]string
		for i := range keymap {
			if bd := &keymap[i]; bd.section == title {
				keys = append(keys, [2]string{effectiveKey(bd.action, bd.key), tr(bd.desc)})
			}
		}
		if len(keys) > 0 {
			sections = append(sections, helpSec{tr(title), keys})
		}
	}
	// Reference sections: the token grammars. They lived only in the README and
	// in the one-line hint under the input, which is gone the moment you need to
	// look something up. Keep in sync with parseQuickAdd (helpers.go) and
	// compileSearch — TestHelpDocumentsEveryToken asserts every token the
	// parsers accept appears here.
	//
	// Tokens are spelled in the active language (inputWord / inputToken,
	// lang_input.go) because that is what the parser now accepts there —
	// advertising `due:friday` on a Danish screen was the visible half of the
	// mismatch. English keeps parsing everywhere, so nothing here revokes it.
	sections = append(sections, helpSec{tr("Quick-add syntax"), [][2]string{
		{"#tag", tr("add a tag (existing tags are suggested; tab inserts)")},
		{"@project", tr("put it in a project")},
		{inputWord("due:") + inputToken("tomorrow"), tr("set a due date (see Date input below)")},
		{"p:" + inputWord("high"), fmt.Sprintf(tr("priority: %s / %s / %s (p:h, p:m, p:l)"),
			inputWord("high"), inputWord("medium"), inputWord("low"))},
		{"s:l", fmt.Sprintf(tr("size: s / m / l (also %s)"), inputWord("size:")+inputWord("large"))},
		{"r:" + inputWord("weekly"), fmt.Sprintf(tr("repeat: %s / %s / %s / %s / %s"),
			inputWord("daily"), inputWord("weekdays"), inputWord("weekly"),
			inputWord("monthly"), inputWord("yearly"))},
		{inputWord("dep:") + "^", fmt.Sprintf(tr("block on the last added task (or %s<id prefix>)"), inputWord("dep:"))},
	}})
	sections = append(sections, helpSec{tr("Search filters"), [][2]string{
		{"#tag", tr("only tasks carrying the tag")},
		{"@project", tr("only tasks in the project")},
		{"p:" + inputWord("high"), tr("only that priority")},
		{inputWord("due:") + "<" + strings.ToLower(localizedWeekday(time.Friday)), tr("due before a date (also >, <=, >= and an exact date)")},
		{inputWord("overdue"), tr("only overdue tasks")},
		{"grcrs", tr("anything else fuzzy-matches the title, or the notes as text")},
	}})

	// Reference section: the annotation glyphs a task row can carry. Not key
	// bindings, so like Date input it lives outside the keymap registry. Keep in
	// sync with renderTaskLineWithSet.
	sections = append(sections, helpSec{tr("Row symbols"), [][2]string{
		{"⧗", tr("timer running")},
		{"!", tr("high priority")},
		{"[~]", tr("blocked — waiting on an unfinished dependency (ST column)")},
		{"↥", tr("others depend on this — finishing it unblocks them")},
		{"↧", tr("blocked — waiting on an unfinished dependency")},
		{"↻", tr("recurring task")},
		{"(2/5)", tr("subtasks done / total")},
		{"+ / -", tr("subtasks collapsed / expanded")},
		{"↑", tr("score lifted by a subtask or by work waiting on it (detail pane)")},
	}})

	// Reference section: the status line. Everything here appears only in a
	// state that is not the default — the line is empty when there is nothing
	// to say — so each entry answers "why is that there?", which is the
	// question a symbol in the corner of the screen actually provokes.
	sections = append(sections, helpSec{tr("Status line"), [][2]string{
		{"✕ sync", tr("background sync is failing — Settings has the error")},
		{tr("FOCUS"), tr("the focus filter is on: today + overdue only")},
		{"/…", tr("a search filter is narrowing the list")},
	}})

	// Reference section: date-input grammar. Not key bindings, so it lives
	// outside the registry and is appended last.
	sections = append(sections, helpSec{tr("Date input"), [][2]string{
		{"dd-mm-yy", tr("exact date (e.g. 15-06-25)")},
		{inputWord("today"), tr("today's date")},
		{inputWord("tomorrow"), tr("tomorrow")},
		{inputWord("next week"), tr("7 days from now")},
		{inputWord("next month"), tr("1 month from now")},
		{strings.ToLower(localizedWeekday(time.Monday)) + ".." + strings.ToLower(localizedWeekday(time.Sunday)), tr("next occurrence of weekday")},
		{"+3d / +2w / +1m", tr("relative days/weeks/months")},
	}})

	// A filter narrows the rows and drops the sections left empty, so a query
	// answers "which key was it?" without scrolling the whole registry.
	sections = filterHelpSections(sections, m.helpFilter)
	if len(sections) == 0 {
		return []string{helpStyle.Render("  " + tr("No shortcut matches that."))}
	}

	var lines []string
	for _, section := range sections {
		lines = append(lines, detailLabelStyle.Render("  "+section.title))
		for _, kv := range section.keys {
			key := padRight(kv[0], 24)
			lines = append(lines,
				helpStyle.Render("  ")+
					selectedStyle.Render(key)+
					normalStyle.Render(kv[1]))
		}
		lines = append(lines, "")
	}
	return lines
}

// helpViewportH is how many body rows fit on screen once the title block and
// footer are reserved. Floored so tiny terminals still show something.
func (m model) helpViewportH() int {
	h := m.termHeight - helpChromeLines
	if h < 3 {
		h = 3
	}
	return h
}

// clampHelpScroll keeps a proposed scroll offset within [0, maxScroll] for a
// body of `total` lines shown through a `viewport`-row window.
func clampHelpScroll(scroll, total, viewport int) int {
	max := total - viewport
	if max < 0 {
		max = 0
	}
	if scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}

func (m model) renderHelpFullscreen() string {
	body := m.helpBodyLines()
	vh := m.helpViewportH()
	scroll := clampHelpScroll(m.helpScroll, len(body), vh)
	end := scroll + vh
	if end > len(body) {
		end = len(body)
	}

	b := getBuilder()
	defer putBuilder(b)

	b.WriteString("\n")
	title := titleStyle.Render("  " + tr("Keyboard shortcuts"))
	if m.helpFilter != "" || m.helpFiltering {
		caret := ""
		if m.helpFiltering {
			caret = "▌"
		}
		// A plain inline suffix, not searchStyle — that one draws the boxed
		// input used at the bottom of the screen.
		title += helpStyle.Render("   /") + selectedStyle.Render(m.helpFilter+caret)
	}
	b.WriteString(title + "\n")
	b.WriteString("\n")

	for _, line := range body[scroll:end] {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	hint := tr("/ filter  ·  ? or esc to close")
	if m.helpFiltering {
		hint = tr("type to filter  ·  enter keep  ·  esc clear")
	} else if m.helpFilter != "" {
		hint = tr("/ filter  ·  esc clear  ·  ? to close")
	}
	if len(body) > vh {
		var scrollHint string
		switch {
		case scroll > 0 && end < len(body):
			scrollHint = tr("↑/↓ scroll")
		case scroll > 0:
			scrollHint = tr("↑ scroll up")
		default:
			scrollHint = tr("↓ scroll down")
		}
		hint = scrollHint + "  ·  " + hint
	}
	b.WriteString(helpStyle.Render("  "+hint) + "\n")

	lines := strings.Split(b.String(), "\n")
	// The overlay is full-screen chrome of its own, so it clips itself — the
	// key column alone is wider than a narrow window.
	if m.termWidth > 0 {
		truncateLines(lines, m.termWidth)
	}
	// A terminal reporting height 0 (startup, a mid-drag resize) would make the
	// target negative and slice out of range below.
	target := m.termHeight - 1
	if target < 0 {
		target = 0
	}
	for len(lines) < target {
		lines = append(lines, "")
	}
	if len(lines) > target {
		lines = lines[:target]
	}

	return strings.Join(lines, "\n")
}

// ── Stats detail (activity heatmap) ──────────────────────────────────────────

// statsCell is one position in the activity histogram grid. gi is the gradient
// index, or -1 for dim/structural glyphs (baseline, separators, labels).
// bg is an optional second gradient index for half-block cells (▀ ▄), where
// the cell shows two stacked colours; -1 means no background colour.
type statsCell struct {
	ch rune
	gi int
	bg int
}

// statsBucket is one column of the Activity chart: the day (or Monday-started
// week) it covers, and how many top-level tasks were completed inside it.
type statsBucket struct {
	start time.Time
	count int
}

// statsActivity builds the Activity chart's buckets for the selected range,
// with the range's label, whether a bucket spans a week, and the total. Shared
// by the chart and by the panel title that names the range, so the two cannot
// disagree about what the count counts.
func (m model) statsActivity() (label string, buckets []statsBucket, weekly bool, total int) {
	now := m.frameTime
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch m.statsRange {
	case statsRange30Days:
		label = tr("Last 30 days")
		for d := 29; d >= 0; d-- {
			buckets = append(buckets, statsBucket{start: today.AddDate(0, 0, -d)})
		}
	case statsRange6Months:
		label = tr("Last 26 weeks")
		weekly = true
		curMon := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
		for w := 25; w >= 0; w-- {
			buckets = append(buckets, statsBucket{start: curMon.AddDate(0, 0, -w*7)})
		}
	default:
		label = tr("Last 7 days")
		for d := 6; d >= 0; d-- {
			buckets = append(buckets, statsBucket{start: today.AddDate(0, 0, -d)})
		}
	}

	first := buckets[0].start
	for _, t := range m.tasks {
		if t.Status != todo.Done || t.CompletedAt.IsZero() || t.ParentID != "" {
			continue
		}
		// CompletedAt comes back from storage in UTC; resolve the day in
		// the user's local zone (same zone as `today`/`first`), otherwise
		// a task closed in the morning local-time can land in tomorrow's
		// UTC date and get skipped as "after today".
		ca := t.CompletedAt.In(now.Location())
		d := time.Date(ca.Year(), ca.Month(), ca.Day(), 0, 0, 0, 0, now.Location())
		if d.Before(first) || d.After(today) {
			continue
		}
		idx := int(d.Sub(first).Hours()/24 + 0.5)
		if weekly {
			idx /= 7
		}
		if idx < 0 || idx >= len(buckets) {
			continue
		}
		buckets[idx].count++
		total++
	}
	return label, buckets, weekly, total
}

// statsPanelTitle is the Activity pane's border title, carrying the caption
// the pane used to spend a row on: the range, the completions in it, and what
// a block stands for. On the border it costs nothing, and the row it gives
// back is one more block a busy day can stack. Dropped back to front as the
// window narrows, so the least useful half goes first.
func (m model) statsPanelTitle() string {
	name := tr("Activity")
	label, _, _, total := m.statsActivity()
	scope := "[" + label + " · " + fmt.Sprintf(tr("%d done"), total) + "]"
	legend := "[" + tr("1 block = 1 completed task") + "]"
	budget := m.termWidth - 10 // withBorderTitle's own max: (termWidth-6) - 4
	for _, form := range []string{
		name + "  " + scope + "  " + legend,
		name + "  " + scope,
		scope, // narrow: the range outranks the pane's own name, which the tab already gives
		"[" + label + "]",
		name,
	} {
		if ansi.StringWidth(form) <= budget {
			return form
		}
	}
	return name // withBorderTitle truncates from here
}

// statsChartHeight is the tallest the Activity chart may draw, and so the
// per-bar cap: past it the bars halve to two tasks a row (▀/▄), and past twice
// it they cap with `+`. The caption moved to the border title, so the pane is
// nothing but chart and takes the panel's budget less the baseline and
// axis-label rows. Capped both ways: a floor so a short window still shows a
// chart, and a ceiling near the gradient's length, past which more blocks stop
// reading as taller and the stats list is the better use of the rows.
func (m model) statsChartHeight() int {
	h := m.termHeight*detailMaxHeightPct/100 - 2 - 8
	if h < statsChartMinH {
		return statsChartMinH
	}
	if h > statsChartMaxH {
		return statsChartMaxH
	}
	return h
}

// statsChartRows is the height the chart actually draws at: the budget, or the
// busiest bucket when that is shorter. Rows above the tallest bar are blank in
// a fixed-height chart — spending the pane's height on them costs the stats
// list rows to show nothing.
func statsChartRows(budget int, buckets []statsBucket) int {
	peak := 0
	for _, bk := range buckets {
		if bk.count > peak {
			peak = bk.count
		}
	}
	if peak >= budget {
		return budget
	}
	if peak < statsChartMinH {
		return statsChartMinH
	}
	return peak
}

func (m model) renderStatsDetail() string {
	b := getBuilder()
	defer putBuilder(b)

	innerW := m.termWidth - 8
	if innerW < 12 {
		innerW = 12
	}
	gradLen := len(statsGradient)

	// The range's name is on the border title now; the chart only needs its
	// shape.
	_, buckets, weekly, total := m.statsActivity()

	chartH := statsChartRows(m.statsChartHeight(), buckets)

	if total == 0 {
		// The range and the count are on the border; all this row has to say
		// is that there is nothing to draw.
		b.WriteString("  " + dimStyle.Render(tr("No completions in this range.")) + "\n")
		return b.String()
	}
	// Stretch the vertical scale when any bucket overflows chartH, so a busy
	// day collapses to half-height (with a ▄ cap for odd counts) instead of
	// capping immediately with a `+`. One step (×2) keeps the chart honest.
	blockScale := 1
	for _, bk := range buckets {
		if bk.count > chartH {
			blockScale = 2
			break
		}
	}
	// Pick a bar width that fills the available width (capped so a handful of
	// bars don't become absurdly fat), with a 1-column gap between bars.
	avail := innerW - 2
	n := len(buckets)
	maxBw := 3
	if m.statsRange == statsRange7Days {
		maxBw = 10 // wide enough to spell weekday names under each bar
	}
	bw := (avail - (n - 1)) / n
	if bw < 1 {
		bw = 1
	}
	if bw > maxBw {
		bw = maxBw
	}
	slot := bw + 1 // bar + gap
	if maxN := (avail + 1) / slot; n > maxN {
		buckets = buckets[n-maxN:] // most recent that fit
		n = len(buckets)
	}
	chartW := n*bw + (n - 1)
	leftMargin := 2 + (avail-chartW)/2 // centre the chart in the pane
	if leftMargin < 2 {
		leftMargin = 2
	}

	// Compose into a grid (gi: -1 = dim/structural, >=0 = gradient index), then
	// render each row grouping same-styled runs.
	rows := chartH + 2 // bars + baseline + labels
	grid := make([][]statsCell, rows)
	for r := range grid {
		grid[r] = make([]statsCell, chartW)
		for c := range grid[r] {
			grid[r][c] = statsCell{' ', -1, -1}
		}
	}
	barStart := func(k int) int { return k * slot }
	gradIdx := func(task int) int {
		if task >= gradLen {
			return gradLen - 1
		}
		return task
	}

	// At scale=1 each task is a full row (█). At scale=2 (any bucket > chartH)
	// the bars halve in row-height: each row holds two tasks via ▀/▄ half
	// blocks (fg=top task, bg=bottom task), so up to 2*chartH = 10 tasks fit
	// before `+` kicks in.
	for k := 0; k < n; k++ {
		start := barStart(k)
		cnt := buckets[k].count

		if blockScale == 1 {
			for r := 0; r < chartH && r < cnt; r++ {
				ch := '█'
				gi := gradIdx(r)
				if cnt > chartH && r == chartH-1 {
					ch = '+'
				}
				rowIdx := chartH - 1 - r
				for c := 0; c < bw; c++ {
					grid[rowIdx][start+c] = statsCell{ch, gi, -1}
				}
			}
			continue
		}

		overflow := cnt > 2*chartH
		for r := 0; r < chartH; r++ { // r = 0 is the bottom row
			bot := 2 * r   // bottom-half task index
			top := 2*r + 1 // top-half task index
			rowIdx := chartH - 1 - r
			var ch rune
			var gi int
			bg := -1
			switch {
			case overflow && r == chartH-1:
				ch = '+'
				gi = gradIdx(top)
			case cnt > top: // both halves present
				botGi := gradIdx(bot)
				topGi := gradIdx(top)
				if botGi == topGi {
					ch = '█'
					gi = topGi
				} else {
					ch = '▀' // upper half: fg = top, bg = bottom
					gi = topGi
					bg = botGi
				}
			case cnt > bot: // only bottom half
				ch = '▄' // lower half: fg = bottom
				gi = gradIdx(bot)
			default:
				continue
			}
			for c := 0; c < bw; c++ {
				grid[rowIdx][start+c] = statsCell{ch, gi, bg}
			}
		}
	}

	// Baseline.
	for c := 0; c < chartW; c++ {
		grid[chartH][c] = statsCell{'─', -1, -1}
	}

	// Dotted separators between weeks (30-day view).
	if m.statsRange == statsRange30Days {
		for k := 0; k < n-1; k++ {
			if buckets[k+1].start.Weekday() == time.Monday {
				col := barStart(k) + bw // the gap column after bar k
				for r := 0; r < rows; r++ {
					grid[r][col] = statsCell{'·', -1, -1}
				}
			}
		}
	}

	// Axis labels. Tagged with gi=-2 so renderCellRow picks the brighter
	// axis style (statsAxisStyle) — distinct from baseline ─ / dotted ·
	// separators which stay dim (gi=-1) to keep the chart structural
	// elements visually quiet.
	label := grid[rows-1]
	if weekly {
		for k := 0; k < n; k += 4 {
			_, wk := buckets[k].start.ISOWeek()
			for j, ch := range []rune(fmt.Sprintf("w%d", wk)) {
				if c := barStart(k) + j; c < chartW {
					label[c] = statsCell{ch, -2, -1}
				}
			}
		}
	} else {
		// Weekday labels under each daily bar, widening with the bars: full names
		// when there's room (7-day view), a 3-letter abbreviation when medium, a
		// single initial when narrow.
		for k := 0; k < n; k++ {
			wd := buckets[k].start.Weekday()
			var lbl string
			switch {
			case m.statsRange == statsRange7Days && bw >= 9:
				lbl = localizedWeekday(wd) // e.g. "Wednesday"
			case m.statsRange == statsRange7Days && bw >= 3:
				lbl = localizedWeekdayShort(wd) // e.g. "Wed"
			default:
				lbl = string(localizedWeekdayInitial(wd))
			}
			start := barStart(k) + (bw-len(lbl))/2
			if start < barStart(k) {
				start = barStart(k)
			}
			for j, ch := range lbl {
				if c := start + j; c >= 0 && c < chartW {
					label[c] = statsCell{ch, -2, -1}
				}
			}
		}
	}

	margin := strings.Repeat(" ", leftMargin)
	for r := 0; r < rows; r++ {
		b.WriteString(margin + renderCellRow(grid[r]) + "\n")
	}
	return b.String()
}

// renderCellRow renders a histogram grid row, grouping consecutive cells that
// share a style into one Render call and dropping trailing blanks. When a
// cell has bg >= 0, the segment is rendered with that secondary colour as the
// terminal background — used by ▀ half-blocks to stack two task colours in
// the same cell.
func renderCellRow(cells []statsCell) string {
	last := -1
	for c := range cells {
		if cells[c].ch != ' ' {
			last = c
		}
	}
	if last < 0 {
		return ""
	}
	var sb strings.Builder
	for c := 0; c <= last; {
		g := cells[c].gi
		bg := cells[c].bg
		start := c
		for c <= last && cells[c].gi == g && cells[c].bg == bg {
			c++
		}
		seg := make([]rune, 0, c-start)
		for _, cl := range cells[start:c] {
			seg = append(seg, cl.ch)
		}
		switch {
		case g == -2:
			sb.WriteString(statsAxisStyle.Render(string(seg)))
		case g < 0:
			sb.WriteString(dimStyle.Render(string(seg)))
		default:
			if bg >= 0 && bg < len(statsGradient) {
				style := lipgloss.NewStyle().
					Foreground(statsGradient[g].GetForeground()).
					Background(statsGradient[bg].GetForeground())
				sb.WriteString(style.Render(string(seg)))
			} else {
				sb.WriteString(statsGradient[g].Render(string(seg)))
			}
		}
	}
	return sb.String()
}

// ── Build helpers ─────────────────────────────────────────────────────────────

func (m model) buildListLines() []string {
	return strings.Split(m.renderListContent(), "\n")
}

func (m model) buildTagDetailLines() []string {
	tags := m.getFilteredTagsForTab()
	if len(tags) == 0 || m.tagTabCursor >= len(tags) {
		return strings.Split(dimStyle.Render("  No tag selected."), "\n")
	}

	tag := tags[m.tagTabCursor]
	b := getBuilder()
	defer putBuilder(b)

	// availW is the panel's inner text width (see View: w = termWidth-6, minus
	// the panel's horizontal padding). Every line is truncated to it so the
	// pane never wraps on a slim window.
	availW := m.termWidth - 8
	if availW < 12 {
		availW = 12
	}

	untagged := tag == untaggedKey

	// The task list comes from tagTaskList — the same ordered slice the drill
	// cursor walks, so the row highlighted here is the row the keys act on.
	// Counts and co-occurring tags are tallied off it.
	tasks := m.tagTaskList(tag)
	matches := make([]string, 0, len(tasks))
	active, done, overdue := 0, 0, 0
	cooccur := make(map[string]int)
	for i := range tasks {
		t := tasks[i]
		matches = append(matches, t.ID)
		if t.Status == todo.Done {
			done++
		} else {
			active++
		}
		if t.IsOverdue() {
			overdue++
		}
		for _, tt := range t.Tags {
			if tt != tag {
				cooccur[tt]++
			}
		}
	}

	count := len(matches)
	countWord := tr("%d task")
	if count != 1 {
		countWord = tr("%d tasks")
	}
	hint := "  (" + fmt.Sprintf(countWord, count)
	switch {
	case m.tagTaskMode:
		hint += tr(" · d: done · t: track · enter: details · esc: back)")
	case untagged:
		hint += tr(" · enter: open · f: filter)")
	default:
		hint += tr(" · enter: open · f: filter · r: rename)")
	}
	b.WriteString(dimStyle.Render(truncate(hint, availW)) + "\n")

	summary := fmt.Sprintf(tr("  %d active · %d done · %d overdue"), active, done, overdue)
	b.WriteString(normalStyle.Render(truncate(summary, availW)) + "\n")

	// Co-occurring tags, most frequent first. Only emit chips that fit so the
	// line can't wrap (no mid-string truncation of styled text).
	if len(cooccur) > 0 {
		type coTag struct {
			name string
			n    int
		}
		co := make([]coTag, 0, len(cooccur))
		for name, n := range cooccur {
			co = append(co, coTag{name, n})
		}
		sort.Slice(co, func(i, j int) bool {
			if co[i].n != co[j].n {
				return co[i].n > co[j].n
			}
			return co[i].name < co[j].name
		})
		label := tr("  often with: ")
		budget := availW - len([]rune(label))
		var chips []string
		used := 0
		for _, c := range co {
			chip := "#" + c.name
			w := len([]rune(chip))
			if len(chips) > 0 {
				w++ // separating space
			}
			if used+w > budget {
				break
			}
			chips = append(chips, chip)
			used += w
		}
		if len(chips) > 0 {
			b.WriteString(dimStyle.Render(label) + tagStyle.Render(strings.Join(chips, " ")) + "\n")
		}
	}
	b.WriteString("\n")

	if len(matches) == 0 {
		b.WriteString(dimStyle.Render(tr("  No tasks carry this tag.")) + "\n")
		return strings.Split(b.String(), "\n")
	}

	// The detail pane is height-capped (see applyDetailScroll). Rather than let
	// the generic scroll indicator hide the overflow, cap the list ourselves and
	// state how many are hidden.
	maxVisible := m.termHeight*detailMaxHeightPct/100 - 2
	if maxVisible < 3 {
		maxVisible = 3
	}
	taskBudget := maxVisible - strings.Count(b.String(), "\n")
	if taskBudget < 1 {
		taskBudget = 1
	}
	hidden := 0
	sel := -1
	if m.tagTaskMode {
		sel = m.cursor
	}
	if len(matches) > taskBudget {
		shown := taskBudget - 1 // reserve a line for the "and N more" notice
		if shown < 0 {
			shown = 0
		}
		hidden = len(matches) - shown
		// Window the capped list around the cursor, so drilling past the
		// budget scrolls instead of parking the selection out of sight.
		start := 0
		if sel >= shown {
			start = sel - shown + 1
		}
		if start+shown > len(matches) {
			start = len(matches) - shown
		}
		if start < 0 {
			start = 0
		}
		matches = matches[start : start+shown]
		if sel >= 0 {
			sel -= start
		}
	}

	for i, id := range matches {
		t := m.get(id)
		if t == nil {
			continue
		}
		status := "[ ]"
		if t.Status == todo.Done {
			status = "[✓]"
		}
		dueStr := ""
		if !t.DueDate.IsZero() {
			dueStr = tr("  due: ") + t.DueDate.Format("02-01-06")
			if t.IsOverdue() {
				dueStr += " !"
			}
		}
		projStr := ""
		if t.Project != "" {
			projStr = "  [" + t.Project + "]"
		}
		lead := cursorGap
		if i == sel {
			lead = cursorMark
		}
		line := truncate(fmt.Sprintf("%s%s %s%s%s", lead, status, t.Title, dueStr, projStr), availW)
		switch {
		case i == sel:
			b.WriteString(selectedStyle.Render(line) + "\n")
			continue
		case t.IsOverdue():
			b.WriteString(overdueStyle.Render(line) + "\n")
		case t.Status == todo.Done:
			b.WriteString(doneCountStyle.Render(line) + "\n")
		default:
			b.WriteString(normalStyle.Render(line) + "\n")
		}
	}

	if hidden > 0 {
		b.WriteString(dimStyle.Render(truncate(fmt.Sprintf(tr("  … and %d more"), hidden), availW)) + "\n")
	}

	return strings.Split(b.String(), "\n")
}

// ── Tabs ──────────────────────────────────────────────────────────────────────

func (m model) renderTabs(avail int) string {
	activeStyles := [numTabs]lipgloss.Style{
		tabTasksActiveStyle,
		tabCalendarActiveStyle,
		tabProjectsActiveStyle,
		tabTagsActiveStyle,
		tabBoardActiveStyle,
		tabStatsActiveStyle,
		tabSettingsActiveStyle,
	}
	inactiveStyles := [numTabs]lipgloss.Style{
		tabTasksInactiveStyle,
		tabCalendarInactiveStyle,
		tabProjectsInactiveStyle,
		tabTagsInactiveStyle,
		tabBoardInactiveStyle,
		tabStatsInactiveStyle,
		tabSettingsInactiveStyle,
	}
	// The selected tab renders as a solid colored pill. Unselected tabs use
	// the per-tab color as the foreground so each tab keeps its identity
	// without a background block.
	full := [numTabs]string{tr("1 Tasks"), tr("2 Calendar"), tr("3 Projects"), tr("4 Tags"), tr("5 Board"), tr("6 Stats"), tr("7 Settings")}
	nums := [numTabs]string{"1", "2", "3", "4", "5", "6", "7"}

	// abbr is the curated short label, one per tab, not a mechanical cut of the
	// full one. Clipping to three letters produced "5 Boa", "6 Sta", "7 Set" —
	// three tabs whose short forms say nothing and two that are nearly the same
	// word — and it degrades worse in translation, where a German "7 Ein" is
	// left of "Einstellungen". A tab that is already short keeps its full label
	// here, which is why several entries repeat it: shortening a five-letter
	// word to buy two cells costs more than the cells are worth.
	abbr := [numTabs]string{tr("1 Tasks"), tr("2 Cal"), tr("3 Proj"), tr("4 Tags"), tr("5 Board"), tr("6 Stats"), tr("7 Setup")}

	// Overdue work is the one fact worth carrying in the bar itself: it is the
	// reason to go to the Tasks tab, and from any other tab there was nothing
	// on screen that said so. The badge rides on the label rather than being
	// drawn separately, so the width fitting below counts it and the bar cannot
	// overflow the header on its account. It is deliberately not on the bare-
	// numbers level — at that width the bar is already down to what it takes to
	// say where you are.
	if n := len(m.cache.overdueSet); n > 0 {
		badge := " !" + strconv.Itoa(n)
		full[tabTasks] += badge
		abbr[tabTasks] += badge
	}

	// The selected tab always shows its full label so it is never truncated
	// away. Unselected tabs degrade uniformly (full → abbr → nums) to fit
	// the remaining budget. tabsWidthMixed measures the mixed arrangement
	// where the selected tab is fixed at selLabel and unselected tabs use
	// the given candidates array.
	selLabel := full[m.tab]
	selRunes := []rune(selLabel)
	if avail > 0 && len(selRunes) > avail {
		// Degenerate: selected title alone exceeds avail — clip it rather than
		// overflow; unselected tabs collapse to bare numbers.
		selLabel = string(selRunes[:avail])
	}

	// Pick the most-verbose unselected level that fits.
	unselNames := nums // fallback: bare numbers always fit (single rune each)
	for _, candidates := range [][numTabs]string{full, abbr, nums} {
		if tabsWidthMixed(candidates, m.tab, selLabel) <= avail {
			unselNames = candidates
			break
		}
	}

	names := unselNames
	names[m.tab] = selLabel

	// A hidden tab is dropped from the bar, leaving a visible gap in the
	// numbering (…4 Tags  6 Stats…). The numbers stay fixed on purpose: they
	// are baked into the labels and their translations, and renumbering would
	// move Stats under the user's fingers every time the board is toggled.
	// A gap reads as "something is off", which is exactly true.
	parts := make([]string, 0, numTabs)
	for i := range names {
		if !tabVisible(tab(i)) {
			continue
		}
		if tab(i) == m.tab {
			parts = append(parts, activeStyles[i].Render(names[i]))
		} else {
			parts = append(parts, inactiveStyles[i].Render(names[i]))
		}
	}
	return strings.Join(parts, " ")
}

// tabsWidthMixed measures the width of a mixed tab bar where tab sel uses
// selLabel and all other tabs use the corresponding label from names
// (rune length of the pre-style plain text, single-space separators).
//
// Every tab is rendered through a style with Padding(0, 1), so each one is two
// cells wider than its label. Leaving that out let renderTabs pick a level that
// measured within budget and rendered fourteen cells past it, which the header
// paid for by truncating whatever sat to the right of the bar — the shortcut
// hint, mid-word.
func tabsWidthMixed(names [numTabs]string, sel tab, selLabel string) int {
	visible := visibleTabCount()
	w := visible - 1 // single-space separators
	w += visible * 2 // the per-tab Padding(0, 1)
	for i, n := range names {
		if !tabVisible(tab(i)) {
			continue
		}
		if tab(i) == sel {
			w += len([]rune(selLabel))
		} else {
			w += len([]rune(n))
		}
	}
	return w
}

func (m model) renderListContent() string {
	switch m.tab {
	case tabTasks:
		if m.showHistory {
			return m.renderHistoryList()
		}
		return m.renderTaskList()
	case tabProjects:
		return m.renderProjectListContent(m.allProjectsForList())
	case tabTags:
		return m.renderTagList()
	case tabBoard:
		return m.renderBoardList()
	case tabStats:
		return m.renderStatsList()
	case tabSettings:
		return m.renderSettingsList()
	}
	return ""
}
