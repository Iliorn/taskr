package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Iliorn/tjek/rank"
	"github.com/Iliorn/tjek/tasksync"
	"github.com/Iliorn/tjek/todo"
)

// syncstore.go is the one place a sync merge touches the database. Both sides
// of a sync — `tjek serve` folding a client's push into the authoritative
// store, and `tjek sync` applying the server's response locally — run
// load → Merge → save inside one SQLite transaction. As three separate steps,
// a writer in another process (a CLI `tjek add` on the same host) could
// commit between the load and the save and have its edit overwritten, or a
// just-added comment tombstoned as "vanished" and deleted on every device.

// mergeTxRetries bounds how many times a merge is retried when a concurrent
// writer invalidates its snapshot. Under WAL a deferred transaction that read
// before another process committed gets SQLITE_BUSY(_SNAPSHOT) on its first
// write; the whole load+merge+save is then re-run against a fresh snapshot.
// Merge is idempotent, so a retry can only converge, never double-apply.
const mergeTxRetries = 3

// mergeStoreTestHook, when non-nil, runs between the transactional load and
// the save. Tests use it to inject a concurrent same-host write at the exact
// moment a non-transactional merge would have clobbered it. Always nil in production.
var mergeStoreTestHook func()

// mergeIntoStore folds incoming into the store at h atomically and returns the
// merged set. changed is false when the store already contained the merged
// result — nothing was written, so callers can skip change broadcasts and the
// fs watcher stays quiet (the no-op-write guard that prevents sync feedback
// loops). The changed rows' `sequence` column is scored with b against the
// merged set's own activity heat, so a merge needs nothing from the caller's
// in-memory state and can run on any goroutine.
func mergeIntoStore(h *sql.DB, incoming []todo.Todo, b rank.Biases) (merged []todo.Todo, changed bool, err error) {
	merged, changed, _, err = mergeIntoStoreRetrying(h, incoming, b, false)
	return merged, changed, err
}

// mergeServerIntoStore is mergeIntoStore for the task set a sync server
// answered with. Where this device and another edited different fields of a
// task since the two last agreed, it keeps both (tasksync.ThreeWay against
// the sync_base row), and it records the server's set as the new base.
// rebased counts the tasks combined that way: their combined version is
// newer than the server's, so the next sync has to push it.
func mergeServerIntoStore(h *sql.DB, incoming []todo.Todo, b rank.Biases) (merged []todo.Todo, rb rebaseResult, err error) {
	merged, _, rb, err = mergeIntoStoreRetrying(h, incoming, b, true)
	return merged, rb, err
}

// rebaseResult is what a merge against the sync base did beyond Merge.
// combined counts the tasks put together from both sides' edits; kept names
// the tasks both sides edited whose local edits all survived, combined or
// not, so the recovery log need not list them as lost.
type rebaseResult struct {
	combined int
	kept     map[string]bool
}

func mergeIntoStoreRetrying(h *sql.DB, incoming []todo.Todo, b rank.Biases, fromServer bool) (merged []todo.Todo, changed bool, rb rebaseResult, err error) {
	for attempt := 0; ; attempt++ {
		merged, changed, rb, err = mergeIntoStoreOnce(h, incoming, b, fromServer)
		if err == nil || attempt >= mergeTxRetries || !isBusyErr(err) {
			return merged, changed, rb, err
		}
		// Brief, growing pause: the competing writer is another local process
		// mid-commit, not a network peer — it clears in milliseconds.
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
}

func mergeIntoStoreOnce(h *sql.DB, incoming []todo.Todo, b rank.Biases, fromServer bool) ([]todo.Todo, bool, rebaseResult, error) {
	tx, err := h.Begin()
	if err != nil {
		return nil, false, rebaseResult{}, err
	}
	defer tx.Rollback()

	current, err := loadTodosForSync(tx)
	if err != nil {
		return nil, false, rebaseResult{}, err
	}
	if mergeStoreTestHook != nil {
		mergeStoreTestHook()
	}
	merged := tasksync.Merge(current, incoming)
	var rb rebaseResult
	var baseWrites map[string]string
	if fromServer {
		base, err := loadSyncBase(tx)
		if err != nil {
			return nil, false, rebaseResult{}, err
		}
		rb = rebaseOnSyncBase(merged, current, incoming, base, time.Now())
		baseWrites = syncBaseChanges(incoming, base)
	}
	// Write only the rows the merge actually changed: rewriting every merged
	// task is O(whole store) per sync and a clobber surface. An empty change set
	// doubles as the no-op guard: nothing written, the deferred rollback just
	// releases the read snapshot.
	dirty := changedTasks(current, merged)
	if len(dirty) == 0 && len(baseWrites) == 0 {
		return merged, false, rb, nil
	}
	if len(dirty) > 0 {
		var live []*todo.Todo
		for i := range merged {
			if !merged[i].Deleted {
				live = append(live, &merged[i])
			}
		}
		rk := rank.Ranker{Biases: b}.Refreshed(time.Now(), live)
		if err := saveNormalizedIn(tx, dirty, nil, rk.ScoreNow()); err != nil {
			return nil, false, rebaseResult{}, err
		}
	}
	if err := writeSyncBase(tx, baseWrites); err != nil {
		return nil, false, rebaseResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, rebaseResult{}, err
	}
	return merged, len(dirty) > 0, rb, nil
}

// ── Sync base ────────────────────────────────────────────────────────────────

// syncBaseJSON is the form a task is kept in as a base: its canonical JSON
// without comments and time entries, which ThreeWay never reads. Canonical, so
// an unchanged task always encodes to the same string and needs no write.
func syncBaseJSON(t todo.Todo) string {
	t.Comments, t.TimeEntries = nil, nil
	return string(tasksync.CanonicalJSON(t))
}

// loadSyncBase reads every base row, undecoded: most are only compared with
// the server's new version, and only a task both sides edited is decoded.
func loadSyncBase(q querier) (map[string]string, error) {
	rows, err := q.Query(`SELECT id, data FROM sync_base`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	base := make(map[string]string)
	for rows.Next() {
		var id, data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		base[id] = data
	}
	return base, rows.Err()
}

// rebaseOnSyncBase replaces each merged task that this device and the server
// both changed since their base with tasksync.ThreeWay's combination of the
// two. current is this device's store before the merge, incoming the server's
// set.
func rebaseOnSyncBase(merged, current, incoming []todo.Todo, base map[string]string, now time.Time) rebaseResult {
	rb := rebaseResult{kept: map[string]bool{}}
	if len(base) == 0 {
		return rb
	}
	local := make(map[string]todo.Todo, len(current))
	for _, t := range current {
		local[t.ID] = t
	}
	remote := make(map[string]todo.Todo, len(incoming))
	for _, t := range incoming {
		remote[t.ID] = t
	}
	for i, m := range merged {
		l, lok := local[m.ID]
		r, rok := remote[m.ID]
		raw, bok := base[m.ID]
		if !lok || !rok || !bok {
			continue
		}
		// Only a task both sides moved away from the base can have lost an
		// edit; everything else is a one-sided change Merge already has.
		if syncBaseJSON(l) == raw || syncBaseJSON(r) == raw {
			continue
		}
		var b todo.Todo
		if json.Unmarshal([]byte(raw), &b) != nil {
			continue // an unreadable base is no base: last-writer-wins
		}
		if combined, ok := tasksync.ThreeWay(b, l, r, m, now); ok {
			merged[i] = combined
			rb.combined++
		}
		if tasksync.KeepsLocalEdits(b, l, merged[i]) {
			rb.kept[m.ID] = true
		}
	}
	return rb
}

// syncBaseChanges is what writeSyncBase must do to make the base the server's
// set: the rows whose encoding differs, and "" for a row to drop because the
// server's version is a tombstone.
func syncBaseChanges(incoming []todo.Todo, base map[string]string) map[string]string {
	out := make(map[string]string)
	for _, t := range incoming {
		old, had := base[t.ID]
		if t.Deleted {
			if had {
				out[t.ID] = ""
			}
			continue
		}
		if data := syncBaseJSON(t); !had || data != old {
			out[t.ID] = data
		}
	}
	return out
}

func writeSyncBase(tx *sql.Tx, changes map[string]string) error {
	if len(changes) == 0 {
		return nil
	}
	up, err := tx.Prepare(`INSERT INTO sync_base (id, data) VALUES (?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data`)
	if err != nil {
		return err
	}
	defer up.Close()
	del, err := tx.Prepare(`DELETE FROM sync_base WHERE id = ?`)
	if err != nil {
		return err
	}
	defer del.Close()
	for id, data := range changes {
		if data == "" {
			_, err = del.Exec(id)
		} else {
			_, err = up.Exec(id, data)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// changedTasks returns pointers to the merged tasks whose canonical form
// differs from their pre-merge counterpart, or that are new. Canonicalization
// reuses the sync digest's ordering rules, so slice reordering introduced by the
// merge never reads as a change (no false positives to loop the watcher), and
// json.Marshal is deterministic, so a real change is never missed. Merge only
// unions IDs — a task present in current is always present in merged — so
// deletions need no separate pass (they arrive as tombstone-field changes).
func changedTasks(current, merged []todo.Todo) []*todo.Todo {
	prev := make(map[string][]byte, len(current))
	for i := range current {
		prev[current[i].ID] = tasksync.CanonicalJSON(current[i])
	}
	var dirty []*todo.Todo
	for i := range merged {
		if b, ok := prev[merged[i].ID]; ok && bytes.Equal(b, tasksync.CanonicalJSON(merged[i])) {
			continue
		}
		dirty = append(dirty, &merged[i])
	}
	return dirty
}

// isBusyErr reports whether err is SQLite telling us another writer got in the
// way (SQLITE_BUSY, or SQLITE_BUSY_SNAPSHOT when our read snapshot went stale
// before we wrote). modernc surfaces these as strings; there is no exported
// error type to test against.
func isBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}
