-- Migration 013: per-field sync stamps.
--
-- `stamps` holds a task's Stamps as JSON: per merge unit (todo/fields.go),
-- the hlc stamp of the edit that set it. '' for a row saved before stamps
-- existed, which reads its stamps from modified_at and deleted_at.
--
-- `hlc_clock` is this device's clock: its node name and the latest stamp it
-- issued or saw, so a stamp is never reissued, across restarts and across
-- the processes (TUI, CLI, server) that share the store. One row.
ALTER TABLE todos ADD COLUMN stamps TEXT NOT NULL DEFAULT '';

CREATE TABLE hlc_clock (
    id   INTEGER PRIMARY KEY CHECK (id = 1),
    node TEXT NOT NULL,
    last TEXT NOT NULL
);
