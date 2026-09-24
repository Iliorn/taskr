package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Iliorn/taskr/todo"
	"github.com/charmbracelet/x/ansi"
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
	// Padded to the pane's inner width (avail), not past it: two cells over
	// meant that a header whose columns filled the pane exactly was clipped by
	// the pane, and the clip took the last letter of "Time" for its ellipsis.
	padW := avail - len([]rune(headerLeft))
	if padW < 0 {
		padW = 0
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
		cur := cursorGap
		if i == m.tagTabCursor {
			cur = cursorMark
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
			// The partial glyph's unfilled part is its background, so it takes
			// the track's tint and the fill runs straight into it.
			barStr.WriteString(tagProgressGradient[gradIdx].Inherit(barTrackStyle).Render(tagBarEighths[partialEighths]))
			// Remaining empty cells: one fewer because the partial cell occupies a slot.
			empty := barW - filled - 1
			if empty > 0 {
				barStr.WriteString(barTrackStyle.Render(strings.Repeat(barTrack, empty)))
			}
		} else if filled < barW {
			barStr.WriteString(barTrackStyle.Render(strings.Repeat(barTrack, barW-filled)))
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

// ── Stats ─────────────────────────────────────────────────────────────────────

// statsScopedTodos returns the task set the Stats tab aggregates: everything
// when no search is active, otherwise the top-level tasks matching the query
// (same compileSearch grammar as the Tasks list — #tag, @project, free text).
// Subtasks are dropped from the filtered form; every stats bucket reads
// top-level rows anyway, and the active `/query` chip already tells the user
// the page is scoped.
func (m model) statsScopedTodos() []*todo.Todo {
	all := m.allTodos()
	if m.searchQuery == "" {
		return all
	}
	match := compileSearch(m.searchQuery)
	scoped := make([]*todo.Todo, 0, len(all))
	for _, t := range all {
		if t.ParentID == "" && match(*t) {
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
	for _, t := range scope {
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
	// stays short enough to fit a not-very-tall screen. minColW is only where
	// the search starts: the sections are built as thunks over colW/valW, and
	// the layout below takes the most columns whose lines all fit.
	const gap = 4
	const minColW = 37
	maxCols := (availW + gap) / (minColW + gap)
	if maxCols < 1 {
		maxCols = 1
	}
	if maxCols > 3 {
		maxCols = 3
	}
	var cols, colW, valW int
	setCols := func(n int) {
		cols, colW, valW = n, availW, statsValueWidth
		if n > 1 {
			colW = (availW - (n-1)*gap) / n
			valW = 5
		}
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
		for j := 0; j < filled; j++ {
			pos := 0.0
			if filled > 1 {
				pos = float64(j) / float64(filled-1)
			}
			gradIdx := int(pos * float64(gradLen-1))
			if gradIdx >= gradLen {
				gradIdx = gradLen - 1
			}
			bar.WriteString(statsGradient[gradIdx].Render("█"))
		}
		// The track shares one style for its whole run, so it costs one Render
		// call rather than one per cell (ARCHITECTURE.md, "Group same-style
		// runs").
		if empty := barW - filled; empty > 0 {
			bar.WriteString(barTrackStyle.Render(strings.Repeat(barTrack, empty)))
		}
		sb.WriteString(detailLabelStyle.Render(labelStr) + normalStyle.Render(padRight(valStr, valW)) +
			bar.String() + dimStyle.Render(fmt.Sprintf(" %3d%%", int(pct*100))) + "\n")
	}

	section := func(build func(*strings.Builder)) func() string {
		return func() string {
			var sb strings.Builder
			build(&sb)
			return strings.TrimRight(sb.String(), "\n")
		}
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
	flowSection := func(title, vsLabel string, created, completed, prevCompleted int) func() string {
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
			// The title takes what the column has left after its age. In a
			// multi-column layout it asks for at least statsOldestMinW, which
			// overflows a column too narrow to say which task it is — and an
			// overflowing line is what sends the layout down a column.
			age := " (" + formatDaysCompact(oldestAge) + ")"
			oldestW := colW - statsLabelWidth - len([]rune(age))
			if want := min(len([]rune(oldestTitle)), statsOldestMinW); cols > 1 && oldestW < want {
				oldestW = want
			}
			sb.WriteString(detailLabelStyle.Render(padRight(tr("  Oldest active"), statsLabelWidth)) +
				normalStyle.Render(truncate(oldestTitle, oldestW)) +
				dimStyle.Render(age) + "\n")
		}
	})

	priority := func() string { return "" }
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

	for n := maxCols; n > 1; n-- {
		setCols(n)
		var columns [][]string
		if n == 3 {
			// Keep the two Flow windows together in the middle column.
			columns = [][]string{
				stackSections(workload(), throughput(), cycleTime()),
				stackSections(flow(), flow30(), projection()),
				stackSections(priority(), velocity()),
			}
		} else {
			columns = [][]string{
				stackSections(workload(), flow(), flow30(), cycleTime()),
				stackSections(throughput(), priority(), velocity(), projection()),
			}
		}
		if linesFit(colW, columns...) {
			b.WriteString(zipColumns(colW, gap, columns...))
			return b.String()
		}
	}
	setCols(1)
	first := true
	for _, sec := range []func() string{workload, flow, flow30, throughput, cycleTime, priority, velocity, projection} {
		s := sec()
		if strings.TrimSpace(s) == "" {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		b.WriteString(s + "\n")
		first = false
	}

	return b.String()
}

// linesFit reports whether every line of every column is at most w cells wide.
func linesFit(w int, columns ...[]string) bool {
	for _, col := range columns {
		for _, line := range col {
			if ansi.StringWidth(line) > w {
				return false
			}
		}
	}
	return true
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
	widths := make([]int, len(columns))
	for i := range widths {
		widths[i] = colW
	}
	return zipColumnsW(widths, gap, columns...)
}

// zipColumnsW is zipColumns with a width per column, for layouts whose columns
// are sized to what they hold rather than to an equal share.
func zipColumnsW(widths []int, gap int, columns ...[]string) string {
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
			w := 0
			if c < len(widths) {
				w = widths[c]
			}
			line = ansi.Truncate(line, w, "")
			if lw := ansi.StringWidth(line); lw < w {
				line += strings.Repeat(" ", w-lw)
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
	m.renderClosedTodayBlock(b, maxVisible-(endIdx-startIdx))
	return b.String()
}

// renderClosedTodayBlock fills unused rows at the bottom of the list pane with
// what the user closed today. The pane is a fixed-height box, so on any list
// shorter than the terminal those rows were drawn as blanks — a third of the
// screen on a normal day's list.
//
// It is a read-out, not a list: the rows carry no cursor and no keys, so
// nothing above them changes, and the block only ever occupies space the active
// list is not using. free is how many rows are going spare; the block declines
// to draw at all below the three it takes to be worth reading (a separating
// blank, its label, and one task). The set itself comes from the cache
// (refreshClosedToday) rather than from a scan of cache.done, which carries the
// user's history sort and is not ordered by completion time at all under
// historySortAlpha.
func (m model) renderClosedTodayBlock(b *strings.Builder, free int) {
	if free < 3 || m.showHistory {
		return
	}
	done := m.cache.closedToday
	n := len(done)
	if n == 0 {
		return
	}
	shown := n
	if max := free - 2; shown > max {
		shown = max
	}
	avail := m.termWidth - 12
	if avail < 8 {
		avail = 8
	}
	b.WriteString("\n")
	label := fmt.Sprintf(tr("  Closed today (%d)"), n)
	b.WriteString(dimStyle.Render(label) + "\n")
	for i := 0; i < shown; i++ {
		b.WriteString(dimStyle.Render("   ") +
			fastCheckDone.render("✓") +
			fastDim.render(" "+truncate(done[i].Title, avail)) + "\n")
	}
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
	cursorStr := cursorGap
	selected := index == cursor && active
	if selected {
		cursorStr = cursorMark
	}
	dueVal := ""
	if !t.DueDate.IsZero() {
		dueVal = t.DueDate.Format("02-01-06")
	}
	completedVal := ""
	if !t.CompletedAt.IsZero() {
		completedVal = t.CompletedAt.Format("02-01-06")
	}
	titleCol := padRight(truncate(t.Title, titleW-listColGap), titleW)
	// History rows get the same two-tone treatment as the active list: the
	// title at full strength, the dates dim. A completed task is a record, and
	// the record's subject is its title, not the day it closed.
	rowStyle, metaStyle := fastNormal, fastDim
	if selected {
		rowStyle, metaStyle = fastSelectedRow, fastSelectedDim
	}
	var r rowBuf
	r.add(rowStyle, cursorStr+"[")
	r.add(fastCheckDone, "✓")
	r.add(rowStyle, "] ")
	r.add(rowStyle, titleCol)
	if cols.showDue {
		r.add(metaStyle, padRight(dueVal, cols.dueW))
	}
	if cols.showLast {
		r.add(metaStyle, padRight(completedVal, 12))
	}

	contentW := m.termWidth - 8
	tagsStr, tagsDrawnW := m.renderRowTags(&t, contentW-r.w, selected)
	line := r.String()
	if selected {
		return line + tagsStr + selectedRowTail(fastSelectedRow, r.w+tagsDrawnW, contentW) + "\n"
	}
	return line + tagsStr + "\n"
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
	cursorStr := cursorGap
	selected := flatIndex == cursor && active
	if selected {
		cursorStr = cursorMark
	}
	check := "[ ]"
	if sub.Status == todo.Done {
		check = "[✓]"
	} else if len(sub.TimeEntries) > 0 {
		check = "[>]"
	}
	body := "   " + connector + " " + check + " " + title

	if selected {
		return fastSelectedRow.render(cursorStr+body) +
			selectedRowTail(fastSelectedRow, len([]rune(cursorStr+body)), m.termWidth-8) + "\n"
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

// taskRowLabel splits a row's title cell into the three pieces fitTaskRowLabel
// lays out: the prefix (answers "can I pick this up?" at the left edge), the
// title text, and the badges (blocker/blocked arrows, recurrence, subtask
// progress). They are kept apart so a narrow column clips the title, which has
// slack in it, and never the badges, which change the decision.
//
// refreshTaskColMetrics sizes the title column from this same function, so the
// width it reserves and the width the row draws cannot drift. Priority has no
// glyph: it is already the largest term in the Score column.
func (m *model) taskRowLabel(t *todo.Todo) (prefix, text, badges string) {
	var p strings.Builder
	if t.IsTimerRunning() {
		p.WriteString("⧗ ")
	}
	// One arrow, not two: a task in the middle of a chain is both, and the
	// half that decides whether you can start it is the one worth a cell.
	switch {
	case m.cache.blockedSet[t.ID]:
		p.WriteString("↧ ") // waiting on an unfinished dependency — sorts last
	case m.cache.blockerSet[t.ID]:
		p.WriteString("↥ ") // others depend on this — clearing it unblocks them
	}
	prefix = p.String()
	text = t.Title
	var b strings.Builder
	if t.IsRecurring() {
		b.WriteString(" ↻")
	}
	if subDone, subTotal := m.subtaskProgress(t.ID); subTotal > 0 {
		fmt.Fprintf(&b, " (%d/%d)", subDone, subTotal)
	}
	return prefix, text, b.String()
}

// taskRowLabelWidth is the display width taskRowLabel's three pieces draw
// together — what the title column has to hold for the row to render whole.
func taskRowLabelWidth(prefix, text, badges string) int {
	return len([]rune(prefix)) + len([]rune(text)) + len([]rune(badges))
}

// fitTaskRowLabel lays the three pieces into avail cells, clipping the title
// text and keeping the badges. When even prefix+badges overrun the column
// there is nothing left to protect, so the whole label is clipped as one
// string rather than drawn past the column's edge.
func fitTaskRowLabel(prefix, text, badges string, avail int) string {
	if avail <= 0 {
		return ""
	}
	fixed := len([]rune(prefix)) + len([]rune(badges))
	if fixed < avail {
		return prefix + truncate(text, avail-fixed) + badges
	}
	return truncate(prefix+text+badges, avail)
}

// rowBuf accumulates a task-list row as styled runs while tracking the plain
// display width it has emitted. Two things make it worth a type: the width has
// to be counted on the unstyled text (an SGR sequence is a dozen runes, and the
// tags cell and the selected-row tail are both positioned from this count), and
// consecutive runs that share a style are coalesced into one render call, so a
// row still costs a handful of escape sequences rather than one per column.
type rowBuf struct {
	out  strings.Builder
	run  strings.Builder
	cur  fastStyle
	open bool
	w    int
}

// add appends s in style st. Styles are compared by their cached SGR prefix and
// suffix rather than by the struct: fastStyle carries a lipgloss.Style, and it
// is the escape sequences, not the struct identity, that decide whether two
// runs can share one render call.
func (r *rowBuf) add(st fastStyle, s string) {
	if s == "" {
		return
	}
	if r.open && st.prefix == r.cur.prefix && st.suffix == r.cur.suffix {
		r.run.WriteString(s)
	} else {
		r.flush()
		r.cur, r.open = st, true
		r.run.WriteString(s)
	}
	r.w += len([]rune(s))
}

func (r *rowBuf) flush() {
	if !r.open || r.run.Len() == 0 {
		r.run.Reset()
		return
	}
	r.out.WriteString(r.cur.render(r.run.String()))
	r.run.Reset()
}

func (r *rowBuf) String() string {
	r.flush()
	r.open = false
	return r.out.String()
}

// rowPalette is the pair of styles one task row is painted with. status carries
// whatever the row's state is saying — normal, overdue, blocked by something
// overdue, timer running — and meta is the dim tone the secondary columns are
// drawn in. Both already carry the selection background when the row is the
// one under the cursor, so a selected overdue row shows the selection and the
// status at once instead of one masking the other.
//
// Splitting the row across two styles is the whole point: painting every cell
// in the status colour meant the score, the size and the project name shouted
// as loudly as the title, and an overdue row coloured its project name red —
// a cell that has nothing to do with being overdue.
type rowPalette struct {
	status fastStyle
	meta   fastStyle
}

func taskRowPalette(t *todo.Todo, hasOverdueDep, selected bool) rowPalette {
	switch {
	case t.IsTimerRunning() && selected:
		return rowPalette{fastSelectedTimer, fastSelectedDim}
	case t.IsTimerRunning():
		return rowPalette{fastTimer, fastDim}
	case t.IsOverdue() && selected:
		return rowPalette{fastSelectedOverdue, fastSelectedDim}
	case t.IsOverdue():
		return rowPalette{fastOverdue, fastDim}
	case hasOverdueDep && selected:
		return rowPalette{fastSelectedDepOverdue, fastSelectedDim}
	case hasOverdueDep:
		return rowPalette{fastDepOverdue, fastDim}
	case selected:
		return rowPalette{fastSelectedRow, fastSelectedDim}
	default:
		return rowPalette{fastNormal, fastDim}
	}
}

func (m *model) renderTaskLineWithSet(t *todo.Todo, index, cursor int, active bool, overdueSet map[string]bool, cols listCols) string {
	titleW := cols.titleW
	cursorStr := cursorGap
	selected := index == cursor && active
	if selected {
		cursorStr = cursorMark
	}
	// The status box holds one fact: where this task stands. Overdue outranks
	// started because it is the one that wants a decision — you already know
	// you started it. Blocked is not in here any more: the sort puts blocked
	// work at the bottom, and the ↧ before the title says why it is there.
	checkbox := "[ ]"
	switch {
	case t.Status == todo.Done:
		checkbox = "[✓]"
	case t.IsOverdue():
		checkbox = "[!]"
	case len(t.TimeEntries) > 0:
		checkbox = "[>]" // in progress: time has been logged against it
	}
	// +/- rather than a triangle: the row already opens with the ▶ cursor, and
	// a second triangle one cell later read as a second cursor — two arrows on
	// the same row, one of which does not move. The tree convention says the
	// same thing without borrowing the cursor's shape.
	foldIcon := " "
	if m.subtaskCount(t.ID) > 0 {
		if m.expandedTasks[t.ID] {
			foldIcon = "-"
		} else {
			foldIcon = "+"
		}
	}
	// hasOverdueDep drives the row colour (see taskRowPalette), not a glyph.
	hasOverdueDep := t.HasOverdueDependencyFast(overdueSet)
	pal := taskRowPalette(t, hasOverdueDep, selected)

	dueVal := ""
	if !t.DueDate.IsZero() {
		dueVal = formatDueShort(t.DueDate, m.frameTime)
	}
	// The due cell is the one piece of metadata the status colour is actually
	// about, so it keeps the status tone when the task is late and drops to the
	// dim tone otherwise. That way a red cell in the Due column means the date
	// is the problem, instead of being one more cell in a uniformly red row.
	dueStyle := pal.meta
	if t.IsOverdue() || hasOverdueDep {
		dueStyle = pal.status
	}

	prefix, text, badges := m.taskRowLabel(t)
	// Reserve one trailing space inside the column so a clipped title never
	// butts up against the Score column that follows.
	label := fitTaskRowLabel(prefix, text, badges, titleW-listColGap)

	var r rowBuf
	r.add(pal.status, cursorStr+checkbox+foldIcon)
	r.add(pal.status, padRight(label, titleW))
	if cols.showLast {
		// Score reads as a percent of the current field (sequence.go): "82%"
		// says how close to the top this is, where a bare "24.4" only said "a
		// number". Right-aligned in the field so every score ends in the same
		// column and the % signs line up; the field's trailing listColGap is
		// the gap to Due.
		r.add(pal.meta, padRight(padLeft(formatSequencePercent(m.rankedScore(t)), cols.lastW-listColGap), cols.lastW))
	}
	if cols.showDue {
		// Right-aligned for the same reason: "2d" and "20-09-27" share a right
		// edge, so the gap to Size is the same on every row.
		r.add(dueStyle, padRight(padLeft(dueVal, cols.dueW-listColGap), cols.dueW))
	}
	if cols.showSize {
		// One letter at the column's left edge, under its header; the column
		// carries its own trailing gap, the same way every other column does.
		r.add(pal.meta, padRight(strings.ToLower(t.Size.Letter()), cols.sizeW))
	}
	if cols.showProject {
		// Truncate at projectW-listColGap so the column always leaves its full
		// gap before the tags, clipped name or not.
		r.add(pal.meta, padRight(truncate(t.Project, cols.projectW-listColGap), cols.projectW))
	}

	contentW := m.termWidth - 8
	tagsStr, tagsDrawnW := m.renderRowTags(t, contentW-r.w, selected)
	line := r.String()
	if selected {
		return line + tagsStr + selectedRowTail(pal.status, r.w+tagsDrawnW, contentW) + "\n"
	}
	return line + tagsStr + "\n"
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

	// The counts are bare numbers right-aligned under their headings.
	activeHdr, doneHdr, overdueHdr := tr("Active"), tr("Done"), tr("Overdue")
	gap := strings.Repeat(" ", listColGap)
	countCol := func(v, hdr string) string { return padLeft(v, len([]rune(hdr))) }

	const prefix = "  "
	headerLeft := prefix + padRight(projHdr, projW) +
		activeHdr + gap + doneHdr + gap + overdueHdr
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
		cursorStr := cursorGap
		if i == m.projectCursor {
			cursorStr = cursorMark
		}
		if m.mode == modeEditProjectInline && i == m.projectCursor {
			b.WriteString(normalStyle.Render(cursorStr) + m.textInput.View() + "\n")
			continue
		}
		// truncate at projW-1 so a truncated name (ending in "(…)") still leaves
		// 1 trailing space before the Active column — same rule as the title col
		// on the Tasks tab.
		nameCol := padRight(truncate(p, projW-1), projW)
		activeStr := countCol(fmt.Sprint(activeCnt), activeHdr) + gap
		doneStr := countCol(fmt.Sprint(doneCnt), doneHdr) + gap
		overdueStr := countCol("─", overdueHdr)
		if overdueCnt > 0 {
			overdueStr = countCol(fmt.Sprint(overdueCnt), overdueHdr)
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

// Settings is one pane of grouped rows, laid out in two columns when the
// terminal is wide enough. The groups say which control belongs to what
// ("Listen" and "Server token" are the sync server's own). Titles are held in English and
// passed through tr() at render time — a package var would freeze them before
// applyLang runs.
type settingsGroup struct {
	title string
	rows  []int
	// preview draws the top-N ranking under this group's rows. It is the whole
	// account the bias knobs give of themselves, so it sits with them rather
	// than at the foot of the pane where a knob change would scroll it away.
	preview bool
}

var settingsGroups = []settingsGroup{
	{title: "Appearance", rows: []int{
		settingTheme,
		settingLanguage,
		settingDetailPos,
	}},
	{title: "General", rows: []int{
		settingAutoCloseParent,
		settingAutoCloseSubtasks,
		settingShowBoard,
		settingStages,
	}},
	{title: "Sequencer", preview: true, rows: []int{
		settingBiasDeadline,
		settingBiasPriority,
		settingBiasMomentum,
		settingAging,
	}},
	{title: "Sync", rows: []int{
		settingSyncAuto,
		settingSyncBoard,
		settingSyncServer,
		settingSyncToken,
		settingSyncNow,
	}},
	{title: "Server", rows: []int{
		settingServerOn,
		settingServerListen,
		settingServerToken,
	}},
	{title: "About", rows: []int{
		settingVersion,
		settingCheckUpdate,
	}},
}

// The pane is drawn as two columns of groups: the first settingsColumnSplit
// groups on the left, the rest on the right. The split is a group boundary, so
// the column-major reading order is exactly settingsNavOrder's order — up/down
// walks the left column, then continues at the top of the right one, and no
// navigation code has to know about columns at all.
//
// Below settingsTwoColMinWidth a column would be too narrow for a label and
// its value, so the pane falls back to the single column.
const (
	settingsColumnSplit    = 3
	settingsTwoColMinWidth = 96
	settingsColGap         = 3
)

// settingsSelectable reports whether the cursor may land on a row. Version is
// a fact, not a control: enter on it did nothing, so stopping there was a dead
// step in the middle of the list.
func settingsSelectable(id int) bool { return id != settingVersion }

// settingsRowVisible hides the rows that configure something this machine is
// not doing. The bind address and the server token describe an endpoint that
// only exists while the server runs, so on a client — which is most installs —
// they were two rows of setup for a thing the user had already said no to.
//
// Turning the server on is what brings them back, and that stays reachable
// without them: the Enabled row asks for the token itself (serverStartFlow)
// when there is none, rather than sending the user to a row it just hid.
func (m model) settingsRowVisible(id int) bool {
	switch id {
	case settingServerListen, settingServerToken:
		return m.inprocServer != nil || m.serverExternal
	}
	return true
}

// settingsEditsText marks the rows whose enter opens a text editor. They render
// with a trailing mark so an editable value can be told apart from a ‹ cycled ›
// one without pressing anything.
func settingsEditsText(id int) bool {
	switch id {
	case settingStages, settingSyncServer, settingSyncToken, settingServerListen, settingServerToken:
		return true
	}
	return false
}

// settingsEditMark is the affordance on a row that opens an editor.
const settingsEditMark = " ⏎"

// settingsNavOrder returns the linear up/down traversal order: the groups in
// the order they are drawn, each top→bottom. Rows the cursor cannot land on —
// unselectable or hidden by the current state — are left out here, so every
// caller inherits the skip.
func (m model) settingsNavOrder() []int {
	out := make([]int, 0, numSettingsRows)
	for _, g := range settingsGroups {
		for _, id := range g.rows {
			if settingsSelectable(id) && m.settingsRowVisible(id) {
				out = append(out, id)
			}
		}
	}
	return out
}

// visibleGroupRows is the group's rows minus the ones the current state hides.
// It returns the group's own slice when nothing is hidden, which is the common
// case and the one worth not allocating for.
func (m model) visibleGroupRows(g settingsGroup) []int {
	hidden := false
	for _, id := range g.rows {
		if !m.settingsRowVisible(id) {
			hidden = true
			break
		}
	}
	if !hidden {
		return g.rows
	}
	out := make([]int, 0, len(g.rows))
	for _, id := range g.rows {
		if m.settingsRowVisible(id) {
			out = append(out, id)
		}
	}
	return out
}

// settingsCursorStep advances the settings cursor by delta along the visual
// traversal order, clamping at the ends so up at the top / down at the bottom
// are no-ops.
func (m model) settingsCursorStep(cur, delta int) int {
	order := m.settingsNavOrder()
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

// renderSettingsSection builds the unboxed Settings content and, with it, the
// line the cursor row is drawn on (-1 when the cursor is on nothing visible).
// The pane scrolls by that number, and it comes back from the renderer rather
// than from a parallel line-counting function: headings, group separators and
// the bias preview all sit between the rows, and a second place counting them
// is a second place to get them wrong. w is the content width available (no
// outer borders); the pane builder applies the final per-line width contract.
func (m model) renderSettingsSection(w int) (string, int) {
	if w < 8 {
		w = 8
	}
	labels := map[int]string{
		settingBiasDeadline:      tr("Deadline pressure"),
		settingBiasPriority:      tr("Priority focus"),
		settingBiasMomentum:      tr("Momentum bias"),
		settingAging:             tr("Aging increases score"),
		settingAutoCloseParent:   tr("Auto-close parent"),
		settingAutoCloseSubtasks: tr("Auto-close subtasks"),
		settingShowBoard:         tr("Kanban board"),
		settingTheme:             tr("Theme"),
		settingLanguage:          tr("Language"),
		settingDetailPos:         tr("Detail pane"),
		settingStages:            tr("Board columns"),
		settingSyncAuto:          tr("Automatic"),
		settingSyncBoard:         tr("Share board columns"),
		settingSyncServer:        tr("Sync server"),
		settingSyncToken:         tr("Sync token"),
		settingSyncNow:           tr("Sync now"),
		settingServerOn:          tr("Enabled"),
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
	showBoardVal := tr("Off")
	if showBoard {
		showBoardVal = tr("On")
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
	syncBoardVal := tr("Off")
	if syncBoardColumns {
		syncBoardVal = tr("On")
	}
	syncServerVal := tr("not set")
	if m.syncCfg.URL != "" {
		syncServerVal = m.syncCfg.URL
	}
	syncTokenVal := tr("not set")
	if m.syncCfg.Token != "" {
		syncTokenVal = "•••• " + tr("set")
	}
	// Off is the answer for every machine that is not the hub. The token is
	// only needed to turn this on, and toggleServer says so when they try.
	serverState := tr("Off")
	switch {
	case m.inprocServer != nil:
		serverState = tr("On")
	case m.serverExternal:
		serverState = tr("external")
	}
	serverTokenVal := tr("not set")
	if m.syncCfg.ServerToken != "" {
		serverTokenVal = "•••• " + tr("set")
		// Flagged where it can be fixed: the row that edits it. The reason
		// itself lives in `taskr doctor`; here there is only room to say that
		// it is worth replacing and which key does that.
		if weakSyncToken(m.syncCfg.ServerToken) != "" {
			serverTokenVal = "•••• " + tr("weak token — ctrl+g on this row generates a strong one")
		}
	}
	values := map[int]string{
		settingBiasDeadline:      biasPickerValue(activeBiases.Deadline),
		settingBiasPriority:      biasPickerValue(activeBiases.Priority),
		settingBiasMomentum:      biasPickerValue(activeBiases.Momentum),
		settingAging:             "‹ " + agingVal + " ›",
		settingAutoCloseParent:   "‹ " + autoCloseVal + " ›",
		settingAutoCloseSubtasks: "‹ " + autoCloseSubsVal + " ›",
		settingShowBoard:         "‹ " + showBoardVal + " ›",
		settingTheme:             "‹ " + m.themeName + " ›",
		settingLanguage:          "‹ " + activeLang.displayName() + " ›",
		settingDetailPos:         "‹ " + trDetailPos(m.detailPos) + " ›",
		settingStages:            stagesDisplay(),
		settingSyncAuto:          syncAutoVal,
		settingSyncBoard:         "‹ " + syncBoardVal + " ›",
		settingSyncServer:        syncServerVal,
		settingSyncToken:         syncTokenVal,
		settingSyncNow:           tr("press enter to sync"),
		settingServerOn:          "‹ " + serverState + " ›",
		settingServerListen:      m.syncCfg.listenAddr(),
		settingServerToken:       serverTokenVal,
		settingVersion:           appVersion,
		settingCheckUpdate:       tr("press enter to check"),
	}

	// One label column across every group, so the values line up down the
	// whole pane rather than stepping in and out at each heading.
	labelW := 0
	for _, g := range settingsGroups {
		for _, id := range m.visibleGroupRows(g) {
			if n := len([]rune(labels[id])); n > labelW {
				labelW = n
			}
		}
	}
	labelW += 2

	renderRow := func(id int) string {
		cursor := cursorGap
		labelStyle := normalStyle
		if id == m.settingsCursor {
			cursor = selectedStyle.Render(cursorMark)
			labelStyle = selectedStyle
		}
		value := values[id]
		if settingsEditsText(id) {
			value += settingsEditMark
		}
		return cursor + labelStyle.Render(padRight(labels[id], labelW)) + helpStyle.Render(value)
	}

	// renderColumn draws one column's worth of groups and reports the line the
	// cursor row landed on (-1 when the cursor is not in this column). colW is
	// the column's own width, so the bias preview sizes its titles to the
	// column it sits in rather than to the whole pane.
	renderColumn := func(groups []settingsGroup, colW int) ([]string, int) {
		var lines []string
		selected := -1
		for _, g := range groups {
			rows := m.visibleGroupRows(g)
			if len(rows) == 0 {
				continue
			}
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, cursorGap+headerStyle.Render(tr(g.title)))
			for _, id := range rows {
				if id == m.settingsCursor {
					selected = len(lines)
				}
				lines = append(lines, renderRow(id))
			}
			// Live preview: the top-N tasks ranked with the current knob values is
			// the whole account the pane gives of a bias change — a prose tagline
			// for the mix said less than the five rows that actually move.
			if g.preview {
				if preview := m.renderSettingsTopPreview(activeBiases, activeHeat, m.frameTime, colW); preview != "" {
					lines = append(lines, strings.Split(strings.TrimRight(preview, "\n"), "\n")...)
				}
			}
		}
		return lines, selected
	}

	var lines []string
	var selected int
	if w >= settingsTwoColMinWidth {
		colW := (w - settingsColGap) / 2
		left, leftSel := renderColumn(settingsGroups[:settingsColumnSplit], colW)
		right, rightSel := renderColumn(settingsGroups[settingsColumnSplit:], colW)
		lines, selected = joinSettingsColumns(left, leftSel, right, rightSel, colW)
	} else {
		lines, selected = renderColumn(settingsGroups, w)
	}

	if m.updateStatus != "" {
		lines = append(lines, "", "  "+activeCountStyle.Render(m.updateStatus))
	}
	if m.syncStatus != "" {
		// Wrapped, not truncated. This line is where a sync failure explains
		// itself, and the explanation is the tail: cutting it at a fixed 60
		// columns once left a user reading "Last sync failed: server returned
		// 500 Internal Server Error" for a week while the sentence naming the
		// stale server sat just past the cut.
		lines = append(lines, "")
		for _, line := range clampLines(wrapText(m.syncStatus, w-4), syncStatusMaxLines) {
			lines = append(lines, "  "+helpStyle.Render(line))
		}
	}
	return strings.Join(lines, "\n") + "\n", selected
}

// joinSettingsColumns lays the two columns of groups side by side inside the
// one pane. The columns are top-aligned, so a row's line number in the joined
// block is its index in its own column — which is exactly what the pane's
// scroll (fitSettingsPane) is given, with no second coordinate system to keep
// in step. Lines are padded to the column width with ansi.StringWidth, not
// len(): every row carries styling, and byte length would indent the right
// column by the width of the escape sequences.
func joinSettingsColumns(left []string, leftSel int, right []string, rightSel, colW int) ([]string, int) {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if r == "" {
			// Nothing to the right of this line, so a value wider than its
			// column share may run on into the empty lane — which is how
			// "Board columns" keeps showing its whole list of columns.
			out = append(out, l)
			continue
		}
		// With a row to the right, the left line is clipped to its share
		// instead: the right column has to start at the same x on every line
		// or it stops reading as a column, and a long value shoving its
		// neighbour sideways is worse than an ellipsis.
		l = ansi.Truncate(l, colW, ellipsis)
		pad := colW + settingsColGap - ansi.StringWidth(l)
		if pad < settingsColGap {
			pad = settingsColGap
		}
		out = append(out, l+strings.Repeat(" ", pad)+r)
	}
	selected := leftSel
	if selected < 0 {
		selected = rightSel
	}
	return out, selected
}

// renderSettingsList preserves a plain, unboxed rendering for focused unit
// tests and other callers. View uses buildSettingsContent to put the same
// content in its pane.
func (m model) renderSettingsList() string {
	content, _ := m.renderSettingsSection(m.termWidth - 8)
	return content
}

// fitSettingsPane keeps the selected row visible when a narrow/short terminal
// cannot show the whole pane, then pads the pane to its assigned height.
func fitSettingsPane(content string, height, width, selectedLine int) []string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	start := 0
	if selectedLine >= height {
		start = selectedLine - height + 1
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	lines = append([]string(nil), lines[start:end]...)
	for len(lines) < height {
		lines = append(lines, "")
	}
	truncateLines(lines, width)
	return lines
}

// buildSettingsContent renders the settings rows into the tab's single pane.
func (m model) buildSettingsContent(w, outerH int) string {
	content, selected := m.renderSettingsSection(w - 2)
	// panelContentHeight, not outerH-2: the panel's chrome is two borders and
	// the padding row above the first line. Counting only the borders left the
	// pane one row taller than it drew, so the last row — and, when the cursor
	// was on it, the row the scroll had just been asked to reveal — fell off
	// the bottom.
	lines := fitSettingsPane(content, panelContentHeight(outerH), w-2, selected)
	panel := listPanelFocusedStyle.Width(w).Render(strings.Join(lines, "\n"))
	return withBorderTitle(panel, m.listPanelTitle(), w, true)
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

	// The preview ranks with knob values that are not live yet, so its
	// percentages are relative to its own field — the live 100% mark belongs
	// to a different set of weights.
	previewMax := 0.0
	for i := range rows {
		if s := sequenceComponentsAt(now, &rows[i], b, heat).Total; s > previewMax {
			previewMax = s
		}
	}
	for i, t := range rows {
		score := sequenceComponentsAt(now, &t, b, heat).Total
		rank := fmt.Sprintf("%2d", i+1)
		scoreStr := fmt.Sprintf("%4s", strconv.Itoa(percentOfField(score, previewMax))+"%")
		title := truncate(t.Title, titleMax)
		line := fmt.Sprintf("  %s  %s  %s", rank, scoreStr, title)
		sb.WriteString(dimStyle.Render(line) + "\n")
	}
	return sb.String()
}

// biasPickerValue formats a bias for the Settings picker the same way the
// theme/language pickers do: title-cased value between thin chevrons.
func biasPickerValue(b biasLevel) string {
	s := tr(b.String())
	if s == "" {
		return "‹ - ›"
	}
	// CapitalizeTitle rather than slicing the first byte: a translated word can
	// start with a multi-byte rune, and s[:1] would cut it in half.
	return "‹ " + todo.CapitalizeTitle(s) + " ›"
}
