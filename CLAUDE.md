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

The version lives **only in git tags + the release** — there is no version constant in the tree (`appVersion` defaults to `"dev"` and is injected at build time). So the next version = bump the latest tag from `gh release list`; don't trust local `git tag` (release tags may exist on the remote but not locally). The full publish flow:

```bash
V=v1.10.0
GOOS=linux   GOARCH=amd64 go build -ldflags "-X main.appVersion=$V" -o taskr .
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.appVersion=$V" -o taskr.exe .
GOOS=darwin  GOARCH=arm64 go build -ldflags "-X main.appVersion=$V" -o taskr-macos-apple-silicon .
GOOS=darwin  GOARCH=amd64 go build -ldflags "-X main.appVersion=$V" -o taskr-macos-intel .
gh release create $V --title "..." --notes "..." \
  taskr taskr.exe taskr-macos-apple-silicon taskr-macos-intel
```

Patch bumps are the norm for stat/layout tweaks; minor bumps for new interactive features. `gh release create` also creates the tag, so a plain `git push origin main` beforehand is enough.

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

**3. Localization (`lang.go`).** UI strings are translated gettext-style: the English literal is the lookup key, so call sites read `tr("Settings")` and any untranslated string falls back to its English source. `activeLang` is a package-level global (like the theme), set by `applyLang(code)` at startup and on language switch (`cycleLang`); `initialModel` applies the stored language, so tests must `applyLang` **after** building the model. Adding a language = one entry in `translations` plus its date-name tables (`monthNames`, `weekdayNames`, etc. — Go's `time` has no locale support, so name-bearing date layouts go through `localized*` helpers). Only display strings are translated; stored data and quick-add/date **parsing** keywords stay English. Priority words are localized at the view layer via `trPriority` to keep the `todo` package locale-free. `TestNarrowNoWrapDanish` guards the no-wrap contract against longer Danish strings by comparing each tab/width to the English baseline.

### Other conventions

- **Persistence is debounced** — mutations set `dirty`/`savePending` and a `saveTickMsg` (300ms) flushes via `prepareSave`. Saves are async `tea.Cmd`s; don't write `tasks.json` synchronously from `Update`.
- **Modes drive input.** `m.mode` (an `appMode`) decides which `update*`/`render*` path runs. Adding a feature with text entry or a confirm prompt means: add an `appMode` const, a handler (usually `update_modes.go`), and a render branch.
- **Subtasks, dependencies, learnings** are all stored inside `m.todos` (subtasks are full `Todo`s with a `ParentID`, linked by `SubtaskIDs`), so global operations loop the whole slice — see `renameTagGlobally`, `deleteLearningByID`.
- Data lives at `~/.taskr/tasks.json` (+ `.bak`), settings at `~/.taskr/settings.json`. Built binaries and `*.bak` are gitignored.

### Rendering conventions

- **ANSI-aware width math.** Once a string has been through a lipgloss `.Render`, `len([]rune(s))` over-counts by the escape sequences and silently breaks alignment/centering. Use `ansi.StringWidth` to measure and `ansi.Truncate` to clip **styled** strings; `len([]rune(...))` is only correct for plain text. Width tests assert no line exceeds the pane's inner width (`termWidth-8`) — that's the no-wrap contract.
- **Shared list-column rule.** The leading "name" column on the Tasks / Projects / Tags / Learnings tabs is sized by `contentFitWidth` (hug the widest entry + gap, floored to the header label, capped by the responsive `nameColWidth`) in `layout.go`. Reuse it for any new list tab instead of inventing per-tab width constants, so all tabs reflow identically on resize.
- **Group same-style runs.** When emitting a row of per-cell-styled glyphs (tag progress bars, the stats histogram via `statsCell`/`renderCellRow`), coalesce consecutive cells that share a style into one `.Render` call — far fewer escape sequences and it keeps `ansi.StringWidth` honest.

## House rules (from global CLAUDE.md)

- Match the existing style of the file you edit; **no blanket reformatting**, and keep any formatting-only change in its own commit.
- TokyoNight-style palette is the visual baseline.
- Share the approach and get buy-in before large multi-file or expensive changes rather than spiraling.
- After meaningful changes, remember this repo is public under GitHub user `Iliorn` (capital I — `git remote` is `https://github.com/Iliorn/taskr.git`).
