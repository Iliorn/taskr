# Changelog

Notable changes to taskr. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions are the git tags the [release workflow](.github/workflows/release.yml) builds from.

Entries describe what changed for someone *using* taskr. Refactors and test work
belong in the commit log, not here — unless they change behaviour.

## [Unreleased]

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
