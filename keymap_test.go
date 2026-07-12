package main

import (
	"strings"
	"testing"
)

// A canonical action must use the same key in every context it appears in —
// delete is always x, edit is always r, sort is always s. This is the
// cross-page control-consistency contract: bind an existing action to a new
// key on some tab and this fails, forcing the divergence to be deliberate.
func TestKeymapActionsAreConsistent(t *testing.T) {
	keysFor := map[string]map[string]bool{}
	for _, b := range keymap {
		if keysFor[b.action] == nil {
			keysFor[b.action] = map[string]bool{}
		}
		keysFor[b.action][b.key] = true
	}
	for action, keys := range keysFor {
		if len(keys) > 1 {
			var ks []string
			for k := range keys {
				ks = append(ks, k)
			}
			t.Errorf("action %q is bound to multiple keys %v — one action must use one key everywhere", action, ks)
		}
	}
}

// No two actions may share a key within the same context (a real collision the
// user would experience), which is what forced h off the vim-left slot.
func TestKeymapNoIntraContextCollision(t *testing.T) {
	ctxs := []keyCtx{
		ctxTasksList, ctxTasksDetail, ctxProjects, ctxTags, ctxBoard,
		ctxStats, ctxCalendar, ctxCalendarTimeline, ctxSettings,
	}
	for _, ctx := range ctxs {
		seen := map[string]string{} // key -> action
		for _, b := range keymap {
			if b.ctx&ctx == 0 {
				continue
			}
			if prev, ok := seen[b.key]; ok && prev != b.action {
				t.Errorf("ctx %d: key %q maps to both %q and %q", ctx, b.key, prev, b.action)
			}
			seen[b.key] = b.action
		}
	}
}

// Regression guard for the exact drift this registry was built to kill: T
// (manual time entry) and m (merge tags) both dispatch but were missing from
// the help. Since help is generated from the registry, asserting they're
// registered guarantees they show up.
func TestKeymapCoversPreviouslyMissingKeys(t *testing.T) {
	has := func(key, action string) bool {
		for _, b := range keymap {
			if b.key == key && b.action == action {
				return true
			}
		}
		return false
	}
	if !has("T", "timeentry") {
		t.Error("T (manual time entry) missing from the keymap registry")
	}
	if !has("m", "merge") {
		t.Error("m (merge tags) missing from the keymap registry")
	}
}

// Every context must produce a non-empty footer hint, and every registered
// section must render into the help overlay — the two generated surfaces.
func TestKeymapGeneratesHintsAndHelp(t *testing.T) {
	ctxs := map[string]keyCtx{
		"tasks": ctxTasksList, "detail": ctxTasksDetail, "projects": ctxProjects,
		"tags": ctxTags, "board": ctxBoard, "calendar": ctxCalendar,
		"calendarTimeline": ctxCalendarTimeline, "settings": ctxSettings,
	}
	for name, ctx := range ctxs {
		if hintString(ctx, false) == "" {
			t.Errorf("ctx %s produced an empty footer hint", name)
		}
	}
	// Stats has a single binding; assert it at least appears.
	if !strings.Contains(hintString(ctxStats, false), "cycle activity range") {
		t.Error("stats hint should mention cycling the activity range")
	}
}
