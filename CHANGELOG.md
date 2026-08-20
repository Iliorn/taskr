# Changelog

Notable changes to taskr. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions are the git tags the [release workflow](.github/workflows/release.yml) builds from.

Entries describe what changed for someone *using* taskr. Refactors and test work
belong in the commit log, not here — unless they change behaviour.

## [Unreleased]

## [1.34.1] - 2026-08-20

### Changed

- **A lifted task now shows the score it was lifted to.** A task that unblocks
  urgent work is ranked by that work's score — that is what puts a prerequisite
  directly above the thing waiting on it — but the Score column kept printing
  the task's own low number. The row looked misplaced rather than promoted, and
  the column contradicted the only thing ordering the list. Both halves of the
  row now read the same number, in the list and in the detail pane, where a `↑`
  marks a score its own five components no longer add up to (`w` says where the
  lift came from). The same applies to a parent lifted by an urgent subtask.

- **100% means the top of the list.** The percentage scale measured against the
  best *raw* score, so a blocker carrying the fan-out bonus could rank past the
  top of the scale — and it and the task it inherited from both printed 100%,
  hiding a difference the ranking still made. The scale now tops out at the
  highest ranked score, and the overlay's 100% is the list's 100%.

## [1.34.0] - 2026-08-19

### Added

- **`taskr update`** installs the latest release from the shell, so updating is
  no longer the one thing that requires opening the TUI and finding the
  Settings tab. `--check` only reports ("update available: v1.34.0 (running
  v1.33.1)") and installs nothing, which makes it safe in a shell profile or a
  cron; `-y` skips the confirmation, and a run with no terminal on stdin says
  so rather than reading EOF as "no". The download is checked against the
  release's `SHA256SUMS` exactly as the Settings button is — it is the same
  code underneath, and the verdict about *your* install (up to date, a local
  build, package-managed, or updatable) now comes from one shared decision, so
  the two surfaces cannot tell you different things.

- **Crash reports.** A panic used to end as a raw Go stack trace over a restored
  terminal, with the last few seconds of edits gone and nothing to send anyone.
  taskr now writes `crash-<timestamp>.log` to its state directory — the stack
  from where it actually broke, plus the version, platform, terminal size and
  what was on screen — flushes whatever the save debounce still owed, and
  finishes with the path and where to report it. The newest five are kept.

### Changed

- **A store written by a newer taskr is no longer opened.** Migrating forward is
  one-way, and an older binary pointed at an already-migrated store used to open
  it and read the columns it knew — silently dropping whatever the newer schema
  added on the next save. Since self-update and sync make mixed versions a
  normal part of upgrading two machines, taskr now refuses, names both schema
  versions, and points at the pre-migration `.bak`.

- **Subtasks fold with `+`/`-` instead of a triangle.** A row with hidden
  subtasks carried a `▸`, one cell after the `▶` that marks the row you are on
  — two triangles pointing the same way on the same row, only one of which is
  the cursor. The fold marker now uses the tree convention (`+` closed, `-`
  open), which says the same thing without borrowing the cursor's shape, and
  the help's row-symbol list documents it.

- **The Stage row looks like the picker it is.** It carried a dim `‹←/→›` hint
  after the value — the right information in a place the eye reads as part of
  the field, and the only row in the detail pane with an instruction glued to
  its contents. The stage now sits inside `‹ … ›` brackets, exactly like the
  values you cycle on the Settings tab, so the shape says "arrow keys change
  this" without spending words on it.

- **Package-managed installs are no longer overwritten.** Self-update knew
  about Homebrew and nothing else, so an update run against a binary installed
  by Scoop or a distribution package replaced a file that package manager owns
  — reverted by its next upgrade, and failing its integrity check until then.
  Those installs are now told which command to use instead. Homebrew, Scoop and
  `/usr/bin` are recognised; `/usr/local/bin` and `~/.local/bin` are yours and
  stay self-updatable.

- **`taskr completion`, `man` and `update` no longer open the database.** Every
  subcommand but `help` and `--version` opened the store first, and opening a
  store older than the binary migrates its schema — a one-way change for anyone
  still running an older taskr against it. Printing a completion script or
  asking GitHub for a version number should not be what upgrades a database.

## [1.33.1] - 2026-08-19

### Changed

- **The calendar's day summary moved into the pane's border.** The
  `3 entries · 1h 20m` line sat inside the agenda, right-aligned above a blank
  spacer, so two of the day's rows went to a fact the border had room for: on a
  short window that was the difference between seeing the next entry and
  scrolling for it. It now reads `Sun 16 Aug 2026 [3 entries · 1h 20m]` in the
  border title, and the agenda pages against the two rows it got back. A window
  too narrow for both drops the bracket whole rather than clipping the date.

- **Every column on the Overview now costs what it shows.** Each one is sized
  as the wider of its header and its widest value plus a two-space gap —
  nothing else. The spacing used to be assembled per column, a two-space lead
  here and a one-space trail there, chosen to sum to five for the *widest*
  value in each column and therefore ragged for every shorter one: a `2d` in a
  Due column sized for `20-09-27` left nine spaces before Size where the full
  date left three, and `100%` left four before Due where `73%` left five, with
  the percent sign a cell adrift on every two-digit score. Score and Due are
  now right-aligned fields, so the numbers share an edge and the dates line up
  under each other. The columns take about four cells less than before, which
  goes where it is worth something: longer titles and tags that fit on a
  narrower window.

### Fixed

- **The Stage row no longer breaks the detail panel.** Its value carries a dim
  `‹←/→›` hint, and the pane truncated that already-styled string by counting
  runes — which counts the invisible escape codes too, so the cut landed in the
  middle of one. The terminal then read the `(…)` marker as more of the escape
  and printed what was left of it (a stray `)` beside the stage name), and with
  no reset the style ran on into the panel border, which drew the frame wrong.
  Styled values are measured and cut as styled text now, and the key hint —
  decoration, not data — is dropped whole when the pane is too narrow for both
  rather than being clipped to a marker.

- **The selected row is highlighted all the way across.** The highlight covered
  only what the row had drawn, so on the Overview it stopped wherever that
  task's last column ended — at the Tags column on a task with no tags, a few
  characters in on a subtask — and the bar looked like a block sitting in the
  middle of the list rather than the line you are on. It now reaches the
  panel's right edge on every row: task, subtask and completed alike.

## [1.33.0] - 2026-08-16

### Added

- **Backlog-review flags on `list`.** `--stale=30d` keeps only what nothing has
  touched in that long, `--sort=seq|due|size|age|idle|pri` orders by any of
  them, and `--wide` adds AGE and IDLE columns. Clearing a backlog used to mean
  exporting to JSON and doing the date arithmetic somewhere else, because the
  CLI could filter on everything except time.
- **`list --unblocked-since=14d`** — tasks that became actionable recently:
  every dependency done, the last one within the window. A task freed weeks ago
  by a blocker you closed looks exactly like one that never had a blocker, so
  nothing surfaced it.
- **`taskr reopen <ref>...`**, the counterpart to `done`. Closing was a
  one-way door from the CLI: an accidental `taskr done` could only be undone by
  opening the TUI. Not a toggle — a verb that closes a pending task when you
  meant to reopen a done one is the wrong thing to hand a script.
- **`taskr edit` takes several refs**, like `done` already did, so one change
  across a cluster is one command. `--title` still takes exactly one: a title
  is identity, not a shared property.
- **Whole-word and regexp search.** `list --search-word` / `search --word`
  match on word boundaries, and `--search-re` / `search --re` take a regular
  expression. Plain substring search answers "RAM" with "Ramte", which is right
  for recall and wrong for "is anything about RAM still open".

### Changed

- **One cursor, everywhere.** The marker for "the row you are on" was three
  different glyphs: `▶` in the task, tag and project lists and all through the
  detail pane, `>` on the board's cards, and `→` on the Settings rows, in the
  command palette and in the tag/project/dependency pickers. Three shapes for
  one idea reads as three kinds of selection. Everything draws `▶` now — which
  also settles a collision, since `→` is the medium-priority icon and could sit
  in the same row as a `→` that meant something else entirely.
- **The header hint is one `?`.** It read
  `? shortcuts · ctrl+k commands` — two doors advertised where one will do, in
  twenty-eight columns taken from the tab bar, to say what a single universal
  character says. `?` is also the door that leads to the other: the help
  overlay lists `ctrl+k` among its shortcuts. The palette did not return the
  favour, so it now has an entry for the help too, and the two point at each
  other whichever one you find first.
- **The tab bar stays inside its width.** Every tab renders with a cell of
  padding either side, which the bar's own measurement left out — so it could
  pick a set of labels that measured within budget and drew fourteen columns
  past it. The header paid for the overrun by truncating what sat to its right,
  which is why the hint used to read `? shortcuts · ctr` on a narrow window.
  Counted properly, the tab labels also stay readable further down: at 80
  columns the bar shows `1 Tasks 2 Cal 3 Pro …` where it used to be bare
  digits. When the window really is too narrow for both, the hint now goes
  whole rather than leaving a fragment behind.

- **The pre-migration backup says what it is for.** Upgrading the schema is
  one-way: once a newer build has opened the store, an older taskr cannot —
  migration 011 dropped a table, so a 1.25 binary fails on every command with
  "no such table". The line announcing the backup named a file and nothing
  else, which is a diagnosis away from being useful when it is a dev build or a
  second machine mid-upgrade that did the upgrading. It now names the schema
  step, says older builds can no longer open the store, and spells out the
  rollback: stop whatever holds the database, copy the backup over it, delete
  the `-wal`/`-shm` sidecars.

### Fixed

- **Equal-scoring tasks now sort in a stable order.** Every sequence sort scored
  each task against its own `time.Now()`, and Age accrues continuously, so two
  identical tasks differed by ~1e-11 — enough for the comparator to separate
  them on the float and never reach the ID tie-break. The order of equal tasks
  was left to `sort.Slice` and could change between two runs over the same data.
  One clock per sort now, so the documented "ties break by ID" is true.
- **`taskr help` no longer claims only auto-sync pauses** past the
  deletion-memory window. A manual `taskr sync` refuses too, and says so.

## [1.32.0] - 2026-08-16

### Added

- **The board scrolls sideways.** Splitting the width across every stage is
  what a three-stage board wants and what a ten-stage one cannot survive —
  eleven columns need a 204-column terminal before each is even readable. The
  columns that fit are now shown at a readable width and the rest scrolled to:
  `←/→` move the focus and the view follows a column at a time, with the panel
  title naming the visible slice. Below three columns it still falls back to
  the stacked list, which shows every stage at once and is the better answer on
  a genuinely narrow window.
- **A Stage row in the detail pane.** `←/→` move a task between board columns
  without leaving the task, the way the arrows change a value in Settings. It
  shows only where it means something — a pending top-level task — since a
  subtask never reaches the board and Done is a status rather than a stage. It
  cannot reach Done either: completing a task has one path, and it carries
  timer, subtask and recurrence semantics this row has no business duplicating.
- **Settings → "Kanban board" turns the whole surface off**: the Board tab and
  the Stage row together. The tab leaves the bar, `tab`/`shift+tab` step over
  it, its digit stops working and the palette drops its commands. The digits do
  not renumber — Stats stays `6` either way — so the numbering shows a gap
  rather than moving under your fingers every time you toggle.
- **The search field completes tags and projects**, the same way quick-add
  does: type `#` or `@` and the existing names appear, most recently used
  first, `tab` splices one in. Search was the other place you had to remember
  your own vocabulary — and the one where a near-miss is silent, since a
  mistyped `#hjme` just drops into the free-text match and takes the results
  with it.

### Changed

- **A healthy sync no longer marks the corner of the screen.** The status line
  carried a dim `✓` whenever background sync was working — a symbol with
  nowhere to look it up, spending the best corner of the screen on "nothing is
  wrong". Settings already answers that in words, next to the rows that
  configure sync and updated by the sync that runs at launch. The corner now
  speaks only when it has something you must act on: `✕ sync` still appears
  when the device is drifting away from the others, and the help overlay has a
  new **Status line** section explaining it, the focus chip and the search chip
  — the three things that can appear there.

- **The footer's boxed fields line up with the pane above them.** The search
  field, quick-add, the pickers and the command palette were all drawn two
  columns to the left of the box they appear under, so opening any of them put
  a visible step in the left edge of the screen. They now carry the same margin
  the panes do, which also lands their text in the column the key hints were
  already written for. The palette's rows and the confirm prompts moved onto
  that column with them.

- **The Activity chart is taller.** Its caption — the range, the completions in
  it, and what one block means — moved onto the panel's border, in brackets
  beside "Activity", the way the task list already carries its position and
  sort there. That gives the chart the row back, and the ceiling on its height
  went from 9 blocks to 12, so a day of ten closed tasks now stacks ten whole
  blocks instead of collapsing to half-height ones. The chart also stops
  drawing empty rows above its tallest bar and hands them to the stats list
  instead. On a narrow window the caption sheds its least useful half first,
  rather than being cut mid-sentence.

- **The detail pane scrolls gradually instead of jumping.** Its top was
  derived from the field cursor, which pinned the cursor to the third row and
  slid the whole document underneath it: every press moved the text by the
  distance between two fields, and a step from Notes to Tags threw the pane
  seven lines. The scroll position is now kept between keystrokes and moves
  only when the cursor would come within two lines of an edge, by as little as
  that takes — so walking down the fields moves nothing at all until the
  cursor reaches the bottom of the pane, and then it follows a line at a time.
  Two sections the old arithmetic never counted (the Stage row and the whole
  time-entries block) put its idea of the cursor four lines above the real one,
  which the old always-snap-to-the-bottom behaviour had been hiding.

- **The Done column is the last one in your list, and you can rename it.** It
  used to be drawn on the end and left out of the column list, so it was the
  one column on the board you could not name — a board otherwise entirely
  yours ended in a word chosen for you. It is now the final entry of Settings →
  "Board columns": call it *Shipped*, *Archive* or *Færdige*, and the completed
  cards sit under that heading. What it *does* has not moved an inch — the last
  column is still `Status == Done` rather than a stored stage, so moving a card
  into it completes the task through the same path `d` uses, and neither the
  detail pane's Stage row nor `taskr edit --stage` can reach it. Existing
  configurations gain the column they were already being shown, under the name
  they were already seeing it, so nothing on screen moves on upgrade.

- **The search parse preview appears wherever the grammar runs.** It was
  limited to the Tasks tab on the belief that no other tab compiled the query.
  The Board and Stats both do — the Board's columns are a projection of the
  same filtered lists — so on those two you were typing tokens with no
  indication of which ones took. Projects still shows neither preview nor
  completions: its search matches project names, where a token would describe
  a filter that is not running.
- **Release notes come from the CHANGELOG.** A release page listed commit
  subjects, which describe the work to whoever wrote it rather than to whoever
  is deciding whether to upgrade — while the entries below, written for exactly
  that reader, stayed in the repository where nobody looking at a release would
  find them. The release body is now this tag's section. A tag with no section
  falls back to generated notes rather than publishing an empty one.

## [1.31.0] - 2026-08-15

### Added

- **taskr mints sync tokens, and says when yours is guessable.**
  `taskr serve --new-token` generates one from the system CSPRNG, stores it and
  prints it; `ctrl+g` on the Settings server-token row does the same. Every
  other protection around the token — constant-time comparison, `0600` on
  `sync.json`, never logged — was downstream of a secret you invented at a
  prompt, which made a short one the likeliest realistic compromise of a sync
  setup. `taskr doctor`, Settings and `taskr serve` now warn when the
  configured token is short or looks like a word. The Settings **server**-token
  row goes further and refuses one outright, since that token is taskr's own
  choice and `ctrl+g` is one keystroke away; the client token, `taskr serve` and
  `TASKR_SYNC_TOKEN` only warn, because the first has to match whatever the
  other end already uses and the last two may come from a secret manager or a
  unit file written a year ago. taskr refuses only where it can offer the
  alternative in the same breath.
- **The updater only follows GitHub.** The download URL arrives inside the
  release API's JSON, so it is data rather than a constant; it is now confined
  to `github.com` and `githubusercontent.com` over TLS, checked on the initial
  request and on every redirect — a pre-flight check alone would have been
  walked past by a 302.
- **Release builds are reproducible** (`-trimpath`, `CGO_ENABLED=0`, the Go
  version pinned in `go.mod`), so a tag can be rebuilt and compared against
  `SHA256SUMS` by anyone. `SECURITY.md` now states plainly what that checksum
  proves — that your download matches what the workflow published — and what it
  does not: that the publisher was honest. Signing that binds a release to the
  workflow identity would close that gap and is not implemented yet.
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

- **Files follow platform conventions instead of one dot-directory.** A new
  install puts config in `$XDG_CONFIG_HOME/taskr`, the database in
  `$XDG_DATA_HOME/taskr`, undo/sync state and logs in `$XDG_STATE_HOME/taskr`
  and the editor scratch file in `$XDG_CACHE_HOME/taskr` — `%APPDATA%` /
  `%LOCALAPPDATA%` on Windows, `~/Library/Application Support` on macOS, and an
  explicitly exported `XDG_*` variable wins everywhere. **An existing
  `~/.taskr` keeps being used exactly as before**: nothing moves, nothing is
  migrated. `TASKR_HOME` puts everything back in one directory, and
  `taskr doctor` prints what it resolved.

- **Search matches notes**, not just the title. Titles stay fuzzy; notes are
  matched as a plain substring, since a subsequence match over a whole note
  would hit almost anything.

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

### Removed

- **Learnings.** A second free-text list beside notes and comments, with its own
  table, its own sync fold and its own CLI command, but never a tab of its own.
  Migration 011 appends every learning to its task's notes under a
  `## Learnings` heading and drops the table, so nothing you wrote is lost —
  and `taskr search` now matches notes, which is where the recall
  `taskr learnings` used to provide now comes from. `taskr learnings` prints
  where the text went instead of silently opening the TUI.

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

- **A stale "plain http" sync warning no longer sticks.** Moving the sync URL
  from a public `http://` host to a Tailscale address or `https://` left the
  earlier warning on screen, still claiming the token travelled unencrypted. A
  security notice that outlives the condition it describes is worse than none.
- **Config files are replaced atomically.** settings.json, sync.json,
  sync-state.json, serve-state.json, the undo stack and task notes were written
  by truncating the old file first, so a crash or a full disk mid-write left a
  truncated file behind. A failed write now leaves the previous contents
  intact.

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

### Security

- **Self-update now verifies what it downloads.** Every release publishes a
  `SHA256SUMS` file; until now the update button ignored it and installed the
  binary unchecked. The download is hashed as it is written and compared
  against the published checksum, and anything that cannot be verified — no
  `SHA256SUMS`, no entry for this platform, a mismatch — installs nothing and
  says why. This is an integrity check, not a signature.
