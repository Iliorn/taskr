package main

import "taskr/todo"

// Store is the single source of truth: the task set plus the undo history.
//
// It is deliberately UI-agnostic — no cursors, tabs, search, sort modes, or
// rendering. Everything the other tabs show (active/done lists, tag stats,
// projects, learnings) is *derived* from these todos by selectors, not stored
// here. The model embeds a Store, so existing `m.todos` / `m.pushUndo()` access
// keeps working via Go's field/method promotion.
type Store struct {
	todos     []todo.Todo
	undoStack []undoEntry
}

// ── Undo ──────────────────────────────────────────────────────────────────────

const maxUndoStack = 20

type undoEntry struct {
	todos []todo.Todo
	desc  string
}

func (s *Store) pushUndo(desc string) {
	snapshot := copyTodos(s.todos)
	s.undoStack = append(s.undoStack, undoEntry{todos: snapshot, desc: desc})
	if len(s.undoStack) > maxUndoStack {
		copy(s.undoStack, s.undoStack[1:])
		s.undoStack = s.undoStack[:maxUndoStack]
	}
}

func (s *Store) popUndo() (undoEntry, bool) {
	if len(s.undoStack) == 0 {
		return undoEntry{}, false
	}
	entry := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]
	return entry, true
}
