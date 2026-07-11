package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"taskr/tasksync"
	"taskr/todo"
)

// ── Types & constants ─────────────────────────────────────────────────────────

type tab int

const (
	tabTasks tab = iota
	tabCalendar
	tabProjects
	tabTags
	tabBoard
	tabStats
	tabSettings
	tabLearnings // demoted to the last slot when the Board took tab 5; kept while learnings live on
)

const numTabs = 8

// Rows in the Settings tab. Bias rows come first because they're the
// sequencing engine's only user-visible knob; cosmetic rows (theme, language)
// sit below to keep the visual layout consistent.
const (
	settingBiasDeadline = iota
	settingBiasPriority
	settingBiasMomentum
	settingAging
	settingAutoCloseParent
	settingAutoCloseSubtasks
	settingTheme
	settingLanguage
	settingSyncAuto
	settingSyncServer
	settingSyncToken
	settingSyncNow
	settingServerOn
	settingServerListen
	settingServerToken
	settingVersion
	settingCheckUpdate
	numSettingsRows
)

type pane int

const (
	paneList pane = iota
	paneDetail
)

// tabView is the slice of UI state that several tabs share the same fields for
// (the cursor, scroll offset, open pane, and task/project search query).
// switchTab snapshots it on the way out of a tab and restores it on the way
// back, so glancing at another tab doesn't wipe your position or filter. Tab-
// private state (projectCursor, tagTabCursor, showHistory, projectTaskMode, the
// per-tab search queries, …) lives in its own fields and simply persists —
// switchTab no longer zeroes it.
type tabView struct {
	cursor     int
	listOffset int
	pane       pane
	search     string
}

type detailField int

const (
	fieldStartDate detailField = iota
	fieldDueDate
	fieldRecurrence
	fieldPriority
	fieldSize
	fieldProject
	fieldNotes
	fieldTags
	fieldDependencies
	fieldLearnings
	fieldSubtasks
	fieldTimeEntries
	fieldComments
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
	// modeConfirm is the generic yes/no prompt: confirmMsg holds the question
	// and confirmOnYes the action to run on y/enter. It replaced a dozen
	// near-identical per-action confirm modes.
	modeConfirm
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
	modeAddTimeEntry
	modeEditSyncURL
	modeEditSyncToken
	modeEditServerListen
	modeEditServerToken
)

type tagSortMode int

const (
	tagSortAlpha tagSortMode = iota
	tagSortCount
	tagSortProgress
	tagSortRecent
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

type historySortMode int

// The history (completed-tasks) list has its own sort, independent of the
// active-task taskSort: the active modes (Sequence score, Size) are
// meaningless once a task is done. History sorts by completion time (most
// recent first) or task title — each lining up with a visible history column
// so the >..< header marker stays meaningful here too.
const (
	historySortCompleted historySortMode = iota // most-recent completion first
	historySortAlpha                            // title A→Z
)

// toastKind selects the style of a transient toast (m.err). The zero value is
// toastError so any plain assignment renders as an error; success/info are set
// via flashSuccess/flashInfo. See renderStatusLine.
type toastKind int

const (
	toastError toastKind = iota
	toastSuccess
	toastInfo
)

// flashError/flashSuccess/flashInfo set the toast text and its kind together so
// the two can't drift — an error can never inherit a prior success's colour.
// clearErrAfter is still returned by the call site to expire the toast.
func (m *model) flashError(s string)   { m.err, m.errKind = s, toastError }
func (m *model) flashSuccess(s string) { m.err, m.errKind = s, toastSuccess }
func (m *model) flashInfo(s string)    { m.err, m.errKind = s, toastInfo }

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
	field           detailField
	commentCursor   int
	depCursor       int
	tagCursor       int
	learningCursor  int
	subtaskCursor   int
	timeEntryCursor int
}

type calendarState struct {
	selected      time.Time // selected day, normalized to midnight
	entryCursor   int
	focusTimeline bool
}

// boardState is the Board tab's cursor: which column is focused and which
// card within it. col indexes activeStages; col == len(activeStages) is the
// Done column. Both are clamped at render/move time, so stale values after a
// stage-list edit or task completion degrade to the nearest valid card.
type boardState struct {
	col    int
	cursor int
}

// ── Model ─────────────────────────────────────────────────────────────────────

type model struct {
	Store  // embedded source of truth (tasks map, indexes, undo) — promotes m.tasks, m.add, m.pushUndo, etc.
	repo   Repository
	cursor int
	tab    tab
	pane   pane
	mode   appMode

	// detail render cache
	detailRC detailRenderCache

	// Detail pane state
	detail detailState

	// Calendar tab state
	calendar    calendarState
	timerTickOn bool

	// Board tab state
	board boardState

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
	confirmMsg string
	// confirmOnYes is the action modeConfirm runs on y/enter; nil is a no-op.
	// Reading pending* fields at call time keeps each action a plain method.
	confirmOnYes         func(*model) tea.Cmd
	pendingDeleteID      string
	pendingComment       int
	pendingDep           int
	pendingTag           int
	pendingLearning      int
	pendingSubtask       int
	pendingCloseParentID string
	pendingReopenID      string
	pendingEntryTaskID   string
	pendingEntryID       string
	termWidth            int
	termHeight           int
	err                  string
	errKind              toastKind // styles the toast: error (default) / success / info
	projectCursor        int
	tagTabCursor         int
	learningCursor       int
	settingsCursor       int
	searchQuery          string
	tagTabSearchQuery    string
	learningSearchQuery  string
	listOffset           int
	helpScroll           int
	tabViews             [numTabs]tabView
	projectTaskMode      bool
	showHistory          bool
	focusFilter          bool
	focusStack           []focusEntry
	expandedTasks        map[string]bool
	editingTagName       string
	editingProjectName   string
	tagSort              tagSortMode
	taskSort             taskSortMode
	historySort          historySortMode
	learningSort         learningSortMode
	statsRange           statsRangeMode
	themeName            string
	updateStatus         string
	autoCloseParent      bool
	autoCloseSubtasks    bool

	// Persistence
	dirty         bool
	savePending   bool
	saveScheduled bool
	editorTaskID  string
	editorCmd     string
	// editorToInput routes the next editor round-trip back into the active text
	// input (the ctrl+e escape hatch from a comment/learning draft) instead of
	// committing to a task's notes.
	editorToInput bool

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

	// Cross-device sync config, loaded once at startup. autoSync gates the
	// periodic background sync (and the launch/exit syncs); it is on whenever a
	// sync URL+token are configured and not explicitly disabled. syncStatus is
	// the last sync outcome shown in the Settings footer.
	syncCfg    syncConfig
	autoSync   bool
	syncStatus string
	// lastSyncFailed drives the header sync-health glyph: true after a failed
	// background sync, cleared on the next success. syncStatus keeps the full
	// message for the Settings footer.
	lastSyncFailed bool
	// inprocServer is the in-process sync server when "Server" is toggled on
	// (nil otherwise). serverExternal is set by probeServer when a headless
	// `taskr serve` is answering at the configured address.
	inprocServer   *http.Server
	inprocStop     func() // stops the in-process server's change watcher
	serverExternal bool
	liveSync       *tasksync.Listener // SSE listener for real-time inbound push
	// lastTimerHeartbeat throttles how often the running timer's last_seen is
	// written to the DB (see the timer tick) so a live timer stays "fresh"
	// against the stale-timer recoverer without writing every second.
	lastTimerHeartbeat time.Time
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
	applyStages(stagesFromSettings(settings))

	store := Store{}
	store.ensureTasks()
	for i := range todos {
		store.add(todos[i])
	}
	// Restore persisted delete-undo entries so a user can `u` a task they
	// removed in a prior session. A corrupt file surfaces in errMsg; the
	// model still builds normally with an empty stack.
	if persisted, err := loadPersistedUndoEntries(); err != nil {
		if errMsg == "" {
			errMsg = fmt.Sprintf("Undo history corrupt (ignored): %v", err)
		}
	} else {
		store.undoStack = append(store.undoStack, persisted...)
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
		historySort:         settings.HistorySort,
		learningSort:        settings.LearningSort,
		autoCloseParent:     settings.AutoCloseParent,
		autoCloseSubtasks:   settings.AutoCloseSubtasks,
		themeName:           th.name,
		expandedTasks:       make(map[string]bool),
		editorCmd:           resolveEditorCmd(),
		frameTime:           time.Now(),
		ganttBarBuf:         make([]rune, 256),
		ganttColorBuf:       make([]int, 256),
		cache: &cacheState{
			dirty:         true,
			overdueSet:    make(map[string]bool),
			blockedSet:    make(map[string]bool),
			blockerSet:    make(map[string]bool),
			tagRender:     make(map[string]string, 32),
			taskTagRender: make(map[string]string, 64),
			projectTasks:  make(map[string][]todo.Todo),
			tagLastUsed:   make(map[string]time.Time),
			projLastUsed:  make(map[string]time.Time),
		},
	}
	m.applyLangPlaceholders()
	m.refreshCaches()
	// Absorb Age drift since the last open: every task's score creeps daily,
	// so a startup resync keeps the persisted column truthful even when the
	// user hasn't touched any task since yesterday.
	if err := m.repo.ResyncScores(); err != nil {
		m.flashError(fmt.Sprintf("Score resync failed: %v", err))
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
	// Load cross-device sync config once; auto-sync drives launch/periodic/exit
	// syncs when a server is configured.
	m.syncCfg = loadSyncConfig()
	m.autoSync = autoSyncEnabled(m.syncCfg)
	// Real-time inbound push: hold an SSE stream to the server so changes from
	// other devices arrive in near-instant, with the periodic tick as fallback.
	if m.autoSync {
		m.liveSync = startLiveSync(m.syncCfg)
	}
	// If this machine is set to serve, start the in-process endpoint now. A bind
	// failure (e.g. an external taskr serve already on that address) is non-fatal
	// — the TUI keeps working and the Settings row will show it's served
	// externally instead.
	if m.syncCfg.ServerOn && m.syncCfg.ServerToken != "" {
		if srv, stop, err := startSyncServer(m.syncCfg.listenAddr(), m.syncCfg.ServerToken); err == nil {
			m.inprocServer = srv
			m.inprocStop = stop
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
	if m.liveSync != nil {
		cmds = append(cmds, waitForSyncEvent(m.liveSync.C))
	}
	// Keep a periodic sync tick running for the whole session so enabling sync
	// from Settings mid-session takes effect; only sync immediately on launch
	// when it's already configured.
	cmds = append(cmds, syncTick())
	if m.autoSync {
		cmds = append(cmds, m.backgroundSync())
	}
	if p := m.probeServer(); p != nil {
		cmds = append(cmds, p)
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

// flushPendingWrites synchronously persists any dirty tasks / tombstones. The
// debounced save returns a tea.Tick command — batched with tea.Quit it races
// the program shutdown and loses the most recent mutation (e.g. add a task,
// hit q within 300ms, the task is gone on next launch). Calling this from the
// quit path closes that window. Best-effort: a save error here can't be shown
// in the TUI anymore, so we surface it on stderr.
func (m *model) flushPendingWrites() {
	dirty, tombstones := m.Store.drainDirty()
	if len(dirty) == 0 && len(tombstones) == 0 {
		return
	}
	if m.watcher != nil {
		m.watcher.recordSelfSave()
	}
	if err := m.repo.Save(dirty, tombstones); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving tasks on quit: %v\n", err)
	}
	m.dirty = false
	m.savePending = false
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

// markModified marks the named IDs dirty for the next save and flags derived
// caches as stale. The cache is refreshed lazily on the next read (via
// ensureCache), so several mutations within one Update only pay for one
// refresh instead of one per mutation. With no IDs, falls back to marking
// every task dirty — used by mass operations not yet refactored to return
// touched IDs. Undo is NOT pushed here: callers that want undo must call
// m.pushUndo(desc, ids...) BEFORE mutating, so the snapshot captures the
// pre-mutation state.
func (m *model) markModified(ids ...string) {
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

// markFilterDirty marks only the filter-derived views (the active/done split and
// its tag-render cache) for rebuild, leaving the data-derived caches — overdue
// set, tag stats, sorted tags, per-project task lists — intact. Use this when
// only the search query or focus filter changed; a full markCacheDirty would
// make every search keystroke rescan and re-sort the whole task set. A pending
// full rebuild (m.cache.dirty) still wins in ensureCache, so combining the two
// in one Update is safe.
func (m *model) markFilterDirty() {
	m.cache.filterDirty = true
	m.invalidateDetailCache()
}

func (m *model) currentTaskID() string {
	// Anchor to the cursor's task on the Tasks tab in either pane — an edit that
	// reorders the list (cycling priority, changing due date, …) should keep the
	// cursor on the same task, not the same row. Arrow/nav keys move the cursor
	// directly without markModified, so they still move freely.
	if m.tab != tabTasks {
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
	if m.showHistory {
		for i, t := range m.cache.done {
			if t.ID == taskID {
				m.cursor = i
				return
			}
		}
		return
	}
	if idx := m.visibleActiveIndexOf(taskID); idx >= 0 {
		m.cursor = idx
	}
}

// ── Recurrence ────────────────────────────────────────────────────────────────

// buildNextRecurrence constructs (but does not store) a fresh pending instance
// for a just-completed recurring task. Returns (zero, false) if the source
// isn't recurring or the rule is unparseable. The new instance inherits
// identity-ish fields (title, priority, size, project, notes, tags, recurrence
// rule) but starts clean on history (no time entries, comments, learnings,
// dependencies, subtasks).
//
// The next DueDate is computed by rolling forward from the previous DueDate (or
// CompletedAt if no due date was set) until it lands at or after today — that
// way a long-overdue "monthly" task doesn't immediately reappear in the past.
// StartDate, if set on the source, is shifted by the same delta the DueDate
// moved by, so the lead time between start and due is preserved.
func buildNextRecurrence(src todo.Todo) (todo.Todo, bool) {
	if !src.IsRecurring() {
		return todo.Todo{}, false
	}
	rule := src.Recurrence
	// Anchor the next instance on the source's due date. A recurring task with
	// no due date intentionally still gets one on respawn: we fall back to its
	// completion time (or now) as the base, so e.g. a "weekly" task closed today
	// yields a fresh instance due one interval out ("next one's due in a week").
	// That keeps urgency scoring meaningful rather than leaving recur rules inert.
	base := src.DueDate
	if base.IsZero() {
		base = src.CompletedAt
	}
	if base.IsZero() {
		base = time.Now()
	}
	next, ok := todo.NextRecurrenceFrom(rule, base)
	if !ok {
		return todo.Todo{}, false
	}
	today := startOfDay(time.Now())
	for next.Before(today) {
		advanced, ok := todo.NextRecurrenceFrom(rule, next)
		if !ok {
			break
		}
		next = advanced
	}

	clone := todo.New(src.Title)
	clone.Priority = src.Priority
	clone.Size = src.Size
	clone.Project = src.Project
	clone.Notes = src.Notes
	clone.Recurrence = src.Recurrence
	if len(src.Tags) > 0 {
		clone.Tags = append([]string{}, src.Tags...)
	}
	clone.DueDate = next
	if !src.StartDate.IsZero() && !src.DueDate.IsZero() {
		clone.StartDate = next.Add(-src.DueDate.Sub(src.StartDate))
	}
	return clone, true
}

// spawnNextRecurrence builds the next instance and adds it to the store.
// Returns the spawned ID, or "" when the source isn't recurring or the rule
// is unparseable. Also clones the source's subtree onto the new parent with
// every child reset to Pending, so a recurring "weekly review" keeps its
// checklist on each spawn instead of losing it.
func (m *model) spawnNextRecurrence(src *todo.Todo) string {
	if src == nil {
		return ""
	}
	next, ok := buildNextRecurrence(*src)
	if !ok {
		return ""
	}
	m.add(next)
	// The whole-parent due-date delta shifts child dates by the same amount,
	// so a "due 2 days before parent" child stays "due 2 days before parent"
	// on the next instance. Zero when either end has no due date.
	var delta time.Duration
	if !src.DueDate.IsZero() && !next.DueDate.IsZero() {
		delta = next.DueDate.Sub(src.DueDate)
	}
	m.cloneSubtreeReset(src.ID, next.ID, delta)
	return next.ID
}

// cloneSubtreeReset clones every descendant of srcParentID, reparented under
// newParentID, with each clone reset to Pending and history wiped
// (CompletedAt, TimeEntries, Comments, Learnings cleared). DueDate and
// StartDate are shifted by `delta` so the subtree's internal scheduling is
// preserved relative to the new parent. Shared traversal in taskops.go.
func (m *model) cloneSubtreeReset(srcParentID, newParentID string, delta time.Duration) {
	for _, clone := range cloneSubtreeResetFrom(m.subtaskIDs, m.get, srcParentID, newParentID, delta) {
		m.add(clone)
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
		if m.showHistory {
			if m.cursor < len(m.cache.done) {
				return m.get(m.cache.done[m.cursor].ID)
			}
			return nil
		}
		return m.visibleActiveAt(m.cursor)
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

// visibleActiveTasks returns the Tasks-tab active list flattened with the
// subtasks of expanded parents interleaved in order, so the list cursor can
// land on a subtask. Returned by value (a snapshot) like cache.active —
// callers re-resolve by ID for mutation.
func (m model) visibleActiveTasks() []todo.Todo {
	active := m.cache.active
	out := make([]todo.Todo, 0, len(active))
	for i := range active {
		out = append(out, active[i])
		if !m.expandedTasks[active[i].ID] {
			continue
		}
		for _, subID := range m.subtaskIDs(active[i].ID) {
			if sub := m.get(subID); sub != nil {
				out = append(out, *sub)
			}
		}
	}
	return out
}

// visibleActiveLen reports how many rows the active Tasks list renders to —
// top-level tasks plus the subtasks of expanded parents — without materializing
// the flattened slice. Mirrors visibleActiveTasks' counting (including its skip
// of any dangling subtask ID) so flat indices line up with it.
func (m *model) visibleActiveLen() int {
	active := m.cache.active
	n := len(active)
	for i := range active {
		if !m.expandedTasks[active[i].ID] {
			continue
		}
		for _, subID := range m.subtaskOf[active[i].ID] {
			if m.get(subID) != nil {
				n++
			}
		}
	}
	return n
}

// visibleActiveAt returns the task at flat row idx in the active Tasks list, or
// nil if idx is out of range — walking the flattened order without building it.
func (m *model) visibleActiveAt(idx int) *todo.Todo {
	if idx < 0 {
		return nil
	}
	row := 0
	active := m.cache.active
	for i := range active {
		if row == idx {
			return m.get(active[i].ID)
		}
		row++
		if !m.expandedTasks[active[i].ID] {
			continue
		}
		for _, subID := range m.subtaskOf[active[i].ID] {
			sub := m.get(subID)
			if sub == nil {
				continue
			}
			if row == idx {
				return sub
			}
			row++
		}
	}
	return nil
}

// visibleActiveIndexOf returns the flat row index of taskID in the active Tasks
// list, or -1 if it isn't currently visible.
func (m *model) visibleActiveIndexOf(taskID string) int {
	row := 0
	active := m.cache.active
	for i := range active {
		if active[i].ID == taskID {
			return row
		}
		row++
		if !m.expandedTasks[active[i].ID] {
			continue
		}
		for _, subID := range m.subtaskOf[active[i].ID] {
			sub := m.get(subID)
			if sub == nil {
				continue
			}
			if sub.ID == taskID {
				return row
			}
			row++
		}
	}
	return -1
}

// visibleActiveWindow materializes only flat rows [start, end) of the active
// Tasks list, so the renderer copies the screenful it draws instead of the whole
// list. Rows outside the window are walked by pointer (no struct copy); only the
// emitted rows are copied into the returned slice.
func (m *model) visibleActiveWindow(start, end int) []todo.Todo {
	if start < 0 {
		start = 0
	}
	if end <= start {
		return nil
	}
	out := make([]todo.Todo, 0, end-start)
	row := 0
	done := false
	visit := func(t *todo.Todo) {
		if row >= start && row < end {
			out = append(out, *t)
		}
		row++
		if row >= end {
			done = true
		}
	}
	active := m.cache.active
	for i := range active {
		visit(&active[i])
		if done {
			break
		}
		if !m.expandedTasks[active[i].ID] {
			continue
		}
		for _, subID := range m.subtaskOf[active[i].ID] {
			if sub := m.get(subID); sub != nil {
				visit(sub)
				if done {
					break
				}
			}
		}
		if done {
			break
		}
	}
	return out
}

// subtaskCount returns how many subtasks parentID has, via the maintained
// subtaskOf index.
func (m *model) subtaskCount(parentID string) int {
	return len(m.subtaskOf[parentID])
}

// descendantIDs returns rootID followed by every transitive subtask ID in
// BFS order, via the maintained subtaskOf index. Shared traversal in
// taskops.go.
func (m model) descendantIDs(rootID string) []string {
	return descendantIDsFrom(m.subtaskIDs, rootID)
}

// subtaskProgress reports the (done, total) count of parentID's direct
// children. The Tasks-tab badge `(2/5)` reads this — direct children match
// the visible tree better than counting transitive descendants.
func (m *model) subtaskProgress(parentID string) (done, total int) {
	ids := m.subtaskIDs(parentID)
	total = len(ids)
	for _, id := range ids {
		if c := m.get(id); c != nil && c.Status == todo.Done {
			done++
		}
	}
	return done, total
}

// hasOverdueDescendant returns true if any task in parentID's subtree (any
// depth) is currently overdue. Recursive so a deeply-nested overdue child
// still surfaces on the root row.
func (m *model) hasOverdueDescendant(parentID string, overdueSet map[string]bool) bool {
	for _, id := range m.subtaskIDs(parentID) {
		if overdueSet[id] {
			return true
		}
		if m.hasOverdueDescendant(id, overdueSet) {
			return true
		}
	}
	return false
}

// descendantTimeSpent sums TotalTimeSpent across parentID's full subtree (not
// including parentID itself). Used by the detail view to roll subtask time
// up onto the parent's display.
func (m model) descendantTimeSpent(parentID string) time.Duration {
	var sum time.Duration
	for _, id := range m.subtaskIDs(parentID) {
		if c := m.get(id); c != nil {
			sum += c.TotalTimeSpent()
		}
		sum += m.descendantTimeSpent(id)
	}
	return sum
}

// allDescendantsDoneOrEmpty reports whether every transitive descendant of
// parentID is Done, returning true when parentID has no children. Used
// recursively so a leaf doesn't fail the "all done" check just by lacking
// subtasks.
func (m model) allDescendantsDoneOrEmpty(parentID string) bool {
	for _, id := range m.subtaskIDs(parentID) {
		c := m.get(id)
		if c == nil || c.Status != todo.Done {
			return false
		}
		if !m.allDescendantsDoneOrEmpty(id) {
			return false
		}
	}
	return true
}

// autoCloseAncestorsIfAllDone walks up from childID and, while the setting
// is enabled, closes every ancestor whose subtree is now fully done. Returns
// the closed ancestor IDs (plus any recurring-spawn IDs) so the caller can
// mark them dirty. No-op when the setting is off or childID itself isn't
// Done. Open ancestors with sibling work pending naturally stop the walk
// because allDescendantsDoneOrEmpty returns false.
func (m *model) autoCloseAncestorsIfAllDone(childID string) []string {
	if !m.autoCloseParent {
		return nil
	}
	var closed []string
	cur := m.get(childID)
	for cur != nil && cur.ParentID != "" {
		parent := m.get(cur.ParentID)
		if parent == nil || parent.Status == todo.Done {
			break
		}
		if !m.allDescendantsDoneOrEmpty(parent.ID) {
			break
		}
		if parent.IsTimerRunning() {
			m.stopTimer(parent.ID)
		}
		parent.Toggle()
		closed = append(closed, parent.ID)
		if parent.IsRecurring() {
			if newID := m.spawnNextRecurrence(parent); newID != "" {
				closed = append(closed, newID)
			}
		}
		cur = parent
	}
	return closed
}

// closePendingSubtree closes every still-pending, non-deleted descendant of
// parentID (the mirror of autoCloseAncestorsIfAllDone), so marking a parent
// Done doesn't strand open subtasks. Stops any running timers and stamps the
// sequence rank like a normal close. Returns the closed descendant IDs so the
// caller can mark them dirty. Does not touch parentID itself.
func (m *model) closePendingSubtree(parentID string) []string {
	var closed []string
	for _, id := range descendantIDsFrom(m.subtaskIDs, parentID)[1:] { // [0] is parentID
		s := m.get(id)
		if s == nil || s.Deleted || s.Status != todo.Pending {
			continue
		}
		if s.IsTimerRunning() {
			m.stopTimer(s.ID)
		}
		captureSeqRankAtDone(m.allTodos(), s)
		s.Toggle()
		closed = append(closed, s.ID)
	}
	return closed
}

// extendParentDueIfNeeded walks up from subID and bumps each ancestor's
// DueDate forward to at least match the child's, recursively. Only extends
// — never shrinks an ancestor's date. Returns the ancestor IDs that were
// modified so the caller can mark them dirty. Shared walk in taskops.go.
func (m *model) extendParentDueIfNeeded(subID string) []string {
	bumped := extendAncestorsDue(m.get, m.get(subID))
	ids := make([]string, len(bumped))
	for i, p := range bumped {
		ids[i] = p.ID
	}
	return ids
}

// toggleSubtask flips a child task's status and returns every ID the caller
// should mark dirty: the toggled subtask, plus the freshly-spawned next
// instance when the subtask was a recurring task closed by this call, plus
// any ancestors auto-closed because their subtree is now fully done.
func (m *model) toggleSubtask(parentID string, subtaskCursor int) []string {
	ids := m.subtaskIDs(parentID)
	if subtaskCursor >= len(ids) {
		return nil
	}
	subID := ids[subtaskCursor]
	t := m.get(subID)
	if t == nil {
		return nil
	}
	// Don't leave a dangling open time entry when closing a subtask. Mirrors
	// the top-level `d` handler in update.go.
	if t.Status == todo.Pending && t.IsTimerRunning() {
		m.stopTimer(t.ID)
	}
	wasPending := t.Status == todo.Pending
	t.Toggle()
	out := []string{subID}
	if wasPending && t.IsRecurring() {
		if newID := m.spawnNextRecurrence(t); newID != "" {
			out = append(out, newID)
		}
	}
	if wasPending {
		out = append(out, m.autoCloseAncestorsIfAllDone(subID)...)
	}
	return out
}

// ── Search/filter helpers ─────────────────────────────────────────────────────

func (m model) matchesSearch(t todo.Todo) bool {
	return todoMatchesSearch(t, m.searchQuery)
}

// loopingDepCandidates returns the task IDs that must not be offered as a new
// dependency of curID, because depending on them would create a cycle: curID
// itself, plus every task that already (transitively) depends on curID. It
// builds the dependents adjacency once and BFS-es out from curID; the visited
// set doubles as the result and guards against pre-existing/malformed cycles so
// the walk always terminates.
func loopingDepCandidates(tasks map[string]*todo.Todo, curID string) map[string]bool {
	excluded := map[string]bool{curID: true}
	dependents := make(map[string][]string)
	for _, c := range tasks {
		for _, dep := range c.Dependencies {
			dependents[dep] = append(dependents[dep], c.ID)
		}
	}
	queue := []string{curID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, d := range dependents[id] {
			if !excluded[d] {
				excluded[d] = true
				queue = append(queue, d)
			}
		}
	}
	return excluded
}

func (m model) depSearchResults() []todo.Todo {
	t := m.currentTodo()
	q := strings.ToLower(m.depSearch.query)
	// Hide any candidate that would close a dependency loop — the current task
	// itself plus everything that already (transitively) depends on it — so the
	// picker can only ever offer a task that's safe to depend on. No error path
	// needed: a cycle-forming task simply isn't in the list.
	var excluded map[string]bool
	if t != nil {
		excluded = loopingDepCandidates(m.tasks, t.ID)
	}
	result := make([]todo.Todo, 0, maxDepSearchResults*2)
	for _, candidate := range m.tasks {
		if excluded[candidate.ID] {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(candidate.Title), q) {
			result = append(result, *candidate)
		}
	}
	// Most-recently-modified first, so the tasks you've just been working on are
	// the likeliest dependencies and sit at the top — matching the recency order
	// the tag/project pickers use. The ID tiebreak keeps the picker stable across
	// redraws (cursor blink, timer tick): a bare map range reshuffled every frame.
	sort.Slice(result, func(i, j int) bool {
		if !result[i].ModifiedAt.Equal(result[j].ModifiedAt) {
			return result[i].ModifiedAt.After(result[j].ModifiedAt)
		}
		return result[i].ID < result[j].ID
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
	sortTags(tags, m.tagSort, m.cache.tags, m.cache.tagLastUsed)
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
	sortByRecency(result, m.cache.tagLastUsed)
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
	sortByRecency(result, m.cache.projLastUsed)
	return result
}

// sortByRecency orders names most-recently-used first (latest ModifiedAt of any
// task carrying the tag/project); names with equal/no usage time fall back to
// alphabetical.
func sortByRecency(names []string, lastUsed map[string]time.Time) {
	sort.SliceStable(names, func(i, j int) bool {
		ti, tj := lastUsed[names[i]], lastUsed[names[j]]
		if ti.Equal(tj) {
			return names[i] < names[j]
		}
		return ti.After(tj)
	})
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
		// RemoveTag normalizes its argument, so it can't match a legacy
		// mixed-case stored tag (e.g. "Work") — strip the literal oldName,
		// then AddTag (which normalizes + dedups) to complete the merge.
		kept := t.Tags[:0]
		for _, tag := range t.Tags {
			if tag != oldName {
				kept = append(kept, tag)
			}
		}
		t.Tags = kept
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
