package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"taskr/todo"
)

// ── Day activities ────────────────────────────────────────────────────────────

type dayActivity struct {
	taskID  string
	entryID string
	title   string
	project string
	tags    []string
	start   time.Time
	stop    time.Time // zero while the entry is still running
	// completed marks this as a completion event (task was marked done on
	// this day) rather than a time-tracking entry. start==stop==CompletedAt
	// and duration is 0; the timeline renders it as "✓ done at HH:MM".
	completed bool
}

func (a dayActivity) duration() time.Duration {
	if a.completed {
		return 0
	}
	if a.stop.IsZero() {
		return time.Since(a.start)
	}
	return a.stop.Sub(a.start)
}

func dayKey(t time.Time) string {
	// Times come back from storage in UTC; the calendar grid is laid out
	// in the user's local zone, so format the date in local time too —
	// otherwise an entry started at 01:00 local (= 23:00 UTC yesterday)
	// gets keyed to the wrong day.
	return t.Local().Format("2006-01-02")
}

// activitiesForDay returns every time entry started on the given day plus
// every completion event (a task marked done on that day) so the calendar
// surfaces work even when the user closed the task without tracking time.
// Result is ordered by timestamp.
func (m model) activitiesForDay(day time.Time) []dayActivity {
	key := dayKey(day)
	var acts []dayActivity
	for _, t := range m.tasks {
		for _, e := range t.TimeEntries {
			if dayKey(e.StartedAt) != key {
				continue
			}
			acts = append(acts, dayActivity{
				taskID:  t.ID,
				entryID: e.ID,
				title:   t.Title,
				project: t.Project,
				tags:    t.Tags,
				start:   e.StartedAt,
				stop:    e.StoppedAt,
			})
		}
		// Completion event: task marked done on this day. Only surfaces when
		// the day has no tracked time for that task — otherwise the time
		// entries already cover the work and a second "done at HH:MM" row
		// would just be noise.
		if t.Status == todo.Done && !t.CompletedAt.IsZero() && dayKey(t.CompletedAt) == key {
			hasTracked := false
			for _, e := range t.TimeEntries {
				if dayKey(e.StartedAt) == key {
					hasTracked = true
					break
				}
			}
			if !hasTracked {
				acts = append(acts, dayActivity{
					taskID:    t.ID,
					title:     t.Title,
					project:   t.Project,
					tags:      t.Tags,
					start:     t.CompletedAt,
					stop:      t.CompletedAt,
					completed: true,
				})
			}
		}
	}
	// Total order with deterministic tiebreakers — m.tasks is a map
	// (randomized iteration) and sort.Slice isn't stable, so without
	// these, entries with equal start times would reshuffle on every
	// render and appear to "jump" as the cursor moves.
	sort.Slice(acts, func(i, j int) bool {
		if !acts[i].start.Equal(acts[j].start) {
			return acts[i].start.Before(acts[j].start)
		}
		if acts[i].taskID != acts[j].taskID {
			return acts[i].taskID < acts[j].taskID
		}
		return acts[i].entryID < acts[j].entryID
	})
	return acts
}

// trackedPerDay sums tracked time per day for entries started in [from, to].
func (m model) trackedPerDay(from, to time.Time) map[string]time.Duration {
	end := to.AddDate(0, 0, 1)
	totals := make(map[string]time.Duration)
	for _, t := range m.tasks {
		for _, e := range t.TimeEntries {
			if e.StartedAt.Before(from) || !e.StartedAt.Before(end) {
				continue
			}
			totals[dayKey(e.StartedAt)] += e.Duration()
		}
	}
	return totals
}

// ── Calendar tab layout ───────────────────────────────────────────────────────

func (m model) buildCalendarContent(w, outerH int) string {
	innerH := outerH - 2
	if innerH < 1 {
		innerH = 1
	}
	tlW := w - calPanelWidth - 4
	if tlW < minInnerWidth {
		tlW = minInnerWidth
	}

	calLines := m.renderMonthCalendarLines()
	tlLines := m.renderTimelineLines(tlW-2, innerH)

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
	calLines = fitLines(calLines, innerH, calPanelWidth-2)
	tlLines = fitLines(tlLines, innerH, tlW-2)

	calPanel := listPanelStyle.Width(calPanelWidth).Render(strings.Join(calLines, "\n"))
	tlPanel := listPanelStyle.Width(tlW).Render(strings.Join(tlLines, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, tlPanel, calPanel)
}

// ── Month calendar (right panel) ──────────────────────────────────────────────

func (m model) renderMonthCalendarLines() []string {
	sel := m.calendar.selected
	monthStart := time.Date(sel.Year(), sel.Month(), 1, 0, 0, 0, 0, sel.Location())
	monthEnd := monthStart.AddDate(0, 1, -1)
	totals := m.trackedPerDay(monthStart, monthEnd)

	var maxDay, monthTotal time.Duration
	for _, d := range totals {
		monthTotal += d
		if d > maxDay {
			maxDay = d
		}
	}

	today := startOfDay(m.frameTime)
	innerW := calPanelWidth - 2

	var lines []string
	title := localizedMonthYear(sel)
	pad := (innerW - len([]rune(title))) / 2
	if pad < 0 {
		pad = 0
	}
	lines = append(lines, strings.Repeat(" ", pad)+calHeaderStyle.Render(title))
	lines = append(lines, dimStyle.Render(localizedWeekdayHeader()))

	// Monday-first offset of the 1st, matching the stats heatmap convention.
	day := monthStart.AddDate(0, 0, -((int(monthStart.Weekday()) + 6) % 7))
	for day.Before(monthEnd) || day.Equal(monthEnd) {
		cells := make([]string, 0, 7)
		for dow := 0; dow < 7; dow++ {
			if day.Month() != sel.Month() {
				cells = append(cells, "  ")
			} else {
				cell := fmt.Sprintf("%2d", day.Day())
				tracked := totals[dayKey(day)]
				switch {
				case day.Equal(sel):
					cell = calSelectedDayStyle.Render(cell)
				case tracked > 0:
					idx := len(calGradient) - 1
					if maxDay > 0 {
						idx = int(float64(tracked) / float64(maxDay) * float64(len(calGradient)-1))
						if idx >= len(calGradient) {
							idx = len(calGradient) - 1
						}
					}
					cell = calGradient[idx].Bold(true).Render(cell)
				case day.Equal(today):
					cell = calTodayStyle.Render(cell)
				default:
					cell = normalStyle.Render(cell)
				}
				cells = append(cells, cell)
			}
			day = day.AddDate(0, 0, 1)
		}
		lines = append(lines, strings.Join(cells, " "))
	}

	lines = append(lines, m.renderDayRollupLines(innerW)...)

	lines = append(lines, "")
	lines = append(lines, dimStyle.Render(tr("month "))+timerStyle.Render(formatDuration(monthTotal)))
	return lines
}

// renderDayRollupLines builds the per-project / per-tag breakdown for the
// selected day, shown under the month grid.
func (m model) renderDayRollupLines(innerW int) []string {
	acts := m.activitiesForDay(m.calendar.selected)
	if len(acts) == 0 {
		return nil
	}

	projTotals := make(map[string]time.Duration)
	tagTotals := make(map[string]time.Duration)
	var dayTotal time.Duration
	for _, a := range acts {
		d := a.duration()
		dayTotal += d
		if a.project != "" {
			projTotals[a.project] += d
		}
		for _, tag := range a.tags {
			tagTotals["#"+tag] += d
		}
	}

	type rollup struct {
		name string
		d    time.Duration
	}
	top := func(totals map[string]time.Duration, limit int) []rollup {
		rs := make([]rollup, 0, len(totals))
		for name, d := range totals {
			rs = append(rs, rollup{name, d})
		}
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].d != rs[j].d {
				return rs[i].d > rs[j].d
			}
			return rs[i].name < rs[j].name
		})
		if len(rs) > limit {
			rs = rs[:limit]
		}
		return rs
	}

	nameW := innerW - 8 // 1 indent + 7 for the right-aligned duration
	lines := []string{"", dimStyle.Render(tr("day ")) + timerStyle.Render(formatDuration(dayTotal))}
	for _, r := range top(projTotals, 3) {
		lines = append(lines, " "+projLabelStyle.Render(padRight(truncate(r.name, nameW), nameW))+
			timerStyle.Render(fmt.Sprintf("%7s", formatDurationCompact(r.d))))
	}
	for _, r := range top(tagTotals, 3) {
		lines = append(lines, " "+tagStyle.Render(padRight(truncate(r.name, nameW), nameW))+
			timerStyle.Render(fmt.Sprintf("%7s", formatDurationCompact(r.d))))
	}
	return lines
}

// ── Activity timeline (right panel) ───────────────────────────────────────────

func (m model) renderTimelineLines(innerW, innerH int) []string {
	acts := m.activitiesForDay(m.calendar.selected)

	var total time.Duration
	for _, a := range acts {
		total += a.duration()
	}

	headerText := localizedDayDateAbbrev(m.calendar.selected)
	header := calHeaderStyle.Render(headerText)
	suffix := fmt.Sprintf(tr("%d entries · %s"), len(acts), formatDuration(total))
	if len(acts) == 1 {
		suffix = tr("1 entry · ") + formatDuration(total)
	}
	pad := innerW - len([]rune(headerText)) - len([]rune(suffix))
	if pad < 1 {
		pad = 1
	}
	lines := []string{header + strings.Repeat(" ", pad) + dimStyle.Render(suffix), ""}

	if len(acts) == 0 {
		lines = append(lines, dimStyle.Render(tr("  No activity on this day.")))
		lines = append(lines, dimStyle.Render(tr("  Press t on a task (tab 1) to start tracking.")))
		return lines
	}

	// Per-entry height is 1 line for bare entries and 2 lines for entries
	// with project/tags. Precompute heights so pagination is honest.
	heights := make([]int, len(acts))
	for i := range acts {
		heights[i] = 1
		if acts[i].project != "" || len(acts[i].tags) > 0 {
			heights[i] = 2
		}
	}

	bodyH := innerH - 2
	if bodyH < 1 {
		bodyH = 1
	}

	// Pick a [start, end) window centered on the cursor that fits in bodyH,
	// counting each entry's height + 1 connector between entries. Reserves
	// one line for a leading/trailing "⋮" marker when entries are clipped.
	fit := func(reserveTop, reserveBot int) (int, int) {
		budget := bodyH - reserveTop - reserveBot
		if budget < 1 {
			budget = 1
		}
		cur := m.calendar.entryCursor
		if cur < 0 {
			cur = 0
		} else if cur >= len(acts) {
			cur = len(acts) - 1
		}
		used := heights[cur]
		start, end := cur, cur+1
		for {
			grew := false
			if end < len(acts) {
				extra := heights[end] + 1 // entry + connector above it
				if used+extra <= budget {
					used += extra
					end++
					grew = true
				}
			}
			if start > 0 {
				extra := heights[start-1] + 1
				if used+extra <= budget {
					used += extra
					start--
					grew = true
				}
			}
			if !grew {
				break
			}
		}
		return start, end
	}

	start, end := fit(0, 0)
	clippedTop := start > 0
	clippedBot := end < len(acts)
	if clippedTop || clippedBot {
		// Re-fit reserving room for the ⋮ markers we're about to emit.
		top, bot := 0, 0
		if clippedTop {
			top = 1
		}
		if clippedBot {
			bot = 1
		}
		start, end = fit(top, bot)
		clippedTop = start > 0
		clippedBot = end < len(acts)
	}

	if clippedTop {
		lines = append(lines, dimStyle.Render("  ⋮"))
	}
	for i := start; i < end; i++ {
		lines = append(lines, m.renderTimelineEntry(acts[i], i, innerW))
		hasNext := i < end-1 || clippedBot
		if sub := m.renderTimelineSub(acts[i], innerW, hasNext); sub != "" {
			lines = append(lines, sub)
		}
		if i < end-1 {
			lines = append(lines, dimStyle.Render("  │"))
		}
	}
	if clippedBot {
		lines = append(lines, dimStyle.Render("  ⋮"))
	}
	return lines
}

func (m model) renderTimelineEntry(a dayActivity, index, innerW int) string {
	focused := m.calendar.focusTimeline && index == m.calendar.entryCursor
	cur := "  "
	if focused {
		cur = "▶ "
	}

	running := a.stop.IsZero() && !a.completed
	endStr := tr(" now ")
	switch {
	case a.completed:
		// Completion event collapses the range to "done at HH:MM"; duration is 0.
		endStr = a.start.Format("15:04")
	case !running:
		endStr = a.stop.Format("15:04")
	}
	rangeStr := a.start.Format("15:04") + "–" + endStr
	if a.completed {
		rangeStr = tr("✓ done at ") + a.start.Format("15:04")
	}
	durStr := formatDuration(a.duration())

	// Fixed right-hand block (range + duration); title/project/tags share the rest.
	// Rune count, not len(): the range separator is a multi-byte en dash.
	leftW := innerW - len([]rune(rangeStr)) - 16
	if leftW < 8 {
		leftW = 8
	}

	title := truncate(a.title, leftW)
	used := len([]rune(title))
	left := normalStyle.Render(title)
	if focused {
		left = selectedStyle.Render(title)
	}
	if pad := leftW - used; pad > 0 {
		left += strings.Repeat(" ", pad)
	}

	durStyled := timerStyle.Render(padRight(durStr, 8))
	switch {
	case running:
		durStyled = calTodayStyle.Render(padRight(durStr+" ◉", 8))
	case a.completed:
		// No duration for a completion event; pad to keep column alignment
		// honest across mixed rows.
		durStyled = dimStyle.Render(strings.Repeat(" ", 8))
	}

	dot := timerStyle.Render("●")
	if a.completed {
		dot = checkDoneStyle.Render("✓")
	}
	return cur + dot + " " + left + " " +
		dimStyle.Render(rangeStr) + "  " + durStyled
}

// renderTimelineSub renders the second line of a calendar entry: project (if
// set) followed by the entry's tags. Returns "" when the entry has neither —
// in which case the caller skips the line so bare entries stay 1-line.
//
// Drops tags first, then project, if the combined plain width would overflow
// innerW. Width is computed on the plain text and the result is styled at the
// end, since truncating a styled string would slice into ANSI escapes.
//
// hasNext extends the vertical "│" connector into this sub-line's gutter so
// the column doesn't visually break between an entry's body and the next
// entry. When false (last visible entry, no clipped bot) the gutter is blank.
func (m model) renderTimelineSub(a dayActivity, innerW int, hasNext bool) string {
	if a.project == "" && len(a.tags) == 0 {
		return ""
	}
	const indentW = 4 // visual cells: "  │ " or "    "
	indent := "    "
	if hasNext {
		indent = "  " + dimStyle.Render("│") + " "
	}
	avail := innerW - indentW
	if avail < 4 {
		return ""
	}

	projPlain := ""
	if a.project != "" {
		projPlain = "[" + a.project + "]"
	}
	tagsPlainW := 0
	for _, tag := range a.tags {
		// "⟨#tag⟩ " = 4 cells of decoration + tag width.
		tagsPlainW += len([]rune(tag)) + 4
	}
	projW := len([]rune(projPlain))

	showProj := projW > 0
	showTags := len(a.tags) > 0
	sep := 0
	if showProj && showTags {
		sep = 1
	}
	if showProj && showTags && projW+sep+tagsPlainW > avail {
		showTags = false
		sep = 0
	}
	if showProj && projW > avail {
		showProj = false
	}
	if !showProj && !showTags {
		return ""
	}

	var b strings.Builder
	b.WriteString(indent)
	if showProj {
		b.WriteString(projLabelStyle.Render(projPlain))
	}
	if showProj && showTags {
		b.WriteString(" ")
	}
	if showTags {
		b.WriteString(m.getRenderedTags(a.tags))
	}
	return b.String()
}
