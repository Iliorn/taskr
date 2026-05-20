package main

import (
    "fmt"
    "sort"
    "strings"
    "time"

    "github.com/charmbracelet/bubbles/textinput"
    tea "github.com/charmbracelet/bubbletea"
    "taskr/todo"
)

// ── Types & constants ─────────────────────────────────────────────────────────

type tab int

const (
    tabTasks tab = iota
    tabProjects
    tabTags
    tabLearnings
    tabStats
)

type pane int

const (
    paneList pane = iota
    paneDetail
)

type detailField int

const (
    fieldStartDate detailField = iota
    fieldDueDate
    fieldPriority
    fieldProject
    fieldNotes
    fieldTags
    fieldDependencies
    fieldLearnings
    fieldSubtasks
)

type appMode int

const (
    modeNormal appMode = iota
    modeHelp
    modeInput
    modeSearch
    modeSearchDep
    modeSearchTag
    modeSearchProject
    modeSearchTagTab
    modeConfirmDelete
    modeConfirmDeleteComment
    modeConfirmDeleteDep
    modeConfirmDeleteTag
    modeConfirmDeleteTagGlobal
    modeConfirmDeleteProject
    modeConfirmDeleteLearning
    modeConfirmDeleteSubtask
    modeEditComment
    modeEditTag
    modeEditProjectInline
    modeEditTitle
    modeEditLearning
    modeAddLearning
    modeAddSubtask
)

type tagSortMode int

const (
    tagSortAlpha tagSortMode = iota
    tagSortCount
)

type taskSortMode int

const (
    taskSortDueDate taskSortMode = iota
    taskSortPriority
    taskSortCreated
)

type learningSortMode int

const (
    learningSortDate learningSortMode = iota
    learningSortAlpha
)

// ── Messages ──────────────────────────────────────────────────────────────────

type clearErrMsg struct{}
type saveDoneMsg struct{}
type saveErrMsg struct{ err error }
type editorFinishedMsg struct{ taskID string }

// ── Model ─────────────────────────────────────────────────────────────────────

type model struct {
    todos                []todo.Todo
    cursor               int
    tab                  tab
    pane                 pane
    detailField          detailField
    detailPage           int
    commentCursor        int
    depCursor            int
    tagCursor            int
    tagTabCursor         int
    searchCursor         int
    learningCursor       int
    learningDetailCursor int
    subtaskCursor        int
    pendingLearning      int
    pendingSubtask       int
    mode                 appMode
    textInput            textinput.Model
    searchInput          textinput.Model
    depSearchInput       textinput.Model
    tagSearchInput       textinput.Model
    projSearchInput      textinput.Model
    tagTabSearchInput    textinput.Model
    learningSearchInput  textinput.Model
    confirmMsg           string
    pendingDelete        int
    pendingComment       int
    pendingDep           int
    pendingTag           int
    termWidth            int
    termHeight           int
    err                  string
    projectCursor        int
    searchQuery          string
    listOffset           int
    depSearchQuery       string
    tagSearchQuery       string
    projSearchQuery      string
    tagTabSearchQuery    string
    learningSearchQuery  string
    projectTaskMode      bool
    showHistory          bool
    focusFilter          bool
    expandedTasks        map[string]bool
    editingTagName       string
    editingProjectName   string
    tagSort              tagSortMode
    taskSort             taskSortMode
    learningSort         learningSortMode
    dirty                bool
    editorTaskID         string

    // ── Undo stack ─────────────────────────────────────────────────────────
    undoStack []undoEntry

    // ── Caches (rebuilt via refreshCaches) ─────────────────────────────────
    cacheDirty   bool
    todoIndex    map[string]int
    overdueSet   map[string]bool
    cachedActive []todo.Todo
    cachedDone   []todo.Todo
    cachedTags   map[string]tagStats
}

func initialModel() model {
    ti := textinput.New()
    ti.CharLimit = 500

    si := textinput.New()
    si.Placeholder = "Search... (use # to filter by tag)"
    si.CharLimit = 100

    di := textinput.New()
    di.Placeholder = "Search for task to add as dependency..."
    di.CharLimit = 100

    tagi := textinput.New()
    tagi.Placeholder = "Search or create tag..."
    tagi.CharLimit = 50

    proji := textinput.New()
    proji.Placeholder = "Search or create project..."
    proji.CharLimit = 100

    tagTabSearch := textinput.New()
    tagTabSearch.Placeholder = "Filter tags..."
    tagTabSearch.CharLimit = 50

    learningSearch := textinput.New()
    learningSearch.Placeholder = "Search learnings... (use # to filter by tag)"
    learningSearch.CharLimit = 100

    todos, err := loadTodos()
    errMsg := ""
    if err != nil {
        errMsg = fmt.Sprintf("Error loading tasks: %v", err)
    }

    m := model{
        todos:               todos,
        textInput:           ti,
        searchInput:         si,
        depSearchInput:      di,
        tagSearchInput:      tagi,
        projSearchInput:     proji,
        tagTabSearchInput:   tagTabSearch,
        learningSearchInput: learningSearch,
        mode:                modeNormal,
        pane:                paneList,
        tab:                 tabTasks,
        termWidth:           80,
        termHeight:          24,
        err:                 errMsg,
        tagSort:             tagSortAlpha,
        taskSort:            taskSortDueDate,
        learningSort:        learningSortDate,
        expandedTasks:       make(map[string]bool),
        cacheDirty:          true,
    }
    m.refreshCaches()
    return m
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
    return nil
}

// ── Error timer ───────────────────────────────────────────────────────────────

func clearErrAfter() tea.Cmd {
    return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
        return clearErrMsg{}
    })
}

// ── Undo ──────────────────────────────────────────────────────────────────────

func (m *model) pushUndo(desc string) {
    snapshot := deepCopyTodos(m.todos)
    m.undoStack = append(m.undoStack, undoEntry{todos: snapshot, desc: desc})
    if len(m.undoStack) > maxUndoStack {
        m.undoStack = m.undoStack[1:]
    }
}

func (m *model) popUndo() (undoEntry, bool) {
    if len(m.undoStack) == 0 {
        return undoEntry{}, false
    }
    entry := m.undoStack[len(m.undoStack)-1]
    m.undoStack = m.undoStack[:len(m.undoStack)-1]
    return entry, true
}

// ── Learnings helpers ─────────────────────────────────────────────────────────

func (m model) allLearnings() []todo.Learning {
    var result []todo.Learning
    for i := range m.todos {
        result = append(result, m.todos[i].Learnings...)
    }
    if m.learningSearchQuery != "" {
        filtered := result[:0]
        q := strings.ToLower(m.learningSearchQuery)
        isTagSearch := strings.HasPrefix(q, "#")
        tagQuery := strings.TrimPrefix(q, "#")
        for _, l := range result {
            if isTagSearch {
                for _, tag := range l.Tags {
                    if strings.Contains(strings.ToLower(tag), tagQuery) {
                        filtered = append(filtered, l)
                        break
                    }
                }
            } else {
                if strings.Contains(strings.ToLower(l.Text), q) {
                    filtered = append(filtered, l)
                }
            }
        }
        result = filtered
    }
    switch m.learningSort {
    case learningSortAlpha:
        sort.SliceStable(result, func(i, j int) bool {
            return strings.ToLower(result[i].Text) < strings.ToLower(result[j].Text)
        })
    default:
        sort.SliceStable(result, func(i, j int) bool {
            return result[i].CreatedAt.After(result[j].CreatedAt)
        })
    }
    return result
}

func (m model) findLearningSource(learningID string) *todo.Todo {
    for i := range m.todos {
        for _, l := range m.todos[i].Learnings {
            if l.ID == learningID {
                return &m.todos[i]
            }
        }
    }
    return nil
}

func (m *model) deleteLearningByID(learningID string) {
    for i := range m.todos {
        for j, l := range m.todos[i].Learnings {
            if l.ID == learningID {
                m.todos[i].DeleteLearning(j)
                return
            }
        }
    }
}

func (m *model) updateLearningByID(learningID, newText string) {
    for i := range m.todos {
        for j, l := range m.todos[i].Learnings {
            if l.ID == learningID {
                m.todos[i].UpdateLearning(j, newText)
                return
            }
        }
    }
}

// ── Subtask helpers ───────────────────────────────────────────────────────────

func (m model) getSubtasks(t *todo.Todo) []*todo.Todo {
    if t == nil || len(t.SubtaskIDs) == 0 {
        return nil
    }
    result := make([]*todo.Todo, 0, len(t.SubtaskIDs))
    for _, id := range t.SubtaskIDs {
        if sub := m.findTodoByID(id); sub != nil {
            result = append(result, sub)
        }
    }
    return result
}

func (m *model) addSubtask(parentIdx int, title string) {
    sub := todo.NewSubtask(title, m.todos[parentIdx].ID)
    m.todos = append(m.todos, sub)
    m.todos[parentIdx].AddSubtaskID(sub.ID)
}

func (m *model) deleteSubtask(parentIdx int, subtaskCursor int) {
    if subtaskCursor >= len(m.todos[parentIdx].SubtaskIDs) {
        return
    }
    subID := m.todos[parentIdx].SubtaskIDs[subtaskCursor]
    m.todos[parentIdx].RemoveSubtaskID(subID)
    for i, t := range m.todos {
        if t.ID == subID {
            m.todos = append(m.todos[:i], m.todos[i+1:]...)
            break
        }
    }
}

func (m *model) toggleSubtask(parentIdx int, subtaskCursor int) {
    if subtaskCursor >= len(m.todos[parentIdx].SubtaskIDs) {
        return
    }
    subID := m.todos[parentIdx].SubtaskIDs[subtaskCursor]
    for i := range m.todos {
        if m.todos[i].ID == subID {
            m.todos[i].Toggle()
            return
        }
    }
}
