-- Migration 009: add `seq_rank_done` — the 1-based position a task held in
-- the top-level sequence ranking at the moment the user completed it. Feeds
-- the "sequence hit rate" stat (how often what you finish is what the engine
-- had on top), the feedback loop for tuning the sequence biases.
--
-- DEFAULT 0 = not recorded: legacy completions, subtasks, auto-closed parents.
-- The hit-rate stat skips zeros, so history before this migration simply
-- doesn't count rather than counting as misses.
ALTER TABLE todos ADD COLUMN seq_rank_done INTEGER NOT NULL DEFAULT 0;
