-- Migration 014: who wrote a comment, and who tracked a time entry.
--
-- Set by the save that first stores the record, and never cleared by a later
-- save of a copy that has not heard of it yet (saveNormalizedIn keeps a
-- stored author over an empty one). '' for records from before authors.
ALTER TABLE task_comments ADD COLUMN author TEXT NOT NULL DEFAULT '';
ALTER TABLE task_time_entries ADD COLUMN author TEXT NOT NULL DEFAULT '';
