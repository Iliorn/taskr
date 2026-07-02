package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"taskr/todo"
)

// Undo for deletions persists to a sidecar JSON so the most recent task/subtask
// deletes survive a restart (the in-memory undoStack is lost on quit). Only
// these two delete kinds — full-task and full-subtask — are persisted because
// they're the destructive operations the user can't otherwise recover. Other
// undo entries (rename, edit notes, remove a tag, etc.) stay in-memory; they
// describe mutations the task survives and lose meaning once the surrounding
// session ends.

const (
	undoPersistFile       = "undo.json"
	undoPersistVersion    = 1
	undoPersistMaxEntries = 5 // user-requested floor: "the last five deletions"
	undoDescDeleteTask    = "delete task"
	undoDescDeleteSubtask = "delete subtask"
)

type persistedUndo struct {
	Version int                  `json:"version"`
	Entries []persistedUndoEntry `json:"entries"`
}

type persistedUndoEntry struct {
	Desc    string      `json:"desc"`
	IDs     []string    `json:"ids"`
	Partial []todo.Todo `json:"partial"`
}

func undoPersistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", undoPersistFile)
}

func isPersistedDelete(desc string) bool {
	return desc == undoDescDeleteTask || desc == undoDescDeleteSubtask
}

// loadPersistedUndoEntries reads up to undoPersistMaxEntries delete entries
// from the sidecar. Missing file is *not* an error — a fresh install has no
// history. A corrupt file is returned as an error so initialModel can surface
// it (and the user can delete the file) instead of silently swallowing it.
func loadPersistedUndoEntries() ([]undoEntry, error) {
	data, err := os.ReadFile(undoPersistPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var p persistedUndo
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("undo.json is corrupt: %w", err)
	}
	if p.Version != undoPersistVersion {
		// Forward-incompatible files are dropped silently — the worst-case
		// is losing some restorable history, never corruption.
		return nil, nil
	}
	out := make([]undoEntry, 0, len(p.Entries))
	for _, pe := range p.Entries {
		out = append(out, undoEntry{
			desc:    pe.Desc,
			ids:     append([]string(nil), pe.IDs...),
			partial: append([]todo.Todo(nil), pe.Partial...),
		})
	}
	return out, nil
}

// savePersistedUndoEntries writes the most recent delete entries from `stack`
// to the sidecar (up to undoPersistMaxEntries, newest last). Non-delete entries
// are skipped. Write errors are returned but most callers swallow them with a
// log/err message — failing to persist undo shouldn't block a delete.
func savePersistedUndoEntries(stack []undoEntry) error {
	keep := make([]persistedUndoEntry, 0, undoPersistMaxEntries)
	for _, e := range stack {
		if !isPersistedDelete(e.desc) {
			continue
		}
		keep = append(keep, persistedUndoEntry{
			Desc:    e.desc,
			IDs:     append([]string(nil), e.ids...),
			Partial: append([]todo.Todo(nil), e.partial...),
		})
	}
	if len(keep) > undoPersistMaxEntries {
		keep = keep[len(keep)-undoPersistMaxEntries:]
	}
	path := undoPersistPath()
	if len(keep) == 0 {
		// Nothing to persist — best to remove the file so a stale older
		// snapshot doesn't reappear if entries get popped back to zero.
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(persistedUndo{Version: undoPersistVersion, Entries: keep})
	if err != nil {
		return err
	}
	// 0600 like settings/sync state — the file holds full task content.
	return os.WriteFile(path, data, 0600)
}
