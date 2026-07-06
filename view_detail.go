package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// joinColumns merges two pre-rendered column streams into one block. Each left
// line is padded to leftW cells (counting ANSI escapes correctly via
// ansi.StringWidth) before a `gap`-wide spacer and the matching right line.
// Short columns are padded with blank lines so the join stays aligned.
func joinColumns(left, right string, leftW, gap int) string {
	leftLines := strings.Split(strings.TrimRight(left, "\n"), "\n")
	rightLines := strings.Split(strings.TrimRight(right, "\n"), "\n")
	n := len(leftLines)
	if len(rightLines) > n {
		n = len(rightLines)
	}
	sep := strings.Repeat(" ", gap)
	var b strings.Builder
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		pad := leftW - ansi.StringWidth(l)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(l + strings.Repeat(" ", pad) + sep + r + "\n")
	}
	return b.String()
}

// ── Detail pages ──────────────────────────────────────────────────────────────

func (m model) renderDetailPage1(t *todo.Todo) string {
	b := getBuilder()
	defer putBuilder(b)

	availableW := m.termWidth - 8
	isDetailFocused := m.pane == paneDetail

	// Value width = content width minus label column and cursor prefix.
	valW := availableW - detailLabelColWidth - 2
	if valW < 10 {
		valW = 10
	}

	renderField := func(label, value string, field detailField) string {
		cur := "  "
		isCurrent := isDetailFocused && m.detail.field == field
		if isCurrent {
			cur = "▶ "
		}
		value = truncate(value, valW)
		paddedLabel := detailLabelStyle.Render(padRight(label+":", detailLabelColWidth))
		var v string
		if isCurrent {
			v = detailSelectedStyle.Render(value)
		} else {
			v = detailValueStyle.Render(value)
		}
		return cur + paddedLabel + v
	}

	b.WriteString(detailTitleStyle.Render(truncate(t.Title, availableW)) + "\n\n")

	startVal := tr("not set")
	if !t.StartDate.IsZero() {
		startVal = formatStartDate(t.StartDate)
	}
	dueVal := tr("not set")
	if !t.DueDate.IsZero() {
		dueVal = t.DueDate.Format("02-01-06")
		if t.IsOverdue() {
			dueVal += tr(" ⚠ overdue")
		}
	}
	recurVal := tr("not set")
	if t.Recurrence != "" {
		recurVal = "↻ " + trRecurrence(t.Recurrence)
	}
	projectVal := tr("not set")
	if t.Project != "" {
		projectVal = t.Project
	}
	notesVal := tr("none (press enter or 'n' to edit)")
	if t.Notes != "" {
		lines := strings.SplitN(t.Notes, "\n", 2)
		// Reserve room for the " (…)" multi-line marker (5 cells) so the
		// final value still fits within valW.
		budget := valW
		if len(lines) > 1 {
			budget -= 5
		}
		if budget < 4 {
			budget = 4
		}
		preview := truncate(lines[0], budget)
		if len(lines) > 1 {
			preview += " (…)"
		}
		notesVal = preview
	}

	// Left column: interactive fields the user navigates through.
	left := getBuilder()
	defer putBuilder(left)
	left.WriteString(renderField(tr("Start date"), startVal, fieldStartDate) + "\n")
	left.WriteString(renderField(tr("Due date"), dueVal, fieldDueDate) + "\n")
	left.WriteString(renderField(tr("Recurrence"), recurVal, fieldRecurrence) + "\n")
	left.WriteString(renderField(tr("Priority"), t.Priority.Icon()+" "+trPriority(t.Priority), fieldPriority) + "\n")
	left.WriteString(renderField(tr("Size"), trSize(t.Size), fieldSize) + "\n")
	left.WriteString(renderField(tr("Project"), projectVal, fieldProject) + "\n")
	left.WriteString(renderField(tr("Notes"), notesVal, fieldNotes) + "\n")

	// Right column (or continuation in single-col mode): read-only metadata.
	right := getBuilder()
	defer putBuilder(right)

	roField := func(label string, valueStyle func(string) string, value string) {
		value = truncate(value, valW)
		right.WriteString("  " + detailLabelStyle.Render(padRight(label, detailLabelColWidth)) +
			valueStyle(value) + "\n")
	}
	plainVal := func(s string) string { return detailValueStyle.Render(s) }
	timerVal := func(s string) string { return timerStyle.Render(s) }
	doneVal := func(s string) string { return checkDoneStyle.Render(s) }

	roField(tr("ID:"), plainVal, shortID(t.ID))
	roField(tr("Created:"), plainVal, t.CreatedAt.Format("02-01-06 15:04"))
	roField(tr("Modified:"), plainVal, t.ModifiedAt.Format("02-01-06 15:04"))

	subTime := m.descendantTimeSpent(t.ID)
	if len(t.TimeEntries) > 0 || subTime > 0 {
		own := t.TotalTimeSpent()
		timeVal := fmt.Sprintf(tr("%s (%d entries)"), formatDuration(own), len(t.TimeEntries))
		if subTime > 0 {
			// Show the rolled-up total separately so the user can see what
			// their own logged time was vs. what subtasks added.
			timeVal += fmt.Sprintf(tr("  +%s subtasks = %s"), formatDuration(subTime), formatDuration(own+subTime))
		}
		if t.IsTimerRunning() {
			timeVal += tr(" ◉ tracking")
		}
		roField(tr("Time spent:"), timerVal, timeVal)
	}

	if t.Status == todo.Done && !t.CompletedAt.IsZero() {
		roField(tr("Completed on:"), doneVal, t.CompletedAt.Format("02-01-06 15:04"))
	}

	// Score breakdown: surfaces *why* a task ranks where it does. Shown only
	// on pending tasks because Done always scores 0 and the breakdown would
	// be a row of zeros.
	if t.Status == todo.Pending {
		sc := sequenceComponentsFor(t)
		// Same precision as before but with the ".0" noise trimmed off whole
		// components — the all-%.1f form overflowed valW at a 120-col
		// terminal and truncated the tail of the breakdown to "(…)".
		comp := func(v float64) string {
			return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
		}
		breakdown := fmt.Sprintf(tr("%.1f  (D %s · P %s · M %s · S %s · A %s)"),
			sc.Total, comp(sc.Urgency), comp(sc.Importance), comp(sc.Momentum), comp(sc.Size), comp(sc.Age))
		roField(tr("Score:"), plainVal, breakdown)
	}

	b.WriteString(left.String())
	b.WriteString(right.String())
	b.WriteString("\n")

	tagCur := "  "
	if isDetailFocused && m.detail.field == fieldTags && len(t.Tags) == 0 {
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
	isDetailFocused := m.pane == paneDetail

	// Each row is "  [?] <title>" — 2 cursor cells + 4 bracket+space cells = 6.
	itemW := availableW - 6
	if itemW < 4 {
		itemW = 4
	}

	// Subtasks, then dependencies (+ inbound Blocks), then learnings, one
	// stacked flow. Separate builders keep the section boundaries obvious.
	subB := getBuilder()
	defer putBuilder(subB)
	depB := getBuilder()
	defer putBuilder(depB)
	learnB := getBuilder()
	defer putBuilder(learnB)

	subtaskCur := "  "
	if isDetailFocused && m.detail.field == fieldSubtasks && m.subtaskCount(t.ID) == 0 {
		subtaskCur = "▶ "
	}
	subB.WriteString(subtaskCur + detailLabelStyle.Render(tr("Subtasks:")) + "\n")
	if m.subtaskCount(t.ID) == 0 {
		subB.WriteString("  " + detailValueStyle.Render(tr("No subtasks. Press 'a' to add one.")) + "\n")
	} else {
		for i, subID := range m.subtaskIDs(t.ID) {
			sub := m.findTodoByID(subID)
			pfx := "  "
			isSubSelected := isDetailFocused && m.detail.field == fieldSubtasks && i == m.detail.subtaskCursor
			if isSubSelected {
				pfx = "▶ "
			}
			if sub == nil {
				subB.WriteString(dimStyle.Render(fmt.Sprintf(tr("%s[?] unknown subtask"), pfx)) + "\n")
				continue
			}
			title := truncate(sub.Title, itemW)
			if sub.Status == todo.Done {
				if isSubSelected {
					subB.WriteString(detailSelectedStyle.Render(pfx+"[") + checkDoneStyle.Render("✓") + detailSelectedStyle.Render("] "+title) + "\n")
				} else {
					subB.WriteString(dimStyle.Render(pfx+"[") + checkDoneStyle.Render("✓") + dimStyle.Render("] "+title) + "\n")
				}
			} else {
				line := fmt.Sprintf("%s[ ] %s", pfx, title)
				if isSubSelected {
					subB.WriteString(detailSelectedStyle.Render(line) + "\n")
				} else {
					subB.WriteString(detailValueStyle.Render(line) + "\n")
				}
			}
		}
	}

	inbound := dependentsOf(m.allTodos(), t.ID)
	depCur := "  "
	if isDetailFocused && m.detail.field == fieldDependencies && len(t.Dependencies) == 0 && len(inbound) == 0 {
		depCur = "▶ "
	}
	depB.WriteString(depCur + detailLabelStyle.Render(tr("Dependencies:")) + "\n")
	if len(t.Dependencies) == 0 {
		if len(inbound) == 0 {
			depB.WriteString("  " + detailValueStyle.Render(tr("No dependencies. Press 'a' to add one.")) + "\n")
		}
	} else {
		for i, depID := range t.Dependencies {
			dep := m.findTodoByID(depID)
			pfx := "  "
			isDepSelected := isDetailFocused && m.detail.field == fieldDependencies && i == m.detail.depCursor
			if isDepSelected {
				pfx = "▶ "
			}
			if dep == nil {
				depB.WriteString(dimStyle.Render(fmt.Sprintf(tr("%s[?] unknown task"), pfx)) + "\n")
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
			// ↧ mirrors the list-row "waiting on this" glyph, and the ↥ on
			// the Blocks lines below — outbound vs inbound at a glance.
			line := fmt.Sprintf("%s%s ↧ %s%s", pfx, status, truncate(dep.Title, itemW-len(warn)-2), warn)
			switch {
			case dep.IsOverdue():
				depB.WriteString(overdueStyle.Render(line) + "\n")
			case isDepSelected:
				depB.WriteString(detailSelectedStyle.Render(line) + "\n")
			default:
				depB.WriteString(detailValueStyle.Render(line) + "\n")
			}
		}
	}
	// Inbound edges continue the same list: dimmed ↥ rows are the pending
	// tasks waiting on this one, aligned so ↥ sits under ↧. Selectable for
	// enter-to-jump, but the edge itself is editable only from the other task.
	for i, d := range inbound {
		sel := isDetailFocused && m.detail.field == fieldDependencies &&
			len(t.Dependencies)+i == m.detail.depCursor
		pfx := "  "
		if sel {
			pfx = "▶ "
		}
		line := pfx + "    ↥ " + truncate(d.Title, itemW-2)
		if sel {
			depB.WriteString(detailSelectedStyle.Render(line) + "\n")
		} else {
			depB.WriteString(dimStyle.Render(line) + "\n")
		}
	}

	learningCur := "  "
	if isDetailFocused && m.detail.field == fieldLearnings && len(t.Learnings) == 0 {
		learningCur = "▶ "
	}
	learnB.WriteString(learningCur + detailLabelStyle.Render(tr("Learnings:")) + "\n")
	if len(t.Learnings) == 0 {
		learnB.WriteString("  " + detailValueStyle.Render(tr("No learnings yet. Press 'a' to add one.")) + "\n")
	} else {
		for i, l := range t.Learnings {
			pfx := "  "
			isLearningSelected := isDetailFocused && m.detail.field == fieldLearnings && i == m.detail.learningCursor
			if isLearningSelected {
				pfx = "▶ "
			}
			line := pfx + truncate(l.Text, availableW-4)
			if isLearningSelected {
				learnB.WriteString(learningSelectedStyle.Render(line) + "\n")
			} else {
				learnB.WriteString(learningStyle.Render(line) + "\n")
			}
		}
	}

	b.WriteString(subB.String())
	b.WriteString("\n")
	b.WriteString(depB.String())
	b.WriteString("\n")
	b.WriteString(learnB.String())

	return b.String()
}

func (m model) renderDetailPage3(t *todo.Todo) string {
	b := getBuilder()
	defer putBuilder(b)

	innerW := m.termWidth - 10
	if innerW < minInnerWidth {
		innerW = minInnerWidth
	}
	isDetailFocused := m.pane == paneDetail
	commentCur := "  "
	if isDetailFocused && m.detail.field == fieldComments && len(t.Comments) == 0 {
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
			isSelected := isDetailFocused && m.detail.field == fieldComments && i == m.detail.commentCursor
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
		// The label can be wider than the chart on very narrow terminals (more
		// likely with longer localized strings), which drives insertPos negative;
		// floor it and clip writes to the divider bounds.
		if insertPos < 0 {
			insertPos = 0
		}
		for i, ch := range []rune(todayLabel) {
			if insertPos+i >= chartW {
				break
			}
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
		isSelected := i == m.cursor && m.projectTaskMode
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
