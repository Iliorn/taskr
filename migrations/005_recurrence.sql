-- Migration 005: add the `recurrence` column used by recurring tasks.
--
-- DEFAULT '' maps to "not recurring" — the zero value of todo.Todo.Recurrence
-- (a string field). Existing rows migrate as non-recurring; users can set a
-- rule via quick-add (`r:daily`) or the detail view.
ALTER TABLE todos ADD COLUMN recurrence TEXT NOT NULL DEFAULT '';
