package main

import (
    "fmt"
    "os/exec"
    "strings"
    "time"

    "github.com/charmbracelet/bubbles/textinput"
    tea "github.com/charmbracelet/bubbletea"
    "taskr/todo"
)

// ── Top-level Update ──────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    m.frameTime = time.Now()

    if sz, ok := msg.(tea.WindowSizeMsg); ok {
        m.termWidth = sz.Width
        m.termHeight = sz.Height
        m.invalidateDetailCache()
    }

    switch msg := msg.(type) {
    case clearErrMsg:
        m.err = ""
        return m, nil
    case saveDoneMsg:
        return m, nil
    case saveErrMsg:
        m.err = fmt.Sprintf("Error saving tasks: %v", msg.err)
        return m, clearErrAfter()
    case editorFinishedMsg:
        return m.handleEditorFinished(msg.taskID)
    case saveTickMsg:
        m.saveScheduled = false
        if m.savePending {
            m.savePending = false
            saveCmd, err := prepareSave(m.todos)
            if err != nil {
                m.err = fmt.Sprintf("Error marshalling tasks: %v", err)
                return m, clearErrAfter()
            }
            return m, saveCmd
        }
        return m, nil
    }

    switch m.mode {
    case modeHelp:
        return m.updateHelp(msg)
    case modeConfirmDelete:
        return m.updateConfirmDelete(msg)
    case modeConfirmDeleteComment:
        return m.updateConfirmDeleteComment(msg)
    case modeConfirmDeleteDep:
        return m.updateConfirmDeleteDep(msg)
    case modeConfirmDeleteTag:
        return m.updateConfirmDeleteTag(msg)
    case modeConfirmDeleteTagGlobal:
        return m.updateConfirmDeleteTagGlobal(msg)
    case modeConfirmDeleteProject:
        return m.updateConfirmDeleteProject(msg)
    case modeConfirmDeleteLearning:
        return m.updateConfirmDeleteLearning(msg)
    case modeConfirmDeleteSubtask:
        return m.updateConfirmDeleteSubtask(msg)
    case modeInput:
        return m.updateInput(msg)
    case modeEditComment:
        return m.updateEditComment(msg)
    case modeEditTag:
        return m.updateEditTag(msg)
    case modeEditTitle:
        return m.updateEditTitle(msg)
    case modeEditProjectInline:
        return m.updateEditProjectInline(msg)
    case modeEditLearning:
        return m.updateEditLearning(msg)
    case modeAddLearning:
        return m.updateAddLearning(msg)
    case modeAddSubtask:
        return m.updateAddSubtask(msg)
    case modeSearch:
        return m.updateSearch(msg)
    case modeSearchDep:
        return m.updateSearchDep(msg)
    case modeSearchTag:
        return m.updateSearchTag(msg)
    case modeSearchProject:
        return m.updateSearchProject(msg)
    case modeSearchTagTab:
        return m.updateSearchTagTab(msg)
    }

    var newModel tea.Model
    var cmd tea.Cmd
    if m.pane == paneList {
        newModel, cmd = m.updateList(msg)
    } else {
        newModel, cmd = m.updateDetail(msg)
    }

    if nm, ok := newModel.(model); ok {
        if nm.dirty {
            nm.dirty = false
            nm.savePending = true
            if !nm.saveScheduled {
                nm.saveScheduled = true
                saveCmd := scheduleSave()
                if cmd != nil {
                    return nm, tea.Batch(cmd, saveCmd)
                }
                return nm, saveCmd
            }
            return nm, cmd
        }
        return nm, cmd
    }
    return newModel, cmd
}

// ── Editor handling ───────────────────────────────────────────────────────────

func (m *model) openEditorForNotes() tea.Cmd {
    idx := m.currentTodoIndex()
    if idx < 0 {
        return nil
    }
    t := &m.todos[idx]
    taskID := t.ID

    if err := writeNotesFile(taskID, t.Notes); err != nil {
        m.err = fmt.Sprintf("Error writing notes file: %v", err)
        return clearErrAfter()
    }

    m.editorTaskID = taskID
    filePath := notesFilePath(taskID)
    c := exec.Command(m.editorCmd, filePath)
    return tea.ExecProcess(c, func(err error) tea.Msg {
        return editorFinishedMsg{taskID: taskID}
    })
}

func (m model) handleEditorFinished(taskID string) (tea.Model, tea.Cmd) {
    content, err := readNotesFile(taskID)
    if err != nil {
        m.err = fmt.Sprintf("Error reading notes: %v", err)
        return m, clearErrAfter()
    }

    for i := range m.todos {
        if m.todos[i].ID == taskID {
            newNotes := strings.TrimRight(content, "\n\r ")
            if newNotes != m.todos[i].Notes {
                m.pushUndo("edit notes")
                m.todos[i].SetNotes(newNotes)
                m.dirty = true
                m.cache.dirty = true
                m.invalidateDetailCache()
                m.refreshCaches()
            }
            break
        }
    }

    cleanupNotesFile(taskID)
    m.editorTaskID = ""

    if m.dirty {
        m.dirty = false
        m.savePending = true
        if !m.saveScheduled {
            m.saveScheduled = true
            return m, scheduleSave()
        }
    }
    return m, nil
}

// ── Undo action ───────────────────────────────────────────────────────────────

func (m *model) performUndo() tea.Cmd {
    entry, ok := m.popUndo()
    if !ok {
        m.err = "Nothing to undo"
        return clearErrAfter()
    }
    m.todos = entry.todos
    m.markModifiedNoUndo()
    m.err = fmt.Sprintf("Undid: %s", entry.desc)
    return clearErrAfter()
}

// ── Help overlay ──────────────────────────────────────────────────────────────

func (m model) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "?", "esc", "q":
            m.mode = modeNormal
        }
    }
    return m, nil
}

// ── List pane ─────────────────────────────────────────────────────────────────

func (m model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "q", "ctrl+c":
            return m, tea.Quit
        case "?":
            m.mode = modeHelp
            return m, nil
        case "u":
            return m, m.performUndo()

        case "n":
            if m.tab == tabTasks && !m.showHistory && m.currentTodo() != nil {
                return m, m.openEditorForNotes()
            }

        case "1":
            m.switchTab(tabTasks)
        case "2":
            m.switchTab(tabProjects)
        case "3":
            m.switchTab(tabTags)
        case "4":
            m.switchTab(tabLearnings)
        case "5":
            m.tab = tabStats
            m.pane = paneList
            m.listOffset = 0

        case "tab":
            m.switchTab((m.tab + 1) % 5)

        case "h":
            if m.tab == tabTasks {
                m.showHistory = !m.showHistory
                m.cursor = 0
                m.listOffset = 0
            }

        case "f":
            if m.tab == tabTasks && !m.showHistory {
                m.focusFilter = !m.focusFilter
                m.cursor = 0
                m.listOffset = 0
                m.markCacheDirty()
            }

        case "right":
            if m.tab == tabTasks && !m.showHistory {
                if t := m.currentTodo(); t != nil && len(t.SubtaskIDs) > 0 {
                    m.expandedTasks[t.ID] = true
                }
            }
        case "left":
            if m.tab == tabTasks && !m.showHistory {
                if t := m.currentTodo(); t != nil {
                    delete(m.expandedTasks, t.ID)
                }
            }

        case "/":
            return m.startSearch()

        case "s":
            m.cycleSortMode()

        case "esc":
            m.handleListEsc()

        case "up", "k":
            m.moveCursorUp()
        case "down", "j":
            m.moveCursorDown()

        case "enter":
            return m.handleListEnter()

        case "r":
            return m.handleListRename()

        case "x", "delete":
            return m.handleListDelete()

        case "a":
            if m.tab == tabTasks && !m.showHistory {
                m.mode = modeInput
                m.textInput.SetValue("")
                m.textInput.Placeholder = "New task (use #tag due:date p:high @project)..."
                m.textInput.Focus()
                return m, textinput.Blink
            }

        case "d":
            if m.tab == tabTasks {
                if idx := m.currentTodoIndex(); idx >= 0 {
                    m.todos[idx].Toggle()
                    m.markModified()
                    if m.cursor > 0 {
                        m.cursor--
                    }
                }
            }
        }
    }

    switch m.tab {
    case tabTasks:
        if m.showHistory {
            m.clampListOffset(len(m.cache.done))
        } else {
            m.clampListOffset(len(m.cache.active))
        }
    case tabProjects:
        m.clampListOffset(len(m.allProjectsForList()))
    }
    return m, nil
}

// ── List helper methods ───────────────────────────────────────────────────────

func (m *model) switchTab(t tab) {
    m.tab = t
    m.cursor = 0
    m.projectCursor = 0
    m.tagTabCursor = 0
    m.learningCursor = 0
    m.listOffset = 0
    m.pane = paneList
    m.searchQuery = ""
    m.tagTabSearchQuery = ""
    m.learningSearchQuery = ""
    m.projectTaskMode = false
    m.showHistory = false
    m.invalidateDetailCache()
    m.markCacheDirty()
}

func (m model) startSearch() (tea.Model, tea.Cmd) {
    switch m.tab {
    case tabTags:
        m.mode = modeSearchTagTab
        m.tagTabSearchInput.SetValue("")
        m.tagTabSearchQuery = ""
        m.tagTabCursor = 0
        m.tagTabSearchInput.Focus()
        return m, textinput.Blink
    case tabLearnings:
        m.mode = modeSearch
        m.learningSearchInput.SetValue("")
        m.learningSearchQuery = ""
        m.learningCursor = 0
        m.learningSearchInput.Focus()
        return m, textinput.Blink
    case tabTasks, tabProjects:
        m.mode = modeSearch
        m.searchInput.SetValue("")
        m.searchInput.Focus()
        return m, textinput.Blink
    }
    return m, nil
}

func (m *model) cycleSortMode() {
    switch m.tab {
    case tabTags:
        if m.tagSort == tagSortAlpha {
            m.tagSort = tagSortCount
        } else {
            m.tagSort = tagSortAlpha
        }
        m.tagTabCursor = 0
    case tabTasks:
        switch m.taskSort {
        case taskSortDueDate:
            m.taskSort = taskSortPriority
        case taskSortPriority:
            m.taskSort = taskSortCreated
        default:
            m.taskSort = taskSortDueDate
        }
        m.cursor = 0
        m.listOffset = 0
        m.markCacheDirty()
    case tabLearnings:
        if m.learningSort == learningSortDate {
            m.learningSort = learningSortAlpha
        } else {
            m.learningSort = learningSortDate
        }
        m.learningCursor = 0
    }
}

func (m *model) handleListEsc() {
    switch {
    case m.tab == tabTasks && m.focusFilter:
        m.focusFilter = false
        m.cursor = 0
        m.listOffset = 0
        m.markCacheDirty()
    case m.tab == tabTags && m.tagTabSearchQuery != "":
        m.tagTabSearchQuery = ""
        m.tagTabCursor = 0
    case m.tab == tabLearnings && m.learningSearchQuery != "":
        m.learningSearchQuery = ""
        m.learningCursor = 0
    case m.tab == tabProjects && m.projectTaskMode:
        m.projectTaskMode = false
        m.cursor = 0
    case m.tab == tabTasks && m.showHistory:
        m.showHistory = false
        m.cursor = 0
        m.listOffset = 0
    }
}

func (m *model) moveCursorUp() {
    switch m.tab {
    case tabTags:
        if m.tagTabCursor > 0 {
            m.tagTabCursor--
        }
    case tabLearnings:
        if m.learningCursor > 0 {
            m.learningCursor--
        }
    case tabProjects:
        if m.projectTaskMode {
            if m.cursor > 0 {
                m.cursor--
            }
        } else if m.projectCursor > 0 {
            m.projectCursor--
            m.cursor = 0
            m.listOffset = 0
        }
    case tabTasks:
        if m.cursor > 0 {
            m.cursor--
        }
    }
}

func (m *model) moveCursorDown() {
    switch m.tab {
    case tabTags:
        if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags)-1 {
            m.tagTabCursor++
        }
    case tabLearnings:
        if learnings := m.allLearnings(); m.learningCursor < len(learnings)-1 {
            m.learningCursor++
        }
    case tabProjects:
        projects := m.allProjectsForList()
        if m.projectTaskMode {
            if m.projectCursor < len(projects) {
                tasks := m.getProjectTasks(projects[m.projectCursor])
                if m.cursor < len(tasks)-1 {
                    m.cursor++
                }
            }
        } else if m.projectCursor < len(projects)-1 {
            m.projectCursor++
            m.cursor = 0
            m.listOffset = 0
        }
    case tabTasks:
        if m.showHistory {
            if m.cursor < len(m.cache.done)-1 {
                m.cursor++
            }
        } else {
            if m.cursor < len(m.cache.active)-1 {
                m.cursor++
            }
        }
    }
}

func (m model) handleListEnter() (tea.Model, tea.Cmd) {
    switch m.tab {
    case tabLearnings:
        if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
            m.pane = paneDetail
        }
    case tabProjects:
        if !m.projectTaskMode {
            if projects := m.allProjectsForList(); m.projectCursor < len(projects) {
                m.projectTaskMode = true
                m.cursor = 0
            }
        } else if m.currentTodo() != nil {
            m.pane = paneDetail
            m.detail = detailState{field: fieldStartDate}
            m.invalidateDetailCache()
        }
    case tabTasks:
        if m.currentTodo() != nil {
            m.pane = paneDetail
            m.detail = detailState{field: fieldStartDate}
            m.invalidateDetailCache()
        }
    }
    return m, nil
}

func (m model) handleListRename() (tea.Model, tea.Cmd) {
    switch m.tab {
    case tabTags:
        if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) {
            m.editingTagName = tags[m.tagTabCursor]
            m.mode = modeEditTag
            m.textInput.SetValue(tags[m.tagTabCursor])
            m.textInput.Placeholder = "Edit tag name..."
            m.textInput.Focus()
            return m, textinput.Blink
        }
    case tabTasks:
        if !m.showHistory {
            if t := m.currentTodo(); t != nil {
                m.mode = modeEditTitle
                m.textInput.SetValue(t.Title)
                m.textInput.Placeholder = "Edit task title..."
                m.textInput.Focus()
                return m, textinput.Blink
            }
        }
    case tabProjects:
        if !m.projectTaskMode {
            if projects := m.allProjectsForList(); m.projectCursor < len(projects) {
                m.editingProjectName = projects[m.projectCursor]
                m.mode = modeEditProjectInline
                m.textInput.SetValue(projects[m.projectCursor])
                m.textInput.Focus()
                return m, textinput.Blink
            }
        }
    case tabLearnings:
        if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
            m.pendingLearning = m.learningCursor
            m.mode = modeEditLearning
            m.textInput.SetValue(learnings[m.learningCursor].Text)
            m.textInput.Placeholder = "Edit learning..."
            m.textInput.Focus()
            return m, textinput.Blink
        }
    }
    return m, nil
}

func (m model) handleListDelete() (tea.Model, tea.Cmd) {
    switch m.tab {
    case tabTags:
        if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) {
            m.mode = modeConfirmDeleteTagGlobal
            m.confirmMsg = fmt.Sprintf("Delete tag '#%s' from ALL tasks? (y/n)", tags[m.tagTabCursor])
        }
    case tabTasks:
        if t := m.currentTodo(); t != nil {
            m.mode = modeConfirmDelete
            m.pendingDelete = m.cursor
            m.confirmMsg = fmt.Sprintf("Delete '%s'? (y/n)", t.Title)
        }
    case tabLearnings:
        if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
            m.mode = modeConfirmDeleteLearning
            m.pendingLearning = m.learningCursor
            m.confirmMsg = fmt.Sprintf("Delete learning '%s'? (y/n)", truncate(learnings[m.learningCursor].Text, 40))
        }
    }
    return m, nil
}

// ── Detail pane ───────────────────────────────────────────────────────────────

func (m model) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
    if m.tab == tabLearnings {
        return m.updateLearningsDetail(msg)
    }

    key, ok := msg.(tea.KeyMsg)
    if !ok {
        return m, nil
    }

    switch key.String() {
    case "?":
        m.mode = modeHelp
        return m, nil
    case "u":
        return m, m.performUndo()
    case "n":
        if m.currentTodo() != nil {
            return m, m.openEditorForNotes()
        }
        return m, nil
    case "esc":
        m.pane = paneList
        m.detail = detailState{field: fieldStartDate}
        m.invalidateDetailCache()

    case "left":
        if m.detail.page > 0 {
            m.detail.page--
            m.detail.field = fieldStartDate
            m.invalidateDetailCache()
        }
    case "right", "l":
        if m.detail.page < 1 {
            m.detail.page++
            m.detail.commentCursor = 0
            m.invalidateDetailCache()
        }

    case "up", "k":
        m.detailCursorUp()
    case "down", "j":
        m.detailCursorDown()

    case "enter":
        return m.startEditing()

    case "d":
        if m.detail.page == 0 && m.detail.field == fieldSubtasks {
            if idx := m.currentTodoIndex(); idx >= 0 {
                if m.detail.subtaskCursor < len(m.todos[idx].SubtaskIDs) {
                    m.toggleSubtask(idx, m.detail.subtaskCursor)
                    m.markModified()
                }
            }
        }

    case "a":
        return m.detailAdd()

    case "x", "delete":
        return m.detailDelete()
    }
    return m, nil
}

func (m *model) detailCursorUp() {
    m.invalidateDetailCache()
    if m.detail.page == 0 {
        switch m.detail.field {
        case fieldDueDate:
            m.detail.field = fieldStartDate
        case fieldPriority:
            m.detail.field = fieldDueDate
        case fieldProject:
            m.detail.field = fieldPriority
        case fieldNotes:
            m.detail.field = fieldProject
        case fieldTags:
            if m.detail.tagCursor > 0 {
                m.detail.tagCursor--
            } else {
                m.detail.field = fieldNotes
            }
        case fieldDependencies:
            if m.detail.depCursor > 0 {
                m.detail.depCursor--
            } else {
                m.detail.field = fieldTags
            }
        case fieldLearnings:
            if m.detail.learningCursor > 0 {
                m.detail.learningCursor--
            } else {
                m.detail.field = fieldDependencies
            }
        case fieldSubtasks:
            if m.detail.subtaskCursor > 0 {
                m.detail.subtaskCursor--
            } else {
                m.detail.field = fieldLearnings
            }
        }
    } else if m.detail.commentCursor > 0 {
        m.detail.commentCursor--
    }
}

func (m *model) detailCursorDown() {
    m.invalidateDetailCache()
    if m.detail.page == 0 {
        t := m.currentTodo()
        switch m.detail.field {
        case fieldStartDate:
            m.detail.field = fieldDueDate
        case fieldDueDate:
            m.detail.field = fieldPriority
        case fieldPriority:
            m.detail.field = fieldProject
        case fieldProject:
            m.detail.field = fieldNotes
        case fieldNotes:
            m.detail.field = fieldTags
            m.detail.tagCursor = 0
        case fieldTags:
            if t != nil && m.detail.tagCursor < len(t.Tags)-1 {
                m.detail.tagCursor++
            } else {
                m.detail.field = fieldDependencies
                m.detail.depCursor = 0
            }
        case fieldDependencies:
            if t != nil && m.detail.depCursor < len(t.Dependencies)-1 {
                m.detail.depCursor++
            } else {
                m.detail.field = fieldLearnings
                m.detail.learningCursor = 0
            }
        case fieldLearnings:
            if t != nil && m.detail.learningCursor < len(t.Learnings)-1 {
                m.detail.learningCursor++
            } else {
                m.detail.field = fieldSubtasks
                m.detail.subtaskCursor = 0
            }
        case fieldSubtasks:
            if t != nil && m.detail.subtaskCursor < len(t.SubtaskIDs)-1 {
                m.detail.subtaskCursor++
            }
        }
    } else if t := m.currentTodo(); t != nil && m.detail.commentCursor < len(t.Comments)-1 {
        m.detail.commentCursor++
    }
}

func (m model) detailAdd() (tea.Model, tea.Cmd) {
    if m.detail.page == 1 {
        m.mode = modeInput
        m.textInput.SetValue("")
        m.textInput.Placeholder = "Add comment..."
        m.textInput.Focus()
        return m, textinput.Blink
    }
    switch m.detail.field {
    case fieldDependencies:
        m.mode = modeSearchDep
        m.depSearchInput.SetValue("")
        m.depSearch = searchState{}
        m.depSearchInput.Focus()
        return m, textinput.Blink
    case fieldTags:
        m.mode = modeSearchTag
        m.tagSearchInput.SetValue("")
        m.tagSearch = searchState{}
        m.tagSearchInput.Focus()
        return m, textinput.Blink
    case fieldProject:
        m.mode = modeSearchProject
        m.projSearchInput.SetValue("")
        m.projSearch = searchState{}
        m.projSearchInput.Focus()
        return m, textinput.Blink
    case fieldLearnings:
        m.mode = modeAddLearning
        m.textInput.SetValue("")
        m.textInput.Placeholder = "Add learning..."
        m.textInput.Focus()
        return m, textinput.Blink
    case fieldSubtasks:
        m.mode = modeAddSubtask
        m.textInput.SetValue("")
        m.textInput.Placeholder = "Add subtask..."
        m.textInput.Focus()
        return m, textinput.Blink
    }
    return m, nil
}

func (m model) detailDelete() (tea.Model, tea.Cmd) {
    idx := m.currentTodoIndex()
    if idx < 0 {
        return m, nil
    }
    if m.detail.page == 1 {
        if len(m.todos[idx].Comments) > 0 {
            m.mode = modeConfirmDeleteComment
            m.pendingComment = m.detail.commentCursor
            m.confirmMsg = "Delete this comment? (y/n)"
        }
        return m, nil
    }
    switch m.detail.field {
    case fieldProject:
        if m.todos[idx].Project != "" {
            m.mode = modeConfirmDeleteProject
            m.confirmMsg = fmt.Sprintf("Remove project '%s' from this task? (y/n)", m.todos[idx].Project)
        }
    case fieldNotes:
        if m.todos[idx].Notes != "" {
            m.pushUndo("clear notes")
            m.todos[idx].SetNotes("")
            m.markModifiedNoUndo()
        }
    case fieldTags:
        if len(m.todos[idx].Tags) > 0 && m.detail.tagCursor < len(m.todos[idx].Tags) {
            m.mode = modeConfirmDeleteTag
            m.pendingTag = m.detail.tagCursor
            m.confirmMsg = fmt.Sprintf("Remove tag '#%s' from this task? (y/n)", m.todos[idx].Tags[m.detail.tagCursor])
        }
    case fieldDependencies:
        if len(m.todos[idx].Dependencies) > 0 && m.detail.depCursor < len(m.todos[idx].Dependencies) {
            m.mode = modeConfirmDeleteDep
            m.pendingDep = m.detail.depCursor
            m.confirmMsg = "Remove this dependency? (y/n)"
        }
    case fieldLearnings:
        if len(m.todos[idx].Learnings) > 0 && m.detail.learningCursor < len(m.todos[idx].Learnings) {
            m.mode = modeConfirmDeleteLearning
            m.pendingLearning = m.detail.learningCursor
            m.confirmMsg = fmt.Sprintf("Delete learning '%s'? (y/n)", truncate(m.todos[idx].Learnings[m.detail.learningCursor].Text, 40))
        }
    case fieldSubtasks:
        if len(m.todos[idx].SubtaskIDs) > 0 && m.detail.subtaskCursor < len(m.todos[idx].SubtaskIDs) {
            subID := m.todos[idx].SubtaskIDs[m.detail.subtaskCursor]
            subTitle := subID
            if sub := m.findTodoByID(subID); sub != nil {
                subTitle = sub.Title
            }
            m.mode = modeConfirmDeleteSubtask
            m.pendingSubtask = m.detail.subtaskCursor
            m.confirmMsg = fmt.Sprintf("Delete subtask '%s'? (y/n)", truncate(subTitle, 40))
        }
    }
    return m, nil
}

func (m model) updateLearningsDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
    key, ok := msg.(tea.KeyMsg)
    if !ok {
        return m, nil
    }
    switch key.String() {
    case "?":
        m.mode = modeHelp
    case "u":
        return m, m.performUndo()
    case "esc":
        m.pane = paneList
    case "x", "delete":
        if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
            m.mode = modeConfirmDeleteLearning
            m.pendingLearning = m.learningCursor
            m.confirmMsg = fmt.Sprintf("Delete learning '%s'? (y/n)", truncate(learnings[m.learningCursor].Text, 40))
        }
    case "r":
        if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
            m.mode = modeEditLearning
            m.textInput.SetValue(learnings[m.learningCursor].Text)
            m.textInput.Placeholder = "Edit learning..."
            m.textInput.Focus()
            return m, textinput.Blink
        }
    }
    return m, nil
}

func (m model) startEditing() (tea.Model, tea.Cmd) {
    idx := m.currentTodoIndex()
    if idx < 0 {
        return m, nil
    }
    t := &m.todos[idx]

    if m.detail.page == 1 {
        if len(t.Comments) > 0 {
            m.mode = modeEditComment
            m.pendingComment = m.detail.commentCursor
            m.textInput.SetValue(t.Comments[m.detail.commentCursor].Text)
            m.textInput.Placeholder = "Edit comment..."
            m.textInput.Focus()
        }
        return m, textinput.Blink
    }

    switch m.detail.field {
    case fieldStartDate:
        m.mode = modeInput
        if !t.StartDate.IsZero() {
            m.textInput.SetValue(t.StartDate.Format("02-01-06"))
        } else {
            m.textInput.SetValue("")
        }
        m.textInput.Placeholder = "Start date (dd-mm-yy, 'today', 'next week', '+3d')..."
        m.textInput.Focus()
    case fieldDueDate:
        m.mode = modeInput
        if !t.DueDate.IsZero() {
            m.textInput.SetValue(t.DueDate.Format("02-01-06"))
        } else {
            m.textInput.SetValue("")
        }
        m.textInput.Placeholder = "Due date (dd-mm-yy, 'today', 'next week', '+3d')..."
        m.textInput.Focus()
    case fieldPriority:
        switch t.Priority {
        case todo.PriorityLow:
            m.todos[idx].SetPriority(todo.PriorityMedium)
        case todo.PriorityMedium:
            m.todos[idx].SetPriority(todo.PriorityHigh)
        default:
            m.todos[idx].SetPriority(todo.PriorityLow)
        }
        m.markModified()
        return m, nil
    case fieldProject:
        m.mode = modeSearchProject
        m.projSearchInput.SetValue(t.Project)
        m.projSearch = searchState{query: t.Project}
        m.projSearchInput.Focus()
        return m, textinput.Blink
    case fieldNotes:
        return m, m.openEditorForNotes()
    case fieldTags:
        m.mode = modeSearchTag
        m.tagSearchInput.SetValue("")
        m.tagSearch = searchState{}
        m.tagSearchInput.Focus()
        return m, textinput.Blink
    case fieldDependencies:
        if len(t.Dependencies) > 0 && m.detail.depCursor < len(t.Dependencies) {
            depID := t.Dependencies[m.detail.depCursor]
            for i, candidate := range m.cache.active {
                if candidate.ID == depID {
                    m.pane = paneList
                    m.cursor = i
                    m.tab = tabTasks
                    m.invalidateDetailCache()
                    return m, nil
                }
            }
        }
        return m, nil
    case fieldLearnings:
        if len(t.Learnings) > 0 && m.detail.learningCursor < len(t.Learnings) {
            m.mode = modeEditLearning
            m.textInput.SetValue(t.Learnings[m.detail.learningCursor].Text)
            m.textInput.Placeholder = "Edit learning..."
            m.textInput.Focus()
            return m, textinput.Blink
        }
    case fieldSubtasks:
        if len(t.SubtaskIDs) > 0 && m.detail.subtaskCursor < len(t.SubtaskIDs) {
            m.toggleSubtask(idx, m.detail.subtaskCursor)
            m.markModified()
            return m, nil
        }
    }
    return m, textinput.Blink
}

// ── Input handlers ────────────────────────────────────────────────────────────

func (m model) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            val := m.textInput.Value()
            m.mode = modeNormal
            if m.pane == paneList {
                if val != "" {
                    parsed := parseQuickAdd(val)
                    t := todo.New(parsed.title)
                    t.Priority = parsed.priority
                    if !parsed.dueDate.IsZero() {
                        t.DueDate = parsed.dueDate
                    }
                    if parsed.project != "" {
                        t.Project = parsed.project
                    }
                    t.Tags = append(t.Tags, parsed.tags...)
                    m.todos = append(m.todos, t)
                    m.markModified()
                }
            } else if idx := m.currentTodoIndex(); idx >= 0 {
                if m.detail.page == 1 {
                    if val != "" {
                        m.todos[idx].AddComment(val)
                        m.detail.commentCursor = len(m.todos[idx].Comments) - 1
                        m.markModified()
                    }
                } else {
                    switch m.detail.field {
                    case fieldStartDate:
                        if val == "" {
                            m.todos[idx].StartDate = time.Time{}
                        } else if d, err := parseDueDate(val); err == nil {
                            m.todos[idx].SetStartDate(d)
                        } else {
                            m.err = "Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'"
                            return m, clearErrAfter()
                        }
                    case fieldDueDate:
                        if val == "" {
                            m.todos[idx].DueDate = time.Time{}
                        } else if d, err := parseDueDate(val); err == nil {
                            m.todos[idx].SetDueDate(d)
                        } else {
                            m.err = "Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'"
                            return m, clearErrAfter()
                        }
                    }
                    m.markModified()
                }
            }
            return m, nil
        case "esc":
            m.mode = modeNormal
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateAddSubtask(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if val := strings.TrimSpace(m.textInput.Value()); val != "" {
                if idx := m.currentTodoIndex(); idx >= 0 {
                    m.addSubtask(idx, val)
                    m.detail.subtaskCursor = len(m.todos[idx].SubtaskIDs) - 1
                    m.markModified()
                }
            }
            m.mode = modeNormal
            return m, nil
        case "esc":
            m.mode = modeNormal
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateAddLearning(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if val := strings.TrimSpace(m.textInput.Value()); val != "" {
                if idx := m.currentTodoIndex(); idx >= 0 {
                    m.todos[idx].AddLearning(val)
                    m.detail.learningCursor = len(m.todos[idx].Learnings) - 1
                    m.markModified()
                }
            }
            m.mode = modeNormal
            return m, nil
        case "esc":
            m.mode = modeNormal
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateEditLearning(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if newText := strings.TrimSpace(m.textInput.Value()); newText != "" {
                if m.tab == tabLearnings {
                    if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
                        m.updateLearningByID(learnings[m.learningCursor].ID, newText)
                        m.markModified()
                    }
                } else if idx := m.currentTodoIndex(); idx >= 0 && m.detail.learningCursor < len(m.todos[idx].Learnings) {
                    m.todos[idx].UpdateLearning(m.detail.learningCursor, newText)
                    m.markModified()
                }
            }
            m.mode = modeNormal
            return m, nil
        case "esc":
            m.mode = modeNormal
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateEditTitle(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if newTitle := strings.TrimSpace(m.textInput.Value()); newTitle != "" {
                if idx := m.currentTodoIndex(); idx >= 0 {
                    m.todos[idx].Title = newTitle
                    m.todos[idx].ModifiedAt = time.Now()
                    m.markModified()
                }
            }
            m.mode = modeNormal
            return m, nil
        case "esc":
            m.mode = modeNormal
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateEditComment(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if idx := m.currentTodoIndex(); idx >= 0 {
                if val := m.textInput.Value(); val != "" {
                    m.todos[idx].UpdateComment(m.pendingComment, val)
                    m.markModified()
                }
            }
            m.mode = modeNormal
            return m, nil
        case "esc":
            m.mode = modeNormal
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateEditTag(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if newName := strings.TrimSpace(m.textInput.Value()); newName != "" && newName != m.editingTagName {
                m.renameTagGlobally(m.editingTagName, newName)
                m.markModified()
            }
            m.mode = modeNormal
            m.editingTagName = ""
            return m, nil
        case "esc":
            m.mode = modeNormal
            m.editingTagName = ""
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateEditProjectInline(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if newName := strings.TrimSpace(m.textInput.Value()); newName != "" && newName != m.editingProjectName {
                m.renameProjectGlobally(m.editingProjectName, newName)
                m.markModified()
            }
            m.mode = modeNormal
            m.editingProjectName = ""
            return m, nil
        case "esc":
            m.mode = modeNormal
            m.editingProjectName = ""
            return m, nil
        }
    }
    m.textInput, cmd = m.textInput.Update(msg)
    return m, cmd
}

func (m model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter", "esc":
            m.mode = modeNormal
            if m.tab == tabLearnings {
                m.learningSearchQuery = m.learningSearchInput.Value()
                m.learningCursor = 0
            } else {
                m.searchQuery = m.searchInput.Value()
                m.cursor = 0
                m.projectCursor = 0
                m.listOffset = 0
                m.markCacheDirty()
            }
            return m, nil
        }
    }
    if m.tab == tabLearnings {
        m.learningSearchInput, cmd = m.learningSearchInput.Update(msg)
        m.learningSearchQuery = m.learningSearchInput.Value()
        m.learningCursor = 0
    } else {
        m.searchInput, cmd = m.searchInput.Update(msg)
        m.searchQuery = m.searchInput.Value()
        m.cursor = 0
        m.projectCursor = 0
        m.listOffset = 0
        m.markCacheDirty()
    }
    return m, cmd
}

func (m model) updateSearchTagTab(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter", "esc":
            m.mode = modeNormal
            m.tagTabCursor = 0
            return m, nil
        }
    }
    m.tagTabSearchInput, cmd = m.tagTabSearchInput.Update(msg)
    m.tagTabSearchQuery = m.tagTabSearchInput.Value()
    m.tagTabCursor = 0
    return m, cmd
}

func (m model) updateSearchDep(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            results := m.depSearchResults()
            if m.depSearch.cursor < len(results) {
                if idx := m.currentTodoIndex(); idx >= 0 {
                    m.todos[idx].AddDependency(results[m.depSearch.cursor].ID)
                    m.markModified()
                }
            }
            m.mode = modeNormal
            m.depSearch = searchState{}
            return m, nil
        case "up", "k":
            if m.depSearch.cursor > 0 {
                m.depSearch.cursor--
            }
            return m, nil
        case "down", "j":
            if results := m.depSearchResults(); m.depSearch.cursor < len(results)-1 {
                m.depSearch.cursor++
            }
            return m, nil
        case "esc":
            m.mode = modeNormal
            m.depSearch = searchState{}
            return m, nil
        }
    }
    oldQuery := m.depSearch.query
    m.depSearchInput, cmd = m.depSearchInput.Update(msg)
    m.depSearch.query = m.depSearchInput.Value()
    if m.depSearch.query != oldQuery {
        m.depSearch.cursor = 0
    }
    return m, cmd
}

func (m model) updateSearchTag(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if idx := m.currentTodoIndex(); idx >= 0 {
                results := m.tagSearchResults()
                var tagToAdd string
                if m.tagSearch.cursor < len(results) {
                    tagToAdd = results[m.tagSearch.cursor]
                } else if m.tagSearch.query != "" {
                    tagToAdd = m.tagSearch.query
                }
                if tagToAdd != "" {
                    m.todos[idx].AddTag(tagToAdd)
                    m.markModified()
                }
            }
            m.mode = modeNormal
            m.tagSearch = searchState{}
            return m, nil
        case "up", "k":
            if m.tagSearch.cursor > 0 {
                m.tagSearch.cursor--
            }
            return m, nil
        case "down", "j":
            if results := m.tagSearchResults(); m.tagSearch.cursor < len(results)-1 {
                m.tagSearch.cursor++
            }
            return m, nil
        case "esc":
            m.mode = modeNormal
            m.tagSearch = searchState{}
            return m, nil
        }
    }
    oldQuery := m.tagSearch.query
    m.tagSearchInput, cmd = m.tagSearchInput.Update(msg)
    m.tagSearch.query = m.tagSearchInput.Value()
    if m.tagSearch.query != oldQuery {
        m.tagSearch.cursor = 0
    }
    return m, cmd
}

func (m model) updateSearchProject(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "enter":
            if idx := m.currentTodoIndex(); idx >= 0 {
                results := m.projSearchResults()
                var projToSet string
                if m.projSearch.cursor < len(results) {
                    projToSet = results[m.projSearch.cursor]
                } else if m.projSearch.query != "" {
                    projToSet = m.projSearch.query
                }
                if projToSet != "" {
                    m.todos[idx].SetProject(projToSet)
                    m.markModified()
                }
            }
            m.mode = modeNormal
            m.projSearch = searchState{}
            return m, nil
        case "up", "k":
            if m.projSearch.cursor > 0 {
                m.projSearch.cursor--
            }
            return m, nil
        case "down", "j":
            if results := m.projSearchResults(); m.projSearch.cursor < len(results)-1 {
                m.projSearch.cursor++
            }
            return m, nil
        case "esc":
            m.mode = modeNormal
            m.projSearch = searchState{}
            return m, nil
        }
    }
    oldQuery := m.projSearch.query
    m.projSearchInput, cmd = m.projSearchInput.Update(msg)
    m.projSearch.query = m.projSearchInput.Value()
    if m.projSearch.query != oldQuery {
        m.projSearch.cursor = 0
    }
    return m, cmd
}

// ── Confirm-delete handlers ───────────────────────────────────────────────────

func (m model) updateConfirmDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            var list []todo.Todo
            if m.showHistory {
                list = m.cache.done
            } else {
                list = m.cache.active
            }
            if m.pendingDelete < len(list) {
                id := list[m.pendingDelete].ID
                m.pushUndo("delete task")
                for i, t := range m.todos {
                    if t.ID == id {
                        m.todos = append(m.todos[:i], m.todos[i+1:]...)
                        break
                    }
                }
            }
            m.markModifiedNoUndo()
            var newLen int
            if m.showHistory {
                newLen = len(m.cache.done)
            } else {
                newLen = len(m.cache.active)
            }
            if m.cursor >= newLen && m.cursor > 0 {
                m.cursor--
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}

func (m model) updateConfirmDeleteComment(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            if idx := m.currentTodoIndex(); idx >= 0 {
                m.pushUndo("delete comment")
                m.todos[idx].DeleteComment(m.pendingComment)
                if m.detail.commentCursor >= len(m.todos[idx].Comments) && m.detail.commentCursor > 0 {
                    m.detail.commentCursor--
                }
                m.markModifiedNoUndo()
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}

func (m model) updateConfirmDeleteDep(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            if idx := m.currentTodoIndex(); idx >= 0 && m.pendingDep < len(m.todos[idx].Dependencies) {
                m.pushUndo("remove dependency")
                m.todos[idx].RemoveDependency(m.todos[idx].Dependencies[m.pendingDep])
                if m.detail.depCursor >= len(m.todos[idx].Dependencies) && m.detail.depCursor > 0 {
                    m.detail.depCursor--
                }
                m.markModifiedNoUndo()
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}

func (m model) updateConfirmDeleteTag(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            if idx := m.currentTodoIndex(); idx >= 0 && m.pendingTag < len(m.todos[idx].Tags) {
                m.pushUndo("remove tag")
                m.todos[idx].RemoveTag(m.todos[idx].Tags[m.pendingTag])
                if m.detail.tagCursor >= len(m.todos[idx].Tags) && m.detail.tagCursor > 0 {
                    m.detail.tagCursor--
                }
                m.markModifiedNoUndo()
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}

func (m model) updateConfirmDeleteTagGlobal(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) {
                m.pushUndo("delete tag globally")
                m.deleteTagGlobally(tags[m.tagTabCursor])
                m.markModifiedNoUndo()
                if remaining := m.getFilteredTagsForTab(); m.tagTabCursor >= len(remaining) && m.tagTabCursor > 0 {
                    m.tagTabCursor--
                }
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}

func (m model) updateConfirmDeleteProject(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            if idx := m.currentTodoIndex(); idx >= 0 {
                m.pushUndo("remove project")
                m.todos[idx].SetProject("")
                m.markModifiedNoUndo()
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}

func (m model) updateConfirmDeleteLearning(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            if m.tab == tabLearnings {
                if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
                    m.pushUndo("delete learning")
                    m.deleteLearningByID(learnings[m.learningCursor].ID)
                    m.markModifiedNoUndo()
                    if remaining := m.allLearnings(); m.learningCursor >= len(remaining) && m.learningCursor > 0 {
                        m.learningCursor--
                    }
                }
            } else if idx := m.currentTodoIndex(); idx >= 0 && m.detail.learningCursor < len(m.todos[idx].Learnings) {
                m.pushUndo("delete learning")
                m.todos[idx].DeleteLearning(m.detail.learningCursor)
                if m.detail.learningCursor >= len(m.todos[idx].Learnings) && m.detail.learningCursor > 0 {
                    m.detail.learningCursor--
                }
                m.markModifiedNoUndo()
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}

func (m model) updateConfirmDeleteSubtask(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyMsg); ok {
        switch key.String() {
        case "y":
            if idx := m.currentTodoIndex(); idx >= 0 && m.pendingSubtask < len(m.todos[idx].SubtaskIDs) {
                m.pushUndo("delete subtask")
                m.deleteSubtask(idx, m.pendingSubtask)
                if m.detail.subtaskCursor >= len(m.todos[idx].SubtaskIDs) && m.detail.subtaskCursor > 0 {
                    m.detail.subtaskCursor--
                }
                m.markModifiedNoUndo()
            }
            m.mode = modeNormal
        case "n", "esc":
            m.mode = modeNormal
        }
    }
    return m, nil
}
