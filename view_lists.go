package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"taskr/todo"
)

// ── Tags list ─────────────────────────────────────────────────────────────────

func (m model) renderTagList() string {
	tags := m.getFilteredTagsForTab()

	if len(tags) == 0 {
		if m.tagTabSearchQuery != "" {
			return normalStyle.Render("  No tags match your filter.")
		}
		return normalStyle.Render("  No tags yet. Add tags to tasks in the detail view.")
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
		stats = computeTagStats(m.todos)
	}

	headerLeft := padRight("  Tag", tagLabelColWidth) + "Progress"
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
		s := stats[tag]
		total := s.total
		done := s.done

		pct := 0.0
		if total > 0 {
			pct = float64(done) / float64(total)
		}
		filled := int(math.Round(pct * float64(barW)))
		cur := "  "
		if i == m.tagTabCursor {
			cur = "▶ "
		}
		tagLabel := padRight(truncate("#"+tag, tagLabelColWidth-4), tagLabelColWidth-2)

		barStr.Reset()
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
				barStr.WriteString(tagProgressGradient[gradIdx].Render("█"))
			} else {
				barStr.WriteString(dimStyle.Render("░"))
			}
		}

		if m.mode == modeEditTag && m.editingTagName == tag {
			b.WriteString(tagSelectedStyle.Render(cur+tagLabel) + m.textInput.View() + "\n")
			continue
		}

		pctStr := fmt.Sprintf(" %3d%% (%d done / %d total)", int(pct*100), done, total)
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
			return normalStyle.Render("  No learnings match your search.")
		}
		return normalStyle.Render("  No learnings yet. Add learnings from a task's detail view.")
	}

	b := getBuilder()
	defer putBuilder(b)

	availW := m.termWidth - 8
	dateW := 8
	tagsW := availW / 4
	if tagsW > 30 {
		tagsW = 30
	}
	if tagsW < 10 {
		tagsW = 10
	}
	textW := availW - dateW - tagsW - 6

	const prefix = "      "
	headerLeft := prefix + padRight("Learning", textW) + padRight("Tags", tagsW) + "Date"
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
		tagsCol := padRight(truncate(strings.TrimSpace(tagsStr), tagsW), tagsW)
		dateCol := l.CreatedAt.Format("02-01-06")

		if i == m.learningCursor {
			b.WriteString(
				learningSelectedStyle.Render(cur+textCol) +
					tagStyle.Render(tagsCol) +
					learningStyle.Render(dateCol) + "\n",
			)
		} else {
			b.WriteString(
				normalStyle.Render(cur+textCol) +
					dimStyle.Render(tagsCol) +
					dimStyle.Render(dateCol) + "\n",
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

	var activeTasks, overdueTasks, dueToday, dueThisWeek int
	var doneToday, doneThisWeek, doneThisMonth, doneLastWeek int
	var createdThisWeek int
	var highPri, medPri, lowPri int
	var timeToDone []time.Duration
	var activeAges []time.Duration
	var oldestAge time.Duration
	oldestTitle := ""

	for i := range m.todos {
		t := &m.todos[i]
		if t.ParentID != "" {
			continue
		}
		if !t.CreatedAt.Before(weekAgo) {
			createdThisWeek++
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

	b.WriteString(statsHeaderStyle.Render("  Productivity Stats") + "\n")
	b.WriteString(renderPlainDivider(availW))

	barW := availW - statsLabelWidth - statsValueWidth - 5
	if barW < 0 {
		barW = 0
	}
	if barW > statsBarWidth {
		barW = statsBarWidth
	}

	renderStat := func(label string, value int, total int, showBar bool) {
		labelStr := padRight("  "+label, statsLabelWidth)
		valStr := fmt.Sprintf("%d", value)
		if showBar && total > 0 {
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
			pctStr := fmt.Sprintf(" %3d%%", int(pct*100))
			b.WriteString(detailLabelStyle.Render(labelStr) + normalStyle.Render(padRight(valStr, statsValueWidth)) + bar.String() + dimStyle.Render(pctStr) + "\n")
		} else {
			b.WriteString(detailLabelStyle.Render(labelStr) + normalStyle.Render(valStr) + "\n")
		}
	}

	// ── Workload: what demands attention right now ───────────────────────
	b.WriteString(statsHeaderStyle.Render("  Workload") + "\n")
	if overdueTasks > 0 {
		labelStr := padRight("  Overdue", statsLabelWidth)
		b.WriteString(detailLabelStyle.Render(labelStr) + overdueCountStyle.Render(fmt.Sprintf("%d", overdueTasks)) + "\n")
	} else {
		renderStat("Overdue", 0, 0, false)
	}
	renderStat("Due today", dueToday, 0, false)
	renderStat("Due this week", dueThisWeek, 0, false)
	renderStat("Active total", activeTasks, 0, false)
	b.WriteString("\n")

	// ── Flow: is the backlog growing or shrinking? ───────────────────────
	b.WriteString(statsHeaderStyle.Render("  Flow (last 7 days)") + "\n")
	renderStat("Created", createdThisWeek, 0, false)
	renderStat("Completed", doneThisWeek, 0, false)
	net := createdThisWeek - doneThisWeek
	netLabel := detailLabelStyle.Render(padRight("  Net backlog", statsLabelWidth))
	switch {
	case net > 0:
		b.WriteString(netLabel + overdueCountStyle.Render(fmt.Sprintf("+%d ▲ growing", net)) + "\n")
	case net < 0:
		b.WriteString(netLabel + activeCountStyle.Render(fmt.Sprintf("%d ▼ shrinking", net)) + "\n")
	default:
		b.WriteString(netLabel + dimStyle.Render("±0 → steady") + "\n")
	}
	trendArrow := "→"
	if doneThisWeek > doneLastWeek {
		trendArrow = "↑"
	} else if doneThisWeek < doneLastWeek {
		trendArrow = "↓"
	}
	b.WriteString(detailLabelStyle.Render(padRight("  vs last week", statsLabelWidth)) +
		normalStyle.Render(fmt.Sprintf("%d done vs %d  %s", doneThisWeek, doneLastWeek, trendArrow)) + "\n")
	b.WriteString("\n")

	// ── Throughput: how long do tasks linger? ────────────────────────────
	b.WriteString(statsHeaderStyle.Render("  Throughput") + "\n")
	ttdLabel := detailLabelStyle.Render(padRight("  Time to done (30d)", statsLabelWidth))
	if len(timeToDone) > 0 {
		b.WriteString(ttdLabel + normalStyle.Render("median "+formatDays(medianDuration(timeToDone))) + "\n")
	} else {
		b.WriteString(ttdLabel + dimStyle.Render("no completions yet") + "\n")
	}
	if len(activeAges) > 0 {
		b.WriteString(detailLabelStyle.Render(padRight("  Median active age", statsLabelWidth)) +
			normalStyle.Render(formatDays(medianDuration(activeAges))) + "\n")
		oldestW := availW - statsLabelWidth - 16
		if oldestW < 10 {
			oldestW = 10
		}
		b.WriteString(detailLabelStyle.Render(padRight("  Oldest active", statsLabelWidth)) +
			normalStyle.Render(truncate(oldestTitle, oldestW)) +
			dimStyle.Render("  ("+formatDays(oldestAge)+")") + "\n")
	}
	b.WriteString("\n")

	if activeTasks > 0 {
		b.WriteString(statsHeaderStyle.Render("  Active by priority") + "\n")
		renderStat("↑ High", highPri, activeTasks, true)
		renderStat("→ Medium", medPri, activeTasks, true)
		renderStat("↓ Low", lowPri, activeTasks, true)
		b.WriteString("\n")
	}

	b.WriteString(statsHeaderStyle.Render("  Completion velocity") + "\n")
	renderStat("Today", doneToday, 0, false)
	renderStat("This week", doneThisWeek, 0, false)
	renderStat("This month", doneThisMonth, 0, false)
	if doneThisWeek > 0 {
		avg := fmt.Sprintf("%.1f tasks/day", float64(doneThisWeek)/7.0)
		b.WriteString(detailLabelStyle.Render(padRight("  Avg (7d)", statsLabelWidth)) + normalStyle.Render(avg) + "\n")
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

func formatDays(d time.Duration) string {
	days := d.Hours() / 24
	switch {
	case days < 1:
		return "<1 day"
	case days < 10:
		return fmt.Sprintf("%.1f days", days)
	default:
		return fmt.Sprintf("%.0f days", days)
	}
}

// ── Task lists ────────────────────────────────────────────────────────────────

func (m model) renderTaskList() string {
	active := m.activeTodos()
	if len(active) == 0 {
		if m.searchQuery != "" {
			return normalStyle.Render("  No tasks match your search.")
		}
		if m.focusFilter {
			return normalStyle.Render("  No tasks due today or overdue. Nice!")
		}
		return normalStyle.Render("  No tasks yet. Press 'a' to add one.")
	}

	b := getBuilder()
	defer putBuilder(b)

	renderListHeader(b, m.termWidth, m.cursor, len(active), false, m.taskSort)

	overdueSet := m.cache.overdueSet

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
		b.WriteString(m.renderTaskLineWithSet(t, i, m.cursor, m.pane == paneList, overdueSet))
		if len(t.SubtaskIDs) > 0 && m.expandedTasks[t.ID] {
			for j, subID := range t.SubtaskIDs {
				sub := m.findTodoByID(subID)
				if sub == nil {
					continue
				}
				b.WriteString(m.renderSubtaskLine(sub, j, len(t.SubtaskIDs)))
			}
		}
	}
	return b.String()
}

func (m model) renderHistoryList() string {
	completed := m.completedTodos()
	if len(completed) == 0 {
		if m.searchQuery != "" {
			return normalStyle.Render("  No completed tasks match your search.")
		}
		return normalStyle.Render("  No completed tasks yet.")
	}

	b := getBuilder()
	defer putBuilder(b)

	renderListHeader(b, m.termWidth, m.cursor, len(completed), true, m.taskSort)

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
		b.WriteString(m.renderHistoryLine(completed[i], i, m.cursor, m.pane == paneList))
	}
	return b.String()
}

func (m model) renderHistoryLine(t todo.Todo, index, cursor int, active bool) string {
	cols := taskListCols(m.termWidth, true)
	titleW := cols.titleW
	cursorStr := "  "
	if index == cursor && active {
		cursorStr = "▶ "
	}
	startVal := ""
	if !t.StartDate.IsZero() {
		startVal = t.StartDate.Format("02-01-06")
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
	if cols.showStart {
		dateCols += padRight(startVal, 12)
	}
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

func (m model) renderSubtaskLine(sub *todo.Todo, index, total int) string {
	connector := "├"
	if index == total-1 {
		connector = "└"
	}
	titleW := taskListCols(m.termWidth, false).titleW - 4
	if titleW < 10 {
		titleW = 10
	}
	if sub.Status == todo.Done {
		return dimStyle.Render("     "+connector+" [") + checkDoneStyle.Render("✓") + dimStyle.Render("] "+truncate(sub.Title, titleW)) + "\n"
	}
	return dimStyle.Render("     "+connector+" [ ] "+truncate(sub.Title, titleW)) + "\n"
}

func (m model) renderTaskLineWithSet(t todo.Todo, index, cursor int, active bool, overdueSet map[string]bool) string {
	cols := taskListCols(m.termWidth, false)
	titleW := cols.titleW
	cursorStr := "  "
	if index == cursor && active {
		cursorStr = "▶ "
	}
	checkbox := "[ ]"
	if t.Status == todo.Done {
		checkbox = "[✓]"
	}
	foldIcon := " "
	if len(t.SubtaskIDs) > 0 {
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
		title += " ◉"
	}
	startVal := ""
	if !t.StartDate.IsZero() {
		startVal = t.StartDate.Format("02-01-06")
	}
	dueVal := ""
	if !t.DueDate.IsZero() {
		dueVal = t.DueDate.Format("02-01-06")
	}
	titleCol := padRight(truncate(title, titleW-1), titleW-1)
	tagsPart := m.getRenderedTags(t.Tags)
	line := cursorStr + checkbox + foldIcon + titleCol
	if cols.showStart {
		line += padRight(startVal, 12)
	}
	if cols.showDue {
		line += padRight(dueVal, 12)
	}
	if cols.showLast {
		line += padRight(t.Priority.Icon()+" "+t.Priority.String(), 12)
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
			return normalStyle.Render("  No projects match your search.")
		}
		return normalStyle.Render("  No projects yet. Add a project to a task first.")
	}

	b := getBuilder()
	defer putBuilder(b)

	w := m.termWidth - 8
	projW := m.termWidth * projectColWidthPct / 100
	if projW < minProjColWidth {
		projW = minProjColWidth
	}
	if projW > maxProjColWidth {
		projW = maxProjColWidth
	}

	const prefix = "      "
	headerLeft := prefix + padRight("Project", projW) +
		padRight("Active", projCountColWidth) +
		padRight("Done", projDoneColWidth) + "Overdue"
	padW := w - len([]rune(headerLeft))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")
	b.WriteString(renderPlainDivider(w))

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
			b.WriteString(normalStyle.Render(cursorStr+"• ") + m.textInput.View() + "\n")
			continue
		}
		nameCol := padRight(truncate(p, projW-2), projW)
		activeStr := padRight(fmt.Sprintf("%d active", activeCnt), projCountColWidth)
		doneStr := padRight(fmt.Sprintf("%d done", doneCnt), projDoneColWidth)
		overdueStr := "─"
		if overdueCnt > 0 {
			overdueStr = fmt.Sprintf("%d overdue", overdueCnt)
		}
		switch {
		case i == m.projectCursor:
			line := selectedStyle.Render(cursorStr + "• " + nameCol + activeStr + doneStr)
			if overdueCnt > 0 {
				b.WriteString(line + overdueStyle.Render(overdueStr) + "\n")
			} else {
				b.WriteString(line + selectedStyle.Render(overdueStr) + "\n")
			}
		case activeCnt == 0:
			b.WriteString(doneCountStyle.Render(cursorStr+"• "+nameCol+activeStr+doneStr+overdueStr) + "\n")
		default:
			ovdRendered := dimStyle.Render(overdueStr)
			if overdueCnt > 0 {
				ovdRendered = overdueCountStyle.Render(overdueStr)
			}
			b.WriteString(
				normalStyle.Render(cursorStr+"• "+nameCol) +
					activeCountStyle.Render(activeStr) +
					doneCountStyle.Render(doneStr) +
					ovdRendered + "\n")
		}
	}
	return b.String()
}
