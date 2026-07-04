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
		// Stale-device guard — same rule as the CLI path; the Settings footer
		// carries the pointer to the manual override.
		if gap, stale := staleSyncGap(time.Now()); stale {
			return syncDoneMsg{err: fmt.Errorf("paused: no sync in %s — run `taskr sync --accept-stale` in a shell to rejoin", shortDur(gap))}
		}
		sum, err := runClientSync(db, cfg, 20*time.Second)
		return syncDoneMsg{summary: sum, err: err}
	}
}

// handleSyncDone records the outcome in the Settings footer (m.syncStatus) and
// flashes a transient toast on the error line in two cases: when a conflict was
// auto-resolved (a local edit was superseded and logged), and on the first
// failure after a run of healthy syncs. Repeated failures stay quiet on the
// toast line (a network blip shouldn't nag) — the header sync glyph and the
// Settings footer carry the ongoing outage.
func (m model) handleSyncDone(msg syncDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.syncStatus = tr("Last sync failed: ") + truncate(msg.err.Error(), 60)
		firstFailure := !m.lastSyncFailed
		m.lastSyncFailed = true
		if firstFailure {
			m.err = tr("Sync failing — devices may be diverging (see Settings)")
			return m, clearErrAfter()
		}
		return m, nil
	}
	m.lastSyncFailed = false
	m.syncStatus = fmt.Sprintf(tr("Last sync: sent %d, received %d"), msg.summary.sent, msg.summary.received)
	if msg.summary.conflicts > 0 {
		m.err = fmt.Sprintf(tr("Sync: %d conflict(s) resolved — see ~/.taskr/sync.log"), msg.summary.conflicts)
		return m, clearErrAfter()
	}
	return m, nil
}
