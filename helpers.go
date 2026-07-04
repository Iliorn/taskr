package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"taskr/todo"
)

// ── Pure utilities ────────────────────────────────────────────────────────────

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "(…)"
}

// shortID returns the first 8 chars of a task ID — the same prefix the CLI
// shows and accepts in commands like `taskr show <prefix>`.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

func padCenter(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	pad := width - len(r)
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	runes := []rune(s)
	lines := make([]string, 0, (len(runes)/width)+1)
	for len(runes) > 0 {
		if len(runes) <= width {
			lines = append(lines, string(runes))
			break
		}
		cutAt := width
		minCut := width / 2
		if minCut < 1 {
			minCut = 1
		}
		for i := width; i > minCut; i-- {
			if runes[i] == ' ' {
				cutAt = i
				break
			}
		}
		lines = append(lines, string(runes[:cutAt]))
		runes = runes[cutAt:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	return lines
}

func commentLineCount(text string, available int) int {
	n := len([]rune(text))
	if n == 0 {
		return 1
	}
	if lines := (n + available - 1) / available; lines > 1 {
		return lines
	}
	return 1
}

func renderTagsPart(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(len(tags) * 12)
	for _, tag := range tags {
		sb.WriteString(tagStyle.Render("⟨#"+tag+"⟩") + " ")
	}
	return sb.String()
}

// listCols decides which columns of the task/history list fit at the current
// terminal width. Columns are dropped least-important-first as the window
// narrows so list lines never wrap inside the panel.
//
// showSize is active-only (history doesn't expose size); showLast carries the
// Score for active rows and the Completed date for history rows.
type listCols struct {
	titleW      int
	showSize    bool
	showDue     bool
	showLast    bool // Score (active) or Completed (history)
	showProject bool
}

// tagsRenderWidth is the on-screen width of a task's trailing tag list as the
// list rows render it (each tag as " #tag" plus styling padding). Used both to
// size rows and to reserve tag room when growing the title column.
func tagsRenderWidth(tags []string) int {
	w := 0
	for _, tag := range tags {
		w += 4 + len([]rune(tag))
	}
	return w
}

func taskListCols(termWidth int, isHistory bool, contentMax, tagsMax int) listCols {
	inner := termWidth - 8 // panel content width (margin + border + padding)
	const fixed = 6        // cursor + checkbox + fold icon
	c := listCols{showDue: true, showLast: true}
	if !isHistory {
		c.showSize = true
		c.showProject = true
	}

	// Title column fits its longest entry (+gap), floored to the header label so
	// it never truncates, capped by the shared responsive width.
	floor := len([]rune(tr("Task")))
	if isHistory {
		floor = len([]rune(tr("Completed tasks")))
	}
	c.titleW = contentFitWidth(termWidth, contentMax, 4, floor)

	lastW := scoreColW
	dueW := dueColW
	if isHistory {
		lastW = 12
		dueW = 12
	}
	colsW := func() int {
		w := 0
		if c.showSize {
			w += sizeColW
		}
		if c.showDue {
			w += dueW
		}
		if c.showLast {
			w += lastW
		}
		if c.showProject {
			w += projectColW
		}
		return w
	}

	// Drop order on narrow terminals:
	//   active:  Project → Size → Score → Due  (Project drops first since it
	//            shows on most rows as a single short word; keep Due longest
	//            — it's the hard fact)
	//   history: Due  → Completed     (Size and Project never shown)
	drop := []*bool{&c.showProject, &c.showSize, &c.showLast, &c.showDue}
	if isHistory {
		drop = []*bool{&c.showDue, &c.showLast}
	}
	for _, d := range drop {
		if inner-fixed-c.titleW-colsW() >= 0 {
			break
		}
		*d = false
	}

	// The flat name-column cap (nameColWidth) keeps the title sane on the other
	// list tabs, but on a wide terminal it can clip a long title while empty
	// space sits to the right of the fixed columns. Grow the title to absorb
	// that slack — but never past what the longest entry actually needs, and
	// leave room for the trailing tags column (a leading space + the widest
	// row's tags) so growing the title can't push tags off the right edge.
	tagsReserve := 0
	if tagsMax > 0 {
		tagsReserve = 1 + tagsMax
	}
	if want := contentMax + 4; c.titleW < want {
		if spare := inner - fixed - c.titleW - colsW() - tagsReserve; spare > 0 {
			grow := want - c.titleW
			if grow > spare {
				grow = spare
			}
			c.titleW += grow
		}
	}

	return c
}

func renderListHeader(b *strings.Builder, termWidth int, isHistory bool, c listCols) {
	dueW := dueColW
	if isHistory {
		dueW = 12
	}
	sizeLabel := padCenter(tr("Size"), sizeColW)
	dueLabel := padRight(tr("Due"), dueW)
	lastLabel := padRight(tr("Score"), scoreColW)
	// The active-sort cue lives in the fixed status line (renderStatusLine),
	// so column headers stay plain — no >..< decoration to reflow.
	title := tr("Task")
	if isHistory {
		title = tr("Completed tasks")
		lastLabel = padRight(tr("Completed"), 12)
	}

	const prefix = "      "
	headerLeft := prefix + padRight(title, c.titleW)
	// Active view: Score sits right after the title. History keeps the
	// historical Due → Completed order so the completion date stays next
	// to the due date.
	if c.showLast && !isHistory {
		headerLeft += lastLabel
	}
	if c.showDue {
		headerLeft += dueLabel
	}
	if c.showSize {
		headerLeft += sizeLabel
	}
	if c.showLast && isHistory {
		headerLeft += lastLabel
	}
	if c.showProject {
		headerLeft += padRight(tr("Project"), projectColW)
	}
	// Row tags are rendered with a leading space (see renderTaskLineWithSet), so
	// the header label needs the same lead-in to line up with the tag content.
	tagsLabel := " " + tr("Tags")
	padW := termWidth - 8 - len([]rune(headerLeft))
	if padW >= len([]rune(tagsLabel)) {
		headerLeft += tagsLabel
		padW -= len([]rune(tagsLabel))
	}
	if padW < 0 {
		padW = 0
	}
	b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")
}

// ── Editor support ────────────────────────────────────────────────────────────

func resolveEditorCmd() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		if path, err := exec.LookPath(editor); err == nil {
			return path
		}
	}
	candidates := []string{"hx", "helix", "nvim", "vim", "nano"}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, "notepad")
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}

func notesFilePath(taskID string) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".taskr", "notes")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, taskID+".md")
}

func writeNotesFile(taskID, content string) error {
	return os.WriteFile(notesFilePath(taskID), []byte(content), 0644)
}

func readNotesFile(taskID string) (string, error) {
	data, err := os.ReadFile(notesFilePath(taskID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func cleanupNotesFile(taskID string) {
	_ = os.Remove(notesFilePath(taskID))
}

// ── tagStats ──────────────────────────────────────────────────────────────────

type tagStats struct {
	total     int
	done      int
	openCount int           // open (non-done) tasks, denominator for avg age
	ageSum    time.Duration // Σ(now - CreatedAt) over open tasks
	tracked   time.Duration // Σ time-entry durations across all tasks
}

func computeTagStats(todos []todo.Todo) map[string]tagStats {
	now := time.Now()
	stats := make(map[string]tagStats, 16)
	for i := range todos {
		t := &todos[i]
		// The Tasks tab list is top-level only, so counting subtasks
		// here would inflate a tag row past what pressing Enter shows.
		if t.ParentID != "" {
			continue
		}
		var tracked time.Duration
		for _, te := range t.TimeEntries {
			tracked += te.Duration()
		}
		open := t.Status != todo.Done
		age := now.Sub(t.CreatedAt)
		for _, tag := range t.Tags {
			s := stats[tag]
			s.total++
			if t.Status == todo.Done {
				s.done++
			}
			if open {
				s.openCount++
				s.ageSum += age
			}
			s.tracked += tracked
			stats[tag] = s
		}
	}
	return stats
}

// ── Quick-add parsing ─────────────────────────────────────────────────────────

type parsedTask struct {
	title      string
	tags       []string
	project    string
	dueDate    time.Time
	priority   todo.Priority
	size       todo.Size
	recurrence string
	// deps holds raw, unresolved refs from dep: tokens (id-prefixes or the
	// `^` last-added shorthand). Resolution needs the live task set, so it
	// happens at the call site, not here.
	deps []string
}

func parseQuickAdd(input string) parsedTask {
	result := parsedTask{priority: todo.PriorityMedium, size: todo.SizeMedium}
	words := strings.Fields(input)
	var titleWords []string

	for _, word := range words {
		lower := strings.ToLower(word)
		switch {
		case strings.HasPrefix(word, "#"):
			if tag := strings.TrimPrefix(word, "#"); tag != "" {
				result.tags = append(result.tags, tag)
			}
		case strings.HasPrefix(lower, "due:"):
			if d, err := parseDueDate(strings.TrimPrefix(lower, "due:")); err == nil {
				result.dueDate = d
			} else {
				titleWords = append(titleWords, word)
			}
		case strings.HasPrefix(word, "@"):
			if proj := strings.TrimPrefix(word, "@"); proj != "" {
				result.project = proj
			}
		case strings.HasPrefix(lower, "p:"):
			switch strings.TrimPrefix(lower, "p:") {
			case "high", "h":
				result.priority = todo.PriorityHigh
			case "medium", "med", "m":
				result.priority = todo.PriorityMedium
			case "low", "l":
				result.priority = todo.PriorityLow
			default:
				titleWords = append(titleWords, word)
			}
		case strings.HasPrefix(lower, "size:") || strings.HasPrefix(lower, "s:"):
			spec := strings.TrimPrefix(strings.TrimPrefix(lower, "size:"), "s:")
			switch spec {
			case "s", "small":
				result.size = todo.SizeSmall
			case "m", "med", "medium":
				result.size = todo.SizeMedium
			case "l", "large":
				result.size = todo.SizeLarge
			default:
				titleWords = append(titleWords, word)
			}
		case strings.HasPrefix(lower, "r:") || strings.HasPrefix(lower, "recur:"):
			spec := strings.TrimPrefix(strings.TrimPrefix(lower, "recur:"), "r:")
			if canonical, ok := todo.ParseRecurrence(spec); ok && canonical != "" {
				result.recurrence = canonical
			} else {
				titleWords = append(titleWords, word)
			}
		case strings.HasPrefix(lower, "dep:"):
			// Whitespace-delimited, so only id-prefix refs (or ^) fit here —
			// title-substring refs have spaces and stay a CLI/edit affair.
			if ref := word[len("dep:"):]; ref != "" {
				result.deps = append(result.deps, ref)
			} else {
				titleWords = append(titleWords, word)
			}
		default:
			titleWords = append(titleWords, word)
		}
	}

	result.title = strings.Join(titleWords, " ")
	return result
}

// ── Time formatting ───────────────────────────────────────────────────────────

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// formatDueShort renders a due date for the tasks-list column as its distance
// in calendar days — "today", "3d", "-2d" (overdue) — because "how soon" is
// the question the list answers. Beyond four weeks a day count reads worse
// than a date, so it falls back to the absolute dd-mm-yy form. Detail views
// keep the absolute form everywhere: it round-trips through the date editor.
func formatDueShort(due, now time.Time) string {
	// Round rather than truncate: local midnights straddling a DST switch are
	// 23 or 25 hours apart and would otherwise land on the wrong day.
	days := int(math.Round(startOfDay(due).Sub(startOfDay(now)).Hours() / 24))
	switch {
	case days == 0:
		return tr("today")
	case days < -28 || days > 28:
		return due.Format("02-01-06")
	default:
		return fmt.Sprintf("%dd", days)
	}
}

// formatDurationLive renders a running duration with seconds, for the
// live timer indicator in the footer.
func formatDurationLive(d time.Duration) string {
	s := int(d.Seconds())
	if s < 0 {
		s = 0
	}
	if s < 3600 {
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh %02dm %02ds", s/3600, (s%3600)/60, s%60)
}

// formatDurationCompact renders without spaces for narrow columns: 48m, 1h39m, 12h.
func formatDurationCompact(d time.Duration) string {
	mins := int(d.Minutes())
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	if mins%60 == 0 {
		return fmt.Sprintf("%dh", mins/60)
	}
	return fmt.Sprintf("%dh%02dm", mins/60, mins%60)
}

// parseEntryEdit parses a time-entry edit: "HH:MM-HH:MM", "HH:MM-now"
// (keeps the entry running), or a bare duration ("45m", "1h30m", "2h").
// Clock times are interpreted on the entry's original start day; an end
// time before the start is taken to cross midnight.
func parseEntryEdit(input string, oldStart time.Time, running bool) (time.Time, time.Time, error) {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("empty input")
	}
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		start, err := parseClockOn(strings.TrimSpace(parts[0]), oldStart)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		endStr := strings.TrimSpace(parts[1])
		if endStr == "now" {
			if !running {
				return time.Time{}, time.Time{}, fmt.Errorf("'now' is only valid for a running entry")
			}
			return start, time.Time{}, nil
		}
		stop, err := parseClockOn(endStr, oldStart)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if !stop.After(start) {
			stop = stop.AddDate(0, 0, 1) // crosses midnight
		}
		return start, stop, nil
	}
	d, err := time.ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil || d <= 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid input '%s' (use HH:MM-HH:MM or 45m, 1h30m)", input)
	}
	return oldStart, oldStart.Add(d), nil
}

// parseManualEntry resolves user input for a backfilled time entry: a bare
// duration ("45m") becomes [now-d, now] — "I just spent 45m on this" almost
// always means it ends now, not starts now — while a clock range
// ("10:00-11:30") is taken literally on today. Shared by the TUI's
// modeAddTimeEntry and `taskr log` so the two surfaces can't drift.
func parseManualEntry(input string, now time.Time) (start, stop time.Time, err error) {
	start, stop, err = parseEntryEdit(input, now, false)
	if err != nil {
		return start, stop, err
	}
	if !strings.Contains(input, "-") {
		d := stop.Sub(start)
		start = now.Add(-d)
		stop = now
	}
	return start, stop, nil
}

func parseClockOn(s string, day time.Time) (time.Time, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time '%s' (use HH:MM)", s)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, day.Location()), nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	days := hours / 24
	hours = hours % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// ── Builder pool ──────────────────────────────────────────────────────────────

var builderPool = sync.Pool{
	New: func() interface{} {
		b := &strings.Builder{}
		b.Grow(2048)
		return b
	},
}

func getBuilder() *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	return b
}

func putBuilder(b *strings.Builder) {
	builderPool.Put(b)
}

// ── Self-update ───────────────────────────────────────────────────────────────

func selfUpdate() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("could not resolve executable path: %w", err)
	}

	assetName := "taskr"
	switch runtime.GOOS {
	case "windows":
		assetName = "taskr.exe"
	case "darwin":
		// Friendly asset names so Mac users don't need to know their CPU arch.
		if runtime.GOARCH == "arm64" {
			assetName = "taskr-macos-apple-silicon"
		} else {
			assetName = "taskr-macos-intel"
		}
	}
	// Stage the download in a private temp dir, not the shared os.TempDir():
	// a fixed, predictable path like /tmp/taskr is writable by any local user,
	// who could swap the file between the download and the install below.
	stageDir, err := os.MkdirTemp("", "taskr-update-")
	if err != nil {
		return fmt.Errorf("could not create staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)
	tmpFile := filepath.Join(stageDir, assetName)

	cmd := exec.Command("gh", "release", "download", "--repo", "iliorn/taskr",
		"--pattern", assetName, "-D", stageDir, "--clobber")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("download failed: %s", strings.TrimSpace(string(out)))
	}

	if runtime.GOOS == "windows" {
		// Windows forbids overwriting a running executable, but allows
		// renaming it. Move it aside, then copy the new binary into place.
		oldPath := execPath + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(execPath, oldPath); err != nil {
			return fmt.Errorf("could not move old binary aside: %w", err)
		}
		if err := copyFile(tmpFile, execPath); err != nil {
			_ = os.Rename(oldPath, execPath) // restore on failure
			return fmt.Errorf("could not install new binary: %w", err)
		}
		return nil
	}

	// Unix refuses to write into a running executable (ETXTBSY). Copy the
	// new binary next to the old one, then rename over it — rename is
	// atomic and allowed while the process is running.
	newPath := execPath + ".new"
	if err := copyFile(tmpFile, newPath); err != nil {
		return fmt.Errorf("could not stage new binary (check permissions): %w", err)
	}
	if err := os.Rename(newPath, execPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("could not replace binary: %w", err)
	}
	return nil
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// latestRelease returns the tag name of the most recent GitHub release.
func latestRelease() (string, error) {
	cmd := exec.Command("gh", "release", "view", "--repo", "iliorn/taskr",
		"--json", "tagName", "-q", ".tagName")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ── Date parsing ─────────────────────────────────────────────────────────────

// parseDueDate accepts dd-mm-yy, dd-mm-yyyy, and natural language shortcuts.
func parseDueDate(s string) (time.Time, error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch lower {
	case "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	case "yesterday":
		return today.AddDate(0, 0, -1), nil
	case "next week":
		return today.AddDate(0, 0, 7), nil
	case "next month":
		return today.AddDate(0, 1, 0), nil
	}

	if strings.HasPrefix(lower, "next ") {
		dayName := strings.TrimPrefix(lower, "next ")
		if weekday, ok := parseWeekday(dayName); ok {
			return nextWeekday(today, weekday), nil
		}
	}

	if weekday, ok := parseWeekday(lower); ok {
		return nextWeekday(today, weekday), nil
	}

	if strings.HasPrefix(lower, "+") && len(lower) > 2 {
		unit := lower[len(lower)-1]
		numStr := lower[1 : len(lower)-1]
		if n, ok := parsePositiveInt(numStr); ok && n > 0 {
			switch unit {
			case 'd':
				return today.AddDate(0, 0, n), nil
			case 'w':
				return today.AddDate(0, 0, n*7), nil
			case 'm':
				return today.AddDate(0, n, 0), nil
			}
		}
	}

	if t, err := time.Parse("02-01-06", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("02-01-2006", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date: use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+Nd/+Nw/+Nm'")
}

func parseWeekday(s string) (time.Weekday, bool) {
	days := map[string]time.Weekday{
		"monday":    time.Monday,
		"tuesday":   time.Tuesday,
		"wednesday": time.Wednesday,
		"thursday":  time.Thursday,
		"friday":    time.Friday,
		"saturday":  time.Saturday,
		"sunday":    time.Sunday,
		"mon":       time.Monday,
		"tue":       time.Tuesday,
		"wed":       time.Wednesday,
		"thu":       time.Thursday,
		"fri":       time.Friday,
		"sat":       time.Saturday,
		"sun":       time.Sunday,
	}
	if wd, ok := days[s]; ok {
		return wd, true
	}
	return 0, false
}

func nextWeekday(today time.Time, target time.Weekday) time.Time {
	current := today.Weekday()
	daysAhead := int(target) - int(current)
	if daysAhead <= 0 {
		daysAhead += 7
	}
	return today.AddDate(0, 0, daysAhead)
}

func parsePositiveInt(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return n, true
}
