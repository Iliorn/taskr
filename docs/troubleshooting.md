# Files and troubleshooting

`taskr doctor` is the first stop: it prints the version, where every file
lives, and whether the database, settings, sync and editor are healthy. Paste
its output into a bug report.

## Where your data lives

Tasks are kept in a SQLite database. Where it and taskr's other files go
depends on what taskr finds, in this order:

1. **`TASKR_HOME`**, if set: everything in that one directory.
2. **`~/.taskr/`**, if that directory exists. Installs from before v1.32
   keep everything there, and nothing is moved.
3. Otherwise each platform's usual places:

| | Linux / BSD | macOS | Windows |
|---|---|---|---|
| Settings: `settings.json`, `sync.json` | `~/.config/taskr` | `~/Library/Application Support/taskr` | `%APPDATA%\taskr` |
| Tasks: `tasks.db` | `~/.local/share/taskr` | `~/Library/Application Support/taskr` | `%LOCALAPPDATA%\taskr` |
| State: undo history, sync state, logs | `~/.local/state/taskr` | `~/Library/Application Support/taskr` | `%LOCALAPPDATA%\taskr` |
| Cache: the `$EDITOR` scratch file | `~/.cache/taskr` | `~/Library/Caches/taskr` | `%LOCALAPPDATA%\taskr` |

An `XDG_*` variable you have set wins on every platform, macOS and Windows
included.

To back up, copy the tasks directory, or run `taskr export > backup.json`.

## "This database is from a newer taskr"

A database that a newer version has already upgraded won't open in an older
one: taskr says which version it found rather than showing an empty list.
Update taskr, or restore the `tasks.db-pre-migration-*.bak` the upgrade saved
next to it.

## After a crash

taskr writes `crash-<timestamp>.log` to the state directory, with the error,
the version, the platform, the window size and what was on screen, and saves
any edits that were still waiting to be written. The newest five are kept.
Attaching one to an issue is the whole bug report.

## When something feels slow

First try `TASKR_NO_WATCH=1 taskr`. Watching the data folder for changes is
the only thing taskr does continuously, and a synced or network home folder,
or antivirus scanning the database on every write, can make it expensive. If
that fixes it, the cost is in the watch; the trade-off is that the app stops
noticing `taskr add` from another window until it next reloads.

If it doesn't, measure: `TASKR_TRACE=1 taskr` writes one line per screen
update to `trace.log` in the state directory (`TASKR_TRACE=/path/to/file`
picks another place):

```
# time                     gap_ms  update_ms  view_ms  gc  msg
12:53:42.841      1.0      0.066    0.915   8  key down
12:53:42.863     15.9      0.641    1.632   9  main.reloadedMsg
```

Quitting adds a summary, which is usually all a report needs:

```
# summary over 412 frames (ms)
#   update  p50   0.090   p95   0.140   max   1.900
#   view    p50   0.900   p95   1.300   max   4.100
```

Large `update_ms` or `view_ms` means taskr itself is slow; a slow frame where
`gc` moved is garbage collection; a fast frame with a long `gap_ms` means the
time went somewhere outside the app: the terminal, ssh, or the keyboard
reader.

**On Windows**, taskr reads the keyboard as a stream so a key arriving after
a pause is seen at once. If that misbehaves on a particular console,
`TASKR_WIN_CONSOLE_INPUT=1` switches to Windows' own console-event reader.
