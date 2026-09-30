-- Migration 013: task history.
--
-- One row per event (todo.Event): who changed a task, how, and what. Events
-- are written once and never updated or tombstoned, so a save inserts with
-- OR IGNORE and a sync merge is a union by id. `fields` is the changed units,
-- comma-separated.
CREATE TABLE task_events (
    id      TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    at      TEXT NOT NULL,
    author  TEXT NOT NULL DEFAULT '',
    source  TEXT NOT NULL DEFAULT '',
    action  TEXT NOT NULL,
    fields  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_task_events_task ON task_events(task_id, at);
