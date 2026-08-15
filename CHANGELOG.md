# Changelog

Notable changes to taskr. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions are the git tags the [release workflow](.github/workflows/release.yml) builds from.

Entries describe what changed for someone *using* taskr. Refactors and test work
belong in the commit log, not here — unless they change behaviour.

## [Unreleased]

### Security

- **Self-update now verifies what it downloads.** Every release publishes a
  `SHA256SUMS` file; until now the update button ignored it and installed the
  binary unchecked. The download is hashed as it is written and compared
  against the published checksum, and anything that cannot be verified — no
  `SHA256SUMS`, no entry for this platform, a mismatch — installs nothing and
  says why. This is an integrity check, not a signature.

### Changed

- **Files follow platform conventions instead of one dot-directory.** A new
  install puts config in `$XDG_CONFIG_HOME/taskr`, the database in
  `$XDG_DATA_HOME/taskr`, undo/sync state and logs in `$XDG_STATE_HOME/taskr`
  and the editor scratch file in `$XDG_CACHE_HOME/taskr` — `%APPDATA%` /
  `%LOCALAPPDATA%` on Windows, `~/Library/Application Support` on macOS, and an
  explicitly exported `XDG_*` variable wins everywhere. **An existing
  `~/.taskr` keeps being used exactly as before**: nothing moves, nothing is
  migrated. `TASKR_HOME` puts everything back in one directory, and
  `taskr doctor` prints what it resolved.

### Removed

- **Learnings.** A second free-text list beside notes and comments, with its own
  table, its own sync fold and its own CLI command, but never a tab of its own.
  Migration 011 appends every learning to its task's notes under a
  `## Learnings` heading and drops the table, so nothing you wrote is lost —
  and `taskr search` now matches notes, which is where the recall
  `taskr learnings` used to provide now comes from. `taskr learnings` prints
  where the text went instead of silently opening the TUI.

### Added

- **`j`/`k` move the cursor**, everywhere `↑`/`↓` do: the task lists, the detail
  pane, the drill-ins, the board, the calendar, settings, and scrolling the help
  overlay. They are aliases resolved at dispatch rather than per-switch cases,
  so a context that answers an arrow answers them too — and a key you rebind
  onto `j` or `k` still wins over the alias.
- **The score reads as a percentage of the current field.** The raw score is
  unbounded upward — Age alone adds 0.2/day forever — so "24.4" in the Score
  column asked to be calibrated against a scale nobody had published, and read
  as an arbitrary number. Everywhere the score is *shown* (the Tasks column,
  the detail pane, the Settings preview, `taskr top`, `taskr show`) it is now a
  percentage, where 100% is the highest-scoring pending task right now. The
  points survive where the arithmetic is being explained — the `w` overlay and
  `taskr why` — and those state what 100% currently costs in points, so the
  mark moving when you finish the top task is visible rather than mysterious.
  `taskr top --json` keeps `score` and gains `percent`.
- **`/` filters the Board.** The columns were already a projection of the same
  filtered lists the Tasks tab shows, but there was no way to set the filter
  from the Board. `#tag`, `@project` and free text now narrow every column at
  once, with the same chip in the status line as everywhere else.
- **`w` explains a task's rank**, and `taskr why <ref>` prints the same answer.
  The sequencing score decided the order and showed you one number for it, so a
  list that reordered itself was something to argue with rather than follow.
  The overlay breaks the score into its five factors with the multiplication
  each bias applied *and the reason behind each reading* ("3 days overdue",
  "project @House saw activity in the last 48h", "nothing here was touched in
  the last 48h"), states the margin to the tasks directly above and below, and
  — the part no breakdown could answer — forecasts the moments the ranking
  moves with no edit from you: the deadline ramp stepping at midnight and
  momentum expiring 48h after the last signal, each with the score and the
  position it lands on.
- **The parser now speaks the interface's language.** Danish and German
  translated everything you *read* and nothing you *type*: a fully Danish
  screen still wanted `due:friday p:high`, and the help advertised those
  English tokens under Danish headings. With the language set, the quick-add
  and search grammars accept that language's words as well — `frist:imorgen`,
  `p:høj`, `størrelse:lille`, `forfalden`, `fällig:freitag`, `p:hoch`,
  `überfällig` — including weekday names, taken from the same tables the
  calendar prints. The spellings are read out of the translation table the
  interface renders from, so the help, the input hints and the parser cannot
  disagree. English keeps working in every language, and only input is
  localized: stored data and the sync format stay English, so installs in
  different languages sync unchanged. (The CLI stays English on both sides.)
- **`TASKR_NO_WATCH=1`** turns off live reload. The filesystem watcher is the
  only thing taskr does continuously against the OS, so it is the first thing
  to remove when input feels laggy — at the cost of not noticing another
  shell's `taskr add` until the next reload.
- **`TASKR_TRACE=1`** writes a per-frame latency log to `~/.taskr/trace.log`:
  wall clock, gap since the last frame, `Update` and `View` times, GC cycles,
  and the message, plus a percentile summary on quit. Off unless asked for.

### Fixed

- **Windows input latency.** A keystroke after a pause waited up to 16 ms to be
  noticed: Bubble Tea reads the Windows console by polling it with a 16 ms
  sleep between attempts, and a burst of keys spins the loop instead of
  sleeping — the first key felt late, the rest did not. taskr now opens
  `CONIN$` as its own file, which selects a blocking read of the console's
  escape-sequence stream and returns the moment a key arrives.
  `TASKR_WIN_CONSOLE_INPUT=1` restores the old reader; the Windows build polls
  the console size four times a second, since resize events only came with it.
- **The first frame is rendered before the program starts.** Building every
  derived cache and filling the string-builder pool used to happen on the first
  keystroke of a session.
- **The renderer now paints at 120 FPS instead of 60.** Bubble Tea repaints on
  a ticker, so the frame rate is also the worst case between pressing a key and
  seeing it: ~17 ms became ~8 ms. Frames are line-diffed, so the extra ticks
  cost nothing when nothing changed.
- **A stutter on the first keystroke, and again after a pause.** A filesystem
  event on our own database write made the app reload and rebuild its whole
  task set — about 15 ms at 2000 tasks, on the event loop, landing on whatever
  key you pressed next. A reload that carries no new task versions is now
  recognised and skipped (~1 ms), and the startup write no longer comes back as
  an external change at all.

### Changed

- **Search matches notes**, not just the title. Titles stay fuzzy; notes are
  matched as a plain substring, since a subsequence match over a whole note
  would hit almost anything.

### Added

- **`taskr doctor` now diagnoses the installation**: version, platform, data
  directory, database size and SQLite integrity, schema version, settings and
  keybinding problems, sync configuration and last sync, and the resolved
  editor. It is the output to paste into a bug report, it never prints a sync
  token, `--json` makes it machine-readable, and it exits non-zero when
  something is actually broken.
- **A version on the sync wire.** Client and server now agree on a protocol
  version and refuse a payload they might misread, with a message naming which
  side to upgrade. Clients from before this change keep working unchanged.
- **Fuzz tests** for the quick-add, search, due-date and time-entry parsers,
  with a bounded CI job that keeps mutating.
- **Arch Linux and Windows packages.** `yay -S taskr-bin` on Arch (with shell
  completions and the man page installed), and
  `scoop install https://github.com/Iliorn/taskr/releases/latest/download/taskr.json`
  on Windows. Both manifests are generated by the release workflow from the
  binaries it just built, so their checksums cannot drift from the release.
- **German UI** (`"language": "de"`, or cycle it in Settings). All 482 UI
  strings plus the month and weekday tables. The no-wrap guard that used to
  check Danish now sweeps every language, so a future one inherits it.
- **A security policy** (`SECURITY.md`) with a private reporting channel, and
  an explicit scope — the sync server, the sync client, self-update and token
  handling are in; plain HTTP without a tunnel is documented behaviour.

### Changed

- **`taskr doctor` was renamed to `taskr suggest`** for its old job — proposing
  dependency links from note refs and related titles. Every other tool means
  "diagnose my installation" by `doctor`, and the shell completions already
  described it that way. If you scripted `taskr doctor --list`, it is now
  `taskr suggest --list`.
- **A build without an injected version now reports a real one.**
  `go install github.com/Iliorn/taskr@latest` used to call itself `dev`
  forever, which also made the update check announce a new release on every
  single run. It now reports the module version, and a local build reports
  `dev+<commit>`.

### Fixed

- **A stale "plain http" sync warning no longer sticks.** Moving the sync URL
  from a public `http://` host to a Tailscale address or `https://` left the
  earlier warning on screen, still claiming the token travelled unencrypted. A
  security notice that outlives the condition it describes is worse than none.
- **Config files are replaced atomically.** settings.json, sync.json,
  sync-state.json, serve-state.json, the undo stack and task notes were written
  by truncating the old file first, so a crash or a full disk mid-write left a
  truncated file behind. A failed write now leaves the previous contents
  intact.

## [1.31.0] - 2026-08-13

### Added

- **Command palette** (`ctrl+k`): find any action by name, with the key that
  performs it and the tab it runs on. An action from another tab switches there
  first.
- **Quick-add completion**: typing `#` or `@` offers your existing tags and
  projects, most recently used first; `tab` inserts the highlighted one.
- **Tags tab is workable**: `enter` drills into a tag's tasks, where the
  row-level keys (`d`, `t`, `p`, `T`, `r`, `x`, `enter`) act on the task under
  the cursor. `a` starts a new task already carrying the tag; `f` shows the
  tag's tasks on the Tasks tab as a filter. The Projects drill gained the same
  keys, and `x` on a project row — long advertised in the help — now clears the
  project off its tasks.
- **`D` sets a due date straight from the task list** — the detail pane's
  prompt and parser, on the row under the cursor. Rescheduling used to mean
  opening the task, walking to the field and pressing enter.
- **Board columns are editable in Settings** (comma-separated). Renaming a
  column carries its cards over.
- **Searchable help**: `/` filters the shortcut overlay by key, description or
  section. The overlay also documents the quick-add and search token grammars.
- **`shift+tab`** steps back through the tabs; the digit shortcuts now work in
  the detail pane too.
- **Shell completions and a man page**: `taskr completion bash|zsh|fish` and
  `taskr man`. Task refs complete from the live store.
- **Custom keybindings**: `"keys": {"done": "D"}` in `~/.taskr/settings.json`
  rebinds any single-key action. The old key is freed, and the hints, help
  overlay and palette all show the new one.
- **linux/arm64 release binary** (`taskr-linux-arm64`) and a `SHA256SUMS` file
  on each release. `go install github.com/Iliorn/taskr@latest` is documented.

### Changed

- **Self-update no longer needs the GitHub CLI.** It reads the release API over
  plain HTTP, so the update button works on a stock install. Rate-limit and
  "no releases" failures now say what happened.
- **The Danish translation is complete.** About a third of the interface was
  still English on a Danish install — the whole sync and server half of
  Settings, the stats labels, the sort names, most of the help reference, and
  the Sequencer pane, which never went through the translation layer at all. A
  missing translation now fails the build instead of quietly rendering English.
- **Roughly twice as fast at scale.** At 2000 tasks a cache refresh went from
  11.1 ms to 5.8 ms and a search keystroke from 7.5 ms to 3.4 ms, with about a
  third of the allocations gone.

### Fixed

- **Crash on small terminals.** Width budgets went negative and the render
  panicked; every shared width helper clamps now. The Calendar and the
  drilled-in Projects view fall back to a single column instead of emitting
  lines wider than the window.
- **The cursor could point past the end of a list** — after an undo, a delete
  confirmed from a modal, or a tab switch — leaving nothing selected, so the
  next keystroke silently did nothing.
- **`go test` on Windows** wrote to the developer's real `~/.taskr`: the test
  isolation set `HOME`, which Windows does not use.
- The help overlay advertised keys that did nothing (`1`–`8` for seven tabs,
  jump/page keys on tabs with no list) and hid `--each`, `--edit` and
  `--delete` from `taskr subtask --help` and `taskr comment --help`.
- The README's fuzzy-search example (`grcry`) never actually matched.
- **An undone deletion could delete itself again on the next sync.** The
  restore was stamped from the clock, which on Windows ticks about every 15 ms
  — often the same instant as the deletion two keystrokes earlier. A tie is
  resolved by content hash, so the tombstone could win. A deletion now carries
  the moment it happened, and the restore is ordered strictly after it.
- **The editor was resolved from scratch every time it was launched**, statting
  every entry on `PATH` (times every `PATHEXT` extension on Windows) before
  opening the file.
