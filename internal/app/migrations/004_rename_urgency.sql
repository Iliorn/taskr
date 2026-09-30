-- Migration 004: rename the score column to match what it actually stores.
--
-- The column was originally added as `urgency` (a simple deadline+priority+age
-- heuristic). v1.11.0 replaced that formula with the 4-dimension Sequence
-- score but kept the legacy column name. The mismatch was confusing — anyone
-- inspecting the schema would assume the column holds "urgency" in some
-- generic sense, when in fact it holds the user-tunable Sequence score
-- computed by sequenceScore(t).
--
-- SQLite ≥ 3.25 supports ALTER TABLE … RENAME COLUMN, so the rename is
-- in-place and preserves every existing value. The index is dropped and
-- recreated under the new name because SQLite indexes don't auto-rename
-- when their underlying column does.
ALTER TABLE todos RENAME COLUMN urgency TO sequence;
DROP INDEX IF EXISTS idx_todos_urgency;
CREATE INDEX IF NOT EXISTS idx_todos_sequence ON todos(deleted, status, sequence DESC);
