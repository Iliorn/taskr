package app

import (
	"strings"
	"testing"

	"github.com/Iliorn/tjek/todo"
)

// Every action that can be rebound has a row, except the ones on enter and
// esc, and every row names a rebindable action with a label.
func TestKeysPageCoversEveryRebindableAction(t *testing.T) {
	if n := numSettingsRows - settingKeyFirst; n != len(keyPageActions) {
		t.Fatalf("the Keys page has %d row IDs for %d actions", n, len(keyPageActions))
	}
	onPage := map[string]bool{}
	for _, a := range keyPageActions {
		onPage[a] = true
		if keyActionLabels[a] == "" {
			t.Errorf("action %q has no label on the Keys page", a)
		}
	}
	for a, k := range rebindableActions() {
		if k == "enter" || k == "esc" {
			if onPage[a] {
				t.Errorf("action %q on %s should not be on the Keys page", a, k)
			}
			continue
		}
		if !onPage[a] {
			t.Errorf("rebindable action %q (%s) is missing from the Keys page", a, k)
		}
	}
	for a := range onPage {
		if _, ok := rebindableActions()[a]; !ok {
			t.Errorf("Keys page row %q is not a rebindable action", a)
		}
	}
}

// Script: Settings → Keys, enter, press a key: the action moves there, the
// old key stops working, a taken key is refused, and backspace puts the
// default back.
func TestScriptRebindFromTheKeysPage(t *testing.T) {
	m := settingsModel(t)
	t.Cleanup(func() { applyKeys(nil) })
	row := settingKeyFirst
	for i, a := range keyPageActions {
		if a == "done" {
			row = settingKeyFirst + i
		}
	}

	// A key the action's contexts already use is refused.
	m.settingsCursor = row
	m = sendKey(t, m, "enter")
	if m.mode != modeCaptureKey {
		t.Fatalf("enter on a Keys row: mode %v, want the capture prompt", m.mode)
	}
	m = sendKey(t, m, "x") // delete
	if m.mode != modeNormal || effectiveKey("done", "d") != "d" {
		t.Fatalf("a taken key was accepted: done is on %q", effectiveKey("done", "d"))
	}
	if !strings.Contains(m.err, tr("Delete")) {
		t.Errorf("the refusal %q does not say what has the key", m.err)
	}

	// esc cancels without changing anything.
	m = sendKey(t, m, "enter")
	m = sendKey(t, m, "esc")
	if m.mode != modeNormal || effectiveKey("done", "d") != "d" {
		t.Fatal("esc did not cancel the capture")
	}

	// A free key takes.
	m = sendKey(t, m, "enter")
	m = sendKey(t, m, "z")
	if got := effectiveKey("done", "d"); got != "z" {
		t.Fatalf("done is on %q, want z", got)
	}
	if s, _ := loadSettings(); s.Keys["done"] != "z" {
		t.Errorf("settings.json keys = %v, want done: z", s.Keys)
	}

	// On the Tasks tab, z closes a task and d no longer does.
	task := todo.New("a task")
	m.add(task)
	m.markCacheDirty()
	m.refreshCaches()
	m.switchTab(tabTasks)
	m = sendKey(t, m, "d")
	if m.get(task.ID).Status == todo.Done {
		t.Fatal("d still closes a task after done moved to z")
	}
	m = sendKey(t, m, "z")
	if m.get(task.ID).Status != todo.Done {
		t.Fatal("z did not close the task")
	}

	// backspace on the row puts the default back.
	m.switchTab(tabSettings)
	m.settingsCursor = row
	m = sendKey(t, m, "backspace")
	if got := effectiveKey("done", "d"); got != "d" {
		t.Errorf("after backspace done is on %q, want d", got)
	}
	if s, _ := loadSettings(); len(s.Keys) != 0 {
		t.Errorf("settings.json still holds %v after the reset", s.Keys)
	}
}
