package main

import (
    "fmt"
    "strings"
    "time"

    "github.com/charmbracelet/lipgloss"
    "github.com/charmbracelet/x/ansi"
    "taskr/todo"
)

// truncateLines ANSI-aware-truncates every line to maxW display cells so
// over-long lines can never wrap inside a bordered panel.
func truncateLines(lines []string, maxW int) {
    for i, line := range lines {
        lines[i] = ansi.Truncate(line, maxW, "")
    }
}

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
    out.WriteString(ansi.Truncate(tabsStr+strings.Repeat(" ", padW)+shortcutHint, m.termWidth-2, "") + "\n")
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
    // Remove from second-to-last so the bottom border is always preserved.
    for len(listSplit) > availableForList {
        n := len(listSplit)
        listSplit = append(listSplit[:n-2], listSplit[n-1:]...)
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
        visible[0] = dimStyle.Render("  (...)")
    }
    if end < len(lines) {
        visible[len(visible)-1] = dimStyle.Render("  (...)")
    }

    return strings.Join(visible, "\n")
}

// ── Footer builder ────────────────────────────────────────────────────────────


func (m model) buildFooterContent(w int) string {
    switch m.mode {
    case modeNormal:
        return m.renderKeyHints(w)
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

// ── Key hints ─────────────────────────────────────────────────────────────────

func (m model) renderKeyHints(w int) string {
    var hints string
    switch {
    case m.tab == tabTasks && m.pane == paneDetail:
        hints = "←/→ pages · enter edit · a add · d toggle · x remove · n notes · esc back"
    case m.tab == tabTasks:
        hints = "enter detail · a add · d done · p prio · r rename · x del · n notes · f focus · s sort · h history · / search"
    case m.tab == tabProjects:
        hints = "j/k nav · r rename · x delete · / filter"
    case m.tab == tabTags:
        hints = "j/k nav · r rename · x delete · s sort · / filter"
    case m.tab == tabLearnings:
        hints = "j/k nav · r edit · x delete · s sort · / search"
    case m.tab == tabStats:
        hints = "tab or 1-5 · switch view"
    }
    return helpStyle.Render("  " + truncate(hints, w))
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
    truncateLines(rawList, w-2)
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
    truncateLines(projLines, w-2)
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
    truncateLines(ganttLines, w-2)
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
            {"p", "cycle priority low/med/high"},
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
            {"u", "undo last change"},
            {"q", "quit"},
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

    // Each column = 1 cell (▄) + 1 space gap = 2 chars, last column = 1 char.
    // 2 chars reserved for day labels on the left.
    innerW := m.termWidth - 8
    numWeeks := (innerW - 2 + 1) / 2
    if numWeeks < 6 {
        numWeeks = 6
    }

    // Start on the Monday of the week numWeeks-1 weeks ago.
    // (weekday+6)%7 converts Go's Sun=0..Sat=6 to Mon=0..Sun=6.
    startMonday := today.AddDate(0, 0, -((int(today.Weekday())+6)%7 + (numWeeks-1)*7))

    // Count completions per calendar day.
    counts := make(map[string]int)
    total := 0
    maxCount := 0
    for i := range m.todos {
        t := &m.todos[i]
        if t.Status != todo.Done || t.CompletedAt.IsZero() || t.ParentID != "" {
            continue
        }
        d := time.Date(t.CompletedAt.Year(), t.CompletedAt.Month(), t.CompletedAt.Day(),
            0, 0, 0, 0, t.CompletedAt.Location())
        if !d.Before(startMonday) && !d.After(today) {
            key := d.Format("2006-01-02")
            counts[key]++
            total++
            if counts[key] > maxCount {
                maxCount = counts[key]
            }
        }
    }

    gradLen := len(statsGradient)

    // Header.
    headerText := fmt.Sprintf("Activity — %d weeks", numWeeks)
    totalText := fmt.Sprintf("%d completed", total)
    spacer := innerW - len([]rune(headerText)) - len([]rune(totalText))
    if spacer < 1 {
        spacer = 1
    }
    b.WriteString(statsHeaderStyle.Render(headerText) + strings.Repeat(" ", spacer) + dimStyle.Render(totalText) + "\n")

    // Month labels: placed at the column where each new month begins.
    // Grid width = numWeeks*2 - 1; each column starts at w*2.
    gridW := numWeeks*2 - 1
    labelRunes := make([]rune, gridW)
    for i := range labelRunes {
        labelRunes[i] = ' '
    }
    prevMonth := time.Month(0)
    for w := 0; w < numWeeks; w++ {
        weekMon := startMonday.AddDate(0, 0, w*7)
        if weekMon.Month() != prevMonth {
            pos := w * 2
            if pos+3 > gridW {
                pos = gridW - 3
            }
            if pos >= 0 {
                for j, ch := range []rune(weekMon.Format("Jan")) {
                    labelRunes[pos+j] = ch
                }
            }
            prevMonth = weekMon.Month()
        }
    }
    b.WriteString("  " + dimStyle.Render(string(labelRunes)) + "\n")

    // 7 day rows (Sunday first). ▄ = lower-half block: the empty upper half
    // of each character acts as a natural row separator, giving a grid look.
    // dow 1=Mon, 3=Wed, 5=Fri get a single-char label; others get a space.
    // dow 0=Mon, 1=Tue, 2=Wed, 3=Thu, 4=Fri, 5=Sat, 6=Sun
    dayLabels := [7]string{"m", " ", "w", " ", "f", " ", " "}
    for dow := 0; dow < 7; dow++ {
        b.WriteString(dimStyle.Render(dayLabels[dow]) + " ")
        for w := 0; w < numWeeks; w++ {
            day := startMonday.AddDate(0, 0, w*7+dow)
            if day.After(today) {
                b.WriteString(" ")
            } else {
                count := counts[day.Format("2006-01-02")]
                if count == 0 {
                    b.WriteString(dimStyle.Render("▄"))
                } else {
                    gradIdx := gradLen - 1
                    if maxCount > 1 {
                        gradIdx = int(float64(count-1) / float64(maxCount-1) * float64(gradLen-1))
                        if gradIdx >= gradLen {
                            gradIdx = gradLen - 1
                        }
                    }
                    b.WriteString(statsGradient[gradIdx].Render("▄"))
                }
            }
            if w < numWeeks-1 {
                b.WriteString(" ")
            }
        }
        b.WriteString("\n")
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

