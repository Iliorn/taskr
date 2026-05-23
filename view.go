package main

import (
    "fmt"
    "math"
    "sort"
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

}// ── Detail scroll ────────────────────────────────────────────────────────────

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

// ── Tags list ─────────────────────────────────────────────────────────────────

func (m model) renderTagList() string {
    tags := m.getFilteredTagsForTab()

    if len(tags) == 0 {
        if m.tagTabSearchQuery != "" {
            return normalStyle.Render("  No tags match your filter.")
        }
        return normalStyle.Render("  No tags yet. Add tags to tasks in the detail view.")
    }

    b := getBuilder()
    defer putBuilder(b)

    barW := m.termWidth / ganttBarWidthDivisor
    if barW < minTagBarWidth {
        barW = minTagBarWidth
    }
    if barW > maxTagBarWidth {
        barW = maxTagBarWidth
    }

    gradLen := len(tagProgressGradient)
    stats := m.cache.tags
    if stats == nil {
        stats = computeTagStats(m.todos)
    }

    headerLeft := padRight("  Tag", tagLabelColWidth) + "Progress"
    padW := m.termWidth - 6 - len([]rune(headerLeft)) - barW
    if padW < 1 {
        padW = 1
    }
    b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW+barW)) + "\n")

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
    barStr.Grow(barW * 4)

    for i := startIdx; i < endIdx; i++ {
        tag := tags[i]
        s := stats[tag]
        total := s.total
        done := s.done

        pct := 0.0
        if total > 0 {
            pct = float64(done) / float64(total)
        }
        filled := int(math.Round(pct * float64(barW)))
        cur := "  "
        if i == m.tagTabCursor {
            cur = "▶ "
        }
        tagLabel := padRight(truncate("#"+tag, tagLabelColWidth-4), tagLabelColWidth-2)

        barStr.Reset()
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
                barStr.WriteString(tagProgressGradient[gradIdx].Render("█"))
            } else {
                barStr.WriteString(dimStyle.Render("░"))
            }
        }

        if m.mode == modeEditTag && m.editingTagName == tag {
            b.WriteString(tagSelectedStyle.Render(cur+tagLabel) + m.textInput.View() + "\n")
            continue
        }

        pctStr := fmt.Sprintf(" %3d%% (%d done / %d total)", int(pct*100), done, total)
        if i == m.tagTabCursor {
            b.WriteString(
                tagSelectedStyle.Render(cur+tagLabel)+
                    barStr.String()+
                    selectedStyle.Render(pctStr)+"\n",
            )
        } else {
            b.WriteString(
                tagStyle.Render(cur+tagLabel)+
                    barStr.String()+
                    normalStyle.Render(pctStr)+"\n",
            )
        }
    }
    return b.String()
}

// ── Learnings list ────────────────────────────────────────────────────────────

func (m model) renderLearningList() string {
    learnings := m.allLearnings()

    if len(learnings) == 0 {
        if m.learningSearchQuery != "" {
            return normalStyle.Render("  No learnings match your search.")
        }
        return normalStyle.Render("  No learnings yet. Add learnings from a task's detail view.")
    }

    b := getBuilder()
    defer putBuilder(b)

    availW := m.termWidth - 8
    dateW := 8
    tagsW := availW / 4
    if tagsW > 30 {
        tagsW = 30
    }
    if tagsW < 10 {
        tagsW = 10
    }
    textW := availW - dateW - tagsW - 6

    const prefix = "      "
    headerLeft := prefix + padRight("Learning", textW) + padRight("Tags", tagsW) + "Date"
    padW := m.termWidth - 6 - len([]rune(headerLeft))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")

    maxVisible := m.estimateListHeight()
    startIdx := m.listOffset
    endIdx := startIdx + maxVisible
    if endIdx > len(learnings) {
        endIdx = len(learnings)
    }
    if startIdx > len(learnings) {
        startIdx = 0
    }

    for i := startIdx; i < endIdx; i++ {
        l := learnings[i]
        cur := "  "
        if i == m.learningCursor {
            cur = "▶ "
        }
        textCol := padRight(truncate(l.Text, textW), textW)
        tagsStr := ""
        for _, tag := range l.Tags {
            tagsStr += "#" + tag + " "
        }
        tagsCol := padRight(truncate(strings.TrimSpace(tagsStr), tagsW), tagsW)
        dateCol := l.CreatedAt.Format("02-01-06")

        if i == m.learningCursor {
            b.WriteString(
                learningSelectedStyle.Render(cur+textCol)+
                    tagStyle.Render(tagsCol)+
                    learningStyle.Render(dateCol)+"\n",
            )
        } else {
            b.WriteString(
                normalStyle.Render(cur+textCol)+
                    dimStyle.Render(tagsCol)+
                    dimStyle.Render(dateCol)+"\n",
            )
        }
    }
    return b.String()
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func (m model) renderStatsList() string {
    b := getBuilder()
    defer putBuilder(b)

    now := m.frameTime
    today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
    weekAgo := today.AddDate(0, 0, -7)
    monthAgo := today.AddDate(0, -1, 0)

    var totalTasks, activeTasks, doneTasks, overdueTasks int
    var doneToday, doneThisWeek, doneThisMonth int
    var highPri, medPri, lowPri int
    var withNotes, withLearnings int
    projectCounts := make(map[string]int)

    for i := range m.todos {
        t := &m.todos[i]
        if t.ParentID != "" {
            continue
        }
        totalTasks++
        if t.Status == todo.Done {
            doneTasks++
            if !t.CompletedAt.IsZero() {
                if !t.CompletedAt.Before(today) {
                    doneToday++
                }
                if !t.CompletedAt.Before(weekAgo) {
                    doneThisWeek++
                }
                if !t.CompletedAt.Before(monthAgo) {
                    doneThisMonth++
                }
            }
        } else {
            activeTasks++
            if t.IsOverdue() {
                overdueTasks++
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
        if t.Notes != "" {
            withNotes++
        }
        if len(t.Learnings) > 0 {
            withLearnings++
        }
        if t.Project != "" {
            projectCounts[t.Project]++
        }
    }

    availW := m.termWidth - 8
    gradLen := len(statsGradient)

    b.WriteString(statsHeaderStyle.Render("  Productivity Stats") + "\n")
    b.WriteString(renderPlainDivider(availW))

    b.WriteString(statsHeaderStyle.Render("  Overview") + "\n")

    renderStat := func(label string, value int, total int, showBar bool) {
        labelStr := padRight("  "+label, statsLabelWidth)
        valStr := fmt.Sprintf("%d", value)
        if showBar && total > 0 {
            pct := float64(value) / float64(total)
            barW := statsBarWidth
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
            pctStr := fmt.Sprintf(" %3d%%", int(pct*100))
            b.WriteString(detailLabelStyle.Render(labelStr) + normalStyle.Render(padRight(valStr, statsValueWidth)) + bar.String() + dimStyle.Render(pctStr) + "\n")
        } else {
            b.WriteString(detailLabelStyle.Render(labelStr) + normalStyle.Render(valStr) + "\n")
        }
    }

    renderStat("Total tasks", totalTasks, 0, false)
    renderStat("Active", activeTasks, totalTasks, true)
    renderStat("Completed", doneTasks, totalTasks, true)
    if overdueTasks > 0 {
        labelStr := padRight("  Overdue", statsLabelWidth)
        b.WriteString(detailLabelStyle.Render(labelStr) + overdueCountStyle.Render(fmt.Sprintf("%d", overdueTasks)) + "\n")
    } else {
        renderStat("Overdue", 0, 0, false)
    }
    b.WriteString("\n")

    b.WriteString(statsHeaderStyle.Render("  Completion velocity") + "\n")
    renderStat("Today", doneToday, 0, false)
    renderStat("This week", doneThisWeek, 0, false)
    renderStat("This month", doneThisMonth, 0, false)
    if doneThisWeek > 0 {
        avg := fmt.Sprintf("%.1f tasks/day", float64(doneThisWeek)/7.0)
        b.WriteString(detailLabelStyle.Render(padRight("  Avg (7d)", statsLabelWidth)) + normalStyle.Render(avg) + "\n")
    }
    b.WriteString("\n")

    if activeTasks > 0 {
        b.WriteString(statsHeaderStyle.Render("  Active by priority") + "\n")
        renderStat("↑ High", highPri, activeTasks, true)
        renderStat("→ Medium", medPri, activeTasks, true)
        renderStat("↓ Low", lowPri, activeTasks, true)
        b.WriteString("\n")
    }

    if len(projectCounts) > 0 {
        b.WriteString(statsHeaderStyle.Render("  Projects") + "\n")
        type projEntry struct {
            name  string
            count int
        }
        entries := make([]projEntry, 0, len(projectCounts))
        for name, count := range projectCounts {
            entries = append(entries, projEntry{name, count})
        }
        sort.Slice(entries, func(i, j int) bool {
            if entries[i].count != entries[j].count {
                return entries[i].count > entries[j].count
            }
            return entries[i].name < entries[j].name
        })
        maxShow := 8
        if len(entries) < maxShow {
            maxShow = len(entries)
        }
        for _, e := range entries[:maxShow] {
            labelStr := padRight("  "+truncate(e.name, statsLabelWidth-4), statsLabelWidth)
            b.WriteString(normalStyle.Render(labelStr) + activeCountStyle.Render(fmt.Sprintf("%d tasks", e.count)) + "\n")
        }
        if len(entries) > maxShow {
            b.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more projects", len(entries)-maxShow)) + "\n")
        }
        b.WriteString("\n")
    }

    b.WriteString(statsHeaderStyle.Render("  Content") + "\n")
    renderStat("With notes", withNotes, totalTasks, false)
    renderStat("With learnings", withLearnings, totalTasks, false)
    renderStat("Total learnings", len(m.allLearnings()), 0, false)
    renderStat("Tags in use", len(m.getAllTagsSorted()), 0, false)

    return b.String()
}

// ── Task lists ────────────────────────────────────────────────────────────────

func (m model) renderTaskList() string {
    active := m.activeTodos()
    if len(active) == 0 {
        if m.searchQuery != "" {
            return normalStyle.Render("  No tasks match your search.")
        }
        if m.focusFilter {
            return normalStyle.Render("  No tasks due today or overdue. Nice!")
        }
        return normalStyle.Render("  No tasks yet. Press 'a' to add one.")
    }

    b := getBuilder()
    defer putBuilder(b)

    renderListHeader(b, m.termWidth, m.cursor, len(active), false, m.taskSort)

    overdueSet := m.cache.overdueSet

    maxVisible := m.estimateListHeight()
    startIdx := m.listOffset
    endIdx := startIdx + maxVisible
    if endIdx > len(active) {
        endIdx = len(active)
    }
    if startIdx > len(active) {
        startIdx = 0
    }

    for i := startIdx; i < endIdx; i++ {
        t := active[i]
        b.WriteString(m.renderTaskLineWithSet(t, i, m.cursor, m.pane == paneList, overdueSet))
        if len(t.SubtaskIDs) > 0 && m.expandedTasks[t.ID] {
            for j, subID := range t.SubtaskIDs {
                sub := m.findTodoByID(subID)
                if sub == nil {
                    continue
                }
                b.WriteString(m.renderSubtaskLine(sub, j, len(t.SubtaskIDs)))
            }
        }
    }
    return b.String()
}

func (m model) renderHistoryList() string {
    completed := m.completedTodos()
    if len(completed) == 0 {
        if m.searchQuery != "" {
            return normalStyle.Render("  No completed tasks match your search.")
        }
        return normalStyle.Render("  No completed tasks yet.")
    }

    b := getBuilder()
    defer putBuilder(b)

    renderListHeader(b, m.termWidth, m.cursor, len(completed), true, m.taskSort)

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
        b.WriteString(m.renderHistoryLine(completed[i], i, m.cursor, m.pane == paneList))
    }
    return b.String()
}

func (m model) renderHistoryLine(t todo.Todo, index, cursor int, active bool) string {
    titleW := titleColWidth(m.termWidth)
    cursorStr := "  "
    if index == cursor && active {
        cursorStr = "▶ "
    }
    startVal := ""
    if !t.StartDate.IsZero() {
        startVal = t.StartDate.Format("02-01-06")
    }
    dueVal := ""
    if !t.DueDate.IsZero() {
        dueVal = t.DueDate.Format("02-01-06")
    }
    completedVal := ""
    if !t.CompletedAt.IsZero() {
        completedVal = t.CompletedAt.Format("02-01-06")
    }
    titleCol := padRight(truncate(t.Title, titleW), titleW)
    startCol := padRight(startVal, 12)
    dueCol := padRight(dueVal, 12)
    completedCol := padRight(completedVal, 12)
    tagsPart := m.getRenderedTags(t.Tags)

    if index == cursor && active {
        return selectedStyle.Render(cursorStr+"[") +
            checkDoneStyle.Render("✓") +
            selectedStyle.Render("] "+titleCol+startCol+dueCol+completedCol) +
            " " + tagsPart + "\n"
    }
    return normalStyle.Render(cursorStr+"[") +
        checkDoneStyle.Render("✓") +
        normalStyle.Render("] "+titleCol+startCol+dueCol+completedCol) +
        " " + tagsPart + "\n"
}

func (m model) renderSubtaskLine(sub *todo.Todo, index, total int) string {
    connector := "├"
    if index == total-1 {
        connector = "└"
    }
    titleW := titleColWidth(m.termWidth) - 4
    if titleW < 10 {
        titleW = 10
    }
    if sub.Status == todo.Done {
        return dimStyle.Render("     "+connector+" [") + checkDoneStyle.Render("✓") + dimStyle.Render("] "+truncate(sub.Title, titleW)) + "\n"
    }
    return dimStyle.Render("     "+connector+" [ ] "+truncate(sub.Title, titleW)) + "\n"
}

func (m model) renderTaskLineWithSet(t todo.Todo, index, cursor int, active bool, overdueSet map[string]bool) string {
    titleW := titleColWidth(m.termWidth)
    cursorStr := "  "
    if index == cursor && active {
        cursorStr = "▶ "
    }
    checkbox := "[ ]"
    if t.Status == todo.Done {
        checkbox = "[✓]"
    }
    foldIcon := " "
    if len(t.SubtaskIDs) > 0 {
        if m.expandedTasks[t.ID] {
            foldIcon = "▾"
        } else {
            foldIcon = "▸"
        }
    }
    title := t.Title
    hasOverdueDep := t.HasOverdueDependencyFast(overdueSet)
    if hasOverdueDep {
        title += " !"
    }
    if t.Notes != "" {
        title += " ¶"
    }
    startVal := ""
    if !t.StartDate.IsZero() {
        startVal = t.StartDate.Format("02-01-06")
    }
    dueVal := ""
    if !t.DueDate.IsZero() {
        dueVal = t.DueDate.Format("02-01-06")
    }
    titleCol := padRight(truncate(title, titleW-1), titleW-1)
    startCol := padRight(startVal, 12)
    dueCol := padRight(dueVal, 12)
    prioCol := padRight(t.Priority.Icon()+" "+t.Priority.String(), 12)
    tagsPart := m.getRenderedTags(t.Tags)
    line := cursorStr + checkbox + foldIcon + titleCol + startCol + dueCol + prioCol
    switch {
    case t.IsOverdue():
        return overdueStyle.Render(line) + " " + tagsPart + "\n"
    case hasOverdueDep:
        return depOverdueStyle.Render(line) + " " + tagsPart + "\n"
    case index == cursor && active:
        return selectedStyle.Render(line) + " " + tagsPart + "\n"
    default:
        return normalStyle.Render(line) + " " + tagsPart + "\n"
    }
}

func (m model) renderTaskLine(t todo.Todo, index, cursor int, active bool) string {
    return m.renderTaskLineWithSet(t, index, cursor, active, m.cache.overdueSet)
}

// ── Projects ──────────────────────────────────────────────────────────────────

func (m model) renderProjectListContent(projects []string) string {
    if len(projects) == 0 {
        if m.searchQuery != "" {
            return normalStyle.Render("  No projects match your search.")
        }
        return normalStyle.Render("  No projects yet. Add a project to a task first.")
    }

    b := getBuilder()
    defer putBuilder(b)

    w := m.termWidth - 8
    projW := m.termWidth * projectColWidthPct / 100
    if projW < minProjColWidth {
        projW = minProjColWidth
    }
    if projW > maxProjColWidth {
        projW = maxProjColWidth
    }

    const prefix = "      "
    headerLeft := prefix + padRight("Project", projW) +
        padRight("Active", projCountColWidth) +
        padRight("Done", projDoneColWidth) + "Overdue"
    padW := w - len([]rune(headerLeft))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)) + "\n")
    b.WriteString(renderPlainDivider(w))

    for i, p := range projects {
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
            b.WriteString(normalStyle.Render(cursorStr+"• ") + m.textInput.View() + "\n")
            continue
        }
        nameCol := padRight(truncate(p, projW-2), projW)
        activeStr := padRight(fmt.Sprintf("%d active", activeCnt), projCountColWidth)
        doneStr := padRight(fmt.Sprintf("%d done", doneCnt), projDoneColWidth)
        overdueStr := "─"
        if overdueCnt > 0 {
            overdueStr = fmt.Sprintf("%d overdue", overdueCnt)
        }
        switch {
        case i == m.projectCursor:
            line := selectedStyle.Render(cursorStr + "• " + nameCol + activeStr + doneStr)
            if overdueCnt > 0 {
                b.WriteString(line + overdueStyle.Render(overdueStr) + "\n")
            } else {
                b.WriteString(line + selectedStyle.Render(overdueStr) + "\n")
            }
        case activeCnt == 0:
            b.WriteString(doneCountStyle.Render(cursorStr+"• "+nameCol+activeStr+doneStr+overdueStr) + "\n")
        default:
            ovdRendered := dimStyle.Render(overdueStr)
            if overdueCnt > 0 {
                ovdRendered = overdueCountStyle.Render(overdueStr)
            }
            b.WriteString(
                normalStyle.Render(cursorStr+"• "+nameCol)+
                    activeCountStyle.Render(activeStr)+
                    doneCountStyle.Render(doneStr)+
                    ovdRendered+"\n")
        }
    }
    return b.String()
}

// ── Detail pages ──────────────────────────────────────────────────────────────

func (m model) renderDetailPage1(t *todo.Todo) string {
    b := getBuilder()
    defer putBuilder(b)

    availableW := m.termWidth - 8
    isDetailFocused := m.pane == paneDetail && m.detail.page == 0

    renderField := func(label, value string, field detailField) string {
        cur := "  "
        isCurrent := isDetailFocused && m.detail.field == field
        if isCurrent {
            cur = "▶ "
        }
        paddedLabel := detailLabelStyle.Render(padRight(label+":", detailLabelColWidth))
        var v string
        if isCurrent {
            v = detailSelectedStyle.Render(value)
        } else {
            v = detailValueStyle.Render(value)
        }
        return cur + paddedLabel + v
    }

    indicator := "[1/3]"
    titleText := truncate(t.Title, availableW-len(indicator)-2)
    padW := availableW - len([]rune(titleText)) - len([]rune(indicator))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(detailTitleStyle.Render(titleText) +
        strings.Repeat(" ", padW) +
        pageIndicatorStyle.Render(indicator) + "\n\n")

    startVal := "not set"
    if !t.StartDate.IsZero() {
        startVal = t.StartDate.Format("02-01-06")
    }
    b.WriteString(renderField("Start date", startVal, fieldStartDate) + "\n")

    dueVal := "not set"
    if !t.DueDate.IsZero() {
        dueVal = t.DueDate.Format("02-01-06")
        if t.IsOverdue() {
            dueVal += " ⚠ overdue"
        }
    }
    b.WriteString(renderField("Due date", dueVal, fieldDueDate) + "\n")
    b.WriteString(renderField("Priority", t.Priority.Icon()+" "+t.Priority.String(), fieldPriority) + "\n")

    projectVal := "not set"
    if t.Project != "" {
        projectVal = t.Project
    }
    b.WriteString(renderField("Project", projectVal, fieldProject) + "\n")

    notesVal := "none (press enter or 'n' to edit)"
    if t.Notes != "" {
        lines := strings.SplitN(t.Notes, "\n", 2)
        preview := truncate(lines[0], availableW-detailLabelColWidth-6)
        if len(lines) > 1 {
            preview += " …"
        }
        notesVal = preview
    }
    b.WriteString(renderField("Notes", notesVal, fieldNotes) + "\n")

    b.WriteString("  " + detailLabelStyle.Render(padRight("Created:", detailLabelColWidth)) +
        detailValueStyle.Render(t.CreatedAt.Format("02-01-06 15:04")) + "\n")
    b.WriteString("  " + detailLabelStyle.Render(padRight("Modified:", detailLabelColWidth)) +
        detailValueStyle.Render(t.ModifiedAt.Format("02-01-06 15:04")) + "\n")

    if t.Status == todo.Done && !t.CompletedAt.IsZero() {
        b.WriteString("  " + detailLabelStyle.Render(padRight("Completed on:", detailLabelColWidth)) +
            checkDoneStyle.Render(t.CompletedAt.Format("02-01-06 15:04")) + "\n")
    }
    b.WriteString("\n")

    tagCur := "  "
    if isDetailFocused && m.detail.field == fieldTags {
        tagCur = "▶ "
    }
    b.WriteString(tagCur + detailLabelStyle.Render("Tags:") + "\n")
    if len(t.Tags) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No tags. Press 'a' to add one.") + "\n")
    } else {
        for i, tag := range t.Tags {
            pfx := "  "
            if isDetailFocused && m.detail.field == fieldTags && i == m.detail.tagCursor {
                pfx = "▶ "
                b.WriteString(detailSelectedStyle.Render(pfx) + tagStyle.Render("⟨#"+tag+"⟩") + "\n")
            } else {
                b.WriteString(dimStyle.Render(pfx) + tagStyle.Render("⟨#"+tag+"⟩") + "\n")
            }
        }
    }

    return b.String()
}

func (m model) renderDetailPage2(t *todo.Todo) string {
    b := getBuilder()
    defer putBuilder(b)

    availableW := m.termWidth - 8
    isDetailFocused := m.pane == paneDetail && m.detail.page == 1

    indicator := "[2/3]"
    titleText := truncate(t.Title, availableW-len(indicator)-2)
    padW := availableW - len([]rune(titleText)) - len([]rune(indicator))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(detailTitleStyle.Render(titleText) +
        strings.Repeat(" ", padW) +
        pageIndicatorStyle.Render(indicator) + "\n\n")

    subtaskCur := "  "
    if isDetailFocused && m.detail.field == fieldSubtasks {
        subtaskCur = "▶ "
    }
    b.WriteString(subtaskCur + detailLabelStyle.Render("Subtasks:") + "\n")
    if len(t.SubtaskIDs) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No subtasks. Press 'a' to add one.") + "\n")
    } else {
        for i, subID := range t.SubtaskIDs {
            sub := m.findTodoByID(subID)
            pfx := "  "
            isSubSelected := isDetailFocused && m.detail.field == fieldSubtasks && i == m.detail.subtaskCursor
            if isSubSelected {
                pfx = "▶ "
            }
            if sub == nil {
                b.WriteString(dimStyle.Render(fmt.Sprintf("%s[?] unknown subtask", pfx)) + "\n")
                continue
            }
            if sub.Status == todo.Done {
                if isSubSelected {
                    b.WriteString(detailSelectedStyle.Render(pfx+"[") + checkDoneStyle.Render("✓") + detailSelectedStyle.Render("] "+truncate(sub.Title, availableW-8)) + "\n")
                } else {
                    b.WriteString(dimStyle.Render(pfx+"[") + checkDoneStyle.Render("✓") + dimStyle.Render("] "+truncate(sub.Title, availableW-8)) + "\n")
                }
            } else {
                line := fmt.Sprintf("%s[ ] %s", pfx, truncate(sub.Title, availableW-8))
                if isSubSelected {
                    b.WriteString(detailSelectedStyle.Render(line) + "\n")
                } else {
                    b.WriteString(detailValueStyle.Render(line) + "\n")
                }
            }
        }
    }
    b.WriteString("\n")

    depCur := "  "
    if isDetailFocused && m.detail.field == fieldDependencies {
        depCur = "▶ "
    }
    b.WriteString(depCur + detailLabelStyle.Render("Dependencies:") + "\n")
    if len(t.Dependencies) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No dependencies. Press 'a' to add one.") + "\n")
    } else {
        for i, depID := range t.Dependencies {
            dep := m.findTodoByID(depID)
            pfx := "  "
            isDepSelected := isDetailFocused && m.detail.field == fieldDependencies && i == m.detail.depCursor
            if isDepSelected {
                pfx = "▶ "
            }
            if dep == nil {
                b.WriteString(dimStyle.Render(fmt.Sprintf("%s[?] unknown task", pfx)) + "\n")
                continue
            }
            status := "[ ]"
            if dep.Status == todo.Done {
                status = "[✓]"
            }
            warn := ""
            if dep.IsOverdue() {
                warn = " !"
            }
            line := fmt.Sprintf("%s%s %s%s", pfx, status, dep.Title, warn)
            switch {
            case dep.IsOverdue():
                b.WriteString(overdueStyle.Render(line) + "\n")
            case isDepSelected:
                b.WriteString(detailSelectedStyle.Render(line) + "\n")
            default:
                b.WriteString(detailValueStyle.Render(line) + "\n")
            }
        }
    }
    b.WriteString("\n")

    learningCur := "  "
    if isDetailFocused && m.detail.field == fieldLearnings {
        learningCur = "▶ "
    }
    b.WriteString(learningCur + detailLabelStyle.Render("Learnings:") + "\n")
    if len(t.Learnings) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No learnings yet. Press 'a' to add one.") + "\n")
    } else {
        for i, l := range t.Learnings {
            pfx := "  "
            isLearningSelected := isDetailFocused && m.detail.field == fieldLearnings && i == m.detail.learningCursor
            if isLearningSelected {
                pfx = "▶ "
            }
            line := pfx + truncate(l.Text, availableW-4)
            if isLearningSelected {
                b.WriteString(learningSelectedStyle.Render(line) + "\n")
            } else {
                b.WriteString(learningStyle.Render(line) + "\n")
            }
        }
    }

    return b.String()
}

func (m model) renderDetailPage3(t *todo.Todo) string {
    b := getBuilder()
    defer putBuilder(b)

    availableW := m.termWidth - 8
    innerW := m.termWidth - 10
    if innerW < minInnerWidth {
        innerW = minInnerWidth
    }
    indicator := "[3/3]"
    titleText := truncate(t.Title, availableW-len(indicator)-2)
    padW := availableW - len([]rune(titleText)) - len([]rune(indicator))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(detailTitleStyle.Render(titleText) +
        strings.Repeat(" ", padW) +
        pageIndicatorStyle.Render(indicator) + "\n\n")
    isDetailFocused := m.pane == paneDetail && m.detail.page == 2
    commentCur := "  "
    if isDetailFocused {
        commentCur = "▶ "
    }
    b.WriteString(commentCur + detailLabelStyle.Render("Comments:") + "\n")
    if len(t.Comments) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No comments yet. Press 'a' to add one.") + "\n")
    } else {
        available := innerW - commentPrefixLen
        if available < 10 {
            available = 10
        }
        for i, c := range t.Comments {
            isSelected := isDetailFocused && i == m.detail.commentCursor
            pfx := "  "
            if isSelected {
                pfx = "▶ "
            }
            header := fmt.Sprintf("%s[%s] ", pfx, c.CreatedAt.Format("02-01-06 15:04"))
            wrapped := wrapText(c.Text, available)
            indent := strings.Repeat(" ", len([]rune(header)))
            for j, line := range wrapped {
                var fullLine string
                if j == 0 {
                    fullLine = header + line
                } else {
                    fullLine = indent + line
                }
                if isSelected {
                    b.WriteString(detailSelectedStyle.Render(fullLine) + "\n")
                } else {
                    b.WriteString(detailValueStyle.Render(fullLine) + "\n")
                }
            }
        }
    }
    return b.String()
}

// ── Gantt ─────────────────────────────────────────────────────────────────────

func (m model) renderGantt(tasks []todo.Todo) string {
    if len(tasks) == 0 {
        return dimStyle.Render("  No tasks in this project.")
    }
    today := m.frameTime
    var minDate, maxDate time.Time
    for _, t := range tasks {
        if !t.StartDate.IsZero() && (minDate.IsZero() || t.StartDate.Before(minDate)) {
            minDate = t.StartDate
        }
        if !t.DueDate.IsZero() && (maxDate.IsZero() || t.DueDate.After(maxDate)) {
            maxDate = t.DueDate
        }
    }
    if minDate.IsZero() {
        minDate = today.AddDate(0, 0, -7)
    }
    if maxDate.IsZero() {
        maxDate = today.AddDate(0, 1, 0)
    }
    if !maxDate.After(minDate) {
        maxDate = minDate.AddDate(0, 0, 14)
    }

    labelW := m.termWidth / ganttLabelWidthDivisor
    if labelW < minGanttLabelWidth {
        labelW = minGanttLabelWidth
    }
    if labelW > maxGanttLabelWidth {
        labelW = maxGanttLabelWidth
    }

    chartW := m.termWidth - labelW - ganttSuffixWidth - ganttChartPadding
    if chartW < minChartWidth {
        chartW = minChartWidth
    }

    totalDays := maxDate.Sub(minDate).Hours() / 24
    if totalDays < 1 {
        totalDays = 1
    }
    todayPos := int(math.Round(today.Sub(minDate).Hours() / 24 * float64(chartW) / totalDays))
    if todayPos < 0 || todayPos >= chartW {
        todayPos = -1
    }

    b := getBuilder()
    defer putBuilder(b)

    leftDate := minDate.Format("02-01")
    rightDate := maxDate.Format("02-01")
    innerSpaces := chartW - len(leftDate) - len(rightDate)
    if innerSpaces < 1 {
        innerSpaces = 1
    }
    timelineHeader := leftDate + strings.Repeat(" ", innerSpaces) + rightDate
    headerLabel := padRight("  Timeline", labelW)
    b.WriteString(headerStyle.Render(headerLabel+timelineHeader) + "\n")

    todayLabel := "today:" + today.Format("02-01")
    divider := make([]rune, chartW)
    for i := range divider {
        divider[i] = '─'
    }
    if todayPos >= 0 {
        insertPos := todayPos - len([]rune(todayLabel))/2
        if insertPos < 0 {
            insertPos = 0
        }
        if insertPos+len([]rune(todayLabel)) > chartW {
            insertPos = chartW - len([]rune(todayLabel))
        }
        for i, ch := range []rune(todayLabel) {
            divider[insertPos+i] = ch
        }
    }
    b.WriteString(dimStyle.Render("  "+strings.Repeat("─", labelW-2)) +
        ganttTodayStyle.Render(string(divider)) + "\n")

    gradLen := len(ganttGradient)
    ovrdLen := len(ganttOverdueGradient)

    // Use pooled buffers
    bufs := getGanttBuffers(chartW)
    defer putGanttBuffers(bufs)
    barRunes := bufs.bar[:chartW]
    barColors := bufs.color[:chartW]

    for i, t := range tasks {
        isSelected := i == m.cursor && m.pane == paneList && m.projectTaskMode
        checkbox := "[ ]"
        if t.Status == todo.Done {
            checkbox = "[✓]"
        }
        titleTrunc := labelW - 6
        if titleTrunc < 5 {
            titleTrunc = 5
        }
        label := checkbox + " " + padRight(truncate(t.Title, titleTrunc), titleTrunc) + " |"

        for j := range barRunes {
            barRunes[j] = ' '
            barColors[j] = -1
        }

        hasDates := !t.StartDate.IsZero() && !t.DueDate.IsZero()
        if hasDates {
            startPos := int(math.Round(t.StartDate.Sub(minDate).Hours() / 24 * float64(chartW) / totalDays))
            endPos := int(math.Round(t.DueDate.Sub(minDate).Hours() / 24 * float64(chartW) / totalDays))
            if startPos < 0 {
                startPos = 0
            }
            if endPos > chartW {
                endPos = chartW
            }
            barLen := endPos - startPos
            if barLen < 1 {
                barLen = 1
            }
            isOverdue := t.IsOverdue()
            isDone := t.Status == todo.Done
            for j := startPos; j < endPos && j < chartW; j++ {
                barRunes[j] = '█'
                var pos float64
                if barLen > 1 {
                    pos = float64(j-startPos) / float64(barLen-1)
                }
                gradIdx := int(pos * float64(gradLen-1))
                if gradIdx >= gradLen {
                    gradIdx = gradLen - 1
                }
                switch {
                case isDone:
                    barColors[j] = 99
                case isOverdue:
                    idx := int(pos * float64(ovrdLen-1))
                    if idx >= ovrdLen {
                        idx = ovrdLen - 1
                    }
                    barColors[j] = 200 + idx
                default:
                    barColors[j] = gradIdx
                }
            }
        }
        if todayPos >= 0 && todayPos < chartW {
            barRunes[todayPos] = '│'
            barColors[todayPos] = -2
        }
        datesSuffix := "|"
        if hasDates {
            datesSuffix = fmt.Sprintf("| %s→%s", t.StartDate.Format("02-01"), t.DueDate.Format("02-01"))
        }

        renderBar := func() {
            j := 0
            for j < chartW {
                colorIdx := barColors[j]
                start := j
                for j < chartW && barColors[j] == colorIdx {
                    j++
                }
                group := string(barRunes[start:j])
                switch {
                case colorIdx == -1:
                    b.WriteString(group)
                case colorIdx == -2:
                    b.WriteString(ganttTodayStyle.Render(group))
                case colorIdx == 99:
                    b.WriteString(ganttDoneStyle.Render(group))
                case colorIdx >= 200:
                    b.WriteString(ganttOverdueGradient[colorIdx-200].Render(group))
                default:
                    b.WriteString(ganttGradient[colorIdx].Render(group))
                }
            }
        }
        if isSelected {
            b.WriteString(selectedStyle.Render(label))
            renderBar()
            b.WriteString(selectedStyle.Render(datesSuffix) + "\n")
        } else {
            b.WriteString(label)
            renderBar()
            b.WriteString(datesSuffix + "\n")
        }
    }
    return b.String()
}
