package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

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
	case "add", "list", "ls", "done", "top",
		"show", "edit", "delete", "rm", "comment",
		"help", "-h", "--help", "--version":
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
	case "show":
		return cliShow(rest)
	case "edit":
		return cliEdit(rest)
	case "delete", "rm":
		return cliDelete(rest)
	case "comment":
		return cliComment(rest)
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
	settings, sErr := loadSettings()
	if sErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v (using defaults)\n", sErr)
	}
	applyBiases(biasesFromSettings(settings))
	repo := newSQLiteRepo()
	todos, err := repo.Load()
	return repo, todos, err
}

// findTaskByRef matches a task by either an id-prefix or a title substring,
// in that order. The id path takes precedence so scripts and aliases remain
// deterministic: a hex-shaped query that happens to appear in a task title
// won't ambiguously swap which task you operate on.
//
// Matching rules:
//   - ID prefix exactly matches one task            → return it
//   - ID prefix matches multiple tasks              → error with candidates
//     (no fallback — ambiguity is the user's call)
//   - ID prefix matches zero, title substring …
//        … matches one task                         → return it
//        … matches multiple                         → error with candidates
//        … matches zero                             → error "no task matches"
//
// Both comparisons are case-insensitive.
func findTaskByRef(todos []todo.Todo, ref string) (*todo.Todo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty task reference (need id-prefix or title substring)")
	}
	q := strings.ToLower(ref)

	// Pass 1: id-prefix.
	var idMatches []int
	for i := range todos {
		if strings.HasPrefix(strings.ToLower(todos[i].ID), q) {
			idMatches = append(idMatches, i)
		}
	}
	switch len(idMatches) {
	case 1:
		return &todos[idMatches[0]], nil
	case 0:
		// fall through to title-substring
	default:
		return nil, ambiguousMatchError("id prefix", ref, todos, idMatches)
	}

	// Pass 2: title substring (case-insensitive).
	var titleMatches []int
	for i := range todos {
		if strings.Contains(strings.ToLower(todos[i].Title), q) {
			titleMatches = append(titleMatches, i)
		}
	}
	switch len(titleMatches) {
	case 0:
		return nil, fmt.Errorf("no task matches %q (tried id-prefix and title substring)", ref)
	case 1:
		return &todos[titleMatches[0]], nil
	default:
		return nil, ambiguousMatchError("title", ref, todos, titleMatches)
	}
}

func ambiguousMatchError(kind, ref string, todos []todo.Todo, matches []int) error {
	lines := make([]string, len(matches))
	for i, m := range matches {
		lines[i] = fmt.Sprintf("    %s  %s", todos[m].ID[:8], todos[m].Title)
	}
	return fmt.Errorf("%s %q matches %d tasks:\n%s", kind, ref, len(matches), strings.Join(lines, "\n"))
}

// findByPrefix is preserved as a thin alias so existing call-sites (and
// tests) keep working while the name catches up everywhere.
func findByPrefix(todos []todo.Todo, ref string) (*todo.Todo, error) {
	return findTaskByRef(todos, ref)
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
	settings, sErr := loadSettings()
	if sErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v (using defaults)\n", sErr)
	}
	applyBiases(biasesFromSettings(settings))
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

// ── show ─────────────────────────────────────────────────────────────────────

func cliShow(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a formatted view")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr show <id-prefix>")
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	t, err := findByPrefix(todos, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		return emitJSON(t)
	}
	printTaskDetail(t)
	return 0
}

func printTaskDetail(t *todo.Todo) {
	fmt.Printf("ID:       %s\n", t.ID)
	fmt.Printf("Title:    %s\n", t.Title)
	status := "pending"
	if t.Status == todo.Done {
		status = "done"
	}
	fmt.Printf("Status:   %s\n", status)
	fmt.Printf("Priority: %s\n", t.Priority.String())
	fmt.Printf("Size:     %s\n", t.Size.String())
	if !t.StartDate.IsZero() {
		fmt.Printf("Start:    %s\n", t.StartDate.Format("2006-01-02"))
	}
	if !t.DueDate.IsZero() {
		fmt.Printf("Due:      %s\n", t.DueDate.Format("2006-01-02"))
	}
	if t.Project != "" {
		fmt.Printf("Project:  %s\n", t.Project)
	}
	if len(t.Tags) > 0 {
		fmt.Printf("Tags:     %s\n", strings.Join(t.Tags, ", "))
	}
	if !t.CompletedAt.IsZero() {
		fmt.Printf("Done at:  %s\n", t.CompletedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("Created:  %s\n", t.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("Modified: %s\n", t.ModifiedAt.Format("2006-01-02 15:04"))

	if t.Status == todo.Pending {
		sc := sequenceComponentsFor(t)
		fmt.Printf("Score:    %.1f  (D %.1f · P %.1f · M %.1f · A %.1f)\n",
			sc.Total, sc.Urgency, sc.Importance, sc.Momentum, sc.Age)
	}
	if len(t.Dependencies) > 0 {
		fmt.Printf("\nDependencies (%d):\n", len(t.Dependencies))
		for _, dep := range t.Dependencies {
			fmt.Printf("  - %s\n", dep)
		}
	}
	if len(t.Learnings) > 0 {
		fmt.Printf("\nLearnings (%d):\n", len(t.Learnings))
		for _, l := range t.Learnings {
			fmt.Printf("  - %s\n", l.Text)
		}
	}
	if len(t.Comments) > 0 {
		fmt.Printf("\nComments (%d):\n", len(t.Comments))
		for _, c := range t.Comments {
			fmt.Printf("  - [%s] %s\n", c.CreatedAt.Format("2006-01-02"), c.Text)
		}
	}
	if t.Notes != "" {
		fmt.Printf("\nNotes:\n%s\n", t.Notes)
	}
}

// ── edit ─────────────────────────────────────────────────────────────────────

// editValueFlags mirrors addValueFlags for splitFlagsAndPositionals: every
// value-taking flag is listed so the user can write the id before or after
// the flags.
var editValueFlags = map[string]bool{
	"title": true, "p": true, "size": true, "due": true, "start": true,
	"project": true, "add-tag": true, "remove-tag": true,
}

func cliEdit(args []string) int {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	title := fs.String("title", "", "new title")
	priority := fs.String("p", "", "new priority: h|m|l")
	size := fs.String("size", "", "new size: s|m|l")
	due := fs.String("due", "", "set due date (today|tomorrow|+3d|dd-mm-yy|...)")
	clearDue := fs.Bool("clear-due", false, "drop the due date")
	start := fs.String("start", "", "set start date")
	clearStart := fs.Bool("clear-start", false, "drop the start date")
	project := fs.String("project", "", "set project name")
	clearProject := fs.Bool("clear-project", false, "drop the project")
	addTag := fs.String("add-tag", "", "comma-separated tags to add")
	removeTag := fs.String("remove-tag", "", "comma-separated tags to remove")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: taskr edit <id-prefix> [flags]")
		fs.PrintDefaults()
	}
	flagArgs, positionals := splitFlagsAndPositionals(args, editValueFlags)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) != 1 {
		fmt.Fprintln(os.Stderr, "taskr edit: exactly one id-prefix required")
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	t, err := findByPrefix(todos, positionals[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	changed := false
	if *title != "" {
		t.Title = *title
		t.ModifiedAt = time.Now()
		changed = true
	}
	if *priority != "" {
		t.SetPriority(parsePriorityFlag(*priority))
		changed = true
	}
	if *size != "" {
		t.SetSize(parseSizeFlag(*size))
		changed = true
	}
	if *clearDue {
		t.DueDate = time.Time{}
		t.ModifiedAt = time.Now()
		changed = true
	} else if *due != "" {
		d, err := parseDueDate(*due)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid due date %q: %v\n", *due, err)
			return 2
		}
		t.SetDueDate(d)
		changed = true
	}
	if *clearStart {
		t.StartDate = time.Time{}
		t.ModifiedAt = time.Now()
		changed = true
	} else if *start != "" {
		d, err := parseDueDate(*start)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid start date %q: %v\n", *start, err)
			return 2
		}
		t.SetStartDate(d)
		changed = true
	}
	if *clearProject {
		t.SetProject("")
		changed = true
	} else if *project != "" {
		t.SetProject(*project)
		changed = true
	}
	if *addTag != "" {
		for _, tag := range strings.Split(*addTag, ",") {
			t.AddTag(tag)
		}
		changed = true
	}
	if *removeTag != "" {
		for _, tag := range strings.Split(*removeTag, ",") {
			t.RemoveTag(tag)
		}
		changed = true
	}
	if !changed {
		fmt.Fprintln(os.Stderr, "taskr edit: no fields changed (nothing to save)")
		return 0
	}
	if err := repo.Save([]*todo.Todo{t}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("edited  %s  %s\n", t.ID[:8], t.Title)
	return 0
}

// ── delete ───────────────────────────────────────────────────────────────────

func cliDelete(args []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr delete <id-prefix>")
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
	// Soft delete via the Repository contract — the row is tombstoned and
	// will not load again. Matches the TUI's delete semantics.
	if err := repo.Save(nil, []string{t.ID}); err != nil {
		fmt.Fprintf(os.Stderr, "delete: %v\n", err)
		return 1
	}
	fmt.Printf("deleted %s  %s\n", t.ID[:8], t.Title)
	return 0
}

// ── comment ──────────────────────────────────────────────────────────────────

func cliComment(args []string) int {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, `usage: taskr comment <id-prefix> "comment text"`)
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
	text := strings.Join(fs.Args()[1:], " ")
	t.AddComment(text)
	if err := repo.Save([]*todo.Todo{t}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("commented on %s\n", t.ID[:8])
	return 0
}

// ── help ─────────────────────────────────────────────────────────────────────

func cliHelp() int {
	fmt.Fprintln(os.Stderr, `taskr — keyboard-driven task manager

Usage:
  taskr                                launch the TUI (no args)
  taskr add "title" [flags]            add a new task
  taskr list [flags]                   list tasks (pending top-level by default)
  taskr top [-n=N] [--json]            show top-N by sequence score
  taskr show <id-prefix> [--json]      full detail of one task (incl. score breakdown)
  taskr edit <id-prefix> [flags]       change fields on one task
  taskr done <id-prefix>               mark a task done
  taskr delete <id-prefix>             soft-delete a task (alias: rm)
  taskr comment <id-prefix> "text"     append a comment
  taskr --version                      print build version
  taskr help                           this message

Task references can be a UUID prefix (` + "`347e`" + `) OR a case-insensitive
substring of the title (` + "`milk`" + `). ID-prefix wins on hex-shaped queries
so scripts stay deterministic. Ambiguous refs fail with exit code 2 and
list each match with its short ID.

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

Flags (edit):
  --title=...     new title
  --p=h|m|l       new priority
  --size=s|m|l    new size
  --due=DATE      set due date         --clear-due       drop due date
  --start=DATE    set start date       --clear-start     drop start date
  --project=NAME  set project          --clear-project   drop project
  --add-tag=t1,t2     append tags
  --remove-tag=t1,t2  remove tags

Notes:
  - Data lives at ~/.taskr/tasks.db (shared with the TUI). Concurrent CLI +
    TUI usage is safe for reads; writes serialize via SQLite's busy-timeout,
    but a running TUI won't see CLI changes until it restarts (a watcher is
    planned).
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
