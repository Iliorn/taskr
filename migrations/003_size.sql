-- Migration 003: add the `size` column used by the sequencing score's
-- Momentum dimension.
--
-- DEFAULT 0 maps to todo.SizeMedium (the zero value of the Go enum), so every
-- existing row migrates to Medium = a neutral 5.0-pt contribution. Users can
-- relabel individual tasks via the detail view or `size:s|m|l` in quick-add.
ALTER TABLE todos ADD COLUMN size INTEGER NOT NULL DEFAULT 0;
