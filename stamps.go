package main

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/todo"
)

// stamps.go gives a local edit its per-field stamps. A save compares each
// task with the version the store holds and stamps only the units that
// differ (todo.Fields, and each tag and dependency), keeping the stored
// stamps of the rest. Stamping at the save, rather than in every mutation,
// is what keeps the many places that edit a task unaware of sync: whatever
// they changed, the save sees. The sync merge (tasksync.Merge) then keeps
// each unit from the version with the later stamp.
//
// A merge's own writes (mergeIntoStore) are not stamped here: they carry the
// stamps they arrived with.

// saveStamped is sqliteRepo.Save: stamp the dirty tasks and tombstones
// against the stored versions with this device's clock, and write them, in
// one transaction. A write that loses a race with another process's commit
// (SQLITE_BUSY after the reads) is run again against a fresh snapshot.
func saveStamped(h *sql.DB, dirty []*todo.Todo, tombstones map[string]time.Time, score func(*todo.Todo) float64, now time.Time) error {
	if len(dirty) == 0 && len(tombstones) == 0 {
		return nil
	}
	for attempt := 0; ; attempt++ {
		err := saveStampedOnce(h, dirty, tombstones, score, now)
		if err == nil || attempt >= mergeTxRetries || !isBusyErr(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
}

func saveStampedOnce(h *sql.DB, dirty []*todo.Todo, tombstones map[string]time.Time, score func(*todo.Todo) float64, now time.Time) error {
	tx, err := h.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	clock, err := loadClock(tx)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(dirty)+len(tombstones))
	for _, t := range dirty {
		ids = append(ids, t.ID)
	}
	for id := range tombstones {
		ids = append(ids, id)
	}
	stored, err := loadStampBases(tx, ids)
	if err != nil {
		return err
	}
	for _, t := range dirty {
		old, ok := stored[t.ID]
		var prev *todo.Todo
		if ok {
			prev = &old
		}
		t.Stamps = stampEdit(prev, t, clock, now)
	}
	if err := saveNormalizedIn(tx, dirty, tombstones, score); err != nil {
		return err
	}
	// A deletion stamps the task's "deleted" unit and keeps the rest.
	for id := range tombstones {
		old, ok := stored[id]
		if !ok || old.Deleted {
			continue
		}
		deleted := old
		deleted.Deleted = true
		stamps := stampEdit(&old, &deleted, clock, now)
		if _, err := tx.Exec(`UPDATE todos SET stamps = ? WHERE id = ?`, encodeStamps(stamps), id); err != nil {
			return err
		}
	}
	if err := saveClock(tx, clock); err != nil {
		return err
	}
	return tx.Commit()
}

// stampEdit is the stamps t is saved with, given the stored version old (nil
// for a task not yet stored): a fresh stamp for each unit that differs, the
// stored stamp for each that does not. All units one save changes share its
// stamp. The stored stamps are written out in full, reconstructed ones
// included, so a later save cannot read an unchanged unit's stamp off a
// newer modification time.
func stampEdit(old, t *todo.Todo, clock *hlc.Clock, now time.Time) map[string]hlc.Stamp {
	var fresh hlc.Stamp
	next := func() hlc.Stamp {
		if fresh == "" {
			fresh = clock.Now(now)
		}
		return fresh
	}
	stamps := make(map[string]hlc.Stamp, len(todo.Fields)+len(t.Tags)+len(t.Dependencies))
	if old == nil {
		for _, f := range todo.Fields {
			stamps[f.Key] = next()
		}
		for _, k := range t.SetKeys() {
			stamps[k] = next()
		}
		// A new task had no other member before this.
		stamps[todo.SetsKey] = next()
		return stamps
	}
	if s := old.Stamp(todo.SetsKey); s != "" {
		stamps[todo.SetsKey] = s
	}
	for _, f := range todo.Fields {
		if f.Same(old, t) {
			if s := old.Stamp(f.Key); s != "" {
				stamps[f.Key] = s
			}
		} else {
			stamps[f.Key] = next()
		}
	}
	keys := old.SetKeys()
	for _, k := range t.SetKeys() {
		if !old.HasMember(k) {
			if _, known := old.Stamps[k]; !known {
				keys = append(keys, k)
			}
		}
	}
	for _, k := range keys {
		if old.HasMember(k) == t.HasMember(k) {
			if s := old.Stamp(k); s != "" {
				stamps[k] = s
			}
		} else {
			stamps[k] = next()
		}
	}
	return stamps
}

// loadStampBases reads the stored versions of ids, with what stampEdit
// compares: the scalar fields, the stamps, tags and dependencies. Comments
// and time entries are left out; they merge as records.
func loadStampBases(tx *sql.Tx, ids []string) (map[string]todo.Todo, error) {
	out := make(map[string]todo.Todo, len(ids))
	const chunk = 500
	for len(ids) > 0 {
		n := min(chunk, len(ids))
		part := ids[:n]
		ids = ids[n:]
		in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(part)), ",") + ")"
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := tx.Query(`SELECT id, title, status, priority, size, project, parent_id,
			modified_at, due_date, start_date, completed_at, notes, recurrence,
			seq_rank_done, stage, deleted, deleted_at, stamps
			FROM todos WHERE id IN `+in, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t todo.Todo
			var status, priority, size, deleted int
			var modifiedAt, dueDate, startDate, completedAt, deletedAt, stamps string
			if err := rows.Scan(&t.ID, &t.Title, &status, &priority, &size, &t.Project, &t.ParentID,
				&modifiedAt, &dueDate, &startDate, &completedAt, &t.Notes, &t.Recurrence,
				&t.SeqRankAtDone, &t.Stage, &deleted, &deletedAt, &stamps); err != nil {
				rows.Close()
				return nil, err
			}
			t.Status = safeStatus(status, t.ID)
			t.Priority = safePriority(priority, t.ID)
			t.Size = safeSize(size, t.ID)
			t.ModifiedAt = parseTime(modifiedAt)
			t.DueDate = parseTime(dueDate)
			t.StartDate = parseTime(startDate)
			t.CompletedAt = parseTime(completedAt)
			t.Deleted = deleted != 0
			t.DeletedAt = parseTime(deletedAt)
			t.Stamps = decodeStamps(stamps)
			out[t.ID] = t
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, set := range []struct {
			query string
			add   func(t *todo.Todo, member string)
		}{
			{`SELECT task_id, tag FROM task_tags WHERE task_id IN ` + in,
				func(t *todo.Todo, m string) { t.Tags = append(t.Tags, m) }},
			{`SELECT task_id, depends_on_id FROM task_dependencies WHERE task_id IN ` + in,
				func(t *todo.Todo, m string) { t.Dependencies = append(t.Dependencies, m) }},
		} {
			rows, err := tx.Query(set.query, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var id, member string
				if err := rows.Scan(&id, &member); err != nil {
					rows.Close()
					return nil, err
				}
				if t, ok := out[id]; ok {
					set.add(&t, member)
					out[id] = t
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// loadClock is this device's clock as the store last left it, with a new
// node name on the first stamp the store ever issues.
func loadClock(q interface {
	QueryRow(query string, args ...any) *sql.Row
}) (*hlc.Clock, error) {
	var node, last string
	switch err := q.QueryRow(`SELECT node, last FROM hlc_clock WHERE id = 1`).Scan(&node, &last); err {
	case nil:
		return hlc.New(node, hlc.Stamp(last)), nil
	case sql.ErrNoRows:
		return hlc.New(hlc.NewNode(), ""), nil
	default:
		return nil, err
	}
}

// saveClock records where the clock got to, so no process sharing the store
// and no restart reissues a stamp.
func saveClock(tx *sql.Tx, c *hlc.Clock) error {
	_, err := tx.Exec(`INSERT INTO hlc_clock (id, node, last) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET node = excluded.node, last = excluded.last`, c.Node(), string(c.Last()))
	return err
}

// encodeStamps is the stamps column: JSON, or the empty string for none.
func encodeStamps(s map[string]hlc.Stamp) string {
	if len(s) == 0 {
		return ""
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// decodeStamps reads the stamps column. An unreadable value reads as none:
// the task then merges on the stamps its recorded times give it.
func decodeStamps(s string) map[string]hlc.Stamp {
	if s == "" {
		return nil
	}
	var out map[string]hlc.Stamp
	if json.Unmarshal([]byte(s), &out) != nil {
		return nil
	}
	return out
}
