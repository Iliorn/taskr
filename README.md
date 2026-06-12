# taskr

A fast, keyboard-driven task manager for the terminal — built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat&logo=go)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=flat)
![License](https://img.shields.io/badge/license-MIT-green?style=flat)

---

## Features

- **Tasks** — add, complete, delete, rename, set priority, due dates, start dates
- **Projects** — group tasks, Gantt timeline view
- **Tags** — tag tasks, filter by tag, rename/delete globally
- **Learnings** — attach notes and learnings to tasks, browse them in a dedicated tab
- **Stats** — productivity overview with an activity heatmap
- **Time tracking** — start/stop a timer per task (`t`), live elapsed display, runaway-timer guard
- **Calendar** — per-day activity timeline with project/tag roll-ups and a tracked-time heatmap; edit or delete entries in place
- **Detail view** — per-task comments, dependencies, subtasks, notes (opens `$EDITOR`)
- **Search** — live filter across tasks, projects, tags and learnings
- **Undo** — multi-level undo for all mutations
- **Self-update** — press `U` to pull the latest release in-place

## Installation

**From source:**

```sh
git clone https://github.com/luciphere/taskr
cd taskr
go mod tidy
go build -o taskr .
mv taskr ~/.local/bin/   # or anywhere on your PATH
```

**Pre-built binary** (Linux / Windows / macOS):

Download the latest release from the [Releases](https://github.com/luciphere/taskr/releases) page — `taskr` for Linux, `taskr.exe` for Windows (x64), `taskr-macos-arm64` (Apple Silicon) or `taskr-macos-amd64` (Intel) for macOS.

On macOS, run `chmod +x taskr-macos-*` after downloading; if Gatekeeper blocks it, clear the quarantine flag with `xattr -d com.apple.quarantine taskr-macos-*`.

On Windows, notes editing uses `EDITOR` if set (`setx EDITOR hx`), falling back to notepad. Self-update (`U`) requires the [GitHub CLI](https://cli.github.com/) on all platforms.

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
| `s` | Cycle sort order |
| `/` | Search / filter |
| `enter` | Open detail view |
| `u` | Undo |
| `U` | Self-update |
| `tab` / `1–6` | Switch tabs |
| `?` | Show all shortcuts |

### Quick-add syntax

```
Buy groceries #shopping due:friday p:high @personal
```

Supports `#tag`, `due:date`, `p:high/medium/low`, `@project` inline when adding a task.

### Date formats

`today` · `tomorrow` · `next week` · `monday` · `15-06-25` · `+3d` · `+2w` · `+1m`

## Data

Tasks are stored in `~/.taskr/tasks.json`. A backup is kept at `~/.taskr/tasks.json.bak`.

## License

MIT
