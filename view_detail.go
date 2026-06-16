package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"taskr/todo"
)

// ── Detail pages ──────────────────────────────────────────────────────────────

func (m model) renderDetailPage1(t *todo.Todo) string {
	b := getBuilder()
	defer putBuilder(b)

	availableW := m.termWidth - 8
	isDetailFocused := m.pane == paneDetail && m.detail.page == 0

	renderField := func(label, value string, field detailField) string {
		cur := "  "
		isCurrent := isDetailFocused && m.detail.field == field
		if isCurrent {
			cur = "▶ "
		}
		paddedLabel := detailLabelStyle.Render(padRight(label+":", detailLabelColWidth))
		var v string
		if isCurrent {
			v = detailSelectedStyle.Render(value)
		} else {
			v = detailValueStyle.Render(value)
		}
		return cur + paddedLabel + v
	}

	indicator := "[1/3]"
	titleText := truncate(t.Title, availableW-len(indicator)-2)
	padW := availableW - len([]rune(titleText)) - len([]rune(indicator))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(detailTitleStyle.Render(titleText) +
		strings.Repeat(" ", padW) +
		pageIndicatorStyle.Render(indicator) + "\n\n")

	startVal := tr("not set")
	if !t.StartDate.IsZero() {
		startVal = t.StartDate.Format("02-01-06")
	}
	b.WriteString(renderField(tr("Start date"), startVal, fieldStartDate) + "\n")

	dueVal := tr("not set")
	if !t.DueDate.IsZero() {
		dueVal = t.DueDate.Format("02-01-06")
		if t.IsOverdue() {
			dueVal += tr(" ⚠ overdue")
		}
	}
	b.WriteString(renderField(tr("Due date"), dueVal, fieldDueDate) + "\n")
	b.WriteString(renderField(tr("Priority"), t.Priority.Icon()+" "+trPriority(t.Priority), fieldPriority) + "\n")

	projectVal := tr("not set")
	if t.Project != "" {
		projectVal = t.Project
	}
	b.WriteString(renderField(tr("Project"), projectVal, fieldProject) + "\n")

	notesVal := tr("none (press enter or 'n' to edit)")
	if t.Notes != "" {
		lines := strings.SplitN(t.Notes, "\n", 2)
		preview := truncate(lines[0], availableW-detailLabelColWidth-6)
		if len(lines) > 1 {
			preview += " …"
		}
		notesVal = preview
	}
	b.WriteString(renderField(tr("Notes"), notesVal, fieldNotes) + "\n")

	b.WriteString("  " + detailLabelStyle.Render(padRight(tr("Created:"), detailLabelColWidth)) +
		detailValueStyle.Render(t.CreatedAt.Format("02-01-06 15:04")) + "\n")
	b.WriteString("  " + detailLabelStyle.Render(padRight(tr("Modified:"), detailLabelColWidth)) +
		detailValueStyle.Render(t.ModifiedAt.Format("02-01-06 15:04")) + "\n")

	if len(t.TimeEntries) > 0 {
		timeVal := fmt.Sprintf(tr("%s (%d entries)"), formatDuration(t.TotalTimeSpent()), len(t.TimeEntries))
		if t.IsTimerRunning() {
			timeVal += tr(" ◉ tracking")
		}
		b.WriteString("  " + detailLabelStyle.Render(padRight(tr("Time spent:"), detailLabelColWidth)) +
			timerStyle.Render(timeVal) + "\n")
	}

	if t.Status == todo.Done && !t.CompletedAt.IsZero() {
		b.WriteString("  " + detailLabelStyle.Render(padRight(tr("Completed on:"), detailLabelColWidth)) +
			checkDoneStyle.Render(t.CompletedAt.Format("02-01-06 15:04")) + "\n")
	}
	b.WriteString("\n")

	tagCur := "  "
	if isDetailFocused && m.detail.field == fieldTags {
		tagCur = "▶ "
	}
	b.WriteString(tagCur + detailLabelStyle.Render(tr("Tags:")) + "\n")
	if len(t.Tags) == 0 {
		b.WriteString("  " + detailValueStyle.Render(tr("No tags. Press 'a' to add one.")) + "\n")
	} else {
		for i, tag := range t.Tags {
			pfx := "  "
			if isDetailFocused && m.detail.field == fieldTags && i == m.detail.tagCursor {
				pfx = "▶ "
				b.WriteString(detailSelectedStyle.Render(pfx) + tagStyle.Render("⟨#"+tag+"⟩") + "\n")
			} else {
				b.WriteString(dimStyle.Render(pfx) + tagStyle.Render("⟨#"+tag+"⟩") + "\n")
			}
		}
	}

	return b.String()
}

func (m model) renderDetailPage2(t *todo.Todo) string {
	b := getBuilder()
	defer putBuilder(b)

	availableW := m.termWidth - 8
	isDetailFocused := m.pane == paneDetail && m.detail.page == 1

	indicator := "[2/3]"
	titleText := truncate(t.Title, availableW-len(indicator)-2)
	padW := availableW - len([]rune(titleText)) - len([]rune(indicator))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(detailTitleStyle.Render(titleText) +
		strings.Repeat(" ", padW) +
		pageIndicatorStyle.Render(indicator) + "\n\n")

	subtaskCur := "  "
	if isDetailFocused && m.detail.field == fieldSubtasks {
		subtaskCur = "▶ "
	}
	b.WriteString(subtaskCur + detailLabelStyle.Render(tr("Subtasks:")) + "\n")
	if len(t.SubtaskIDs) == 0 {
		b.WriteString("  " + detailValueStyle.Render(tr("No subtasks. Press 'a' to add one.")) + "\n")
	} else {
		for i, subID := range t.SubtaskIDs {
			sub := m.findTodoByID(subID)
			pfx := "  "
			isSubSelected := isDetailFocused && m.detail.field == fieldSubtasks && i == m.detail.subtaskCursor
			if isSubSelected {
				pfx = "▶ "
			}
			if sub == nil {
				b.WriteString(dimStyle.Render(fmt.Sprintf(tr("%s[?] unknown subtask"), pfx)) + "\n")
				continue
			}
			if sub.Status == todo.Done {
				if isSubSelected {
					b.WriteString(detailSelectedStyle.Render(pfx+"[") + checkDoneStyle.Render("✓") + detailSelectedStyle.Render("] "+truncate(sub.Title, availableW-8)) + "\n")
				} else {
					b.WriteString(dimStyle.Render(pfx+"[") + checkDoneStyle.Render("✓") + dimStyle.Render("] "+truncate(sub.Title, availableW-8)) + "\n")
				}
			} else {
				line := fmt.Sprintf("%s[ ] %s", pfx, truncate(sub.Title, availableW-8))
				if isSubSelected {
					b.WriteString(detailSelectedStyle.Render(line) + "\n")
				} else {
					b.WriteString(detailValueStyle.Render(line) + "\n")
				}
			}
		}
	}
	b.WriteString("\n")

	depCur := "  "
	if isDetailFocused && m.detail.field == fieldDependencies {
		depCur = "▶ "
	}
	b.WriteString(depCur + detailLabelStyle.Render(tr("Dependencies:")) + "\n")
	if len(t.Dependencies) == 0 {
		b.WriteString("  " + detailValueStyle.Render(tr("No dependencies. Press 'a' to add one.")) + "\n")
	} else {
		for i, depID := range t.Dependencies {
			dep := m.findTodoByID(depID)
			pfx := "  "
			isDepSelected := isDetailFocused && m.detail.field == fieldDependencies && i == m.detail.depCursor
			if isDepSelected {
				pfx = "▶ "
			}
			if dep == nil {
				b.WriteString(dimStyle.Render(fmt.Sprintf(tr("%s[?] unknown task"), pfx)) + "\n")
				continue
			}
			status := "[ ]"
			if dep.Status == todo.Done {
				status = "[✓]"
			}
			warn := ""
			if dep.IsOverdue() {
				warn = " !"
			}
			line := fmt.Sprintf("%s%s %s%s", pfx, status, dep.Title, warn)
			switch {
			case dep.IsOverdue():
				b.WriteString(overdueStyle.Render(line) + "\n")
			case isDepSelected:
				b.WriteString(detailSelectedStyle.Render(line) + "\n")
			default:
				b.WriteString(detailValueStyle.Render(line) + "\n")
			}
		}
	}
	b.WriteString("\n")

	learningCur := "  "
	if isDetailFocused && m.detail.field == fieldLearnings {
		learningCur = "▶ "
	}
	b.WriteString(learningCur + detailLabelStyle.Render(tr("Learnings:")) + "\n")
	if len(t.Learnings) == 0 {
		b.WriteString("  " + detailValueStyle.Render(tr("No learnings yet. Press 'a' to add one.")) + "\n")
	} else {
		for i, l := range t.Learnings {
			pfx := "  "
			isLearningSelected := isDetailFocused && m.detail.field == fieldLearnings && i == m.detail.learningCursor
			if isLearningSelected {
				pfx = "▶ "
			}
			line := pfx + truncate(l.Text, availableW-4)
			if isLearningSelected {
				b.WriteString(learningSelectedStyle.Render(line) + "\n")
			} else {
				b.WriteString(learningStyle.Render(line) + "\n")
			}
		}
	}

	return b.String()
}

func (m model) renderDetailPage3(t *todo.Todo) string {
	b := getBuilder()
	defer putBuilder(b)

	availableW := m.termWidth - 8
	innerW := m.termWidth - 10
	if innerW < minInnerWidth {
		innerW = minInnerWidth
	}
	indicator := "[3/3]"
	titleText := truncate(t.Title, availableW-len(indicator)-2)
	padW := availableW - len([]rune(titleText)) - len([]rune(indicator))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(detailTitleStyle.Render(titleText) +
		strings.Repeat(" ", padW) +
		pageIndicatorStyle.Render(indicator) + "\n\n")
	isDetailFocused := m.pane == paneDetail && m.detail.page == 2
	commentCur := "  "
	if isDetailFocused {
		commentCur = "▶ "
	}
	b.WriteString(commentCur + detailLabelStyle.Render(tr("Comments:")) + "\n")
	if len(t.Comments) == 0 {
		b.WriteString("  " + detailValueStyle.Render(tr("No comments yet. Press 'a' to add one.")) + "\n")
	} else {
		available := innerW - commentPrefixLen
		if available < 10 {
			available = 10
		}
		for i, c := range t.Comments {
			isSelected := isDetailFocused && i == m.detail.commentCursor
			pfx := "  "
			if isSelected {
				pfx = "▶ "
			}
			header := fmt.Sprintf("%s[%s] ", pfx, c.CreatedAt.Format("02-01-06 15:04"))
			wrapped := wrapText(c.Text, available)
			indent := strings.Repeat(" ", len([]rune(header)))
			for j, line := range wrapped {
				var fullLine string
				if j == 0 {
					fullLine = header + line
				} else {
					fullLine = indent + line
				}
				if isSelected {
					b.WriteString(detailSelectedStyle.Render(fullLine) + "\n")
				} else {
					b.WriteString(detailValueStyle.Render(fullLine) + "\n")
				}
			}
		}
	}
	return b.String()
}

// ── Gantt ─────────────────────────────────────────────────────────────────────

func (m model) renderGantt(tasks []todo.Todo) string {
	if len(tasks) == 0 {
		return dimStyle.Render(tr("  No tasks in this project."))
	}
	today := m.frameTime
	var minDate, maxDate time.Time
	for _, t := range tasks {
		if !t.StartDate.IsZero() && (minDate.IsZero() || t.StartDate.Before(minDate)) {
			minDate = t.StartDate
		}
		if !t.DueDate.IsZero() && (maxDate.IsZero() || t.DueDate.After(maxDate)) {
			maxDate = t.DueDate
		}
	}
	if minDate.IsZero() {
		minDate = today.AddDate(0, 0, -7)
	}
	if maxDate.IsZero() {
		maxDate = today.AddDate(0, 1, 0)
	}
	if !maxDate.After(minDate) {
		maxDate = minDate.AddDate(0, 0, 14)
	}

	labelW := m.termWidth / ganttLabelWidthDivisor
	if labelW < minGanttLabelWidth {
		labelW = minGanttLabelWidth
	}
	if labelW > maxGanttLabelWidth {
		labelW = maxGanttLabelWidth
	}

	chartW := m.termWidth - labelW - ganttSuffixWidth - ganttChartPadding
	if chartW < minChartWidth {
		chartW = minChartWidth
	}

	totalDays := maxDate.Sub(minDate).Hours() / 24
	if totalDays < 1 {
		totalDays = 1
	}
	todayPos := int(math.Round(today.Sub(minDate).Hours() / 24 * float64(chartW) / totalDays))
	if todayPos < 0 || todayPos >= chartW {
		todayPos = -1
	}

	b := getBuilder()
	defer putBuilder(b)

	leftDate := minDate.Format("02-01")
	rightDate := maxDate.Format("02-01")
	innerSpaces := chartW - len(leftDate) - len(rightDate)
	if innerSpaces < 1 {
		innerSpaces = 1
	}
	timelineHeader := leftDate + strings.Repeat(" ", innerSpaces) + rightDate
	headerLabel := padRight(tr("  Timeline"), labelW)
	b.WriteString(headerStyle.Render(headerLabel+timelineHeader) + "\n")

	todayLabel := tr("today:") + today.Format("02-01")
	divider := make([]rune, chartW)
	for i := range divider {
		divider[i] = '─'
	}
	if todayPos >= 0 {
		insertPos := todayPos - len([]rune(todayLabel))/2
		if insertPos < 0 {
			insertPos = 0
		}
		if insertPos+len([]rune(todayLabel)) > chartW {
			insertPos = chartW - len([]rune(todayLabel))
		}
		for i, ch := range []rune(todayLabel) {
			divider[insertPos+i] = ch
		}
	}
	b.WriteString(dimStyle.Render("  "+strings.Repeat("─", labelW-2)) +
		ganttTodayStyle.Render(string(divider)) + "\n")

	gradLen := len(ganttGradient)
	ovrdLen := len(ganttOverdueGradient)

	bufs := getGanttBuffers(chartW)
	defer putGanttBuffers(bufs)
	barRunes := bufs.bar[:chartW]
	barColors := bufs.color[:chartW]

	for i, t := range tasks {
		isSelected := i == m.cursor && m.pane == paneList && m.projectTaskMode
		checkbox := "[ ]"
		if t.Status == todo.Done {
			checkbox = "[✓]"
		}
		titleTrunc := labelW - 6
		if titleTrunc < 5 {
			titleTrunc = 5
		}
		label := checkbox + " " + padRight(truncate(t.Title, titleTrunc), titleTrunc) + " |"

		for j := range barRunes {
			barRunes[j] = ' '
			barColors[j] = -1
		}

		hasDates := !t.StartDate.IsZero() && !t.DueDate.IsZero()
		if hasDates {
			startPos := int(math.Round(t.StartDate.Sub(minDate).Hours() / 24 * float64(chartW) / totalDays))
			endPos := int(math.Round(t.DueDate.Sub(minDate).Hours() / 24 * float64(chartW) / totalDays))
			if startPos < 0 {
				startPos = 0
			}
			if endPos > chartW {
				endPos = chartW
			}
			barLen := endPos - startPos
			if barLen < 1 {
				barLen = 1
			}
			isOverdue := t.IsOverdue()
			isDone := t.Status == todo.Done
			for j := startPos; j < endPos && j < chartW; j++ {
				barRunes[j] = '█'
				var pos float64
				if barLen > 1 {
					pos = float64(j-startPos) / float64(barLen-1)
				}
				gradIdx := int(pos * float64(gradLen-1))
				if gradIdx >= gradLen {
					gradIdx = gradLen - 1
				}
				switch {
				case isDone:
					barColors[j] = 99
				case isOverdue:
					idx := int(pos * float64(ovrdLen-1))
					if idx >= ovrdLen {
						idx = ovrdLen - 1
					}
					barColors[j] = 200 + idx
				default:
					barColors[j] = gradIdx
				}
			}
		}
		if todayPos >= 0 && todayPos < chartW {
			barRunes[todayPos] = '│'
			barColors[todayPos] = -2
		}
		datesSuffix := "|"
		if hasDates {
			datesSuffix = fmt.Sprintf("| %s→%s", t.StartDate.Format("02-01"), t.DueDate.Format("02-01"))
		}

		renderBar := func() {
			j := 0
			for j < chartW {
				colorIdx := barColors[j]
				start := j
				for j < chartW && barColors[j] == colorIdx {
					j++
				}
				group := string(barRunes[start:j])
				switch {
				case colorIdx == -1:
					b.WriteString(group)
				case colorIdx == -2:
					b.WriteString(ganttTodayStyle.Render(group))
				case colorIdx == 99:
					b.WriteString(ganttDoneStyle.Render(group))
				case colorIdx >= 200:
					b.WriteString(ganttOverdueGradient[colorIdx-200].Render(group))
				default:
					b.WriteString(ganttGradient[colorIdx].Render(group))
				}
			}
		}
		if isSelected {
			b.WriteString(selectedStyle.Render(label))
			renderBar()
			b.WriteString(selectedStyle.Render(datesSuffix) + "\n")
		} else {
			b.WriteString(label)
			renderBar()
			b.WriteString(datesSuffix + "\n")
		}
	}
	return b.String()
}
