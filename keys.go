package main

import (
	"fmt"
	"sort"
	"strings"
)

// keys.go — user-defined keybindings, as a thin overlay on the keymap registry.
//
// The registry already gives every binding a canonical action id, so a rebind
// is a one-line map from action to key: settings.json "keys": {"done": "D"}.
// The dispatch does not learn about it. Instead the key is translated at the
// door (resolveKeyOverride): a pressed key that the user bound to an action is
// rewritten to that action's *default* key, which is what the big switches in
// updateList/updateDetail already case on. Two consequences worth stating:
//
//   - Rebinding is only possible for actions whose registry key is a single
//     key. The pair/range forms (←/→, H/L, home/end · pgup/pgdn) describe
//     several keys at once, so there is nothing to swap.
//   - A key freed by a rebind stops working. If "done" moves from d to D, then
//     d does nothing — otherwise the old key would linger and the new binding
//     would be an alias rather than a rebind.
//
// Everything the user reads — the footer hints, the help overlay, the command
// palette — renders through effectiveKey, so a rebind shows up everywhere at
// once. That is the whole reason the registry carries action ids.

// activeKeys maps action id → the key the user bound it to. Package-level and
// set by applyKeys at startup, mirroring applyTheme / applyLang / applyStages.
// Empty means "defaults everywhere", which is the normal case.
var activeKeys = map[string]string{}

func applyKeys(overrides map[string]string) {
	next := make(map[string]string, len(overrides))
	for action, key := range overrides {
		next[action] = key
	}
	activeKeys = next
}

// effectiveKey returns the key a binding is actually on: the user's override
// when there is one, otherwise the registry default.
func effectiveKey(action, defaultKey string) string {
	if k, ok := activeKeys[action]; ok && k != "" {
		return k
	}
	return defaultKey
}

// rebindableActions lists every action that can be rebound, with its default
// key — derived from the registry, so a new single-key binding becomes
// rebindable without touching this file.
func rebindableActions() map[string]string {
	out := make(map[string]string, len(keymap))
	for i := range keymap {
		bd := &keymap[i]
		if !paletteSendable(bd.key) {
			continue // a pair or a range: nothing to swap
		}
		// An action can appear in several contexts; the registry guarantees one
		// key per action, so the first one is the answer.
		if _, seen := out[bd.action]; !seen {
			out[bd.action] = bd.key
		}
	}
	return out
}

// sanitizeKeyOverrides filters a raw "keys" map down to the overrides that are
// safe to apply, and returns a human-readable problem list for the ones it
// dropped. A broken entry must never take a key away from the user, so every
// rejection falls back to the default rather than to nothing.
func sanitizeKeyOverrides(raw map[string]string) (map[string]string, []string) {
	if len(raw) == 0 {
		return nil, nil
	}
	defaults := rebindableActions()
	clean := make(map[string]string, len(raw))
	var problems []string

	// Deterministic order so the problem list (and which of two colliding
	// entries is rejected) doesn't depend on map iteration.
	actions := make([]string, 0, len(raw))
	for action := range raw {
		actions = append(actions, action)
	}
	sort.Strings(actions)

	for _, action := range actions {
		key := strings.TrimSpace(raw[action])
		def, ok := defaults[action]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%q is not a rebindable action", action))
			continue
		case key == "":
			problems = append(problems, fmt.Sprintf("%q has no key", action))
			continue
		case key == "ctrl+c":
			// The one key the app can't give away: it is the quit of last
			// resort, handled before any dispatch.
			problems = append(problems, fmt.Sprintf("%q: ctrl+c is reserved", action))
			continue
		case !paletteSendable(key):
			problems = append(problems, fmt.Sprintf("%q: %q is not a single key", action, key))
			continue
		case key == def:
			continue // a no-op override, not worth carrying
		}
		clean[action] = key
	}

	// Collisions: within a context, two actions on the same key would make one
	// of them unreachable. Reject the override rather than guess.
	for _, ctxName := range keyCtxAll {
		seen := map[string]string{} // key → action
		for i := range keymap {
			bd := &keymap[i]
			if bd.ctx&ctxName == 0 || !paletteSendable(bd.key) {
				continue
			}
			key := bd.key
			if k, ok := clean[bd.action]; ok {
				key = k
			}
			if prev, dup := seen[key]; dup && prev != bd.action {
				loser, winner := bd.action, prev
				if _, overridden := clean[bd.action]; !overridden {
					loser, winner = prev, bd.action
				}
				if _, overridden := clean[loser]; overridden {
					problems = append(problems, fmt.Sprintf(
						"%q: %q is already taken by %q here", loser, key, winner))
					delete(clean, loser)
				}
				continue
			}
			seen[key] = bd.action
		}
	}
	if len(clean) == 0 {
		return nil, problems
	}
	return clean, problems
}

// keyCtxAll is every context, for the collision check. Kept beside the check
// that uses it rather than in the registry, which has no need for a list form.
var keyCtxAll = []keyCtx{
	ctxTasksList, ctxTasksDetail, ctxProjects, ctxTags, ctxStats,
	ctxCalendar, ctxCalendarTimeline, ctxSettings, ctxBoard,
	ctxTagDrill, ctxProjectDrill,
}

// resolveKeyOverride translates a pressed key into the key the handlers switch
// on. It returns the (possibly rewritten) key and whether the press should be
// delivered at all: a key whose action has been rebound away is swallowed, so
// the rebind actually frees it.
//
// Only meaningful in normal mode — modals own their keys (y/n on a confirm, the
// text a user types) and are left alone by the caller.
func resolveKeyOverride(ctx keyCtx, pressed string) (string, bool) {
	if len(activeKeys) == 0 {
		return pressed, true
	}
	var freed bool
	for i := range keymap {
		bd := &keymap[i]
		if bd.ctx&ctx == 0 || !paletteSendable(bd.key) {
			continue
		}
		eff := effectiveKey(bd.action, bd.key)
		if eff == pressed {
			return bd.key, true // the user's key → the handler's key
		}
		if bd.key == pressed && eff != pressed {
			freed = true // this action moved off the key that was just pressed
		}
	}
	if freed {
		return "", false
	}
	return pressed, true
}
