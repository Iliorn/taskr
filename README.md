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

## Data

Tasks are stored in `~/.taskr/tasks.db` (SQLite, WAL mode). On first launch any legacy `~/.taskr/tasks.json` is imported into the new database and then left in place as a backup.

## License

MIT
