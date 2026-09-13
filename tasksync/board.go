package tasksync

import "time"

// board.go carries the kanban column list across devices.
//
// The columns are a preference and live in settings.json, which does not sync;
// the column a task sits in is a field on the task, which does. That split is
// the bug this closes — a task moved to "Implementation" on one machine
// arrived on another that had never heard the name, and fell into its first
// column (stageIndex's fallback, which is the right answer for an unknown name
// and the wrong one for a name the fleet simply had not been told).
//
// So the list rides along with the task sync as one optional field in each
// direction. A client that sends no board is not participating and gets its
// own columns left alone; a server with no board to send leaves the client's
// alone. Both halves are what make this safe to roll into a live fleet one
// device at a time, and why it needs no protocol bump: an older peer ignores a
// field it has never heard of and the sync is unaffected.

// Board is a device's kanban column list and the moment it was last edited.
// The zero value — no stages, no timestamp — is "I have nothing to say about
// the columns", which is what every device that has never edited them sends.
type Board struct {
	Stages     []string  `json:"stages"`
	ModifiedAt time.Time `json:"modified_at"`
}

// BoardStore is the optional second doorway into the application's storage:
// where the shared column list is kept between syncs. A Server without one
// never answers with a board, which is exactly what an older server looks like
// to a newer client — so that case needs no separate handling anywhere.
type BoardStore interface {
	LoadBoard() (Board, error)
	SaveBoard(Board) error
}

// MergeBoard folds a client's list into the stored one: the later edit wins.
//
// A zero timestamp never wins. That is the rule that lets this default to on
// without a fresh install resetting a fleet's board on its first sync: a
// device that has never touched its columns is carrying the shipped defaults,
// has nothing to say, and says nothing. An exact tie keeps what is stored —
// the two lists came from the same edit, so there is nothing to choose.
func MergeBoard(stored, incoming Board) Board {
	if len(incoming.Stages) == 0 || incoming.ModifiedAt.IsZero() {
		return stored
	}
	if len(stored.Stages) == 0 || incoming.ModifiedAt.After(stored.ModifiedAt) {
		return incoming
	}
	return stored
}

// SameBoard reports whether two lists are identical in names and order. The
// timestamp is deliberately not part of it: callers use this to decide whether
// anything needs writing or redrawing, and a list that came back unchanged with
// a newer stamp is still the same board.
func SameBoard(a, b Board) bool {
	if len(a.Stages) != len(b.Stages) {
		return false
	}
	for i := range a.Stages {
		if a.Stages[i] != b.Stages[i] {
			return false
		}
	}
	return true
}
