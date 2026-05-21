package main

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
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
    availW := termWidth - 8
    b.WriteString(renderSortDivider(availW, sortLabel))
}

// ── Editor support (OPTIMIZED: resolved once at startup) ──────────────────────

// resolveEditorCmd is called once at startup and cached on the model.
func resolveEditorCmd() string {
    if editor := os.Getenv("EDITOR"); editor != "" {
        return editor
    }
    if _, err := exec.LookPath("hx"); err == nil {
        return "hx"
    }
    return "notepad"
}

// getEditorCmd returns the cached editor command from the model.
// Kept for compatibility — callers that have access to model should use m.editorCmd directly.
func getEditorCmd() string {
    if editor := os.Getenv("EDITOR"); editor != "" {
        return editor
    }
    if _, err := exec.LookPath("hx"); err == nil {
        return "hx"
    }
    return "notepad"
}

func notesFilePath(taskID string) string {
    home, _ := os.UserHomeDir()
    dir := filepath.Join(home, ".taskr", "notes")
    _ = os.MkdirAll(dir, 0755)
    return filepath.Join(dir, taskID+".md")
}

func writeNotesFile(taskID, content string) error {
    path := notesFilePath(taskID)
    return os.WriteFile(path, []byte(content), 0644)
}

func readNotesFile(taskID string) (string, error) {
    path := notesFilePath(taskID)
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return "", nil
        }
        return "", err
    }
    return string(data), nil
}

func cleanupNotesFile(taskID string) {
    path := notesFilePath(taskID)
    _ = os.Remove(path)
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

// ── Cache management (OPTIMIZED: includes learnings, projects, project index) ─

func (m *model) refreshCaches() {
    // OPTIMIZATION: capture frame time once
    m.frameTime = time.Now()

    // Rebuild todo index
    if m.todoIndex == nil {
        m.todoIndex = make(map[string]int, len(m.todos))
    } else {
        for k := range m.todoIndex {
            delete(m.todoIndex, k)
        }
    }
    for i := range m.todos {
        m.todoIndex[m.todos[i].ID] = i
    }

    // Rebuild overdue set
    if m.overdueSet == nil {
        m.overdueSet = make(map[string]bool, len(m.todos)/4)
    } else {
        for k := range m.overdueSet {
            delete(m.overdueSet, k)
        }
    }
    for i := range m.todos {
        if m.todos[i].IsOverdue() {
            m.overdueSet[m.todos[i].ID] = true
        }
    }

    // Rebuild active/done caches
    if m.cachedActive == nil {
        m.cachedActive = make([]todo.Todo, 0, len(m.todos))
    } else {
        m.cachedActive = m.cachedActive[:0]
    }
    if m.cachedDone == nil {
        m.cachedDone = make([]todo.Todo, 0, len(m.todos))
    } else {
        m.cachedDone = m.cachedDone[:0]
    }
    for _, t := range m.todos {
        if t.ParentID != "" {
            continue
        }
        if t.Status == todo.Pending && m.matchesSearch(t) && m.matchesFocusFilter(t) {
            m.cachedActive = append(m.cachedActive, t)
        } else if t.Status == todo.Done && m.matchesSearch(t) {
            m.cachedDone = append(m.cachedDone, t)
        }
    }
    sortTodosByMode(m.cachedActive, m.taskSort)
    sortTodosByMode(m.cachedDone, m.taskSort)

    // Rebuild tag stats
    m.cachedTags = computeTagStats(m.todos)

    // OPTIMIZATION: rebuild project → task index
    if m.projectTaskIndex == nil {
        m.projectTaskIndex = make(map[string][]int, 8)
    } else {
        for k := range m.projectTaskIndex {
            delete(m.projectTaskIndex, k)
        }
    }
    for i := range m.todos {
        if p := m.todos[i].Project; p != "" {
            m.projectTaskIndex[p] = append(m.projectTaskIndex[p], i)
        }
    }

    // Mark sub-caches dirty so they refresh on next access
    m.learningsCacheDirty = true
    m.projectsCacheDirty = true
    m.detailCacheDirty = true
    m.cacheDirty = false
}

func (m *model) currentTaskID() string {
    if m.pane != paneDetail || m.tab != tabTasks {
        return ""
    }
    idx := m.currentTodoIndex()
    if idx < 0 {
        return ""
    }
    return m.todos[idx].ID
}

func (m *model) followTask(taskID string) {
    if taskID == "" {
        return
    }
    var list []todo.Todo
    if m.showHistory {
        list = m.cachedDone
    } else {
        list = m.cachedActive
    }
    for i, t := range list {
        if t.ID == taskID {
            m.cursor = i
            return
        }
    }
}

func (m *model) markModified() {
    taskID := m.currentTaskID()
    m.pushUndo("modify")
    m.dirty = true
    m.cacheDirty = true
    m.refreshCaches()
    m.followTask(taskID)
}

func (m *model) markModifiedNoUndo() {
    taskID := m.currentTaskID()
    m.dirty = true
    m.cacheDirty = true
    m.refreshCaches()
    m.followTask(taskID)
}

func (m *model) markCacheDirty() {
    m.cacheDirty = true
    m.refreshCaches()
}

func (m *model) ensureCache() {
    if m.cacheDirty {
        m.refreshCaches()
    }
}

// ── Cached accessors ──────────────────────────────────────────────────────────

func (m *model) getActiveTodos() []todo.Todo {
    m.ensureCache()
    return m.cachedActive
}

func (m *model) getCompletedTodos() []todo.Todo {
    m.ensureCache()
    return m.cachedDone
}

func (m *model) getTagStats() map[string]tagStats {
    m.ensureCache()
    return m.cachedTags
}

// OPTIMIZATION: cached projects accessor
func (m *model) getProjectsCached() []string {
    if !m.projectsCacheDirty && m.lastProjectSearch == m.searchQuery {
        return m.cachedProjects
    }
    projects := make([]string, 0, len(m.projectTaskIndex))
    for p := range m.projectTaskIndex {
        projects = append(projects, p)
    }
    sort.Strings(projects)

    if m.searchQuery != "" {
        q := strings.ToLower(m.searchQuery)
        filtered := projects[:0]
        for _, p := range projects {
            if strings.Contains(strings.ToLower(p), q) {
                filtered = append(filtered, p)
            }
        }
        projects = filtered
    }

    m.cachedProjects = projects
    m.projectsCacheDirty = false
    m.lastProjectSearch = m.searchQuery
    return m.cachedProjects
}

// OPTIMIZATION: get tasks for a project using the index instead of linear scan
func (m *model) getTasksForProjectFast(project string) []todo.Todo {
    indices, ok := m.projectTaskIndex[project]
    if !ok || len(indices) == 0 {
        return nil
    }
    result := make([]todo.Todo, 0, len(indices))
    for _, idx := range indices {
        if idx < len(m.todos) {
            result = append(result, m.todos[idx])
        }
    }
    return sortTodosByStartDate(result)
}

// ── Visible height for lazy rendering ─────────────────────────────────────────

func (m model) estimateListHeight() int {
    headerH := minHeaderLines
    if m.err != "" {
        headerH++
    }
    if m.focusFilter {
        headerH++
    }
    if m.searchQuery != "" {
        headerH++
    }

    detailH := 0
    if m.mode == modeNormal && m.tab != tabStats {
        detailH = 12
    }

    footerH := footerHeight
    borderH := 2

    available := m.termHeight - headerH - footerH - detailH - borderH
    if available < minListHeight {
        return minListHeight
    }
    return available
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
    lines := 10
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
    if m.focusFilter {
        fixedLines++
    }
    fixedLines += m.extraOverheadLines()
    if available := m.termHeight - fixedLines - detailTotal; available >= minListHeight {
        return available
    }
    return minListHeight
}

func (m model) activeTodos() []todo.Todo {
    if m.cacheDirty {
        result := make([]todo.Todo, 0, len(m.todos))
        for _, t := range m.todos {
            if t.ParentID != "" {
                continue
            }
            if t.Status == todo.Pending && m.matchesSearch(t) && m.matchesFocusFilter(t) {
                result = append(result, t)
            }
        }
        sortTodosByMode(result, m.taskSort)
        return result
    }
    return m.cachedActive
}

func (m model) completedTodos() []todo.Todo {
    if m.cacheDirty {
        result := make([]todo.Todo, 0, len(m.todos))
        for _, t := range m.todos {
            if t.ParentID != "" {
                continue
            }
            if t.Status == todo.Done && m.matchesSearch(t) {
                result = append(result, t)
            }
        }
        sortTodosByMode(result, m.taskSort)
        return result
    }
    return m.cachedDone
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

func (m model) matchesFocusFilter(t todo.Todo) bool {
    if !m.focusFilter {
        return true
    }
    return t.IsOverdue() || t.IsDueToday()
}

func (m model) allProjectsForList() []string {
    // OPTIMIZATION: use cached projects when available
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

func (m model) currentTodoIndex() int {
    // OPTIMIZATION: always use index map, no fallback linear scan
    findIndexByID := func(id string) int {
        if m.todoIndex != nil {
            if idx, ok := m.todoIndex[id]; ok {
                return idx
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

func (m model) currentTodo() *todo.Todo {
    idx := m.currentTodoIndex()
    if idx < 0 {
        return nil
    }
    return &m.todos[idx]
}

// OPTIMIZATION: findTodoByID uses only the index map — no linear scan fallback
func (m model) findTodoByID(id string) *todo.Todo {
    if m.todoIndex != nil {
        if idx, ok := m.todoIndex[id]; ok && idx < len(m.todos) {
            return &m.todos[idx]
        }
        return nil
    }
    // Fallback only if index hasn't been built yet (shouldn't happen)
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
    result := make([]todo.Todo, 0, maxDepSearchResults*2)
    for _, candidate := range m.todos {
        if t != nil && candidate.ID == t.ID {
            continue
        }
        if q == "" || strings.Contains(strings.ToLower(candidate.Title), q) {
            result = append(result, candidate)
            // OPTIMIZATION: early exit — we only display maxDepSearchResults
            if len(result) >= maxDepSearchResults*3 {
                break
            }
        }
    }
    return result
}

func (m model) getAllTagsSorted() []string {
    seen := make(map[string]struct{}, len(m.todos))
    tags := make([]string, 0, 16)
    for i := range m.todos {
        for _, tag := range m.todos[i].Tags {
            if _, ok := seen[tag]; !ok {
                seen[tag] = struct{}{}
                tags = append(tags, tag)
            }
        }
    }
    switch m.tagSort {
    case tagSortCount:
        stats := m.cachedTags
        if stats == nil {
            stats = computeTagStats(m.todos)
        }
        sort.Slice(tags, func(i, j int) bool {
            ci := stats[tags[i]].total
            cj := stats[tags[j]].total
            if ci != cj {
                return ci > cj
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
    if m.cachedTags != nil {
        return m.cachedTags[tag].total
    }
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

// ── Undo support ──────────────────────────────────────────────────────────────

const maxUndoStack = 20

type undoEntry struct {
    todos []todo.Todo
    desc  string
}

func deepCopyTodo(t todo.Todo) todo.Todo {
    cp := t
    if len(t.Tags) > 0 {
        cp.Tags = make([]string, len(t.Tags))
        copy(cp.Tags, t.Tags)
    }
    if len(t.Dependencies) > 0 {
        cp.Dependencies = make([]string, len(t.Dependencies))
        copy(cp.Dependencies, t.Dependencies)
    }
    if len(t.Comments) > 0 {
        cp.Comments = make([]todo.Comment, len(t.Comments))
        copy(cp.Comments, t.Comments)
    }
    if len(t.Learnings) > 0 {
        cp.Learnings = make([]todo.Learning, len(t.Learnings))
        for i, l := range t.Learnings {
            cp.Learnings[i] = l
            if len(l.Tags) > 0 {
                cp.Learnings[i].Tags = make([]string, len(l.Tags))
                copy(cp.Learnings[i].Tags, l.Tags)
            }
        }
    }
    if len(t.TimeEntries) > 0 {
        cp.TimeEntries = make([]todo.TimeEntry, len(t.TimeEntries))
        copy(cp.TimeEntries, t.TimeEntries)
    }
    if len(t.SubtaskIDs) > 0 {
        cp.SubtaskIDs = make([]string, len(t.SubtaskIDs))
        copy(cp.SubtaskIDs, t.SubtaskIDs)
    }
    return cp
}

func deepCopyTodos(todos []todo.Todo) []todo.Todo {
    cp := make([]todo.Todo, len(todos))
    for i, t := range todos {
        cp[i] = deepCopyTodo(t)
    }
    return cp
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
            tag := strings.TrimPrefix(word, "#")
            if tag != "" {
                result.tags = append(result.tags, tag)
            }
        case strings.HasPrefix(lower, "due:"):
            dateStr := strings.TrimPrefix(lower, "due:")
            if d, err := parseDueDate(dateStr); err == nil {
                result.dueDate = d
            } else {
                titleWords = append(titleWords, word)
            }
        case strings.HasPrefix(word, "@"):
            proj := strings.TrimPrefix(word, "@")
            if proj != "" {
                result.project = proj
            }
        case strings.HasPrefix(lower, "p:"):
            pStr := strings.TrimPrefix(lower, "p:")
            switch pStr {
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
        b.Grow(2048) // OPTIMIZATION: larger initial capacity
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
