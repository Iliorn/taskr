package main

import (
    "fmt"
    "math"
    "strings"
    "time"

    "github.com/charmbracelet/lipgloss"
    "taskr/todo"
)

// ── Top-level View ────────────────────────────────────────────────────────────

func (m model) View() string {
    w := m.termWidth - 4

    // ── HEADER ───────────────────────────────────────────────────────────
    var headerLines []string
    shortcutHint := helpStyle.Render("? shortcuts")
    tabsStr := titleStyle.Render("taskr") + "  " + m.renderTabs()
    padW := m.termWidth - len([]rune(tabsStr)) - len([]rune("? shortcuts")) - 4
    if padW < 1 {
        padW = 1
    }
    headerLines = append(headerLines, tabsStr+strings.Repeat(" ", padW)+shortcutHint)
    headerLines = append(headerLines, "")
    if m.err != "" {
        headerLines = append(headerLines, confirmStyle.Render(m.err))
    }
    if m.searchQuery != "" {
        headerLines = append(headerLines, searchStyle.Render("/ "+m.searchQuery))
    }
    if m.tab == tabTags && m.tagTabSearchQuery != "" {
        headerLines = append(headerLines, searchStyle.Render("/ "+m.tagTabSearchQuery))
    }
    if m.tab == tabLearnings && m.learningSearchQuery != "" {
        headerLines = append(headerLines, searchStyle.Render("/ "+m.learningSearchQuery))
    }

    // ── FOOTER ───────────────────────────────────────────────────────────
    var footerLines []string
    switch m.mode {
    case modeInput, modeEditComment, modeEditTag, modeEditTitle, modeAddLearning, modeEditLearning:
        footerLines = append(footerLines, inputStyle.Width(w).Render(m.textInput.View()))
    case modeSearch:
        if m.tab == tabLearnings {
            footerLines = append(footerLines, searchStyle.Width(w).Render(m.learningSearchInput.View()))
        } else {
            footerLines = append(footerLines, searchStyle.Width(w).Render(m.searchInput.View()))
        }
    case modeSearchTagTab:
        footerLines = append(footerLines, searchStyle.Width(w).Render(m.tagTabSearchInput.View()))
    case modeSearchDep:
        footerLines = append(footerLines, searchStyle.Width(w).Render(m.depSearchInput.View()))
        for i, r := range m.depSearchResults() {
            if i >= maxDepSearchResults {
                break
            }
            if i == m.searchCursor {
                footerLines = append(footerLines, selectedStyle.Render("  → "+r.Title))
            } else {
                footerLines = append(footerLines, normalStyle.Render("    "+r.Title))
            }
        }
    case modeSearchTag:
        footerLines = append(footerLines, searchStyle.Width(w).Render(m.tagSearchInput.View()))
        results := m.tagSearchResults()
        for i, r := range results {
            if i >= maxTagSearchResults {
                break
            }
            if i == m.searchCursor {
                footerLines = append(footerLines, selectedStyle.Render("  → #"+r))
            } else {
                footerLines = append(footerLines, normalStyle.Render("    #"+r))
            }
        }
        if len(results) == 0 && m.tagSearchQuery != "" {
            footerLines = append(footerLines, dimStyle.Render("  → create new tag: ")+tagStyle.Render(m.tagSearchQuery))
        }
    case modeSearchProject:
        footerLines = append(footerLines, searchStyle.Width(w).Render(m.projSearchInput.View()))
        results := m.projSearchResults()
        for i, r := range results {
            if i >= maxProjSearchResults {
                break
            }
            if i == m.searchCursor {
                footerLines = append(footerLines, selectedStyle.Render("  → "+r))
            } else {
                footerLines = append(footerLines, normalStyle.Render("    "+r))
            }
        }
        if len(results) == 0 && m.projSearchQuery != "" {
            footerLines = append(footerLines, dimStyle.Render("  → create new project: ")+selectedStyle.Render(m.projSearchQuery))
        }
    case modeConfirmDelete, modeConfirmDeleteComment,
        modeConfirmDeleteDep, modeConfirmDeleteTag,
        modeConfirmDeleteTagGlobal, modeConfirmDeleteProject,
        modeConfirmDeleteLearning:
        footerLines = append(footerLines, confirmStyle.Render(m.confirmMsg))
    case modeHelp:
        footerLines = append(footerLines, helpStyle.Render("? or esc  close help"))
    default:
        footerLines = append(footerLines, "")
    }

    // ── DETAIL ───────────────────────────────────────────────────────────
    detailContent := m.buildDetailLines()
    maxDetailContent := m.termHeight * detailMaxHeightPct / 100
    if len(detailContent) > maxDetailContent {
        detailContent = detailContent[:maxDetailContent]
    }
    detailRendered := strings.Split(
        detailPanelStyle.Width(w).Render(strings.Join(detailContent, "\n")),
        "\n",
    )
    for len(detailRendered) > 0 && strings.TrimSpace(detailRendered[len(detailRendered)-1]) == "" {
        detailRendered = detailRendered[:len(detailRendered)-1]
    }

    // ── LIST ─────────────────────────────────────────────────────────────
    listH := m.termHeight - len(headerLines) - footerHeight - len(detailRendered)
    if listH < minListHeight {
        listH = minListHeight
    }

    var listRendered []string
    if m.tab == tabProjects {
        projects := m.allProjectsForList()
        if len(projects) == 0 {
            empty := normalStyle.Render("  No projects yet. Add a project to a task first.")
            if m.searchQuery != "" {
                empty = normalStyle.Render("  No projects match your search.")
            }
            emptyLines := strings.Split(empty, "\n")
            for len(emptyLines) < listH {
                emptyLines = append(emptyLines, "")
            }
            if len(emptyLines) > listH {
                emptyLines = emptyLines[:listH]
            }
            listRendered = strings.Split(
                listPanelStyle.Width(w).Render(strings.Join(emptyLines, "\n")),
                "\n",
            )
        } else {
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
            projRendered := strings.Split(
                listPanelStyle.Width(w).Render(strings.Join(projLines, "\n")),
                "\n",
            )
            ganttH := listH - len(projRendered)
            if ganttH < minListPanelLines {
                ganttH = minListPanelLines
            }
            var ganttLines []string
            if m.projectCursor < len(projects) {
                tasks := getTasksForProject(m.todos, projects[m.projectCursor])
                ganttLines = strings.Split(m.renderGantt(tasks), "\n")
                ganttEnd := len(ganttLines)
                for ganttEnd > 0 && strings.TrimSpace(ganttLines[ganttEnd-1]) == "" {
                    ganttEnd--
                }
                ganttLines = ganttLines[:ganttEnd]
            }
            if len(ganttLines) > ganttH {
                ganttLines = ganttLines[:ganttH]
            }
            for len(ganttLines) < ganttH {
                ganttLines = append(ganttLines, "")
            }
            listRendered = append(projRendered, ganttLines...)
        }
    } else {
        rawList := m.buildListLines()
        if m.listOffset > 0 && m.listOffset < len(rawList) {
            rawList = rawList[m.listOffset:]
        }
        for len(rawList) < listH {
            rawList = append(rawList, "")
        }
        if len(rawList) > listH {
            rawList = rawList[:listH]
        }
        listRendered = strings.Split(
            listPanelStyle.Width(w).Render(strings.Join(rawList, "\n")),
            "\n",
        )
    }
    for len(listRendered) > 0 && strings.TrimSpace(listRendered[len(listRendered)-1]) == "" {
        listRendered = listRendered[:len(listRendered)-1]
    }

    // ── HELP OVERLAY ──────────────────────────────────────────────────────
    if m.mode == modeHelp {
        listRendered = m.renderHelpOverlay(listRendered)
    }

    // ── ASSEMBLE ─────────────────────────────────────────────────────────
    all := make([]string, 0, m.termHeight)
    all = append(all, headerLines...)
    all = append(all, listRendered...)
    all = append(all, detailRendered...)
    all = append(all, footerLines[:footerHeight]...)

    target := m.termHeight - 1
    diff := len(all) - target

    if diff > 0 {
        listStart := len(headerLines)
        listEnd := listStart + len(listRendered)
        canRemove := listEnd - listStart - 1
        remove := diff
        if remove > canRemove {
            remove = canRemove
        }
        if remove > 0 {
            newListEnd := listEnd - remove
            all = append(all[:newListEnd], all[listEnd:]...)
        }
    } else if diff < 0 {
        listEnd := len(headerLines) + len(listRendered)
        abs := -diff
        padding := make([]string, abs)
        newAll := make([]string, 0, target+1)
        newAll = append(newAll, all[:listEnd]...)
        newAll = append(newAll, padding...)
        newAll = append(newAll, all[listEnd:]...)
        all = newAll
    }

    for len(all) > target {
        listStart := len(headerLines)
        listEnd := listStart + len(listRendered)
        if listEnd > listStart {
            all = append(all[:listEnd-1], all[listEnd:]...)
            listRendered = listRendered[:len(listRendered)-1]
        } else {
            break
        }
    }
    for len(all) < target {
        listEnd := len(headerLines) + len(listRendered)
        newAll := make([]string, 0, target+1)
        newAll = append(newAll, all[:listEnd]...)
        newAll = append(newAll, "")
        newAll = append(newAll, all[listEnd:]...)
        all = newAll
        listRendered = append(listRendered, "")
    }

    return strings.Join(all, "\n")
}

// ── Help overlay ──────────────────────────────────────────────────────────────

func (m model) renderHelpOverlay(listLines []string) []string {
    sections := []struct {
        title string
        keys  [][2]string
    }{
        {"Navigation", [][2]string{
            {"↑/↓  or  j/k", "navigate list"},
            {"enter", "open details"},
            {"esc", "go back"},
            {"tab  or  1-4", "switch tabs"},
            {"?", "close help"},
        }},
        {"Tasks", [][2]string{
            {"a", "add task"},
            {"r", "rename task"},
            {"d", "toggle done"},
            {"x", "delete"},
            {"h", "toggle history"},
            {"s", "cycle sort order"},
            {"/", "search"},
        }},
        {"Detail view", [][2]string{
            {"←/→", "switch pages"},
            {"enter", "edit field"},
            {"a", "add tag / dep / comment"},
            {"x", "remove field"},
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
    }

    var lines []string
    lines = append(lines, titleStyle.Render("  Keyboard shortcuts"))
    lines = append(lines, "")
    for _, section := range sections {
        lines = append(lines, detailLabelStyle.Render("  "+section.title))
        for _, kv := range section.keys {
            key := padRight(kv[0], 24)
            lines = append(lines,
                helpStyle.Render("  ")+
                    selectedStyle.Render(key)+
                    normalStyle.Render(kv[1]))
        }
        lines = append(lines, "")
    }

    overlayW := m.termWidth * overlayWidthPct / 100
    if overlayW < minOverlayWidth {
        overlayW = minOverlayWidth
    }
    if overlayW > m.termWidth-8 {
        overlayW = m.termWidth - 8
    }

    rendered := strings.Split(
        inputStyle.Width(overlayW).Render(strings.Join(lines, "\n")),
        "\n",
    )

    if len(listLines) == 0 {
        return rendered
    }
    startRow := (len(listLines) - len(rendered)) / 2
    if startRow < 0 {
        startRow = 0
    }
    startCol := (m.termWidth - overlayW) / 2
    if startCol < 0 {
        startCol = 0
    }
    prefix := strings.Repeat(" ", startCol)

    result := make([]string, len(listLines))
    copy(result, listLines)
    for i, overlayLine := range rendered {
        row := startRow + i
        if row >= len(result) {
            break
        }
        result[row] = prefix + overlayLine
    }
    return result
}

// ── Build helpers ─────────────────────────────────────────────────────────────

func (m model) buildListLines() []string {
    return strings.Split(m.renderListContent(), "\n")
}

func (m model) buildDetailLines() []string {
    switch {
    case m.tab == tabTags:
        return m.buildTagDetailLines()
    case m.tab == tabLearnings:
        return m.buildLearningDetailLines()
    case m.currentTodo() != nil:
        t := m.currentTodo()
        var content string
        if m.detailPage == 0 {
            content = m.renderDetailPage1(t)
        } else {
            content = m.renderDetailPage2(t)
        }
        return strings.Split(content, "\n")
    default:
        return strings.Split(dimStyle.Render("  No task selected."), "\n")
    }
}

func (m model) buildLearningDetailLines() []string {
    learnings := m.allLearnings()
    if len(learnings) == 0 || m.learningCursor >= len(learnings) {
        return strings.Split(dimStyle.Render("  No learning selected."), "\n")
    }

    l := learnings[m.learningCursor]
    var b strings.Builder
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
        b.WriteString(renderTagsPart(l.Tags) + "\n")
    }

    return strings.Split(b.String(), "\n")
}

func (m model) buildTagDetailLines() []string {
    tags := m.getFilteredTagsForTab()
    if len(tags) == 0 || m.tagTabCursor >= len(tags) {
        return strings.Split(dimStyle.Render("  No tag selected."), "\n")
    }

    tag := tags[m.tagTabCursor]
    var b strings.Builder

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
    for _, t := range m.todos {
        for _, tt := range t.Tags {
            if tt != tag {
                continue
            }
            hasAny = true
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
    activeStyles := [4]lipgloss.Style{
        tabTasksActiveStyle,
        tabProjectsActiveStyle,
        tabTagsActiveStyle,
        tabLearningsActiveStyle,
    }
    names := [4]string{"1:Tasks", "2:Projects", "3:Tags", "4:Learnings"}
    var parts [4]string
    for i := range names {
        if tab(i) == m.tab {
            parts[i] = activeStyles[i].Render(names[i])
        } else {
            parts[i] = tabInactiveStyle.Render(names[i])
        }
    }
    return parts[0] + " " + parts[1] + " " + parts[2] + " " + parts[3]
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
    }
    return ""
}

// ── Learnings list ────────────────────────────────────────────────────────────

func (m model) renderLearningList() string {
    var b strings.Builder
    learnings := m.allLearnings()
    if len(learnings) == 0 {
        if m.learningSearchQuery != "" {
            return normalStyle.Render("  No learnings match your search.")
        }
        return normalStyle.Render("  No learnings yet. Add learnings from a task's detail view.")
    }

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

    sortLabel := "date"
    if m.learningSort == learningSortAlpha {
        sortLabel = "alpha"
    }
    counter := fmt.Sprintf("%d/%d  sort:%s", m.learningCursor+1, len(learnings), sortLabel)
    headerLeft := "      " + padRight("Learning", textW) + padRight("Tags", tagsW) + "Date"
    padW := m.termWidth - 6 - len([]rune(headerLeft)) - len([]rune(counter))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(
        headerStyle.Render(headerLeft+strings.Repeat(" ", padW))+
            learningSortIndicatorStyle.Render(counter)+"\n",
    )
    b.WriteString(dimStyle.Render("  "+strings.Repeat("─", availW)) + "\n")

    for i, l := range learnings {
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
// ── Projects ──────────────────────────────────────────────────────────────────

func (m model) renderProjectListContent(projects []string) string {
    if len(projects) == 0 {
        if m.searchQuery != "" {
            return normalStyle.Render("  No projects match your search.")
        }
        return normalStyle.Render("  No projects yet. Add a project to a task first.")
    }

    var b strings.Builder
    w := m.termWidth - 8
    counter := fmt.Sprintf("%d/%d", m.projectCursor+1, len(projects))

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
    padW := w - len([]rune(headerLeft)) - len([]rune(counter))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(headerStyle.Render(headerLeft+strings.Repeat(" ", padW)+counter) + "\n")
    b.WriteString(dimStyle.Render("  "+strings.Repeat("─", w-4)) + "\n")

    for i, p := range projects {
        tasks := getTasksForProject(m.todos, p)
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

// ── Gantt ─────────────────────────────────────────────────────────────────────

func (m model) renderGantt(tasks []todo.Todo) string {
    if len(tasks) == 0 {
        return dimStyle.Render("  No tasks in this project.")
    }
    today := time.Now()
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

    var b strings.Builder

    leftDate := minDate.Format("02-01")
    rightDate := maxDate.Format("02-01")
    innerSpaces := chartW - len(leftDate) - len(rightDate)
    if innerSpaces < 1 {
        innerSpaces = 1
    }
    timelineHeader := leftDate + strings.Repeat(" ", innerSpaces) + rightDate
    headerLabel := padRight("  Task", labelW)
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
    barRunes := make([]rune, chartW)
    barColors := make([]int, chartW)

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

// ── Tags list ─────────────────────────────────────────────────────────────────

func (m model) renderTagList() string {
    var b strings.Builder
    tags := m.getFilteredTagsForTab()

    if len(tags) == 0 {
        if m.tagTabSearchQuery != "" {
            return normalStyle.Render("  No tags match your filter.")
        }
        return normalStyle.Render("  No tags yet. Add tags to tasks in the detail view.")
    }

    barW := m.termWidth / ganttBarWidthDivisor
    if barW < minTagBarWidth {
        barW = minTagBarWidth
    }
    if barW > maxTagBarWidth {
        barW = maxTagBarWidth
    }

    gradLen := len(tagProgressGradient)
    stats := computeTagStats(m.todos)

    sortLabel := "alpha"
    if m.tagSort == tagSortCount {
        sortLabel = "count"
    }
    counter := fmt.Sprintf("%d/%d  sort:%s", m.tagTabCursor+1, len(tags), sortLabel)
    headerLeft := padRight("  Tag", tagLabelColWidth) + "Progress"
    padW := m.termWidth - 6 - len([]rune(headerLeft)) - len([]rune(counter)) - barW
    if padW < 1 {
        padW = 1
    }
    b.WriteString(
        headerStyle.Render(headerLeft+strings.Repeat(" ", padW+barW))+
            tagSortIndicatorStyle.Render(counter)+"\n",
    )
    b.WriteString(dimStyle.Render("  "+strings.Repeat("─", tagLabelColWidth+barW+18)) + "\n")

    for i, tag := range tags {
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

        var barStr strings.Builder
        barStr.Grow(barW * 4)
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

// ── Task lists ────────────────────────────────────────────────────────────────

func (m model) renderTaskList() string {
    var b strings.Builder
    active := m.activeTodos()
    if len(active) == 0 {
        if m.searchQuery != "" {
            return normalStyle.Render("  No tasks match your search.")
        }
        return normalStyle.Render("  No tasks yet. Press 'a' to add one.")
    }
    renderListHeader(&b, m.termWidth, m.cursor, len(active), false, m.taskSort)
    for i, t := range active {
        b.WriteString(m.renderTaskLine(t, i, m.cursor, m.pane == paneList))
    }
    return b.String()
}

func (m model) renderHistoryList() string {
    var b strings.Builder
    completed := m.completedTodos()
    if len(completed) == 0 {
        if m.searchQuery != "" {
            return normalStyle.Render("  No completed tasks match your search.")
        }
        return normalStyle.Render("  No completed tasks yet.")
    }
    renderListHeader(&b, m.termWidth, m.cursor, len(completed), true, m.taskSort)
    for i, t := range completed {
        b.WriteString(m.renderHistoryLine(t, i, m.cursor, m.pane == paneList))
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
    startCol := padRight(startVal, 10)
    dueCol := padRight(dueVal, 10)
    completedCol := padRight(completedVal, 10)
    tagsPart := renderTagsPart(t.Tags)

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

func (m model) renderTaskLine(t todo.Todo, index, cursor int, active bool) string {
    titleW := titleColWidth(m.termWidth)
    cursorStr := "  "
    if index == cursor && active {
        cursorStr = "▶ "
    }
    checkbox := "[ ]"
    if t.Status == todo.Done {
        checkbox = "[✓]"
    }
    title := t.Title
    if t.HasOverdueDependency(m.todos) {
        title += " !"
    }
    startVal := ""
    if !t.StartDate.IsZero() {
        startVal = t.StartDate.Format("02-01-06")
    }
    dueVal := ""
    if !t.DueDate.IsZero() {
        dueVal = t.DueDate.Format("02-01-06")
    }
    titleCol := padRight(truncate(title, titleW), titleW)
    startCol := padRight(startVal, 10)
    dueCol := padRight(dueVal, 10)
    prioCol := padRight(t.Priority.Icon()+" "+t.Priority.String(), 10)
    tagsPart := renderTagsPart(t.Tags)
    line := cursorStr + checkbox + " " + titleCol + startCol + dueCol + prioCol
    switch {
    case t.IsOverdue():
        return overdueStyle.Render(line) + " " + tagsPart + "\n"
    case t.HasOverdueDependency(m.todos):
        return depOverdueStyle.Render(line) + " " + tagsPart + "\n"
    case index == cursor && active:
        return selectedStyle.Render(line) + " " + tagsPart + "\n"
    default:
        return normalStyle.Render(line) + " " + tagsPart + "\n"
    }
}

// ── Detail pages ──────────────────────────────────────────────────────────────

func (m model) renderDetailPage1(t *todo.Todo) string {
    var b strings.Builder
    availableW := m.termWidth - 8
    isDetailFocused := m.pane == paneDetail && m.detailPage == 0

    renderField := func(label, value string, field detailField) string {
        cur := "  "
        isCurrent := isDetailFocused && m.detailField == field
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

    indicator := "[1/2]"
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
    if isDetailFocused && m.detailField == fieldTags {
        tagCur = "▶ "
    }
    b.WriteString(tagCur + detailLabelStyle.Render("Tags:") + "\n")
    if len(t.Tags) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No tags. Press 'a' to add one.") + "\n")
    } else {
        for i, tag := range t.Tags {
            pfx := "  "
            if isDetailFocused && m.detailField == fieldTags && i == m.tagCursor {
                pfx = "▶ "
                b.WriteString(detailSelectedStyle.Render(pfx) + tagStyle.Render("⟨#"+tag+"⟩") + "\n")
            } else {
                b.WriteString(dimStyle.Render(pfx) + tagStyle.Render("⟨#"+tag+"⟩") + "\n")
            }
        }
    }
    b.WriteString("\n")

    depCur := "  "
    if isDetailFocused && m.detailField == fieldDependencies {
        depCur = "▶ "
    }
    b.WriteString(depCur + detailLabelStyle.Render("Dependencies:") + "\n")
    if len(t.Dependencies) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No dependencies. Press 'a' to add one.") + "\n")
    } else {
        for i, depID := range t.Dependencies {
            dep := m.findTodoByID(depID)
            pfx := "  "
            isDepSelected := isDetailFocused && m.detailField == fieldDependencies && i == m.depCursor
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
    if isDetailFocused && m.detailField == fieldLearnings {
        learningCur = "▶ "
    }
    b.WriteString(learningCur + detailLabelStyle.Render("Learnings:") + "\n")
    if len(t.Learnings) == 0 {
        b.WriteString("  " + detailValueStyle.Render("No learnings yet. Press 'a' to add one.") + "\n")
    } else {
        for i, l := range t.Learnings {
            pfx := "  "
            isLearningSelected := isDetailFocused && m.detailField == fieldLearnings && i == m.learningDetailCursor
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

func (m model) renderDetailPage2(t *todo.Todo) string {
    var b strings.Builder
    availableW := m.termWidth - 8
    innerW := m.termWidth - 10
    if innerW < minInnerWidth {
        innerW = minInnerWidth
    }
    indicator := "[2/2]"
    titleText := truncate(t.Title, availableW-len(indicator)-2)
    padW := availableW - len([]rune(titleText)) - len([]rune(indicator))
    if padW < 1 {
        padW = 1
    }
    b.WriteString(detailTitleStyle.Render(titleText) +
        strings.Repeat(" ", padW) +
        pageIndicatorStyle.Render(indicator) + "\n\n")
    isDetailFocused := m.pane == paneDetail && m.detailPage == 1
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
            isSelected := isDetailFocused && i == m.commentCursor
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
