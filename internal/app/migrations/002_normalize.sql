-- Migration 002: split the JSON blob into normalized child tables.
--
-- The SQL portion creates the child tables and indexes. The Go portion
-- (registered in migration_002.go) reads each row's `data` blob, parses the
-- JSON, and INSERTs into the child tables. After backfill the `data` column
-- is no longer the source of truth; we keep it through this migration for
-- safety (so an aborted run can be retried), and a future migration can drop
-- it once we're confident reads come exclusively from the normalized tables.
--
-- New scalar columns on `todos`:
--   notes        — was inside the JSON blob
--   completed_at — was inside the JSON blob
--   urgency      — derived; maintained by the Store on save, indexed for
--                  "next up" queries (Step 13).
ALTER TABLE todos ADD COLUMN notes        TEXT NOT NULL DEFAULT '';
ALTER TABLE todos ADD COLUMN completed_at TEXT NOT NULL DEFAULT '';
ALTER TABLE todos ADD COLUMN urgency      REAL NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS task_tags (
    task_id TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (task_id, tag)
);
CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id       TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    depends_on_id TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, depends_on_id)
);
CREATE TABLE IF NOT EXISTS task_comments (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS task_learnings (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    text       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS task_time_entries (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    started_at TEXT NOT NULL,
    stopped_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_task_tags_tag        ON task_tags(tag);
CREATE INDEX IF NOT EXISTS idx_time_entries_task    ON task_time_entries(task_id, started_at);
CREATE INDEX IF NOT EXISTS idx_dependencies_dep_on  ON task_dependencies(depends_on_id);
CREATE INDEX IF NOT EXISTS idx_todos_parent         ON todos(parent_id) WHERE deleted = 0;
CREATE INDEX IF NOT EXISTS idx_todos_urgency        ON todos(deleted, status, urgency DESC);
