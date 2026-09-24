# Architecture

How taskr is put together, and the conventions that are load-bearing rather
than stylistic. Read the section for the area you are touching before changing
it. Each rule carries a short reason; the longer story of how it came about is
in the commit that introduced it.

This is the document [CONTRIBUTING.md](CONTRIBUTING.md) refers to, and the one
[CLAUDE.md](CLAUDE.md) hands to Claude Code. There is one copy so the two
audiences cannot be told different things.

## What this is

`taskr` is a keyboard-driven terminal task manager built with Go and Bubble Tea
(Charm). It is a standalone app with its own SQLite storage — **not** a
Taskwarrior frontend. Beyond tasks it has a calendar/time-tracking view,
projects (Gantt), tags, a kanban board, a stats dashboard, a CLI, cross-device
sync, and in-app self-update.

## Commands

```bash
go build -o taskr .                                        # build (version = "dev")
go build -ldflags "-X main.appVersion=v1.8.0" -o taskr .   # build with a real version
go run .                                                   # build & run
go test ./...                                              # all packages
go test -run TestName ./...                                # one test
go vet ./...
golangci-lint run ./...                                    # config in .golangci.yml
```

CI (`.github/workflows/ci.yml`) runs vet, test and build on ubuntu, windows and
macos runners, a `cross` job that cross-compiles every release target, and a
separate golangci-lint job. The matrix exists so platform-specific code runs
before a release rather than being first compiled by it. `os.UserHomeDir` reads
`%USERPROFILE%` on Windows and `$HOME` elsewhere, so `TestMain` sets both;
`TestStorageStaysInsideTheTestHome` fails if the redirect stops covering a
platform.

Tests live beside the code (`*_test.go`). The Bubble Tea loop has two dedicated
suites: `update_keyscript_test.go` drives real `Update` dispatch with scripted
keys, and `undo_property_test.go` runs randomized op+undo pairs (fixed seeds)
and checks the content digest round-trips. **When adding a modal interaction,
add a script flow for it.**

### Releasing

Pushing a `v*` tag runs `.github/workflows/release.yml`, which cross-compiles
Linux and Windows with the version baked in, creates the GitHub release and
attaches the assets:

```bash
git push origin main          # land the commits first
git tag v1.10.0               # bump from the latest release tag
git push origin v1.10.0       # ← triggers the build + release
```

- **The version lives only in tags.** `appVersion` defaults to `"dev"` and is
  injected at build time. Take the next number from `gh release list`, not the
  local `git tag` (remote tags may be missing locally). Patch bumps for
  stat/layout tweaks, minor bumps for new interactive features.
- **Asset names are load-bearing — never rename one.** An installed binary
  looks for the name it was built with, so a name can be added but not
  changed: `taskr` (Linux x64), `taskr-linux-arm64`, `taskr.exe` (Windows
  x64), plus `SHA256SUMS`. `selfUpdateAsset(goos, goarch)` is the one map
  from platform to asset; a new build target needs a case there.
- **macOS ships from source** through the `Iliorn/homebrew-tap` repository
  (`brew install iliorn/tap/taskr`). Do not attach macOS binaries or `.app`
  bundles to releases; Homebrew installs are pointed at Homebrew rather than
  having their managed files replaced.
- **Self-update** (Settings → "Update to latest release") reads
  `/repos/iliorn/taskr/releases/latest` over stdlib `net/http`
  (`fetchLatestRelease`, `downloadReleaseAsset`), so it needs no other tool
  installed. `downloadVerifiedAsset` checks the asset against `SHA256SUMS`
  and fails closed. The endpoint needs no auth; the unauthenticated rate limit
  gets its own message. Tests point `releaseAPIBase` at an `httptest` server.
- **A CHANGELOG entry is one line.** The release body is the tag's CHANGELOG
  section (`packaging/release-notes.sh`, tied to the file by
  `TestReleaseNotesExtractionMatchesTheChangelog`), and
  `TestChangelogEntriesAreOneLine` enforces the cap. A release page is read by
  someone deciding whether to upgrade; the argument for a change belongs in its
  commit message.
- A manual release is the same two `go build -ldflags "-s -w -X
  main.appVersion=$V"` invocations fed to `gh release create`. `-s -w` strips
  symbols and DWARF (~30% smaller); dev builds keep them for `dlv` and full
  panic traces.

## Packages

The app is package `main`, split into files by concern. Four packages sit
beside it, each with a boundary the compiler enforces:

- **`todo/`** — the domain: `todo.Todo` and its methods (`Toggle`, `AddTag`,
  `StartTimer`, `IsOverdue`, subtask/comment/time-entry mutations). No Bubble
  Tea, no rendering.
- **`rank/`** — the sequencing engine. It imports only `todo`, so it is
  locale-free and model-free by construction. See *The ranking engine*.
- **`paths/`** — where files live. See *Paths*.
- **`tasksync/`** — the sync engine. See *Sync*.

## The app (package main)

Standard Bubble Tea MVU with one large `model` struct threaded through
everything.

- **`model.go`** — the `model` struct, the enums (`tab`, `appMode`, `pane`,
  sort modes), message types, `initialModel`, the undo stack, and most pure
  lookup/mutation helpers.
- **`model_layout.go`** — geometry on the model: detail/list heights,
  list-offset clamping, and detail scrolling. The detail pane's scroll is a
  **persistent offset** (`detail.scroll`) that `detailScrollWindow` moves only
  when the cursor comes within `detailScrollMargin` of an edge; deriving the
  top from the cursor would glue the cursor to one row and slide the document
  under it. It runs twice per key: `clampDetailScroll` (from `clampCursors`)
  against an estimate, then `applyDetailScrollN` against the lines actually
  rendered. So `estimateDetailCursorLine` and the `detail*Height` helpers must
  track `view_detail.go` section for section —
  `TestDetailCursorEstimateMatchesTheRenderedDocument` checks by finding the
  `▶` in the rendered pane.
- **`update.go`** — top-level `Update`, list keys, tab switching, editor
  launching, self-update. Row-level task keys (`d`/`t`/`p`/`T`/`r`/`x`) gate on
  `drilledIntoTasks()`, so the Tasks tab and both drill-in lists behave as one.
- **`update_detail.go`** — the detail pane's input side (`updateDetail`,
  `detailAdd`/`detailDelete`, `startEditing`); mirrors `view_detail.go`.
- **`update_modes.go`** — text-entry and search modes (`updateInput`,
  `updateSearch`, `updateEditTitle`, …). A new modal handler usually goes here.
- **`view.go`** — top-level `View`, the Tasks tab, shared rendering helpers,
  and the help overlay (`helpBodyLines` builds `helpSec` blocks from the keymap
  registry plus reference sections; `filterHelpSections` serves its `/`
  filter). `TestHelpDocumentsEveryToken` ties the token sections to
  `parseQuickAdd`/`compileSearch`. Other tabs render from `view_lists.go`,
  `view_calendar.go`, `view_board.go` and `view_detail.go`.
- **`cache.go`** — `cacheState`; see *The derived-view cache*.
- **`storage_sqlite.go`** — see *Storage*. **`storage.go`** holds settings
  load/save, the legacy JSON envelope (`taskFile`/`migrate`/`decodeTaskFile`,
  now only an import source), and the task comparators.
- **`helpers.go`** — parsing (quick-add syntax, dates, time-entry edits),
  formatting, column layout, editor resolution, self-update file operations.
- **`cli.go` + `cli_*.go`** — the command-line verbs, one file per group:
  `cli.go` (dispatch table, `help`, `update`, shared helpers), `cli_add.go`,
  `cli_edit.go`, `cli_lifecycle.go` (done, reopen, delete, undelete, undo),
  `cli_time.go`, `cli_query.go` (list, search, tags, projects, top),
  `cli_show.go` (show, why, stats). A new verb goes in the group it reads like,
  and into `dispatchCLI` and `cliCommandSpecs`. The CLI stays English: it never
  calls `applyLang`.
- **`cli_refs.go`** — ref resolution, `loadForCLI`, and the list filter behind
  `list` and `search` (`listFilterOpts`/`filterTopLevel`), including the
  review filters (`staleFor`, `unblockedFor`, word and regexp matching) and the
  CLI-only sorts in `sortTodosByCLIMode` (age/idle/pri — the TUI's sort modes
  must each line up with a visible column). `now` is injectable for tests.
- **`completion.go`** — `taskr completion bash|zsh|fish` and `taskr man`,
  generated from `cliCommandSpecs`. `TestCompletionMatchesFlagSets` compares
  the table with each command's real `flag.FlagSet`;
  `TestCompletionCoversEveryCommand` ties it to the dispatch.
- **`keymap.go`** — the keymap registry: every binding with its action id,
  contexts and description. It generates the footer hints, the help overlay
  and the command palette, so a new key is added there first.
  `TestKeymapActionsAreConsistent` keeps one action on one key everywhere.
- **`keys.go`** — keybinding overrides as an overlay on the keymap registry.
  `navAlias` maps j/k to down/up at dispatch (after the override pass, so a
  rebind onto j or k wins). `sanitizeKeyOverrides` drops unknown actions,
  multi-key values, `ctrl+c` and per-context collisions, each with a reason.
  `resolveKeyOverride` translates a pressed key into the registry default the
  `update*` switches expect, so dispatch never learns about rebinding; a key
  that a rebind freed is swallowed. Everything user-facing renders through
  `effectiveKey`, so hints, help and palette move together.
- **`palette.go`** — the command palette (`ctrl+k`, `modePalette`). Entries
  come from the keymap registry and **press their key** rather than call the
  action, so the palette cannot grow a second code path. Multi-key bindings
  are skipped by `paletteSendable`; the few worth listing are in
  `paletteExtras`. Ranking: whole-query substring, then per-word substring,
  then subsequence for queries of ≤ 3 runes.
- **`suggest.go`** — inline `#tag`/`@project` completion for both the
  quick-add and search fields. `completionMatches` decides which field is
  completing and `applyCompletionKey` drives it, so the two share one
  implementation. It renders on the single footer line
  (`renderQuickAddSuggestions`), which the parse preview takes back once the
  caret leaves the token. Both are gated on `searchUsesTokenGrammar` — true for
  Tasks, Board and Stats, which run `compileSearch`; false for Projects, which
  filters project names.
- **`groups.go`** — **the Tags and Projects tabs are one implementation.** A
  tag and a project are both a named group of tasks, so the row summary
  (`groupSummary`), order (`groupSort`, persisted as `tag_order`/
  `project_order`), hide-finished rule (`visibleGroups`, toggled by `h` via
  `showFinishedGroups`) and the list behind a row (`groupTaskList`) are
  shared. Each tab only says how a task maps to its groups
  (`tagGroupKeys`/`inTagGroup`, `projectGroupKeys`/`inProjectGroup`); the two
  halves must agree, or a row counts a different list from the one enter
  opens. Summaries are built in `refreshCaches` (`refreshGroups`). Both lists
  draw through `renderGroupRows` and their panes through `groupPaneLines`. The
  Projects pane shows the timeline only when an open task has a date
  (`hasDatedOpenTask`), and the timeline always reaches today
  (`ganttDateWindow`).
- **`board.go` / `view_board.go` / `update_board.go` / `board_carry.go`** —
  the kanban tab; see *The board*.
- **`input.go`** and **`console.go`** (each with `_windows`/`_other`
  variants) — see *Terminals*.
- **`trace.go`** — opt-in latency tracing (`TASKR_TRACE=1` →
  `~/.taskr/trace.log`): per frame, the wall clock, gap since the previous
  frame, `Update` and `View` durations, GC count and message. It writes on its
  own goroutine and drops rather than blocks. Measured: a keystroke costs
  ~0.1ms in `Update` and ~1ms in `View` at 2000 tasks, and an idle app produces
  no messages, so a late keystroke is not the model's compute.
  `TASKR_NO_WATCH=1` removes the file watcher, the app's only continuous OS
  interaction; bisect with it first.
- **`crash.go`** — the panic path. Bubble Tea already recovers panics (on its
  loop and in commands) and restores the terminal, so the guard is deferred
  *inside* `Update` and `View` and re-panics: that is the only place holding
  both the stack from the panic site and the live model. It writes
  `crash-<ts>.log` to the state dir (newest five kept), flushes pending writes,
  and records the path for `main` to print after `Run` returns. `msg` is
  formatted (`msgKind`) only on the panic path, keeping the guard
  allocation-neutral (`BenchmarkView`, `BenchmarkSearchKeystroke`).
- **`layout.go` / `styles.go` / `constants.go`** — width/height math, theming,
  magic numbers.

## Patterns that matter most

### The derived-view cache

The `Store`'s `tasks` map (`map[string]*todo.Todo`, store.go) is the single
source of truth. Everything the UI shows — active and done lists, tag and
project summaries, the overdue set, subtask progress — is derived and cached on
the model. The store keeps two indexes itself, `subtaskOf` and
`runningTimers`; only its own mutators may write them. After **any** mutation,
call the right invalidator or the UI goes stale:

- `m.markModified(ids...)` — mark tasks dirty for the next save, invalidate
  caches, refresh, re-anchor the cursor. Push the undo snapshot yourself
  (`m.pushUndo`) *before* mutating.
- `m.markCacheDirty()` — caches only; no save.
- `m.markFilterDirty()` — only the filter-derived views, for a changed search
  or focus filter.

`refreshCaches()` rebuilds derived data and calls `followTask`, so the cursor
stays on the same task across re-sorts. **Address tasks by string ID**
(`findTodoByID`, `m.get(id)`, `currentTodo`), never by slice position.

Sorting is the refresh's cost centre. `selectActiveDone` sorts
`[]*todo.Todo` and builds the cached value lists once at the end, because
sorting values moves a 416-byte struct per swap. Every mode's order is one
`less(a, b *todo.Todo)` comparator (`lessByDueDate`, `lessBySize`, the
engine's `LessTie`, …) that ends at `ID`, so each is a total order and
`sort.Slice` suffices. `cache.subProgress` is built in one pass; a missing key
means "no subtasks", so the warm signal is the map being non-nil.

### Cursors are clamped in one place

`clampCursors` runs once at the tail of every `dispatch` and pulls each list
cursor back inside its list. Lists shrink under their cursor constantly (undo,
delete, a narrowing filter, a tab restoring an old cursor), and a cursor past
the end selects nothing. Don't clamp per mutation; a new list with a cursor is
added there. `invariants_test.go` drives randomized keys (fixed seeds) and
checks this plus the store's invariants (`subtaskOf` ↔ `ParentID`,
`runningTimers` ↔ open time entries, no self-dependency) after every key.

The **drill-in lists** (Tags/Projects → enter, `drillTaskList`) are not cached;
they re-derive on every read, so `updateList` captures the task ID before a key
and re-follows it after. One level up, the group itself is pinned by name
(`tagPinned`/`projectPinned`): `visibleGroups` keeps a pinned group listed once
finished, and `clampCursors` re-finds it (`followPinnedGroups`).

### State the model owns, and the few globals left

Preferences that code on another goroutine also needs are **values held by the
model and copied out**, never package variables:

- `m.rank` — the ranker (bias knobs, activity heat, the 100% mark), refreshed
  in `refreshCaches`. The repository keeps its own copy through `SetRanker`
  (mutex-guarded, because saves run on a background command), and a sync merge
  scores with biases it is handed.
- `m.boardCfg` — a `boardConfig` (column list, board shown, last edited,
  shared with the fleet). A sync is handed the wire form; an arriving list is
  adopted on the loop.

The CLI has no model: `loadForCLI` puts the ranker on the repository it
returns, and the paths that run beside a command read settings.json directly
(`storedBiases`, `storedBoard`).

What remains global is set once at startup and read on the Update loop:

- **Theme.** lipgloss styles are package-level vars reassigned by
  `applyTheme(theme)`; rendering reads them directly. `init()` in `styles.go`
  applies `themes[0]` so styles are never nil in tests.
- **Language.** `activeLang`, set by `applyLang`. See *Localization*.
- **Keybindings.** `activeKeys`, set by `applyKeys` from settings.json
  `"keys"`.

### Localization

UI strings are translated gettext-style: the English literal is the key
(`tr("Settings")`), and an untranslated string falls back to English.
`initialModel` applies the stored language, so tests call `applyLang` **after**
building a model. Adding a language = one entry in `translations` plus its
date-name tables (`monthNames`, `weekdayNames`, …; Go's `time` has no locale
support, so name-bearing layouts go through `localized*` helpers).

- **Input is localized; stored data is not.** The quick-add and search grammars
  (`lang_input.go`) accept the active language's keywords (`frist:imorgen`,
  `p:høj`, `überfällig`, weekday names) as well as English. The aliases are
  derived from the translation table itself plus `weekdayNames`/
  `weekdayAbbrevs`, so what the screen prints and what the parser takes cannot
  drift; `extraInputAliases` holds only genuine irregularities. `applyLang`
  rebuilds `activeInputWords`. Adding a keyword = one entry in `inputKeywords`
  plus its translation. Stored values and the sync wire stay canonical English,
  so installs in different languages sync unchanged.
- **Hints are assembled from the grammar.** `quickAddHint`, `searchHint` and
  `invalidDateMsg` are built from the keyword helpers, and
  `TestHintsOnlyAdvertiseTokensThatParse` parses each back in every language.
- **Every string must be translated.** `TestEveryLanguageTranslatesEveryUIString`
  scans for `tr("…")` literals (skipping `lang.go` itself) and enumerates the
  strings that reach `tr` through a variable — keymap descriptions,
  `shortLabel`, help titles, bias levels, the words behind
  `trPriority`/`trSize`/`trRecurrence`. A string that reaches the screen some
  other way goes in `dynamicUIStrings`. Sentences therefore live outside
  `lang.go` (e.g. `trSeqReason` in `view_explain.go`).
- **Translations must fit.** `TestNarrowNoWrapTranslated` compares every tab
  and width with the English baseline for each of `availableLanguages`.
  Shipping languages are Danish and German; German is the width stress case.

## The ranking engine (`rank/`)

- **`score.go`** — the score: five dimensions, three bias knobs, the
  activity-heat snapshot behind Momentum, the `Ranker` value that carries all
  three inputs, hit rate and miss analysis.
- **`explain.go`** — one task's score as a breakdown with per-factor reason
  codes, its position and margins to its neighbours, and a forecast of the
  moments its rank moves on its own (the midnight deadline step, momentum
  expiring). Heat maps each key to its newest signal *instant*, which is what
  lets `Expire`/`HeatExpiries` say when heat runs out.
- **`order.go`** — the lifts a task inherits from its subtasks and from the
  work waiting on it (`Lifts`), the partition that sinks blocked work
  (`DependencySets`), the sequence sort (`SortPtrs`, `SortValues`) and `Top`.

Rules:

- **Sorts score against one instant.** Every sort and ranking takes its score
  function from `ScoreNow()`, never `Score`, which reads the clock per call.
  Age accrues continuously, so two identical tasks scored microseconds apart
  differ by ~1e-11 and never reach the tie-break
  (`TestSequenceSortIsDeterministicForIdenticalTasks`).
- **Displayed scores are percentages.** The raw score is unbounded, so every
  surface that shows one renders `FormatPercent` — a share of the
  highest-scoring pending task. Points appear only where the arithmetic is
  explained (the `w` overlay, `taskr why`, `stats --seq`), and those state
  what 100% currently costs. A hypothetical field (the Settings knob preview)
  passes its own maximum to `PercentOfField`.
- **The reading side is in the app.** `view_explain.go` owns the sentences
  (`trSeqReason`), the row layout, the `w` overlay and the `taskr why` output,
  so the two surfaces cannot describe one score differently.

## Storage

- **`storage_sqlite.go`** is the SQLite backend behind the `Repository` port
  (repository.go): schema, `openStore`/`openStoreAt`, `sqliteRepo.Save`, row
  encoding, and the first-run import of legacy `tasks.json`.
- **Adding a field to `todo.Todo` requires a migration.** The schema is fully
  normalized (child records in `task_tags`/`task_comments`/
  `task_time_entries`/`task_dependencies`). A new field needs a
  `migrations/NNN_*.sql`, plus wiring into the `sqliteRepo.Save` upsert and the
  `loadTodosCore` scan — a field with only a struct tag silently drops on the
  first round trip.
- **Deletes are tombstones.** `Save` upserts the dirty set and marks the IDs it
  is handed `deleted=1`, so a deletion syncs. The tombstone map carries *when*
  each delete happened (`map[string]time.Time`): saves are debounced, and it
  must be the same instant the undo path clamps against (`touchRestored`).
- **One writer.** A single connection (`SetMaxOpenConns(1)`) serializes writes.
- **Fail closed on a store from the future.** `pendingMigrations` returns
  `errSchemaTooNew` when `schema_version` is newer than this build knows, and
  `main` exits rather than show an empty list over a full database and write
  older columns back.
- **Saves are debounced and differential.** Mutations set
  `dirty`/`savePending`; a `saveTickMsg` (300ms) drains the change set with
  `Store.drainDirty()` (deep copies, so the save goroutine never reads a task
  being edited) and hands it to `Repository.Save` on a background command.
  Quitting calls `flushPendingWrites` synchronously. Never write the store
  synchronously from `Update`.

## Paths (`paths/`)

Resolve every path through `paths.Dir`/`paths.For`/`paths.Ensure`, never
`os.UserHomeDir` plus a literal. There are four kinds — config, data, state,
cache — mapped to XDG, `%APPDATA%`/`%LOCALAPPDATA%` or `~/Library`. Two
overrides come first: `TASKR_HOME` collapses all four into one directory, and
an existing `~/.taskr` pins an old install there for good (no migration, by
design). Only the data directory is created eagerly, so a new writer calls
`paths.Ensure(kind)`.

Data lives at `<data>/tasks.db` (WAL, so `-wal`/`-shm` sidecars). **Tests must
not touch the real home:** `TestMain` (`main_test.go`) and `setTestHome`
redirect `$HOME` and neutralize `XDG_*`/`TASKR_HOME`, and
`TestStorageStaysInsideTheTestHome` walks every path the app can write.

## Sync (`tasksync/` and the app's glue)

`tasksync` is the one package where a bug loses data rather than mis-renders a
list, so it is held to high coverage. It holds the pure merge fold (`Merge`),
the `/v1/sync` protocol (`Request`/`Response`, `PostSync`), the HTTP `Server`,
real-time push (`Hub` for SSE, `Listener` for the client), conflict detection
(`DroppedLocalEdits`, the recovery net behind sync.log) and the digest helpers.
It is storage- and UI-free: its only demand on the app is the one-method
`Store` interface (`dbStore` over `mergeIntoStore`). SQL, file paths, config
and Bubble Tea glue stay in the app.

- **Two versions, two questions.** `ProtocolVersion` is the wire *format*; a
  mismatch is refused with a 409 before merging. `VersionHeader`
  (`Taskr-Version`) is the *build*, stamped by `stampVersion` on every
  response — including a bare 500 from `http.Error`, which is what a stale
  server answers after a newer build migrated its store. `PostSync` puts it in
  the error text (`serverError`) and on `Response.ServerVersion`, where
  `VersionGapWarning` warns about the case that does *not* fail: an additive
  migration an old server silently drops. Package text is English and
  unlocalized; the app wraps it in a translated frame.
- **Bodies are gzipped where both ends agree** (`compress.go`). Responses need
  no negotiation. Requests are compressed only toward a server that advertised
  `Accept-Encoding: gzip`; a 400/415 to a compressed request drops the
  capability and resends plain once. The 64 MB request cap applies after
  decompression (`cappedReader`). The SSE stream is never compressed.
- **`servetls.go`** — https for headless `taskr serve`
  (`--tls-cert`/`--tls-key`). The pair is loaded before binding, then re-read
  by `certReloader.getCertificate` whenever a file's mtime moves, since
  `tailscale cert` and Let's Encrypt renew in place. A failed reload keeps the
  last good pair. The TUI's in-process server stays plain; `healthAnswers`
  probes plain, then https.
- **The board's column list syncs** (`boardsync.go`, `tasksync/board.go`) as
  one optional field each way (`Request.Board`/`Response.Board`), so a stage
  name means the same column on every device. No protocol bump: an older peer
  ignores the field, and a client that sends none means "leave my columns
  alone". `MergeBoard`: the later edit wins, and **a zero timestamp never
  wins**, so a device still on the defaults has nothing to say. The edit stamp
  is set only by `applyStageEdit`. The server keeps the fleet's list in
  `board.json`. An adopted list does not re-stage cards: an unknown stage falls
  into the first column.
- **First sync asks before uploading** (`syncadopt.go`). The merge is a union
  by ID, so a device's first sync hands every local task to every other device,
  with no undo. `firstSyncNeedsChoice` (never synced, has live tasks, no answer
  recorded) is the gate. Unattended syncs (TUI launch and timer, CLI
  after-command) decline with `firstSyncNotice`; manual `taskr sync` goes
  through `resolveFirstSync` and needs `--adopt-local` or `--adopt-remote`.
  The answer is stored in sync.json (`Adopted`), so losing the state directory
  does not ask again. `--adopt-remote` exports a backup, clears local rows
  outright (no tombstones — these IDs never left the machine), then pulls.

## The board

The kanban tab (tab 5). Its configuration is a `boardConfig` on the model.

- **The last column is Done.** It is `Status==Done`, not a stored stage, but
  its heading is renameable. Every lookup goes through the config's `pending`
  and `doneColumn`; `stageIndex` and `canonicalStage` search pending columns
  only, so no pending task can be filed under Done and `--stage <Done>` cannot
  complete one. `setStages` runs `ensureDoneColumn`, so "at least one working
  column, then Done" holds whatever set the list.
- **Editing columns** (Settings → "Board columns", `modeEditStages`,
  `applyStageEdit`) carries a renamed column's cards over with `stageRemap`.
- **Columns are a projection** of the same filtered, cached lists the Tasks tab
  shows, so `/` on the Board is the shared search and there is no board-only
  filter state.
- **One close path.** `closePendingTask` (update_board.go) is the only
  pending→done transition — Tasks `d`, Board `d` and a card dropped in Done all
  use it, or timer/subtask/rank/recurrence handling forks.
- **Carrying** (`modeBoardCarry`): enter picks a card up, ←/→ carry it,
  enter/esc put it down. Nothing is stored until the drop
  (`boardColumnsForView` only draws the held card over its target), so the trip
  is one undo step; the drop goes through `boardPlaceCard` like H/L. A held
  card is lit in the carry colour (`board_carry.go`), green with ✓ over Done.
- **Layout.** Columns are an even grid (`boardColWidths`), so the board does
  not shift under a carried card. Cards are rounded boxes (`renderBoardBox`:
  border colour carries overdue/timer/done; the bottom edge carries project and
  due, `boardBoxBottom`). `chooseBoardCardLayout` steps down to one-line boxes,
  then plain rows, all or nothing across the visible columns. Columns scroll
  through a window (`boardWindow`, clamped by `clampBoardWindow` from
  `clampCursors`), and a tall column scrolls its cards (`boardCardWindow`,
  `clampBoardCardScroll`), both against the render's own `boardGeometry`.
  Below `boardMinWindowCols` visible columns it falls back to
  `renderBoardStacked`.
- Space opens a read-only card view (`modeBoardCard`, `renderBoardCardView`);
  editing stays in the Tasks detail pane. `a` files a new card into the focused
  column (`board.addCol`).
- **Hiding the board** (settings `board_disabled`) removes the tab from the
  bar, tab cycling, the digit keys and the palette (`tabVisible`), and the
  detail pane's Stage row (`stageFieldVisible`). Tab numbers never renumber —
  they are part of the translated labels (`tr("6 Stats")`).

## Terminals

- **Keyboard (`input.go`).** Off Windows the files are stubs. On Windows,
  Bubble Tea's console-event reader polls with a 16ms sleep, so the first key
  after a pause lags; `tea.WithInputTTY()` opens `CONIN$` and reads the VT
  stream instead. That path has no resize events, so the Windows build polls
  the console size (`startResizePoller`). `TASKR_WIN_CONSOLE_INPUT=1` goes back.
- **Encoding (`console.go`).** A Windows console decodes output with its code
  page (CP850 on a Danish install), which garbles UTF-8. `useUTF8Console` sets
  the output and input code pages to 65001 for the run and restores them — on
  every exit path, since `os.Exit` skips defers. `taskr doctor` reports the
  page. mintty (Git Bash, MSYS2) is not a console: its charset comes from the
  locale, so `prepareConsole` sends OSC 701 (`minttyUTF8Sequence`, only when
  `MSYSTEM`/`TERM_PROGRAM` say mintty and stdout is a tty). That one is not
  restored on exit.

## Other conventions

- **Settings is one pane of grouped rows** (`settingsGroups`, view_lists.go).
  A row in no group is never drawn, so a new setting needs a group entry.
  `settingsNavOrder` skips rows that `settingsSelectable` rejects (Version) or
  `settingsRowVisible` hides (Listen and Server token while no server runs).
  `renderSettingsSection` returns the content *and* the cursor row's line,
  which the pane scrolls by. `settingsEditsText` marks rows whose enter opens
  an editor. All value changes go through `settingsAdjust(dir)`, which ←, →
  and enter share.
- **Modes drive input.** `m.mode` (an `appMode`) picks the `update*`/`render*`
  path. A feature with text entry or a confirm prompt adds an `appMode`, a
  handler (usually `update_modes.go`) and a render branch.
- **Subtasks and dependencies share the task set** (a subtask is a full `Todo`
  with a `ParentID`), so global operations loop the whole set
  (`renameTagGlobally`, `summarizeGroups`). Two fields are tree-scoped and
  travel opposite ways: a deadline runs *up* (`extendAncestorsDue`), priority
  runs *down* (`clampPriorityToParent`, `clampDescendantsPriority`) — raising a
  child never lifts a parked parent, and the TUI says the child was capped.
  Both live in `taskops.go` and are called from the TUI (`cyclePriority`) and
  the CLI (`editOneTask`) alike.
- **Detail placement is one predicate.** `detailPos` (settings
  `detail_position`: `right`/`left`/`bottom`) feeds `sideBySide()`; bottom
  makes it false at every width, and left swaps the two sized panels at the end
  of `buildSideBySide`. Unknown values read as `right`.

## Rendering conventions

- **ANSI-aware width math.** After a lipgloss `.Render`, `len([]rune(s))`
  counts escape sequences. Measure styled text with `ansi.StringWidth` and clip
  it with `ansi.Truncate`. Width tests assert no line exceeds the pane's inner
  width (`termWidth-8`) — the no-wrap contract.
- **Shared name column.** The leading column on Tasks/Projects/Tags is sized by
  `contentFitWidth` (layout.go). Reuse it for a new list tab.
- **Small terminals are supported.** Width budgets go negative on a tiny
  window, so `truncate`/`padRight`/`padLeft`/`padCenter` clamp negative widths
  to zero. Two-column layouts need a narrow fallback, not floors
  (`buildCalendarNarrow` below `calSideBySideMinWidth`,
  `buildProjectDrillNarrow` below `projDrillMinWidth`, `renderBoardStacked`).
  `smallterm_test.go` sweeps every tab × state × size from 0×0 for panics and
  the no-wrap contract.
- **Group same-style runs.** Coalesce consecutive same-styled cells into one
  `.Render` (`statsCell`/`renderCellRow`): fewer escape sequences, honest
  widths.
- **A task row is two tones.** `renderTaskLineWithSet` builds rows through
  `rowBuf`, which counts width on the unstyled text and coalesces runs by SGR
  prefix/suffix. `taskRowPalette` gives `status` (normal/overdue/blocked/timer,
  with the selection background) to cursor, checkbox and title, and `meta`
  (dim) to Score, Size and Project. The Due cell takes the status tone only
  when the task is late, so red in that column means the date is the problem.
- **Titles clip; badges don't.** `taskRowLabel` splits a label into prefix,
  title text and badges (`!`, `↥`, `↧`, `↻`, `(1/2)`); `fitTaskRowLabel` clips
  only the text. `refreshTaskColMetrics` sizes the column from the same
  function, so a new badge is one edit.
- **The title takes width first; tags degrade.** `taskListCols` reserves at
  most `tagsReservePct` for tags (floored at `tagsOverflowMinW`). The tags cell
  is sized per row at render time (`renderRowTags` → `renderTaskTagsClipped`),
  dropping chips from the end behind a `+N` count.
- **No glyph a terminal may draw double-width.** Symbols with an emoji face
  (`⚠`, `⚡`, anything ≥ U+1F000) are out of the UI; arrows, box drawing, `✓`
  and `▶` are fine. `TestUIAvoidsGlyphsTerminalsDrawDoubleWide` scans the
  sources. User text is data and renders however the terminal draws it.
- **One ellipsis, one cell.** Clipped strings end in `ellipsis` (`…`).
  `truncate` counts runes; `truncateStyled`/`truncateLines` go through `ansi`,
  and `truncateLines` carries the marker too.
