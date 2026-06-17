-- Hybrid schema (todos table + JSON blob + queryable column mirrors).
-- Captured as migration 001 so existing databases continue to work unchanged
-- and new databases get this exact shape. The IF NOT EXISTS clauses make this
-- migration safe to apply on databases that already have the table from the
-- pre-migrator era.
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
