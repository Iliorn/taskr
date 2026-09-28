# taskr

A fast, keyboard-driven task manager for the terminal — built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

[![CI](https://github.com/Iliorn/taskr/actions/workflows/ci.yml/badge.svg)](https://github.com/Iliorn/taskr/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat&logo=go)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=flat)
![License](https://img.shields.io/badge/license-MIT-green?style=flat)

---

taskr keeps your tasks in one fast list you drive entirely from the keyboard,
and tells you which one to do next. It runs on Linux, macOS and Windows,
keeps everything in a local file, and can sync between your machines through
a small server you run yourself — no account, no cloud service.

## What it does

- **Tells you what's next.** Tasks are ranked by deadline, priority, what
  you've been working on, size and age, and a task blocking something urgent
  rises with it. Press `w` on any task to see why it ranks where it does.
- **Tasks with the details you need** — due and start dates, priority, size,
  tags, projects, subtasks, dependencies, comments, notes and repeating tasks.
- **Quick entry.** `Buy milk #shopping due:friday p:high @home` sets it all in
  one line.
- **Several ways to look at the same tasks:** a calendar with tracked time,
  projects with a timeline, tags, a kanban board, and a stats page.
- **Time tracking** — start and stop a timer per task with `t`.
- **A daily reminder** — a desktop notification of what's due today and
  what's overdue.
- **Sync** between computers, **undo** for every change, a **command palette**
  (`ctrl+k`) so you never need to remember a key, and a **command line** for
  scripting.
- **In English, Danish or German**, with themes and rebindable keys.

## Install

| | |
|---|---|
| macOS | `brew install iliorn/tap/taskr` |
| Windows | `scoop install https://github.com/Iliorn/taskr/releases/latest/download/taskr.json` |
| Arch Linux | `yay -S taskr-bin` |
| Linux / Windows binary | download from [Releases](https://github.com/iliorn/taskr/releases) |
| Anywhere Go runs | `go install github.com/Iliorn/taskr@latest` |

Once installed, taskr updates itself: Settings → "Update to latest release".
[docs/install.md](docs/install.md) covers building from source and checking
a download.

## Getting started

Run `taskr`. Press `a` to add a task, `d` to mark it done, `enter` to open
it, `?` to see every key, and `ctrl+k` to find any action by name.

| Key | Action |
|-----|--------|
| `a` | Add task |
| `d` | Done |
| `enter` | Details (comments, subtasks, dependencies, notes) |
| `t` | Start/stop the timer |
| `D` | Set the due date |
| `/` | Search |
| `f` | Focus: only today and overdue |
| `u` | Undo |
| `tab` / `1–7` | Switch tab |
| `?` | All shortcuts |

When adding a task, `#tag`, `@project`, `due:friday`, `p:high` and `size:s`
fill in the details, and the same words work in search: `/` then
`@work overdue` shows your overdue work tasks. Dates can be `today`,
`tomorrow`, `monday`, `+3d`, `15-06-25` and more.

## Learn more

- [Using taskr](docs/guide.md) — every tab, search, the board, the daily
  reminder, and custom keys
- [Command line](docs/cli.md) — `taskr add`, `list`, `done`, JSON output,
  export and import
- [Sync between devices](docs/sync.md) — running a server and connecting
  your machines
- [Files and troubleshooting](docs/troubleshooting.md) — where data lives,
  backups, crashes, and slowness
- [Changelog](CHANGELOG.md) — what changed in each release

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building and testing, and
[ARCHITECTURE.md](ARCHITECTURE.md) for how taskr is put together.
Security issues: [SECURITY.md](SECURITY.md).

## License

MIT
