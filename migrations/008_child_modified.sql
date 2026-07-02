-- Migration 008: add `modified_at` to the identified child tables (comments,
-- learnings, time entries) so the sync merge can resolve two live versions of
-- the same record by recency instead of a content-hash coin flip. Without it,
-- editing a comment on one device could lose to the stale copy from another,
-- and a stopped timer could lose to its still-running copy and resurrect.
--
-- DEFAULT '' = written before the field existed; the merge falls back to a
-- hash tiebreak for those (and to stopped_at for time entries).
ALTER TABLE task_comments ADD COLUMN modified_at TEXT NOT NULL DEFAULT '';
ALTER TABLE task_learnings ADD COLUMN modified_at TEXT NOT NULL DEFAULT '';
ALTER TABLE task_time_entries ADD COLUMN modified_at TEXT NOT NULL DEFAULT '';
