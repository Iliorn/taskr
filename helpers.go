package main

import (
    "fmt"
    "io"
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
    return string(r[:max-3]) + "..."
}

func padRight(s string, width int) string {
    r := []rune(s)
    if len(r) >= width {
        return string(r[:width])
    }
    return s + strings.Repeat(" ", width-len(r))
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

func titleColWidth(termWidth int) int {
    max := termWidth * titleColMaxWidthPct / 100
    w := termWidth - titleColFixedCols
    if w < minTitleColWidth {
        return minTitleColWidth
    }
    if w > max {
        return max
    }
    return w
}

// listCols decides which columns of the task/history list fit at the current
// terminal width. Columns are dropped least-important-first as the window
// narrows so list lines never wrap inside the panel.
type listCols struct {
    titleW    int
    showStart bool
    showDue   bool
    showLast  bool // Priority (tasks) or Completed (history)
}

func taskListCols(termWidth int, isHistory bool) listCols {
    inner := termWidth - 8 // panel content width (margin + border + padding)
    const fixed = 6        // cursor + checkbox + fold icon
    c := listCols{showStart: true, showDue: true, showLast: true}

    colsW := func() int {
        w := 0
        if c.showStart {
            w += 12
        }
        if c.showDue {
            w += 12
        }
        if c.showLast {
            w += 12
        }
        return w
    }

    drop := []*bool{&c.showStart, &c.showLast, &c.showDue}
    if isHistory {
        // History keeps the Completed column the longest.
        drop = []*bool{&c.showStart, &c.showDue, &c.showLast}
    }
    for _, d := range drop {
        if inner-fixed-colsW() >= minTitleColWidth {
            break
        }
        *d = false
    }

    c.titleW = inner - fixed - colsW()
    if c.titleW < minTitleColWidth {
        c.titleW = minTitleColWidth
    }
    if max := termWidth * titleColMaxWidthPct / 100; c.titleW > max && max >= minTitleColWidth {
        c.titleW = max
    }
    return c
}

func renderSortDivider(availW int, sortLabel string) string {
    prefix := "  ↕ sort:" + sortLabel + " "
    remainW := availW - len([]rune(prefix))
    if remainW < 4 {
        remainW = 4
    }
    return dimStyle.Render(prefix+strings.Repeat("─", remainW)) + "\n"
}

func renderPlainDivider(availW int) string {
    return dimStyle.Render("  "+strings.Repeat("─", availW)) + "\n"
}

func renderListHeader(b *strings.Builder, termWidth, cursor, total int, isHistory bool, sortMode taskSortMode) {
    c := taskListCols(termWidth, isHistory)

    startLabel := padRight("Start", 12)
    dueLabel := padRight("Due", 12)
    lastLabel := padRight("Priority", 12)
    if isHistory {
        lastLabel = padRight("Completed", 12)
    } else {
        switch sortMode {
        case taskSortDueDate:
            dueLabel = padRight(">Due<", 12)
        case taskSortStartDate:
            startLabel = padRight(">Start<", 12)
        case taskSortPriority:
            lastLabel = padRight(">Priority<", 12)
        }
    }

    const prefix = "      "
    title := "Task"
    if isHistory {
        title = "Completed tasks"
    }
    headerLeft := prefix + padRight(title, c.titleW)
    if c.showStart {
        headerLeft += startLabel
    }
    if c.showDue {
        headerLeft += dueLabel
    }
    if c.showLast {
        headerLeft += lastLabel
    }
    padW := termWidth - 8 - len([]rune(headerLeft))
    if padW >= 4 {
        headerLeft += "Tags"
        padW -= 4
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
    total int
    done  int
}

func computeTagStats(todos []todo.Todo) map[string]tagStats {
    stats := make(map[string]tagStats, 16)
    for i := range todos {
        for _, tag := range todos[i].Tags {
            s := stats[tag]
            s.total++
            if todos[i].Status == todo.Done {
                s.done++
            }
            stats[tag] = s
        }
    }
    return stats
}

// ── Quick-add parsing ─────────────────────────────────────────────────────────

type parsedTask struct {
    title    string
    tags     []string
    project  string
    dueDate  time.Time
    priority todo.Priority
}

func parseQuickAdd(input string) parsedTask {
    result := parsedTask{priority: todo.PriorityMedium}
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
        default:
            titleWords = append(titleWords, word)
        }
    }

    result.title = strings.Join(titleWords, " ")
    return result
}

// ── Time formatting ───────────────────────────────────────────────────────────

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
        assetName = "taskr-macos-" + runtime.GOARCH
    }
    tmpFile := filepath.Join(os.TempDir(), assetName)
    defer os.Remove(tmpFile)

    cmd := exec.Command("gh", "release", "download", "--repo", "luciphere/taskr",
        "--pattern", assetName, "-D", os.TempDir(), "--clobber")
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

    if err := copyFile(tmpFile, execPath); err != nil {
        return fmt.Errorf("could not replace binary (try sudo): %w", err)
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
