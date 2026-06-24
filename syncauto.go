package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// syncauto.go wires cross-device auto-sync into the Bubble Tea loop. The merge
// and transport live in syncclient.go; here we only schedule periodic syncs and
// surface a brief status when a conflict was auto-resolved. The DB write a sync
// performs is picked up by the existing filesystem watcher, which reloads the
// task list — so this code never touches m.todos directly.

const syncTickInterval = 180 * time.Second

type syncTickMsg struct{}

type syncDoneMsg struct {
	summary syncSummary
	err     error
}

func syncTick() tea.Cmd {
	return tea.Tick(syncTickInterval, func(time.Time) tea.Msg { return syncTickMsg{} })
}

// backgroundSync runs one fail-soft sync against the configured server, using
// the package-level store handle directly (independent of model state). If the
// merge changed anything on disk, the watcher reloads the UI.
func (m model) backgroundSync() tea.Cmd {
	cfg := m.syncCfg
	return func() tea.Msg {
		sum, err := runClientSync(db, cfg, 20*time.Second)
		return syncDoneMsg{summary: sum, err: err}
	}
}

// handleSyncDone flashes a transient status. Sync failures are intentionally
// quiet — a network blip shouldn't nag — so only resolved conflicts are
// surfaced, since they mean a local edit was superseded and logged.
func (m model) handleSyncDone(msg syncDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil && msg.summary.conflicts > 0 {
		m.err = fmt.Sprintf(tr("Sync: %d conflict(s) resolved — see ~/.taskr/sync.log"), msg.summary.conflicts)
		return m, clearErrAfter()
	}
	return m, nil
}
