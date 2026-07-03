package main

import (
	"strings"
	"testing"
)

func TestSyncSettingsRenderUnconfigured(t *testing.T) {
	m := initialModel(&fakeRepo{})
	m.syncCfg = syncConfig{}
	m.autoSync = autoSyncEnabled(m.syncCfg)

	out := m.renderSettingsList()
	for _, want := range []string{"Sync", "Sync server", "Sync token", "Sync now", "needs server", "not set"} {
		if !strings.Contains(out, want) {
			t.Errorf("unconfigured settings should show %q; got:\n%s", want, out)
		}
	}
}

func TestSyncSettingsRenderConfiguredMasksToken(t *testing.T) {
	m := initialModel(&fakeRepo{})
	m.syncCfg = syncConfig{URL: "http://100.122.178.43:8765", Token: "supersecret-token-value"}
	m.autoSync = autoSyncEnabled(m.syncCfg)

	out := m.renderSettingsList()
	if !strings.Contains(out, "http://100.122.178.43:8765") {
		t.Errorf("configured settings should show the server URL; got:\n%s", out)
	}
	if strings.Contains(out, "supersecret-token-value") {
		t.Errorf("token must be masked, never rendered in plaintext; got:\n%s", out)
	}
	if !strings.Contains(out, "‹ On ›") {
		t.Errorf("auto-sync should read On when configured; got:\n%s", out)
	}
}

func TestToggleSyncAuto(t *testing.T) {
	m := initialModel(&fakeRepo{})

	// Unconfigured: toggling is a no-op (nothing to sync against).
	m.syncCfg = syncConfig{}
	m.autoSync = false
	m.toggleSyncAuto()
	if m.autoSync {
		t.Errorf("toggle with no server/token should stay off")
	}

	// Configured: defaults on, toggles off, then back on.
	m.syncCfg = syncConfig{URL: "http://x:8765", Token: "tok"}
	m.autoSync = autoSyncEnabled(m.syncCfg)
	if !m.autoSync {
		t.Fatalf("configured sync should default to on")
	}
	m.toggleSyncAuto()
	if m.autoSync || m.syncCfg.AutoSync == nil || *m.syncCfg.AutoSync {
		t.Errorf("toggle should turn auto-sync off")
	}
	m.toggleSyncAuto()
	if !m.autoSync {
		t.Errorf("second toggle should turn auto-sync back on")
	}
}

func TestRestartLiveSyncSwapsListener(t *testing.T) {
	m := initialModel(&fakeRepo{})
	m.syncCfg = syncConfig{URL: "http://127.0.0.1:1", Token: "tok"}
	m.autoSync = autoSyncEnabled(m.syncCfg)

	cmd := m.restartLiveSync()
	old := m.liveSync
	if old == nil || cmd == nil {
		t.Fatalf("configured sync should start a listener and return an arm cmd")
	}
	defer old.Close()

	// Editing the URL must swap the listener — the old one captured the old
	// config and would keep reconnecting to the old server.
	m.syncCfg.URL = "http://127.0.0.1:2"
	if cmd := m.restartLiveSync(); cmd == nil || m.liveSync == nil || m.liveSync == old {
		t.Fatalf("restart should replace the listener with one on the new config")
	}
	defer m.liveSync.Close()
	select {
	case <-old.Done():
	default:
		t.Errorf("old listener should be stopped after restart")
	}

	// Clearing the config must stop the listener and start nothing.
	running := m.liveSync
	m.syncCfg = syncConfig{}
	m.autoSync = autoSyncEnabled(m.syncCfg)
	if cmd := m.restartLiveSync(); cmd != nil || m.liveSync != nil {
		t.Errorf("unconfigured sync must not run a listener")
	}
	select {
	case <-running.Done():
	default:
		t.Errorf("listener should be stopped when config is cleared")
	}
}
