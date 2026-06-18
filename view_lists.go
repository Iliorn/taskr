package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// ── Tags list ─────────────────────────────────────────────────────────────────

func (m model) renderTagList() string {
	tags := m.getFilteredTagsForTab()

	if len(tags) == 0 {
		if m.tagTabSearchQuery != "" {
			return normalStyle.Render(tr("  No tags match your filter."))
		}
		return strings.Join([]string{
			normalStyle.Render(tr("  No tags yet. Add tags to tasks in the detail view.")),
			dimStyle.Render(tr("  Tags group related tasks; this tab shows progress per tag.")),
		}, "\n")
	}

	b := getBuilder()
	defer putBuilder(b)

	barW := m.termWidth / ganttBarWidthDivisor
	if barW < minTagBarWidth {
		barW = minTagBarWidth
	}
	if barW > maxTagBarWidth {
		barW = maxTagBarWidth
	}

	gradLen := len(tagProgressGradient)
	stats := m.cache.tags
	if stats == nil {
		stats = computeTagStats(m.allTodos())
	}

	// Size the tag column to the widest tag so Progress sits close behind it.
	// gap 4 = the 2-space cursor lead-in baked into this column + a 2-space gap.
	labelW := 0
	for _, tag := range tags {
		w := len([]rune(tag)) + 1 // leading '#'
		if tag == untaggedKey {
			w = len([]rune(tr("(untagged)")))
		}
		if w > labelW {
			labelW = w
		}
	}
	tagHdr := tr("  Tag")
	nameW := contentFitWidth(m.termWidth, labelW, 4, len([]rune(tagHdr)))
	headerLeft := padRight(tagHdr, nameW) + tr("Progress")
	padW := m.termWidth - 6 - len([]rune(headerLeft)) - barW
	if padW < 1 {
		padW = 1
	}
	b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW+barW)) + "\n")

	maxVisible := m.estimateListHeight()
	startIdx := m.listOffset
	endIdx := startIdx + maxVisible
	if endIdx > len(tags) {
		endIdx = len(tags)
	}
	if startIdx > len(tags) {
		startIdx = 0
	}

	var barStr strings.Builder
	barStr.Grow(barW * 4)

	for i := startIdx; i < endIdx; i++ {
		tag := tags[i]
		var s tagStats
		label := "#" + tag
		if tag == untaggedKey {
			s = tagStats{total: m.cache.untaggedTotal, done: m.cache.untaggedDone}
			label = tr("(untagged)")
		} else {
			s = stats[tag]
		}
		total, done := s.total, s.done

		pct := 0.0
		if total > 0 {
			pct = float64(done) / float64(total)
		}
		filled := int(math.Round(pct * float64(barW)))
		cur := "  "
		if i == m.tagTabCursor {
			cur = "▶ "
		}
		tagLabel := padRight(truncate(label, nameW-4), nameW-2)

		barStr.Reset()
		// Group consecutive cells that share a gradient color into a single
		// styled Render call (≤gradLen calls instead of one per column).
		prevIdx := -1
		runLen := 0
		for j := 0; j < filled; j++ {
			pos := 0.0
			if filled > 1 {
				pos = float64(j) / float64(filled-1)
			}
			gradIdx := int(pos * float64(gradLen-1))
			if gradIdx >= gradLen {
				gradIdx = gradLen - 1
			}
			if gradIdx != prevIdx {
				if runLen > 0 {
					barStr.WriteString(tagProgressGradient[prevIdx].Render(strings.Repeat("█", runLen)))
				}
				prevIdx = gradIdx
				runLen = 0
			}
			runLen++
		}
		if runLen > 0 {
			barStr.WriteString(tagProgressGradient[prevIdx].Render(strings.Repeat("█", runLen)))
		}
		if filled < barW {
			barStr.WriteString(dimStyle.Render(strings.Repeat("░", barW-filled)))
		}

		if m.mode == modeEditTag && m.editingTagName == tag {
			b.WriteString(tagSelectedStyle.Render(cur+tagLabel) + m.textInput.View() + "\n")
			continue
		}

		pctStr := fmt.Sprintf(tr(" %3d%% (%d done / %d total)"), int(pct*100), done, total)
		// Extra columns (clipped first on narrow terminals): average age of open
		// tasks, then total tracked time. Labelled inline so they're self-
		// explanatory, dot-separated. Skipped for the virtual untagged row.
		if tag != untaggedKey {
			var extras []string
			if s.openCount > 0 {
				extras = append(extras, tr("avg age ")+formatDaysCompact(s.ageSum/time.Duration(s.openCount)))
			}
			if s.tracked > 0 {
				extras = append(extras, tr("⏱ time spent ")+formatDurationCompact(s.tracked))
			}
			if len(extras) > 0 {
				pctStr += "  " + strings.Join(extras, " · ")
			}
		}
		if i == m.tagTabCursor {
			b.WriteString(
				tagSelectedStyle.Render(cur+tagLabel) +
					barStr.String() +
					selectedStyle.Render(pctStr) + "\n",
			)
		} else {
			b.WriteString(
				tagStyle.Render(cur+tagLabel) +
					barStr.String() +
					normalStyle.Render(pctStr) + "\n",
			)
		}
	}
	return b.String()
}

// ── Learnings list ────────────────────────────────────────────────────────────

func (m model) renderLearningList() string {
	learnings := m.allLearnings()

	if len(learnings) == 0 {
		if m.learningSearchQuery != "" {
			return normalStyle.Render(tr("  No learnings match your search."))
		}
		return strings.Join([]string{
			normalStyle.Render(tr("  No learnings yet. Add learnings from a task's detail view.")),
			dimStyle.Render(tr("  A learning is a takeaway you save on a task to keep for later.")),
		}, "\n")
	}

	b := getBuilder()
	defer putBuilder(b)

	availW := m.termWidth - 8
	tagsW := availW / 4
	if tagsW > 30 {
		tagsW = 30
	}
	if tagsW < 10 {
		tagsW = 10
	}
	const dateW = 10 // "02-01-06" + gap
	textMax := 0
	for i := range learnings {
		if tw := len([]rune(learnings[i].Text)); tw > textMax {
			textMax = tw
		}
	}
	learningHdr := tr("Learning")
	textW := contentFitWidth(m.termWidth, textMax, 2, len([]rune(learningHdr)))

	const prefix = "  "
	headerLeft := prefix + padRight(learningHdr, textW) + padRight(tr("Date"), dateW) + tr("Tags")
	padW := m.termWidth - 6 - len([]rune(headerLeft))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")

	maxVisible := m.estimateListHeight()
	startIdx := m.listOffset
	endIdx := startIdx + maxVisible
	if endIdx > len(learnings) {
		endIdx = len(learnings)
	}
	if startIdx > len(learnings) {
		startIdx = 0
	}

	for i := startIdx; i < endIdx; i++ {
		l := learnings[i]
		cur := "  "
		if i == m.learningCursor {
			cur = "▶ "
		}
		textCol := padRight(truncate(l.Text, textW), textW)
		tagsStr := ""
		for _, tag := range l.Tags {
			tagsStr += "#" + tag + " "
		}
		tagsCol := truncate(strings.TrimSpace(tagsStr), tagsW)
		dateCol := padRight(l.CreatedAt.Format("02-01-06"), dateW)

		if i == m.learningCursor {
			b.WriteString(
				learningSelectedStyle.Render(cur+textCol) +
					learningStyle.Render(dateCol) +
					tagStyle.Render(tagsCol) + "\n",
			)
		} else {
			b.WriteString(
				normalStyle.Render(cur+textCol) +
					dimStyle.Render(dateCol) +
					dimStyle.Render(tagsCol) + "\n",
			)
		}
	}
	return b.String()
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func (m model) renderStatsList() string {
	b := getBuilder()
	defer putBuilder(b)

	now := m.frameTime
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrow := today.AddDate(0, 0, 1)
	weekAhead := today.AddDate(0, 0, 7)
	weekAgo := today.AddDate(0, 0, -7)
	twoWeeksAgo := today.AddDate(0, 0, -14)
	monthAgo := today.AddDate(0, -1, 0)
	twoMonthsAgo := today.AddDate(0, -2, 0)

	var activeTasks, overdueTasks, dueToday, dueThisWeek int
	var doneToday, doneThisWeek, doneThisMonth, doneLastWeek, donePrevMonth int
	var createdThisWeek, createdThisMonth int
	var highPri, medPri, lowPri int
	var timeToDone []time.Duration
	var activeAges []time.Duration
	var oldestAge time.Duration
	oldestTitle := ""

	for _, t := range m.tasks {
		if t.ParentID != "" {
			continue
		}
		if !t.CreatedAt.Before(weekAgo) {
			createdThisWeek++
		}
		if !t.CreatedAt.Before(monthAgo) {
			createdThisMonth++
		}
		if t.Status == todo.Done {
			if !t.CompletedAt.IsZero() {
				if !t.CompletedAt.Before(today) {
					doneToday++
				}
				if !t.CompletedAt.Before(weekAgo) {
					doneThisWeek++
				}
				if !t.CompletedAt.Before(twoWeeksAgo) && t.CompletedAt.Before(weekAgo) {
					doneLastWeek++
				}
				if !t.CompletedAt.Before(monthAgo) {
					doneThisMonth++
					timeToDone = append(timeToDone, t.CompletedAt.Sub(t.CreatedAt))
				}
				if !t.CompletedAt.Before(twoMonthsAgo) && t.CompletedAt.Before(monthAgo) {
					donePrevMonth++
				}
			}
		} else {
			activeTasks++
			age := now.Sub(t.CreatedAt)
			activeAges = append(activeAges, age)
			if age > oldestAge {
				oldestAge = age
				oldestTitle = t.Title
			}
			switch {
			case t.IsOverdue():
				overdueTasks++
			case !t.DueDate.IsZero() && t.DueDate.Before(tomorrow):
				dueToday++
			case !t.DueDate.IsZero() && t.DueDate.Before(weekAhead):
				dueThisWeek++
			}
			switch t.Priority {
			case todo.PriorityHigh:
				highPri++
			case todo.PriorityMedium:
				medPri++
			default:
				lowPri++
			}
		}
	}

	availW := m.termWidth - 8
	gradLen := len(statsGradient)

	// Lay sections out in up to three columns when there's room, so the page
	// stays short enough to fit a not-very-tall screen. minColW guarantees each
	// column is wide enough to show the longest stat line ("vs …" trend) without
	// truncation, so colW >= minColW always holds in multi-column mode.
	const gap = 4
	const minColW = 37
	cols := (availW + gap) / (minColW + gap)
	if cols < 1 {
		cols = 1
	}
	if cols > 3 {
		cols = 3
	}
	colW := availW
	valW := statsValueWidth
	if cols > 1 {
		colW = (availW - (cols-1)*gap) / cols
		valW = 5
	}

	// stat writes one row sized to colW into sb (bar only if it fits).
	stat := func(sb *strings.Builder, label string, value, total int, showBar bool) {
		labelStr := padRight("  "+label, statsLabelWidth)
		valStr := fmt.Sprintf("%d", value)
		barW := colW - statsLabelWidth - valW - 6
		if barW > statsBarWidth {
			barW = statsBarWidth
		}
		if !showBar || total <= 0 || barW < 6 {
			sb.WriteString(detailLabelStyle.Render(labelStr) + normalStyle.Render(valStr) + "\n")
			return
		}
		pct := float64(value) / float64(total)
		filled := int(pct * float64(barW))
		if filled > barW {
			filled = barW
		}
		var bar strings.Builder
		bar.Grow(barW * 4)
		for j := 0; j < barW; j++ {
			if j < filled {
				pos := 0.0
				if filled > 1 {
					pos = float64(j) / float64(filled-1)
				}
				gradIdx := int(pos * float64(gradLen-1))
				if gradIdx >= gradLen {
					gradIdx = gradLen - 1
				}
				bar.WriteString(statsGradient[gradIdx].Render("█"))
			} else {
				bar.WriteString(dimStyle.Render("░"))
			}
		}
		sb.WriteString(detailLabelStyle.Render(labelStr) + normalStyle.Render(padRight(valStr, valW)) +
			bar.String() + dimStyle.Render(fmt.Sprintf(" %3d%%", int(pct*100))) + "\n")
	}

	section := func(build func(*strings.Builder)) string {
		var sb strings.Builder
		build(&sb)
		return strings.TrimRight(sb.String(), "\n")
	}

	workload := section(func(sb *strings.Builder) {
		sb.WriteString(statsHeaderStyle.Render(tr("  Workload")) + "\n")
		if overdueTasks > 0 {
			sb.WriteString(detailLabelStyle.Render(padRight("  "+tr("Overdue"), statsLabelWidth)) +
				overdueCountStyle.Render(fmt.Sprintf("%d", overdueTasks)) + "\n")
		} else {
			stat(sb, tr("Overdue"), 0, 0, false)
		}
		stat(sb, tr("Due today"), dueToday, 0, false)
		stat(sb, tr("Due this week"), dueThisWeek, 0, false)
		stat(sb, tr("Active total"), activeTasks, 0, false)
	})

	// flowSection renders a created/completed/net-backlog block with a trend
	// comparison against the previous equal-length period.
	flowSection := func(title, vsLabel string, created, completed, prevCompleted int) string {
		return section(func(sb *strings.Builder) {
			sb.WriteString(statsHeaderStyle.Render("  "+title) + "\n")
			stat(sb, tr("Created"), created, 0, false)
			stat(sb, tr("Completed"), completed, 0, false)
			net := created - completed
			netLabel := detailLabelStyle.Render(padRight(tr("  Net backlog"), statsLabelWidth))
			switch {
			case net > 0:
				sb.WriteString(netLabel + overdueCountStyle.Render(fmt.Sprintf(tr("+%d ▲ growing"), net)) + "\n")
			case net < 0:
				sb.WriteString(netLabel + activeCountStyle.Render(fmt.Sprintf(tr("%d ▼ shrinking"), net)) + "\n")
			default:
				sb.WriteString(netLabel + dimStyle.Render(tr("±0 → steady")) + "\n")
			}
			trendArrow := "→"
			if completed > prevCompleted {
				trendArrow = "↑"
			} else if completed < prevCompleted {
				trendArrow = "↓"
			}
			sb.WriteString(detailLabelStyle.Render(padRight("  "+vsLabel, statsLabelWidth)) +
				normalStyle.Render(fmt.Sprintf(tr("%d done vs %d  %s"), completed, prevCompleted, trendArrow)) + "\n")
		})
	}

	flow := flowSection(tr("Flow (last 7 days)"), tr("vs last week"), createdThisWeek, doneThisWeek, doneLastWeek)
	flow30 := flowSection(tr("Flow (last 30 days)"), tr("vs prior 30d"), createdThisMonth, doneThisMonth, donePrevMonth)

	throughput := section(func(sb *strings.Builder) {
		sb.WriteString(statsHeaderStyle.Render(tr("  Throughput")) + "\n")
		ttdLabel := detailLabelStyle.Render(padRight(tr("  Time to done (30d)"), statsLabelWidth))
		if len(timeToDone) > 0 {
			sb.WriteString(ttdLabel + normalStyle.Render(tr("median ")+formatDaysCompact(medianDuration(timeToDone))) + "\n")
		} else {
			sb.WriteString(ttdLabel + dimStyle.Render(tr("none yet")) + "\n")
		}
		if len(activeAges) > 0 {
			sb.WriteString(detailLabelStyle.Render(padRight(tr("  Median active age"), statsLabelWidth)) +
				normalStyle.Render(formatDaysCompact(medianDuration(activeAges))) + "\n")
			oldestW := colW - statsLabelWidth - 12
			if oldestW < 8 {
				oldestW = 8
			}
			sb.WriteString(detailLabelStyle.Render(padRight(tr("  Oldest active"), statsLabelWidth)) +
				normalStyle.Render(truncate(oldestTitle, oldestW)) +
				dimStyle.Render(" ("+formatDaysCompact(oldestAge)+")") + "\n")
		}
	})

	var priority string
	if activeTasks > 0 {
		priority = section(func(sb *strings.Builder) {
			sb.WriteString(statsHeaderStyle.Render(tr("  Active by priority")) + "\n")
			stat(sb, tr("↑ High"), highPri, activeTasks, true)
			stat(sb, tr("→ Medium"), medPri, activeTasks, true)
			stat(sb, tr("↓ Low"), lowPri, activeTasks, true)
		})
	}

	velocity := section(func(sb *strings.Builder) {
		sb.WriteString(statsHeaderStyle.Render(tr("  Completion velocity")) + "\n")
		stat(sb, tr("Today"), doneToday, 0, false)
		stat(sb, tr("This week"), doneThisWeek, 0, false)
		stat(sb, tr("This month"), doneThisMonth, 0, false)
		if doneThisWeek > 0 {
			sb.WriteString(detailLabelStyle.Render(padRight(tr("  Avg (7d)"), statsLabelWidth)) +
				normalStyle.Render(fmt.Sprintf(tr("%.1f tasks/day"), float64(doneThisWeek)/7.0)) + "\n")
		}
	})

	switch cols {
	case 3:
		// Keep the two Flow windows together in the middle column.
		b.WriteString(zipColumns(colW, gap,
			stackSections(workload, throughput),
			stackSections(flow, flow30),
			stackSections(priority, velocity)))
	case 2:
		b.WriteString(zipColumns(colW, gap,
			stackSections(workload, flow, flow30),
			stackSections(throughput, priority, velocity)))
	default:
		first := true
		for _, s := range []string{workload, flow, flow30, throughput, priority, velocity} {
			if strings.TrimSpace(s) == "" {
				continue
			}
			if !first {
				b.WriteString("\n")
			}
			b.WriteString(s + "\n")
			first = false
		}
	}

	return b.String()
}

// stackSections concatenates non-empty section blocks into a line slice with a
// blank line between each.
func stackSections(sections ...string) []string {
	var lines []string
	for _, s := range sections {
		if strings.TrimSpace(s) == "" {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(s, "\n")...)
	}
	return lines
}

// zipColumns places line slices side by side, each padded (ANSI-aware) to colW
// with a gap between. Every column but the last is truncated to colW so a long
// line can never bleed into its neighbour.
func zipColumns(colW, gap int, columns ...[]string) string {
	maxLen := 0
	for _, col := range columns {
		if len(col) > maxLen {
			maxLen = len(col)
		}
	}
	var b strings.Builder
	pad := strings.Repeat(" ", gap)
	for i := 0; i < maxLen; i++ {
		for c, col := range columns {
			line := ""
			if i < len(col) {
				line = col[i]
			}
			if c == len(columns)-1 {
				b.WriteString(strings.TrimRight(line, " "))
				continue
			}
			line = ansi.Truncate(line, colW, "")
			if lw := ansi.StringWidth(line); lw < colW {
				line += strings.Repeat(" ", colW-lw)
			}
			b.WriteString(line + pad)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// medianDuration returns the median of ds, sorting in place.
func medianDuration(ds []time.Duration) time.Duration {
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	mid := len(ds) / 2
	if len(ds)%2 == 0 {
		return (ds[mid-1] + ds[mid]) / 2
	}
	return ds[mid]
}

// formatDaysCompact is a tight form of formatDays ("~12d") for inline columns.
func formatDaysCompact(d time.Duration) string {
	days := d.Hours() / 24
	switch {
	case days < 1:
		return "<1d"
	case days < 10:
		return fmt.Sprintf("%.1fd", days)
	default:
		return fmt.Sprintf("%.0fd", days)
	}
}

// ── Task lists ────────────────────────────────────────────────────────────────

func (m model) renderTaskList() string {
	active := m.activeTodos()
	if len(active) == 0 {
		if m.searchQuery != "" {
			return normalStyle.Render(tr("  No tasks match your search."))
		}
		if m.focusFilter {
			return normalStyle.Render(tr("  No tasks due today or overdue. Nice!"))
		}
		// First-run guidance: show the quick-add syntax (English keywords stay
		// literal — they're parsing tokens, not display strings) plus a pointer to
		// the full help. Width-clip the example so it honours the no-wrap contract.
		availW := m.termWidth - 8
		return strings.Join([]string{
			normalStyle.Render(tr("  No tasks yet. Press 'a' to add one.")),
			"",
			dimStyle.Render(truncate(tr("  Try:  ")+tr("Buy milk #shopping due:friday p:high @home"), availW)),
			dimStyle.Render(tr("  Press ? for all keyboard shortcuts.")),
		}, "\n")
	}

	b := getBuilder()
	defer putBuilder(b)

	overdueSet := m.cache.overdueSet

	// Size the title column to the widest displayed task (title + its indicators).
	contentMax := 0
	for i := range active {
		w := len([]rune(active[i].Title))
		if active[i].HasOverdueDependencyFast(overdueSet) {
			w += 2 // " !"
		}
		if active[i].Notes != "" {
			w += 2 // " ¶"
		}
		if active[i].IsTimerRunning() {
			w += 2 // " ⏱"
		}
		if w > contentMax {
			contentMax = w
		}
	}
	cols := taskListCols(m.termWidth, false, contentMax)
	renderListHeader(b, m.termWidth, m.cursor, len(active), false, m.taskSort, cols)

	maxVisible := m.estimateListHeight()
	startIdx := m.listOffset
	endIdx := startIdx + maxVisible
	if endIdx > len(active) {
		endIdx = len(active)
	}
	if startIdx > len(active) {
		startIdx = 0
	}

	for i := startIdx; i < endIdx; i++ {
		t := active[i]
		b.WriteString(m.renderTaskLineWithSet(t, i, m.cursor, m.pane == paneList, overdueSet, cols))
		if subIDs := m.subtaskIDs(t.ID); len(subIDs) > 0 && m.expandedTasks[t.ID] {
			for j, subID := range subIDs {
				sub := m.findTodoByID(subID)
				if sub == nil {
					continue
				}
				b.WriteString(m.renderSubtaskLine(sub, j, len(subIDs), cols))
			}
		}
	}
	return b.String()
}

func (m model) renderHistoryList() string {
	completed := m.completedTodos()
	if len(completed) == 0 {
		if m.searchQuery != "" {
			return normalStyle.Render(tr("  No completed tasks match your search."))
		}
		return normalStyle.Render(tr("  No completed tasks yet."))
	}

	b := getBuilder()
	defer putBuilder(b)

	contentMax := 0
	for i := range completed {
		if w := len([]rune(completed[i].Title)); w > contentMax {
			contentMax = w
		}
	}
	cols := taskListCols(m.termWidth, true, contentMax)
	renderListHeader(b, m.termWidth, m.cursor, len(completed), true, m.taskSort, cols)

	maxVisible := m.estimateListHeight()
	startIdx := m.listOffset
	endIdx := startIdx + maxVisible
	if endIdx > len(completed) {
		endIdx = len(completed)
	}
	if startIdx > len(completed) {
		startIdx = 0
	}

	for i := startIdx; i < endIdx; i++ {
		b.WriteString(m.renderHistoryLine(completed[i], i, m.cursor, m.pane == paneList, cols))
	}
	return b.String()
}

func (m model) renderHistoryLine(t todo.Todo, index, cursor int, active bool, cols listCols) string {
	titleW := cols.titleW
	cursorStr := "  "
	if index == cursor && active {
		cursorStr = "▶ "
	}
	dueVal := ""
	if !t.DueDate.IsZero() {
		dueVal = t.DueDate.Format("02-01-06")
	}
	completedVal := ""
	if !t.CompletedAt.IsZero() {
		completedVal = t.CompletedAt.Format("02-01-06")
	}
	titleCol := padRight(truncate(t.Title, titleW), titleW)
	dateCols := ""
	if cols.showDue {
		dateCols += padRight(dueVal, 12)
	}
	if cols.showLast {
		dateCols += padRight(completedVal, 12)
	}
	tagsPart := m.getRenderedTags(t.Tags)
	mainW := len([]rune(cursorStr)) + 4 + len([]rune(titleCol)) + len([]rune(dateCols))
	tagsStr := ""
	if tagsPart != "" {
		tagsW := 0
		for _, tag := range t.Tags {
			tagsW += 4 + len([]rune(tag))
		}
		if mainW+1+tagsW <= m.termWidth-8 {
			tagsStr = " " + tagsPart
		} else {
			// Trim last 5 chars of titleCol to make room for (...).
			r := []rune(titleCol)
			if len(r) > 5 {
				titleCol = string(r[:len(r)-5]) + dimStyle.Render("(...)")
			}
		}
	}

	if index == cursor && active {
		return selectedStyle.Render(cursorStr+"[") +
			checkDoneStyle.Render("✓") +
			selectedStyle.Render("] "+titleCol+dateCols) +
			tagsStr + "\n"
	}
	return normalStyle.Render(cursorStr+"[") +
		checkDoneStyle.Render("✓") +
		normalStyle.Render("] "+titleCol+dateCols) +
		tagsStr + "\n"
}

func (m model) renderSubtaskLine(sub *todo.Todo, index, total int, cols listCols) string {
	connector := "├"
	if index == total-1 {
		connector = "└"
	}
	titleW := cols.titleW - 4
	if titleW < 10 {
		titleW = 10
	}
	if sub.Status == todo.Done {
		return dimStyle.Render("     "+connector+" [") + checkDoneStyle.Render("✓") + dimStyle.Render("] "+truncate(sub.Title, titleW)) + "\n"
	}
	return dimStyle.Render("     "+connector+" [ ] "+truncate(sub.Title, titleW)) + "\n"
}

func (m model) renderTaskLineWithSet(t todo.Todo, index, cursor int, active bool, overdueSet map[string]bool, cols listCols) string {
	titleW := cols.titleW
	cursorStr := "  "
	if index == cursor && active {
		cursorStr = "▶ "
	}
	checkbox := "[ ]"
	if t.Status == todo.Done {
		checkbox = "[✓]"
	} else if len(t.TimeEntries) > 0 {
		checkbox = "[>]"
	}
	foldIcon := " "
	if m.subtaskCount(t.ID) > 0 {
		if m.expandedTasks[t.ID] {
			foldIcon = "▾"
		} else {
			foldIcon = "▸"
		}
	}
	title := t.Title
	hasOverdueDep := t.HasOverdueDependencyFast(overdueSet)
	if hasOverdueDep {
		title += " !"
	}
	if t.Notes != "" {
		title += " ¶"
	}
	if t.IsTimerRunning() {
		title = "⏱ " + title
	}
	dueVal := ""
	if !t.DueDate.IsZero() {
		dueVal = t.DueDate.Format("02-01-06")
	}
	titleCol := padRight(truncate(title, titleW), titleW)
	tagsPart := m.getRenderedTags(t.Tags)
	line := cursorStr + checkbox + foldIcon + titleCol
	if cols.showSize {
		line += padRight(strings.ToLower(t.Size.Letter()), sizeColW)
	}
	if cols.showDue {
		line += padRight(dueVal, 12)
	}
	if cols.showLast {
		// Score column is always score now — priority lives only in the
		// detail view, where the user can still set it.
		line += padRight(fmt.Sprintf("%.1f", sequenceScore(&t)), 12)
	}
	if cols.showProject {
		line += padRight(truncate(t.Project, projectColW-1), projectColW)
	}

	// Only append tags if they fit within the inner panel content width.
	tagsStr := ""
	if tagsPart != "" {
		tagsW := 0
		for _, tag := range t.Tags {
			tagsW += 4 + len([]rune(tag))
		}
		if len([]rune(line))+1+tagsW <= m.termWidth-8 {
			tagsStr = " " + tagsPart
		} else {
			// Overwrite the last 5 chars of the line with (...) so it always fits.
			runes := []rune(line)
			if len(runes) > 5 {
				line = string(runes[:len(runes)-5]) + dimStyle.Render("(...)")
			}
		}
	}

	switch {
	case t.IsTimerRunning():
		return timerStyle.Render(line) + tagsStr + "\n"
	case t.IsOverdue():
		return overdueStyle.Render(line) + tagsStr + "\n"
	case hasOverdueDep:
		return depOverdueStyle.Render(line) + tagsStr + "\n"
	case index == cursor && active:
		return selectedStyle.Render(line) + tagsStr + "\n"
	default:
		return normalStyle.Render(line) + tagsStr + "\n"
	}
}

// ── Projects ──────────────────────────────────────────────────────────────────

func (m model) renderProjectListContent(projects []string) string {
	if len(projects) == 0 {
		if m.searchQuery != "" {
			return normalStyle.Render(tr("  No projects match your search."))
		}
		return normalStyle.Render(tr("  No projects yet. Add a project to a task first."))
	}

	b := getBuilder()
	defer putBuilder(b)

	w := m.termWidth - 8
	nameMax := 0
	for _, p := range projects {
		if pw := len([]rune(p)); pw > nameMax {
			nameMax = pw
		}
	}
	projHdr := tr("Project")
	projW := contentFitWidth(m.termWidth, nameMax, 2, len([]rune(projHdr)))

	const prefix = "  "
	headerLeft := prefix + padRight(projHdr, projW) +
		padRight(tr("Active"), projCountColWidth) +
		padRight(tr("Done"), projDoneColWidth) + tr("Overdue")
	padW := w - len([]rune(headerLeft))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")

	for i, p := range projects {
		tasks := m.getProjectTasks(p)
		var activeCnt, doneCnt, overdueCnt int
		for _, t := range tasks {
			if t.Status == todo.Done {
				doneCnt++
			} else {
				activeCnt++
				if t.IsOverdue() {
					overdueCnt++
				}
			}
		}
		cursorStr := "  "
		if i == m.projectCursor {
			cursorStr = "▶ "
		}
		if m.mode == modeEditProjectInline && i == m.projectCursor {
			b.WriteString(normalStyle.Render(cursorStr) + m.textInput.View() + "\n")
			continue
		}
		nameCol := padRight(truncate(p, projW-2), projW)
		activeStr := padRight(fmt.Sprintf(tr("%d active"), activeCnt), projCountColWidth)
		doneStr := padRight(fmt.Sprintf(tr("%d done"), doneCnt), projDoneColWidth)
		overdueStr := "─"
		if overdueCnt > 0 {
			overdueStr = fmt.Sprintf(tr("%d overdue"), overdueCnt)
		}
		switch {
		case i == m.projectCursor:
			line := selectedStyle.Render(cursorStr + nameCol + activeStr + doneStr)
			if overdueCnt > 0 {
				b.WriteString(line + overdueStyle.Render(overdueStr) + "\n")
			} else {
				b.WriteString(line + selectedStyle.Render(overdueStr) + "\n")
			}
		case activeCnt == 0:
			b.WriteString(doneCountStyle.Render(cursorStr+nameCol+activeStr+doneStr+overdueStr) + "\n")
		default:
			ovdRendered := dimStyle.Render(overdueStr)
			if overdueCnt > 0 {
				ovdRendered = overdueCountStyle.Render(overdueStr)
			}
			b.WriteString(
				normalStyle.Render(cursorStr+nameCol) +
					activeCountStyle.Render(activeStr) +
					doneCountStyle.Render(doneStr) +
					ovdRendered + "\n")
		}
	}
	return b.String()
}

// ── Settings list ─────────────────────────────────────────────────────────────

func (m model) renderSettingsList() string {
	b := getBuilder()
	defer putBuilder(b)

	labels := [numSettingsRows]string{
		tr("Deadline pressure"),
		tr("Priority focus"),
		tr("Momentum bias"),
		tr("Theme"),
		tr("Language"),
		tr("Version"),
		tr("Check for updates"),
	}
	values := [numSettingsRows]string{
		biasPickerValue(activeBiases.Deadline),
		biasPickerValue(activeBiases.Priority),
		biasPickerValue(activeBiases.Momentum),
		"‹ " + m.themeName + " ›",
		"‹ " + activeLang.displayName() + " ›",
		appVersion,
		tr("press enter to check"),
	}

	b.WriteString(headerStyle.Render(tr("Settings")) + "\n\n")
	for i := 0; i < numSettingsRows; i++ {
		cursor := "  "
		labelStyle := normalStyle
		if i == m.settingsCursor {
			cursor = selectedStyle.Render("→ ")
			labelStyle = selectedStyle
		}
		label := labelStyle.Render(fmt.Sprintf("%-26s", labels[i]))
		b.WriteString(cursor + label + helpStyle.Render(values[i]) + "\n")
	}

	// Personality footer: a one-line summary of what the current bias mix
	// "feels like", so a user tweaking a single bias gets immediate text
	// feedback that their sequence has shifted.
	name, descr := personality(activeBiases)
	b.WriteString("\n  " + activeCountStyle.Render("Sequence: "+name) +
		"  " + helpStyle.Render(descr) + "\n")

	if m.updateStatus != "" {
		b.WriteString("\n  " + activeCountStyle.Render(m.updateStatus) + "\n")
	}
	return b.String()
}

// biasPickerValue formats a bias for the Settings picker the same way the
// theme/language pickers do: title-cased value between thin chevrons.
func biasPickerValue(b biasLevel) string {
	s := b.String()
	if s == "" {
		return "‹ - ›"
	}
	return "‹ " + strings.ToUpper(s[:1]) + s[1:] + " ›"
}
