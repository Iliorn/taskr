-- Migration 006: add `deleted_at` tombstone columns to the child tables that
-- carry their own IDs (comments, learnings, time entries), so a child deleted
-- on one device propagates during cross-device sync instead of reappearing
-- from another device's stale copy.
--
-- Tags and dependencies are intentionally excluded: they are value-sets with no
-- per-row identity, so they resolve by whole-set last-writer-wins and need no
-- tombstone column.
--
-- DEFAULT '' means "live" — every existing child row migrates as not-deleted.
-- The live load filters `deleted_at = ''`; the sync load keeps tombstones so
-- deletions can be merged.
ALTER TABLE task_comments ADD COLUMN deleted_at TEXT NOT NULL DEFAULT '';
ALTER TABLE task_learnings ADD COLUMN deleted_at TEXT NOT NULL DEFAULT '';
ALTER TABLE task_time_entries ADD COLUMN deleted_at TEXT NOT NULL DEFAULT '';
