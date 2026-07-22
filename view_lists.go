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

// tagBarEighths maps a sub-cell fill index (0–7) to the corresponding Unicode
// block element. Index 0 = empty (not used at the fill boundary), 1 = ▏ (1/8),
// …, 7 = ▉ (7/8). A full cell (index 8) uses █ in the main fill loop.
var tagBarEighths = [8]string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

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

	// Right-aligned numeric columns after the bar: Done (done/total), Age (avg
	// age of open tasks), Time (total tracked). Values are formatted for every
	// filtered tag — not just the visible window — so column widths hold steady
	// while scrolling. On narrow terminals whole columns are dropped right to
	// left: a missing column reads better than a value chopped mid-word.
	type tagRow struct {
		s                tagStats
		label            string
		done, age, spent string
	}
	rows := make([]tagRow, len(tags))
	doneHdr, ageHdr, timeHdr := tr("Done"), tr("Age"), tr("Time")
	doneW := len([]rune(doneHdr))
	ageW := len([]rune(ageHdr))
	timeW := len([]rune(timeHdr))
	for i, tag := range tags {
		r := tagRow{label: "#" + tag}
		if tag == untaggedKey {
			// The virtual row only triages counts; age/time stay blank.
			r.s = tagStats{total: m.cache.untaggedTotal, done: m.cache.untaggedDone}
			r.label = tr("(untagged)")
		} else {
			r.s = stats[tag]
			r.age, r.spent = "—", "—"
			if r.s.openCount > 0 {
				r.age = formatDaysCompact(r.s.ageSum / time.Duration(r.s.openCount))
			}
			if r.s.tracked > 0 {
				r.spent = formatDurationCompact(r.s.tracked)
			}
		}
		r.done = fmt.Sprintf("%d/%d", r.s.done, r.s.total)
		doneW = max(doneW, len([]rune(r.done)))
		ageW = max(ageW, len([]rune(r.age)))
		timeW = max(timeW, len([]rune(r.spent)))
		rows[i] = r
	}

	const pctW = 5 // " 100%"
	const colGap = 2
	avail := m.termWidth - 8

	// Determine which optional data columns fit using the minimum bar width,
	// then expand the bar to claim whatever space is left over so the bar
	// fills the full available pane width instead of leaving dead space.
	usedMin := nameW + minTagBarWidth + pctW
	showDone := usedMin+colGap+doneW <= avail
	if showDone {
		usedMin += colGap + doneW
	}
	showAge := showDone && usedMin+colGap+ageW <= avail
	if showAge {
		usedMin += colGap + ageW
	}
	showTime := showAge && usedMin+colGap+timeW <= avail
	if showTime {
		usedMin += colGap + timeW
	}
	// barW = all remaining space after fixed and shown columns; floor at minimum.
	barW := avail - (usedMin - minTagBarWidth)
	if barW < minTagBarWidth {
		barW = minTagBarWidth
	}

	headerLeft := padRight(tagHdr, nameW) + padRight(tr("Progress"), barW+pctW)
	if showDone {
		headerLeft += strings.Repeat(" ", colGap) + padLeft(doneHdr, doneW)
	}
	if showAge {
		headerLeft += strings.Repeat(" ", colGap) + padLeft(ageHdr, ageW)
	}
	if showTime {
		headerLeft += strings.Repeat(" ", colGap) + padLeft(timeHdr, timeW)
	}
	padW := m.termWidth - 6 - len([]rune(headerLeft))
	if padW < 1 {
		padW = 1
	}
	b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")

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
	barStr.Grow(barW * 6) // extra headroom for partial-block glyph (3 bytes)

	for i := startIdx; i < endIdx; i++ {
		tag := tags[i]
		r := rows[i]
		total, done := r.s.total, r.s.done

		pct := 0.0
		if total > 0 {
			pct = float64(done) / float64(total)
		}
		// Compute fill at 1/8-cell resolution: filledEighths counts total
		// eighth-block steps, so filled full cells = filledEighths/8 and the
		// partial-boundary cell uses tagBarEighths[filledEighths%8].
		filledEighths := int(math.Round(pct * float64(barW) * 8))
		if filledEighths > barW*8 {
			filledEighths = barW * 8
		}
		filled := filledEighths / 8
		partialEighths := filledEighths % 8
		cur := "  "
		if i == m.tagTabCursor {
			cur = "▶ "
		}
		tagLabel := padRight(truncate(r.label, nameW-4), nameW-2)

		barStr.Reset()
		// Group consecutive full cells that share a gradient color into a single
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
		// Partial-fill boundary cell: use the gradient color at the filled
		// position so the sub-cell glyph blends with the full cells beside it.
		if partialEighths > 0 && filled < barW {
			pos := 0.0
			if filled > 0 {
				pos = float64(filled) / float64(barW)
			}
			gradIdx := int(pos * float64(gradLen-1))
			if gradIdx >= gradLen {
				gradIdx = gradLen - 1
			}
			barStr.WriteString(tagProgressGradient[gradIdx].Render(tagBarEighths[partialEighths]))
			// Remaining empty cells: one fewer because the partial cell occupies a slot.
			empty := barW - filled - 1
			if empty > 0 {
				barStr.WriteString(dimStyle.Render(strings.Repeat("░", empty)))
			}
		} else if filled < barW {
			barStr.WriteString(dimStyle.Render(strings.Repeat("░", barW-filled)))
		}

		if m.mode == modeEditTag && m.editingTagName == tag {
			b.WriteString(tagSelectedStyle.Render(cur+tagLabel) + m.textInput.View() + "\n")
			continue
		}

		right := fmt.Sprintf(" %3d%%", int(pct*100))
		if showDone {
			right += strings.Repeat(" ", colGap) + padLeft(r.done, doneW)
		}
		if showAge {
			right += strings.Repeat(" ", colGap) + padLeft(r.age, ageW)
		}
		if showTime {
			right += strings.Repeat(" ", colGap) + padLeft(r.spent, timeW)
		}
		if i == m.tagTabCursor {
			b.WriteString(
				tagSelectedStyle.Render(cur+tagLabel) +
					barStr.String() +
					selectedStyle.Render(right) + "\n",
			)
		} else {
			b.WriteString(
				tagStyle.Render(cur+tagLabel) +
					barStr.String() +
					normalStyle.Render(right) + "\n",
			)
		}
	}
	return b.String()
}

// ── Learnings list ────────────────────────────────────────────────────────────

// ── Stats ─────────────────────────────────────────────────────────────────────

// statsScopedTodos returns the task set the Stats tab aggregates: everything
// when no search is active, otherwise the top-level tasks matching the query
// (same compileSearch grammar as the Tasks list — #tag, @project, free text).
// Subtasks are dropped from the filtered form; every stats bucket reads
// top-level rows anyway, and the active `/query` chip already tells the user
// the page is scoped.
func (m model) statsScopedTodos() []todo.Todo {
	all := m.allTodos()
	if m.searchQuery == "" {
		return all
	}
	match := compileSearch(m.searchQuery)
	scoped := make([]todo.Todo, 0, len(all))
	for _, t := range all {
		if t.ParentID == "" && match(t) {
			scoped = append(scoped, t)
		}
	}
	return scoped
}

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

	scope := m.statsScopedTodos()
	for i := range scope {
		t := &scope[i]
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
		if hits, rated := sequenceHitStats(scope, seqHitWindow); rated > 0 {
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
	cols := taskListCols(m.termWidth, false, m.cache.activeColContentMax, m.cache.activeColTagsMax, m.cache.activeColHasDue, dueColMax(m.cache.active, m.frameTime), m.cache.activeColProjectMax)
	total := m.visibleActiveLen()
	// Cursor/total and sort status are shown in the Overview border title.
	renderListHeader(b, m.termWidth, false, cols, "")

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
	hasDue := false
	for i := range completed {
		if w := len([]rune(completed[i].Title)); w > contentMax {
			contentMax = w
		}
		if tw := tagsRenderWidth(completed[i].Tags); tw > tagsMax {
			tagsMax = tw
		}
		if !completed[i].DueDate.IsZero() {
			hasDue = true
		}
	}
	// dueMax (0) is ignored for history — it forces its fixed 12-wide date column.
	cols := taskListCols(m.termWidth, true, contentMax, tagsMax, hasDue, 0, 0)
	// Cursor/total and sort status are shown in the History border title.
	renderListHeader(b, m.termWidth, true, cols, "")

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
	selected := index == cursor && active
	if selected {
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
		dateCols += padRight(dueVal, cols.dueW)
	}
	if cols.showLast {
		dateCols += padRight(completedVal, 12)
	}
	rowTail := titleCol + dateCols
	tagsPart := m.getRenderedTagsForTask(&t)
	mainW := len([]rune(cursorStr)) + 4 + len([]rune(rowTail))
	tagsStr := ""
	if tagsPart != "" {
		tagsW := tagsRenderWidth(t.Tags)
		if mainW+1+tagsW <= m.termWidth-8 {
			if selected {
				tagsStr = renderSelectedTaskTagsPart(t.Tags)
			} else {
				tagsStr = " " + tagsPart
			}
		} else {
			// Render the omission marker as the Tags-column value, in tag colour.
			// The fixed cursor+checkbox prefix occupies six cells and is rendered
			// separately below so the done checkmark can keep its own colour.
			rowTail, tagsStr = renderTaskTagOverflow(rowTail, m.termWidth-8-6, selected)
		}
	}

	if selected {
		return fastSelectedRow.render(cursorStr+"[") +
			fastCheckDone.render("✓") +
			fastSelectedRow.render("] "+rowTail) +
			tagsStr + "\n"
	}
	return fastNormal.render(cursorStr+"[") +
		fastCheckDone.render("✓") +
		fastNormal.render("] "+rowTail) +
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
		title = "⧗ " + title
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
		return fastSelectedRow.render(cursorStr+body) + "\n"
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
	selected := index == cursor && active
	if selected {
		cursorStr = "▶ "
	}
	checkbox := "[ ]"
	if t.Status == todo.Done {
		checkbox = "[✓]"
	} else if len(t.TimeEntries) > 0 {
		checkbox = "[>]"
	} else if m.cache.blockedSet[t.ID] {
		checkbox = "[~]" // blocked: waiting on an unfinished dependency
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
	if t.Priority == todo.PriorityHigh {
		title += " !"
	}
	// hasOverdueDep drives the row color (the switch at the end), not a glyph.
	hasOverdueDep := t.HasOverdueDependencyFast(overdueSet)
	if m.cache.blockerSet[t.ID] {
		title += " ↥" // others depend on this — clearing it unblocks them
	}
	if m.cache.blockedSet[t.ID] {
		title += " ↧" // waiting on an unfinished dependency
	}
	if t.IsRecurring() {
		title += " ↻"
	}
	if subDone, subTotal := m.subtaskProgress(t.ID); subTotal > 0 {
		title += fmt.Sprintf(" (%d/%d)", subDone, subTotal)
	}
	if t.IsTimerRunning() {
		title = "⧗ " + title
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
		line += padRight(dueVal, cols.dueW)
	}
	if cols.showSize {
		// Asymmetric pad (2 left + letter + 5 right) so the gap from Due to the
		// letter matches the gap from the letter to the Project column — both 5
		// chars, matching the Score→Due rhythm.
		line += "  " + padRight(strings.ToLower(t.Size.Letter()), sizeColW-2)
	}
	if cols.showProject {
		// Truncate at projectW-4 so the column always leaves ≥4 trailing
		// spaces; combined with the 1-space prefix on tags below that's a 5-char
		// minimum gap between project text and the first tag.
		line += padRight(truncate(t.Project, cols.projectW-4), cols.projectW)
	}

	// Only append tags if they fit within the inner panel content width.
	tagsStr := ""
	if tagsPart != "" {
		tagsW := tagsRenderWidth(t.Tags)
		if len([]rune(line))+1+tagsW <= m.termWidth-8 {
			if selected {
				tagsStr = renderSelectedTaskTagsPart(t.Tags)
			} else {
				tagsStr = " " + tagsPart
			}
		} else {
			// The omission marker is a Tags-column value, not part of whichever
			// preceding column happened to be last, so it keeps the tag colour.
			line, tagsStr = renderTaskTagOverflow(line, m.termWidth-8, selected)
		}
	}

	// Status colour owns the foreground; selection owns the background — so a
	// selected overdue/timer row shows both instead of the status masking the
	// cursor.
	var st fastStyle
	switch {
	case t.IsTimerRunning() && selected:
		st = fastSelectedTimer
	case t.IsTimerRunning():
		st = fastTimer
	case t.IsOverdue() && selected:
		st = fastSelectedOverdue
	case t.IsOverdue():
		st = fastOverdue
	case hasOverdueDep && selected:
		st = fastSelectedDepOverdue
	case hasOverdueDep:
		st = fastDepOverdue
	case selected:
		st = fastSelectedRow
	default:
		st = fastNormal
	}
	return st.render(line) + tagsStr + "\n"
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

// renderProjectDrillTaskList renders the task-list panel shown in the left
// column of the drilled-in Projects view. It reuses renderTaskLineWithSet so
// rows look identical to the Tasks tab: same checkbox, priority glyphs, status
// colours, and cursor marker. m.termWidth is already narrowed to the column's
// share by the caller so taskListCols and no-wrap math apply per column.
func (m model) renderProjectDrillTaskList(tasks []todo.Todo) []string {
	if len(tasks) == 0 {
		return []string{dimStyle.Render(tr("  No tasks in this project."))}
	}

	b := getBuilder()
	defer putBuilder(b)

	overdueSet := m.cache.overdueSet

	// Compute column widths from this project's tasks, not the full active set.
	contentMax, tagsMax, projectMax := 0, 0, 0
	hasDue := false
	for i := range tasks {
		if w := len([]rune(tasks[i].Title)); w > contentMax {
			contentMax = w
		}
		if tw := tagsRenderWidth(tasks[i].Tags); tw > tagsMax {
			tagsMax = tw
		}
		if !tasks[i].DueDate.IsZero() {
			hasDue = true
		}
		if pw := len([]rune(tasks[i].Project)); pw > projectMax {
			projectMax = pw
		}
	}
	cols := taskListCols(m.termWidth, false, contentMax, tagsMax, hasDue, dueColMax(tasks, m.frameTime), projectMax)
	renderListHeader(b, m.termWidth, false, cols, listPosLabel(m.cursor, len(tasks)))

	// Use projectDrillTaskVisibleRows (= listVisible()-1) to match the clamp
	// window exactly. Both sides read the same helper so an off-by-one is
	// impossible: the header row is already accounted for in the helper.
	maxVisible := m.projectDrillTaskVisibleRows()
	startIdx := m.listOffset
	if startIdx > len(tasks) {
		startIdx = 0
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(tasks) {
		endIdx = len(tasks)
	}

	for i := startIdx; i < endIdx; i++ {
		t := tasks[i]
		b.WriteString(m.renderTaskLineWithSet(&t, i, m.cursor, true, overdueSet, cols))
	}

	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
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
		// Live preview: show the top-N tasks ranked with the current knob values
		// so the user can see the effect without switching tabs.
		if preview := m.renderSettingsTopPreview(activeBiases, activeHeat, m.frameTime, rightW); preview != "" {
			right.WriteString(preview)
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
		if preview := m.renderSettingsTopPreview(activeBiases, activeHeat, m.frameTime, availW); preview != "" {
			b.WriteString(preview)
		}
	}

	if m.updateStatus != "" {
		b.WriteString("\n  " + activeCountStyle.Render(m.updateStatus) + "\n")
	}
	if m.syncStatus != "" {
		b.WriteString("\n  " + helpStyle.Render(m.syncStatus) + "\n")
	}
	return b.String()
}

// settingsPreviewN is the number of ranked rows shown in the bias-knob preview.
const settingsPreviewN = 5

// renderSettingsTopPreview returns a small block showing the top N pending
// tasks ranked by the supplied biases/heat (pure — no global mutation). On
// empty task sets it returns an empty string so the caller can skip it.
// maxW is the column width available (content, no outer borders).
func (m model) renderSettingsTopPreview(b biases, heat activityHeat, now time.Time, maxW int) string {
	all := m.allTodos()
	rows := rankTopBySequenceWith(all, b, heat, now)
	if len(rows) == 0 {
		return ""
	}
	if len(rows) > settingsPreviewN {
		rows = rows[:settingsPreviewN]
	}

	var sb strings.Builder

	hdr := tr("Top 5 with these weights:")
	sb.WriteString("\n  " + dimStyle.Render(hdr) + "\n")

	// Row format: "  NN  SS.S  <title>"
	// "  " (2) + rank (2) + "  " (2) + score (4, e.g. "12.3") + "  " (2) = 12 chars before title.
	const rowPrefixW = 12
	titleMax := maxW - rowPrefixW
	if titleMax < 8 {
		titleMax = 8
	}

	for i, t := range rows {
		score := sequenceComponentsAt(now, &t, b, heat).Total
		rank := fmt.Sprintf("%2d", i+1)
		scoreStr := fmt.Sprintf("%4.1f", score)
		title := truncate(t.Title, titleMax)
		line := fmt.Sprintf("  %s  %s  %s", rank, scoreStr, title)
		sb.WriteString(dimStyle.Render(line) + "\n")
	}
	return sb.String()
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
