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

// truncateLines ANSI-aware-truncates every line to maxW display cells so
// over-long lines can never wrap inside a bordered panel.
func truncateLines(lines []string, maxW int) {
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, maxW, "")
	}
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
	// panel is hidden until the user explicitly opens it.
	switch m.tab {
	case tabTasks, tabProjects, tabLearnings:
		showDetail = showDetail && m.pane == paneDetail
	}

	if showDetail {
		switch {
		case m.tab == tabSettings:
			detailContent = "" // settings tab has no detail pane
		case m.tab == tabTags || m.tab == tabLearnings || m.tab == tabStats:
			detailContent = m.buildDetailContent()
		default:
			detailContent = m.getCachedDetailContent()
		}

		if detailContent != "" {
			detailContent = detailPanelStyle.Width(w).Render(m.applyDetailScroll(detailContent))
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
// bar: filter/history chips on the left, active-sort label and sync-health
// glyph on the right. A toast (m.err) overlays the whole line for its lifetime
// instead of claiming its own row, so filters and toasts coming and going never
// reflow the list below.
func (m model) renderStatusLine() string {
	width := m.termWidth - 2
	if width < 1 {
		width = 1
	}
	if m.err != "" {
		return ansi.Truncate(confirmStyle.Render(m.err), width, "")
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
	if m.tab == tabLearnings && m.learningSearchQuery != "" {
		chips = append(chips, searchChipStyle.Render("/"+m.learningSearchQuery))
	}
	if m.showHistory {
		chips = append(chips, historyChipStyle.Render(tr("HISTORY")))
	}
	left := strings.Join(chips, " ")

	var right []string
	if m.tab == tabTasks {
		right = append(right, statusSortStyle.Render(tr("sort: ")+m.sortLabel()))
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
			// Quick-add is the only input with inline syntax; surface it here —
			// otherwise it's only discoverable in the help overlay. The
			// keywords themselves stay English in every language (parsing is
			// locale-free), so only the example words are translated.
			return field + "\n" +
				helpStyle.Render("    "+truncate(tr("#tag @project due:tomorrow p:high s:l r:weekly dep:^"), w))
		}
		return field
	case modeIdlePrompt, modeConfirmUpdate:
		return calTodayStyle.Render(m.confirmMsg)
	case modeSearch:
		if m.tab == tabLearnings {
			return searchStyle.Width(w).Render(m.learningSearchInput.View())
		}
		return searchStyle.Width(w).Render(m.searchInput.View())
	case modeSearchTagTab:
		return searchStyle.Width(w).Render(m.tagTabSearchInput.View())
	case modeSearchDep:
		b := getBuilder()
		defer putBuilder(b)
		b.WriteString(searchStyle.Width(w).Render(m.depSearchInput.View()))
		shown := 0
		for i, r := range m.depSearchResults() {
			if i >= maxDepSearchResults {
				break
			}
			if i == m.depSearch.cursor {
				b.WriteString("\n" + selectedStyle.Render("  → "+r.Title))
			} else {
				b.WriteString("\n" + normalStyle.Render("    "+r.Title))
			}
			shown++
		}
		for shown < maxDepSearchResults {
			b.WriteString("\n")
			shown++
		}
		return b.String()
	case modeSearchTag:
		b := getBuilder()
		defer putBuilder(b)
		b.WriteString(searchStyle.Width(w).Render(m.tagSearchInput.View()))
		results := m.tagSearchResults()
		shown := 0
		for i, r := range results {
			if i >= maxTagSearchResults {
				break
			}
			if i == m.tagSearch.cursor {
				b.WriteString("\n" + selectedStyle.Render("  → #"+r))
			} else {
				b.WriteString("\n" + normalStyle.Render("    #"+r))
			}
			shown++
		}
		if len(results) == 0 && m.tagSearch.query != "" {
			b.WriteString("\n" + dimStyle.Render("  → "+tr("create new tag: ")) + tagStyle.Render(m.tagSearch.query))
			shown++
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
		for i, r := range results {
			if i >= maxProjSearchResults {
				break
			}
			if i == m.projSearch.cursor {
				b.WriteString("\n" + selectedStyle.Render("  → "+r))
			} else {
				b.WriteString("\n" + normalStyle.Render("    "+r))
			}
			shown++
		}
		if len(results) == 0 && m.projSearch.query != "" {
			b.WriteString("\n" + dimStyle.Render("  → "+tr("create new project: ")) + selectedStyle.Render(m.projSearch.query))
			shown++
		}
		for shown < maxProjSearchResults {
			b.WriteString("\n")
			shown++
		}
		return b.String()
	case modeConfirmDelete, modeConfirmDeleteComment,
		modeConfirmDeleteDep, modeConfirmDeleteTag,
		modeConfirmDeleteTagGlobal, modeConfirmDeleteProject,
		modeConfirmDeleteLearning, modeConfirmDeleteSubtask,
		modeConfirmDeleteTimeEntry, modeConfirmCloseParent:
		return confirmStyle.Render(m.confirmMsg)
	}
	return ""
}

// ── Key hints ─────────────────────────────────────────────────────────────────

func (m model) renderKeyHints(w int) string {
	var hints, short string
	switch {
	case m.tab == tabTasks && m.pane == paneDetail:
		hints = tr("←/→ pages · enter edit · a add · d toggle · x remove · n notes · esc back")
		short = tr("←/→ pages · enter edit · a add · x remove · esc back")
	case m.tab == tabTasks:
		hints = tr("enter detail · a add · d done · t track · p prio · r rename · x del · n notes · f focus · s sort · h history · / search")
		short = tr("a add · d done · t track · x del · s sort · / search · ? more")
	case m.tab == tabProjects:
		hints = tr("j/k nav · r rename · x delete · / filter")
	case m.tab == tabTags:
		hints = tr("j/k nav · r rename · m merge · x delete · s sort · / filter")
	case m.tab == tabLearnings:
		hints = tr("j/k nav · r edit · x delete · s sort · / search")
	case m.tab == tabStats:
		hints = tr("enter · cycle activity range")
	case m.tab == tabCalendar && m.calendar.focusTimeline:
		hints = tr("j/k select entry · r edit times · x delete · esc back")
	case m.tab == tabCalendar:
		hints = tr("←/→ day · ↑/↓ week · [ ] month · t today · enter entries")
	case m.tab == tabSettings:
		hints = tr("↑/↓ select · ←/→ change theme · enter activate")
	}
	// Prefer the full hint line; when it can't fit, fall back to a curated
	// short set instead of truncating mid-list — plain truncation always cut
	// the same trailing keys (e.g. / search on the Tasks tab), hiding them at
	// common terminal widths. hints is pre-Render plain text, so rune length
	// is the display width.
	if short != "" && len([]rune(hints)) > w {
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
	case m.tab == tabLearnings:
		lines := m.buildLearningDetailLines()
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
		if m.detail.page == 0 {
			return m.renderDetailPage1(t)
		}
		if m.detail.page == 1 {
			return m.renderDetailPage2(t)
		}
		return m.renderDetailPage3(t)
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
	return listPanelStyle.Width(w).Render(strings.Join(rawList, "\n"))
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
		return listPanelStyle.Width(w).Render(strings.Join(emptyLines, "\n"))
	}

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

	b := getBuilder()
	defer putBuilder(b)
	b.WriteString(projRendered)
	b.WriteString("\n")
	b.WriteString(ganttRendered)
	return b.String()
}

// ── Help ──────────────────────────────────────────────────────────────────────

func (m model) renderHelpFullscreen() string {
	sections := []struct {
		title string
		keys  [][2]string
	}{
		{tr("Navigation"), [][2]string{
			{"↑/↓  or  j/k", tr("navigate list")},
			{"enter", tr("open details")},
			{"esc", tr("go back")},
			{"tab  or  1-7", tr("switch tabs")},
			{"?", tr("close help")},
		}},
		{tr("Tasks"), [][2]string{
			{"a", tr("add task (quick-add: #tag due:date p:high @proj s:M)")},
			{"r", tr("rename task")},
			{"d", tr("toggle done")},
			{"t", tr("start/stop time tracking")},
			{"p", tr("cycle priority low/med/high")},
			{"x", tr("delete")},
			{"n", tr("edit notes (opens $EDITOR)")},
			{"f", tr("focus: today + overdue only")},
			{"h", tr("toggle history")},
			{"s", tr("cycle sort order")},
			{"←/→", tr("expand/collapse subtasks")},
			{"/", tr("search")},
		}},
		{tr("Detail view"), [][2]string{
			{"←/→", tr("switch pages")},
			{"enter", tr("edit field / toggle subtask")},
			{"n", tr("edit notes (opens $EDITOR)")},
			{"a", tr("add tag / dep / comment / learning / subtask")},
			{"d", tr("toggle subtask done")},
			{"t", tr("start/stop subtask timer")},
			{"x", tr("remove field / delete subtask")},
		}},
		{tr("Tags & Projects"), [][2]string{
			{"r", tr("rename globally")},
			{"x", tr("delete globally")},
			{"s", tr("toggle sort")},
			{"/", tr("filter")},
		}},
		{tr("Learnings"), [][2]string{
			{"r", tr("edit learning")},
			{"x", tr("delete learning")},
			{"s", tr("sort date/alpha")},
		}},
		{tr("Calendar (tab 2)"), [][2]string{
			{"←/→  ↑/↓", tr("move by day / week")},
			{"[ / ]", tr("previous / next month")},
			{"t", tr("jump to today")},
			{"enter", tr("focus the day's entries")},
			{"r", tr("edit entry times (09:12-10:00 or 45m)")},
			{"x", tr("delete selected entry")},
		}},
		{tr("Stats (tab 6)"), [][2]string{
			{"6 or tab", tr("switch to stats view")},
		}},
		{tr("Settings (tab 7)"), [][2]string{
			{"↑/↓", tr("select setting")},
			{"←/→", tr("change theme")},
			{"enter", tr("apply theme / check for updates")},
			{"y / n", tr("confirm update when one is offered")},
		}},
		{tr("App"), [][2]string{
			{"u", tr("undo last change")},
			{"q", tr("quit")},
		}},
		{tr("Date input"), [][2]string{
			{"dd-mm-yy", tr("exact date (e.g. 15-06-25)")},
			{"today", tr("today's date")},
			{"tomorrow", tr("tomorrow")},
			{"next week", tr("7 days from now")},
			{"next month", tr("1 month from now")},
			{"monday..sunday", tr("next occurrence of weekday")},
			{"+3d / +2w / +1m", tr("relative days/weeks/months")},
		}},
	}

	b := getBuilder()
	defer putBuilder(b)

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("  "+tr("Keyboard shortcuts")) + "\n")
	b.WriteString("\n")

	for _, section := range sections {
		b.WriteString(detailLabelStyle.Render("  "+section.title) + "\n")
		for _, kv := range section.keys {
			key := padRight(kv[0], 24)
			b.WriteString(
				helpStyle.Render("  ") +
					selectedStyle.Render(key) +
					normalStyle.Render(kv[1]) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  "+tr("Press ? or esc to close")) + "\n")

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

func (m model) buildLearningDetailLines() []string {
	learnings := m.allLearnings()
	if len(learnings) == 0 || m.learningCursor >= len(learnings) {
		return strings.Split(dimStyle.Render(tr("  No learning selected.")), "\n")
	}

	l := learnings[m.learningCursor]
	b := getBuilder()
	defer putBuilder(b)
	availW := m.termWidth - 8

	b.WriteString(learningSelectedStyle.Render("  "+truncate(l.Text, availW)) + "\n\n")

	wrapped := wrapText(l.Text, availW-2)
	if len(wrapped) > 1 {
		for _, line := range wrapped {
			b.WriteString(normalStyle.Render("  "+line) + "\n")
		}
		b.WriteString("\n")
	}

	sourceLabel := "  " + detailLabelStyle.Render(tr("Source task:  "))
	source := m.findLearningSource(l.ID)
	if source != nil {
		status := ""
		if source.Status == todo.Done {
			status = "  " + checkDoneStyle.Render(tr("[done]"))
		}
		b.WriteString(sourceLabel + normalStyle.Render(source.Title) + status + "\n")
	} else {
		b.WriteString(sourceLabel + dimStyle.Render(tr("[task removed]")) + "\n")
	}

	b.WriteString("  " + detailLabelStyle.Render(tr("Date:         ")) +
		normalStyle.Render(l.CreatedAt.Format("02-01-06 15:04")) + "\n")

	b.WriteString("  " + detailLabelStyle.Render(tr("Tags:         ")))
	if len(l.Tags) == 0 {
		b.WriteString(dimStyle.Render(tr("none")) + "\n")
	} else {
		b.WriteString(m.getRenderedTags(l.Tags) + "\n")
	}

	return strings.Split(b.String(), "\n")
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
	title := fmt.Sprintf("  #%s", tag)
	if untagged {
		title = tr("  (untagged)")
	}
	countWord := tr("%d task")
	if count != 1 {
		countWord = tr("%d tasks")
	}
	hint := "(" + fmt.Sprintf(countWord, count)
	if untagged {
		hint += tr(" · enter: filter)")
	} else {
		hint += tr(" · enter: filter · r: rename)")
	}
	if len([]rune(title))+1+len([]rune(hint)) <= availW {
		padW := availW - len([]rune(title)) - len([]rune(hint))
		b.WriteString(tagSelectedStyle.Render(title) + strings.Repeat(" ", padW) + dimStyle.Render(hint) + "\n")
	} else {
		b.WriteString(tagSelectedStyle.Render(truncate(title, availW)) + "\n")
	}

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
		tabLearningsActiveStyle,
		tabStatsActiveStyle,
		tabSettingsActiveStyle,
	}
	inactiveStyles := [numTabs]lipgloss.Style{
		tabTasksInactiveStyle,
		tabCalendarInactiveStyle,
		tabProjectsInactiveStyle,
		tabTagsInactiveStyle,
		tabLearningsInactiveStyle,
		tabStatsInactiveStyle,
		tabSettingsInactiveStyle,
	}
	// The selected tab renders as a solid colored pill. Unselected tabs use
	// the per-tab color as the foreground so each tab keeps its identity
	// without a background block.
	full := [numTabs]string{tr("1 Tasks"), tr("2 Calendar"), tr("3 Projects"), tr("4 Tags"), tr("5 Learnings"), tr("6 Stats"), tr("7 Settings")}
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

	// Degrade as the window narrows: full labels → 3-letter abbreviations →
	// only the active tab keeps its abbreviation (rest collapse to bare
	// numbers) → numbers only.
	names := nums
	switch {
	case tabsWidth(full[:]) <= avail:
		names = full
	case tabsWidth(abbr[:]) <= avail:
		names = abbr
	default:
		mixed := nums
		mixed[m.tab] = abbr[m.tab]
		if tabsWidth(mixed[:]) <= avail {
			names = mixed
		}
	}

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

// tabsWidth is the visible width of the tab labels joined with single spaces.
func tabsWidth(names []string) int {
	w := len(names) - 1 // single-space separators
	for _, n := range names {
		w += len([]rune(n))
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
	case tabLearnings:
		return m.renderLearningList()
	case tabStats:
		return m.renderStatsList()
	case tabSettings:
		return m.renderSettingsList()
	}
	return ""
}
