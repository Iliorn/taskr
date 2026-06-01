package main

import (
    "fmt"
    "strings"
    "time"

    "github.com/charmbracelet/lipgloss"
    "taskr/todo"
)

// ── Top-level View ────────────────────────────────────────────────────────────

func (m model) View() string {
    if m.mode == modeHelp {
        return m.renderHelpFullscreen()
    }

    out := getBuilder()
    defer putBuilder(out)

    w := m.termWidth - 6

    // ── HEADER ───────────────────────────────────────────────────────────
    shortcutHint := helpStyle.Render("? shortcuts")
    tabsStr := titleStyle.Render("taskr") + "  " + m.renderTabs()
    padW := m.termWidth - len([]rune(tabsStr)) - len([]rune("? shortcuts")) - 4
    if padW < 1 {
        padW = 1
    }
    out.WriteString(tabsStr + strings.Repeat(" ", padW) + shortcutHint + "\n")
    out.WriteString("\n")

    if m.err != "" {
        out.WriteString(confirmStyle.Render(m.err) + "\n")
    }
    if m.focusFilter {
        out.WriteString(confirmStyle.Render("⚡ FOCUS: today + overdue only (f to toggle)") + "\n")
    }
    if m.searchQuery != "" {
        out.WriteString(searchStyle.Render("/ "+m.searchQuery) + "\n")
    }
    if m.tab == tabTags && m.tagTabSearchQuery != "" {
        out.WriteString(searchStyle.Render("/ "+m.tagTabSearchQuery) + "\n")
    }
    if m.tab == tabLearnings && m.learningSearchQuery != "" {
        out.WriteString(searchStyle.Render("/ "+m.learningSearchQuery) + "\n")
    }

    // ── FOOTER ───────────────────────────────────────────────────────────
    footerContent := m.buildFooterContent(w)
    footerLines := 0
    if footerContent != "" {
        footerLines = strings.Count(footerContent, "\n") + 1
    }

    // ── DETAIL (with caching) ────────────────────────────────────────────
    var detailContent string
    detailLineCount := 0
    showDetail := m.mode == modeNormal

    if showDetail {
        switch {
        case m.tab == tabTags || m.tab == tabLearnings || m.tab == tabStats:
            detailContent = m.buildDetailContent()
        default:
            detailContent = m.getCachedDetailContent()
        }

        if detailContent != "" {
            detailContent = detailPanelStyle.Width(w).Render(m.applyDetailScroll(detailContent))
            detailSplit := strings.Split(detailContent, "\n")
            for len(detailSplit) > 0 && strings.TrimSpace(detailSplit[len(detailSplit)-1]) == "" {
                detailSplit = detailSplit[:len(detailSplit)-1]
            }
            detailContent = strings.Join(detailSplit, "\n")
            detailLineCount = len(detailSplit)
        }
    }

    // ── LAYOUT ───────────────────────────────────────────────────────────
    li := computeLayout(layoutInput{
        termW:             m.termWidth,
        termH:             m.termHeight,
        hasErr:            m.err != "",
        hasSearch:         m.searchQuery != "",
        hasFocus:          m.focusFilter,
        hasTagSearch:      m.tab == tabTags && m.tagTabSearchQuery != "",
        hasLearningSearch: m.tab == tabLearnings && m.learningSearchQuery != "",
        mode:              m.mode,
        tab:               m.tab,
        detailLines:       detailLineCount,
    })

    // ── LIST ─────────────────────────────────────────────────────────────
    target := m.termHeight
    availableForList := target - li.headerH - detailLineCount - footerLines
    if availableForList < minListHeight {
        availableForList = minListHeight
    }
    listContent := m.buildListContent(w, availableForList)
    listSplit := strings.Split(listContent, "\n")
    for len(listSplit) > 0 && strings.TrimSpace(listSplit[len(listSplit)-1]) == "" {
        listSplit = listSplit[:len(listSplit)-1]
    }

    // ── ASSEMBLE ─────────────────────────────────────────────────────────
    for len(listSplit) > availableForList {
        listSplit = listSplit[:len(listSplit)-1]
    }
    for len(listSplit) < availableForList {
        listSplit = append(listSplit, "")
    }
    for _, line := range listSplit {
        out.WriteString(line + "\n")
    }
    if detailContent != "" {
        out.WriteString(detailContent + "\n")
    }
    if footerContent != "" {
        out.WriteString(footerContent)
    }
    result := out.String()
    resultLines := strings.Split(result, "\n")
    for len(resultLines) < target {
        resultLines = append(resultLines, "")
    }
    if len(resultLines) > target {
        resultLines = resultLines[:target]
    }

    for i, line := range resultLines {
        resultLines[i] = " " + line
    }
    return strings.Join(resultLines, "\n")

}
// ── Detail scroll ────────────────────────────────────────────────────────────

func (m model) applyDetailScroll(content string) string {
    maxVisible := m.termHeight*detailMaxHeightPct/100 - 2
    if maxVisible < 3 {
        maxVisible = 3
    }
    lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
    if len(lines) <= maxVisible {
        return strings.Join(lines, "\n")
    }

    cursorLine := m.estimateDetailCursorLine()
    if cursorLine >= len(lines) {
        cursorLine = len(lines) - 1
    }

    scrollStart := cursorLine - 2
    if scrollStart < 0 {
        scrollStart = 0
    }
    if scrollStart+maxVisible > len(lines) {
        scrollStart = len(lines) - maxVisible
    }
    if scrollStart < 0 {
        scrollStart = 0
    }
    if scrollStart <= 2 {
        scrollStart = 0
    }
    end := scrollStart + maxVisible
    if end > len(lines) {
        end = len(lines)
    }

    visible := make([]string, end-scrollStart)
    copy(visible, lines[scrollStart:end])

    if scrollStart > 0 {
        visible[0] = dimStyle.Render(fmt.Sprintf("  ↑ %d more above", scrollStart))
    }
    if end < len(lines) {
        visible[len(visible)-1] = dimStyle.Render(fmt.Sprintf("  ↓ %d more below", len(lines)-end))
    }

    return strings.Join(visible, "\n")
}

// ── Footer builder ────────────────────────────────────────────────────────────


func (m model) buildFooterContent(w int) string {
    switch m.mode {
    case modeInput, modeEditComment, modeEditTag, modeEditTitle,
        modeAddLearning, modeEditLearning, modeAddSubtask, modeEditProjectInline:
        return inputStyle.Width(w).Render(m.textInput.View())
    case modeSearch:
        if m.tab == tabLearnings {
            return searchStyle.Width(w).Render(m.learningSearchInput.View())
        }
        return searchStyle.Width(w).Render(m.searchInput.View())
    case modeSearchTagTab:
        return searchStyle.Width(w).Render(m.tagTabSearchInput.View())
    case modeSearchDep:
        b := getBuilder()
        defer putBuilder(b)
        b.WriteString(searchStyle.Width(w).Render(m.depSearchInput.View()))
        for i, r := range m.depSearchResults() {
            if i >= maxDepSearchResults {
                break
            }
            if i == m.depSearch.cursor {
                b.WriteString("\n" + selectedStyle.Render("  → "+r.Title))
            } else {
                b.WriteString("\n" + normalStyle.Render("    "+r.Title))
            }
        }
        return b.String()
    case modeSearchTag:
        b := getBuilder()
        defer putBuilder(b)
        b.WriteString(searchStyle.Width(w).Render(m.tagSearchInput.View()))
        results := m.tagSearchResults()
        for i, r := range results {
            if i >= maxTagSearchResults {
                break
            }
            if i == m.tagSearch.cursor {
                b.WriteString("\n" + selectedStyle.Render("  → #"+r))
            } else {
                b.WriteString("\n" + normalStyle.Render("    #"+r))
            }
        }
        if len(results) == 0 && m.tagSearch.query != "" {
            b.WriteString("\n" + dimStyle.Render("  → create new tag: ") + tagStyle.Render(m.tagSearch.query))
        }
        return b.String()
    case modeSearchProject:
        b := getBuilder()
        defer putBuilder(b)
        b.WriteString(searchStyle.Width(w).Render(m.projSearchInput.View()))
        results := m.projSearchResults()
        for i, r := range results {
            if i >= maxProjSearchResults {
                break
            }
            if i == m.projSearch.cursor {
                b.WriteString("\n" + selectedStyle.Render("  → "+r))
            } else {
                b.WriteString("\n" + normalStyle.Render("    "+r))
            }
        }
        if len(results) == 0 && m.projSearch.query != "" {
            b.WriteString("\n" + dimStyle.Render("  → create new project: ") + selectedStyle.Render(m.projSearch.query))
        }
        return b.String()
    case modeConfirmDelete, modeConfirmDeleteComment,
        modeConfirmDeleteDep, modeConfirmDeleteTag,
        modeConfirmDeleteTagGlobal, modeConfirmDeleteProject,
        modeConfirmDeleteLearning, modeConfirmDeleteSubtask:
        return confirmStyle.Render(m.confirmMsg)
    }
    return ""
}

// ── Detail content ────────────────────────────────────────────────────────────

func (m model) buildDetailContent() string {
    switch {
    case m.tab == tabTags:
        lines := m.buildTagDetailLines()
        if len(lines) == 0 {
            return ""
        }
        return strings.Join(lines, "\n")
    case m.tab == tabLearnings:
        lines := m.buildLearningDetailLines()
        if len(lines) == 0 {
            return ""
        }
        return strings.Join(lines, "\n")
    case m.tab == tabStats:
        return m.renderStatsDetail()
    default:
        t := m.currentTodo()
        if t == nil {
            return dimStyle.Render("  No task selected.")
        }
        if m.detail.page == 0 {
            return m.renderDetailPage1(t)
        }
        if m.detail.page == 1 {
            return m.renderDetailPage2(t)
        }
        return m.renderDetailPage3(t)
    }
}

// ── List content builder ──────────────────────────────────────────────────────

func (m model) buildListContent(w, outerH int) string {
    if m.tab == tabProjects {
        return m.buildProjectListContent(w, outerH)
    }

    innerH := outerH - 2 // subtract top and bottom border lines
    if innerH < 1 {
        innerH = 1
    }
    rawList := m.buildListLines()
    for len(rawList) < innerH {
        rawList = append(rawList, "")
    }
    if len(rawList) > innerH {
        rawList = rawList[:innerH]
    }
    return listPanelStyle.Width(w).Render(strings.Join(rawList, "\n"))
}

func (m model) buildProjectListContent(w, listH int) string {
    projects := m.allProjectsForList()
    if len(projects) == 0 {
        empty := normalStyle.Render("  No projects yet. Add a project to a task first.")
        if m.searchQuery != "" {
            empty = normalStyle.Render("  No projects match your search.")
        }
        innerH := listH - 2
        if innerH < 1 {
            innerH = 1
        }
        emptyLines := strings.Split(empty, "\n")
        for len(emptyLines) < innerH {
            emptyLines = append(emptyLines, "")
        }
        if len(emptyLines) > innerH {
            emptyLines = emptyLines[:innerH]
        }
        return listPanelStyle.Width(w).Render(strings.Join(emptyLines, "\n"))
    }

    projMaxH := listH / 3
    if projMaxH < minListPanelLines {
        projMaxH = minListPanelLines
    }
    projLines := strings.Split(m.renderProjectListContent(projects), "\n")
    projEnd := len(projLines)
    for projEnd > 0 && strings.TrimSpace(projLines[projEnd-1]) == "" {
        projEnd--
    }
    projLines = projLines[:projEnd]
    if len(projLines) > projMaxH {
        projLines = projLines[:projMaxH]
    }
    for len(projLines) < projMaxH {
        projLines = append(projLines, "")
    }
    projRendered := listPanelStyle.Width(w).Render(strings.Join(projLines, "\n"))

    projRenderedLines := strings.Split(projRendered, "\n")
    ganttOuterH := listH - len(projRenderedLines)
    if ganttOuterH < minListPanelLines+2 {
        ganttOuterH = minListPanelLines + 2
    }
    ganttInnerH := ganttOuterH - 2
    if ganttInnerH < 1 {
        ganttInnerH = 1
    }

    var ganttLines []string
    if m.projectCursor < len(projects) {
        tasks := m.getProjectTasks(projects[m.projectCursor])
        ganttContent := m.renderGantt(tasks)
        ganttLines = strings.Split(ganttContent, "\n")
        ganttEnd := len(ganttLines)
        for ganttEnd > 0 && strings.TrimSpace(ganttLines[ganttEnd-1]) == "" {
            ganttEnd--
        }
        ganttLines = ganttLines[:ganttEnd]
    }
    if len(ganttLines) > ganttInnerH {
        ganttLines = ganttLines[:ganttInnerH]
    }
    for len(ganttLines) < ganttInnerH {
        ganttLines = append(ganttLines, "")
    }
    ganttRendered := listPanelStyle.Width(w).Render(strings.Join(ganttLines, "\n"))

    b := getBuilder()
    defer putBuilder(b)
    b.WriteString(projRendered)
    b.WriteString("\n")
    b.WriteString(ganttRendered)
    return b.String()
}

// ── Help ──────────────────────────────────────────────────────────────────────

func (m model) renderHelpFullscreen() string {
    sections := []struct {
        title string
        keys  [][2]string
    }{
        {"Navigation", [][2]string{
            {"↑/↓  or  j/k", "navigate list"},
            {"enter", "open details"},
            {"esc", "go back"},
            {"tab  or  1-5", "switch tabs"},
            {"?", "close help"},
        }},
        {"Tasks", [][2]string{
            {"a", "add task (quick-add: #tag due:date p:high @proj)"},
            {"r", "rename task"},
            {"d", "toggle done"},
            {"x", "delete"},
            {"n", "edit notes (opens $EDITOR)"},
            {"f", "focus: today + overdue only"},
            {"h", "toggle history"},
            {"s", "cycle sort order"},
            {"←/→", "expand/collapse subtasks"},
            {"/", "search"},
        }},
        {"Detail view", [][2]string{
            {"←/→", "switch pages"},
            {"enter", "edit field / toggle subtask"},
            {"n", "edit notes (opens $EDITOR)"},
            {"a", "add tag / dep / comment / learning / subtask"},
            {"d", "toggle subtask done"},
            {"x", "remove field / delete subtask"},
        }},
        {"Tags & Projects", [][2]string{
            {"r", "rename globally"},
            {"x", "delete globally"},
            {"s", "toggle sort"},
            {"/", "filter"},
        }},
        {"Learnings", [][2]string{
            {"r", "edit learning"},
            {"x", "delete learning"},
            {"s", "sort date/alpha"},
        }},
        {"Stats (tab 5)", [][2]string{
            {"5 or tab", "switch to stats view"},
        }},
        {"App", [][2]string{
            {"U", "self-update to latest release"},
        }},
        {"Date input", [][2]string{
            {"dd-mm-yy", "exact date (e.g. 15-06-25)"},
            {"today", "today's date"},
            {"tomorrow", "tomorrow"},
            {"next week", "7 days from now"},
            {"next month", "1 month from now"},
            {"monday..sunday", "next occurrence of weekday"},
            {"+3d / +2w / +1m", "relative days/weeks/months"},
        }},
    }

    b := getBuilder()
    defer putBuilder(b)

    b.WriteString("\n")
    b.WriteString(titleStyle.Render("  Keyboard shortcuts") + "\n")
    b.WriteString("\n")

    for _, section := range sections {
        b.WriteString(detailLabelStyle.Render("  "+section.title) + "\n")
        for _, kv := range section.keys {
            key := padRight(kv[0], 24)
            b.WriteString(
                helpStyle.Render("  ") +
                    selectedStyle.Render(key) +
                    normalStyle.Render(kv[1]) + "\n")
        }
        b.WriteString("\n")
    }

    b.WriteString("\n")
    b.WriteString(helpStyle.Render("  Press ? or esc to close") + "\n")

    lines := strings.Split(b.String(), "\n")
    target := m.termHeight - 1
    for len(lines) < target {
        lines = append(lines, "")
    }
    if len(lines) > target {
        lines = lines[:target]
    }

    return strings.Join(lines, "\n")
}

// ── Stats detail (activity heatmap) ──────────────────────────────────────────

func (m model) renderStatsDetail() string {
    b := getBuilder()
    defer putBuilder(b)

    now := m.frameTime
    today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

    counts := make([]int, 30)
    maxCount := 0
    total := 0
    for i := range m.todos {
        t := &m.todos[i]
        if t.Status != todo.Done || t.CompletedAt.IsZero() || t.ParentID != "" {
            continue
        }
        d := time.Date(t.CompletedAt.Year(), t.CompletedAt.Month(), t.CompletedAt.Day(), 0, 0, 0, 0, t.CompletedAt.Location())
        daysAgo := int(today.Sub(d).Hours() / 24)
        if daysAgo >= 0 && daysAgo < 30 {
            idx := 29 - daysAgo
            counts[idx]++
            total++
            if counts[idx] > maxCount {
                maxCount = counts[idx]
            }
        }
    }

    gradLen := len(statsGradient)
    availW := m.termWidth - 10
    headerLabel := "  Activity — last 30 days"
    totalStr := fmt.Sprintf("%d completed", total)
    padW := availW - len([]rune(headerLabel)) - len([]rune(totalStr))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(statsHeaderStyle.Render(headerLabel) + strings.Repeat(" ", padW) + dimStyle.Render(totalStr) + "\n\n")

    b.WriteString("  ")
    for i := 0; i < 30; i++ {
        count := counts[i]
        if count == 0 {
            b.WriteString(dimStyle.Render("░░"))
        } else {
            gradIdx := gradLen - 1
            if maxCount > 1 {
                gradIdx = int(float64(count-1) / float64(maxCount-1) * float64(gradLen-1))
                if gradIdx >= gradLen {
                    gradIdx = gradLen - 1
                }
            }
            b.WriteString(statsGradient[gradIdx].Render("██"))
        }
        if i < 29 {
            b.WriteString(" ")
        }
    }
    b.WriteString("\n")

    b.WriteString("  ")
    for i := 0; i < 30; i++ {
        day := today.AddDate(0, 0, -(29 - i))
        if i%5 == 0 || i == 29 {
            b.WriteString(dimStyle.Render(day.Format("02")))
        } else {
            b.WriteString("  ")
        }
        if i < 29 {
            b.WriteString(" ")
        }
    }
    b.WriteString("\n")

    return b.String()
}

// ── Build helpers ─────────────────────────────────────────────────────────────

func (m model) buildListLines() []string {
    return strings.Split(m.renderListContent(), "\n")
}

func (m model) buildLearningDetailLines() []string {
    learnings := m.allLearnings()
    if len(learnings) == 0 || m.learningCursor >= len(learnings) {
        return strings.Split(dimStyle.Render("  No learning selected."), "\n")
    }

    l := learnings[m.learningCursor]
    b := getBuilder()
    defer putBuilder(b)
    availW := m.termWidth - 8

    b.WriteString(learningSelectedStyle.Render("  "+truncate(l.Text, availW)) + "\n\n")

    wrapped := wrapText(l.Text, availW-2)
    if len(wrapped) > 1 {
        for _, line := range wrapped {
            b.WriteString(normalStyle.Render("  "+line) + "\n")
        }
        b.WriteString("\n")
    }

    sourceLabel := "  " + detailLabelStyle.Render("Source task:  ")
    source := m.findLearningSource(l.ID)
    if source != nil {
        status := ""
        if source.Status == todo.Done {
            status = "  " + checkDoneStyle.Render("[done]")
        }
        b.WriteString(sourceLabel + normalStyle.Render(source.Title) + status + "\n")
    } else {
        b.WriteString(sourceLabel + dimStyle.Render("[task removed]") + "\n")
    }

    b.WriteString("  " + detailLabelStyle.Render("Date:         ") +
        normalStyle.Render(l.CreatedAt.Format("02-01-06 15:04")) + "\n")

    b.WriteString("  " + detailLabelStyle.Render("Tags:         "))
    if len(l.Tags) == 0 {
        b.WriteString(dimStyle.Render("none") + "\n")
    } else {
        b.WriteString(m.getRenderedTags(l.Tags) + "\n")
    }

    return strings.Split(b.String(), "\n")
}

func (m model) buildTagDetailLines() []string {
    tags := m.getFilteredTagsForTab()
    if len(tags) == 0 || m.tagTabCursor >= len(tags) {
        return strings.Split(dimStyle.Render("  No tag selected."), "\n")
    }

    tag := tags[m.tagTabCursor]
    b := getBuilder()
    defer putBuilder(b)

    title := fmt.Sprintf("  #%s", tag)
    count := m.countTasksWithTag(tag)
    hint := fmt.Sprintf("(%d task", count)
    if count != 1 {
        hint += "s"
    }
    hint += ")  r to rename"

    availW := m.termWidth - 8
    padW := availW - len([]rune(title)) - len([]rune(hint))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(
        tagSelectedStyle.Render(title)+
            strings.Repeat(" ", padW)+
            dimStyle.Render(hint)+"\n\n",
    )

    hasAny := false
    for i := range m.todos {
        for _, tt := range m.todos[i].Tags {
            if tt != tag {
                continue
            }
            hasAny = true
            t := &m.todos[i]
            status := "[ ]"
            if t.Status == todo.Done {
                status = "[✓]"
            }
            dueStr := ""
            if !t.DueDate.IsZero() {
                dueStr = "  due: " + t.DueDate.Format("02-01-06")
                if t.IsOverdue() {
                    dueStr += " ⚠"
                }
            }
            projStr := ""
            if t.Project != "" {
                projStr = "  [" + t.Project + "]"
            }
            line := fmt.Sprintf("  %s %s%s%s", status, truncate(t.Title, 34), dueStr, projStr)
            switch {
            case t.IsOverdue():
                b.WriteString(overdueStyle.Render(line) + "\n")
            case t.Status == todo.Done:
                b.WriteString(doneCountStyle.Render(line) + "\n")
            default:
                b.WriteString(normalStyle.Render(line) + "\n")
            }
        }
    }

    if !hasAny {
        b.WriteString(dimStyle.Render("  No tasks carry this tag.") + "\n")
    }
    return strings.Split(b.String(), "\n")
}

// ── Tabs ──────────────────────────────────────────────────────────────────────

func (m model) renderTabs() string {
    activeStyles := [5]lipgloss.Style{
        tabTasksActiveStyle,
        tabProjectsActiveStyle,
        tabTagsActiveStyle,
        tabLearningsActiveStyle,
        tabStatsActiveStyle,
    }
    names := [5]string{"1:Tasks", "2:Projects", "3:Tags", "4:Learnings", "5:Stats"}
    var parts [5]string
    for i := range names {
        if tab(i) == m.tab {
            parts[i] = activeStyles[i].Render(names[i])
        } else {
            parts[i] = tabInactiveStyle.Render(names[i])
        }
    }
    return parts[0] + " " + parts[1] + " " + parts[2] + " " + parts[3] + " " + parts[4]
}

func (m model) renderListContent() string {
    switch m.tab {
    case tabTasks:
        if m.showHistory {
            return m.renderHistoryList()
        }
        return m.renderTaskList()
    case tabProjects:
        return m.renderProjectListContent(m.allProjectsForList())
    case tabTags:
        return m.renderTagList()
    case tabLearnings:
        return m.renderLearningList()
    case tabStats:
        return m.renderStatsList()
    }
    return ""
}

