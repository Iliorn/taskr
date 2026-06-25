package main

import (
	"net"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// syncserver.go is the Settings-tab surface for running THIS machine as a sync
// hub: the Server on/off toggle (an in-process endpoint while the TUI is open),
// inline editors for the listen address and server token, and a probe that
// detects an external `taskr serve` (e.g. a systemd service) so the row reads
// "external" instead of looking unconfigured.

func (c syncConfig) listenAddr() string {
	if c.ServerListen != "" {
		return c.ServerListen
	}
	return defaultServerListen
}

// toggleServer starts or stops the in-process sync server. Starting needs a
// server token; the outcome is reported in the Settings footer (m.syncStatus).
func (m *model) toggleServer() {
	if m.inprocServer != nil {
		_ = m.inprocServer.Close()
		m.inprocServer = nil
		if m.inprocStop != nil {
			m.inprocStop()
			m.inprocStop = nil
		}
		m.syncCfg.ServerOn = false
		m.saveSyncCfg()
		m.syncStatus = tr("Server stopped")
		return
	}
	if m.syncCfg.ServerToken == "" {
		m.syncStatus = tr("Set a server token first")
		return
	}
	listen := m.syncCfg.listenAddr()
	srv, stop, err := startSyncServer(listen, m.syncCfg.ServerToken)
	if err != nil {
		m.syncStatus = tr("Server: ") + err.Error()
		return
	}
	m.inprocServer = srv
	m.inprocStop = stop
	m.syncCfg.ServerOn = true
	m.syncCfg.ServerListen = listen
	m.saveSyncCfg()
	m.syncStatus = tr("Serving on ") + listen
}

func (m model) updateEditServerListen(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			// Blank clears (falls back to the default bind address); the editor
			// is pre-filled, so an empty field is a deliberate clear.
			m.syncCfg.ServerListen = strings.TrimSpace(m.textInput.Value())
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

func (m model) updateEditServerToken(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			// Pre-filled editor: blank is a deliberate clear of the server token.
			m.syncCfg.ServerToken = strings.TrimSpace(m.textInput.Value())
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

// ── External-server probe ─────────────────────────────────────────────────────

type serverProbeMsg struct{ reachable bool }

// probeServer checks whether something answers /v1/health at the configured
// listen address. It lets the Settings row show "external" when this machine
// runs the headless `taskr serve` (e.g. a systemd service) rather than the
// in-process one. Returns nil (no probe) when no listen address is set.
func (m model) probeServer() tea.Cmd {
	addr := m.syncCfg.ServerListen
	if addr == "" {
		return nil
	}
	return func() tea.Msg {
		probeAddr := addr
		if h, p, err := net.SplitHostPort(addr); err == nil && (h == "" || h == "0.0.0.0" || h == "::") {
			probeAddr = net.JoinHostPort("127.0.0.1", p)
		}
		client := &http.Client{Timeout: 1500 * time.Millisecond}
		resp, err := client.Get("http://" + probeAddr + "/v1/health")
		if err != nil {
			return serverProbeMsg{reachable: false}
		}
		defer resp.Body.Close()
		return serverProbeMsg{reachable: resp.StatusCode == http.StatusOK}
	}
}
