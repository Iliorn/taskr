# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`taskr` is a keyboard-driven terminal task manager built with Go and Bubble Tea (Charm). It is a standalone app with its own JSON storage — **not** a Taskwarrior frontend. Beyond tasks it provides a calendar/time-tracking view, projects (Gantt), tags, per-task "learnings", a stats dashboard, and in-app self-update.

## Commands

```bash
go build -o taskr .                                   # build (version = "dev")
go build -ldflags "-X main.appVersion=v1.8.0" -o taskr .   # build with a real version
go run .                                              # build & run
go test ./...                                         # run all tests (root pkg + todo pkg)
go test -run TestName ./...                           # run a single test
go vet ./...                                          # vet
```

There is no linter config or CI; `go test` and `go vet` are the full check suite. Tests live alongside code (`*_test.go`) and cover storage, helpers, layout, tags, stats — not the Bubble Tea event loop.

### Releasing

Self-update depends on **exact release asset names** — they are load-bearing, do not rename. Cross-compile four targets, each with the `-ldflags` version string, and attach to a `gh release`:

- `taskr` (Linux x64) · `taskr.exe` (Windows x64) · `taskr-macos-apple-silicon` (arm64) · `taskr-macos-intel` (amd64)

Settings tab → "Update to latest release" shells out to the **GitHub CLI** (`gh`) to fetch the matching asset, so `gh` must be installed at runtime for self-update.

## Architecture

Standard Bubble Tea MVU (`Model`/`Init`/`Update`/`View`), but the single-file convention is gone — the app is split by *concern*, with one big `model` struct threaded through everything:

- **`model.go`** — the `model` struct (large, flat), all the enums (`tab`, `appMode`, `pane`, sort modes), message types, `initialModel`, undo stack, and most pure model-mutation/lookup helpers.
- **`update.go`** — top-level `Update`, the normal-mode list/detail key handling, tab switching, editor launching, self-update plumbing.
- **`update_modes.go`** — `Update` handlers for the text-entry / search modes (`updateInput`, `updateSearch`, `updateEditTitle`, etc.). When adding a modal interaction, the handler usually lives here.
- **`view.go`** — top-level `View` + the Tasks tab and shared rendering helpers; dispatches to `view_lists.go` (projects/tags/learnings/stats), `view_calendar.go`, `view_detail.go`.
- **`cache.go`** — `cacheState` (see below).
- **`storage.go`** — JSON load/save, settings, atomic write + `.bak` backup, task sorting.
- **`helpers.go`** — parsing (quick-add syntax, dates, time-entry edits), formatting, column layout, editor resolution, self-update file ops.
- **`layout.go` / `styles.go` / `constants.go`** — width/height math, theming, magic numbers.
- **`todo/`** — the **domain package**, framework-free. `todo.Todo` and its methods (`Toggle`, `AddTag`, `StartTimer`, `IsOverdue`, subtask/learning/comment/time-entry mutations). No Bubble Tea or rendering here; keep it that way.

### Two patterns that matter most

**1. The derived-view cache (`cacheState`).** `m.todos` is the single source of truth; everything the UI shows (active vs. done lists, sorted tags + counts, projects, learnings, a `todoIndex` ID→slice-index map, overdue set, subtask index) is *derived* and cached on the model. After **any** mutation to `m.todos`, call the right invalidator or the UI goes stale:

- `m.markModified()` — mutate + push undo + mark dirty + refresh (the usual path).
- `m.markModifiedNoUndo()` — same without an undo snapshot.
- `m.markCacheDirty()` — caches only, no undo, no `dirty` flag.

`refreshCaches()` rebuilds derived data; it also calls `followTask` so the cursor stays on the same task ID across re-sorts. Tasks are addressed by **string ID**, not slice position — use `findTodoByID` / `currentTodoIndex`, since sorting/filtering constantly reorders the slice.

**2. Global theme state.** lipgloss styles are **package-level vars** reassigned by `applyTheme(theme)` (called at startup and on theme switch). Rendering code reads these globals directly; it does not receive a style set. Switching theme = call `applyTheme` with a different palette from `themes`. `init()` in `styles.go` applies `themes[0]` so styles are never nil in tests.

### Other conventions

- **Persistence is debounced** — mutations set `dirty`/`savePending` and a `saveTickMsg` (300ms) flushes via `prepareSave`. Saves are async `tea.Cmd`s; don't write `tasks.json` synchronously from `Update`.
- **Modes drive input.** `m.mode` (an `appMode`) decides which `update*`/`render*` path runs. Adding a feature with text entry or a confirm prompt means: add an `appMode` const, a handler (usually `update_modes.go`), and a render branch.
- **Subtasks, dependencies, learnings** are all stored inside `m.todos` (subtasks are full `Todo`s with a `ParentID`, linked by `SubtaskIDs`), so global operations loop the whole slice — see `renameTagGlobally`, `deleteLearningByID`.
- Data lives at `~/.taskr/tasks.json` (+ `.bak`), settings at `~/.taskr/settings.json`. Built binaries and `*.bak` are gitignored.

## House rules (from global CLAUDE.md)

- Match the existing style of the file you edit; **no blanket reformatting**, and keep any formatting-only change in its own commit.
- TokyoNight-style palette is the visual baseline.
- Share the approach and get buy-in before large multi-file or expensive changes rather than spiraling.
- After meaningful changes, remember this repo is public under GitHub user `iliorn`.
