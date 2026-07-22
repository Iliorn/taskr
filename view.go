package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
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
func truncateLines(lines []string, maxW int) {
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, maxW, "")
	}
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
		return tr("Activity")
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
		return tr("Workflow")
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
	m.ensureCache()
	if m.mode == modeHelp {
		return m.renderHelpFullscreen()
	}

	out := getBuilder()
	defer putBuilder(out)

	w := m.termWidth - 6

	// ── HEADER ───────────────────────────────────────────────────────────
	shortcutHint := helpStyle.Render(tr("? shortcuts"))
	title := titleStyle.Render("taskr")
	// Width left for the tab bar between the title and the right-aligned hint.
	avail := m.termWidth - ansi.StringWidth(title) - 2 - ansi.StringWidth(shortcutHint) - 4
	tabsStr := title + "  " + m.renderTabs(avail)
	padW := m.termWidth - ansi.StringWidth(tabsStr) - ansi.StringWidth(shortcutHint) - 4
	if padW < 1 {
		padW = 1
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
			// enter-to-open tabs; the always-on previews (Tags/Stats) never do.
			focused := m.pane == paneDetail
			dst := detailPanelStyle
			if focused {
				dst = detailPanelFocusedStyle
			}
			detailContent = dst.Width(w).Render(m.applyDetailScroll(detailContent))
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
		chips = append(chips, focusChipStyle.Render(tr("⚡FOCUS")))
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

// syncGlyph reports background-sync health for the status line: a quiet dim
// tick when sync is configured and healthy, a red mark after a failure, and
// nothing when sync isn't configured.
func (m model) syncGlyph() string {
	if !m.autoSync {
		return ""
	}
	if m.lastSyncFailed {
		return syncFailStyle.Render(tr("✕ sync"))
	}
	return syncOkStyle.Render("✓")
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

	scrollStart := cursorLine - 2
	if scrollStart < 0 {
		scrollStart = 0
	}
	if scrollStart+maxVisible > len(lines) {
		scrollStart = len(lines) - maxVisible
	}
	if scrollStart < 0 {
		scrollStart = 0
	}
	if scrollStart <= 2 {
		scrollStart = 0
	}
	end := scrollStart + maxVisible
	if end > len(lines) {
		end = len(lines)
	}

	visible := make([]string, end-scrollStart)
	copy(visible, lines[scrollStart:end])

	if scrollStart > 0 {
		visible[0] = dimStyle.Render("  (…)")
	}
	if end < len(lines) {
		visible[len(visible)-1] = dimStyle.Render("  (…)")
	}

	return strings.Join(visible, "\n")
}

// ── Footer builder ────────────────────────────────────────────────────────────

func (m model) buildFooterContent(w int) string {
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
	case modeInput, modeEditComment, modeEditTag, modeEditTitle,
		modeAddLearning, modeEditLearning, modeAddSubtask, modeEditSubtask,
		modeEditProjectInline, modeEditTimeEntry, modeAddTimeEntry,
		modeEditSyncURL, modeEditSyncToken,
		modeEditServerListen, modeEditServerToken:
		field := inputStyle.Width(w).Render(m.textInput.View())
		if m.mode == modeInput && m.pane == paneList {
			// Quick-add: on a blank input show the syntax reference (the keywords
			// stay English in every language — parsing is locale-free — so only
			// the example words are translated); once typing, replace it with a
			// live preview of the parsed fields so a mistyped token is visible.
			if strings.TrimSpace(m.textInput.Value()) == "" {
				return field + "\n" +
					helpStyle.Render("    "+truncate(tr("#tag @project due:tomorrow p:high s:l r:weekly dep:^"), w))
			}
			return field + "\n" + renderQuickAddPreview(m.textInput.Value(), w)
		}
		// The single-line comment/learning inputs get a ctrl+e escape hatch to
		// compose in $EDITOR; advertise it under the field.
		switch {
		case m.mode == modeEditComment, m.mode == modeAddLearning, m.mode == modeEditLearning,
			m.mode == modeInput && m.pane != paneList && m.detail.field == fieldComments:
			return field + "\n" + helpStyle.Render("    "+tr("ctrl+e  edit in $EDITOR"))
		}
		return field
	case modeIdlePrompt, modeConfirmUpdate:
		return calTodayStyle.Render(m.confirmMsg)
	case modeSearch:
		field := searchStyle.Width(w).Render(m.searchInput.View())
		// The token grammar (compileSearch) only drives the Tasks list; on the
		// other tabs the query is a plain name substring, so a chip preview
		// would misrepresent it. Show the preview once the query is non-empty.
		if m.tab == tabTasks {
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
					b.WriteString("\n" + selectedStyle.Render("  → "+r.Title))
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
			b.WriteString("\n" + dimStyle.Render("  → "+tr("create new tag: ")) + tagStyle.Render(m.tagSearch.query))
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
						b.WriteString("\n" + selectedStyle.Render("  → #"+r))
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
			b.WriteString("\n" + dimStyle.Render("  → "+tr("create new project: ")) + selectedStyle.Render(m.projSearch.query))
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
						b.WriteString("\n" + selectedStyle.Render("  → "+r))
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
	case modeConfirm:
		return confirmStyle.Render(m.confirmMsg)
	}
	return ""
}

// ── Key hints ─────────────────────────────────────────────────────────────────

func (m model) renderKeyHints(w int) string {
	// Both the hint line and the help overlay are generated from the keymap
	// registry (keymap.go), so they can't drift from each other or from
	// dispatch.
	ctx := m.currentKeyCtx()
	hints := hintString(ctx, false)
	// Prefer the full hint line; when it can't fit, fall back to the curated
	// short (primary-only) set instead of truncating mid-list — plain
	// truncation always cut the same trailing keys (e.g. / search on the Tasks
	// tab), hiding them at common terminal widths. hints is pre-Render plain
	// text, so rune length is the display width.
	if short := hintString(ctx, true); short != "" && len([]rune(hints)) > w {
		hints = short
	}
	// 4-space indent aligns the hint under the box's inner content (margin 2 +
	// border 1 + padding 1) — so it begins at the same column as the task rows.
	return helpStyle.Render("    " + truncate(hints, w))
}

// ── Detail content ────────────────────────────────────────────────────────────

func (m model) buildDetailContent() string {
	switch {
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
	if m.sideBySide() {
		return m.buildSideBySide(w, outerH)
	}

	innerH := outerH - 2 // subtract top and bottom border lines
	if innerH < 1 {
		innerH = 1
	}
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

// buildSideBySide renders the side-by-side list tabs (Tasks/Learnings/Tags) as
// two columns: the list keeps full height on the left and the detail pane is an
// always-on preview of the cursor item on the right. Mirrors buildCalendarContent's
// approach — each
// column is rendered through a model copy whose termWidth is the column's
// share, so the existing width math (list columns, tag fitting, the no-wrap
// contract, the detail's own two-column threshold) applies per column
// unchanged. The focused pane carries the accent border.
func (m model) buildSideBySide(w, outerH int) string {
	innerH := outerH - 2
	if innerH < 1 {
		innerH = 1
	}
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
	detailFocused := m.pane == paneDetail
	if detailFocused {
		listStyle, detailStyle = listPanelStyle, detailPanelFocusedStyle
	}
	listPanel := listStyle.Width(listW).Render(strings.Join(listLines, "\n"))
	detailPanel := detailStyle.Width(detailW).Render(strings.Join(detailLines, "\n"))
	listPanel = withBorderTitle(listPanel, m.listPanelTitle(), listW, !detailFocused)
	detailPanel = withBorderTitle(detailPanel, m.detailPanelTitle(), detailW, detailFocused)
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
		innerH := listH - 2
		if innerH < 1 {
			innerH = 1
		}
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
	// same list+detail side-by-side contract as the Tasks/Learnings/Tags tabs:
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
	if ganttOuterH < minListPanelLines+2 {
		ganttOuterH = minListPanelLines + 2
	}
	ganttInnerH := ganttOuterH - 2
	if ganttInnerH < 1 {
		ganttInnerH = 1
	}

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
// in the right column, either the Gantt chart (when browsing the list,
// pane == paneList) or the task detail (when the user has pressed Enter on a
// task, pane == paneDetail). Mirrors buildSideBySide's contract — each column
// is rendered through a model copy whose termWidth is the column's share, and
// the focused pane carries the accent border.
func (m model) buildProjectDrillContent(projects []string, w, outerH int) string {
	innerH := outerH - 2
	if innerH < 1 {
		innerH = 1
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
		if len(tasks) > 0 {
			ganttContent := dm.renderGantt(tasks)
			rightLines = strings.Split(strings.TrimRight(ganttContent, "\n"), "\n")
		} else {
			rightLines = []string{dimStyle.Render(tr("  No tasks in this project."))}
		}
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
func (m model) helpBodyLines() []string {
	type helpSec struct {
		title string
		keys  [][2]string
	}
	// Key sections are generated from the keymap registry (keymap.go), so the
	// help overlay can't drift from the footer hints or from dispatch.
	var sections []helpSec
	for _, title := range helpSectionOrder {
		var keys [][2]string
		for i := range keymap {
			if bd := &keymap[i]; bd.section == title {
				keys = append(keys, [2]string{bd.key, tr(bd.desc)})
			}
		}
		if len(keys) > 0 {
			sections = append(sections, helpSec{tr(title), keys})
		}
	}
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
	}})

	// Reference section: date-input grammar. Not key bindings, so it lives
	// outside the registry and is appended last.
	sections = append(sections, helpSec{tr("Date input"), [][2]string{
		{"dd-mm-yy", tr("exact date (e.g. 15-06-25)")},
		{"today", tr("today's date")},
		{"tomorrow", tr("tomorrow")},
		{"next week", tr("7 days from now")},
		{"next month", tr("1 month from now")},
		{"monday..sunday", tr("next occurrence of weekday")},
		{"+3d / +2w / +1m", tr("relative days/weeks/months")},
	}})

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
	b.WriteString(titleStyle.Render("  "+tr("Keyboard shortcuts")) + "\n")
	b.WriteString("\n")

	for _, line := range body[scroll:end] {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	hint := tr("Press ? or esc to close")
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
	target := m.termHeight - 1
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

func (m model) renderStatsDetail() string {
	b := getBuilder()
	defer putBuilder(b)

	now := m.frameTime
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	innerW := m.termWidth - 8
	if innerW < 12 {
		innerW = 12
	}
	gradLen := len(statsGradient)

	// Build the time buckets for the selected range. Each bucket spans one day,
	// except the 6-month view which spans one (Mon-started) week.
	type bucket struct {
		start time.Time
		count int
	}
	var buckets []bucket
	var title string
	weekly := false
	switch m.statsRange {
	case statsRange30Days:
		title = tr("Last 30 days")
		for d := 29; d >= 0; d-- {
			buckets = append(buckets, bucket{start: today.AddDate(0, 0, -d)})
		}
	case statsRange6Months:
		title = tr("Last 26 weeks")
		weekly = true
		curMon := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
		for w := 25; w >= 0; w-- {
			buckets = append(buckets, bucket{start: curMon.AddDate(0, 0, -w*7)})
		}
	default:
		title = tr("Last 7 days")
		for d := 6; d >= 0; d-- {
			buckets = append(buckets, bucket{start: today.AddDate(0, 0, -d)})
		}
	}

	first := buckets[0].start
	total := 0
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

	// Chart height, also the per-bar cap. Kept modest so the stats list keeps
	// most of a short screen.
	chartH := m.termHeight*detailMaxHeightPct/100 - 2 - 9
	if chartH < 5 {
		chartH = 5
	}
	if chartH > 9 {
		chartH = 9
	}

	// Header: "<range> · N done" on the left, the legend on the right. Folding
	// the legend in here saves a whole row versus a separate caption line.
	headerLeft := statsHeaderStyle.Render(title) + dimStyle.Render("  ·  "+fmt.Sprintf(tr("%d done"), total))
	if total == 0 {
		b.WriteString(headerLeft + "\n")
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
	swatch := statsGradient[0].Render("▆") + statsGradient[gradLen/2].Render("▆") + statsGradient[gradLen-1].Render("▆")
	legend := swatch + dimStyle.Render(tr(": 1 block = 1 completed task"))
	spacer := innerW - ansi.StringWidth(headerLeft) - ansi.StringWidth(legend)
	if spacer < 1 {
		spacer = 1
	}
	b.WriteString(ansi.Truncate(headerLeft+strings.Repeat(" ", spacer)+legend, innerW, "") + "\n")

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

	// One pass: split active/done/overdue, tally co-occurring tags, and collect
	// the matching task IDs to list below.
	var matches []string
	active, done, overdue := 0, 0, 0
	cooccur := make(map[string]int)
	for id, t := range m.tasks {
		// Mirror selectActiveDone: the Tasks tab list is top-level
		// only, so the detail count must not include subtasks.
		if t.ParentID != "" {
			continue
		}
		match := false
		if untagged {
			match = len(t.Tags) == 0
		} else {
			for _, tt := range t.Tags {
				if tt == tag {
					match = true
					break
				}
			}
		}
		if !match {
			continue
		}
		matches = append(matches, id)
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
	if untagged {
		hint += tr(" · enter: filter)")
	} else {
		hint += tr(" · enter: filter · r: rename)")
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

	// Order: overdue first, then active, then done; alphabetical within each
	// group so the height-capped list always shows the most relevant tasks.
	cat := func(t *todo.Todo) int {
		switch {
		case t.Status == todo.Done:
			return 2
		case t.IsOverdue():
			return 0
		default:
			return 1
		}
	}
	sort.SliceStable(matches, func(a, b int) bool {
		ta, tb := m.get(matches[a]), m.get(matches[b])
		if ta == nil || tb == nil {
			return false
		}
		if ca, cb := cat(ta), cat(tb); ca != cb {
			return ca < cb
		}
		return strings.ToLower(ta.Title) < strings.ToLower(tb.Title)
	})

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
	if len(matches) > taskBudget {
		shown := taskBudget - 1 // reserve a line for the "and N more" notice
		if shown < 0 {
			shown = 0
		}
		hidden = len(matches) - shown
		matches = matches[:shown]
	}

	for _, id := range matches {
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
				dueStr += " ⚠"
			}
		}
		projStr := ""
		if t.Project != "" {
			projStr = "  [" + t.Project + "]"
		}
		line := truncate(fmt.Sprintf("  %s %s%s%s", status, t.Title, dueStr, projStr), availW)
		switch {
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

	// abbr keeps the "N " prefix and the first 3 letters of the name ("1 Tas").
	var abbr [numTabs]string
	for i, n := range full {
		space := strings.IndexByte(n, ' ')
		word := []rune(n[space+1:])
		if len(word) > 3 {
			word = word[:3]
		}
		abbr[i] = n[:space+1] + string(word)
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

	var parts [numTabs]string
	for i := range names {
		if tab(i) == m.tab {
			parts[i] = activeStyles[i].Render(names[i])
		} else {
			parts[i] = inactiveStyles[i].Render(names[i])
		}
	}
	return strings.Join(parts[:], " ")
}

// tabsWidthMixed measures the width of a mixed tab bar where tab sel uses
// selLabel and all other tabs use the corresponding label from names
// (rune length of the pre-style plain text, single-space separators).
func tabsWidthMixed(names [numTabs]string, sel tab, selLabel string) int {
	w := numTabs - 1 // single-space separators
	for i, n := range names {
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
