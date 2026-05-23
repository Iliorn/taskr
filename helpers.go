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
    titleW := titleColWidth(termWidth)
    counter := fmt.Sprintf("%d/%d", cursor+1, total)

    const prefix = "      "
    var headerLeft string
    if isHistory {
        headerLeft = prefix + padRight("Completed tasks", titleW) + padRight("Start", 10) +
            padRight("Due", 10) + padRight("Completed", 10) + "Tags"
    } else {
        headerLeft = prefix + padRight("Task", titleW) + padRight("Start", 10) +
            padRight("Due", 10) + padRight("Priority", 10) + "Tags"
    }
    padW := termWidth - 6 - len([]rune(headerLeft)) - len([]rune(counter))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + counter + "\n")

    var sortLabel string
    switch sortMode {
    case taskSortPriority:
        sortLabel = "priority"
    case taskSortCreated:
        sortLabel = "created"
    default:
        sortLabel = "due"
    }
    b.WriteString(renderSortDivider(termWidth-8, sortLabel))
}

// ── Editor support ────────────────────────────────────────────────────────────

func resolveEditorCmd() string {
    if editor := os.Getenv("EDITOR"); editor != "" {
        if path, err := exec.LookPath(editor); err == nil {
            return path
        }
    }
    for _, candidate := range []string{"hx", "helix", "nvim", "vim", "nano"} {
        if path, err := exec.LookPath(candidate); err == nil {
            return path
        }
    }
    return ""
}

func getEditorCmd() string {
    return resolveEditorCmd()
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
    result := parsedTask{priority: todo.PriorityLow}
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

    tmpFile := execPath + ".new"
    defer os.Remove(tmpFile)

    var downloadArgs []string
    if runtime.GOOS == "windows" {
        downloadArgs = []string{"release", "download", "--repo", "luciphere/taskr", "--pattern", "taskr.exe", "-D", filepath.Dir(tmpFile), "--clobber"}
        tmpFile = filepath.Join(filepath.Dir(execPath), "taskr.exe.new")
    } else {
        downloadArgs = []string{"release", "download", "--repo", "luciphere/taskr", "--pattern", "taskr", "-D", os.TempDir(), "--clobber"}
        tmpFile = filepath.Join(os.TempDir(), "taskr")
    }

    cmd := exec.Command("gh", downloadArgs...)
    if out, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("download failed: %s", strings.TrimSpace(string(out)))
    }

    src, err := os.Open(tmpFile)
    if err != nil {
        return fmt.Errorf("could not open downloaded file: %w", err)
    }
    defer src.Close()

    dst, err := os.OpenFile(execPath, os.O_WRONLY|os.O_TRUNC, 0755)
    if err != nil {
        return fmt.Errorf("could not write to %s (try sudo): %w", execPath, err)
    }
    defer dst.Close()

    if _, err := io.Copy(dst, src); err != nil {
        return fmt.Errorf("could not replace binary: %w", err)
    }

    return nil
}
