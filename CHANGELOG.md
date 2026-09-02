# Changelog

Notable changes to taskr. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions are the git tags the [release workflow](.github/workflows/release.yml) builds from.

Entries describe what changed for someone *using* taskr. Refactors and test work
belong in the commit log, not here — unless they change behaviour.

**One line per entry.** No explanation, no rationale, no second sentence — the
reader is deciding whether to upgrade, not reviewing the change. The reasoning
belongs in the commit message, where it is kept next to the code it explains.
`TestChangelogEntriesAreOneLine` enforces it.

## [Unreleased]

### Changed

- Windows: the console is switched to UTF-8, so æøå and the box borders stop arriving as mojibake.
- The overdue badge and the focus chip drop the symbols terminals draw two cells wide.
- The detail pane can sit right, left, or at the bottom (Settings → Detail pane).
- The `/` search filter survives a restart, and esc still clears it.
- An undated task marks the timeline where it happened: a diamond if high priority, else a dot.
- A drilled-in project draws bars beside the task list instead of repeating every title.
- The board draws an even column grid with dividers, and headings now sit over their own cards.
- Settings drops the sequencer's personality tagline; the top-5 preview stands alone.
- Settings groups Preferences into sections led by Theme and Language, and marks editable rows.
- The Settings server row reads Off when this machine is not a sync hub, instead of needs token.
- Listen and Server token are hidden while the server is off; switching it on asks for the token.

## [1.35.0] - 2026-08-23

### Added

- The list pane fills spare rows with a dim `Closed today (3)` read-out.
- The Tasks tab carries an overdue count in the tab bar (`1 Tasks ⚠3`).
- Release binaries carry Sigstore build provenance (`gh attestation verify`).
- `--json` output is a documented contract, pinned by golden files in CI.

### Changed

- Sync names which end runs an older taskr, on failure and on success.
- The task list dims its secondary columns so the title carries the row's status.
- Title badges (`!`, `↻`, `↥`, `↧`, `(1/2)`) survive clipping; only the title text is cut.
- The tag cell drops chips it cannot fit and counts them: `⟨#bug⟩ +2`.
- Titles claim width before the tag cell reserves it.
- Clipped text ends in `…` instead of `(…)`, panel rows included.
- The detail pane leads with the score, and hides `Modified` when it matches `Created`.
- Detail-pane scroll markers read `↑ 3 more` / `↓ 12 more`.
- Board columns are sized by what they hold, with a floor per stage.
- Progress bars use a thin rule for the unfilled part.
- The footer names what the key would do (no `t track` on a running timer).
- Narrow tab labels are chosen rather than chopped to three letters.
- The activity chart no longer reserves empty rows above a single block.
- A short task list no longer runs its column headers together.

### Fixed

- A failing sync shows the server's whole message instead of cutting it at 60 columns.
- `go install github.com/Iliorn/taskr@latest` works; the module path matches the repo.

## [1.34.1] - 2026-08-20

### Changed

- A lifted task shows the score it was lifted to, marked `↑`.
- 100% on the score scale means the top of the list, not the best raw score.

## [1.34.0] - 2026-08-19

### Added

- `taskr update` installs the latest release from the shell; `--check` only reports.
- Crash reports: a panic writes `crash-<timestamp>.log` and flushes pending saves.

### Changed

- A store written by a newer taskr is refused rather than silently downgraded.
- Subtasks fold with `+`/`-` instead of a second triangle beside the cursor.
- The Stage row sits in `‹ … ›` brackets like the Settings values you cycle.
- Package-managed installs are told to update through their own manager.
- `taskr completion`, `man` and `update` no longer open — or migrate — the database.

## [1.33.1] - 2026-08-19

### Changed

- The calendar's day summary moved into the pane border, giving the agenda two rows back.
- Overview columns cost what they show; Score and Due are right-aligned.

### Fixed

- The Stage row no longer breaks the detail panel by cutting inside an escape sequence.
- The selected row is highlighted to the panel's right edge.

## [1.33.0] - 2026-08-16

### Added

- Backlog-review flags on `list`: `--stale=30d`, `--sort=seq|due|size|age|idle|pri`, `--wide`.
- `list --unblocked-since=14d` — tasks whose last dependency closed inside the window.
- `taskr reopen <ref>...`, the counterpart to `done`.
- `taskr edit` takes several refs; `--title` still takes one.
- Whole-word and regexp search: `--search-word` / `--word`, `--search-re` / `--re`.

### Changed

- One cursor glyph everywhere: `▶` in every list, the board and Settings.
- The header hint is a single `?`; the palette now lists the help too.
- The tab bar counts its own padding and stays inside its width.
- The pre-migration backup line names the schema step and spells out the rollback.

### Fixed

- Equal-scoring tasks sort in a stable order — one clock per sort, so ties break by ID.
- `taskr help` no longer claims only auto-sync pauses past the deletion-memory window.

## [1.32.0] - 2026-08-16

### Added

- The board scrolls sideways: `←/→` move the focus and the view follows a column at a time.
- A Stage row in the detail pane moves a task between board columns with `←/→`.
- Settings → "Kanban board" turns off the Board tab and the Stage row together.
- The search field completes `#tag` and `@project` the way quick-add does.

### Changed

- A healthy sync no longer marks the screen; only `✕ sync` appears, and the help explains it.
- The footer's boxed fields line up with the pane above them.
- The Activity chart is taller, with its caption moved into the panel border.
- The detail pane scrolls gradually instead of jumping a section per keypress.
- The Done column is the last entry of your column list, and renameable.
- The search parse preview appears on every tab whose search runs the grammar.
- Release notes come from the CHANGELOG section for the tag.

## [1.31.0] - 2026-08-15

### Added

- `taskr serve --new-token` mints a sync token; short or word-like tokens are flagged.
- The updater follows only `github.com` and `githubusercontent.com` over TLS.
- Release builds are reproducible (`-trimpath`, `CGO_ENABLED=0`, pinned Go version).
- `j`/`k` move the cursor everywhere `↑`/`↓` do.
- The score reads as a percentage of the field; 100% is the top pending task.
- `/` filters the Board with the same tokens as everywhere else.
- `w` explains a task's rank, and `taskr why <ref>` prints the same answer.
- The quick-add and search grammars accept the interface language's words too.
- `TASKR_NO_WATCH=1` turns off live reload.
- `TASKR_TRACE=1` writes a per-frame latency log to `~/.taskr/trace.log`.
- `taskr doctor` diagnoses the installation; `--json` makes it machine-readable.
- A version on the sync wire: both ends refuse a payload they might misread.
- Fuzz tests for the quick-add, search, due-date and time-entry parsers.
- Arch (`yay -S taskr-bin`) and Windows (`scoop install …`) packages.
- German UI (`"language": "de"`), with the no-wrap guard sweeping every language.
- A security policy (`SECURITY.md`) with a private reporting channel and a stated scope.
- Command palette (`ctrl+k`): find any action by name and the key that performs it.
- Quick-add completion: `#` and `@` offer existing tags and projects, most recent first.
- The Tags tab drills into a tag's tasks and takes the row-level keys.
- `D` sets a due date straight from the task list.
- Board columns are editable in Settings; renaming one carries its cards over.
- Searchable help: `/` filters the overlay, which documents the token grammars.
- `shift+tab` steps back through the tabs; digit shortcuts work in the detail pane.
- Shell completions and a man page: `taskr completion bash|zsh|fish` and `taskr man`.
- Custom keybindings: `"keys": {"done": "D"}` rebinds any single-key action.
- A linux/arm64 binary and a `SHA256SUMS` file on each release.

### Changed

- Files follow XDG conventions; an existing `~/.taskr` keeps being used unchanged.
- Search matches notes as well as titles.
- `taskr doctor` was renamed to `taskr suggest` for its old job.
- A build without an injected version reports the module version, not `dev`.
- Self-update no longer needs the GitHub CLI.
- The Danish translation is complete, and a missing one now fails the build.
- Roughly twice as fast at 2000 tasks: 5.8 ms per refresh, 3.4 ms per search keystroke.

### Removed

- Learnings. Migration 011 appends each one to its task's notes and drops the table.

### Fixed

- Windows input latency: `CONIN$` is opened for a blocking read instead of a 16 ms poll.
- The first frame is rendered before the program starts.
- The renderer paints at 120 FPS instead of 60.
- A stutter on the first keystroke: a reload carrying no new task versions is skipped.
- A stale "plain http" sync warning no longer sticks after the URL changes.
- Config files are replaced atomically.
- Crash on small terminals: every shared width helper clamps a negative budget.
- The cursor could point past the end of a list, leaving nothing selected.
- `go test` on Windows wrote to the developer's real `~/.taskr`.
- The help overlay advertised keys that did nothing, and hid three CLI flags.
- The README's fuzzy-search example (`grcry`) never matched.
- An undone deletion could delete itself again on the next sync.
- The editor was resolved from scratch on every launch.

### Security

- Self-update verifies its download against the release's `SHA256SUMS`, and fails closed.
