package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// syncsettings.go is the Settings-tab surface for cross-device sync: the
// auto-sync toggle and the inline editors for the server URL and token. Config
// is the same ~/.taskr/sync.json that the CLI and auto-sync use (loadSyncConfig
// / saveSyncConfig), so a change here takes effect everywhere.

// saveSyncCfg persists the current sync config and refreshes the live auto-sync
// gate. A write failure surfaces on the error line.
func (m *model) saveSyncCfg() {
	m.autoSync = autoSyncEnabled(m.syncCfg)
	if err := saveSyncConfig(m.syncCfg); err != nil {
		m.err = fmt.Sprintf("Sync config save failed: %v", err)
	}
}

// toggleSyncAuto flips auto-sync on/off. It's a no-op (with a hint) until a
// server URL and token are set, since there's nothing to sync against yet.
func (m *model) toggleSyncAuto() {
	if !m.syncCfg.ready() {
		m.syncStatus = tr("Set sync server + token first")
		return
	}
	v := !autoSyncEnabled(m.syncCfg)
	m.syncCfg.AutoSync = &v
	m.saveSyncCfg()
}

// updateEditSyncURL handles the inline server-URL editor.
func (m model) updateEditSyncURL(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			// Save whatever's in the field, blank included: the editor is
			// pre-filled with the current value, so an empty field is a
			// deliberate clear, not an accidental no-op.
			m.syncCfg.URL = strings.TrimSpace(m.textInput.Value())
			m.saveSyncCfg()
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

// updateEditSyncToken handles the inline token editor. The field is pre-filled
// with the current token, so clearing it to blank clears the stored token.
func (m model) updateEditSyncToken(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			m.syncCfg.Token = strings.TrimSpace(m.textInput.Value())
			m.saveSyncCfg()
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
