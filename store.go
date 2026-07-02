package main

import (
	"taskr/todo"
)

// Store is the single source of truth: the task set, the undo history, and the
// per-save change set (dirtyIDs / tombstones).
//
// Tasks are held in a map keyed by ID so:
//   - Lookup by ID is O(1) with no separate index.
//   - Pointers into the map are stable for the lifetime of the value, so
//     mutators can hand *todo.Todo around without worrying about slice growth.
//   - Mutation cost is independent of the corpus size.
//
// Order is *derived* — when the UI needs a sorted list it goes through a
// selector against an ordered view, never directly through map iteration.
type Store struct {
	tasks      map[string]*todo.Todo
	undoStack  []undoEntry
	dirtyIDs   map[string]struct{}
	tombstones map[string]struct{}

	// Maintained indexes. Update them via Store mutators; never write directly
	// or they will drift from `tasks`.
	subtaskOf     map[string][]string // parentID → child IDs in CreatedAt order
	runningTimers map[string]struct{} // task IDs with an active TimeEntry
}

func (s *Store) ensureTasks() {
	if s.tasks == nil {
		s.tasks = make(map[string]*todo.Todo)
	}
	if s.subtaskOf == nil {
		s.subtaskOf = make(map[string][]string)
	}
	if s.runningTimers == nil {
		s.runningTimers = make(map[string]struct{})
	}
}

// addSubtaskOf inserts childID into subtaskOf[parentID] at the position that
// keeps the slice in CreatedAt order. O(siblings) per call — fast in practice
// since subtask counts are tiny.
func (s *Store) addSubtaskOf(parentID string, child *todo.Todo) {
	if parentID == "" || child == nil {
		return
	}
	siblings := s.subtaskOf[parentID]
	insertAt := len(siblings)
	for i, sibID := range siblings {
		sib := s.tasks[sibID]
		if sib == nil {
			continue
		}
		if child.CreatedAt.Before(sib.CreatedAt) {
			insertAt = i
			break
		}
	}
	siblings = append(siblings, "")
	copy(siblings[insertAt+1:], siblings[insertAt:])
	siblings[insertAt] = child.ID
	s.subtaskOf[parentID] = siblings
}

func (s *Store) removeSubtaskOf(parentID, childID string) {
	if parentID == "" {
		return
	}
	siblings := s.subtaskOf[parentID]
	for i, id := range siblings {
		if id == childID {
			s.subtaskOf[parentID] = append(siblings[:i], siblings[i+1:]...)
			break
		}
	}
	if len(s.subtaskOf[parentID]) == 0 {
		delete(s.subtaskOf, parentID)
	}
}

// startTimer / stopTimer wrap todo.Todo's timer mutators and keep the
// runningTimers set in sync. Callers should use these instead of poking
// t.StartTimer() / t.StopTimer() directly.
func (s *Store) startTimer(id string) {
	t := s.get(id)
	if t == nil {
		return
	}
	t.StartTimer()
	s.ensureTasks()
	s.runningTimers[id] = struct{}{}
}

func (s *Store) stopTimer(id string) {
	t := s.get(id)
	if t == nil {
		return
	}
	t.StopTimer()
	delete(s.runningTimers, id)
}

func (s *Store) get(id string) *todo.Todo {
	if s.tasks == nil {
		return nil
	}
	return s.tasks[id]
}

// add stores a value copy of t and returns the pointer the store will hold.
// Callers that want to mutate further should use the returned pointer (or
// re-fetch via get(id)); mutating the value passed in does not affect the
// store. Maintained indexes (subtaskOf, runningTimers) are updated here.
func (s *Store) add(t todo.Todo) *todo.Todo {
	s.ensureTasks()
	cp := t
	s.tasks[cp.ID] = &cp
	if cp.ParentID != "" {
		s.addSubtaskOf(cp.ParentID, &cp)
	}
	if cp.IsTimerRunning() {
		s.runningTimers[cp.ID] = struct{}{}
	}
	return s.tasks[cp.ID]
}

// remove drops a task and updates every maintained index.
func (s *Store) remove(id string) {
	t := s.tasks[id]
	if t == nil {
		return
	}
	if t.ParentID != "" {
		s.removeSubtaskOf(t.ParentID, id)
	}
	// Children of this task are now orphaned; drop the subtaskOf bucket so
	// callers don't see stale IDs after a cascade delete.
	delete(s.subtaskOf, id)
	delete(s.runningTimers, id)
	delete(s.tasks, id)
}

func (s *Store) len() int { return len(s.tasks) }

// allTodos returns value copies of every task in unspecified order. Used by
// cache rebuilds, selector inputs, and undo snapshots — anywhere the caller
// wants an iterable slice. Each call allocates the slice.
//
// READ-ONLY CONTRACT: the returned structs are shallow copies — their slice
// fields (Tags, Dependencies, Comments, Learnings, TimeEntries) still alias the
// backing arrays of the live *Todo in the store. Treat the result as read-only.
// The in-place slice mutators in the todo package (RemoveTag, DeleteComment, …)
// scribble those same arrays, so a caller that both holds an allTodos() result
// and mutates the store would see torn data. Every current caller either only
// reads, or deep-copies first with copyTodo (as pushUndo does) — keep it that
// way; if you need to mutate a returned task, copyTodo it first.
func (s *Store) allTodos() []todo.Todo {
	out := make([]todo.Todo, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, *t)
	}
	return out
}

func (s *Store) markDirty(ids ...string) {
	if s.dirtyIDs == nil {
		s.dirtyIDs = make(map[string]struct{}, len(ids))
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		s.dirtyIDs[id] = struct{}{}
	}
}

// markAllDirty is the fallback used by callers that mutate without naming a
// specific ID (e.g. mass operations not yet refactored to return touched IDs).
// It marks every live task dirty; the save still ends up O(N) until callers
// are precise.
func (s *Store) markAllDirty() {
	if s.dirtyIDs == nil {
		s.dirtyIDs = make(map[string]struct{}, len(s.tasks))
	}
	for id := range s.tasks {
		s.dirtyIDs[id] = struct{}{}
	}
}

// markTombstone records a deletion. If the same ID was previously marked dirty,
// the dirty entry is dropped — there is no point writing a row we're about to
// tombstone in the same save.
func (s *Store) markTombstone(id string) {
	if id == "" {
		return
	}
	if s.tombstones == nil {
		s.tombstones = make(map[string]struct{})
	}
	s.tombstones[id] = struct{}{}
	delete(s.dirtyIDs, id)
}

// drainDirty extracts the current dirty set and tombstones for a save. Each
// dirty task is deep-copied so the async save goroutine sees a stable snapshot
// even if the Update goroutine continues mutating the stored pointer
// concurrently. (Pointer stability across mutations is guaranteed by the map,
// but field-level concurrent reads still need a snapshot.)
func (s *Store) drainDirty() (dirty []*todo.Todo, tombstones []string) {
	if n := len(s.dirtyIDs); n > 0 {
		dirty = make([]*todo.Todo, 0, n)
		for id := range s.dirtyIDs {
			t := s.tasks[id]
			if t == nil {
				continue
			}
			cp := copyTodo(*t)
			dirty = append(dirty, &cp)
		}
	}
	if n := len(s.tombstones); n > 0 {
		tombstones = make([]string, 0, n)
		for id := range s.tombstones {
			tombstones = append(tombstones, id)
		}
	}
	s.dirtyIDs = nil
	s.tombstones = nil
	return dirty, tombstones
}

// ── Undo ──────────────────────────────────────────────────────────────────────

const maxUndoStack = 20

// undoEntry is either a partial snapshot (the previous state of one or more
// specific task IDs — patch-like, O(touched)) or a full snapshot (every task,
// O(N) — used by mass operations or when callers don't name specific IDs).
// On undo we restore the captured tasks: missing IDs in a partial snapshot mean
// "newly created since" → those tasks should be removed.
type undoEntry struct {
	desc    string
	full    []todo.Todo // populated only when partial is nil; legacy fallback
	partial []todo.Todo // captured "before" states for the named IDs
	ids     []string    // IDs the partial entry covers (superset of partial IDs)
}

// pushUndo records the current state of the named task IDs as a patch-style
// snapshot. With no IDs, falls back to a full deep-copy of every task — O(N)
// memory, used by mass operations like global tag rename where many tasks
// change and per-ID tracking would be larger than the snapshot.
func (s *Store) pushUndo(desc string, ids ...string) {
	var entry undoEntry
	entry.desc = desc
	if len(ids) == 0 {
		full := s.allTodos()
		for i := range full {
			full[i] = copyTodo(full[i])
		}
		entry.full = full
	} else {
		entry.ids = append([]string(nil), ids...)
		entry.partial = make([]todo.Todo, 0, len(ids))
		for _, id := range ids {
			if t := s.tasks[id]; t != nil {
				entry.partial = append(entry.partial, copyTodo(*t))
			}
		}
	}
	s.undoStack = append(s.undoStack, entry)
	if len(s.undoStack) > maxUndoStack {
		copy(s.undoStack, s.undoStack[1:])
		s.undoStack = s.undoStack[:maxUndoStack]
	}
	// Persist the last few task/subtask deletions so they survive a restart —
	// the user expects deletions to be reversible even after closing taskr,
	// since they're the destructive op with no in-app fallback. Other undo
	// kinds stay in-memory only. A persist failure is swallowed; the worst
	// case is losing the cross-restart safety net, never blocking the delete.
	if isPersistedDelete(entry.desc) {
		_ = savePersistedUndoEntries(s.undoStack)
	}
}

func (s *Store) popUndo() (undoEntry, bool) {
	if len(s.undoStack) == 0 {
		return undoEntry{}, false
	}
	entry := s.undoStack[len(s.undoStack)-1]
	s.undoStack = s.undoStack[:len(s.undoStack)-1]
	// Keep the sidecar in sync when a persisted delete entry is consumed —
	// otherwise a popped delete could resurrect on next start.
	if isPersistedDelete(entry.desc) {
		_ = savePersistedUndoEntries(s.undoStack)
	}
	return entry, true
}

// restoreFromUndo applies an entry. For a full snapshot the entire task map
// is rebuilt. For a partial snapshot only the named IDs are touched: each
// captured task is restored to its prior value, and any ID in entry.ids that
// has no captured "before" state is removed (it was created after the push).
// Callers (performUndo) compute the inverse set-difference for tombstoning
// when restoring a full snapshot.
func (s *Store) restoreFromUndo(entry undoEntry) {
	if entry.partial != nil || entry.ids != nil {
		captured := make(map[string]*todo.Todo, len(entry.partial))
		for i := range entry.partial {
			t := entry.partial[i]
			captured[t.ID] = &t
		}
		for _, id := range entry.ids {
			if before, ok := captured[id]; ok {
				// Restore prior state in place by replacing the map entry.
				// remove() wipes subtaskOf[id], but this task's children are
				// not part of the entry — without re-attaching the bucket they
				// stay live in the map yet unreachable from every subtask view
				// until restart. (Children that ARE in the entry re-insert
				// themselves via their own add.)
				children := s.subtaskOf[id]
				s.remove(id)
				s.add(*before)
				for _, cid := range children {
					if c := s.tasks[cid]; c != nil && c.ParentID == id {
						s.addSubtaskOf(id, c)
					}
				}
			} else {
				// Created after the push — undo means remove.
				s.remove(id)
			}
		}
		return
	}
	// Full snapshot fallback.
	s.tasks = make(map[string]*todo.Todo, len(entry.full))
	s.subtaskOf = make(map[string][]string)
	s.runningTimers = make(map[string]struct{})
	for i := range entry.full {
		s.add(entry.full[i])
	}
}
