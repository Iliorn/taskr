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
    fieldTags
    fieldDependencies
    fieldLearnings
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
    modeEditComment
    modeEditTag
    modeEditProjectInline
    modeEditTitle
    modeEditLearning
    modeAddLearning
)

// tagSortMode controls how the tag list is ordered.
type tagSortMode int

const (
    tagSortAlpha tagSortMode = iota
    tagSortCount
)

// taskSortMode controls how the task list is ordered.
type taskSortMode int

const (
    taskSortDueDate taskSortMode = iota
    taskSortPriority
    taskSortCreated
)

// learningSortMode controls how the learnings list is ordered.
type learningSortMode int

const (
    learningSortDate learningSortMode = iota
    learningSortAlpha
)

// clearErrMsg is sent after a 3-second delay to clear the error banner.
type clearErrMsg struct{}

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
    pendingLearning      int
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
    editingTagName       string
    editingProjectName   string
    tagSort              tagSortMode
    taskSort             taskSortMode
    learningSort         learningSortMode
    dirty                bool
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

    return model{
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
    }
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

// ── Learnings helpers ─────────────────────────────────────────────────────────

func (m model) allLearnings() []todo.Learning {
    var result []todo.Learning
    for _, t := range m.todos {
        result = append(result, t.Learnings...)
    }
    if m.learningSearchQuery != "" {
        var filtered []todo.Learning
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
