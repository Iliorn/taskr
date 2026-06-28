# taskr

A fast, keyboard-driven task manager for the terminal — built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

[![CI](https://github.com/Iliorn/taskr/actions/workflows/ci.yml/badge.svg)](https://github.com/Iliorn/taskr/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat&logo=go)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=flat)
![License](https://img.shields.io/badge/license-MIT-green?style=flat)

---

## Features

- **Tasks** — add, complete, delete, rename, set priority, size (S/M/L), due dates, start dates
- **Sequencing engine** — a weighted score (deadline + priority + size + age) decides the next-best task automatically; cycle `s` to switch between Sequence / Due / Size sort. Tune the weights in Settings (Relaxed / Balanced / Intense for each dimension)
- **Calendar** — per-day activity timeline with project/tag roll-ups and a tracked-time heatmap; edit or delete entries in place
- **Projects** — group tasks, Gantt timeline view
- **Tags** — tag tasks, filter by tag, rename/delete globally
- **Learnings** — attach notes and learnings to tasks, browse them in a dedicated tab
- **Stats** — productivity overview with an activity heatmap
- **Time tracking** — start/stop a timer per task (`t`), live elapsed display, runaway-timer guard
- **Detail view** — per-task comments, dependencies, subtasks, notes (opens `$EDITOR`), plus a live score breakdown so you can see why a task ranks where it does
- **Search** — live filter across tasks, projects, tags and learnings
- **Undo** — multi-level undo for all mutations
- **Settings** — three sequencing-bias knobs, theme, language, version, in-app self-update (tab 7)

## Installation

**From source:**

```sh
git clone https://github.com/iliorn/taskr
cd taskr
go mod tidy
go build -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)" -o taskr .
mv taskr ~/.local/bin/   # or anywhere on your PATH
```

**Pre-built binary** (Linux / Windows / macOS):

Download the latest release from the [Releases](https://github.com/iliorn/taskr/releases) page — `taskr` for Linux, `taskr.exe` for Windows (x64), and for macOS pick `taskr-macos-apple-silicon` (Apple Silicon — M1/M2/M3/M4, i.e. any Mac from 2020 onward) or `taskr-macos-intel` (older Intel-based Macs).

On macOS, run `chmod +x taskr-macos-*` after downloading; if Gatekeeper blocks it, clear the quarantine flag with `xattr -d com.apple.quarantine taskr-macos-*`.

On Windows, notes editing uses `EDITOR` if set (`setx EDITOR hx`), falling back to notepad. Self-update (Settings tab → "Update to latest release") requires the [GitHub CLI](https://cli.github.com/) on all platforms.

## Usage

```sh
taskr
```

### Keyboard shortcuts

| Key | Action |
|-----|--------|
| `a` | Add task |
| `d` | Toggle done |
| `t` | Start/stop time tracking |
| `r` | Rename |
| `x` / `del` | Delete |
| `n` | Edit notes in `$EDITOR` |
| `f` | Focus mode (today + overdue) |
| `h` | Toggle history |
| `s` | Cycle sort: Sequence → Due → Size |
| `/` | Search / filter |
| `enter` | Open detail view |
| `u` | Undo |
| `tab` / `1–7` | Switch tabs (7 = Settings) |
| `?` | Show all shortcuts |

### Quick-add syntax

```
Buy groceries #shopping due:friday p:high size:s @personal
```

Supports `#tag`, `due:date`, `p:high/medium/low`, `size:s/m/l`, `@project` inline when adding a task.

### Date formats

`today` · `tomorrow` · `next week` · `monday` · `15-06-25` · `+3d` · `+2w` · `+1m`

## CLI

`taskr` ships with a small command-line surface for scripting. Bare `taskr` still launches the TUI; pass a subcommand to drop into CLI mode:

```sh
taskr add "Buy milk" --size=s --due=tomorrow --p=high --tag=shopping
taskr list                       # pending top-level tasks (table)
taskr list --json --focus        # JSON, today + overdue only
taskr top -n=5                   # top 5 by sequence score
taskr show milk                  # full detail (incl. score breakdown + subtask IDs)
taskr edit milk --p=high --add-tag=urgent --due=tomorrow
taskr done milk                  # mark a task done
taskr delete milk                # soft delete (alias: taskr rm)
taskr subtask milk "find receipt"   # create a subtask of "milk"
taskr start milk                 # start the time tracker
taskr stop                       # stop the running tracker (no ref needed)
taskr comment milk "blocked on review"
taskr comment milk --edit=1 "still blocked, asked Sam"
taskr comment milk --delete=2
taskr stats                      # one-line summary
taskr stats --format=waybar      # Waybar-shaped JSON for a status-bar widget
taskr export > backup.json       # JSON snapshot of every live task
taskr help
```

**Task references** can be either a UUID prefix (`60b9`) or a case-insensitive substring of the title (`milk`). ID-prefix takes precedence so scripts stay deterministic. Ambiguous references fail with exit code 2 and list every match with its short ID for easy disambiguation:

```
$ taskr done milk
title "milk" matches 2 tasks:
    21a164e1  Buy milk
    2ffe832a  Buy more milk
```

Flags can appear before or after the reference. `taskr top --json` and `taskr show --json` are the recommended hooks for scripts and other tools. The CLI reads the same `~/.taskr/settings.json` as the TUI, so ranking matches your current bias personality.

The TUI and CLI share the SQLite store. Concurrent reads are safe; writes serialize via SQLite's busy-timeout. A running TUI watches `~/.taskr` and live-reloads when the CLI (or a sync from another device) mutates the database, so scripted changes show up without restarting — a reload is briefly deferred while you're mid-edit so it can't clobber in-flight input.

## Data

Tasks are stored in `~/.taskr/tasks.db` (SQLite, WAL mode). On first launch any legacy `~/.taskr/tasks.json` is imported into the new database and then left in place as a backup.

## Sync

taskr can sync tasks across devices through a small **self-hosted** server — one
authoritative merge point, no third-party service. The same binary is both client
and server.

### Run a server

On the machine that should host the canonical store (e.g. a home server reachable
over Tailscale/LAN):

```sh
taskr serve --listen 100.x.y.z:8765 --token "$(openssl rand -hex 32)"
# or: TASKR_SYNC_TOKEN=… taskr serve --listen 100.x.y.z:8765
```

A token is **mandatory** — taskr refuses to run unauthenticated. `--listen`
defaults to `127.0.0.1:8765`; bind to a Tailscale/LAN address (or put it behind a
reverse proxy for TLS) to reach it from other devices. The server persists to its
own `~/.taskr/tasks.db` and exposes:

- `POST /v1/sync` — full-snapshot sync (Bearer token)
- `GET  /v1/health` — liveness check
- `GET  /v1/events` — Server-Sent Events "doorbell" so clients pull in real time

To keep it running, wrap it in a `systemd --user` unit with the token in an
`EnvironmentFile` (mode 600) and enable lingering.

### Point a client at it

```sh
taskr sync --url http://100.x.y.z:8765 --token "<token>" --save
```

`--save` writes the URL + token to `~/.taskr/sync.json` so future syncs need no
flags; `TASKR_SYNC_URL` / `TASKR_SYNC_TOKEN` work too. Once configured, the TUI
auto-syncs (on launch/exit, on a periodic tick, and live via SSE), and CLI
mutations sync best-effort in the background. Set `"auto_sync": false` in
`sync.json` to require manual `taskr sync`. The local SQLite store is always the
source of truth — network failures never block the UI.

You can also manage all of this from the **Settings tab**: toggle auto-sync, edit
the server URL/token inline (token masked), run "Sync now", and (v1.17+) flip the
local instance into server mode.

### How merge works

Sync is UUID-keyed with last-writer-wins on scalars (by `ModifiedAt`), union of
child collections (comments/learnings/time-entries) by UUID, and soft-delete
tombstones so a deletion propagates instead of the row reappearing. Edit-vs-delete
conflicts surface as a brief toast, and the losing version is appended to
`~/.taskr/sync.log` for recovery. Clock-based LWW assumes roughly synced clocks
(NTP); only tasks sync, not `settings.json`.

## License

MIT
