package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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
}

func (a dayActivity) duration() time.Duration {
	if a.stop.IsZero() {
		return time.Since(a.start)
	}
	return a.stop.Sub(a.start)
}

func dayKey(t time.Time) string {
	return t.Format("2006-01-02")
}

// activitiesForDay returns every time entry started on the given day,
// ordered by start time.
func (m model) activitiesForDay(day time.Time) []dayActivity {
	key := dayKey(day)
	var acts []dayActivity
	for i := range m.todos {
		t := &m.todos[i]
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
	}
	sort.Slice(acts, func(i, j int) bool { return acts[i].start.Before(acts[j].start) })
	return acts
}

// trackedPerDay sums tracked time per day for entries started in [from, to].
func (m model) trackedPerDay(from, to time.Time) map[string]time.Duration {
	end := to.AddDate(0, 0, 1)
	totals := make(map[string]time.Duration)
	for i := range m.todos {
		for _, e := range m.todos[i].TimeEntries {
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
	title := sel.Format("January 2006")
	pad := (innerW - len(title)) / 2
	if pad < 0 {
		pad = 0
	}
	lines = append(lines, strings.Repeat(" ", pad)+calHeaderStyle.Render(title))
	lines = append(lines, dimStyle.Render("Mo Tu We Th Fr Sa Su"))

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
	lines = append(lines, dimStyle.Render("month ")+timerStyle.Render(formatDuration(monthTotal)))
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
	lines := []string{"", dimStyle.Render("day ") + timerStyle.Render(formatDuration(dayTotal))}
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

	header := calHeaderStyle.Render(m.calendar.selected.Format("Mon 02 Jan 2006"))
	headerText := m.calendar.selected.Format("Mon 02 Jan 2006")
	suffix := fmt.Sprintf("%d entries · %s", len(acts), formatDuration(total))
	if len(acts) == 1 {
		suffix = "1 entry · " + formatDuration(total)
	}
	pad := innerW - len([]rune(headerText)) - len([]rune(suffix))
	if pad < 1 {
		pad = 1
	}
	lines := []string{header + strings.Repeat(" ", pad) + dimStyle.Render(suffix), ""}

	if len(acts) == 0 {
		lines = append(lines, dimStyle.Render("  No activity on this day."))
		lines = append(lines, dimStyle.Render("  Press t on a task (tab 1) to start tracking."))
		return lines
	}

	// Each entry takes 2 lines (dot + connector), the last one takes 1.
	bodyH := innerH - 2
	if bodyH < 1 {
		bodyH = 1
	}
	maxEntries := (bodyH + 1) / 2
	start, end := 0, len(acts)
	clipped := len(acts) > maxEntries
	if clipped {
		maxEntries = (bodyH - 1) / 2 // reserve lines for the ⋮ markers
		if maxEntries < 1 {
			maxEntries = 1
		}
		start = m.calendar.entryCursor - maxEntries/2
		start = clamp(start, 0, len(acts)-maxEntries)
		end = start + maxEntries
	}

	if start > 0 {
		lines = append(lines, dimStyle.Render("  ⋮"))
	}
	for i := start; i < end; i++ {
		lines = append(lines, m.renderTimelineEntry(acts[i], i, innerW))
		if i < end-1 {
			lines = append(lines, dimStyle.Render("  │"))
		}
	}
	if end < len(acts) {
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

	running := a.stop.IsZero()
	endStr := " now "
	if !running {
		endStr = a.stop.Format("15:04")
	}
	rangeStr := a.start.Format("15:04") + "–" + endStr
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

	// Project and tags are appended only when the whole block fits,
	// mirroring the task list behavior.
	if a.project != "" {
		projPart := " [" + a.project + "]"
		if used+len([]rune(projPart)) <= leftW {
			left += projLabelStyle.Render(projPart)
			used += len([]rune(projPart))
		}
	}
	if len(a.tags) > 0 {
		tagsW := 1
		for _, tag := range a.tags {
			tagsW += len([]rune(tag)) + 4
		}
		if used+tagsW <= leftW {
			left += " " + m.getRenderedTags(a.tags)
			used += tagsW
		}
	}
	if pad := leftW - used; pad > 0 {
		left += strings.Repeat(" ", pad)
	}

	durStyled := timerStyle.Render(padRight(durStr, 8))
	if running {
		durStyled = calTodayStyle.Render(padRight(durStr+" ◉", 8))
	}

	return cur + timerStyle.Render("●") + " " + left + " " +
		dimStyle.Render(rangeStr) + "  " + durStyled
}
