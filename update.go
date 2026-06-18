package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"taskr/todo"
)

// ── Top-level Update ──────────────────────────────────────────────────────────

// Update is the Bubble Tea entry point. It delegates the real work to
// dispatch, then layers on a single concern: if the user just left a modal
// mode and a watcher reload was deferred while they were typing, schedule
// the reload now so the deferred external change isn't silently lost.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.dispatch(msg)
	n, ok := next.(model)
	if !ok {
		return next, cmd
	}
	if n.mode == modeNormal && n.watcher != nil && n.watcher.drainPending() {
		repo := n.repo
		reload := func() tea.Msg {
			todos, err := repo.Load()
			return reloadedMsg{todos: todos, err: err}
		}
		if cmd == nil {
			cmd = reload
		} else {
			cmd = tea.Batch(cmd, reload)
		}
	}
	return n, cmd
}

func (m model) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case timerTickMsg:
		if m.anyTimerRunning() {
			return m, timerTick()
		}
		m.timerTickOn = false
		return m, nil
	case updateDoneMsg:
		if msg.err != nil {
			m.err = fmt.Sprintf("Update failed: %v", msg.err)
			m.updateStatus = tr("Update failed")
		} else {
			m.err = tr("Updated! Restart taskr to apply.")
			m.updateStatus = tr("Updated — restart to apply")
		}
		return m, clearErrAfter()
	case updateCheckMsg:
		if msg.err != nil {
			m.err = fmt.Sprintf("Update check failed: %v", msg.err)
			m.updateStatus = tr("Check failed")
			return m, clearErrAfter()
		}
		if msg.latest == "" || msg.latest == appVersion {
			m.updateStatus = tr("Up to date (") + appVersion + ")"
			return m, nil
		}
		// Newer release available — ask before pulling it.
		m.updateStatus = tr("Update available: ") + msg.latest
		m.mode = modeConfirmUpdate
		m.confirmMsg = msg.latest + tr(" is available — update now? (y/n)")
		return m, nil
	case saveDoneMsg:
		return m, nil
	case saveErrMsg:
		m.err = fmt.Sprintf("Error saving tasks: %v", msg.err)
		return m, clearErrAfter()
	case editorFinishedMsg:
		return m.handleEditorFinished(msg)
	case saveTickMsg:
		m.saveScheduled = false
		if m.savePending {
			m.savePending = false
			// Drain only the dirty IDs and tombstones from the Store. Per-task
			// deep copies in drainDirty keep the save goroutine safe from
			// concurrent mutation. Step 5 (map[ID]*Todo) eliminates the copy
			// once pointers become stable across slice growth.
			dirty, tombstones := m.Store.drainDirty()
			if len(dirty) == 0 && len(tombstones) == 0 {
				return m, nil
			}
			repo := m.repo
			if m.watcher != nil {
				// Record the timestamp BEFORE the save so a fast fs event
				// firing during the write is still inside the suppression
				// window. The save goroutine doesn't need to update this.
				m.watcher.recordSelfSave()
			}
			return m, func() tea.Msg {
				if err := repo.Save(dirty, tombstones); err != nil {
					return saveErrMsg{err}
				}
				return saveDoneMsg{}
			}
		}
		return m, nil
	case dbChangedMsg:
		// External writer (CLI, another process) touched the DB. Decide
		// whether to reload now or defer until the user exits a modal mode.
		// Always re-arm the watcher channel listener.
		var cmds []tea.Cmd
		if m.watcher != nil {
			cmds = append(cmds, waitForDBChange(m.watcher.ch))
			if m.watcher.shouldReloadNow(time.Now(), m.mode) {
				repo := m.repo
				cmds = append(cmds, func() tea.Msg {
					todos, err := repo.Load()
					return reloadedMsg{todos: todos, err: err}
				})
			}
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)
	case reloadedMsg:
		if msg.err != nil {
			m.err = fmt.Sprintf("External reload failed: %v", msg.err)
			return m, clearErrAfter()
		}
		// Atomic swap: rebuild the Store from the freshly-loaded task set,
		// invalidate caches, and follow the same task ID across the new
		// ordering so the cursor stays anchored where the user expected.
		taskID := m.currentTaskID()
		m.Store = Store{}
		m.Store.ensureTasks()
		for i := range msg.todos {
			m.Store.add(msg.todos[i])
		}
		m.markCacheDirty()
		m.refreshCaches()
		m.followTask(taskID)
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
	case modeConfirmDeleteTimeEntry:
		return m.updateConfirmDeleteTimeEntry(msg)
	case modeConfirmUpdate:
		return m.updateConfirmUpdate(msg)
	case modeEditTimeEntry:
		return m.updateEditTimeEntry(msg)
	case modeIdlePrompt:
		return m.updateIdlePrompt(msg)
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
	case modeEditSubtask:
		return m.updateEditSubtask(msg)
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
	t := m.currentTodo()
	if t == nil {
		return nil
	}
	taskID := t.ID

	if err := writeNotesFile(taskID, t.Notes); err != nil {
		m.err = fmt.Sprintf("Error writing notes file: %v", err)
		return clearErrAfter()
	}

	editorCmd := resolveEditorCmd()
	if editorCmd == "" {
		if runtime.GOOS == "windows" {
			m.err = tr("No editor found — set EDITOR permanently, e.g: setx EDITOR notepad (then restart taskr)")
		} else {
			m.err = tr("No editor found — set $EDITOR permanently, e.g: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish")
		}
		return clearErrAfter()
	}

	m.editorTaskID = taskID
	return execEditor(editorCmd, taskID, false)
}

func execEditor(editorCmd, taskID string, isFallback bool) tea.Cmd {
	c := exec.Command(editorCmd, notesFilePath(taskID))
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{taskID: taskID, err: err, fallback: isFallback}
	})
}

func (m model) handleEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// On Windows, fall back to notepad once if the configured editor failed.
		if runtime.GOOS == "windows" && !msg.fallback {
			if notepad, lookErr := exec.LookPath("notepad"); lookErr == nil {
				m.err = tr("Editor failed — falling back to notepad")
				return m, tea.Batch(clearErrAfter(), execEditor(notepad, msg.taskID, true))
			}
		}
		m.err = fmt.Sprintf("Editor exited with error: %v", msg.err)
		return m, clearErrAfter()
	}
	taskID := msg.taskID
	content, err := readNotesFile(taskID)
	if err != nil {
		m.err = fmt.Sprintf("Error reading notes: %v", err)
		return m, clearErrAfter()
	}

	if t := m.get(taskID); t != nil {
		newNotes := strings.TrimRight(content, "\n\r ")
		if newNotes != t.Notes {
			m.pushUndo("edit notes", t.ID)
			t.SetNotes(newNotes)
			m.markDirty(t.ID)
			m.dirty = true
			m.cache.dirty = true
			m.invalidateDetailCache()
			m.refreshCaches()
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
		m.err = tr("Nothing to undo")
		return clearErrAfter()
	}
	// Partial entries name the IDs they touched. Tasks captured in the entry
	// are restored to their prior state (mark dirty). Tasks named but not
	// captured were newly-created — undo means delete them (mark tombstone).
	// Any tombstones in the current save set are cleared for restored IDs.
	if entry.partial != nil || entry.ids != nil {
		captured := make(map[string]struct{}, len(entry.partial))
		for i := range entry.partial {
			captured[entry.partial[i].ID] = struct{}{}
		}
		var restored, removed []string
		for _, id := range entry.ids {
			if _, ok := captured[id]; ok {
				restored = append(restored, id)
				delete(m.tombstones, id) // restoration overrides any pending tombstone
			} else {
				removed = append(removed, id)
			}
		}
		m.restoreFromUndo(entry)
		for _, id := range removed {
			m.markTombstone(id)
		}
		m.markModifiedNoUndo(restored...)
		m.err = fmt.Sprintf(tr("Undid: %s"), entry.desc)
		return clearErrAfter()
	}

	// Full snapshot fallback: compute the set difference and tombstone IDs
	// that existed before the undo but vanish from the restored snapshot.
	before := make(map[string]struct{}, len(m.tasks))
	for id := range m.tasks {
		before[id] = struct{}{}
	}
	m.restoreFromUndo(entry)
	restoredIDs := make([]string, 0, len(m.tasks))
	for id := range m.tasks {
		restoredIDs = append(restoredIDs, id)
		delete(before, id)
	}
	for id := range before {
		m.markTombstone(id)
	}
	m.markModifiedNoUndo(restoredIDs...)
	m.err = fmt.Sprintf(tr("Undid: %s"), entry.desc)
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
			m.switchTab(tabCalendar)
		case "3":
			m.switchTab(tabProjects)
		case "4":
			m.switchTab(tabTags)
		case "5":
			m.switchTab(tabLearnings)
		case "6":
			m.tab = tabStats
			m.pane = paneList
			m.listOffset = 0
		case "7":
			m.switchTab(tabSettings)

		case "tab":
			// In the task detail pane, Tab cycles the detail page (main →
			// subtasks → comments → main); everywhere else it advances the
			// main tab bar.
			if m.pane == paneDetail && m.tab == tabTasks {
				m.detail.page = (m.detail.page + 1) % 3
				if m.detail.page == 0 {
					m.detail.field = fieldStartDate
				} else if m.detail.page == 1 {
					m.detail.field = fieldSubtasks
					m.detail.subtaskCursor = 0
				} else {
					m.detail.commentCursor = 0
				}
				m.invalidateDetailCache()
			} else {
				m.switchTab((m.tab + 1) % numTabs)
			}

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
			if m.tab == tabCalendar {
				m.moveCalendarDay(1)
			} else if m.tab == tabSettings && m.isBiasSettingRow(m.settingsCursor) {
				m.cycleBias(m.settingsCursor, +1)
			} else if m.tab == tabSettings && m.settingsCursor == settingTheme {
				m.cycleTheme(1)
			} else if m.tab == tabSettings && m.settingsCursor == settingLanguage {
				m.cycleLang(1)
			} else if m.tab == tabTasks && !m.showHistory {
				if t := m.currentTodo(); t != nil && m.subtaskCount(t.ID) > 0 {
					m.expandedTasks[t.ID] = true
				}
			}
		case "left":
			if m.tab == tabCalendar {
				m.moveCalendarDay(-1)
			} else if m.tab == tabSettings && m.isBiasSettingRow(m.settingsCursor) {
				m.cycleBias(m.settingsCursor, -1)
			} else if m.tab == tabSettings && m.settingsCursor == settingTheme {
				m.cycleTheme(-1)
			} else if m.tab == tabSettings && m.settingsCursor == settingLanguage {
				m.cycleLang(-1)
			} else if m.tab == tabTasks && !m.showHistory {
				if t := m.currentTodo(); t != nil {
					delete(m.expandedTasks, t.ID)
				}
			}

		case "[", "]":
			if m.tab == tabCalendar {
				months := 1
				if key.String() == "[" {
					months = -1
				}
				m.calendar.selected = startOfDay(m.calendar.selected.AddDate(0, months, 0))
				m.calendar.entryCursor = 0
				m.calendar.focusTimeline = false
			}

		case "t":
			if m.tab == tabCalendar {
				m.calendar.selected = startOfDay(time.Now())
				m.calendar.entryCursor = 0
				m.calendar.focusTimeline = false
			} else if m.tab == tabTasks && !m.showHistory {
				if t := m.currentTodo(); t != nil {
					if e := t.RunningEntry(); e != nil && time.Since(e.StartedAt) > idleThreshold {
						m.openIdlePrompt(t)
						return m, nil
					}
					m.toggleTimer(t)
					m.markModified(t.ID)
					if !m.timerTickOn && m.anyTimerRunning() {
						m.timerTickOn = true
						return m, timerTick()
					}
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
				m.textInput.Placeholder = tr("New task (use #tag due:date p:high @project)...")
				m.textInput.Focus()
				return m, textinput.Blink
			}

		case "d":
			if m.tab == tabTasks {
				if t := m.currentTodo(); t != nil {
					t.Toggle()
					m.markModified(t.ID)
					if m.cursor > 0 {
						m.cursor--
					}
				}
			}

		case "p":
			if m.tab == tabTasks && !m.showHistory {
				if t := m.currentTodo(); t != nil {
					switch t.Priority {
					case todo.PriorityLow:
						t.SetPriority(todo.PriorityMedium)
					case todo.PriorityMedium:
						t.SetPriority(todo.PriorityHigh)
					default:
						t.SetPriority(todo.PriorityLow)
					}
					m.markModified(t.ID)
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
	m.settingsCursor = 0
	m.listOffset = 0
	m.pane = paneList
	m.searchQuery = ""
	m.tagTabSearchQuery = ""
	m.learningSearchQuery = ""
	m.projectTaskMode = false
	m.showHistory = false
	if t == tabCalendar {
		m.calendar.selected = startOfDay(time.Now())
		m.calendar.entryCursor = 0
		m.calendar.focusTimeline = false
	}
	m.invalidateDetailCache()
	m.markCacheDirty()
}

// startEditTimeEntry opens the inline editor for an entry's times,
// prefilled with the current range.
func (m *model) startEditTimeEntry(taskID, entryID string) tea.Cmd {
	t := m.findTodoByID(taskID)
	if t == nil {
		return nil
	}
	for i := range t.TimeEntries {
		if t.TimeEntries[i].ID == entryID {
			e := &t.TimeEntries[i]
			val := e.StartedAt.Format("15:04") + "-"
			if e.IsRunning() {
				val += "now"
			} else {
				val += e.StoppedAt.Format("15:04")
			}
			m.pendingEntryTaskID = taskID
			m.pendingEntryID = entryID
			m.mode = modeEditTimeEntry
			m.textInput.SetValue(val)
			m.textInput.Placeholder = tr("HH:MM-HH:MM or duration (45m, 1h30m)...")
			m.textInput.Focus()
			return textinput.Blink
		}
	}
	return nil
}

func (m *model) moveCalendarDay(days int) {
	m.calendar.selected = m.calendar.selected.AddDate(0, 0, days)
	m.calendar.entryCursor = 0
	m.calendar.focusTimeline = false
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
		m.sortCachedTags()
	case tabTasks:
		// Three-state cycle: Sequence → DueDate → Size → Sequence.
		switch m.taskSort {
		case taskSortSequence:
			m.taskSort = taskSortDueDate
		case taskSortDueDate:
			m.taskSort = taskSortSize
		default:
			m.taskSort = taskSortSequence
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
	m.persistSettings()
}

// isBiasSettingRow reports whether the given Settings cursor row is one of
// the three sequencing-bias knobs (so ←/→ should cycle a bias rather than the
// theme/language picker).
func (m *model) isBiasSettingRow(row int) bool {
	return row == settingBiasDeadline || row == settingBiasPriority || row == settingBiasMomentum
}

// cycleBias rotates the named bias by `direction` (+1 next, -1 prev), updates
// the activeBiases global, invalidates the sort cache so the new ranking takes
// effect on the next render, persists the change, and resyncs the persisted
// `sequence` column so any SQL consumer (TopBySequence, future sync) sees the
// new weights immediately rather than waiting for the next mutation.
func (m *model) cycleBias(row, direction int) {
	switch row {
	case settingBiasDeadline:
		activeBiases.Deadline = cycleBiasLevel(activeBiases.Deadline, direction)
	case settingBiasPriority:
		activeBiases.Priority = cycleBiasLevel(activeBiases.Priority, direction)
	case settingBiasMomentum:
		activeBiases.Momentum = cycleBiasLevel(activeBiases.Momentum, direction)
	default:
		return
	}
	m.markCacheDirty()
	m.persistSettings()
	if err := m.repo.ResyncScores(); err != nil {
		m.err = fmt.Sprintf(tr("Score resync failed: %v"), err)
	}
}

// persistSettings writes all current preferences to disk, surfacing any write
// failure so a setting that silently won't stick is at least visible.
func (m *model) persistSettings() {
	if err := saveSettings(appSettings{
		TaskSort:        m.taskSort,
		TagSort:         m.tagSort,
		LearningSort:    m.learningSort,
		Theme:           m.themeName,
		Language:        string(activeLang),
		SeqBiasDeadline: activeBiases.Deadline,
		SeqBiasPriority: activeBiases.Priority,
		SeqBiasMomentum: activeBiases.Momentum,
	}); err != nil {
		m.err = fmt.Sprintf(tr("Error saving settings: %v"), err)
	}
}

func (m *model) handleListEsc() {
	switch {
	case m.tab == tabCalendar && m.calendar.focusTimeline:
		m.calendar.focusTimeline = false
	case m.tab == tabTasks && m.focusFilter:
		m.focusFilter = false
		m.cursor = 0
		m.listOffset = 0
		m.markCacheDirty()
	case m.tab == tabTasks && m.searchQuery != "":
		m.searchQuery = ""
		m.searchInput.SetValue("")
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
	case tabCalendar:
		if m.calendar.focusTimeline {
			if m.calendar.entryCursor > 0 {
				m.calendar.entryCursor--
			}
		} else {
			m.moveCalendarDay(-7)
		}
	case tabTags:
		if m.tagTabCursor > 0 {
			m.tagTabCursor--
		}
	case tabLearnings:
		if m.learningCursor > 0 {
			m.learningCursor--
		}
	case tabSettings:
		if m.settingsCursor > 0 {
			m.settingsCursor--
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
	case tabCalendar:
		if m.calendar.focusTimeline {
			if acts := m.activitiesForDay(m.calendar.selected); m.calendar.entryCursor < len(acts)-1 {
				m.calendar.entryCursor++
			}
		} else {
			m.moveCalendarDay(7)
		}
	case tabTags:
		if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags)-1 {
			m.tagTabCursor++
		}
	case tabLearnings:
		if learnings := m.allLearnings(); m.learningCursor < len(learnings)-1 {
			m.learningCursor++
		}
	case tabSettings:
		if m.settingsCursor < numSettingsRows-1 {
			m.settingsCursor++
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
	case tabCalendar:
		if len(m.activitiesForDay(m.calendar.selected)) > 0 {
			m.calendar.focusTimeline = true
			m.calendar.entryCursor = 0
		}
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
	case tabTags:
		if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) {
			tag := tags[m.tagTabCursor]
			m.switchTab(tabTasks)
			if tag == untaggedKey {
				m.searchQuery = untaggedKey
			} else {
				m.searchQuery = "#" + tag
			}
			m.markCacheDirty()
		}
	case tabStats:
		m.statsRange = (m.statsRange + 1) % statsRangeCount
	case tabSettings:
		return m.handleSettingsEnter()
	}
	return m, nil
}

// ── Settings tab ──────────────────────────────────────────────────────────────

func (m model) handleSettingsEnter() (tea.Model, tea.Cmd) {
	switch m.settingsCursor {
	case settingTheme:
		m.cycleTheme(1)
	case settingLanguage:
		m.cycleLang(1)
	case settingCheckUpdate:
		m.updateStatus = tr("Checking…")
		return m, checkForUpdate()
	}
	return m, nil
}

// updateConfirmUpdate handles the "newer release available — update now?" prompt.
func (m model) updateConfirmUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y", "enter":
			m.mode = modeNormal
			m.updateStatus = tr("Updating…")
			return m, func() tea.Msg {
				return updateDoneMsg{err: selfUpdate()}
			}
		case "n", "esc":
			m.mode = modeNormal
		}
	}
	return m, nil
}

// checkForUpdate queries the latest release tag asynchronously.
func checkForUpdate() tea.Cmd {
	return func() tea.Msg {
		latest, err := latestRelease()
		return updateCheckMsg{latest: latest, err: err}
	}
}

// cycleTheme advances the theme by dir (+1 / -1), applies it, and persists.
func (m *model) cycleTheme(dir int) {
	idx := 0
	for i, t := range themes {
		if t.name == m.themeName {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(themes)) % len(themes)
	m.themeName = themes[idx].name
	applyTheme(themes[idx])
	m.persistSettings()
	m.invalidateDetailCache()
	m.markCacheDirty()
}

// cycleLang steps the UI language through availableLanguages. Only what the user
// sees changes; stored data (titles, tags, dates) is untouched.
func (m *model) cycleLang(dir int) {
	idx := 0
	for i, l := range availableLanguages {
		if l == activeLang {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(availableLanguages)) % len(availableLanguages)
	applyLang(string(availableLanguages[idx]))
	m.applyLangPlaceholders()
	m.persistSettings()
	m.invalidateDetailCache()
	m.markCacheDirty()
}

// applyLangPlaceholders sets every text-input placeholder from the active
// language. Called at startup and whenever the language changes so the prompts
// switch live rather than waiting for a restart.
func (m *model) applyLangPlaceholders() {
	m.searchInput.Placeholder = tr("Search... (use # to filter by tag)")
	m.depSearchInput.Placeholder = tr("Search for task to add as dependency...")
	m.tagSearchInput.Placeholder = tr("Search or create tag...")
	m.projSearchInput.Placeholder = tr("Search or create project...")
	m.tagTabSearchInput.Placeholder = tr("Filter tags...")
	m.learningSearchInput.Placeholder = tr("Search learnings... (use # to filter by tag)")
}

func (m model) handleListRename() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabCalendar:
		if m.calendar.focusTimeline {
			acts := m.activitiesForDay(m.calendar.selected)
			if m.calendar.entryCursor < len(acts) {
				a := acts[m.calendar.entryCursor]
				return m, m.startEditTimeEntry(a.taskID, a.entryID)
			}
		}
	case tabTags:
		if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) && tags[m.tagTabCursor] != untaggedKey {
			m.editingTagName = tags[m.tagTabCursor]
			m.mode = modeEditTag
			m.textInput.SetValue(tags[m.tagTabCursor])
			m.textInput.Placeholder = tr("Edit tag name...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	case tabTasks:
		if !m.showHistory {
			if t := m.currentTodo(); t != nil {
				m.mode = modeEditTitle
				m.textInput.SetValue(t.Title)
				m.textInput.Placeholder = tr("Edit task title...")
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
			m.textInput.Placeholder = tr("Edit learning...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m model) handleListDelete() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabCalendar:
		if m.calendar.focusTimeline {
			acts := m.activitiesForDay(m.calendar.selected)
			if m.calendar.entryCursor < len(acts) {
				a := acts[m.calendar.entryCursor]
				m.mode = modeConfirmDeleteTimeEntry
				m.pendingEntryTaskID = a.taskID
				m.pendingEntryID = a.entryID
				m.confirmMsg = fmt.Sprintf(tr("Delete %s entry for '%s'? (y/n)"),
					formatDuration(a.duration()), truncate(a.title, 30))
			}
		}
	case tabTags:
		if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) && tags[m.tagTabCursor] != untaggedKey {
			m.mode = modeConfirmDeleteTagGlobal
			m.confirmMsg = fmt.Sprintf(tr("Delete tag '#%s' from ALL tasks? (y/n)"), tags[m.tagTabCursor])
		}
	case tabTasks:
		if t := m.currentTodo(); t != nil {
			m.mode = modeConfirmDelete
			m.pendingDelete = m.cursor
			m.confirmMsg = fmt.Sprintf(tr("Delete '%s'? (y/n)"), t.Title)
		}
	case tabLearnings:
		if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
			m.mode = modeConfirmDeleteLearning
			m.pendingLearning = m.learningCursor
			m.confirmMsg = fmt.Sprintf(tr("Delete learning '%s'? (y/n)"), truncate(learnings[m.learningCursor].Text, 40))
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
			if m.detail.page == 0 {
				m.detail.field = fieldStartDate
			} else {
				m.detail.field = fieldSubtasks
			}
			m.invalidateDetailCache()
		}
	case "right", "l":
		if m.detail.page < 2 {
			m.detail.page++
			if m.detail.page == 1 {
				m.detail.field = fieldSubtasks
				m.detail.subtaskCursor = 0
			} else {
				m.detail.commentCursor = 0
			}
			m.invalidateDetailCache()
		}

	case "up", "k":
		m.detailCursorUp()
	case "down", "j":
		m.detailCursorDown()

	case "enter":
		return m.startEditing()

	case "d":
		if m.detail.page == 1 && m.detail.field == fieldSubtasks {
			if t := m.currentTodo(); t != nil {
				if m.detail.subtaskCursor < m.subtaskCount(t.ID) {
					subID := m.toggleSubtask(t.ID, m.detail.subtaskCursor)
					m.markModified(subID)
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
		case fieldSize:
			m.detail.field = fieldPriority
		case fieldProject:
			m.detail.field = fieldSize
		case fieldNotes:
			m.detail.field = fieldProject
		case fieldTags:
			if m.detail.tagCursor > 0 {
				m.detail.tagCursor--
			} else {
				m.detail.field = fieldNotes
			}
		}
	} else if m.detail.page == 1 {
		t := m.currentTodo()
		switch m.detail.field {
		case fieldSubtasks:
			if m.detail.subtaskCursor > 0 {
				m.detail.subtaskCursor--
			}
		case fieldDependencies:
			if m.detail.depCursor > 0 {
				m.detail.depCursor--
			} else {
				m.detail.field = fieldSubtasks
				if t != nil && m.subtaskCount(t.ID) > 0 {
					m.detail.subtaskCursor = m.subtaskCount(t.ID) - 1
				}
			}
		case fieldLearnings:
			if m.detail.learningCursor > 0 {
				m.detail.learningCursor--
			} else {
				m.detail.field = fieldDependencies
				if t != nil && len(t.Dependencies) > 0 {
					m.detail.depCursor = len(t.Dependencies) - 1
				}
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
			m.detail.field = fieldSize
		case fieldSize:
			m.detail.field = fieldProject
		case fieldProject:
			m.detail.field = fieldNotes
		case fieldNotes:
			m.detail.field = fieldTags
			m.detail.tagCursor = 0
		case fieldTags:
			if t != nil && m.detail.tagCursor < len(t.Tags)-1 {
				m.detail.tagCursor++
			}
		}
	} else if m.detail.page == 1 {
		t := m.currentTodo()
		switch m.detail.field {
		case fieldSubtasks:
			if t != nil && m.detail.subtaskCursor < m.subtaskCount(t.ID)-1 {
				m.detail.subtaskCursor++
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
			}
		}
	} else if t := m.currentTodo(); t != nil && m.detail.commentCursor < len(t.Comments)-1 {
		m.detail.commentCursor++
	}
}

func (m model) detailAdd() (tea.Model, tea.Cmd) {
	if m.detail.page == 2 {
		m.mode = modeInput
		m.textInput.SetValue("")
		m.textInput.Placeholder = tr("Add comment...")
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
		m.textInput.Placeholder = tr("Add learning...")
		m.textInput.Focus()
		return m, textinput.Blink
	case fieldSubtasks:
		m.mode = modeAddSubtask
		m.textInput.SetValue("")
		m.textInput.Placeholder = tr("Add subtask...")
		m.textInput.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m model) detailDelete() (tea.Model, tea.Cmd) {
	t := m.currentTodo()
	if t == nil {
		return m, nil
	}
	if m.detail.page == 2 {
		if len(t.Comments) > 0 {
			m.mode = modeConfirmDeleteComment
			m.pendingComment = m.detail.commentCursor
			m.confirmMsg = tr("Delete this comment? (y/n)")
		}
		return m, nil
	}
	switch m.detail.field {
	case fieldProject:
		if t.Project != "" {
			m.mode = modeConfirmDeleteProject
			m.confirmMsg = fmt.Sprintf(tr("Remove project '%s' from this task? (y/n)"), t.Project)
		}
	case fieldNotes:
		if t.Notes != "" {
			m.pushUndo("clear notes", t.ID)
			t.SetNotes("")
			m.markModifiedNoUndo(t.ID)
		}
	case fieldTags:
		if len(t.Tags) > 0 && m.detail.tagCursor < len(t.Tags) {
			m.mode = modeConfirmDeleteTag
			m.pendingTag = m.detail.tagCursor
			m.confirmMsg = fmt.Sprintf(tr("Remove tag '#%s' from this task? (y/n)"), t.Tags[m.detail.tagCursor])
		}
	case fieldDependencies:
		if len(t.Dependencies) > 0 && m.detail.depCursor < len(t.Dependencies) {
			m.mode = modeConfirmDeleteDep
			m.pendingDep = m.detail.depCursor
			m.confirmMsg = tr("Remove this dependency? (y/n)")
		}
	case fieldLearnings:
		if len(t.Learnings) > 0 && m.detail.learningCursor < len(t.Learnings) {
			m.mode = modeConfirmDeleteLearning
			m.pendingLearning = m.detail.learningCursor
			m.confirmMsg = fmt.Sprintf(tr("Delete learning '%s'? (y/n)"), truncate(t.Learnings[m.detail.learningCursor].Text, 40))
		}
	case fieldSubtasks:
		if m.subtaskCount(t.ID) > 0 && m.detail.subtaskCursor < m.subtaskCount(t.ID) {
			subID := m.subtaskIDs(t.ID)[m.detail.subtaskCursor]
			subTitle := subID
			if sub := m.findTodoByID(subID); sub != nil {
				subTitle = sub.Title
			}
			m.mode = modeConfirmDeleteSubtask
			m.pendingSubtask = m.detail.subtaskCursor
			m.confirmMsg = fmt.Sprintf(tr("Delete subtask '%s'? (y/n)"), truncate(subTitle, 40))
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
			m.confirmMsg = fmt.Sprintf(tr("Delete learning '%s'? (y/n)"), truncate(learnings[m.learningCursor].Text, 40))
		}
	case "r":
		if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
			m.mode = modeEditLearning
			m.textInput.SetValue(learnings[m.learningCursor].Text)
			m.textInput.Placeholder = tr("Edit learning...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m model) startEditing() (tea.Model, tea.Cmd) {
	t := m.currentTodo()
	if t == nil {
		return m, nil
	}

	if m.detail.page == 2 {
		if len(t.Comments) > 0 {
			m.mode = modeEditComment
			m.pendingComment = m.detail.commentCursor
			m.textInput.SetValue(t.Comments[m.detail.commentCursor].Text)
			m.textInput.Placeholder = tr("Edit comment...")
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
		m.textInput.Placeholder = tr("Start date (dd-mm-yy, 'today', 'next week', '+3d')...")
		m.textInput.Focus()
	case fieldDueDate:
		m.mode = modeInput
		if !t.DueDate.IsZero() {
			m.textInput.SetValue(t.DueDate.Format("02-01-06"))
		} else {
			m.textInput.SetValue("")
		}
		m.textInput.Placeholder = tr("Due date (dd-mm-yy, 'today', 'next week', '+3d')...")
		m.textInput.Focus()
	case fieldPriority:
		switch t.Priority {
		case todo.PriorityLow:
			t.SetPriority(todo.PriorityMedium)
		case todo.PriorityMedium:
			t.SetPriority(todo.PriorityHigh)
		default:
			t.SetPriority(todo.PriorityLow)
		}
		m.markModified(t.ID)
		return m, nil
	case fieldSize:
		// Cycle Medium → Small → Large → Medium. Starts at Medium so the first
		// press moves toward "Small" (the small-task floor) — the direction
		// users will most often want.
		switch t.Size {
		case todo.SizeMedium:
			t.SetSize(todo.SizeSmall)
		case todo.SizeSmall:
			t.SetSize(todo.SizeLarge)
		default:
			t.SetSize(todo.SizeMedium)
		}
		m.markModified(t.ID)
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
			m.textInput.Placeholder = tr("Edit learning...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	case fieldSubtasks:
		// Enter on a subtask edits the title (matches comments + learnings).
		// 'd' still toggles done/pending.
		if m.subtaskCount(t.ID) > 0 && m.detail.subtaskCursor < m.subtaskCount(t.ID) {
			ids := m.subtaskIDs(t.ID)
			sub := m.findTodoByID(ids[m.detail.subtaskCursor])
			if sub == nil {
				return m, nil
			}
			m.mode = modeEditSubtask
			m.pendingSubtask = m.detail.subtaskCursor
			m.textInput.SetValue(sub.Title)
			m.textInput.Placeholder = tr("Edit subtask title...")
			m.textInput.Focus()
			return m, textinput.Blink
		}
	}
	return m, textinput.Blink
}
