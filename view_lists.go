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
	// Cycle time (start date→completed) of every completed task that had a
	// start date, plus the count of pending tasks, both bucketed by size
	// (index = int(todo.Size)). A task can be created well before it's started,
	// so we measure from StartDate, not CreatedAt; tasks with no start date
	// don't contribute. Feed the "Cycle time by size" and "Projected backlog
	// clear" blocks below.
	var cycleBySize [3][]time.Duration
	var pendingBySize [3]int

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
				if i := int(t.Size); i >= 0 && i < len(cycleBySize) && !t.StartDate.IsZero() {
					// Skip anomalies where completion predates the start date.
					if d := t.CompletedAt.Sub(t.StartDate); d >= 0 {
						cycleBySize[i] = append(cycleBySize[i], d)
					}
				}
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
			if i := int(t.Size); i >= 0 && i < len(pendingBySize) {
				pendingBySize[i]++
			}
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

	// Median cycle time per size, computed once (medianDuration sorts in place)
	// and shared by the cycle-time and projection blocks.
	var medBySize [3]time.Duration
	var haveMed [3]bool
	for i := range cycleBySize {
		if len(cycleBySize[i]) > 0 {
			medBySize[i] = medianDuration(cycleBySize[i])
			haveMed[i] = true
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
		// How often a completed task sat in the engine's top-5 at close —
		// the feedback loop for tuning the sequence biases. Hidden until
		// rank-stamped completions exist.
		if hits, rated := sequenceHitStats(m.allTodos(), seqHitWindow); rated > 0 {
			stat(sb, tr("Seq hit (top-5)"), hits, rated, true)
		}
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

	// Size rows share one order (Small, Medium, Large) across both size blocks.
	sizeRows := []struct {
		label string
		idx   int
	}{
		{tr("Small"), int(todo.SizeSmall)},
		{tr("Medium"), int(todo.SizeMedium)},
		{tr("Large"), int(todo.SizeLarge)},
	}

	// Median calendar time from start date to completion, per size — the "how
	// long does a task of this size actually take once started" cue that feeds
	// the estimate. Only started+completed tasks contribute.
	cycleTime := section(func(sb *strings.Builder) {
		sb.WriteString(statsHeaderStyle.Render(tr("  Cycle time by size")) + "\n")
		for _, s := range sizeRows {
			label := detailLabelStyle.Render(padRight("  "+s.label, statsLabelWidth))
			if haveMed[s.idx] {
				sb.WriteString(label + normalStyle.Render(formatDaysCompact(medBySize[s.idx])) +
					dimStyle.Render(fmt.Sprintf(" (n=%d)", len(cycleBySize[s.idx]))) + "\n")
			} else {
				sb.WriteString(label + dimStyle.Render(tr("none yet")) + "\n")
			}
		}
	})

	// Rough ETA to clear the pending backlog: median cycle time × pending count
	// per size, summed. Serial estimate (assumes one task finished after the
	// next), so it's an upper-bound feel, not a schedule.
	projection := section(func(sb *strings.Builder) {
		sb.WriteString(statsHeaderStyle.Render(tr("  Projected backlog clear")) + "\n")
		var total time.Duration
		haveTotal := false
		for _, s := range sizeRows {
			n := pendingBySize[s.idx]
			if n == 0 {
				continue
			}
			label := detailLabelStyle.Render(padRight("  "+s.label, statsLabelWidth))
			if !haveMed[s.idx] {
				sb.WriteString(label + dimStyle.Render(fmt.Sprintf(tr("%d pending, no pace"), n)) + "\n")
				continue
			}
			sub := time.Duration(n) * medBySize[s.idx]
			total += sub
			haveTotal = true
			sb.WriteString(label + normalStyle.Render(fmt.Sprintf("%d×%s=%s",
				n, formatDaysCompact(medBySize[s.idx]), formatDaysCompact(sub))) + "\n")
		}
		totalLabel := detailLabelStyle.Render(padRight(tr("  Projected clear"), statsLabelWidth))
		if haveTotal {
			sb.WriteString(totalLabel + normalStyle.Render("~"+formatDaysCompact(total)) + "\n")
		} else {
			sb.WriteString(totalLabel + dimStyle.Render(tr("none yet")) + "\n")
		}
	})

	switch cols {
	case 3:
		// Keep the two Flow windows together in the middle column.
		b.WriteString(zipColumns(colW, gap,
			stackSections(workload, throughput, cycleTime),
			stackSections(flow, flow30, projection),
			stackSections(priority, velocity)))
	case 2:
		b.WriteString(zipColumns(colW, gap,
			stackSections(workload, flow, flow30, cycleTime),
			stackSections(throughput, priority, velocity, projection)))
	default:
		first := true
		for _, s := range []string{workload, flow, flow30, throughput, cycleTime, priority, velocity, projection} {
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

	// Column widths (widest row content + widest tag cell) are derived from the
	// active set and cached by refreshTaskColMetrics, so the frame doesn't
	// rescan every task — see cache.go.
	cols := taskListCols(m.termWidth, false, m.cache.activeColContentMax, m.cache.activeColTagsMax)
	renderListHeader(b, m.termWidth, false, cols)

	total := m.visibleActiveLen()
	maxVisible := m.estimateListHeight()
	startIdx := m.listOffset
	if startIdx > total {
		startIdx = 0
	}
	endIdx := startIdx + maxVisible
	if endIdx > total {
		endIdx = total
	}
	// Materialize only the rows we draw, not the whole flattened list.
	window := m.visibleActiveWindow(startIdx, endIdx)

	for i := startIdx; i < endIdx; i++ {
		t := &window[i-startIdx]
		if t.ParentID == "" {
			b.WriteString(m.renderTaskLineWithSet(t, i, m.cursor, true, overdueSet, cols))
			continue
		}
		siblings := m.subtaskIDs(t.ParentID)
		subIdx := 0
		for j, id := range siblings {
			if id == t.ID {
				subIdx = j
				break
			}
		}
		b.WriteString(m.renderSubtaskLine(t, subIdx, len(siblings), cols, i, m.cursor, true))
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
	tagsMax := 0
	for i := range completed {
		if w := len([]rune(completed[i].Title)); w > contentMax {
			contentMax = w
		}
		if tw := tagsRenderWidth(completed[i].Tags); tw > tagsMax {
			tagsMax = tw
		}
	}
	cols := taskListCols(m.termWidth, true, contentMax, tagsMax)
	renderListHeader(b, m.termWidth, true, cols)

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
		b.WriteString(m.renderHistoryLine(completed[i], i, m.cursor, true, cols))
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
	titleCol := padRight(truncate(t.Title, titleW-1), titleW)
	dateCols := ""
	if cols.showDue {
		dateCols += padRight(dueVal, 12)
	}
	if cols.showLast {
		dateCols += padRight(completedVal, 12)
	}
	tagsPart := m.getRenderedTagsForTask(&t)
	mainW := len([]rune(cursorStr)) + 4 + len([]rune(titleCol)) + len([]rune(dateCols))
	tagsStr := ""
	if tagsPart != "" {
		tagsW := tagsRenderWidth(t.Tags)
		if mainW+1+tagsW <= m.termWidth-8 {
			tagsStr = " " + tagsPart
		} else {
			// Trim last 3 chars of titleCol to make room for (…).
			r := []rune(titleCol)
			if len(r) > 3 {
				titleCol = string(r[:len(r)-3]) + dimStyle.Render("(…)")
			}
		}
	}

	if index == cursor && active {
		return fastSelected.render(cursorStr+"[") +
			fastCheckDone.render("✓") +
			fastSelected.render("] "+titleCol+dateCols) +
			tagsStr + "\n"
	}
	return fastNormal.render(cursorStr+"[") +
		fastCheckDone.render("✓") +
		fastNormal.render("] "+titleCol+dateCols) +
		tagsStr + "\n"
}

func (m *model) renderSubtaskLine(sub *todo.Todo, subIndex, subTotal int, cols listCols, flatIndex, cursor int, active bool) string {
	connector := "├"
	if subIndex == subTotal-1 {
		connector = "└"
	}
	titleW := cols.titleW - 4
	if titleW < 10 {
		titleW = 10
	}
	title := truncate(sub.Title, titleW)
	if sub.IsTimerRunning() {
		title = "⏱ " + title
	}
	cursorStr := "  "
	selected := flatIndex == cursor && active
	if selected {
		cursorStr = "▶ "
	}
	check := "[ ]"
	if sub.Status == todo.Done {
		check = "[✓]"
	} else if len(sub.TimeEntries) > 0 {
		check = "[>]"
	}
	body := "   " + connector + " " + check + " " + title

	if selected {
		return fastSelected.render(cursorStr+body) + "\n"
	}
	if sub.Status == todo.Done {
		// Keep ✓ in checkDoneStyle so the done marker stays legible
		// against the surrounding dim row.
		return fastDim.render(cursorStr+"   "+connector+" [") +
			fastCheckDone.render("✓") +
			fastDim.render("] "+title) + "\n"
	}
	return fastDim.render(cursorStr+body) + "\n"
}

func (m *model) renderTaskLineWithSet(t *todo.Todo, index, cursor int, active bool, overdueSet map[string]bool, cols listCols) string {
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
	if m.cache.blockerSet[t.ID] {
		title += " ↥" // others depend on this — clearing it unblocks them
	}
	if m.cache.blockedSet[t.ID] {
		title += " ↧" // waiting on an unfinished dependency
	}
	if t.Notes != "" {
		title += " ¶"
	}
	if t.IsRecurring() {
		title += " ↻"
	}
	if subDone, subTotal := m.subtaskProgress(t.ID); subTotal > 0 {
		// Trailing ‼ marks "something in the subtree is overdue" — distinct
		// from the leading dep-overdue ! so the user can tell which side is
		// on fire.
		badge := fmt.Sprintf(" (%d/%d)", subDone, subTotal)
		if m.hasOverdueDescendant(t.ID, overdueSet) {
			badge += "‼"
		}
		title += badge
	}
	if t.IsTimerRunning() {
		title = "⏱ " + title
	}
	dueVal := ""
	if !t.DueDate.IsZero() {
		dueVal = formatDueShort(t.DueDate, m.frameTime)
	}
	// Reserve one trailing space inside the column so a truncated title (ending
	// in "(…)") never butts up against the Score column that follows.
	titleCol := padRight(truncate(title, titleW-1), titleW)
	tagsPart := m.getRenderedTagsForTask(t)
	line := cursorStr + checkbox + foldIcon + titleCol
	if cols.showLast {
		// Score column is always score now — priority lives only in the
		// detail view, where the user can still set it.
		line += padRight(fmt.Sprintf("%.1f", sequenceScore(t)), scoreColW)
	}
	if cols.showDue {
		line += padRight(dueVal, dueColW)
	}
	if cols.showSize {
		// Asymmetric pad (2 left + letter + 5 right) so the gap from Due to the
		// letter matches the gap from the letter to the Project column — both 5
		// chars, matching the Score→Due rhythm.
		line += "  " + padRight(strings.ToLower(t.Size.Letter()), sizeColW-2)
	}
	if cols.showProject {
		// Truncate at projectColW-4 so the column always leaves ≥4 trailing
		// spaces; combined with the 1-space prefix on tags below that's a 5-char
		// minimum gap between project text and the first tag.
		line += padRight(truncate(t.Project, projectColW-4), projectColW)
	}

	// Only append tags if they fit within the inner panel content width.
	tagsStr := ""
	if tagsPart != "" {
		tagsW := tagsRenderWidth(t.Tags)
		if len([]rune(line))+1+tagsW <= m.termWidth-8 {
			tagsStr = " " + tagsPart
		} else {
			// Overwrite the last 3 chars of the line with (…) so it always fits.
			runes := []rune(line)
			if len(runes) > 3 {
				line = string(runes[:len(runes)-3]) + dimStyle.Render("(…)")
			}
		}
	}

	switch {
	case t.IsTimerRunning():
		return fastTimer.render(line) + tagsStr + "\n"
	case t.IsOverdue():
		return fastOverdue.render(line) + tagsStr + "\n"
	case hasOverdueDep:
		return fastDepOverdue.render(line) + tagsStr + "\n"
	case index == cursor && active:
		return fastSelected.render(line) + tagsStr + "\n"
	default:
		return fastNormal.render(line) + tagsStr + "\n"
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
	// gap=4 matches the Tasks tab title column so non-truncated names leave a
	// 4-char visible gap before the Active column, mirroring the title→score gap.
	// Floor bakes the gap into the header label too — Tasks tab gets away with a
	// bare-header floor because real titles dwarf the "Task" label, but project
	// names are often as short as "Project", so the floor has to enforce the gap
	// or the "Project" / "Active" headers butt up against each other.
	projW := contentFitWidth(m.termWidth, nameMax, 4, len([]rune(projHdr))+4)

	const prefix = "  "
	headerLeft := prefix + padRight(projHdr, projW) +
		padRight(tr("Active"), projCountColWidth) +
		padRight(tr("Done"), projDoneColWidth) + tr("Overdue")
	padW := w - len([]rune(headerLeft))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")

	maxVisible := m.projectListVisibleRows()
	startIdx := m.listOffset
	if startIdx > len(projects) {
		startIdx = 0
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(projects) {
		endIdx = len(projects)
	}
	for i := startIdx; i < endIdx; i++ {
		p := projects[i]
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
		// truncate at projW-1 so a truncated name (ending in "(…)") still leaves
		// 1 trailing space before the Active column — same rule as the title col
		// on the Tasks tab.
		nameCol := padRight(truncate(p, projW-1), projW)
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

// Settings split into two visual columns. Right column clusters the
// score/sequencing knobs so the bias mix reads as one tool; left column holds
// the unrelated app-prefs and system rows.
var settingsLeftCol = []int{
	settingAutoCloseParent,
	settingAutoCloseSubtasks,
	settingTheme,
	settingLanguage,
	settingSyncAuto,
	settingSyncServer,
	settingSyncToken,
	settingSyncNow,
	settingServerOn,
	settingServerListen,
	settingServerToken,
	settingVersion,
	settingCheckUpdate,
}
var settingsRightCol = []int{
	settingBiasDeadline,
	settingBiasPriority,
	settingBiasMomentum,
	settingAging,
}

// twoColumnSettingsMinWidth is the minimum available content width at which the
// Settings tab uses the two-column layout. Below this it falls back to a single
// stacked column so labels and values still fit without wrapping.
const twoColumnSettingsMinWidth = 80

// settingsNavOrder returns the linear up/down traversal order across both
// columns: left column top→bottom, then right column top→bottom.
func settingsNavOrder() []int {
	out := make([]int, 0, len(settingsLeftCol)+len(settingsRightCol))
	out = append(out, settingsLeftCol...)
	out = append(out, settingsRightCol...)
	return out
}

// settingsCursorStep advances the settings cursor by delta along the visual
// traversal order, clamping at the ends so up at the top / down at the bottom
// are no-ops (matching the pre-split behaviour).
func settingsCursorStep(cur, delta int) int {
	order := settingsNavOrder()
	idx := 0
	for i, id := range order {
		if id == cur {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	} else if idx >= len(order) {
		idx = len(order) - 1
	}
	return order[idx]
}

func (m model) renderSettingsList() string {
	b := getBuilder()
	defer putBuilder(b)

	labels := map[int]string{
		settingBiasDeadline:      tr("Deadline pressure"),
		settingBiasPriority:      tr("Priority focus"),
		settingBiasMomentum:      tr("Momentum bias"),
		settingAging:             tr("Aging increases score"),
		settingAutoCloseParent:   tr("Auto-close parent"),
		settingAutoCloseSubtasks: tr("Auto-close subtasks"),
		settingTheme:             tr("Theme"),
		settingLanguage:          tr("Language"),
		settingSyncAuto:          tr("Sync"),
		settingSyncServer:        tr("Sync server"),
		settingSyncToken:         tr("Sync token"),
		settingSyncNow:           tr("Sync now"),
		settingServerOn:          tr("Server"),
		settingServerListen:      tr("Listen"),
		settingServerToken:       tr("Server token"),
		settingVersion:           tr("Version"),
		settingCheckUpdate:       tr("Check for updates"),
	}
	agingVal := tr("Off")
	if activeBiases.Aging {
		agingVal = tr("On")
	}
	autoCloseVal := tr("Off")
	if m.autoCloseParent {
		autoCloseVal = tr("On")
	}
	autoCloseSubsVal := tr("Off")
	if m.autoCloseSubtasks {
		autoCloseSubsVal = tr("On")
	}
	syncAutoVal := "‹ " + tr("needs server") + " ›"
	if m.syncCfg.ready() {
		if m.autoSync {
			syncAutoVal = "‹ " + tr("On") + " ›"
		} else {
			syncAutoVal = "‹ " + tr("Off") + " ›"
		}
	}
	syncServerVal := tr("not set")
	if m.syncCfg.URL != "" {
		syncServerVal = m.syncCfg.URL
	}
	syncTokenVal := tr("not set")
	if m.syncCfg.Token != "" {
		syncTokenVal = "•••• " + tr("set")
	}
	serverState := tr("Off")
	switch {
	case m.inprocServer != nil:
		serverState = tr("On")
	case m.serverExternal:
		serverState = tr("external")
	case m.syncCfg.ServerToken == "":
		serverState = tr("needs token")
	}
	serverTokenVal := tr("not set")
	if m.syncCfg.ServerToken != "" {
		serverTokenVal = "•••• " + tr("set")
	}
	values := map[int]string{
		settingBiasDeadline:      biasPickerValue(activeBiases.Deadline),
		settingBiasPriority:      biasPickerValue(activeBiases.Priority),
		settingBiasMomentum:      biasPickerValue(activeBiases.Momentum),
		settingAging:             "‹ " + agingVal + " ›",
		settingAutoCloseParent:   "‹ " + autoCloseVal + " ›",
		settingAutoCloseSubtasks: "‹ " + autoCloseSubsVal + " ›",
		settingTheme:             "‹ " + m.themeName + " ›",
		settingLanguage:          "‹ " + activeLang.displayName() + " ›",
		settingSyncAuto:          syncAutoVal,
		settingSyncServer:        syncServerVal,
		settingSyncToken:         syncTokenVal,
		settingSyncNow:           tr("press enter to sync"),
		settingServerOn:          "‹ " + serverState + " ›",
		settingServerListen:      m.syncCfg.listenAddr(),
		settingServerToken:       serverTokenVal,
		settingVersion:           appVersion,
		settingCheckUpdate:       tr("press enter to check"),
	}

	maxLabelW := func(ids []int) int {
		w := 0
		for _, id := range ids {
			if n := len([]rune(labels[id])); n > w {
				w = n
			}
		}
		return w + 2
	}

	renderRow := func(id, labelW int) string {
		cursor := "  "
		labelStyle := normalStyle
		if id == m.settingsCursor {
			cursor = selectedStyle.Render("→ ")
			labelStyle = selectedStyle
		}
		return cursor + labelStyle.Render(padRight(labels[id], labelW)) + helpStyle.Render(values[id])
	}

	b.WriteString(headerStyle.Render(tr("Settings")) + "\n\n")

	// Personality summary: what the current bias mix "feels like", so tweaking a
	// single bias gives immediate feedback that the sequence has shifted. It
	// belongs next to the biases that produce it (the right column).
	name, descr := personality(activeBiases)

	availW := m.termWidth - 8
	if availW >= twoColumnSettingsMinWidth {
		gap := 4
		leftW := (availW - gap) / 2
		rightW := availW - leftW - gap
		leftLabelW := maxLabelW(settingsLeftCol)
		rightLabelW := maxLabelW(settingsRightCol)
		var left, right strings.Builder
		for _, id := range settingsLeftCol {
			left.WriteString(renderRow(id, leftLabelW) + "\n")
		}
		for _, id := range settingsRightCol {
			right.WriteString(renderRow(id, rightLabelW) + "\n")
		}
		// Drop the sequence summary directly under the bias controls, wrapped to
		// the column width so it respects the no-wrap contract.
		right.WriteString("\n  " + activeCountStyle.Render(tr("Sequence: ")+name) + "\n")
		for _, line := range wrapText(descr, rightW-4) {
			right.WriteString("    " + helpStyle.Render(line) + "\n")
		}
		b.WriteString(joinColumns(left.String(), right.String(), leftW, gap))
	} else {
		// Narrow layout has no columns, so the summary stays a stacked footer.
		order := settingsNavOrder()
		labelW := maxLabelW(order)
		for _, id := range order {
			b.WriteString(renderRow(id, labelW) + "\n")
		}
		b.WriteString("\n  " + activeCountStyle.Render(tr("Sequence: ")+name) +
			"  " + helpStyle.Render(descr) + "\n")
	}

	if m.updateStatus != "" {
		b.WriteString("\n  " + activeCountStyle.Render(m.updateStatus) + "\n")
	}
	if m.syncStatus != "" {
		b.WriteString("\n  " + helpStyle.Render(m.syncStatus) + "\n")
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
