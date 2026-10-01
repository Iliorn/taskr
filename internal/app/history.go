package app

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

// adoptSaved takes what a save wrote onto the tasks it handed back
// (saveDoneMsg) onto the live tasks: the history, with the event it recorded,
// and the authors it gave new comments and time entries, so both show without
// a reload. The history is a union, not a copy: the task may have been
// reloaded with newer events meanwhile, and the save's copy may be from an
// undo snapshot that lacked some.
func (m *model) adoptSaved(saved []*todo.Todo) {
	changed := false
	for _, s := range saved {
		t := m.get(s.ID)
		if t == nil {
			continue
		}
		if len(s.History) > 0 {
			if merged := todo.MergeHistory(t.History, s.History); len(merged) != len(t.History) {
				t.History = merged
				changed = true
			}
		}
		authors := make(map[string]string)
		for _, c := range s.Comments {
			authors[c.ID] = c.Author
		}
		for _, e := range s.TimeEntries {
			authors[e.ID] = e.Author
		}
		for i := range t.Comments {
			if c := &t.Comments[i]; c.Author == "" && authors[c.ID] != "" {
				c.Author = authors[c.ID]
				changed = true
			}
		}
		for i := range t.TimeEntries {
			if e := &t.TimeEntries[i]; e.Author == "" && authors[e.ID] != "" {
				e.Author = authors[e.ID]
				changed = true
			}
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
			m.refreshTimerScope()
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
