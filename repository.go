package main

import "taskr/todo"

// Repository is the persistence port. The app depends on this contract rather
// than on any concrete store: SQLite fulfils it in production (sqliteRepo), an
// in-memory fake fulfils it in tests. This keeps storage details out of the
// domain/UI layer and makes the store swappable (e.g. a future sync adapter).
//
// Save is differential: callers pass only the tasks that changed since the
// last save and the IDs of tasks that have been deleted. Until the in-memory
// Store gains dirty-tracking (step 4), callers pass the whole live set as
// dirty and nil tombstones; the adapter still tombstones vanished rows via
// scan-and-detect, preserving the old whole-snapshot semantics. After step 4
// the adapter drops the scan and trusts the explicit tombstone list.
type Repository interface {
	Load() ([]todo.Todo, error)
	Save(dirty []*todo.Todo, tombstones []string) error
	// ResyncScores rewrites the persisted `sequence` column for every live
	// row at the current activeBiases. Without this, a bias change or
	// passage of time (Age drift) leaves the column stale relative to the
	// in-memory formula — invisible to the TUI (which sorts in memory) but
	// a trap for any SQL consumer like TopBySequence or future sync.
	ResyncScores() error
}
