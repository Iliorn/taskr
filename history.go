package main

import (
	"os"
	"os/user"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Iliorn/tjek/todo"
)

// history.go is the app's side of a task's history (todo.Event): who the
// device's edits are signed as. The events themselves are written by the
// save (stamps.go) and drawn by the detail pane (view_history.go).

// authorName is the name this device's edits carry: TJEK_AUTHOR, so a
// script or an agent can sign as itself, then the Settings name, then the
// account's own.
func authorName(s appSettings) string {
	if n := strings.TrimSpace(os.Getenv("TJEK_AUTHOR")); n != "" {
		return n
	}
	if n := strings.TrimSpace(s.Name); n != "" {
		return n
	}
	return accountName()
}

// accountName is the logged-in account's full name, or its login name when
// it has none. A GECOS name carries its office and phone fields after
// commas, and a Windows login its domain before a backslash.
func accountName() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	if full, _, _ := strings.Cut(u.Name, ","); strings.TrimSpace(full) != "" {
		return strings.TrimSpace(full)
	}
	login := u.Username
	if i := strings.LastIndexByte(login, '\\'); i >= 0 {
		login = login[i+1:]
	}
	return login
}

// adoptSavedHistory takes the history a save read back (saveDoneMsg) onto the
// live tasks, so the event it recorded shows without a reload. A union, not a
// copy: the task may have been reloaded with newer events meanwhile, and the
// save's copy may be from an undo snapshot that lacked some.
func (m *model) adoptSavedHistory(saved []*todo.Todo) {
	changed := false
	for _, s := range saved {
		t := m.get(s.ID)
		if t == nil || len(s.History) == 0 {
			continue
		}
		merged := todo.MergeHistory(t.History, s.History)
		if len(merged) != len(t.History) {
			t.History = merged
			changed = true
		}
	}
	if changed {
		m.invalidateDetailCache()
	}
}

// updateEditName handles the Settings name editor. The field is pre-filled
// with the current name, so clearing it goes back to the account's.
func (m model) updateEditName(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			m.userName = strings.TrimSpace(m.textInput.Value())
			m.repo.SetAuthor(authorName(appSettings{Name: m.userName}))
			m.persistSettings()
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
