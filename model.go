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
	tabCalendar
	tabProjects
	tabTags
	tabLearnings
	tabStats
	tabSettings
)

const numTabs = 7

// Rows in the Settings tab.
const (
	settingTheme = iota
	settingVersion
	settingCheckUpdate
	numSettingsRows
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
	modeConfirmDeleteTimeEntry
	modeConfirmUpdate
	modeEditTimeEntry
	modeIdlePrompt
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
	taskSortStartDate
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
type editorFinishedMsg struct {
	taskID   string
	err      error
	fallback bool // true when this run already used the notepad fallback
}
type saveTickMsg struct{}
type updateDoneMsg struct{ err error }
type updateCheckMsg struct {
	latest string
	err    error
}
type timerTickMsg struct{}

// ── Sub-state structs ─────────────────────────────────────────────────────────

type searchState struct {
	query  string
	cursor int
}

type detailState struct {
	field          detailField
	page           int
	commentCursor  int
	depCursor      int
	tagCursor      int
	learningCursor int
	subtaskCursor  int
}

type calendarState struct {
	selected      time.Time // selected day, normalized to midnight
	entryCursor   int
	focusTimeline bool
}

// ── Model ─────────────────────────────────────────────────────────────────────

type model struct {
	todos  []todo.Todo
	cursor int
	tab    tab
	pane   pane
	mode   appMode

	// View metrics cache
	metrics viewMetrics

	// detail render cache
	detailRC detailRenderCache

	// Detail pane state
	detail detailState

	// Calendar tab state
	calendar    calendarState
	timerTickOn bool

	// Search state per context
	depSearch  searchState
	tagSearch  searchState
	projSearch searchState

	// Inputs
	textInput           textinput.Model
	searchInput         textinput.Model
	depSearchInput      textinput.Model
	tagSearchInput      textinput.Model
	projSearchInput     textinput.Model
	tagTabSearchInput   textinput.Model
	learningSearchInput textinput.Model

	// UI state
	confirmMsg          string
	pendingDelete       int
	pendingComment      int
	pendingDep          int
	pendingTag          int
	pendingLearning     int
	pendingSubtask      int
	pendingEntryTaskID  string
	pendingEntryID      string
	termWidth           int
	termHeight          int
	err                 string
	projectCursor       int
	tagTabCursor        int
	learningCursor      int
	settingsCursor      int
	searchQuery         string
	tagTabSearchQuery   string
	learningSearchQuery string
	listOffset          int
	projectTaskMode     bool
	showHistory         bool
	focusFilter         bool
	expandedTasks       map[string]bool
	editingTagName      string
	editingProjectName  string
	tagSort             tagSortMode
	taskSort            taskSortMode
	learningSort        learningSortMode
	themeName           string
	updateStatus        string
	searchCursor        int

	// Persistence
	dirty         bool
	savePending   bool
	saveScheduled bool
	editorTaskID  string
	editorCmd     string

	// Frame
	frameTime time.Time

	// Gantt reusable buffers
	ganttBarBuf   []rune
	ganttColorBuf []int

	// Undo
	undoStack []undoEntry

	// Caches
	cache cacheState
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

	settings := loadSettings()
	th := themeByName(settings.Theme)
	applyTheme(th)

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
		tagSort:             settings.TagSort,
		taskSort:            settings.TaskSort,
		learningSort:        settings.LearningSort,
		themeName:           th.name,
		expandedTasks:       make(map[string]bool),
		editorCmd:           resolveEditorCmd(),
		frameTime:           time.Now(),
		ganttBarBuf:         make([]rune, 256),
		ganttColorBuf:       make([]int, 256),
		cache: cacheState{
			dirty:        true,
			todoIndex:    make(map[string]int),
			overdueSet:   make(map[string]bool),
			tagRender:    make(map[string]string, 32),
			subtaskIndex: make(map[string][]int),
			projectTasks: make(map[string][]todo.Todo),
		},
	}
	m.refreshCaches()
	m.calendar.selected = startOfDay(time.Now())
	m.timerTickOn = m.runningTodoIndex() >= 0
	if idx := m.runningTodoIndex(); idx >= 0 {
		if e := m.todos[idx].RunningEntry(); e != nil && time.Since(e.StartedAt) > idleThreshold {
			m.openIdlePrompt(idx)
		}
	}
	return m
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	if m.timerTickOn {
		return timerTick()
	}
	return nil
}

// ── Error timer ───────────────────────────────────────────────────────────────

func clearErrAfter() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearErrMsg{}
	})
}

// ── Timer tick ────────────────────────────────────────────────────────────────

// A timer running longer than this is assumed forgotten and triggers the
// idle prompt (on startup and when stopping it).
const idleThreshold = 4 * time.Hour

func timerTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return timerTickMsg{}
	})
}

// ── Debounced save ────────────────────────────────────────────────────────────

const saveDebounceDuration = 300 * time.Millisecond

func scheduleSave() tea.Cmd {
	return tea.Tick(saveDebounceDuration, func(t time.Time) tea.Msg {
		return saveTickMsg{}
	})
}

// ── Undo ──────────────────────────────────────────────────────────────────────

const maxUndoStack = 20

type undoEntry struct {
	todos []todo.Todo
	desc  string
}

func (m *model) pushUndo(desc string) {
	snapshot := copyTodos(m.todos)
	m.undoStack = append(m.undoStack, undoEntry{todos: snapshot, desc: desc})
	if len(m.undoStack) > maxUndoStack {
		copy(m.undoStack, m.undoStack[1:])
		m.undoStack = m.undoStack[:maxUndoStack]
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

// copyTodos creates an independent copy. Tasks without slices share memory
// (safe because undo restores the whole slice, never mutates individual tasks from a snapshot).
func copyTodos(todos []todo.Todo) []todo.Todo {
	cp := make([]todo.Todo, len(todos))
	for i, t := range todos {
		cp[i] = t
		if len(t.Tags) > 0 {
			cp[i].Tags = append([]string{}, t.Tags...)
		}
		if len(t.Dependencies) > 0 {
			cp[i].Dependencies = append([]string{}, t.Dependencies...)
		}
		if len(t.Comments) > 0 {
			cp[i].Comments = append([]todo.Comment{}, t.Comments...)
		}
		if len(t.Learnings) > 0 {
			cp[i].Learnings = make([]todo.Learning, len(t.Learnings))
			for j, l := range t.Learnings {
				cp[i].Learnings[j] = l
				if len(l.Tags) > 0 {
					cp[i].Learnings[j].Tags = append([]string{}, l.Tags...)
				}
			}
		}
		if len(t.TimeEntries) > 0 {
			cp[i].TimeEntries = append([]todo.TimeEntry{}, t.TimeEntries...)
		}
		if len(t.SubtaskIDs) > 0 {
			cp[i].SubtaskIDs = append([]string{}, t.SubtaskIDs...)
		}
	}
	return cp
}

// ── Model mutations ───────────────────────────────────────────────────────────

func (m *model) markModified() {
	taskID := m.currentTaskID()
	m.pushUndo("modify")
	m.dirty = true
	m.cache.dirty = true
	m.invalidateDetailCache()
	m.refreshCaches()
	m.followTask(taskID)
}

func (m *model) markModifiedNoUndo() {
	taskID := m.currentTaskID()
	m.dirty = true
	m.cache.dirty = true
	m.invalidateDetailCache()
	m.refreshCaches()
	m.followTask(taskID)
}

func (m *model) markCacheDirty() {
	m.cache.dirty = true
	m.invalidateDetailCache()
	m.refreshCaches()
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
		list = m.cache.done
	} else {
		list = m.cache.active
	}
	for i, t := range list {
		if t.ID == taskID {
			m.cursor = i
			return
		}
	}
}

// ── Time tracking helpers ─────────────────────────────────────────────────────

func (m model) runningTodoIndex() int {
	for i := range m.todos {
		if m.todos[i].IsTimerRunning() {
			return i
		}
	}
	return -1
}

func (m model) anyTimerRunning() bool {
	return m.runningTodoIndex() >= 0
}

// openIdlePrompt switches to the runaway-timer prompt for the task's
// running entry.
func (m *model) openIdlePrompt(idx int) {
	t := &m.todos[idx]
	e := t.RunningEntry()
	if e == nil {
		return
	}
	m.pendingEntryTaskID = t.ID
	m.pendingEntryID = e.ID
	m.mode = modeIdlePrompt
	m.confirmMsg = fmt.Sprintf("◉ '%s' tracking for %s — [k]eep · [s]top · [e]dit · [d]iscard",
		truncate(t.Title, 30), formatDuration(time.Since(e.StartedAt)))
}

// toggleTimer stops the task's timer if running, otherwise stops any other
// running timer (only one task is tracked at a time) and starts this one.
func (m *model) toggleTimer(idx int) {
	if m.todos[idx].IsTimerRunning() {
		m.todos[idx].StopTimer()
		return
	}
	for i := range m.todos {
		if m.todos[i].IsTimerRunning() {
			m.todos[i].StopTimer()
		}
	}
	m.todos[idx].StartTimer()
}

// ── Lookup helpers ────────────────────────────────────────────────────────────

func (m model) findTodoByID(id string) *todo.Todo {
	if idx, ok := m.cache.todoIndex[id]; ok && idx < len(m.todos) {
		return &m.todos[idx]
	}
	return nil
}

func (m model) currentTodoIndex() int {
	findByID := func(id string) int {
		if idx, ok := m.cache.todoIndex[id]; ok {
			return idx
		}
		return -1
	}
	switch m.tab {
	case tabTasks:
		var list []todo.Todo
		if m.showHistory {
			list = m.cache.done
		} else {
			list = m.cache.active
		}
		if m.cursor < len(list) {
			return findByID(list[m.cursor].ID)
		}
	case tabProjects:
		if m.projectTaskMode {
			projects := m.cache.projects
			if m.projectCursor < len(projects) {
				tasks := m.getProjectTasks(projects[m.projectCursor])
				if m.cursor < len(tasks) {
					return findByID(tasks[m.cursor].ID)
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

// ── Learnings helpers ─────────────────────────────────────────────────────────

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

// ── Search/filter helpers ─────────────────────────────────────────────────────

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

func (m model) depSearchResults() []todo.Todo {
	t := m.currentTodo()
	q := strings.ToLower(m.depSearch.query)
	result := make([]todo.Todo, 0, maxDepSearchResults*2)
	for _, candidate := range m.todos {
		if t != nil && candidate.ID == t.ID {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(candidate.Title), q) {
			result = append(result, candidate)
			if len(result) >= maxDepSearchResults*3 {
				break
			}
		}
	}
	return result
}

func (m model) getAllTagsSorted() []string {
	if m.cache.tagsSorted != nil && m.cache.tagsSortMode == m.tagSort {
		return m.cache.tagsSorted
	}
	// Fallback: cache absent or stale for the current sort mode (e.g. tests
	// that mutate tagSort without a refresh). Rebuild without touching cache.
	seen := make(map[string]struct{}, 16)
	tags := make([]string, 0, 16)
	for i := range m.todos {
		for _, tag := range m.todos[i].Tags {
			if _, ok := seen[tag]; !ok {
				seen[tag] = struct{}{}
				tags = append(tags, tag)
			}
		}
	}
	sortTags(tags, m.tagSort, m.cache.tags)
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
	q := strings.ToLower(m.tagSearch.query)
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
	q := strings.ToLower(m.projSearch.query)
	result := make([]string, 0, len(m.cache.projectTasks))
	for p := range m.cache.projectTasks {
		if q == "" || strings.Contains(strings.ToLower(p), q) {
			result = append(result, p)
		}
	}
	sort.Strings(result)
	return result
}

func (m model) countTasksWithTag(tag string) int {
	return m.cache.tags[tag].total
}

// ── Global mutations ──────────────────────────────────────────────────────────

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

// ── Detail scroll estimation ──────────────────────────────────────────────────

func (m model) estimateDetailCursorLine() int {
	t := m.currentTodo()
	if t == nil {
		return 0
	}
	switch m.detail.page {
	case 0:
		line := 2 // title + blank
		switch m.detail.field {
		case fieldStartDate:
			return line
		case fieldDueDate:
			return line + 1
		case fieldPriority:
			return line + 2
		case fieldProject:
			return line + 3
		case fieldNotes:
			return line + 4
		default: // fieldTags
			line += 7 // start, due, priority, project, notes, created, modified
			if len(t.TimeEntries) > 0 {
				line++
			}
			if t.Status == todo.Done && !t.CompletedAt.IsZero() {
				line++
			}
			line += 2 // blank + tags label
			return line + m.detail.tagCursor
		}
	case 1:
		line := 3 // title + blank + subtasks label
		switch m.detail.field {
		case fieldSubtasks:
			return line + m.detail.subtaskCursor
		case fieldDependencies:
			if len(t.SubtaskIDs) == 0 {
				line++
			} else {
				line += len(t.SubtaskIDs)
			}
			line += 2 // blank + deps label
			return line + m.detail.depCursor
		default: // fieldLearnings
			if len(t.SubtaskIDs) == 0 {
				line++
			} else {
				line += len(t.SubtaskIDs)
			}
			line++
			if len(t.Dependencies) == 0 {
				line++
			} else {
				line += len(t.Dependencies)
			}
			line += 2 // blank + learnings label
			return line + m.detail.learningCursor
		}
	case 2:
		return 3 + m.detail.commentCursor // title + blank + comments label
	}
	return 0
}

// ── List offset clamping ──────────────────────────────────────────────────────

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

func (m model) listVisible() int {
	var contentH int
	switch m.detail.page {
	case 1:
		contentH = m.detailPage2ContentHeight()
	case 2:
		contentH = m.detailPage3ContentHeight()
	default:
		contentH = m.detailPage1ContentHeight()
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
	if m.anyTimerRunning() {
		fixedLines++ // live timer line above the key hints
	}
	fixedLines += m.extraOverheadLines()
	if available := m.termHeight - fixedLines - detailTotal; available >= minListHeight {
		return available
	}
	return minListHeight
}

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
	if m.anyTimerRunning() {
		headerH++ // live timer line above the key hints
	}
	detailH := 0
	if m.mode == modeNormal && m.tab != tabStats {
		detailH = 12
	}
	available := m.termHeight - headerH - footerHeight - detailH - 2
	if available < minListHeight {
		return minListHeight
	}
	return available
}

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
	if len(t.Tags) == 0 {
		lines += 2
	} else {
		lines += 1 + len(t.Tags)
	}
	if len(t.TimeEntries) > 0 {
		lines++
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
	lines := 3 // title + blank + subtasks label
	if len(t.SubtaskIDs) == 0 {
		lines += 2
	} else {
		lines += 1 + len(t.SubtaskIDs)
	}
	lines++ // blank
	if len(t.Dependencies) == 0 {
		lines += 2
	} else {
		lines += 1 + len(t.Dependencies)
	}
	lines++ // blank
	if len(t.Learnings) == 0 {
		lines += 2
	} else {
		lines += 1 + len(t.Learnings)
	}
	return lines
}

func (m model) detailPage3ContentHeight() int {
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
	case modeInput, modeEditComment, modeEditTag, modeEditTitle,
		modeSearch, modeAddLearning, modeEditLearning, modeAddSubtask,
		modeEditProjectInline, modeEditTimeEntry:
		return 3
	case modeSearchDep, modeSearchTag, modeSearchProject:
		return 8
	case modeSearchTagTab:
		return 3
	case modeConfirmDelete, modeConfirmDeleteComment,
		modeConfirmDeleteDep, modeConfirmDeleteTag,
		modeConfirmDeleteTagGlobal, modeConfirmDeleteProject,
		modeConfirmDeleteLearning, modeConfirmDeleteSubtask,
		modeConfirmDeleteTimeEntry, modeConfirmUpdate, modeIdlePrompt:
		return 1
	}
	return 0
}
