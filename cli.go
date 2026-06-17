package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"taskr/todo"
)

// cli.go is the non-TUI surface: a small set of subcommands sharing the same
// SQLite store as the Bubble Tea app. Designed to be scriptable (--json on
// every list-shaped command, exit codes 0=ok, 1=runtime, 2=usage) without
// pulling in a CLI framework — the standard `flag` package is enough.

// isCLICommand reports whether the first arg names a subcommand main should
// route to runCLI instead of launching the TUI.
func isCLICommand(arg string) bool {
	switch arg {
	case "add", "list", "ls", "done", "top", "help", "-h", "--help", "--version":
		return true
	}
	return false
}

func runCLI(args []string) int {
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "add":
		return cliAdd(rest)
	case "list", "ls":
		return cliList(rest)
	case "done":
		return cliDone(rest)
	case "top":
		return cliTop(rest)
	case "--version":
		fmt.Println(appVersion)
		return 0
	default: // help, -h, --help
		return cliHelp()
	}
}

// loadForCLI opens the store with the user's persisted biases applied so any
// score-based output ranks the same way the TUI would.
func loadForCLI() (Repository, []todo.Todo, error) {
	applyBiases(biasesFromSettings(loadSettings()))
	repo := newSQLiteRepo()
	todos, err := repo.Load()
	return repo, todos, err
}

// findByPrefix returns the single task whose ID begins with prefix, or an
// error if zero or multiple tasks match. The prefix is case-insensitive so
// it works regardless of how UUIDs were displayed elsewhere.
func findByPrefix(todos []todo.Todo, prefix string) (*todo.Todo, error) {
	if prefix == "" {
		return nil, fmt.Errorf("empty id prefix")
	}
	p := strings.ToLower(prefix)
	var matches []int
	for i := range todos {
		if strings.HasPrefix(strings.ToLower(todos[i].ID), p) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no task matches id prefix %q", prefix)
	case 1:
		return &todos[matches[0]], nil
	default:
		short := make([]string, len(matches))
		for i, m := range matches {
			short[i] = todos[m].ID[:8]
		}
		return nil, fmt.Errorf("prefix %q matches %d tasks: %s", prefix, len(matches), strings.Join(short, ", "))
	}
}

// ── add ──────────────────────────────────────────────────────────────────────

// addValueFlags lists the value-taking flags `add` accepts. splitFlagsAndPositionals
// uses this to know which flags consume the next arg (vs. being self-contained
// `--name=value`), so users can put the title in any position.
var addValueFlags = map[string]bool{
	"due": true, "p": true, "size": true, "project": true, "tag": true,
}

func cliAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	due := fs.String("due", "", "due date (today|tomorrow|+3d|dd-mm-yy|...)")
	priority := fs.String("p", "m", "priority: h|m|l")
	size := fs.String("size", "m", "size: s|m|l")
	project := fs.String("project", "", "project name")
	tags := fs.String("tag", "", "comma-separated tags")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: taskr add \"title\" [flags]")
		fs.PrintDefaults()
	}
	flagArgs, titleParts := splitFlagsAndPositionals(args, addValueFlags)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(titleParts) == 0 {
		fmt.Fprintln(os.Stderr, "taskr add: title required")
		return 2
	}
	t := todo.New(strings.Join(titleParts, " "))
	t.Priority = parsePriorityFlag(*priority)
	t.Size = parseSizeFlag(*size)
	if *due != "" {
		d, err := parseDueDate(*due)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid due date %q: %v\n", *due, err)
			return 2
		}
		t.DueDate = d
	}
	if *project != "" {
		t.Project = *project
	}
	if *tags != "" {
		for _, tag := range strings.Split(*tags, ",") {
			t.AddTag(tag)
		}
	}
	applyBiases(biasesFromSettings(loadSettings()))
	repo := newSQLiteRepo()
	if err := repo.Save([]*todo.Todo{&t}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("added %s  %s\n", t.ID[:8], t.Title)
	return 0
}

// ── list ─────────────────────────────────────────────────────────────────────

func cliList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	all := fs.Bool("all", false, "include completed tasks")
	focus := fs.Bool("focus", false, "only today + overdue")
	limit := fs.Int("limit", 0, "cap rows (0 = no cap)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	rows := make([]todo.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ParentID != "" {
			continue
		}
		if !*all && t.Status != todo.Pending {
			continue
		}
		if *focus && !(t.IsOverdue() || t.IsDueToday()) {
			continue
		}
		rows = append(rows, t)
	}
	sortTodosByMode(rows, taskSortSequence)
	if *limit > 0 && len(rows) > *limit {
		rows = rows[:*limit]
	}
	if *asJSON {
		return emitJSON(rows)
	}
	printTaskTable(rows)
	return 0
}

// ── done ─────────────────────────────────────────────────────────────────────

func cliDone(args []string) int {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr done <id-prefix>")
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	t, err := findByPrefix(todos, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if t.Status == todo.Done {
		fmt.Fprintf(os.Stderr, "already done: %s\n", t.Title)
		return 0
	}
	t.Toggle()
	if err := repo.Save([]*todo.Todo{t}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("done  %s  %s\n", t.ID[:8], t.Title)
	return 0
}

// ── top ──────────────────────────────────────────────────────────────────────

func cliTop(args []string) int {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	n := fs.Int("n", 10, "rows to show")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	rows := make([]todo.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ParentID == "" && t.Status == todo.Pending {
			rows = append(rows, t)
		}
	}
	sortTodosByMode(rows, taskSortSequence)
	if *n > 0 && len(rows) > *n {
		rows = rows[:*n]
	}
	if *asJSON {
		type scoredOut struct {
			ID    string  `json:"id"`
			Title string  `json:"title"`
			Score float64 `json:"score"`
			Due   string  `json:"due,omitempty"`
		}
		out := make([]scoredOut, len(rows))
		for i := range rows {
			due := ""
			if !rows[i].DueDate.IsZero() {
				due = rows[i].DueDate.Format("2006-01-02")
			}
			out[i] = scoredOut{rows[i].ID, rows[i].Title, sequenceScore(&rows[i]), due}
		}
		return emitJSON(out)
	}
	for i := range rows {
		fmt.Printf("%-8s %5.1f  %s\n", rows[i].ID[:8], sequenceScore(&rows[i]), truncate(rows[i].Title, 60))
	}
	return 0
}

// ── help ─────────────────────────────────────────────────────────────────────

func cliHelp() int {
	fmt.Fprintln(os.Stderr, `taskr — keyboard-driven task manager

Usage:
  taskr                          launch the TUI (no args)
  taskr add "title" [flags]      add a new task
  taskr list [flags]             list tasks (pending top-level by default)
  taskr top [-n=N] [--json]      show top-N by sequence score
  taskr done <id-prefix>         mark a task done (matches by id prefix)
  taskr --version                print build version
  taskr help                     this message

Flags (add):
  --due=DATE      today|tomorrow|+3d|dd-mm-yy|monday|...
  --p=h|m|l       priority (default m)
  --size=s|m|l    task size (default m)
  --project=NAME  project
  --tag=t1,t2     comma-separated tags

Flags (list):
  --json          emit JSON
  --all           include completed tasks
  --focus         only today + overdue
  --limit=N       cap rows

Notes:
  - Data lives at ~/.taskr/tasks.db (shared with the TUI). Concurrent CLI +
    TUI usage is safe for reads; writes serialize via SQLite's busy-timeout,
    but a running TUI won't see CLI changes until it restarts.
  - The sequencing engine's biases (Deadline/Priority/Momentum) are loaded
    from ~/.taskr/settings.json, so 'top' and 'list' rank the same way as
    the TUI under the user's current personality.`)
	return 0
}

// ── shared helpers ───────────────────────────────────────────────────────────

// splitFlagsAndPositionals separates a CLI subcommand's argv into a flag-only
// slice (safe to pass to flag.Parse) and a positional-only slice (the title,
// etc). Go's stdlib flag package stops at the first non-flag token, so without
// this helper users would have to write `taskr add --p=h "Buy milk"` instead
// of the more natural `taskr add "Buy milk" --p=h`.
//
// valueFlags names the flags that consume the next arg when written without an
// embedded `=` (e.g. `--due tomorrow`). Boolean flags should be omitted from
// the map so their following non-flag token stays a positional.
func splitFlagsAndPositionals(args []string, valueFlags map[string]bool) (flags, positionals []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			// POSIX end-of-flags marker — everything after is positional.
			positionals = append(positionals, args[i+1:]...)
			return
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

func printTaskTable(rows []todo.Todo) {
	if len(rows) == 0 {
		fmt.Println("(no tasks)")
		return
	}
	fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s\n", "ID", "ST", "SIZE", "PRI", "DUE", "TITLE")
	for _, t := range rows {
		st := "[ ]"
		if t.Status == todo.Done {
			st = "[✓]"
		}
		due := ""
		if !t.DueDate.IsZero() {
			due = t.DueDate.Format("02-01-06")
		}
		fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s\n",
			t.ID[:8], st, t.Size.Letter(), priorityLetter(t.Priority), due, t.Title)
	}
}
