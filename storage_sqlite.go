package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"taskr/todo"

	_ "modernc.org/sqlite"
)

// SQLite is the live storage backend. The legacy JSON file (storage.go) is kept
// only as the one-time import source on first run and as a corruption fallback.
//
// Schema is "hybrid + sync-ready": the full todo.Todo is stored as a JSON blob
// in `data` (the source of truth, reconstructed losslessly on load), while the
// queryable fields are mirrored into real columns for indexing and future
// filter features. Deletes are soft (tombstones) so a deletion on one machine
// can propagate during Phase-2 sync instead of the row silently reappearing.

func dbPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "tasks.db")
}

var (
	db     *sql.DB
	dbOnce sync.Once
	dbErr  error
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS todos (
	id          TEXT PRIMARY KEY,
	title       TEXT NOT NULL,
	status      INTEGER NOT NULL,
	priority    INTEGER NOT NULL,
	project     TEXT NOT NULL DEFAULT '',
	parent_id   TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL DEFAULT '',
	modified_at TEXT NOT NULL DEFAULT '',
	due_date    TEXT NOT NULL DEFAULT '',
	start_date  TEXT NOT NULL DEFAULT '',
	data        TEXT NOT NULL,
	deleted     INTEGER NOT NULL DEFAULT 0,
	deleted_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_todos_live ON todos(deleted, status, due_date);
`

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
	if _, err := handle.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		handle.Close()
		return nil, err
	}

	fresh := !tableExists(handle, "todos")
	if _, err := handle.Exec(schemaSQL); err != nil {
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
	rows := make([]todoRow, 0, len(todos))
	for i := range todos {
		r, err := encodeRow(&todos[i])
		if err != nil {
			return err
		}
		rows = append(rows, r)
	}
	return syncRowsToDB(h, rows)
}

// todoRow is a todo serialized for the columns + JSON blob. Encoding happens
// synchronously (off the async save Cmd) so the snapshot can't race a later
// mutation of m.todos.
type todoRow struct {
	id, title             string
	status, priority      int
	project, parentID     string
	createdAt, modifiedAt string
	dueDate, startDate    string
	data                  string
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func encodeRow(t *todo.Todo) (todoRow, error) {
	blob, err := json.Marshal(t)
	if err != nil {
		return todoRow{}, err
	}
	return todoRow{
		id:         t.ID,
		title:      t.Title,
		status:     int(t.Status),
		priority:   int(t.Priority),
		project:    t.Project,
		parentID:   t.ParentID,
		createdAt:  fmtTime(t.CreatedAt),
		modifiedAt: fmtTime(t.ModifiedAt),
		dueDate:    fmtTime(t.DueDate),
		startDate:  fmtTime(t.StartDate),
		data:       string(blob),
	}, nil
}

// syncRowsToDB upserts every current row and tombstones any live row that is no
// longer present, all in one transaction. This mirrors the old "save the whole
// snapshot" semantics while preserving deletions for sync.
func syncRowsToDB(h *sql.DB, rows []todoRow) error {
	tx, err := h.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	upsert, err := tx.Prepare(`INSERT INTO todos
		(id,title,status,priority,project,parent_id,created_at,modified_at,due_date,start_date,data,deleted,deleted_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,0,'')
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, status=excluded.status, priority=excluded.priority,
			project=excluded.project, parent_id=excluded.parent_id,
			created_at=excluded.created_at, modified_at=excluded.modified_at,
			due_date=excluded.due_date, start_date=excluded.start_date,
			data=excluded.data, deleted=0, deleted_at=''`)
	if err != nil {
		return err
	}
	defer upsert.Close()

	live := make(map[string]bool, len(rows))
	for _, r := range rows {
		live[r.id] = true
		if _, err := upsert.Exec(r.id, r.title, r.status, r.priority, r.project,
			r.parentID, r.createdAt, r.modifiedAt, r.dueDate, r.startDate, r.data); err != nil {
			return err
		}
	}

	// Tombstone rows that vanished from the current set.
	existing, err := tx.Query(`SELECT id FROM todos WHERE deleted=0`)
	if err != nil {
		return err
	}
	var stale []string
	for existing.Next() {
		var id string
		if err := existing.Scan(&id); err != nil {
			existing.Close()
			return err
		}
		if !live[id] {
			stale = append(stale, id)
		}
	}
	existing.Close()
	if err := existing.Err(); err != nil {
		return err
	}

	now := fmtTime(time.Now())
	for _, id := range stale {
		if _, err := tx.Exec(`UPDATE todos SET deleted=1, deleted_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func loadTodosFromDB(h *sql.DB) ([]todo.Todo, error) {
	rows, err := h.Query(`SELECT data FROM todos WHERE deleted=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var todos []todo.Todo
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var t todo.Todo
		if err := json.Unmarshal([]byte(data), &t); err != nil {
			return nil, err
		}
		todos = append(todos, t)
	}
	return todos, rows.Err()
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
	sortTodosByMode(todos, taskSortDueDate)
	return todos, nil
}

// prepareSave encodes the current todos synchronously (so the async write can't
// race a later mutation) and returns a Cmd that commits them to SQLite.
func prepareSave(todos []todo.Todo) (tea.Cmd, error) {
	if err := openStore(); err != nil {
		return nil, err
	}
	rows := make([]todoRow, 0, len(todos))
	for i := range todos {
		r, err := encodeRow(&todos[i])
		if err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return func() tea.Msg {
		if err := syncRowsToDB(db, rows); err != nil {
			return saveErrMsg{err}
		}
		return saveDoneMsg{}
	}, nil
}
