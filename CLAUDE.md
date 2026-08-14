# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`taskr` is a keyboard-driven terminal task manager built with Go and Bubble Tea (Charm). It is a standalone app with its own SQLite storage (legacy JSON is imported on first run) — **not** a Taskwarrior frontend. Beyond tasks it provides a calendar/time-tracking view, projects (Gantt), tags, a kanban board, per-task "learnings" (managed in the task detail; recalled via `taskr learnings`), a stats dashboard, and in-app self-update.

## Commands

```bash
go build -o taskr .                                   # build (version = "dev")
go build -ldflags "-X main.appVersion=v1.8.0" -o taskr .   # build with a real version
go run .                                              # build & run
go test ./...                                         # run all tests (root pkg + todo pkg)
go test -run TestName ./...                           # run a single test
go vet ./...                                          # vet
golangci-lint run ./...                               # lint (config in .golangci.yml)
```

CI (`.github/workflows/ci.yml`) runs `go vet` + `go test` + `go build` on the
platforms taskr ships — ubuntu, windows and macos runners — plus a `cross` job
that cross-compiles the release targets (linux/amd64, windows/amd64,
darwin/arm64+amd64) and a separate `golangci-lint` job. The matrix is the point:
Linux-only CI meant the Windows binary was first compiled by the release job,
after the tag existed, and its platform-specific paths were never run. Note that
`os.UserHomeDir` reads `%USERPROFILE%` on Windows and `$HOME` elsewhere, so
`TestMain` sets both — `TestStorageStaysInsideTheTestHome` fails loudly if the
redirect ever stops covering a platform. The check suite is `go test`, `go vet`, and
golangci-lint (standard linters; `.golangci.yml` excludes the conventional
ignored errors and the opinionated QF* style nits). Tests live alongside code
(`*_test.go`) and cover storage, helpers, layout, tags, stats, and the
client↔server sync round trip. The Bubble Tea event loop is covered by two
dedicated suites: `update_keyscript_test.go` drives real `Update` dispatch
with scripted key sequences and asserts store/undo/save bookkeeping, and
`undo_property_test.go` runs randomized op+undo pairs (fixed seeds) checking
content digests round-trip. When adding a modal interaction, add a script
flow for it.

### Releasing

Linux/Windows self-update depends on **exact release asset names** — they are load-bearing, do not rename (an installed binary looks for the name it was built with, so a name can be *added* but never changed). Cross-compile the targets, each with the `-ldflags` version string, and attach them to a `gh release` along with `SHA256SUMS`:

- `taskr` (Linux x64) · `taskr-linux-arm64` (Linux arm64) · `taskr.exe` (Windows x64)

`selfUpdateAsset(goos, goarch)` is the single map from platform to asset name; adding a build target means adding a case there too, or that platform's update button downloads a binary it cannot run.

macOS is distributed from tagged source through the separate `Iliorn/homebrew-tap` repository (`brew install iliorn/tap/taskr`). Do not add macOS binaries or `.app` bundles to GitHub releases. The tap checks for new releases hourly, updates the formula URL/checksum, and validates source installation on macOS CI.

On Linux and Windows, Settings tab → "Update to latest release" reads `/repos/iliorn/taskr/releases/latest` over stdlib `net/http` (`fetchLatestRelease` / `downloadReleaseAsset` in helpers.go) and installs the matching asset — **no runtime dependency**; it used to shell out to `gh`, which meant the update button failed for anyone without a second tool installed. The repo is public, so the endpoint needs no auth; the unauthenticated hourly rate limit is the one failure worth its own message. Tests point `releaseAPIBase` at an `httptest` server. macOS and other Homebrew installations direct the user to Homebrew instead of modifying managed files.

The version lives **only in git tags + the release** — there is no version constant in the tree (`appVersion` defaults to `"dev"` and is injected at build time). So the next version = bump the latest tag from `gh release list`; don't trust local `git tag` (release tags may exist on the remote but not locally).

**Releases are automated** by `.github/workflows/release.yml`: pushing a `v*` tag cross-compiles Linux and Windows (version baked in from the tag), creates the GitHub release, and attaches the two assets under their exact names. So the publish flow is just:

```bash
git push origin main          # land the commits first
git tag v1.10.0               # bump from the latest release tag
git push origin v1.10.0       # ← triggers the build + release
```

Patch bumps are the norm for stat/layout tweaks; minor bumps for new interactive features.

The manual equivalent (if ever building locally) is the same two Linux/Windows `go build -ldflags "-s -w -X main.appVersion=$V"` invocations, feeding `taskr` and `taskr.exe` to `gh release create`. `-s -w` strips the symbol table and DWARF debug info, cutting ~30% off each binary with no functional change; local dev builds (`go run .` / `go build .`) deliberately keep them so `dlv` and rich panic traces still work.

## Architecture

Standard Bubble Tea MVU (`Model`/`Init`/`Update`/`View`), but the single-file convention is gone — the app is split by *concern*, with one big `model` struct threaded through everything:

- **`model.go`** — the `model` struct (large, flat), all the enums (`tab`, `appMode`, `pane`, sort modes), message types, `initialModel`, undo stack, and most pure model-mutation/lookup helpers.
- **`model_layout.go`** — `model`-method geometry helpers split out of `model.go`: detail-scroll cursor estimation (`estimateDetailCursorLine`), list-offset clamping, and the detail/list height math (`detailVisible`, `listVisible`, `maxDetailHeight`, `detailContentHeight`). Pairs with the pure width/height math in `layout.go`.
- **`update.go`** — top-level `Update`, the normal-mode list key handling, tab switching, editor launching, self-update plumbing. Row-level task keys (`d`/`t`/`p`/`T`/`r`/`x`) gate on `drilledIntoTasks()` so the Tasks tab and both drill-in lists stay one behaviour, not three.
- **`update_detail.go`** — `Update` handlers for the detail pane (`updateDetail`, detail cursor moves, `detailAdd`/`detailDelete`, `startEditing`); the input-side mirror of `view_detail.go`.
- **`update_modes.go`** — `Update` handlers for the text-entry / search modes (`updateInput`, `updateSearch`, `updateEditTitle`, etc.). When adding a modal interaction, the handler usually lives here.
- **`view.go`** — top-level `View` + the Tasks tab and shared rendering helpers. The help overlay lives here too: `helpBodyLines` builds `helpSec` blocks from the keymap registry plus the reference sections (quick-add tokens, search filters, row symbols, date input) and `filterHelpSections` narrows them for the overlay's `/` filter. `TestHelpDocumentsEveryToken` ties the token sections to `parseQuickAdd`/`compileSearch`, so a grammar change that skips the help fails the build; dispatches to `view_lists.go` (projects/tags/stats), `view_calendar.go`, `view_detail.go`.
- **`cache.go`** — `cacheState` (see below).
- **`board.go` / `view_board.go` / `update_board.go`** — the kanban Board tab (tab 5; there is no Learnings tab — learnings live in the task detail and the `taskr learnings` CLI): stage config (`activeStages`, an applyTheme-style global fed by settings.json `"stages"` and editable in Settings → "Board columns" via `modeEditStages`/`applyStageEdit`, which carries a renamed column's cards over with `stageRemap`; the Done column is `Status==Done` itself, never a stage), column rendering as a projection of the same filtered/cached lists the Tasks tab shows, and the interactions. `closePendingTask` (update_board.go) is the one pending→done path — the Tasks-tab `d`, the board `d`, and a card moved into Done all go through it; keep it that way or the timer/subtask/rank/recurrence semantics fork.
- **`storage_sqlite.go`** — the live SQLite backend behind the `Repository` port (repository.go): schema, `openStore`/`openStoreAt`, `loadTodos`, `sqliteRepo.Save`, row encode/decode, and the first-run JSON import. Schema is **fully normalized** since migration 002: every `todo.Todo` field maps to a real column (child records live in `task_tags`/`task_comments`/`task_learnings`/`task_time_entries`/`task_dependencies`); the legacy `data` JSON blob column still exists but is written as `''` and never read. **Adding a field to `todo.Todo` therefore requires a migration** (new `migrations/NNN_*.sql`) plus wiring it into the `sqliteRepo.Save` upsert and the `loadTodosCore` scan — a field with only a struct tag silently drops on the first save/load round-trip. Deletes are **soft (tombstones)** — `Save` upserts the dirty set and marks the IDs the Store hands it as `deleted=1` — so a deletion syncs instead of the row reappearing. The tombstone map carries *when* each delete happened (`map[string]time.Time`), not just which IDs: saves are debounced, so a flush-time `deleted_at` would order the deletion after edits that actually followed it, and it has to be the same recorded instant the undo path clamps against (`touchRestored`) or an undone delete can tie with its own tombstone and lose the merge by hash. A single connection (`SetMaxOpenConns(1)`) serializes the one writer.
- **`storage.go`** — settings load/save, the legacy JSON envelope (`taskFile`/`migrate`/`decodeTaskFile`), `loadTodosJSON` (now only the import source + corruption fallback), and task sorting.
- **`helpers.go`** — parsing (quick-add syntax, dates, time-entry edits), formatting, column layout, editor resolution, self-update file ops.
- **`completion.go`** — `taskr completion bash|zsh|fish` and `taskr man`, both generated from `cliCommandSpecs` (name, summary, flags, whether the first positional is a task ref). Three hand-written scripts plus a roff file would be four places to forget a new subcommand; `TestCompletionMatchesFlagSets` asks each command's own `flag.FlagSet` what its flags are and fails when the table disagrees, and `TestCompletionCoversEveryCommand` ties the table to what the CLI actually routes.
- **`keys.go`** — user keybindings as an overlay on the registry: `activeKeys` (action → key, an applyTheme-style global fed by settings.json `"keys"`), `sanitizeKeyOverrides` (drops unknown actions, non-single keys, `ctrl+c`, and per-context collisions, each with a reason), and `resolveKeyOverride`, which translates a pressed key into the registry default the `update*` switches case on — so the dispatch never learns about rebinding. A key a rebind freed is swallowed, or the old binding would linger as an alias. Everything user-facing renders through `effectiveKey`, which is why the hints, the help overlay and the palette all move together.
- **`palette.go`** — the command palette (`ctrl+k`, `modePalette`): entries generated from the keymap registry, and an entry *presses its key* rather than calling the action, so the palette can't grow a second code path for an action or drift from what the keys do. Multi-key bindings (`H/L`, `←/→`) are skipped by `paletteSendable`; the few worth naming get one explicit entry per direction in `paletteExtras`. Matching ranks whole-query substring, then per-word substring, then subsequence for queries of ≤ 3 runes.
- **`suggest.go`** — inline completion for the quick-add field's `#tag`/`@project` tokens: caret-token detection (`quickAddToken`), the candidate pool (same substring+recency rule as the detail-pane pickers, prefix matches floated first), and the splice (`acceptQuickAddSuggestion`). Keys live in `updateInput` (update_modes.go) gated on `quickAddMatches`, rendering in `renderQuickAddSuggestions` (view_preview.go) — one footer line, so the layout math is untouched.
- **`layout.go` / `styles.go` / `constants.go`** — width/height math, theming, magic numbers.
- **`tasksync/`** — the **sync engine package** (94% covered, and deliberately so: it is the one package where a bug loses user data rather than mis-rendering a list — `protocol_test.go` drives the real handler over httptest for the round trip, auth, SSE and the listener; `conflict_test.go` covers `DroppedLocalEdits`/`scalarHash`, the recovery net behind sync.log; `children_test.go` covers the learnings fold and the digest's order/zone invariance): the pure merge fold (`Merge`), the `/v1/sync` wire protocol (`Request`/`Response`, `PostSync`), the HTTP `Server`, real-time push (`Hub` for SSE fan-out, `Listener` for the client stream), conflict detection (`DroppedLocalEdits`), and the digest/canonicalization helpers. Storage- and UI-free by contract: its only demand on the app is the one-method `Store` interface (fold a task set into storage atomically — implemented by `dbStore` over `mergeIntoStore` in main). SQL, file paths (`sync.json`, `sync-state.json`, `sync.log`), config, and Bubble Tea glue stay in main; keep it that way.
- **`todo/`** — the **domain package**, framework-free. `todo.Todo` and its methods (`Toggle`, `AddTag`, `StartTimer`, `IsOverdue`, subtask/learning/comment/time-entry mutations). No Bubble Tea or rendering here; keep it that way.

### Two patterns that matter most

**1. The derived-view cache (`cacheState`).** The `Store`'s `tasks` map (`map[string]*todo.Todo`, store.go) is the single source of truth; everything the UI shows (active vs. done lists, sorted tags + counts, projects, overdue set, per-parent subtask progress) is *derived* and cached on the model. The store also maintains two indexes of its own — `subtaskOf` and `runningTimers` — which its mutators keep in step; never write them directly. After **any** mutation, call the right invalidator or the UI goes stale:

- `m.markModified(ids...)` — mark those tasks dirty for the next save, invalidate the caches, refresh, and re-anchor the cursor. Push the undo snapshot yourself (`m.pushUndo`) *before* mutating; markModified does not.
- `m.markCacheDirty()` — caches only, no dirty flag, no save.
- `m.markFilterDirty()` — only the filter-derived views (the active/done split and the tag-render cache), for a changed search query or focus filter.

`refreshCaches()` rebuilds derived data; it also calls `followTask` so the cursor stays on the same task ID across re-sorts. Tasks are addressed by **string ID**, not slice position — use `findTodoByID` / `m.get(id)` / `currentTodo`, since sorting and filtering constantly reorder every derived list.

**Sorting and per-row derivations are the refresh's cost centres**, not the scan: `selectActiveDone` splits and sorts the live set as `[]*todo.Todo` and materializes the two cached value lists once at the end, because sorting `[]todo.Todo` moved a 416-byte struct on every swap. Every mode's ordering is one `less(a, b *todo.Todo)` comparator (`lessByDueDate`, `lessBySize`, `lessBySequenceTie`, …) shared by `sortTodoPtrs` and `sortTodoValues`; each chain ends at `ID`, so they are total orders and the sorts use `sort.Slice` rather than a stable merge. The sequence sort carries its score in the slice being sorted instead of hashing IDs into a score map per comparison. Per-row facts that the row-metrics pass asks for every task — currently just subtask progress (`cache.subProgress`) — are built in one pass over the children; a *missing* key means "no subtasks", so the warm signal is the map being non-nil, never the lookup's `ok`.

**Cursors are clamped in one place.** `clampCursors` runs once per dispatch (the tail of `dispatch`) and pulls every list cursor back inside its list. Lists shrink out from under their cursor constantly — an undo that removes the task you were on, a delete confirmed from a modal, a narrowing filter, a tab switch restoring a cursor saved when the list was longer — and a cursor past the end selects *nothing*, so the next keystroke silently does nothing. Don't clamp per mutation; if a new list grows a cursor, add it there. `invariants_test.go` drives randomized key sequences (fixed seeds) and asserts this plus the store's structural invariants (`subtaskOf` ↔ `ParentID`, `runningTimers` ↔ open time entries, no self-dependency) after every key.

One consequence worth knowing: the **drill-in lists** (Tags/Projects `enter` → the row's tasks, `drillTaskList`) are *not* cached — they re-derive from the store on every read, so a mutation re-sorts them before `markModified` can note which task the cursor was on. `updateList` therefore anchors the drill cursor itself: it captures the task ID up front and re-follows it after the key, unless the key moved the cursor on purpose. Add a mutating key to the drill and you inherit that; add a cache for the drill list and you can drop it.

**2. Global theme state.** lipgloss styles are **package-level vars** reassigned by `applyTheme(theme)` (called at startup and on theme switch). Rendering code reads these globals directly; it does not receive a style set. Switching theme = call `applyTheme` with a different palette from `themes`. `init()` in `styles.go` applies `themes[0]` so styles are never nil in tests.

**3. Localization (`lang.go`).** UI strings are translated gettext-style: the English literal is the lookup key, so call sites read `tr("Settings")` and any untranslated string falls back to its English source. `activeLang` is a package-level global (like the theme), set by `applyLang(code)` at startup and on language switch (`cycleLang`); `initialModel` applies the stored language, so tests must `applyLang` **after** building the model. Adding a language = one entry in `translations` plus its date-name tables (`monthNames`, `weekdayNames`, etc. — Go's `time` has no locale support, so name-bearing date layouts go through `localized*` helpers). Only display strings are translated; stored data and quick-add/date **parsing** keywords stay English. `TestEveryLanguageTranslatesEveryUIString` (lang_test.go) is the guard against the failure mode this scheme invites — a new string ships untranslated and nothing breaks, the screen just renders half in the wrong language. It scans the sources for `tr("…")` literals *and* enumerates the strings that reach `tr` through a variable (keymap descriptions, `shortLabel`, help section titles, the sequencer personalities, the enum words behind `trPriority`/`trSize`/`trRecurrence`), so a string that never goes through `tr` at all is caught by adding it to `dynamicUIStrings`. Priority words are localized at the view layer via `trPriority` to keep the `todo` package locale-free. `TestNarrowNoWrapDanish` guards the no-wrap contract against longer Danish strings by comparing each tab/width to the English baseline.

### Other conventions

- **Persistence is debounced and differential** — mutations set `dirty`/`savePending`, and a `saveTickMsg` (300ms) drains the change set with `Store.drainDirty()` and hands it to `Repository.Save(dirty, tombstones)` on a background command. `drainDirty` deep-copies each dirty task, so the save goroutine can never read a task the Update goroutine is still editing; the quit path calls `flushPendingWrites` synchronously so the last 300ms of edits survive `q`. Don't write the store synchronously from `Update`.
- **Modes drive input.** `m.mode` (an `appMode`) decides which `update*`/`render*` path runs. Adding a feature with text entry or a confirm prompt means: add an `appMode` const, a handler (usually `update_modes.go`), and a render branch.
- **Subtasks, dependencies, learnings** all live in the same task set (a subtask is a full `Todo` carrying a `ParentID`; the store's `subtaskOf` index maps a parent to its children in `CreatedAt` order), so global operations loop the whole set — see `renameTagGlobally`, `selectLearnings`.
- Data lives at `~/.taskr/tasks.db` (SQLite; WAL, so `-wal`/`-shm` sidecars), settings at `~/.taskr/settings.json`. The legacy `~/.taskr/tasks.json` (+ `.bak`) is read only to seed a fresh database, then left in place. Built binaries and `*.bak` are gitignored. **Tests must not touch real `~/.taskr`** — `TestMain` (`main_test.go`) redirects `$HOME` to a temp dir for the whole test binary, because several tests build a `model` (→ `initialModel` → `loadTodos`) which opens the store.

### Rendering conventions

- **ANSI-aware width math.** Once a string has been through a lipgloss `.Render`, `len([]rune(s))` over-counts by the escape sequences and silently breaks alignment/centering. Use `ansi.StringWidth` to measure and `ansi.Truncate` to clip **styled** strings; `len([]rune(...))` is only correct for plain text. Width tests assert no line exceeds the pane's inner width (`termWidth-8`) — that's the no-wrap contract.
- **Shared list-column rule.** The leading "name" column on the Tasks / Projects / Tags list tabs is sized by `contentFitWidth` (hug the widest entry + gap, floored to the header label, capped by the responsive `nameColWidth`) in `layout.go`. Reuse it for any new list tab instead of inventing per-tab width constants, so all tabs reflow identically on resize.
- **Small terminals are a supported size.** Every width budget is derived from the window (`termWidth-6` and friends) and goes *negative* on a few-column terminal, where a rune slice or `strings.Repeat` panics — that was a real crash. `truncate`/`padRight`/`padLeft`/`padCenter` clamp a negative width to zero, so new call sites inherit the guard; keep it that way rather than clamping per caller. Two-column layouts need a narrow fallback instead of floors: two floored columns joined on a small window produce lines wider than the terminal (`buildCalendarNarrow` below `calSideBySideMinWidth`, `buildProjectDrillNarrow` below `projDrillMinWidth`, `renderBoardStacked` below `boardMinColW`). `smallterm_test.go` sweeps every tab × on-screen state × size from 0×0 up, asserting both "no panic" and the no-wrap contract (from 8 columns up, below which only chrome is left).
- **Group same-style runs.** When emitting a row of per-cell-styled glyphs (tag progress bars, the stats histogram via `statsCell`/`renderCellRow`), coalesce consecutive cells that share a style into one `.Render` call — far fewer escape sequences and it keeps `ansi.StringWidth` honest.

## House rules (from global CLAUDE.md)

- Match the existing style of the file you edit; **no blanket reformatting**, and keep any formatting-only change in its own commit.
- TokyoNight-style palette is the visual baseline.
- Share the approach and get buy-in before large multi-file or expensive changes rather than spiraling.
- After meaningful changes, remember this repo is public under GitHub user `Iliorn` (capital I — `git remote` is `https://github.com/Iliorn/taskr.git`).
