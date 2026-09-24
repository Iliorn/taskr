package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Iliorn/taskr/todo"
)

// cli.go is the non-TUI surface: a small set of subcommands sharing the same
// SQLite store as the Bubble Tea app. Designed to be scriptable (--json on
// every list-shaped command, exit codes 0=ok, 1=runtime, 2=usage) without
// pulling in a CLI framework — the standard `flag` package is enough.
//
// Stream contract: stdout carries only a command's primary result (the thing
// a pipe or $(...) capture wants); side-effect notices — another timer being
// stopped, ancestor due dates bumped, recovery hints — go to stderr.

// isCLICommand reports whether the first arg names a subcommand main should
// route to runCLI instead of launching the TUI.
func isCLICommand(arg string) bool {
	switch arg {
	case "add", "list", "ls", "done", "reopen", "top",
		"show", "why", "edit", "delete", "rm", "undelete", "comment",
		"stats", "start", "stop", "log", "export", "import", "subtask",
		"search", "tags", "projects", "serve", "sync", "undo",
		"doctor", "update", "suggest", "completion", "man", "help", "-h", "--help", "--version",
		// Retired, but still routed so muscle memory gets an explanation
		// instead of the TUI opening on top of the typed command.
		"learnings":
		return true
	}
	return false
}

func runCLI(args []string) int {
	// Recover any timer a prior (possibly crashed) session left running, before
	// running the command, so the warning rides along with this invocation.
	reconcileStaleTimersCLI(args[0])
	rc := dispatchCLI(args)
	// After a successful mutating command, push the change to the sync server so
	// a shell edit propagates even when the TUI isn't open. Fail-soft and gated
	// on sync being configured.
	if rc == 0 && cliMutates(args[0]) {
		maybeAutoSyncCLI()
	}
	return rc
}

// cliMutates reports whether a subcommand changes stored tasks (and so should
// trigger an auto-sync afterward).
func cliMutates(cmd string) bool {
	switch cmd {
	case "add", "done", "reopen", "edit", "delete", "rm", "undelete", "comment", "start", "stop", "log", "subtask", "undo", "suggest", "import":
		return true
	}
	return false
}

func dispatchCLI(args []string) int {
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		return cliAdd(rest)
	case "list", "ls":
		return cliList(rest)
	case "done":
		return cliDone(rest)
	case "reopen":
		return cliReopen(rest)
	case "top":
		return cliTop(rest)
	case "show":
		return cliShow(rest)
	case "why":
		return cliWhy(rest)
	case "edit":
		return cliEdit(rest)
	case "delete", "rm":
		return cliDelete(rest)
	case "undelete":
		return cliUndelete(rest)
	case "comment":
		return cliComment(rest)
	case "stats":
		return cliStats(rest)
	case "start":
		return cliStart(rest)
	case "stop":
		return cliStop(rest)
	case "log":
		return cliLog(rest)
	case "undo":
		return cliUndo(rest)
	case "export":
		return cliExport(rest)
	case "import":
		return cliImport(rest)
	case "subtask":
		return cliSubtask(rest)
	case "search":
		return cliSearch(rest)
	case "tags":
		return cliTags(rest)
	case "projects":
		return cliProjects(rest)
	case "serve":
		return cliServe(rest)
	case "sync":
		return cliSync(rest)
	case "learnings":
		fmt.Fprintln(os.Stderr, "taskr learnings: removed — learnings were folded into each task's notes.")
		fmt.Fprintln(os.Stderr, "Search them with: taskr search \"Learnings\"  (notes are searched too), or open the task and press n.")
		return 2
	case "completion":
		return cliCompletion(rest)
	case "man":
		return cliMan(rest)
	case "update":
		return cliUpdate(rest)
	case "doctor":
		return cliDoctor(rest)
	case "suggest":
		return cliSuggest(rest)
	case "--version":
		fmt.Println(appVersion)
		return 0
	default: // help, -h, --help
		return cliHelp()
	}
}

// ── help ─────────────────────────────────────────────────────────────────────

// cliHelp prints the command reference. It goes to stdout, not stderr:
// dispatchCLI only reaches it for an explicit `taskr help` / `-h` / `--help`
// (an unrecognised word never gets this far — isCLICommand sends it to the
// TUI instead), and an explicitly requested document belongs on stdout so
// `taskr help | grep sync` and `taskr help | less` work.

// ── update ───────────────────────────────────────────────────────────────────

// cliUpdate is the shell-side door to the same self-update the Settings tab
// offers. The verdict comes from planUpdate, shared with the TUI so a binary cannot be
// told two different things about itself; only the sentences are local, since
// CLI output is deliberately English (see lang.go).
func cliUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	check := fs.Bool("check", false, "report the running and latest version, install nothing")
	yes := fs.Bool("y", false, "install without the confirmation prompt")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: taskr update [--check] [-y]   install the latest release (macOS and package-managed installs are told which tool to use instead)")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	latest, err := latestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr update: %v\n", err)
		return 1
	}

	action, hint := planUpdate(appVersion, latest)
	switch action {
	case updateUpToDate:
		fmt.Printf("%s is the latest release\n", appVersion)
		return 0
	case updateLocalBuild:
		fmt.Printf("latest release %s (running %s — a local build, left alone)\n", latest, appVersion)
		return 0
	case updateManaged:
		fmt.Printf("update available: %s (running %s)\n", latest, appVersion)
		fmt.Fprintf(os.Stderr, "this install is package-managed; update it with `%s`\n", hint)
		return 1
	}

	fmt.Printf("update available: %s (running %s)\n", latest, appVersion)
	if *check {
		return 0
	}
	// A prompt nobody can answer is just a failure with extra steps, so a
	// non-interactive run says what flag would have carried it through rather
	// than reading EOF as "no".
	if !*yes {
		if !stdinIsTTY() {
			fmt.Fprintln(os.Stderr, "taskr update: not a terminal — rerun with -y to install without confirming")
			return 1
		}
		if !confirmStdin(fmt.Sprintf("install %s over the running binary?", latest)) {
			fmt.Fprintln(os.Stderr, "aborted: nothing installed")
			return 1
		}
	}
	if err := selfUpdate(); err != nil {
		fmt.Fprintf(os.Stderr, "taskr update: %v\n", err)
		return 1
	}
	// The running process keeps executing the old image either way (the file is
	// renamed out from under it on Unix, moved aside on Windows), so say so.
	fmt.Printf("installed %s — restart taskr to run it\n", latest)
	return 0
}

func cliHelp() int {
	fmt.Fprintln(os.Stdout, `taskr — keyboard-driven task manager

Usage:
  taskr                                launch the TUI (no args)

Tasks:
  taskr add "title" [flags]            add a new task (--like <ref> clones, --depends <ref>|^ blocks on, --start tracks)
  taskr add -                          batch add: one task per stdin line (flags apply to all; --chain links each
                                       line as depending on the previous — a plan typed in execution order)
  taskr list [flags]                   list pending top-level tasks (ST: [ ] ready, [>] in progress, [!] overdue, [✓] done)
                                       review filters: --stale=30d (untouched that long), --unblocked-since=14d
                                       (every blocker now done, the last one recently), --sort=seq|due|size|age|idle|pri,
                                       --wide (AGE + IDLE columns), --search-word / --search-re
  taskr search "term" [flags]          title/notes substring search (includes done by default; --word matches
                                       whole words only, --re treats the term as a regular expression)
  taskr top [-n=N] [--json] [--wide]   show top-N by sequence score
  taskr show <ref> [--json]            full detail (incl. score breakdown + subtask IDs)
  taskr why <ref> [--json]             why it ranks where it does: each score factor with its cause,
                                       the margins to the tasks either side, and when the ranking
                                       moves on its own (deadline steps, momentum expiring)
  taskr edit <ref>... [flags]          change fields on one or more tasks (incl. --note/--append-note/--clear-note,
                                       --stage to move it on the board — stage names live in settings.json;
                                       --title takes a single ref)
  taskr done <ref>... [-m "why"]       mark one or more tasks done, stopping any running timer on them
                                       (--cascade also closes pending subtasks; without it a parent with
                                       open subtasks prompts on a TTY, else warns and leaves them open)
                                       (-m/--comment adds a closing comment to each)
  taskr reopen <ref>... [-m "why"]     move tasks back to pending (the counterpart to done; already-pending
                                       tasks are reported and skipped)
  taskr delete <ref> [-f]              soft-delete a task (alias: rm; substring matches confirm first)
  taskr undo [--list]                  restore the most recent deletion (task + subtasks)
  taskr undelete <ref> | --list        restore a specific deleted task by ref (browse with --list)
  taskr subtask <parent> "title"       create a subtask (--each for multiple titles)

Shell integration:
  taskr completion bash|zsh|fish       print a completion script (install paths in the man page)
  taskr man                            print the man page in roff (e.g. > ~/.local/share/man/man1/taskr.1)

Discovery:
  taskr tags [--json]                  pending tags with counts
  taskr projects [--json]              pending projects with counts
  taskr suggest [--list]               suggest dependency links from note refs + related titles (interactive)

Tracking:
  taskr start <ref>                    start the time tracker, stopping any other task's timer first
                                       (no-op if already tracking ref)
  taskr stop [<ref>]                   stop the tracker (no ref = whichever's running)
  taskr log <ref> <45m|10:00-11:30>    backfill a time entry (duration ends now; range is today)

Comments:
  taskr comment <ref> "text"           append a comment
  taskr comment <ref> -                read comment text from stdin (for long/heredoc input)
  taskr comment <ref> --edit=N "text"  edit comment N (1-based)
  taskr comment <ref> --delete=N       delete comment N

Diagnostics:
  taskr doctor [--json]                report this installation's health — version, data directory,
                                       database integrity, schema version, settings, sync and editor
                                       (paste the output into a bug report; exits non-zero on a problem)
  taskr update [--check] [-y]          install the latest release, verified against the release's SHA256SUMS
                                       (--check only reports; macOS and package-managed installs are pointed
                                       at brew/scoop/the distro instead of being overwritten)

Reporting / backup:
  taskr stats [--format=text|json|waybar]   one-line health summary (default text)
                                            (--tag/--project/--search scope the stats to matching tasks;
                                             --seq appends the sequence miss analysis: which score dimension
                                             buried the tasks you finished anyway, and a bias hint)
  taskr export [--include-done]             JSON snapshot (versioned envelope) to stdout
  taskr import <file>|-                     merge an export file into the local store (- = stdin)

Sync (cross-device):
  taskr serve [--listen=ADDR] [--token=T]   run the sync server (self-hosted; binds 127.0.0.1:8765 by default)
    [--tls-cert=F --tls-key=F]              serve https with this PEM pair (re-read when the files are renewed)
  taskr sync [--url=U] [--token=T] [--save]  push/pull once against a sync server (--save stores config)
                                            auto-sync runs on its own once configured (set "auto_sync":false in
                                            ~/.taskr/sync.json to disable); conflicts log to ~/.taskr/sync.log
  taskr sync --status                        print the last sync time/result (local only, no network)
  taskr sync --accept-stale                  rejoin after being offline past the deletion-memory window
                                            (~6 months; BOTH auto-sync and a manual "taskr sync" refuse until
                                            then, so tasks deleted elsewhere can't resurrect)
  taskr sync --adopt-local                   first sync only: keep this device's tasks and push them to the fleet
  taskr sync --adopt-remote                  first sync only: back them up, clear them here, pull the fleet's list
                                            (a device that has never synced and holds tasks of its own refuses to
                                             sync until one of these is given, so its old tasks can't land on every
                                             device by surprise; the backup is a normal export, taskr import undoes it)
  taskr sync --recover                       list dropped edits from ~/.taskr/sync.log (local only, no network)
  taskr sync --recover=<ref>                 reapply one dropped edit by id-prefix or title substring;
                                            stamps a fresh ModifiedAt so the fix propagates on the next sync

Meta:
  taskr --version                      print build version
  taskr help                           this message

Task references can be a UUID prefix (`+"`347e`"+`) OR a case-insensitive
substring of the title (`+"`milk`"+`). ID-prefix wins on hex-shaped queries
so scripts stay deterministic. Ambiguous refs fail with exit code 2 and
list each match with its short ID.

Flags (add):
  --due=DATE           today|tomorrow|+3d|dd-mm-yy|monday|...
  --p=h|m|l            priority (default m, or copied from --like)
  --priority=h|m|l     priority (alias for --p)
  --size=s|m|l         task size (default m, or copied from --like)
  --project=NAME       project
  --tag=t1,t2          comma-separated tags
  --like=REF           clone priority/size/project/tags from existing task (flags above override)
  --depends=REF        block the new task on an existing task (^ = last-added); echoed on success
  --note=TEXT|-        set the notes field (freeform body; '-' reads from stdin)
  --comment=TEXT       add an initial timestamped comment
  --start              start the time tracker on the new task (stops any other running timer first)
  --json               emit the created task as JSON (includes its id)
  --quiet-id           print only the new task's full id (for scripting)

Flags (list / search):
  --json          emit JSON
  --all           include completed tasks (list only; search includes by default)
  --pending       exclude completed (search only; inverts default)
  --focus         only today + overdue (list only)
  --ready         only actionable tasks — ST [ ] (no unfinished dependencies; list only)
  --blocked       only tasks waiting on an unfinished dependency (these sort last; list only)
  --tag=NAME      only tasks carrying this tag
  --project=NAME  only tasks in this project
  --search=TERM   only tasks whose title contains TERM (list; redundant with 'search' verb)
  --limit=N       cap rows

Flags (top):
  --n=N           rows to show (default 10)
  --json          emit JSON (includes tags, priority, due)
  --wide          table with priority, due, tags columns

Flags (edit):
  --title=...          new title
  --p=h|m|l            new priority
  --priority=h|m|l     new priority (alias for --p)
  --size=s|m|l         new size
  --due=DATE      set due date         --clear-due       drop due date
  --start=DATE    set start date       --clear-start     drop start date
  --project=NAME  set project          --clear-project   drop project
  --add-tag=t1,t2     append tags
  --remove-tag=t1,t2  remove tags
  --add-dep=REF       add a dependency (refused if it would loop)
  --remove-dep=REF    remove a dependency

Notes:
  - Data lives at ~/.taskr/tasks.db (shared with the TUI). Concurrent CLI +
    TUI usage is safe for reads; writes serialize via SQLite's busy-timeout.
    A running TUI live-reloads on external writes via a filesystem watcher,
    so CLI changes appear without restarting it.
  - The sequencing engine's biases (Deadline/Priority/Momentum) are loaded
    from ~/.taskr/settings.json, so 'top' and 'list' rank the same way as
    the TUI under the user's current bias settings.`)
	return 0
}

// ── shared helpers ───────────────────────────────────────────────────────────

// splitFlagsAndPositionals separates a CLI subcommand's argv into a flag-only
// slice (safe to pass to flag.Parse) and a positional-only slice (the title,
// etc). Go's stdlib flag package stops at the first non-flag token, so without
// this helper users would have to write `taskr add --p=h "Buy milk"` instead
// of the more natural `taskr add "Buy milk" --p=h`.
//
// Which flags consume the next arg when written without an embedded `=`
// (e.g. `--due tomorrow`) is derived from fs itself: every registered flag
// whose Value does not implement the stdlib's boolFlag interface takes a
// value, so there is no separate list to drift from the FlagSet. Callers must therefore define all flags on fs BEFORE calling this.
func splitFlagsAndPositionals(fs *flag.FlagSet, args []string) (flags, positionals []string) {
	valueFlags := make(map[string]bool)
	fs.VisitAll(func(f *flag.Flag) {
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
			valueFlags[f.Name] = true
		}
	})
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			// POSIX end-of-flags marker — everything after is positional.
			positionals = append(positionals, args[i+1:]...)
			return
		case a == "-":
			// Bare single dash is conventional stdin / "this position", not a
			// flag. Without this, `taskr comment <ref> -` would route the dash
			// into flag parsing and lose it before reaching the stdin reader.
			positionals = append(positionals, a)
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue // self-contained --key=value
			}
			if valueFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positionals = append(positionals, a)
		}
	}
	return
}

func parsePriorityFlag(s string) todo.Priority {
	switch strings.ToLower(s) {
	case "h", "high":
		return todo.PriorityHigh
	case "l", "low":
		return todo.PriorityLow
	default:
		return todo.PriorityMedium
	}
}

func parseSizeFlag(s string) todo.Size {
	switch strings.ToLower(s) {
	case "s", "small":
		return todo.SizeSmall
	case "l", "large":
		return todo.SizeLarge
	default:
		return todo.SizeMedium
	}
}

func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func priorityLetter(p todo.Priority) string {
	switch p {
	case todo.PriorityHigh:
		return "H"
	case todo.PriorityLow:
		return "L"
	default:
		return "M"
	}
}

// printTaskTable renders rows as a fixed-column table. blocked marks the tasks
// waiting on an unfinished dependency: they sort last, and carry a ↧ before the
// title saying why they are down there.
func printTaskTable(rows []todo.Todo, blocked map[string]bool) {
	printTaskTableWide(rows, blocked, false)
}

// printTaskTableWide is printTaskTable with the two time columns --wide adds:
// AGE (days since the task was created) and IDLE (days since it last changed).
// They are opt-in rather than always shown because they are review columns, not
// working ones — a daily `taskr list` doesn't need them, and every column costs
// width the title would otherwise have.
func printTaskTableWide(rows []todo.Todo, blocked map[string]bool, wide bool) {
	if len(rows) == 0 {
		fmt.Println("(no tasks)")
		return
	}
	now := time.Now()
	// Whole days, floored: "how long has this been sitting" is a question
	// about days, and rounding up would report a task edited an hour ago as
	// one day idle.
	days := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		d := int(now.Sub(t).Hours() / 24)
		if d < 0 {
			d = 0
		}
		return strconv.Itoa(d) + "d"
	}
	// Adaptive PROJ column: shown only when at least one row has a project,
	// width hugging the widest entry (capped at 15 so a long project name
	// can't crowd out the title). Omitted entirely when nothing has a
	// project, so the layout matches the pre-2026-06-18 output for
	// projectless boards.
	projW := 0
	for _, t := range rows {
		if w := len(t.Project); w > projW {
			projW = w
		}
	}
	if projW > 15 {
		projW = 15
	}
	if projW > 0 && projW < len("PROJ") {
		projW = len("PROJ")
	}
	// The time columns sit between DUE and PROJ: the row then reads
	// identifier, state, then everything time-shaped, then the labels.
	timeHdr, timeFmt := "", ""
	if wide {
		timeHdr = fmt.Sprintf("%-5s  %-5s  ", "AGE", "IDLE")
		timeFmt = "%-5s  %-5s  "
	}
	if projW > 0 {
		fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s%-*s  %s\n",
			"ID", "ST", "SIZE", "PRI", "DUE", timeHdr, projW, "PROJ", "TITLE")
	} else {
		fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s%s\n", "ID", "ST", "SIZE", "PRI", "DUE", timeHdr, "TITLE")
	}
	for _, t := range rows {
		// Same four states as the TUI's status column, same precedence: one
		// fact about where the task stands, with overdue ahead of started.
		// Blocked is not among them — these rows are already sorted last.
		st := "[ ]"
		switch {
		case t.Status == todo.Done:
			st = "[✓]"
		case t.IsOverdue():
			st = "[!]"
		case len(t.TimeEntries) > 0:
			st = "[>]"
		}
		due := ""
		if !t.DueDate.IsZero() {
			due = t.DueDate.Format("02-01-06")
		}
		// Lowercase the size letter to match the TUI list column — uppercase
		// "M" looked like a hotkey hint.
		sz := strings.ToLower(t.Size.Letter())
		// Before the title, as in the TUI: it answers "can I pick this up?",
		// which is asked before the title has been read.
		title := t.Title
		if blocked[t.ID] {
			title = "↧ " + title
		}
		times := ""
		if wide {
			times = fmt.Sprintf(timeFmt, days(t.CreatedAt), days(t.ModifiedAt))
		}
		if projW > 0 {
			fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s%-*s  %s\n",
				t.ID[:8], st, sz, priorityLetter(t.Priority), due, times, projW, truncate(t.Project, projW), title)
		} else {
			fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s%s\n",
				t.ID[:8], st, sz, priorityLetter(t.Priority), due, times, title)
		}
	}
}

// noteFlagText resolves a --note/--append-note flag value: the "-" sentinel
// reads the whole of r (piped or heredoc input for long notes), anything else
// is taken literally. Trailing newlines are trimmed like comment stdin input.
func noteFlagText(v string, r io.Reader) (string, error) {
	if v != "-" {
		return v, nil
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}
