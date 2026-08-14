-- Migration 011: retire per-task "learnings".
--
-- Learnings were a second free-text list living beside notes and comments, with
-- their own table, their own sync fold and their own CLI command, but no tab of
-- their own — the feature never earned that surface. Notes already are the
-- task's long-form field, so the takeaways move there and the table goes.
--
-- Live learnings (deleted_at = '') are appended to the task's notes under a
-- "## Learnings" heading in created_at order, so nothing a user wrote is lost.
-- Tombstoned rows are deliberately dropped: they were deleted on purpose.
UPDATE todos
SET notes = TRIM(
        CASE WHEN TRIM(notes) = '' THEN '' ELSE notes || char(10) || char(10) END
        || '## Learnings' || char(10)
        || (SELECT group_concat('- ' || text, char(10))
            FROM (SELECT text FROM task_learnings
                  WHERE task_id = todos.id AND deleted_at = ''
                  ORDER BY created_at))
    )
WHERE EXISTS (SELECT 1 FROM task_learnings
              WHERE task_id = todos.id AND deleted_at = '');

DROP TABLE task_learnings;
