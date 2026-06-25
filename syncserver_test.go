package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServerSettingsRender(t *testing.T) {
	m := initialModel(&fakeRepo{})

	// Unconfigured: no token → "needs token", listen shows the default.
	m.syncCfg = syncConfig{}
	out := m.renderSettingsList()
	for _, want := range []string{"Server", "Listen", "Server token", "needs token", defaultServerListen, "not set"} {
		if !strings.Contains(out, want) {
			t.Errorf("unconfigured server settings should show %q; got:\n%s", want, out)
		}
	}

	// Token set, not running → "Off", token masked (never plaintext).
	m.syncCfg = syncConfig{ServerToken: "hunter2-secret", ServerListen: "100.122.178.43:8765"}
	out = m.renderSettingsList()
	if strings.Contains(out, "hunter2-secret") {
		t.Errorf("server token must be masked; got:\n%s", out)
	}
	if !strings.Contains(out, "‹ Off ›") || !strings.Contains(out, "100.122.178.43:8765") {
		t.Errorf("server should read Off with its listen address; got:\n%s", out)
	}
}

func TestToggleServerNeedsToken(t *testing.T) {
	if err := openStore(); err != nil {
		t.Fatalf("openStore: %v", err)
	}
	m := initialModel(&fakeRepo{})
	m.syncCfg = syncConfig{} // no token

	m.toggleServer()
	if m.inprocServer != nil {
		t.Errorf("server must not start without a token")
		_ = m.inprocServer.Close()
	}
	if !strings.Contains(m.syncStatus, "token") {
		t.Errorf("expected a 'set a server token' hint, got %q", m.syncStatus)
	}
}

func TestToggleServerLifecycleAndServes(t *testing.T) {
	if err := openStore(); err != nil {
		t.Fatalf("openStore: %v", err)
	}
	m := initialModel(&fakeRepo{})
	m.syncCfg = syncConfig{ServerToken: "tok", ServerListen: "127.0.0.1:0"} // OS-assigned port

	m.toggleServer()
	if m.inprocServer == nil {
		t.Fatalf("server should start once a token is set (status: %q)", m.syncStatus)
	}
	if !m.syncCfg.ServerOn {
		t.Errorf("ServerOn should be persisted true")
	}

	// It actually answers health on the bound address.
	addr := m.inprocServer.Addr
	var ok bool
	for i := 0; i < 20; i++ {
		resp, err := (&http.Client{Timeout: time.Second}).Get("http://" + addr + "/v1/health")
		if err == nil {
			resp.Body.Close()
			ok = resp.StatusCode == http.StatusOK
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ok {
		t.Errorf("in-process server did not answer health on %s", addr)
	}

	// Toggling again stops it.
	m.toggleServer()
	if m.inprocServer != nil || m.syncCfg.ServerOn {
		t.Errorf("second toggle should stop the server and clear ServerOn")
	}
}
