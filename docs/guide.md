# Using tjek

The full tour of the terminal app. The [README](../README.md) has the short
version; the [CLI reference](cli.md) covers the `tjek <command>` side.

## The tabs

- **Tasks**: the main list. Add, complete, delete, rename, set priority,
  size (S/M/L), due and start dates. The detail pane (`enter`) holds comments,
  dependencies, subtasks, notes (opened in `$EDITOR`) and a live score
  breakdown.
- **Calendar**: a per-day activity timeline with project and tag roll-ups
  and a tracked-time heatmap. Time entries can be edited or deleted in place.
- **Projects**: tasks grouped by project, with a timeline when an open task
  has a date. `enter` walks into a project's tasks, where the task keys
  (`d` done, `t` track, `p` priority, `r` rename, `x` delete, `enter`
  details) all work; `a` adds a task already in that project, `x` on the
  project row clears the project from its tasks.
- **Tags**: the same, grouped by tag. Tags can be renamed, merged or deleted
  across every task; `f` shows a tag's tasks on the Tasks tab as a filter.
- **Board**: a kanban view; see [The board](#the-board).
- **Stats**: a productivity overview with an activity heatmap. It follows
  the active search, so `#tag` scopes every number to that tag.
- **Settings**: the sequencing knobs, theme, language (English, Dansk,
  Deutsch), the daily reminder, board columns, sync, and in-app update.

On Tags and Projects, `enter` walks in one level at a time (row, then its
tasks, then the selected task's detail) and `esc` walks back out the same way. Inside,
`→` unfolds a task's subtasks and `←` folds them, as on the Tasks tab.

## Subtasks

A subtask is a full task with a parent. `+` in front of a task means it has
subtasks folded away, `-` that they are showing; `→`/`←` unfold and fold them
on the Tasks tab and inside a tag or project. A new subtask starts with its
parent's project, deadline and tags, and never outranks its parent's
priority. Moving a parent to another project takes its subtasks along. If you
would rather tag each step yourself, turn off Settings → "Subtasks copy
tags".

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `a` | Add task |
| `d` | Toggle done |
| `t` | Start/stop time tracking |
| `D` | Set / clear the due date |
| `r` | Rename |
| `x` / `del` | Delete |
| `n` | Edit notes in `$EDITOR` |
| `f` | Focus mode (today + overdue) |
| `h` | Toggle history |
| `s` | Cycle sort: Sequence → Due → Size |
| `w` | Why this rank: the points behind the percentage and what moves it next |
| `/` | Search / filter |
| `enter` | Open detail view |
| `u` | Undo |
| `↑`/`↓` or `j`/`k` | Move the cursor |
| `tab` / `shift+tab` | Next / previous tab |
| `1–7` | Jump straight to a tab (7 = Settings) |
| `ctrl+k` | Command palette: find any action by name |
| `?` | Show all shortcuts (`/` filters them) |

## What to work on next

The **Sequence** sort ranks pending tasks by a weighted score: deadline,
priority, momentum (what you have been working on), size and age. The Score
column is a percentage of the top task, so 100% is what tjek thinks you
should do now. Tune the weights in Settings (Relaxed / Balanced / Intense per
dimension), and press `w` on any task to see the points behind its
percentage, their causes, the margins to the rows either side, and when the
ranking will move on its own.

Dependencies feed the ranking: a task that blocks others inherits their
urgency, so the prerequisite for an urgent task surfaces right above it. In
the list, `↥` marks a blocker and `↧` a task still waiting on one.

## Adding tasks quickly

```
Buy groceries #shopping due:friday p:high size:s @personal
```

The add field understands `#tag`, `@project`, `due:date`,
`p:high/medium/low` and `size:s/m/l`. Typing `#` or `@` offers your existing
tags and projects, most recently used first; `tab` inserts the highlighted
one, `↑/↓` pick another. Projects whose name contains a space aren't offered
there, since the field splits on spaces; set those from the detail pane's `@`
picker. Tags are lowercase, and spaces become `-` (`Deep Work` becomes
`#deep-work`).

Dates: `today` · `tomorrow` · `next week` · `monday` · `15-06-25` · `+3d` ·
`+2w` · `+1m` · `-2d` (counting back)

## Searching

`/` filters the list. Words are combined, and use the same vocabulary as
adding:

```
@work p:high due:<friday        # high-priority Work tasks due before Friday
#urgent overdue                 # overdue tasks tagged urgent
grcrs                           # finds "Buy groceries"
```

Supported: `#tag`, `@project`, `p:high/medium/low`, `due:<date`,
`due:>date`, `due:date` (`<=` and `>=` too) and the word `overdue`. Anything
else matches the title loosely (every letter in order, so `dply` finds
"Deploy release") or the notes as plain text.

## In your own language

With the interface set to Dansk or Deutsch, adding and searching accept that
language's words too, such as `frist:imorgen p:høj størrelse:lille` or
`fällig:freitag p:hoch überfällig`, and the hints show those spellings.
English always works. Only what you type is translated: tags, repeat rules
and everything else stored stays in English, so devices set to different
languages sync without trouble. The CLI is English throughout.

## The board

A kanban view of your pending tasks: one column per stage, and a last column
holding the completed ones. The default columns are Backlog / In progress /
Review / Done, and every name is yours to change in Settings → "Board
columns" (comma-separated). Renaming a column takes its cards with it.

The last column always means *done*, and its heading always carries a ✓:
calling it "Shipped" changes the name and nothing else. Moving a card into it
completes the task, and moving one out reopens it (after asking).

### Column icons

Give a column an icon by writing it in brackets before the name:

```
[B] Backlog, [>] In progress, [R] Review, Done
```

The icon leads the column's heading, and every task list shows it in the task's
status box, so `[R]` beside a task says it is in Review, on any tab. A column
without an icon leaves the box blank. The icon is one character: a letter, a
digit or a symbol. Emojis are two cells wide in a terminal and are refused, and
✓ is fixed on the last column, so no other column can have it. Once
any column has an icon, the box shows columns only; an overdue task still shows
red. Icons sync with the columns.

- `←/→` switch columns; `H`/`L` move the selected card between them.
- `enter` picks a card up, `←/→` carry it, `enter` or `esc` put it down.
- `a` adds a card to the focused column; space shows a card's details.
- `/` filters every column at once, with the same search as the Tasks tab.

With more columns than fit, the board scrolls sideways and its title says
which slice you are on (`Workflow ‹ 3–8/11 ›`); below three visible columns
it shows the stages as one stacked list instead. A task's stage can also be
changed on the detail pane's **Stage** row with `←/→`, or with
`tjek edit <ref> --stage <name>`. Not using kanban? Settings → "Kanban
board" hides the tab and the Stage row.

## The daily reminder

Once a day, at the time set in Settings → "Reminder time" (09:00 unless you
change it), tjek shows a desktop notification listing what is overdue and
what is due today. Settings → "Daily reminder" turns it off and on again,
keeping the time. Opening tjek after the reminder time counts as that day's
reminder, since the list on screen already says the same thing.

When the desktop can't show the pop-up (no notification service on a Linux
session over ssh, for example), the app still shows the reminder in its
status line, and `tjek remind --now` prints it with a line saying why the
pop-up was unavailable.

The app sends it while it is running. To be reminded when it isn't, have
your system run `tjek remind` every few minutes; it does nothing until the
time comes, and reminds only once a day however many times it runs.
`tjek remind --now` sends one straight away, which is a quick way to check
notifications work.

**Linux**: notifications need `notify-send` (Debian/Ubuntu:
`apt install libnotify-bin`). A systemd user timer keeps the reminder
running:

```ini
# ~/.config/systemd/user/tjek-remind.service
[Service]
Type=oneshot
ExecStart=%h/.local/bin/tjek remind

# ~/.config/systemd/user/tjek-remind.timer
[Timer]
OnCalendar=*:0/15
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
systemctl --user enable --now tjek-remind.timer
```

**macOS**: notifications use the built-in `osascript`; a launchd agent with
`StartInterval` 900 running `tjek remind` does the scheduling.

**Windows**: notifications appear under "tjek" in the notification
centre. tjek calls the Windows notification API itself rather than going
through PowerShell, so it works on work PCs where PowerShell is locked down.
The first one registers tjek for notifications under your own user
(`HKCU\Software\Classes\AppUserModelId\tjek`); no administrator rights are
needed. To schedule it:

```bat
schtasks /create /sc minute /mo 15 /tn "tjek remind" /tr "\"%LOCALAPPDATA%\Programs\tjek\tjek.exe\" remind"
```

(adjust the path to wherever `tjek.exe` lives). Each run opens a console
window for a moment; if that is a bother, leaving the app running does the
same job.

## Export and import

Settings → Export keeps a copy of all your tasks, finished ones included, in
a folder you choose. In a OneDrive or Dropbox folder it doubles as a backup,
and another tool can read it.

- **Auto-export folder**: press enter, type or paste the folder (`tab`
  completes folder names), and enter again. tjek writes
  `tjek-export.json` there straight away, then keeps it current: within a
  minute of a change, and again when you quit. Clear the path to turn it off.
- **Import from file**: press enter and give the path to a tjek export
  (`tab` completes). Its tasks are merged in: new ones are added, ones you
  already have take the newer version, and nothing is deleted, so importing
  the same file twice changes nothing. `u` takes the whole import back.

The file is the same one `tjek export --include-done` prints, so the
[command line](cli.md#export-and-import) reads and writes it too.

## Custom keybindings

Every binding has an action name, so rebinding one is a line in
`settings.json` (`tjek doctor` prints where that file is):

```json
{
  "keys": {
    "done": "D",
    "search": "s",
    "sort": "/"
  }
}
```

The action names are listed in the `?` overlay. A rebind moves the action:
the old key stops working, and the footer hints, the help overlay and the
command palette all show the new key. An entry that names an unknown action,
uses more than one key, or clashes with another binding in the same view is
ignored with a warning, and so is `ctrl+c`, which always quits. Bindings written
as a pair or range (`←/→`, `H/L`) can't be rebound.
