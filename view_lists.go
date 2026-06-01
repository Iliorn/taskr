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
				tagSelectedStyle.Render(cur+tagLabel)+
					barStr.String()+
					selectedStyle.Render(pctStr)+"\n",
			)
		} else {
			b.WriteString(
				tagStyle.Render(cur+tagLabel)+
					barStr.String()+
					normalStyle.Render(pctStr)+"\n",
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
				learningSelectedStyle.Render(cur+textCol)+
					tagStyle.Render(tagsCol)+
					learningStyle.Render(dateCol)+"\n",
			)
		} else {
			b.WriteString(
				normalStyle.Render(cur+textCol)+
					dimStyle.Render(tagsCol)+
					dimStyle.Render(dateCol)+"\n",
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
	weekAgo := today.AddDate(0, 0, -7)
	monthAgo := today.AddDate(0, -1, 0)

	var totalTasks, activeTasks, doneTasks, overdueTasks int
	var doneToday, doneThisWeek, doneThisMonth int
	var highPri, medPri, lowPri int
	var withNotes, withLearnings int
	projectCounts := make(map[string]int)

	for i := range m.todos {
		t := &m.todos[i]
		if t.ParentID != "" {
			continue
		}
		totalTasks++
		if t.Status == todo.Done {
			doneTasks++
			if !t.CompletedAt.IsZero() {
				if !t.CompletedAt.Before(today) {
					doneToday++
				}
				if !t.CompletedAt.Before(weekAgo) {
					doneThisWeek++
				}
				if !t.CompletedAt.Before(monthAgo) {
					doneThisMonth++
				}
			}
		} else {
			activeTasks++
			if t.IsOverdue() {
				overdueTasks++
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
		if t.Notes != "" {
			withNotes++
		}
		if len(t.Learnings) > 0 {
			withLearnings++
		}
		if t.Project != "" {
			projectCounts[t.Project]++
		}
	}

	availW := m.termWidth - 8
	gradLen := len(statsGradient)

	b.WriteString(statsHeaderStyle.Render("  Productivity Stats") + "\n")
	b.WriteString(renderPlainDivider(availW))

	b.WriteString(statsHeaderStyle.Render("  Overview") + "\n")

	renderStat := func(label string, value int, total int, showBar bool) {
		labelStr := padRight("  "+label, statsLabelWidth)
		valStr := fmt.Sprintf("%d", value)
		if showBar && total > 0 {
			pct := float64(value) / float64(total)
			barW := statsBarWidth
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

	renderStat("Total tasks", totalTasks, 0, false)
	renderStat("Active", activeTasks, totalTasks, true)
	renderStat("Completed", doneTasks, totalTasks, true)
	if overdueTasks > 0 {
		labelStr := padRight("  Overdue", statsLabelWidth)
		b.WriteString(detailLabelStyle.Render(labelStr) + overdueCountStyle.Render(fmt.Sprintf("%d", overdueTasks)) + "\n")
	} else {
		renderStat("Overdue", 0, 0, false)
	}
	b.WriteString("\n")

	b.WriteString(statsHeaderStyle.Render("  Completion velocity") + "\n")
	renderStat("Today", doneToday, 0, false)
	renderStat("This week", doneThisWeek, 0, false)
	renderStat("This month", doneThisMonth, 0, false)
	if doneThisWeek > 0 {
		avg := fmt.Sprintf("%.1f tasks/day", float64(doneThisWeek)/7.0)
		b.WriteString(detailLabelStyle.Render(padRight("  Avg (7d)", statsLabelWidth)) + normalStyle.Render(avg) + "\n")
	}
	b.WriteString("\n")

	if activeTasks > 0 {
		b.WriteString(statsHeaderStyle.Render("  Active by priority") + "\n")
		renderStat("↑ High", highPri, activeTasks, true)
		renderStat("→ Medium", medPri, activeTasks, true)
		renderStat("↓ Low", lowPri, activeTasks, true)
		b.WriteString("\n")
	}

	if len(projectCounts) > 0 {
		b.WriteString(statsHeaderStyle.Render("  Projects") + "\n")
		type projEntry struct {
			name  string
			count int
		}
		entries := make([]projEntry, 0, len(projectCounts))
		for name, count := range projectCounts {
			entries = append(entries, projEntry{name, count})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].count != entries[j].count {
				return entries[i].count > entries[j].count
			}
			return entries[i].name < entries[j].name
		})
		maxShow := 8
		if len(entries) < maxShow {
			maxShow = len(entries)
		}
		for _, e := range entries[:maxShow] {
			labelStr := padRight("  "+truncate(e.name, statsLabelWidth-4), statsLabelWidth)
			b.WriteString(normalStyle.Render(labelStr) + activeCountStyle.Render(fmt.Sprintf("%d tasks", e.count)) + "\n")
		}
		if len(entries) > maxShow {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more projects", len(entries)-maxShow)) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(statsHeaderStyle.Render("  Content") + "\n")
	renderStat("With notes", withNotes, totalTasks, false)
	renderStat("With learnings", withLearnings, totalTasks, false)
	renderStat("Total learnings", len(m.allLearnings()), 0, false)
	renderStat("Tags in use", len(m.getAllTagsSorted()), 0, false)

	return b.String()
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
	titleW := titleColWidth(m.termWidth)
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
	startCol := padRight(startVal, 12)
	dueCol := padRight(dueVal, 12)
	completedCol := padRight(completedVal, 12)
	tagsPart := m.getRenderedTags(t.Tags)

	if index == cursor && active {
		return selectedStyle.Render(cursorStr+"[") +
			checkDoneStyle.Render("✓") +
			selectedStyle.Render("] "+titleCol+startCol+dueCol+completedCol) +
			" " + tagsPart + "\n"
	}
	return normalStyle.Render(cursorStr+"[") +
		checkDoneStyle.Render("✓") +
		normalStyle.Render("] "+titleCol+startCol+dueCol+completedCol) +
		" " + tagsPart + "\n"
}

func (m model) renderSubtaskLine(sub *todo.Todo, index, total int) string {
	connector := "├"
	if index == total-1 {
		connector = "└"
	}
	titleW := titleColWidth(m.termWidth) - 4
	if titleW < 10 {
		titleW = 10
	}
	if sub.Status == todo.Done {
		return dimStyle.Render("     "+connector+" [") + checkDoneStyle.Render("✓") + dimStyle.Render("] "+truncate(sub.Title, titleW)) + "\n"
	}
	return dimStyle.Render("     "+connector+" [ ] "+truncate(sub.Title, titleW)) + "\n"
}

func (m model) renderTaskLineWithSet(t todo.Todo, index, cursor int, active bool, overdueSet map[string]bool) string {
	titleW := titleColWidth(m.termWidth)
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
	startVal := ""
	if !t.StartDate.IsZero() {
		startVal = t.StartDate.Format("02-01-06")
	}
	dueVal := ""
	if !t.DueDate.IsZero() {
		dueVal = t.DueDate.Format("02-01-06")
	}
	titleCol := padRight(truncate(title, titleW-1), titleW-1)
	startCol := padRight(startVal, 12)
	dueCol := padRight(dueVal, 12)
	prioCol := padRight(t.Priority.Icon()+" "+t.Priority.String(), 12)
	tagsPart := m.getRenderedTags(t.Tags)
	line := cursorStr + checkbox + foldIcon + titleCol + startCol + dueCol + prioCol
	switch {
	case t.IsOverdue():
		return overdueStyle.Render(line) + " " + tagsPart + "\n"
	case hasOverdueDep:
		return depOverdueStyle.Render(line) + " " + tagsPart + "\n"
	case index == cursor && active:
		return selectedStyle.Render(line) + " " + tagsPart + "\n"
	default:
		return normalStyle.Render(line) + " " + tagsPart + "\n"
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
				normalStyle.Render(cursorStr+"• "+nameCol)+
					activeCountStyle.Render(activeStr)+
					doneCountStyle.Render(doneStr)+
					ovdRendered+"\n")
		}
	}
	return b.String()
}
