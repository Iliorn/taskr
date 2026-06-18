package main

import (
	"fmt"
	"os"
	"path/filepath"
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

// Rows in the Settings tab. Bias rows come first because they're the
// sequencing engine's only user-visible knob; cosmetic rows (theme, language)
// sit below to keep the visual layout consistent.
const (
	settingBiasDeadline = iota
	settingBiasPriority
	settingBiasMomentum
	settingTheme
	settingLanguage
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
	fieldSize
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
	modeEditSubtask
)

type tagSortMode int

const (
	tagSortAlpha tagSortMode = iota
	tagSortCount
)

// untaggedKey is a sentinel used both as the Tags-tab virtual row for tasks
// with no tags and as the Tasks-tab search token that filters to them. The
// NUL prefix guarantees it can never collide with a real (normalized) tag.
const untaggedKey = "\x00untagged"

// statsRangeMode selects the window shown by the stats activity histogram,
// cycled with Enter on the Stats tab.
type statsRangeMode int

const (
	statsRange7Days statsRangeMode = iota
	statsRange30Days
	statsRange6Months
	statsRangeCount
)

type taskSortMode int

// Three sort modes survive the sequencing engine: Sequence (the score-based
// default), DueDate (strict deadline view), and Size (Small → Medium → Large
// for "show me the quick wins"). Each mode lines up with a visible column so
// the >..< header marker is always meaningful.
const (
	taskSortSequence taskSortMode = iota
	taskSortDueDate
	taskSortSize
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
	Store  // embedded source of truth (tasks map, indexes, undo) — promotes m.tasks, m.add, m.pushUndo, etc.
	repo   Repository
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
	statsRange          statsRangeMode
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

	// Caches
	cache *cacheState

	// Filesystem watcher state. nil if the watcher couldn't start (in which
	// case the TUI behaves exactly as before — no live reload, no errors).
	watcher *watcherState
}

func initialModel(repo Repository) model {
	ti := textinput.New()
	ti.CharLimit = 500

	si := textinput.New()
	si.CharLimit = 100

	di := textinput.New()
	di.CharLimit = 100

	tagi := textinput.New()
	tagi.CharLimit = 50

	proji := textinput.New()
	proji.CharLimit = 100

	tagTabSearch := textinput.New()
	tagTabSearch.CharLimit = 50

	learningSearch := textinput.New()
	learningSearch.CharLimit = 100

	todos, err := repo.Load()
	errMsg := ""
	if err != nil {
		errMsg = fmt.Sprintf("Error loading tasks: %v", err)
	}

	settings, settingsErr := loadSettings()
	if settingsErr != nil {
		// Settings load failed in a way that *isn't* "file doesn't exist".
		// Don't silently reset the user's preferences — surface the cause
		// so they know what to fix. If a task-load error is also pending,
		// keep that one (it's the more user-blocking failure).
		if errMsg == "" {
			errMsg = fmt.Sprintf("Settings load failed (using defaults): %v", settingsErr)
		}
	}
	th := themeByName(settings.Theme)
	applyTheme(th)
	applyLang(settings.Language)
	applyBiases(biasesFromSettings(settings))

	store := Store{}
	store.ensureTasks()
	for i := range todos {
		store.add(todos[i])
	}
	m := model{
		Store:               store,
		repo:                repo,
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
		cache: &cacheState{
			dirty:        true,
			overdueSet:   make(map[string]bool),
			tagRender:    make(map[string]string, 32),
			projectTasks: make(map[string][]todo.Todo),
		},
	}
	m.applyLangPlaceholders()
	m.refreshCaches()
	// Absorb Age drift since the last open: every task's score creeps daily,
	// so a startup resync keeps the persisted column truthful even when the
	// user hasn't touched any task since yesterday.
	if err := m.repo.ResyncScores(); err != nil {
		m.err = fmt.Sprintf("Score resync failed: %v", err)
	}
	// Spin up the filesystem watcher so CLI writes (and any other process
	// touching ~/.taskr/tasks.db) refresh the TUI without a restart. If it
	// fails to start (e.g. weird filesystem, permissions, OS limits), the
	// TUI keeps working — live reload just isn't available.
	if home, err := os.UserHomeDir(); err == nil {
		watchDir := filepath.Join(home, ".taskr")
		state := newWatcherState()
		if _, werr := startWatcher(state, watchDir); werr == nil {
			m.watcher = state
		}
	}
	m.calendar.selected = startOfDay(time.Now())
	if t := m.runningTask(); t != nil {
		m.timerTickOn = true
		if e := t.RunningEntry(); e != nil && time.Since(e.StartedAt) > idleThreshold {
			m.openIdlePrompt(t)
		}
	}
	return m
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.timerTickOn {
		cmds = append(cmds, timerTick())
	}
	if m.watcher != nil {
		cmds = append(cmds, waitForDBChange(m.watcher.ch))
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
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

// copyTodo deep-copies the nested slices of a single task so the result can be
// safely mutated (or read from another goroutine) without affecting the source.
func copyTodo(t todo.Todo) todo.Todo {
	cp := t
	if len(t.Tags) > 0 {
		cp.Tags = append([]string{}, t.Tags...)
	}
	if len(t.Dependencies) > 0 {
		cp.Dependencies = append([]string{}, t.Dependencies...)
	}
	if len(t.Comments) > 0 {
		cp.Comments = append([]todo.Comment{}, t.Comments...)
	}
	if len(t.Learnings) > 0 {
		cp.Learnings = append([]todo.Learning{}, t.Learnings...)
	}
	if len(t.TimeEntries) > 0 {
		cp.TimeEntries = append([]todo.TimeEntry{}, t.TimeEntries...)
	}
	return cp
}

// copyTodos creates an independent copy of a task slice. Used by the undo stack
// (still snapshot-based until step 8 swaps it for a patch log).
func copyTodos(todos []todo.Todo) []todo.Todo {
	cp := make([]todo.Todo, len(todos))
	for i := range todos {
		cp[i] = copyTodo(todos[i])
	}
	return cp
}

// ── Model mutations ───────────────────────────────────────────────────────────

// markModified records a mutation: pushes undo, marks the named IDs dirty for
// the next save, and flags derived caches as stale. The cache is refreshed
// lazily on the next read (via ensureCache), so several mutations within one
// Update only pay for one refresh instead of one per mutation. With no IDs,
// falls back to marking every task dirty — used by mass operations not yet
// refactored to return touched IDs.
func (m *model) markModified(ids ...string) {
	taskID := m.currentTaskID()
	m.pushUndo("modify", ids...)
	if len(ids) == 0 {
		m.markAllDirty()
	} else {
		m.markDirty(ids...)
	}
	m.dirty = true
	m.cache.dirty = true
	m.invalidateDetailCache()
	m.ensureCache()
	m.followTask(taskID)
}

func (m *model) markModifiedNoUndo(ids ...string) {
	taskID := m.currentTaskID()
	if len(ids) == 0 {
		m.markAllDirty()
	} else {
		m.markDirty(ids...)
	}
	m.dirty = true
	m.cache.dirty = true
	m.invalidateDetailCache()
	m.ensureCache()
	m.followTask(taskID)
}

func (m *model) markCacheDirty() {
	m.cache.dirty = true
	m.invalidateDetailCache()
}

func (m *model) currentTaskID() string {
	if m.pane != paneDetail || m.tab != tabTasks {
		return ""
	}
	if t := m.currentTodo(); t != nil {
		return t.ID
	}
	return ""
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

// runningTask returns the task with the active timer, or nil. Reads from the
// maintained runningTimers index — O(1) instead of a full map scan.
func (m model) runningTask() *todo.Todo {
	for id := range m.runningTimers {
		if t := m.get(id); t != nil {
			return t
		}
	}
	return nil
}

func (m model) anyTimerRunning() bool {
	return len(m.runningTimers) > 0
}

// openIdlePrompt switches to the runaway-timer prompt for the task's
// running entry.
func (m *model) openIdlePrompt(t *todo.Todo) {
	if t == nil {
		return
	}
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

// toggleTimer stops t's timer if running, otherwise stops any other running
// timer (only one task is tracked at a time) and starts this one. The Store's
// runningTimers index is maintained by Store.startTimer / stopTimer.
func (m *model) toggleTimer(t *todo.Todo) {
	if t == nil {
		return
	}
	if t.IsTimerRunning() {
		m.stopTimer(t.ID)
		return
	}
	for otherID := range m.runningTimers {
		if otherID != t.ID {
			m.stopTimer(otherID)
		}
	}
	m.startTimer(t.ID)
}

// ── Lookup helpers ────────────────────────────────────────────────────────────

func (m model) findTodoByID(id string) *todo.Todo {
	return m.get(id)
}

// currentTodo returns the *Todo at the current cursor position in whichever
// task list the user is viewing (active / done / project-tasks), or nil if the
// cursor is past the end of that list.
func (m model) currentTodo() *todo.Todo {
	switch m.tab {
	case tabTasks:
		var list []todo.Todo
		if m.showHistory {
			list = m.cache.done
		} else {
			list = m.cache.active
		}
		if m.cursor < len(list) {
			return m.get(list[m.cursor].ID)
		}
	case tabProjects:
		if m.projectTaskMode {
			projects := m.cache.projects
			if m.projectCursor < len(projects) {
				tasks := m.getProjectTasks(projects[m.projectCursor])
				if m.cursor < len(tasks) {
					return m.get(tasks[m.cursor].ID)
				}
			}
		}
	}
	return nil
}

// ── Learnings helpers ─────────────────────────────────────────────────────────

func (m model) findLearningSource(learningID string) *todo.Todo {
	for _, t := range m.tasks {
		for _, l := range t.Learnings {
			if l.ID == learningID {
				return t
			}
		}
	}
	return nil
}

// deleteLearningByID removes the named learning from its parent task and
// returns the parent's ID (or "" if not found) so callers can mark it dirty.
func (m *model) deleteLearningByID(learningID string) string {
	for _, t := range m.tasks {
		for j, l := range t.Learnings {
			if l.ID == learningID {
				t.DeleteLearning(j)
				return t.ID
			}
		}
	}
	return ""
}

// updateLearningByID rewrites a learning's text and returns its parent's ID.
func (m *model) updateLearningByID(learningID, newText string) string {
	for _, t := range m.tasks {
		for j, l := range t.Learnings {
			if l.ID == learningID {
				t.UpdateLearning(j, newText)
				return t.ID
			}
		}
	}
	return ""
}

// ── Subtask helpers ───────────────────────────────────────────────────────────

// subtaskIDs returns parentID's child task IDs in CreatedAt order, read
// directly from the maintained subtaskOf index — O(1) lookup, no rebuild.
func (m model) subtaskIDs(parentID string) []string {
	return m.subtaskOf[parentID]
}

// subtaskCount returns how many subtasks parentID has, via the maintained
// subtaskOf index.
func (m model) subtaskCount(parentID string) int {
	return len(m.subtaskOf[parentID])
}

// addSubtask creates a child of parentID and returns the new subtask's ID so
// the caller can mark it dirty.
func (m *model) addSubtask(parentID, title string) string {
	sub := todo.NewSubtask(title, parentID)
	sub.InheritContextFrom(m.get(parentID))
	m.add(sub)
	return sub.ID
}

// deleteSubtask removes the child task at subtaskCursor under parentID and
// returns the deleted subtask's ID so the caller can record it as a tombstone.
func (m *model) deleteSubtask(parentID string, subtaskCursor int) string {
	ids := m.subtaskIDs(parentID)
	if subtaskCursor >= len(ids) {
		return ""
	}
	subID := ids[subtaskCursor]
	m.remove(subID)
	return subID
}

// toggleSubtask flips a child task's status and returns the toggled task's ID
// so the caller can mark it dirty.
func (m *model) toggleSubtask(parentID string, subtaskCursor int) string {
	ids := m.subtaskIDs(parentID)
	if subtaskCursor >= len(ids) {
		return ""
	}
	subID := ids[subtaskCursor]
	if t := m.get(subID); t != nil {
		t.Toggle()
		return subID
	}
	return ""
}

// ── Search/filter helpers ─────────────────────────────────────────────────────

func (m model) matchesSearch(t todo.Todo) bool {
	return todoMatchesSearch(t, m.searchQuery)
}

func (m model) matchesFocusFilter(t todo.Todo) bool {
	return todoMatchesFocus(t, m.focusFilter)
}

func (m model) depSearchResults() []todo.Todo {
	t := m.currentTodo()
	q := strings.ToLower(m.depSearch.query)
	result := make([]todo.Todo, 0, maxDepSearchResults*2)
	for _, candidate := range m.tasks {
		if t != nil && candidate.ID == t.ID {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(candidate.Title), q) {
			result = append(result, *candidate)
		}
	}
	// Range over a map gives unstable order, so the list visibly reshuffled on
	// every redraw (cursor blink, timer tick, anything). Sort alphabetically so
	// the picker is stable across redraws and the cursor points at the same
	// task between frames.
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Title) < strings.ToLower(result[j].Title)
	})
	if len(result) > maxDepSearchResults*3 {
		result = result[:maxDepSearchResults*3]
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
	for _, t := range m.tasks {
		for _, tag := range t.Tags {
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
	q := strings.ToLower(m.tagTabSearchQuery)

	result := all
	if q != "" {
		result = make([]string, 0, len(all))
		for _, tag := range all {
			if strings.Contains(strings.ToLower(tag), q) {
				result = append(result, tag)
			}
		}
	}

	// Surface a virtual "(untagged)" row at the top so tasks with no tags are
	// reachable for triage. Only when such tasks exist and the row matches the
	// filter text.
	if m.cache.untaggedTotal > 0 && (q == "" || strings.Contains("untagged", q)) {
		return append([]string{untaggedKey}, result...)
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

// ── Global mutations ──────────────────────────────────────────────────────────

// renameTagGlobally rewrites every occurrence of oldName to newName and
// returns the IDs of the tasks it touched, for dirty marking.
func (m *model) renameTagGlobally(oldName, newName string) []string {
	newName = todo.NormalizeTag(newName)
	if newName == "" || newName == oldName {
		return nil
	}
	var touched []string
	for _, t := range m.tasks {
		has := false
		for _, tag := range t.Tags {
			if tag == oldName {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		// RemoveTag + AddTag merges into an existing tag (no duplicates) and
		// normalizes, rather than blindly overwriting in place.
		t.RemoveTag(oldName)
		t.AddTag(newName)
		touched = append(touched, t.ID)
	}
	return touched
}

func (m *model) deleteTagGlobally(tagName string) []string {
	var touched []string
	for _, t := range m.tasks {
		hadTag := false
		tags := t.Tags[:0]
		for _, tag := range t.Tags {
			if tag == tagName {
				hadTag = true
				continue
			}
			tags = append(tags, tag)
		}
		t.Tags = tags
		if hadTag {
			touched = append(touched, t.ID)
		}
	}
	return touched
}

func (m *model) renameProjectGlobally(oldName, newName string) []string {
	var touched []string
	for _, t := range m.tasks {
		if t.Project == oldName {
			t.Project = newName
			touched = append(touched, t.ID)
		}
	}
	return touched
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
		case fieldSize:
			return line + 3
		case fieldProject:
			return line + 4
		case fieldNotes:
			return line + 5
		default: // fieldTags
			line += 8 // start, due, priority, size, project, notes, created, modified
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
			if m.subtaskCount(t.ID) == 0 {
				line++
			} else {
				line += m.subtaskCount(t.ID)
			}
			line += 2 // blank + deps label
			return line + m.detail.depCursor
		default: // fieldLearnings
			if m.subtaskCount(t.ID) == 0 {
				line++
			} else {
				line += m.subtaskCount(t.ID)
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
	lines := 11 // 10 fixed + 1 for the Size row added with the sequencing engine
	if t.Status == todo.Pending {
		lines++ // Score breakdown row, rendered only for pending tasks
	}
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
	if m.subtaskCount(t.ID) == 0 {
		lines += 2
	} else {
		lines += 1 + m.subtaskCount(t.ID)
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
		modeEditSubtask, modeEditProjectInline, modeEditTimeEntry:
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
