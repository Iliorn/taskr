-- Migration 010: add `stage` — the kanban board column a pending top-level
-- task sits in, stored as the configured stage name verbatim (settings.json
-- "stages"; Backlog / In progress / Review by default).
--
-- DEFAULT '' = "first configured stage", so existing tasks land in Backlog
-- with no backfill, and a stage later renamed in settings strands its tasks
-- visibly in the first column instead of hiding them. Completion is NOT a
-- stage: the board's final column is status=Done itself, so "done" never has
-- two sources of truth.
ALTER TABLE todos ADD COLUMN stage TEXT NOT NULL DEFAULT '';
