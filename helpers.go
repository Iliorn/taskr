package main

import (
    "fmt"
    "sort"
    "strings"

    "taskr/todo"
)

// ── Pure utilities ────────────────────────────────────────────────────────────

// clamp returns val clamped between min and max.
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
    var lines []string
    for len(runes) > 0 {
        if len(runes) <= width {
            lines = append(lines, string(runes))
            break
        }
        cutAt := width
        for i := width; i > 0; i-- {
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
    for _, tag := range tags {
        sb.WriteString(tagStyle.Render("⟨#"+tag+"⟩") + " ")
    }
    return sb.String()
}

// titleColWidth computes a dynamic title column width based on terminal width.
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

// renderListHeader renders the column header row for the task and history lists.
func renderListHeader(b *strings.Builder, termWidth, cursor, total int, isHistory bool, sortMode taskSortMode) {
    titleW := titleColWidth(termWidth)
    counter := fmt.Sprintf("%d/%d", cursor+1, total)

    var sortLabel string
    switch sortMode {
    case taskSortPriority:
        sortLabel = "  sort:priority"
    case taskSortCreated:
        sortLabel = "  sort:created"
    default:
        sortLabel = "  sort:due"
    }

    const prefix = "      "
    var headerLeft string
    if isHistory {
        headerLeft = prefix + padRight("Completed tasks", titleW) + padRight("Start", 10) +
            padRight("Due", 10) + padRight("Completed", 10) + "Tags"
    } else {
        headerLeft = prefix + padRight("Task", titleW) + padRight("Start", 10) +
            padRight("Due", 10) + padRight("Priority", 10) + "Tags"
    }
    counterFull := counter + dimStyle.Render(sortLabel)
    padW := termWidth - 6 - len([]rune(headerLeft)) - len([]rune(counter)) - len([]rune(sortLabel))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + counterFull + "\n")
    b.WriteString(dimStyle.Render("  "+strings.Repeat("─", termWidth-6)) + "\n")
}

// tagStats holds precomputed done/total counts for a single tag.
type tagStats struct {
    total int
    done  int
}

// computeTagStats returns a map of tag name → tagStats for all todos.
func computeTagStats(todos []todo.Todo) map[string]tagStats {
    stats := make(map[string]tagStats, 16)
    for _, t := range todos {
        for _, tag := range t.Tags {
            s := stats[tag]
            s.total++
            if t.Status == todo.Done {
                s.done++
            }
            stats[tag] = s
        }
    }
    return stats
}

// ── Model helper methods ──────────────────────────────────────────────────────

func (m model) maxDetailHeight() int {
    available := m.termHeight - minHeaderLines - footerHeight - detailBorderLines - minListPanelLines
    if available < minDetailHeight {
        return minDetailHeight
    }
    return available
}

func (m model) detailPage1ContentHeight() int {
    t := m.currentTodo()
    if t == nil {
        return 1
    }
    lines := 9
    lines++
    if len(t.Tags) == 0 {
        lines++
    } else {
        lines += len(t.Tags)
    }
    lines++
    lines++
    if len(t.Dependencies) == 0 {
        lines++
    } else {
        lines += len(t.Dependencies)
    }
    if t.Status == todo.Done && !t.CompletedAt.IsZero() {
        lines++
    }
    return lines
}

func (m model) detailPage2ContentHeight() int {
    t := m.currentTodo()
    if t == nil {
        return 1
    }
    lines := 3
    if len(t.Comments) == 0 {
        lines++
    } else {
        available := m.termWidth - 32
        if available < 10 {
            available = 10
        }
        for _, c := range t.Comments {
            lines += commentLineCount(c.Text, available)
        }
    }
    return lines
}

func (m model) extraOverheadLines() int {
    switch m.mode {
    case modeHelp:
        return 0
    case modeInput, modeEditComment, modeEditTag, modeEditTitle, modeSearch:
        return 3
    case modeSearchDep, modeSearchTag, modeSearchProject:
        return 8
    case modeSearchTagTab:
        return 3
    case modeConfirmDelete, modeConfirmDeleteComment,
        modeConfirmDeleteDep, modeConfirmDeleteTag,
        modeConfirmDeleteTagGlobal, modeConfirmDeleteProject:
        return 1
    }
    return 0
}

func (m model) listVisible() int {
    contentH := m.detailPage1ContentHeight()
    if m.detailPage != 0 {
        contentH = m.detailPage2ContentHeight()
    }
    if maxH := m.maxDetailHeight(); contentH > maxH {
        contentH = maxH
    }
    detailTotal := contentH + 4
    fixedLines := 4
    if m.err != "" {
        fixedLines++
    }
    if m.searchQuery != "" {
        fixedLines++
    }
    fixedLines += m.extraOverheadLines()
    if available := m.termHeight - fixedLines - detailTotal; available >= minListHeight {
        return available
    }
    return minListHeight
}

func (m model) activeTodos() []todo.Todo {
    result := make([]todo.Todo, 0, len(m.todos))
    for _, t := range m.todos {
        if t.Status == todo.Pending && m.matchesSearch(t) {
            result = append(result, t)
        }
    }
    sortTodosByMode(result, m.taskSort)
    return result
}

func (m model) completedTodos() []todo.Todo {
    result := make([]todo.Todo, 0, len(m.todos))
    for _, t := range m.todos {
        if t.Status == todo.Done && m.matchesSearch(t) {
            result = append(result, t)
        }
    }
    sortTodosByMode(result, m.taskSort)
    return result
}

func (m model) matchesSearch(t todo.Todo) bool {
    if m.searchQuery == "" {
        return true
    }
    if strings.HasPrefix(m.searchQuery, "#") {
        tagQuery := strings.ToLower(strings.TrimPrefix(m.searchQuery, "#"))
        for _, tag := range t.Tags {
            if strings.Contains(strings.ToLower(tag), tagQuery) {
                return true
            }
        }
        return false
    }
    return strings.Contains(strings.ToLower(t.Title), strings.ToLower(m.searchQuery))
}

func (m model) allProjectsForList() []string {
    projects := getProjects(m.todos)
    if m.searchQuery == "" {
        return projects
    }
    q := strings.ToLower(m.searchQuery)
    result := make([]string, 0, len(projects))
    for _, p := range projects {
        if strings.Contains(strings.ToLower(p), q) {
            result = append(result, p)
        }
    }
    return result
}

// currentTodoIndex returns the index into m.todos of the currently selected
// todo, or -1 if nothing is selected.
func (m model) currentTodoIndex() int {
    findIndexByID := func(id string) int {
        for i := range m.todos {
            if m.todos[i].ID == id {
                return i
            }
        }
        return -1
    }
    switch m.tab {
    case tabTasks:
        if m.showHistory {
            completed := m.completedTodos()
            if m.cursor < len(completed) {
                return findIndexByID(completed[m.cursor].ID)
            }
        } else {
            active := m.activeTodos()
            if m.cursor < len(active) {
                return findIndexByID(active[m.cursor].ID)
            }
        }
    case tabProjects:
        if m.projectTaskMode {
            projects := m.allProjectsForList()
            if m.projectCursor < len(projects) {
                tasks := getTasksForProject(m.todos, projects[m.projectCursor])
                if m.cursor < len(tasks) {
                    return findIndexByID(tasks[m.cursor].ID)
                }
            }
        }
    }
    return -1
}

// currentTodo returns a pointer to the currently selected todo for READ-ONLY use.
func (m model) currentTodo() *todo.Todo {
    idx := m.currentTodoIndex()
    if idx < 0 {
        return nil
    }
    return &m.todos[idx]
}

func (m model) findTodoByID(id string) *todo.Todo {
    for i := range m.todos {
        if m.todos[i].ID == id {
            return &m.todos[i]
        }
    }
    return nil
}

func (m *model) clampListOffset(listLen int) {
    visible := m.listVisible()
    if m.cursor < m.listOffset {
        m.listOffset = m.cursor
    }
    if m.cursor >= m.listOffset+visible {
        m.listOffset = m.cursor - visible + 1
    }
    if m.listOffset < 0 {
        m.listOffset = 0
    }
    if max := listLen - visible; m.listOffset > max {
        if max < 0 {
            m.listOffset = 0
        } else {
            m.listOffset = max
        }
    }
}

func (m model) depSearchResults() []todo.Todo {
    t := m.currentTodo()
    q := strings.ToLower(m.depSearchQuery)
    result := make([]todo.Todo, 0, len(m.todos))
    for _, candidate := range m.todos {
        if t != nil && candidate.ID == t.ID {
            continue
        }
        if q == "" || strings.Contains(strings.ToLower(candidate.Title), q) {
            result = append(result, candidate)
        }
    }
    return result
}

func (m model) getAllTagsSorted() []string {
    seen := make(map[string]struct{}, len(m.todos))
    tags := make([]string, 0, 16)
    for _, t := range m.todos {
        for _, tag := range t.Tags {
            if _, ok := seen[tag]; !ok {
                seen[tag] = struct{}{}
                tags = append(tags, tag)
            }
        }
    }
    switch m.tagSort {
    case tagSortCount:
        counts := make(map[string]int, len(tags))
        for _, t := range m.todos {
            for _, tag := range t.Tags {
                counts[tag]++
            }
        }
        sort.Slice(tags, func(i, j int) bool {
            if counts[tags[i]] != counts[tags[j]] {
                return counts[tags[i]] > counts[tags[j]]
            }
            return tags[i] < tags[j]
        })
    default:
        sort.Strings(tags)
    }
    return tags
}

func (m model) getFilteredTagsForTab() []string {
    all := m.getAllTagsSorted()
    if m.tagTabSearchQuery == "" {
        return all
    }
    q := strings.ToLower(m.tagTabSearchQuery)
    result := make([]string, 0, len(all))
    for _, tag := range all {
        if strings.Contains(strings.ToLower(tag), q) {
            result = append(result, tag)
        }
    }
    return result
}

func (m model) tagSearchResults() []string {
    allTags := m.getAllTagsSorted()
    t := m.currentTodo()
    q := strings.ToLower(m.tagSearchQuery)
    existing := make(map[string]struct{})
    if t != nil {
        for _, tag := range t.Tags {
            existing[tag] = struct{}{}
        }
    }
    result := make([]string, 0, len(allTags))
    for _, tag := range allTags {
        if _, added := existing[tag]; added {
            continue
        }
        if q == "" || strings.Contains(strings.ToLower(tag), q) {
            result = append(result, tag)
        }
    }
    return result
}

func (m model) projSearchResults() []string {
    allProjects := getProjects(m.todos)
    q := strings.ToLower(m.projSearchQuery)
    result := make([]string, 0, len(allProjects))
    for _, p := range allProjects {
        if q == "" || strings.Contains(strings.ToLower(p), q) {
            result = append(result, p)
        }
    }
    return result
}

func (m *model) renameTagGlobally(oldName, newName string) {
    for i := range m.todos {
        for j, tag := range m.todos[i].Tags {
            if tag == oldName {
                m.todos[i].Tags[j] = newName
            }
        }
    }
}

func (m *model) deleteTagGlobally(tagName string) {
    for i := range m.todos {
        tags := m.todos[i].Tags[:0]
        for _, tag := range m.todos[i].Tags {
            if tag != tagName {
                tags = append(tags, tag)
            }
        }
        m.todos[i].Tags = tags
    }
}

func (m *model) renameProjectGlobally(oldName, newName string) {
    for i := range m.todos {
        if m.todos[i].Project == oldName {
            m.todos[i].Project = newName
        }
    }
}

func (m model) countTasksWithTag(tag string) int {
    n := 0
    for _, t := range m.todos {
        for _, tt := range t.Tags {
            if tt == tag {
                n++
                break
            }
        }
    }
    return n
}

// zeroTime was removed — use time.Time{} directly instead.
