-- Migration 012: add `sync_base` — per task, the version this device last
-- agreed on with its sync server, as the task's canonical JSON without its
-- comments and time entries (which merge by their own IDs).
--
-- It is what lets a sync keep two devices' edits to different fields of one
-- task (tasksync.ThreeWay): a field that differs from the base was changed on
-- that side. Local to this device and never synced; a task with no row here
-- merges last-writer-wins as before.
CREATE TABLE sync_base (
    id   TEXT PRIMARY KEY,
    data TEXT NOT NULL
);
