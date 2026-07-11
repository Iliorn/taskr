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

	// ctrl+c quits from anywhere — every mode, both panes — after flushing any
	// mutation still inside the 300ms save debounce. Bubble Tea delivers it as
	// an ordinary key (there is no built-in quit), and previously only the list
	// pane's normal mode handled it, so ctrl+c in a modal or the detail pane
	// silently did nothing.
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		m.flushPendingWrites()
		return m, tea.Quit
	}

	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.termWidth = sz.Width
		m.termHeight = sz.Height
		m.invalidateDetailCache()
	}

	switch msg := msg.(type) {
	case clearErrMsg:
		m.err = ""
		m.errKind = toastError
		return m, nil
	case timerTickMsg:
		if m.anyTimerRunning() {
			// Heartbeat the running timer's last_seen at most once a minute so
			// the stale-timer recoverer never mistakes this live timer for an
			// abandoned one. recordSelfSave keeps the fs watcher from reloading
			// on our own write. The write itself runs as a tea.Cmd — off the
			// Update goroutine — so a busy DB (concurrent sync/CLI write inside
			// busy_timeout) can't freeze the UI for up to 5s.
			if time.Since(m.lastTimerHeartbeat) >= time.Minute {
				m.lastTimerHeartbeat = time.Now()
				// Keep the in-memory entries in step with the DB heartbeat —
				// see stampRunningTimersSeen for why saves depend on this.
				m.stampRunningTimersSeen(m.lastTimerHeartbeat)
				if m.watcher != nil {
					m.watcher.recordSelfSave()
				}
				return m, tea.Batch(timerTick(), func() tea.Msg {
					_ = heartbeatRunningTimers(db, time.Now())
					return nil
				})
			}
			return m, timerTick()
		}
		m.timerTickOn = false
		return m, nil
	case updateDoneMsg:
		if msg.err != nil {
			m.flashError(fmt.Sprintf("Update failed: %v", msg.err))
			m.updateStatus = tr("Update failed")
		} else {
			m.flashSuccess(tr("Updated! Restart taskr to apply."))
			m.updateStatus = tr("Updated — restart to apply")
		}
		return m, clearErrAfter()
	case updateCheckMsg:
		if msg.err != nil {
			m.flashError(fmt.Sprintf("Update check failed: %v", msg.err))
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
	case syncTickMsg:
		cmds := []tea.Cmd{syncTick()}
		if m.autoSync {
			cmds = append(cmds, m.backgroundSync())
			// Mid-session enable: start the real-time listener if sync was just
			// turned on (it isn't running yet) and arm its reader once.
			if m.liveSync == nil {
				if ls := startLiveSync(m.syncCfg); ls != nil {
					m.liveSync = ls
					cmds = append(cmds, waitForSyncEvent(ls.C))
				}
			}
		}
		if p := m.probeServer(); p != nil {
			cmds = append(cmds, p)
		}
		return m, tea.Batch(cmds...)
	case syncEventMsg:
		// Server signalled a change. Re-arm the listener and pull now.
		var cmds []tea.Cmd
		if m.liveSync != nil {
			cmds = append(cmds, waitForSyncEvent(m.liveSync.C))
		}
		if m.autoSync {
			cmds = append(cmds, m.backgroundSync())
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)
	case syncDoneMsg:
		return m.handleSyncDone(msg)
	case serverProbeMsg:
		// Only flag "external" when we aren't the one serving in-process.
		m.serverExternal = msg.reachable && m.inprocServer == nil
		return m, nil
	case saveErrMsg:
		m.flashError(fmt.Sprintf("Error saving tasks: %v", msg.err))
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
			m.flashError(fmt.Sprintf("External reload failed: %v", msg.err))
			return m, clearErrAfter()
		}
		// Atomic swap: rebuild the Store from the freshly-loaded task set,
		// invalidate caches, and follow the same task ID across the new
		// ordering so the cursor stays anchored where the user expected.
		//
		// The swap must not wipe what only exists in memory: the undo stack,
		// and any mutation still inside the save debounce (dirty tasks and
		// pending tombstones the snapshot predates). Those local changes are
		// newer than anything on disk — overlay them on the loaded set and
		// carry the change set across so the scheduled save still flushes it.
		taskID := m.currentTaskID()
		undo := m.undoStack
		dirtyIDs := m.dirtyIDs
		tombstones := m.tombstones
		dirtyTasks := make(map[string]todo.Todo, len(dirtyIDs))
		for id := range dirtyIDs {
			if t := m.get(id); t != nil {
				dirtyTasks[id] = copyTodo(*t)
			}
		}
		m.Store = Store{}
		m.Store.ensureTasks()
		m.undoStack = undo
		m.dirtyIDs = dirtyIDs
		m.tombstones = tombstones
		for i := range msg.todos {
			t := msg.todos[i]
			if _, dead := tombstones[t.ID]; dead {
				continue // deleted locally, deletion not yet flushed — stays dead
			}
			if d, ok := dirtyTasks[t.ID]; ok {
				t = d // unsaved local edit is newer than the DB snapshot
			}
			m.Store.add(t)
		}
		// Dirty tasks the snapshot doesn't know yet (created locally, unflushed).
		for id, d := range dirtyTasks {
			if m.get(id) == nil {
				m.Store.add(d)
			}
		}
		m.markCacheDirty()
		m.refreshCaches()
		m.followTask(taskID)
		return m, nil
	}

	// All handler paths feed through the common tail below so the dirty
	// flag set by a modal mutation (add task, confirm delete, edit title,
	// etc.) schedules the 300ms save immediately — not on the next
	// keystroke, which previously left a window where quitting between
	// the modal Enter and any subsequent key would lose the change.
	var newModel tea.Model
	var cmd tea.Cmd
	switch m.mode {
	case modeHelp:
		newModel, cmd = m.updateHelp(msg)
	case modeConfirm:
		newModel, cmd = m.updateConfirm(msg)
	case modeConfirmUpdate:
		newModel, cmd = m.updateConfirmUpdate(msg)
	case modeEditTimeEntry:
		newModel, cmd = m.updateEditTimeEntry(msg)
	case modeIdlePrompt:
		newModel, cmd = m.updateIdlePrompt(msg)
	case modeInput:
		newModel, cmd = m.updateInput(msg)
	case modeEditComment:
		newModel, cmd = m.updateEditComment(msg)
	case modeEditTag:
		newModel, cmd = m.updateEditTag(msg)
	case modeEditTitle:
		newModel, cmd = m.updateEditTitle(msg)
	case modeEditProjectInline:
		newModel, cmd = m.updateEditProjectInline(msg)
	case modeEditSyncURL:
		newModel, cmd = m.updateEditSyncURL(msg)
	case modeEditSyncToken:
		newModel, cmd = m.updateEditSyncToken(msg)
	case modeEditServerListen:
		newModel, cmd = m.updateEditServerListen(msg)
	case modeEditServerToken:
		newModel, cmd = m.updateEditServerToken(msg)
	case modeEditLearning:
		newModel, cmd = m.updateEditLearning(msg)
	case modeAddLearning:
		newModel, cmd = m.updateAddLearning(msg)
	case modeAddSubtask:
		newModel, cmd = m.updateAddSubtask(msg)
	case modeEditSubtask:
		newModel, cmd = m.updateEditSubtask(msg)
	case modeAddTimeEntry:
		newModel, cmd = m.updateAddTimeEntry(msg)
	case modeSearch:
		newModel, cmd = m.updateSearch(msg)
	case modeSearchDep:
		newModel, cmd = m.updateSearchDep(msg)
	case modeSearchTag:
		newModel, cmd = m.updateSearchTag(msg)
	case modeSearchProject:
		newModel, cmd = m.updateSearchProject(msg)
	case modeSearchTagTab:
		newModel, cmd = m.updateSearchTagTab(msg)
	default:
		if m.pane == paneList {
			newModel, cmd = m.updateList(msg)
		} else {
			newModel, cmd = m.updateDetail(msg)
		}
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
		m.flashError(fmt.Sprintf("Error writing notes file: %v", err))
		return clearErrAfter()
	}

	editorCmd := resolveEditorCmd()
	if editorCmd == "" {
		if runtime.GOOS == "windows" {
			m.flashError(tr("No editor found — set EDITOR permanently, e.g: setx EDITOR notepad (then restart taskr)"))
		} else {
			m.flashError(tr("No editor found — set $EDITOR permanently, e.g: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish"))
		}
		return clearErrAfter()
	}

	m.editorTaskID = taskID
	m.editorToInput = false
	return execEditor(editorCmd, taskID, false)
}

// openEditorForInput backs the ctrl+e escape hatch from the single-line
// comment/learning inputs: it seeds $EDITOR with the current draft and, on
// return, reloads the edited text into the input (handleEditorFinished), leaving
// the existing Enter path to commit it. That reuse is why it doesn't duplicate
// any of the add/edit commit logic.
func (m *model) openEditorForInput() tea.Cmd {
	if err := writeNotesFile(editorDraftKey, m.textInput.Value()); err != nil {
		m.flashError(fmt.Sprintf("Error writing draft file: %v", err))
		return clearErrAfter()
	}

	editorCmd := resolveEditorCmd()
	if editorCmd == "" {
		if runtime.GOOS == "windows" {
			m.flashError(tr("No editor found — set EDITOR permanently, e.g: setx EDITOR notepad (then restart taskr)"))
		} else {
			m.flashError(tr("No editor found — set $EDITOR permanently, e.g: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish"))
		}
		return clearErrAfter()
	}

	m.editorTaskID = editorDraftKey
	m.editorToInput = true
	return execEditor(editorCmd, editorDraftKey, false)
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
				m.flashError(tr("Editor failed — falling back to notepad"))
				return m, tea.Batch(clearErrAfter(), execEditor(notepad, msg.taskID, true))
			}
		}
		m.flashError(fmt.Sprintf("Editor exited with error: %v", msg.err))
		return m, clearErrAfter()
	}
	taskID := msg.taskID
	content, err := readNotesFile(taskID)
	if err != nil {
		m.flashError(fmt.Sprintf("Error reading notes: %v", err))
		return m, clearErrAfter()
	}

	// ctrl+e escape hatch: the content is a comment/learning draft, not notes —
	// reload it into the active input (collapsing the editor's newlines, since
	// these are single-line fields) and let Enter commit it as usual.
	if m.editorToInput {
		m.editorToInput = false
		cleanupNotesFile(taskID)
		m.editorTaskID = ""
		v := strings.TrimSpace(content)
		v = strings.ReplaceAll(v, "\r\n", " ")
		v = strings.ReplaceAll(v, "\n", " ")
		m.textInput.SetValue(v)
		m.textInput.CursorEnd()
		return m, nil
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
		m.flashInfo(tr("Nothing to undo"))
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
		m.touchRestored(restored)
		for _, id := range removed {
			m.markTombstone(id)
		}
		m.markModified(restored...)
		m.flashSuccess(fmt.Sprintf(tr("Undid: %s"), entry.desc))
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
	m.touchRestored(restoredIDs)
	for id := range before {
		m.markTombstone(id)
	}
	m.markModified(restoredIDs...)
	m.flashSuccess(fmt.Sprintf(tr("Undid: %s"), entry.desc))
	return clearErrAfter()
}

// touchRestored stamps ModifiedAt=now on each task an undo just restored. The
// restored state carries its original (old) ModifiedAt, and the sync merge is
// last-writer-wins by that timestamp — without the bump, the state being undone
// (a delete's tombstone with a newer DeletedAt, or an already-synced edit with a
// newer ModifiedAt) wins the merge and silently re-applies itself on the next
// sync. Stamping now makes the undo the latest writer, so it propagates.
func (m *model) touchRestored(ids []string) {
	for _, id := range ids {
		if t := m.get(id); t != nil {
			// Clamp against the live tombstone's deleted_at, not just the
			// restored snapshot's ModifiedAt: after a slow-clock delete both
			// collapse to the same prev+1ms, and an exact event-time tie
			// resolves by content hash (laterWins) — a coin flip the restore
			// could lose on devices that already received the tombstone via
			// the push sync. If the debounced save hasn't flushed the
			// tombstone yet, tombstoneDeletedAt reads the still-live row
			// (zero) and the clamp falls back to the snapshot stamp — fine,
			// since a tombstone that never reached the DB never syncs out.
			prev := t.ModifiedAt
			if d := tombstoneDeletedAt(db, t.ID); d.After(prev) {
				prev = d
			}
			t.ModifiedAt = todo.StampModified(prev)
		}
	}
}

// ── Help overlay ──────────────────────────────────────────────────────────────

func (m model) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "?", "esc", "q":
			m.mode = modeNormal
			m.helpScroll = 0
		case "up":
			m.helpScroll = clampHelpScroll(m.helpScroll-1, len(m.helpBodyLines()), m.helpViewportH())
		case "down":
			m.helpScroll = clampHelpScroll(m.helpScroll+1, len(m.helpBodyLines()), m.helpViewportH())
		case "pgup":
			m.helpScroll = clampHelpScroll(m.helpScroll-m.helpViewportH(), len(m.helpBodyLines()), m.helpViewportH())
		case "pgdown", " ":
			m.helpScroll = clampHelpScroll(m.helpScroll+m.helpViewportH(), len(m.helpBodyLines()), m.helpViewportH())
		}
	}
	return m, nil
}

// ── List pane ─────────────────────────────────────────────────────────────────

func (m model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q": // ctrl+c is handled globally in dispatch
			m.flushPendingWrites()
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
			m.switchTab(tabStats)
		case "7":
			m.switchTab(tabSettings)

		case "tab":
			m.switchTab((m.tab + 1) % numTabs)

		case "h":
			if m.tab == tabTasks {
				m.showHistory = !m.showHistory
				if m.showHistory {
					m.pushFocus(stateHistory)
				} else {
					m.dropFocus(stateHistory)
				}
				m.cursor = 0
				m.listOffset = 0
			}

		case "f":
			if m.tab == tabTasks && !m.showHistory {
				m.focusFilter = !m.focusFilter
				if m.focusFilter {
					m.pushFocus(stateFocusFilter)
				} else {
					m.dropFocus(stateFocusFilter)
				}
				m.cursor = 0
				m.listOffset = 0
				m.markFilterDirty()
			}

		case "right":
			if m.tab == tabCalendar {
				m.moveCalendarDay(1)
			} else if m.tab == tabSettings && m.isBiasSettingRow(m.settingsCursor) {
				m.cycleBias(m.settingsCursor, +1)
			} else if m.tab == tabSettings && m.settingsCursor == settingAging {
				m.toggleAging()
			} else if m.tab == tabSettings && m.settingsCursor == settingAutoCloseParent {
				m.toggleAutoCloseParent()
			} else if m.tab == tabSettings && m.settingsCursor == settingAutoCloseSubtasks {
				m.toggleAutoCloseSubtasks()
			} else if m.tab == tabSettings && m.settingsCursor == settingTheme {
				m.cycleTheme(1)
			} else if m.tab == tabSettings && m.settingsCursor == settingLanguage {
				m.cycleLang(1)
			} else if m.tab == tabSettings && m.settingsCursor == settingSyncAuto {
				m.toggleSyncAuto()
			} else if m.tab == tabSettings && m.settingsCursor == settingServerOn {
				m.toggleServer()
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
			} else if m.tab == tabSettings && m.settingsCursor == settingAging {
				m.toggleAging()
			} else if m.tab == tabSettings && m.settingsCursor == settingAutoCloseParent {
				m.toggleAutoCloseParent()
			} else if m.tab == tabSettings && m.settingsCursor == settingAutoCloseSubtasks {
				m.toggleAutoCloseSubtasks()
			} else if m.tab == tabSettings && m.settingsCursor == settingTheme {
				m.cycleTheme(-1)
			} else if m.tab == tabSettings && m.settingsCursor == settingLanguage {
				m.cycleLang(-1)
			} else if m.tab == tabSettings && m.settingsCursor == settingSyncAuto {
				m.toggleSyncAuto()
			} else if m.tab == tabSettings && m.settingsCursor == settingServerOn {
				m.toggleServer()
			} else if m.tab == tabTasks && !m.showHistory {
				if t := m.currentTodo(); t != nil {
					// On a subtask: collapse the containing parent and
					// return the cursor to it, so ← always "moves out"
					// of the unfolded region.
					parentID := t.ID
					if t.ParentID != "" {
						parentID = t.ParentID
					}
					delete(m.expandedTasks, parentID)
					m.followTask(parentID)
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
			} else if m.tab == tabTasks {
				if t := m.currentTodo(); t != nil {
					// History view: only allow stopping a running
					// timer — a done task shouldn't accrue new tracked
					// time. Recovery path for tasks marked done while
					// the timer was still running.
					if m.showHistory && !t.IsTimerRunning() {
						return m, nil
					}
					if e := t.RunningEntry(); e != nil && time.Since(e.StartedAt) > idleThreshold {
						m.openIdlePrompt(t)
						return m, nil
					}
					// Capture t plus any currently-running other task
					// (toggleTimer stops it when starting a new one) so undo
					// can restore both sides.
					undoIDs := []string{t.ID}
					if !t.IsTimerRunning() {
						for otherID := range m.runningTimers {
							if otherID != t.ID {
								undoIDs = append(undoIDs, otherID)
							}
						}
					}
					m.pushUndo("toggle timer", undoIDs...)
					m.toggleTimer(t)
					m.markModified(t.ID)
					if !m.timerTickOn && m.anyTimerRunning() {
						m.timerTickOn = true
						return m, timerTick()
					}
				}
			}

		case "T":
			// Manual time entry — log work that wasn't captured by the live
			// timer. Available on the Tasks tab so the user can backfill
			// before marking a task done.
			if m.tab == tabTasks && !m.showHistory {
				if t := m.currentTodo(); t != nil {
					m.pendingEntryTaskID = t.ID
					m.mode = modeAddTimeEntry
					m.textInput.SetValue("")
					m.textInput.Placeholder = tr("Time spent (45m, 1h30m) or HH:MM-HH:MM…")
					m.textInput.Focus()
					return m, textinput.Blink
				}
			}

		case "/":
			return m.startSearch()

		case "s":
			m.cycleSortMode()

		case "esc":
			m.popFocus()

		case "up":
			m.moveCursorUp()
		case "down":
			m.moveCursorDown()
		case "home":
			m.listJumpTop()
		case "end":
			m.listJumpBottom()
		case "pgup":
			m.listPage(-1)
		case "pgdown":
			m.listPage(1)

		case "enter":
			return m.handleListEnter()

		case "r":
			return m.handleListRename()

		case "m":
			// Merge tag — Tags tab only. Opens the same editor as rename but
			// with an empty input and a "Merge into…" placeholder, so the
			// user understands they're picking a *target* tag. The save
			// path (renameTagGlobally) is merge-aware: if the typed name
			// matches an existing tag, all tasks switch over and the source
			// tag disappears.
			if m.tab == tabTags {
				if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) && tags[m.tagTabCursor] != untaggedKey {
					m.editingTagName = tags[m.tagTabCursor]
					m.mode = modeEditTag
					m.textInput.SetValue("")
					m.textInput.Placeholder = fmt.Sprintf(tr("Merge #%s into…"), tags[m.tagTabCursor])
					m.textInput.Focus()
					return m, textinput.Blink
				}
			}

		case "x", "delete":
			return m.handleListDelete()

		case "a":
			if m.tab == tabTasks && !m.showHistory {
				m.mode = modeInput
				m.textInput.SetValue("")
				// Syntax lives in the persistent hint line under the input
				// (buildFooterContent) — a placeholder vanishes on the first
				// keystroke, exactly when the syntax reference is needed.
				m.textInput.Placeholder = tr("New task...")
				m.textInput.Focus()
				return m, textinput.Blink
			}

		case "d":
			if m.tab == tabTasks {
				if t := m.currentTodo(); t != nil {
					// Closing a task while its timer is running would
					// leave a dangling open entry — and the runningTimers
					// index would go stale. Stop first, then toggle.
					// Mirrors the CLI done path.
					if t.Status == todo.Pending && t.IsTimerRunning() {
						m.stopTimer(t.ID)
					}
					isSub := t.ParentID != ""
					wasPending := t.Status == todo.Pending
					// Un-marking a done task is a state change the user
					// rarely means (usually a stray 'd' on a completed row)
					// and it voids the completion rank — so confirm it.
					// Marking done stays immediate.
					if !wasPending {
						m.pendingReopenID = t.ID
						m.mode = modeConfirm
						m.confirmOnYes = (*model).confirmReopen
						m.confirmMsg = fmt.Sprintf(tr("Move '%s' to active? (y/n)"), truncate(t.Title, 40))
						return m, nil
					}
					// Pending parent with open subtasks: with auto-close-subtasks
					// on, cascade them closed; otherwise stage a confirm rather
					// than silently close (and hide) the open work.
					cascadeSubs := false
					if wasPending && !isSub {
						if done, total := m.subtaskProgress(t.ID); total > 0 && done < total {
							if m.autoCloseSubtasks {
								cascadeSubs = true
							} else {
								m.pendingCloseParentID = t.ID
								m.mode = modeConfirm
								m.confirmOnYes = (*model).confirmCloseParent
								m.confirmMsg = fmt.Sprintf(tr("Close '%s' with %d open subtask(s)? (y/n)"), truncate(t.Title, 40), total-done)
								return m, nil
							}
						}
					}
					// Full snapshot: ancestor cascade + recurrence spawn can
					// touch arbitrary IDs not knowable until mid-mutation, so
					// capture all state for a clean undo.
					if wasPending && (isSub || cascadeSubs || t.IsRecurring()) {
						m.pushUndo("close task")
					} else {
						m.pushUndo("toggle done", t.ID)
					}
					if wasPending {
						captureSeqRankAtDone(m.allTodos(), t)
					}
					t.Toggle()
					ids := []string{t.ID}
					if wasPending && t.IsRecurring() {
						if newID := m.spawnNextRecurrence(t); newID != "" {
							ids = append(ids, newID)
						}
					}
					if wasPending && isSub {
						ids = append(ids, m.autoCloseAncestorsIfAllDone(t.ID)...)
					}
					if cascadeSubs {
						ids = append(ids, m.closePendingSubtree(t.ID)...)
					}
					m.markModified(ids...)
					// Subtasks stay visible after toggling (dimmed with a
					// check), so the cursor stays on the same row. Parents
					// disappear from active and the cursor would land on
					// the next row — decrement so it lands on the previous
					// one instead.
					if !isSub && m.cursor > 0 {
						m.cursor--
					}
				}
			}

		case "p":
			if m.tab == tabTasks && !m.showHistory {
				if t := m.currentTodo(); t != nil {
					m.pushUndo("cycle priority", t.ID)
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
			m.clampListOffset(m.visibleActiveLen())
		}
	case tabProjects:
		if m.projectTaskMode {
			// Drilled in: clamp against projectDrillTaskVisibleRows so the window
			// size matches the renderer (which subtracts 1 row for the header).
			m.clampListOffsetVisible(m.cursor, m.currentProjectTaskLen(), m.projectDrillTaskVisibleRows())
		} else {
			m.clampListOffsetVisible(m.projectCursor, len(m.allProjectsForList()), m.projectListVisibleRows())
		}
	case tabTags:
		m.clampListOffsetFor(m.tagTabCursor, len(m.getFilteredTagsForTab()))
	case tabLearnings:
		m.clampListOffsetFor(m.learningCursor, len(m.allLearnings()))
	}
	return m, nil
}

// ── List helper methods ───────────────────────────────────────────────────────

func (m *model) switchTab(t tab) {
	if t == m.tab {
		return
	}
	// Snapshot the leaving tab's shared UI state and restore the entering
	// tab's, so a tab switch is non-destructive: your cursor, scroll, open
	// pane, and search survive a detour to another tab. Tab-private state
	// (projectCursor, tagTabCursor, showHistory, the calendar day, …) lives in
	// its own fields and persists on its own — no longer zeroed here.
	m.tabViews[m.tab] = tabView{
		cursor:     m.cursor,
		listOffset: m.listOffset,
		pane:       m.pane,
		search:     m.searchQuery,
	}
	m.tab = t
	v := m.tabViews[t]
	m.cursor = v.cursor
	m.listOffset = v.listOffset
	m.pane = v.pane
	m.searchQuery = v.search

	m.invalidateDetailCache()
	m.markFilterDirty()
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
	case tabTasks, tabProjects, tabStats:
		// Stats shares the Tasks-list query: renderStatsList aggregates only
		// the matching top-level tasks, so a #tag or @project search scopes
		// every stat block on the page.
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
		// Four-state cycle: Alpha → Count → Progress → Recent → Alpha.
		switch m.tagSort {
		case tagSortAlpha:
			m.tagSort = tagSortCount
		case tagSortCount:
			m.tagSort = tagSortProgress
		case tagSortProgress:
			m.tagSort = tagSortRecent
		default:
			m.tagSort = tagSortAlpha
		}
		m.tagTabCursor = 0
		m.sortCachedTags()
	case tabTasks:
		if m.showHistory {
			// History has its own two-state cycle: Completed → Alpha.
			if m.historySort == historySortCompleted {
				m.historySort = historySortAlpha
			} else {
				m.historySort = historySortCompleted
			}
		} else {
			// Three-state cycle: Sequence → DueDate → Size → Sequence.
			switch m.taskSort {
			case taskSortSequence:
				m.taskSort = taskSortDueDate
			case taskSortDueDate:
				m.taskSort = taskSortSize
			default:
				m.taskSort = taskSortSequence
			}
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
		m.flashError(fmt.Sprintf(tr("Score resync failed: %v"), err))
	}
}

// toggleAging flips the Aging contribution on/off, mirroring cycleBias's
// invalidate-persist-resync pattern so the new ranking is visible immediately
// and persists across restarts.
func (m *model) toggleAging() {
	activeBiases.Aging = !activeBiases.Aging
	m.markCacheDirty()
	m.persistSettings()
	if err := m.repo.ResyncScores(); err != nil {
		m.flashError(fmt.Sprintf(tr("Score resync failed: %v"), err))
	}
}

// toggleAutoCloseParent flips the "close parent when all subtasks done"
// preference. Doesn't retroactively close already-complete subtrees — only
// future subtask transitions trigger the auto-close.
func (m *model) toggleAutoCloseParent() {
	m.autoCloseParent = !m.autoCloseParent
	m.persistSettings()
}

// toggleAutoCloseSubtasks flips the "close subtasks when the parent is closed"
// preference. Like its mirror, it's forward-only — it doesn't retroactively
// close subtasks already stranded under a done parent.
func (m *model) toggleAutoCloseSubtasks() {
	m.autoCloseSubtasks = !m.autoCloseSubtasks
	m.persistSettings()
}

// persistSettings writes all current preferences to disk, surfacing any write
// failure so a setting that silently won't stick is at least visible.
func (m *model) persistSettings() {
	if err := saveSettings(appSettings{
		TaskSort:          m.taskSort,
		HistorySort:       m.historySort,
		TagSort:           m.tagSort,
		LearningSort:      m.learningSort,
		Theme:             m.themeName,
		Language:          string(activeLang),
		SeqBiasDeadline:   activeBiases.Deadline,
		SeqBiasPriority:   activeBiases.Priority,
		SeqBiasMomentum:   activeBiases.Momentum,
		SeqAgingDisabled:  !activeBiases.Aging,
		AutoCloseParent:   m.autoCloseParent,
		AutoCloseSubtasks: m.autoCloseSubtasks,
	}); err != nil {
		m.flashError(fmt.Sprintf(tr("Error saving settings: %v"), err))
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
		if n := len(m.getFilteredTagsForTab()); n > 0 {
			m.tagTabCursor = (m.tagTabCursor - 1 + n) % n
		}
	case tabLearnings:
		if n := len(m.allLearnings()); n > 0 {
			m.learningCursor = (m.learningCursor - 1 + n) % n
		}
	case tabSettings:
		m.settingsCursor = settingsCursorStep(m.settingsCursor, -1)
	case tabProjects:
		if m.projectTaskMode {
			if n := m.currentProjectTaskLen(); n > 0 {
				m.cursor = (m.cursor - 1 + n) % n
			}
		} else if n := len(m.allProjectsForList()); n > 0 {
			m.projectCursor = (m.projectCursor - 1 + n) % n
			m.cursor = 0
			m.listOffset = 0
		}
	case tabTasks:
		if n := m.currentTaskListLen(); n > 0 {
			m.cursor = (m.cursor - 1 + n) % n
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
		if n := len(m.getFilteredTagsForTab()); n > 0 {
			m.tagTabCursor = (m.tagTabCursor + 1) % n
		}
	case tabLearnings:
		if n := len(m.allLearnings()); n > 0 {
			m.learningCursor = (m.learningCursor + 1) % n
		}
	case tabSettings:
		m.settingsCursor = settingsCursorStep(m.settingsCursor, +1)
	case tabProjects:
		if m.projectTaskMode {
			if n := m.currentProjectTaskLen(); n > 0 {
				m.cursor = (m.cursor + 1) % n
			}
		} else if n := len(m.allProjectsForList()); n > 0 {
			m.projectCursor = (m.projectCursor + 1) % n
			m.cursor = 0
			m.listOffset = 0
		}
	case tabTasks:
		if n := m.currentTaskListLen(); n > 0 {
			m.cursor = (m.cursor + 1) % n
		}
	}
}

// listNavTarget abstracts the current tab's linear list for the jump/page keys
// (Home/End/PgUp/PgDn): a pointer to the tab's cursor and the row count.
// Returns (nil, 0) for tabs without a simple linear list (calendar, settings,
// stats), where those keys are a no-op.
func (m *model) listNavTarget() (*int, int) {
	switch m.tab {
	case tabTasks:
		return &m.cursor, m.currentTaskListLen()
	case tabTags:
		return &m.tagTabCursor, len(m.getFilteredTagsForTab())
	case tabLearnings:
		return &m.learningCursor, len(m.allLearnings())
	case tabProjects:
		if m.projectTaskMode {
			return &m.cursor, m.currentProjectTaskLen()
		}
		return &m.projectCursor, len(m.allProjectsForList())
	}
	return nil, 0
}

// moveListCursorTo clamps target into range and applies it to the current tab's
// list cursor. In the Projects list (not task mode) picking a different project
// resets the task sub-cursor + its scroll, mirroring moveCursorUp/Down.
func (m *model) moveListCursorTo(target int) {
	c, n := m.listNavTarget()
	if c == nil || n == 0 {
		return
	}
	if target < 0 {
		target = 0
	}
	if target > n-1 {
		target = n - 1
	}
	if m.tab == tabProjects && !m.projectTaskMode && target != *c {
		m.cursor = 0
		m.listOffset = 0
	}
	*c = target
}

// listPageStep is roughly one visible page for PgUp/PgDn, keeping one row of
// overlap for context.
func (m *model) listPageStep() int {
	step := m.listVisible() - 1
	if step < 1 {
		step = 1
	}
	return step
}

func (m *model) listJumpTop() { m.moveListCursorTo(0) }

func (m *model) listJumpBottom() {
	_, n := m.listNavTarget()
	m.moveListCursorTo(n - 1)
}

func (m *model) listPage(dir int) {
	if c, _ := m.listNavTarget(); c != nil {
		m.moveListCursorTo(*c + dir*m.listPageStep())
	}
}

// currentTaskListLen is the row count of the active Tasks list — the history
// list when showing history, otherwise the visible active list.
func (m *model) currentTaskListLen() int {
	if m.showHistory {
		return len(m.cache.done)
	}
	return m.visibleActiveLen()
}

// currentProjectTaskLen is the task count of the project under the project
// cursor (used while drilled into a project's task list).
func (m *model) currentProjectTaskLen() int {
	projects := m.allProjectsForList()
	if m.projectCursor < 0 || m.projectCursor >= len(projects) {
		return 0
	}
	return len(m.getProjectTasks(projects[m.projectCursor]))
}

func (m model) handleListEnter() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabCalendar:
		if len(m.activitiesForDay(m.calendar.selected)) > 0 {
			m.calendar.focusTimeline = true
			m.calendar.entryCursor = 0
			m.pushFocus(stateCalTimeline)
		}
	case tabLearnings:
		if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
			m.pane = paneDetail
			m.pushFocus(stateDetailPane)
		}
	case tabProjects:
		if !m.projectTaskMode {
			if projects := m.allProjectsForList(); m.projectCursor < len(projects) {
				m.projectTaskMode = true
				m.cursor = 0
				m.pushFocus(stateProjectDrill)
			}
		} else if m.currentTodo() != nil {
			m.pane = paneDetail
			m.detail = detailState{field: fieldStartDate}
			m.invalidateDetailCache()
			m.pushFocus(stateDetailPane)
		}
	case tabTasks:
		if m.currentTodo() != nil {
			m.pane = paneDetail
			m.detail = detailState{field: fieldStartDate}
			m.invalidateDetailCache()
			m.pushFocus(stateDetailPane)
		}
	case tabTags:
		if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) {
			tag := tags[m.tagTabCursor]
			m.switchTab(tabTasks)
			// Drilling in applies a fresh filter, so start at the top rather
			// than the Tasks cursor switchTab just restored.
			if tag == untaggedKey {
				m.searchQuery = untaggedKey
			} else {
				m.searchQuery = "#" + tag
			}
			// switchTab already moved us to Tasks, so the entry lands on
			// the tab the search filters.
			m.pushFocus(stateSearch)
			m.cursor = 0
			m.listOffset = 0
			m.markFilterDirty()
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
	case settingAging:
		m.toggleAging()
	case settingAutoCloseParent:
		m.toggleAutoCloseParent()
	case settingAutoCloseSubtasks:
		m.toggleAutoCloseSubtasks()
	case settingTheme:
		m.cycleTheme(1)
	case settingLanguage:
		m.cycleLang(1)
	case settingSyncAuto:
		m.toggleSyncAuto()
	case settingSyncServer:
		m.mode = modeEditSyncURL
		m.textInput.SetValue(m.syncCfg.URL)
		m.textInput.Placeholder = tr("Sync server URL, e.g. http://100.x.y.z:8765")
		m.textInput.Focus()
		return m, textinput.Blink
	case settingSyncToken:
		m.mode = modeEditSyncToken
		m.textInput.SetValue(m.syncCfg.Token)
		// Mask the pre-filled secret — the list row shows "•••• set" but the
		// editor used to echo it back in plaintext. The editors reset EchoMode
		// on exit so the shared input doesn't stay masked for other modes.
		m.textInput.EchoMode = textinput.EchoPassword
		m.textInput.Placeholder = tr("Sync token (clear the field to remove it)")
		m.textInput.Focus()
		return m, textinput.Blink
	case settingSyncNow:
		if !m.syncCfg.ready() {
			m.syncStatus = tr("Set sync server + token first")
			return m, nil
		}
		m.syncStatus = tr("Syncing…")
		return m, m.backgroundSync()
	case settingServerOn:
		m.toggleServer()
	case settingServerListen:
		m.mode = modeEditServerListen
		m.textInput.SetValue(m.syncCfg.listenAddr())
		m.textInput.Placeholder = tr("Bind address, e.g. 100.x.y.z:8765 or 127.0.0.1:8765")
		m.textInput.Focus()
		return m, textinput.Blink
	case settingServerToken:
		m.mode = modeEditServerToken
		m.textInput.SetValue(m.syncCfg.ServerToken)
		m.textInput.EchoMode = textinput.EchoPassword // see settingSyncToken
		m.textInput.Placeholder = tr("Server token clients must present (clear the field to remove it)")
		m.textInput.Focus()
		return m, textinput.Blink
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
	m.searchInput.Placeholder = tr("Search... (#tag @project p:high due:<fri)")
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
				m.mode = modeConfirm
				m.confirmOnYes = (*model).confirmDeleteTimeEntry
				m.pendingEntryTaskID = a.taskID
				m.pendingEntryID = a.entryID
				m.confirmMsg = fmt.Sprintf(tr("Delete %s entry for '%s'? (y/n)"),
					formatDuration(a.duration()), truncate(a.title, 30))
			}
		}
	case tabTags:
		if tags := m.getFilteredTagsForTab(); m.tagTabCursor < len(tags) && tags[m.tagTabCursor] != untaggedKey {
			m.mode = modeConfirm
			m.confirmOnYes = (*model).confirmDeleteTagGlobal
			m.confirmMsg = fmt.Sprintf(tr("Delete tag '#%s' from ALL tasks? (y/n)"), tags[m.tagTabCursor])
		}
	case tabTasks:
		if t := m.currentTodo(); t != nil {
			m.mode = modeConfirm
			m.confirmOnYes = (*model).confirmDeleteTask
			m.pendingDeleteID = t.ID
			if n := len(m.descendantIDs(t.ID)) - 1; n > 0 {
				m.confirmMsg = fmt.Sprintf(tr("Delete '%s' and %d subtask(s)? (y/n)"), t.Title, n)
			} else {
				m.confirmMsg = fmt.Sprintf(tr("Delete '%s'? (y/n)"), t.Title)
			}
		}
	case tabLearnings:
		if learnings := m.allLearnings(); m.learningCursor < len(learnings) {
			m.mode = modeConfirm
			m.confirmOnYes = (*model).confirmDeleteLearning
			m.pendingLearning = m.learningCursor
			m.confirmMsg = fmt.Sprintf(tr("Delete learning '%s'? (y/n)"), truncate(learnings[m.learningCursor].Text, 40))
		}
	}
	return m, nil
}
