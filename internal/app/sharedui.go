package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// sharedui.go is the app's side of shared projects (sharedproject.go): S on a
// Projects row shares it in a file or leaves it, Settings joins a file's
// project, and a background pass keeps every shared project in step with its
// file.
//
// The pass runs soon after each save, so a change reaches the file within
// seconds, and on a poll, so the others' changes arrive while nothing happens
// here. Its merge writes the store, which the file watcher turns into a
// reload, as it does a sync. It runs once more on quit.

const (
	sharedPollInterval = 30 * time.Second
	sharedDebounce     = 3 * time.Second
	// sharedMark follows a shared project's name on the Projects tab.
	sharedMark = " ⇄"
)

// sharedPollMsg is the poll's tick, which schedules the next; sharedSoonMsg
// is a one-off pass after a save, a share or a join.
type sharedPollMsg struct{}
type sharedSoonMsg struct{}

type sharedDoneMsg struct {
	changed bool
	err     error
}

func sharedPoll() tea.Cmd {
	return tea.Tick(sharedPollInterval, func(time.Time) tea.Msg { return sharedPollMsg{} })
}

// sharedSoon schedules a pass shortly, for a change just saved.
func (m *model) sharedSoon() tea.Cmd {
	if len(m.shared.Projects) == 0 || m.sharedScheduled {
		return nil
	}
	m.sharedScheduled = true
	return tea.Tick(sharedDebounce, func(time.Time) tea.Msg { return sharedSoonMsg{} })
}

// handleSharedTick starts a pass unless one is running. Only the poll's own
// tick schedules the next poll, so a pass after a save does not start a
// second chain of them.
func (m model) handleSharedTick(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if _, poll := msg.(sharedPollMsg); poll {
		cmds = append(cmds, sharedPoll())
	} else {
		m.sharedScheduled = false
	}
	if len(m.shared.Projects) > 0 && !m.sharedRunning && db != nil {
		m.sharedRunning = true
		b := m.rank.Biases
		cmds = append(cmds, func() tea.Msg {
			changed, err := syncAllShared(db, b)
			return sharedDoneMsg{changed: changed, err: err}
		})
	}
	return m, tea.Batch(cmds...)
}

// handleSharedDone reports a failed pass once, on the first failure after a
// good one; the ones after it stay quiet, as sync's do. A merge that changed
// the store reloads it here only when no watcher will.
func (m model) handleSharedDone(msg sharedDoneMsg) (tea.Model, tea.Cmd) {
	m.sharedRunning = false
	if msg.err != nil {
		m.sharedStatus = msg.err.Error()
		if !m.sharedFailed {
			m.sharedFailed = true
			m.flashError(fmt.Sprintf(tr("Shared project: %v"), msg.err))
			return m, clearErrAfter()
		}
		return m, nil
	}
	m.sharedFailed, m.sharedStatus = false, ""
	if msg.changed && m.watcher == nil {
		repo := m.repo
		return m, func() tea.Msg {
			todos, err := repo.Load()
			return reloadedMsg{todos: todos, err: err}
		}
	}
	return m, nil
}

// flushShared brings the shared files up to date on quit, after the last
// save, which cannot wait for a tick.
func (m *model) flushShared() {
	if len(m.shared.Projects) == 0 || db == nil {
		return
	}
	if _, err := syncAllShared(db, m.rank.Biases); err != nil {
		fmt.Fprintf(os.Stderr, "Shared project sync on quit failed: %v\n", err)
	}
}

// runSharedNow starts a pass at once, after sharing or joining.
func (m *model) runSharedNow() tea.Cmd {
	return func() tea.Msg { return sharedSoonMsg{} }
}

// ── Sharing and leaving from the Projects tab ───────────────────────────────

// startShareOrLeave is S on a project row: a shared project asks whether to
// stop sharing it, any other opens the folder prompt.
func (m model) startShareOrLeave(name string) (tea.Model, tea.Cmd) {
	if _, ok := m.shared.find(name); ok {
		m.pendingProjectName = name
		m.mode = modeConfirm
		m.confirmOnYes = (*model).confirmLeaveShared
		m.confirmMsg = fmt.Sprintf(tr("Leave '%s'? Its tasks are removed from this device; the others keep theirs. (y/n)"), name)
		return m, nil
	}
	m.pendingProjectName = name
	m.mode = modeShareFolder
	m.textInput.SetValue("")
	m.textInput.Placeholder = fmt.Sprintf(tr("Folder to share '%s' in, e.g. in your OneDrive"), name)
	m.textInput.Focus()
	return m, textinput.Blink
}

// refuseSharedProjectEdit stops a rename or removal of a whole shared project.
// The name is what ties its tasks to the file on every device, so renaming it
// here would take every task out of the project for everyone.
func (m *model) refuseSharedProjectEdit(name string) (bool, tea.Cmd) {
	if _, ok := m.shared.find(name); !ok {
		return false, nil
	}
	m.flashError(fmt.Sprintf(tr("'%s' is shared; leave it before renaming or removing it"), name))
	return true, clearErrAfter()
}

// confirmLeaveShared leaves the project and removes its tasks here. Edits
// still inside the save debounce are written first, so the file gets them
// and no later save puts a removed task back; a pass over the files that
// is running could merge the tasks back in behind the removal, so the leave
// waits for it.
func (m *model) confirmLeaveShared() tea.Cmd {
	if m.sharedRunning {
		m.flashInfo(tr("A shared project is syncing; try again in a moment"))
		return clearErrAfter()
	}
	if dirty, tombstones := m.Store.drainDirty(); len(dirty) > 0 || len(tombstones) > 0 {
		m.savePending = false
		if err := m.repo.Save(dirty, tombstones); err != nil {
			m.flashError(fmt.Sprintf(tr("Shared project: %v"), err))
			return clearErrAfter()
		}
	}
	c := m.shared.clone()
	p, n, err := leaveShared(db, &c, m.pendingProjectName, m.rank.Biases)
	if err == nil {
		err = saveSharedConfig(c)
	}
	if err != nil {
		m.flashError(fmt.Sprintf(tr("Shared project: %v"), err))
		return clearErrAfter()
	}
	m.shared = c
	m.flashInfo(fmt.Sprintf(tr("Left '%s' and removed its %d task(s) here"), p.Name, n))
	repo := m.repo
	return tea.Batch(clearErrAfter(), func() tea.Msg {
		todos, err := repo.Load()
		return reloadedMsg{todos: todos, err: err}
	})
}

// updateShareFolder takes the folder to make a project's file in: tab
// completes a folder name, enter shares, esc leaves it unshared.
func (m model) updateShareFolder(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "tab":
			m.textInput.SetValue(completePath(m.textInput.Value(), true))
			m.textInput.CursorEnd()
			return m, nil
		case "enter":
			c := m.shared.clone()
			p, err := startSharing(&c, m.pendingProjectName, m.textInput.Value())
			if err == nil {
				err = saveSharedConfig(c)
			}
			if err != nil {
				m.flashError(fmt.Sprintf(tr("Shared project: %v"), err))
				return m, clearErrAfter()
			}
			m.shared = c
			m.mode = modeNormal
			m.markCacheDirty()
			m.flashSuccess(fmt.Sprintf(tr("Sharing '%s' in %s"), p.Name, exportFolderDisplay(p.File)))
			return m, tea.Batch(clearErrAfter(), m.runSharedNow())
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// ── Joining from Settings ────────────────────────────────────────────────────

func (m model) openShareJoin() (tea.Model, tea.Cmd) {
	m.mode = modeShareJoin
	m.textInput.SetValue("")
	m.textInput.Placeholder = tr("The .tjek file of a shared project")
	m.textInput.Focus()
	return m, textinput.Blink
}

// updateShareJoin takes the file to join; tab completes files and folders.
// Joining hands every task already filed under the project's name to
// everyone sharing it, so when there
// are any it asks first.
func (m model) updateShareJoin(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "tab":
			m.textInput.SetValue(completePath(m.textInput.Value(), false))
			m.textInput.CursorEnd()
			return m, nil
		case "enter":
			c := m.shared.clone()
			p, err := joinShared(&c, m.textInput.Value())
			if err != nil {
				m.flashError(fmt.Sprintf(tr("Shared project: %v"), err))
				return m, clearErrAfter()
			}
			m.pendingShareFile = p.File
			n := 0
			for _, t := range m.allTodos() {
				if t.Project == p.Name {
					n++
				}
			}
			if n > 0 {
				m.mode = modeConfirm
				m.confirmOnYes = (*model).confirmJoinShared
				m.confirmMsg = fmt.Sprintf(tr("You have %d task(s) in '%s'. Share them with everyone sharing it? (y/n)"), n, p.Name)
				return m, nil
			}
			m.mode = modeNormal
			return m, m.confirmJoinShared()
		case "esc":
			m.mode = modeNormal
			return m, nil
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *model) confirmJoinShared() tea.Cmd {
	c := m.shared.clone()
	p, err := joinShared(&c, m.pendingShareFile)
	if err == nil {
		err = saveSharedConfig(c)
	}
	if err != nil {
		m.flashError(fmt.Sprintf(tr("Shared project: %v"), err))
		return clearErrAfter()
	}
	m.shared = c
	m.markCacheDirty()
	m.flashSuccess(fmt.Sprintf(tr("Joined '%s'"), p.Name))
	return tea.Batch(clearErrAfter(), m.runSharedNow())
}

// sharedJoinDisplay is the Settings row's value: how many projects are shared
// here, or what enter does.
func (m model) sharedJoinDisplay() string {
	if n := len(m.shared.Projects); n > 0 {
		names := make([]string, n)
		for i, p := range m.shared.Projects {
			names[i] = p.Name
		}
		return strings.Join(names, ", ")
	}
	return tr("choose a file")
}
