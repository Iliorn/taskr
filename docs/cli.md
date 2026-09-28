# The command line

`taskr` with no arguments opens the app; with a command it runs that command
and exits, which makes it scriptable. `taskr help` prints the full reference
and `taskr man` a man page.

```sh
taskr add "Buy milk" --size=s --due=tomorrow --p=high --tag=shopping
taskr list                       # pending top-level tasks (table)
taskr list --json --focus        # JSON, today + overdue only
taskr list --stale=30d --sort=idle --wide   # backlog review: nothing touched in a month,
                                 # longest-untouched first, with AGE and IDLE columns
taskr list --unblocked-since=14d # tasks freed recently: every blocker done, the last one this fortnight
taskr search RAM --word          # whole-word match ("RAM" won't match "Ramte"); --re for a regexp
taskr top -n=5                   # top 5 by sequence score (percent of the current field)
taskr show milk                  # full detail (incl. score breakdown + subtask IDs)
taskr why milk                   # why it ranks there: each factor's cause, the margins to the
                                 # tasks either side, and when the ranking shifts on its own
taskr edit milk --p=high --add-tag=urgent --due=tomorrow
taskr edit deploy --add-dep=sign-off   # depend on another task (refused if it would loop)
taskr edit deploy --remove-dep=sign-off
taskr edit a1b2 c3d4 e5f6 --project=hoth   # one change across several tasks (--title stays single-ref)
taskr done milk                  # mark a task done
taskr reopen milk                # move it back to pending (the counterpart to done)
taskr delete milk                # soft delete (alias: taskr rm)
taskr subtask milk "find receipt"   # create a subtask of "milk"
taskr add "Deploy release" --depends="sign-off"   # block the new task until "sign-off" is done
taskr start milk                 # start the time tracker
taskr stop                       # stop the running tracker (no ref needed)
taskr comment milk "blocked on review"
taskr comment milk --edit=1 "still blocked, asked Sam"
taskr comment milk --delete=2
taskr remind                     # the daily reminder, for a scheduler (see the guide)
taskr remind --now               # send the reminder straight away
taskr stats                      # one-line summary
taskr stats --tag=work           # same, scoped to tasks carrying a tag (also --project / --search)
taskr stats --seq                # sequence miss analysis: which score dimension buried the
                                 # tasks you finished anyway, plus a bias-tuning hint
taskr stats --format=waybar      # Waybar-shaped JSON for a status-bar widget
taskr export > backup.json       # versioned JSON snapshot of every live task
taskr export --include-done > full.json  # include completed tasks
taskr import backup.json         # merge an export file into the local store
taskr import - < backup.json     # same, reading from stdin
taskr doctor                     # this installation's health, for bug reports
taskr help
```

## Referring to tasks

A task reference is either the start of its ID (`60b9`) or part of its title
(`milk`, any case). An ID prefix wins, so scripts stay predictable. A
reference that matches several tasks fails with exit code 2 and lists them:

```
$ taskr done milk
title "milk" matches 2 tasks:
    21a164e1  Buy milk
    2ffe832a  Buy more milk
```

Flags can go before or after the reference. The CLI reads the same
`settings.json` as the app, so its ranking matches your bias settings.

The app and the CLI share one database. A running app notices changes made
from the command line (or synced from another device) and reloads, waiting
until you finish any edit you are in the middle of.

## The `--json` output is a contract

`list`, `search`, `top`, `show`, `why`, `add`, `tags`, `projects`, `doctor`,
`stats --format=json|waybar` and `export` all emit JSON, and that shape is
treated as an interface:

- **Fields are not renamed or removed in a patch or minor release.** New
  fields may be added at any time, so read the keys you need and ignore the
  rest.
- **A removal or rename waits for a major version** and is called out in the
  [changelog](../CHANGELOG.md).
- Every one of those shapes is pinned by golden files
  (`testdata/json_contract/`) that CI compares on every run.

Two things worth knowing: timestamps are always present (an unset date is
`0001-01-01T00:00:00Z`, not a missing key), and `size` is absent for
medium-sized tasks, which is the default.

## Shell completion and man page

Both are generated from the same command table the CLI runs from, so they
can't fall behind a new command or flag. Task references complete from your
open tasks.

```sh
taskr completion bash > /etc/bash_completion.d/taskr
taskr completion zsh > "${fpath[1]}/_taskr"
taskr completion fish > ~/.config/fish/completions/taskr.fish
taskr man > ~/.local/share/man/man1/taskr.1
```

## Export and import

`taskr export` writes a versioned JSON envelope to stdout:

```json
{
  "version": 1,
  "exported_at": "2026-07-10T12:00:00Z",
  "tasks": [ ... ]
}
```

The main fields of each task:

| Field | Type | Notes |
|-------|------|-------|
| `id` | string (UUID) | stable identifier, used as the merge key |
| `title` | string | |
| `status` | int | 0 = pending, 1 = done |
| `priority` | int | 0 = low, 1 = medium, 2 = high |
| `size` | int | 0 = medium, 1 = small, 2 = large |
| `created_at` / `modified_at` | RFC 3339 | the later `modified_at` wins a conflict |
| `due_date` | RFC 3339 (omitempty) | |
| `project` | string (omitempty) | |
| `tags` | string array (omitempty) | |
| `dependencies` | string array (omitempty) | IDs of tasks this one is blocked by |
| `comments` | array (omitempty) | each has `id`, `text`, `created_at` |
| `deleted` / `deleted_at` | bool / RFC 3339 | deletion markers, so a delete syncs |
| `parent_id` | string (omitempty) | set on subtasks |

`taskr import <file>` (or `taskr import -` for stdin) merges the file into
your tasks with the same merge that powers sync. It **never replaces**
anything wholesale, so `export | import` is always safe and importing the same
file twice changes nothing. A bare JSON array of tasks is accepted too.
