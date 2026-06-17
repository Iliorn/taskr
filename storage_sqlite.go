package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"time"

	"taskr/todo"

	_ "modernc.org/sqlite"
)

// SQLite is the live storage backend. After migration 002, every field is
// queryable from SQL — scalars live in dedicated columns on `todos`, and
// nested data (tags, dependencies, comments, learnings, time entries) lives in
// child tables joined by task_id. The legacy `data` column survives this
// migration for safety (a future migration can drop it) but is no longer the
// source of truth: the adapter reads from the normalized tables exclusively.
// Deletes are soft (tombstones) so a deletion can propagate in a future sync
// adapter instead of the row silently reappearing.

func dbPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "tasks.db")
}

var (
	db     *sql.DB
	dbOnce sync.Once
	dbErr  error
)

// openStore opens (once) the SQLite database, creates the schema, and — only if
// the todos table did not previously exist — imports any legacy JSON file.
// Gating import on a freshly-created table avoids "zombie" tasks: deleting every
// task in the app must not re-import the old JSON on the next launch.
func openStore() error {
	dbOnce.Do(func() {
		if err := ensureStorageDir(); err != nil {
			dbErr = err
			return
		}
		db, dbErr = openStoreAt(dbPath())
	})
	return dbErr
}

// openStoreAt opens the database at path, applies the schema, and imports the
// legacy JSON only when the todos table did not already exist. Split out from
// openStore (which owns the package-level singleton) so tests can drive it
// against a throwaway path.
func openStoreAt(path string) (*sql.DB, error) {
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Single connection: a personal TUI has one writer, and serializing all
	// access sidesteps connection-pool pragma quirks (busy_timeout is
	// per-connection). WAL persists on the file once set.
	handle.SetMaxOpenConns(1)
	// synchronous=NORMAL is the major fsync-lag fix: with WAL it remains
	// crash-safe (consistent on power loss, only the last committed txn at risk)
	// while skipping the per-commit fsync of FULL. mmap_size enables zero-copy
	// reads for typical working sets. foreign_keys is enabled now so future
	// migrations can rely on ON DELETE CASCADE.
	if _, err := handle.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
		PRAGMA busy_timeout=5000;
		PRAGMA foreign_keys=ON;
		PRAGMA temp_store=MEMORY;
		PRAGMA mmap_size=67108864;
		PRAGMA wal_autocheckpoint=1000;
	`); err != nil {
		handle.Close()
		return nil, err
	}

	fresh := !tableExists(handle, "todos")
	if err := runMigrations(handle); err != nil {
		handle.Close()
		return nil, err
	}
	if fresh {
		if err := importFromJSON(handle); err != nil {
			handle.Close()
			return nil, err
		}
	}
	return handle, nil
}

func tableExists(h *sql.DB, name string) bool {
	var found string
	err := h.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&found)
	return err == nil
}

// importFromJSON seeds a fresh database from the legacy tasks.json. A missing or
// corrupt JSON file is not fatal — a new user simply starts empty.
func importFromJSON(h *sql.DB) error {
	todos, err := loadTodosJSON()
	if err != nil || len(todos) == 0 {
		return nil
	}
	ptrs := make([]*todo.Todo, len(todos))
	for i := range todos {
		ptrs[i] = &todos[i]
	}
	return saveNormalized(h, ptrs, nil)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// saveNormalized writes the dirty tasks and tombstones to the normalized
// schema. One transaction; per task the scalars go into `todos` and the
// children replace the previous child rows. Untouched tasks are not rewritten.
func saveNormalized(h *sql.DB, dirty []*todo.Todo, tombstones []string) error {
	if len(dirty) == 0 && len(tombstones) == 0 {
		return nil
	}
	tx, err := h.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if len(dirty) > 0 {
		// `data` is the legacy blob column — still NOT NULL after migration 002
		// (we deliberately didn't drop it, so a future migration can if/when
		// rollback is no longer a concern). Write an empty string; the column
		// is no longer the source of truth.
		upsertTask, err := tx.Prepare(`INSERT INTO todos
			(id,title,status,priority,size,project,parent_id,created_at,modified_at,due_date,start_date,notes,completed_at,sequence,data,deleted,deleted_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,'',0,'')
			ON CONFLICT(id) DO UPDATE SET
				title=excluded.title, status=excluded.status, priority=excluded.priority,
				size=excluded.size, project=excluded.project, parent_id=excluded.parent_id,
				created_at=excluded.created_at, modified_at=excluded.modified_at,
				due_date=excluded.due_date, start_date=excluded.start_date,
				notes=excluded.notes, completed_at=excluded.completed_at,
				sequence=excluded.sequence, deleted=0, deleted_at=''`)
		if err != nil {
			return err
		}
		defer upsertTask.Close()

		deleteChildren := func(table string) (*sql.Stmt, error) {
			return tx.Prepare(`DELETE FROM ` + table + ` WHERE task_id=?`)
		}
		delTags, err := deleteChildren("task_tags")
		if err != nil {
			return err
		}
		defer delTags.Close()
		delDeps, err := deleteChildren("task_dependencies")
		if err != nil {
			return err
		}
		defer delDeps.Close()
		delComments, err := deleteChildren("task_comments")
		if err != nil {
			return err
		}
		defer delComments.Close()
		delLearnings, err := deleteChildren("task_learnings")
		if err != nil {
			return err
		}
		defer delLearnings.Close()
		delEntries, err := deleteChildren("task_time_entries")
		if err != nil {
			return err
		}
		defer delEntries.Close()

		insTag, err := tx.Prepare(`INSERT INTO task_tags (task_id, tag) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer insTag.Close()
		insDep, err := tx.Prepare(`INSERT INTO task_dependencies (task_id, depends_on_id) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer insDep.Close()
		insComment, err := tx.Prepare(`INSERT INTO task_comments (id, task_id, text, created_at) VALUES (?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insComment.Close()
		insLearning, err := tx.Prepare(`INSERT INTO task_learnings (id, task_id, text, created_at) VALUES (?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insLearning.Close()
		insEntry, err := tx.Prepare(`INSERT INTO task_time_entries (id, task_id, started_at, stopped_at) VALUES (?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insEntry.Close()

		for _, t := range dirty {
			if _, err := upsertTask.Exec(t.ID, t.Title, int(t.Status), int(t.Priority), int(t.Size),
				t.Project, t.ParentID, fmtTime(t.CreatedAt), fmtTime(t.ModifiedAt),
				fmtTime(t.DueDate), fmtTime(t.StartDate), t.Notes, fmtTime(t.CompletedAt),
				sequenceScore(t)); err != nil {
				return err
			}
			// Replace-all-children: simple and bounded — typical tasks have
			// <10 entries per child kind, so the cost is tens of writes per
			// dirty task even on rich tasks. A future field-level diff
			// optimization can replace this when scale demands.
			for _, table := range []*sql.Stmt{delTags, delDeps, delComments, delLearnings, delEntries} {
				if _, err := table.Exec(t.ID); err != nil {
					return err
				}
			}
			for _, tag := range t.Tags {
				if _, err := insTag.Exec(t.ID, tag); err != nil {
					return err
				}
			}
			for _, dep := range t.Dependencies {
				if _, err := insDep.Exec(t.ID, dep); err != nil {
					return err
				}
			}
			for _, c := range t.Comments {
				if _, err := insComment.Exec(c.ID, t.ID, c.Text, fmtTime(c.CreatedAt)); err != nil {
					return err
				}
			}
			for _, l := range t.Learnings {
				if _, err := insLearning.Exec(l.ID, t.ID, l.Text, fmtTime(l.CreatedAt)); err != nil {
					return err
				}
			}
			for _, e := range t.TimeEntries {
				if _, err := insEntry.Exec(e.ID, t.ID, fmtTime(e.StartedAt), fmtTime(e.StoppedAt)); err != nil {
					return err
				}
			}
		}
	}

	if len(tombstones) > 0 {
		now := fmtTime(time.Now())
		tomb, err := tx.Prepare(`UPDATE todos SET deleted=1, deleted_at=? WHERE id=?`)
		if err != nil {
			return err
		}
		defer tomb.Close()
		for _, id := range tombstones {
			if _, err := tomb.Exec(now, id); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// loadTodosFromDB reads every live row plus its children, assembling them
// into the in-memory todo.Todo shape. Children are read with one query each
// and merged by task_id in a single pass.
func loadTodosFromDB(h *sql.DB) ([]todo.Todo, error) {
	rows, err := h.Query(`SELECT id, title, status, priority, size, project, parent_id,
		created_at, modified_at, due_date, start_date, completed_at, notes
		FROM todos WHERE deleted = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	todos := make(map[string]*todo.Todo, 64)
	var ordered []string
	for rows.Next() {
		var t todo.Todo
		var status, priority, size int
		var createdAt, modifiedAt, dueDate, startDate, completedAt string
		if err := rows.Scan(&t.ID, &t.Title, &status, &priority, &size, &t.Project,
			&t.ParentID, &createdAt, &modifiedAt, &dueDate, &startDate,
			&completedAt, &t.Notes); err != nil {
			return nil, err
		}
		t.Status = todo.Status(status)
		t.Priority = todo.Priority(priority)
		t.Size = todo.Size(size)
		t.CreatedAt = parseTime(createdAt)
		t.ModifiedAt = parseTime(modifiedAt)
		t.DueDate = parseTime(dueDate)
		t.StartDate = parseTime(startDate)
		t.CompletedAt = parseTime(completedAt)
		todos[t.ID] = &t
		ordered = append(ordered, t.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	if err := loadChildren(h, todos, "task_tags", "task_id, tag",
		func(t *todo.Todo, s *sql.Rows) error {
			var taskID, tag string
			if err := s.Scan(&taskID, &tag); err != nil {
				return err
			}
			if t = todos[taskID]; t != nil {
				t.Tags = append(t.Tags, tag)
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := loadChildren(h, todos, "task_dependencies", "task_id, depends_on_id",
		func(t *todo.Todo, s *sql.Rows) error {
			var taskID, dep string
			if err := s.Scan(&taskID, &dep); err != nil {
				return err
			}
			if t = todos[taskID]; t != nil {
				t.Dependencies = append(t.Dependencies, dep)
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := loadChildren(h, todos, "task_comments", "id, task_id, text, created_at",
		func(t *todo.Todo, s *sql.Rows) error {
			var c todo.Comment
			var taskID, createdAt string
			if err := s.Scan(&c.ID, &taskID, &c.Text, &createdAt); err != nil {
				return err
			}
			c.CreatedAt = parseTime(createdAt)
			if t = todos[taskID]; t != nil {
				t.Comments = append(t.Comments, c)
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := loadChildren(h, todos, "task_learnings", "id, task_id, text, created_at",
		func(t *todo.Todo, s *sql.Rows) error {
			var l todo.Learning
			var taskID, createdAt string
			if err := s.Scan(&l.ID, &taskID, &l.Text, &createdAt); err != nil {
				return err
			}
			l.CreatedAt = parseTime(createdAt)
			if t = todos[taskID]; t != nil {
				t.Learnings = append(t.Learnings, l)
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := loadChildren(h, todos, "task_time_entries", "id, task_id, started_at, stopped_at",
		func(t *todo.Todo, s *sql.Rows) error {
			var e todo.TimeEntry
			var taskID, startedAt, stoppedAt string
			if err := s.Scan(&e.ID, &taskID, &startedAt, &stoppedAt); err != nil {
				return err
			}
			e.StartedAt = parseTime(startedAt)
			e.StoppedAt = parseTime(stoppedAt)
			if t = todos[taskID]; t != nil {
				t.TimeEntries = append(t.TimeEntries, e)
			}
			return nil
		}); err != nil {
		return nil, err
	}

	out := make([]todo.Todo, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, *todos[id])
	}
	return out, nil
}

func loadChildren(h *sql.DB, todos map[string]*todo.Todo, table, cols string,
	scan func(*todo.Todo, *sql.Rows) error,
) error {
	rows, err := h.Query(`SELECT ` + cols + ` FROM ` + table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(nil, rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// loadTodos is the storage entry point used by initialModel: open the store
// (importing legacy JSON on first run) and return the live tasks, sorted.
func loadTodos() ([]todo.Todo, error) {
	if err := openStore(); err != nil {
		return nil, err
	}
	todos, err := loadTodosFromDB(db)
	if err != nil {
		return nil, err
	}
	sortTodosByMode(todos, taskSortSequence)
	return todos, nil
}

// sqliteRepo is the SQLite Repository adapter. It reuses the package-level
// connection opened lazily by openStore.
type sqliteRepo struct{}

func newSQLiteRepo() *sqliteRepo { return &sqliteRepo{} }

func (r *sqliteRepo) Load() ([]todo.Todo, error) {
	return loadTodos()
}

// ResyncScores rewrites the persisted `sequence` column for every live row
// at the current activeBiases. See Repository.ResyncScores for the why.
func (r *sqliteRepo) ResyncScores() error {
	if err := openStore(); err != nil {
		return err
	}
	return resyncSequenceColumn(db)
}

// resyncSequenceColumn is the worker: load the score-relevant fields of
// every live row, compute sequenceScore in Go using the current activeBiases,
// and write back only the score column in one transaction. Touches no
// child tables and no other scalars — cheap even on large task sets.
func resyncSequenceColumn(h *sql.DB) error {
	rows, err := h.Query(`SELECT id, status, priority, size, due_date, created_at
		FROM todos WHERE deleted = 0`)
	if err != nil {
		return err
	}
	type scored struct {
		id    string
		score float64
	}
	var updates []scored
	for rows.Next() {
		var t todo.Todo
		var status, priority, size int
		var due, created string
		if err := rows.Scan(&t.ID, &status, &priority, &size, &due, &created); err != nil {
			rows.Close()
			return err
		}
		t.Status = todo.Status(status)
		t.Priority = todo.Priority(priority)
		t.Size = todo.Size(size)
		t.DueDate = parseTime(due)
		t.CreatedAt = parseTime(created)
		updates = append(updates, scored{t.ID, sequenceScore(&t)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tx, err := h.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`UPDATE todos SET sequence = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, u := range updates {
		if _, err := stmt.Exec(u.score, u.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TopBySequence returns the top n highest-scoring open tasks (status=Pending),
// backed by the (deleted, status, sequence DESC) index. O(log N + n) at any
// scale — the foundation for "what should I do next?" / auto-planning
// features that want to skip loading the whole task set.
func (r *sqliteRepo) TopBySequence(n int) ([]string, error) {
	if err := openStore(); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id FROM todos
		WHERE deleted = 0 AND status = 0
		ORDER BY sequence DESC, due_date ASC
		LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, n)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Save writes dirty tasks to the normalized schema and tombstones the explicit
// ID set, all in one transaction. Differential: untouched rows are not
// rewritten; vanished rows must be in tombstones.
//
// Callers must guarantee the *Todo pointers are stable for the duration of
// the call (no concurrent mutation). The Store.drainDirty path hands us a
// deep-copied snapshot, which satisfies this.
func (r *sqliteRepo) Save(dirty []*todo.Todo, tombstones []string) error {
	if err := openStore(); err != nil {
		return err
	}
	return saveNormalized(db, dirty, tombstones)
}
