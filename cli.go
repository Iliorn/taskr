package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
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
		"stats", "start", "stop", "export", "subtask",
		"search", "tags", "projects",
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
	case "stats":
		return cliStats(rest)
	case "start":
		return cliStart(rest)
	case "stop":
		return cliStop(rest)
	case "export":
		return cliExport(rest)
	case "subtask":
		return cliSubtask(rest)
	case "search":
		return cliSearch(rest)
	case "tags":
		return cliTags(rest)
	case "projects":
		return cliProjects(rest)
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

// resolveRefs resolves N refs at once, failing fast on the first unresolvable
// or ambiguous one. Used by batch verbs (`done a b c`) so we can validate the
// whole set before mutating anything. Duplicate refs collapse to the first
// match — `done abc abc` is a single done, not an error.
func resolveRefs(todos []todo.Todo, refs []string) ([]*todo.Todo, error) {
	out := make([]*todo.Todo, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		t, err := findTaskByRef(todos, ref)
		if err != nil {
			return nil, err
		}
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out, nil
}

// listFilterOpts collects every filter `list` and `search` apply. Lifted into
// a struct so the filter logic is unit-testable separately from CLI parsing.
type listFilterOpts struct {
	includeDone bool
	focus       bool
	tag         string // matched case-insensitively after NormalizeTag
	project     string // matched case-insensitively for equality
	search      string // case-insensitive substring of title
}

func filterTopLevel(todos []todo.Todo, opts listFilterOpts) []todo.Todo {
	tagQ := todo.NormalizeTag(opts.tag)
	projQ := strings.ToLower(strings.TrimSpace(opts.project))
	searchQ := strings.ToLower(strings.TrimSpace(opts.search))

	rows := make([]todo.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ParentID != "" {
			continue
		}
		if !opts.includeDone && t.Status != todo.Pending {
			continue
		}
		if opts.focus && !(t.IsOverdue() || t.IsDueToday()) {
			continue
		}
		if tagQ != "" {
			found := false
			for _, tag := range t.Tags {
				if tag == tagQ {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if projQ != "" && strings.ToLower(t.Project) != projQ {
			continue
		}
		if searchQ != "" && !strings.Contains(strings.ToLower(t.Title), searchQ) {
			continue
		}
		rows = append(rows, t)
	}
	return rows
}

// trackedToday sums the portion of every TimeEntry across all todos that falls
// within today's local window. Running entries count up to `now`. Used by the
// stats one-liner.
func trackedTodayDuration(todos []todo.Todo, now time.Time) time.Duration {
	today := startOfDay(now)
	tomorrow := today.AddDate(0, 0, 1)
	var total time.Duration
	for _, t := range todos {
		for _, e := range t.TimeEntries {
			start := e.StartedAt
			end := e.StoppedAt
			if end.IsZero() {
				end = now
			}
			if !end.After(today) || !start.Before(tomorrow) {
				continue
			}
			if start.Before(today) {
				start = today
			}
			if end.After(tomorrow) {
				end = tomorrow
			}
			total += end.Sub(start)
		}
	}
	return total
}

// ── add ──────────────────────────────────────────────────────────────────────

// addValueFlags lists the value-taking flags `add` accepts. splitFlagsAndPositionals
// uses this to know which flags consume the next arg (vs. being self-contained
// `--name=value`), so users can put the title in any position.
var addValueFlags = map[string]bool{
	"due": true, "p": true, "size": true, "project": true, "tag": true, "like": true,
}

func cliAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	due := fs.String("due", "", "due date (today|tomorrow|+3d|dd-mm-yy|...)")
	// Defaults stay empty so --like can fill them; without --like, todo.New
	// already sets Medium for both, so the user-visible behavior is unchanged.
	priority := fs.String("p", "", "priority: h|m|l (default m, or copied from --like)")
	size := fs.String("size", "", "size: s|m|l (default m, or copied from --like)")
	project := fs.String("project", "", "project name")
	tags := fs.String("tag", "", "comma-separated tags")
	like := fs.String("like", "", "clone priority/size/project/tags from an existing task ref")
	startNow := fs.Bool("start", false, "start the time tracker on the new task (stops any other running timer first)")
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
	settings, sErr := loadSettings()
	if sErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v (using defaults)\n", sErr)
	}
	applyBiases(biasesFromSettings(settings))
	repo := newSQLiteRepo()

	t := todo.New(strings.Join(titleParts, " "))
	// --like is applied first so explicit flags below override the cloned
	// values. Skipped when empty to keep the no-clone path a single Save.
	if *like != "" {
		existing, err := repo.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "load: %v\n", err)
			return 1
		}
		src, err := findTaskByRef(existing, *like)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		t.Priority = src.Priority
		t.Size = src.Size
		t.Project = src.Project
		for _, tag := range src.Tags {
			t.AddTag(tag)
		}
	}
	if *priority != "" {
		t.Priority = parsePriorityFlag(*priority)
	}
	if *size != "" {
		t.Size = parseSizeFlag(*size)
	}
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
	// --start collapses the common "add then start tracking" two-call dance
	// into one. We re-load to find any other running timer (the TUI's
	// single-timer invariant applies to the CLI too), then stop those + start
	// the new task + persist in one Save.
	if *startNow {
		existing, err := repo.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "load: %v\n", err)
			return 1
		}
		dirty := []*todo.Todo{&t}
		for i := range existing {
			if existing[i].IsTimerRunning() {
				x := existing[i]
				x.StopTimer()
				dirty = append(dirty, &x)
				fmt.Fprintf(os.Stderr, "stopped: %s  %s\n", x.ID[:8], x.Title)
			}
		}
		t.StartTimer()
		if err := repo.Save(dirty, nil); err != nil {
			fmt.Fprintf(os.Stderr, "save: %v\n", err)
			return 1
		}
		fmt.Printf("added + started: %s  %s\n", t.ID[:8], t.Title)
		return 0
	}
	if err := repo.Save([]*todo.Todo{&t}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("added %s  %s\n", t.ID[:8], t.Title)
	return 0
}

// ── list ─────────────────────────────────────────────────────────────────────

var listValueFlags = map[string]bool{
	"limit": true, "tag": true, "project": true, "search": true,
}

func cliList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	all := fs.Bool("all", false, "include completed tasks")
	focus := fs.Bool("focus", false, "only today + overdue")
	limit := fs.Int("limit", 0, "cap rows (0 = no cap)")
	tag := fs.String("tag", "", "only tasks carrying this tag (case-insensitive)")
	project := fs.String("project", "", "only tasks in this project")
	search := fs.String("search", "", "only tasks whose title contains this substring")
	flagArgs, _ := splitFlagsAndPositionals(args, listValueFlags)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	rows := filterTopLevel(todos, listFilterOpts{
		includeDone: *all,
		focus:       *focus,
		tag:         *tag,
		project:     *project,
		search:      *search,
	})
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

// cliSearch is sugar for `list --all --search=...` — kept as its own verb for
// discoverability. Includes done by default since searching is usually for
// recall, not focus.
func cliSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	pendingOnly := fs.Bool("pending", false, "exclude completed tasks (default: include)")
	limit := fs.Int("limit", 0, "cap rows (0 = no cap)")
	flagArgs, positionals := splitFlagsAndPositionals(args, map[string]bool{"limit": true})
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) == 0 {
		fmt.Fprintln(os.Stderr, `usage: taskr search "term" [--json] [--pending] [--limit=N]`)
		return 2
	}
	term := strings.Join(positionals, " ")
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	rows := filterTopLevel(todos, listFilterOpts{
		includeDone: !*pendingOnly,
		search:      term,
	})
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

// cliTags / cliProjects: discovery commands. Counts only count pending
// top-level tasks so the listing reflects what's in flight, not historical
// fragments. Sort by count desc, then name asc, for stable output.

type nameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func sortNameCounts(rows []nameCount) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Name < rows[j].Name
	})
}

func cliTags(args []string) int {
	fs := flag.NewFlagSet("tags", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	counts := map[string]int{}
	for _, t := range todos {
		if t.ParentID != "" || t.Status != todo.Pending {
			continue
		}
		for _, tag := range t.Tags {
			counts[tag]++
		}
	}
	rows := make([]nameCount, 0, len(counts))
	for tag, c := range counts {
		rows = append(rows, nameCount{tag, c})
	}
	sortNameCounts(rows)
	if *asJSON {
		return emitJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Println("(no tags)")
		return 0
	}
	for _, r := range rows {
		fmt.Printf("%4d  %s\n", r.Count, r.Name)
	}
	return 0
}

func cliProjects(args []string) int {
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	counts := map[string]int{}
	for _, t := range todos {
		if t.ParentID != "" || t.Status != todo.Pending {
			continue
		}
		if t.Project == "" {
			continue
		}
		counts[t.Project]++
	}
	rows := make([]nameCount, 0, len(counts))
	for p, c := range counts {
		rows = append(rows, nameCount{p, c})
	}
	sortNameCounts(rows)
	if *asJSON {
		return emitJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Println("(no projects)")
		return 0
	}
	for _, r := range rows {
		fmt.Printf("%4d  %s\n", r.Count, r.Name)
	}
	return 0
}

// ── done ─────────────────────────────────────────────────────────────────────

func cliDone(args []string) int {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	comment := fs.String("comment", "", "append this comment to each task transitioned to done")
	flagArgs, positionals := splitFlagsAndPositionals(args, map[string]bool{"comment": true})
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr done <ref> [<ref>...] [--comment=\"why\"]")
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	// Resolve every ref before mutating anything — that way an ambiguity in
	// position 3 doesn't leave the first two already toggled.
	targets, err := resolveRefs(todos, positionals)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var dirty, skipped []*todo.Todo
	for _, t := range targets {
		if t.Status == todo.Done {
			skipped = append(skipped, t)
			continue
		}
		// Comment lands BEFORE Toggle so the timeline reads "added the why,
		// then closed it" — and the comment timestamp matches the close.
		if *comment != "" {
			t.AddComment(*comment)
		}
		t.Toggle()
		dirty = append(dirty, t)
	}
	if len(dirty) > 0 {
		if err := repo.Save(dirty, nil); err != nil {
			fmt.Fprintf(os.Stderr, "save: %v\n", err)
			return 1
		}
	}
	for _, t := range dirty {
		fmt.Printf("done  %s  %s\n", t.ID[:8], t.Title)
	}
	for _, t := range skipped {
		fmt.Fprintf(os.Stderr, "already done: %s\n", t.Title)
	}
	return 0
}

// ── top ──────────────────────────────────────────────────────────────────────

func cliTop(args []string) int {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	n := fs.Int("n", 10, "rows to show")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	wide := fs.Bool("wide", false, "include priority, due date, and tags columns")
	flagArgs, _ := splitFlagsAndPositionals(args, map[string]bool{"n": true})
	if err := fs.Parse(flagArgs); err != nil {
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
			ID       string   `json:"id"`
			Title    string   `json:"title"`
			Score    float64  `json:"score"`
			Priority string   `json:"priority"`
			Due      string   `json:"due,omitempty"`
			Tags     []string `json:"tags,omitempty"`
		}
		out := make([]scoredOut, len(rows))
		for i := range rows {
			due := ""
			if !rows[i].DueDate.IsZero() {
				due = rows[i].DueDate.Format("2006-01-02")
			}
			out[i] = scoredOut{rows[i].ID, rows[i].Title, sequenceScore(&rows[i]), priorityLetter(rows[i].Priority), due, rows[i].Tags}
		}
		return emitJSON(out)
	}
	if *wide {
		// Adaptive tag-column width: hug the widest tag string in the result
		// set instead of locking to 20 chars and chopping anything over. Cap
		// at 40 so a wildly tagged task can't push the title off the screen.
		tagW := len("TAGS")
		tagStrings := make([]string, len(rows))
		for i := range rows {
			tagStrings[i] = strings.Join(rows[i].Tags, ",")
			if w := len(tagStrings[i]); w > tagW {
				tagW = w
			}
		}
		if tagW > 40 {
			tagW = 40
		}
		fmt.Printf("%-8s  %-5s  %-3s  %-10s  %-*s  %s\n", "ID", "SCORE", "PRI", "DUE", tagW, "TAGS", "TITLE")
		for i := range rows {
			due := ""
			if !rows[i].DueDate.IsZero() {
				due = rows[i].DueDate.Format("02-01-06")
			}
			tags := truncate(tagStrings[i], tagW)
			fmt.Printf("%-8s  %5.1f  %-3s  %-10s  %-*s  %s\n",
				rows[i].ID[:8], sequenceScore(&rows[i]),
				priorityLetter(rows[i].Priority), due, tagW, tags,
				truncate(rows[i].Title, 60))
		}
		return 0
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
	// Route through splitFlagsAndPositionals so `taskr show <ref> --json` works
	// the same as `taskr show --json <ref>`. Stdlib flag.Parse stops at the
	// first non-flag token, which otherwise turns a trailing --json into a
	// second positional and trips the usage check below.
	flagArgs, positionals := splitFlagsAndPositionals(args, nil)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) != 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr show <ref>")
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	t, err := findTaskByRef(todos, positionals[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		return emitJSON(t)
	}
	// Gather subtasks from the loaded slice (parent→child via ParentID).
	// Sorted by CreatedAt to match TUI ordering.
	var subs []todo.Todo
	for _, s := range todos {
		if s.ParentID == t.ID {
			subs = append(subs, s)
		}
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].CreatedAt.Before(subs[j].CreatedAt) })
	printTaskDetail(t, subs)
	return 0
}

func printTaskDetail(t *todo.Todo, subs []todo.Todo) {
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
		// Spelled-out component names instead of single letters — the previous
		// `D/P/M/A` was a stat-readout cliff for anyone not already steeped in
		// the sequencing engine's terminology.
		fmt.Printf("Score:    %.1f  (Deadline %.1f · Priority %.1f · Momentum %.1f · Age %.1f)\n",
			sc.Total, sc.Urgency, sc.Importance, sc.Momentum, sc.Age)
	}
	if len(subs) > 0 {
		fmt.Printf("\nSubtasks (%d):\n", len(subs))
		for _, s := range subs {
			marker := "[ ]"
			if s.Status == todo.Done {
				marker = "[✓]"
			}
			fmt.Printf("  %s  %s  %s\n", s.ID[:8], marker, s.Title)
		}
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
		// 1-based indices so the user can pass them directly to
		// `taskr comment <ref> --edit=N` / `--delete=N`. Timestamp includes
		// HH:MM so multiple comments on the same day stay ordered/readable.
		fmt.Printf("\nComments (%d):\n", len(t.Comments))
		for i, c := range t.Comments {
			fmt.Printf("  %d. [%s] %s\n", i+1, c.CreatedAt.Format("2006-01-02 15:04"), c.Text)
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
		t.Title = todo.CapitalizeTitle(*title)
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
	editIdx := fs.Int("edit", 0, "1-based comment index to edit (with new text as positional)")
	delIdx := fs.Int("delete", 0, "1-based comment index to delete")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage:
  taskr comment <ref> "text"              append a new comment
  taskr comment <ref> -                   read comment text from stdin
  taskr comment <ref> --edit=N "new text" edit comment N (1-based)
  taskr comment <ref> --delete=N          delete comment N (1-based)`)
	}
	// comment supports interspersed flags so --edit / --delete can sit
	// before or after the ref, just like other mutation commands.
	flagArgs, positionals := splitFlagsAndPositionals(args, map[string]bool{"edit": true, "delete": true})
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) < 1 {
		fs.Usage()
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	t, err := findTaskByRef(todos, positionals[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	switch {
	case *delIdx > 0:
		// Delete: index is 1-based for humans, 0-based internally.
		i := *delIdx - 1
		if i < 0 || i >= len(t.Comments) {
			fmt.Fprintf(os.Stderr, "comment index %d out of range (task has %d comments)\n", *delIdx, len(t.Comments))
			return 2
		}
		t.DeleteComment(i)
		if err := repo.Save([]*todo.Todo{t}, nil); err != nil {
			fmt.Fprintf(os.Stderr, "save: %v\n", err)
			return 1
		}
		fmt.Printf("deleted comment %d on %s\n", *delIdx, t.ID[:8])
		return 0
	case *editIdx > 0:
		i := *editIdx - 1
		if i < 0 || i >= len(t.Comments) {
			fmt.Fprintf(os.Stderr, "comment index %d out of range (task has %d comments)\n", *editIdx, len(t.Comments))
			return 2
		}
		if len(positionals) < 2 {
			fmt.Fprintln(os.Stderr, "taskr comment --edit: new comment text required")
			return 2
		}
		text, terr := commentTextFromPositionals(positionals[1:], os.Stdin)
		if terr != nil {
			fmt.Fprintf(os.Stderr, "stdin: %v\n", terr)
			return 1
		}
		t.UpdateComment(i, text)
		if err := repo.Save([]*todo.Todo{t}, nil); err != nil {
			fmt.Fprintf(os.Stderr, "save: %v\n", err)
			return 1
		}
		fmt.Printf("edited comment %d on %s\n", *editIdx, t.ID[:8])
		return 0
	default:
		// Append (default behavior).
		if len(positionals) < 2 {
			fs.Usage()
			return 2
		}
		text, terr := commentTextFromPositionals(positionals[1:], os.Stdin)
		if terr != nil {
			fmt.Fprintf(os.Stderr, "stdin: %v\n", terr)
			return 1
		}
		t.AddComment(text)
		if err := repo.Save([]*todo.Todo{t}, nil); err != nil {
			fmt.Fprintf(os.Stderr, "save: %v\n", err)
			return 1
		}
		fmt.Printf("commented on %s\n", t.ID[:8])
		return 0
	}
}

// commentTextFromPositionals resolves the user's comment text. If the single
// positional is "-", read everything from the given reader (lets `taskr
// comment <ref> -` accept piped or here-doc input for long comments instead
// of forcing shell-escape gymnastics). Trailing newline trimmed so a heredoc
// doesn't leave a blank line in the comment.
func commentTextFromPositionals(positionals []string, r io.Reader) (string, error) {
	if len(positionals) == 1 && positionals[0] == "-" {
		b, err := io.ReadAll(r)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\n"), nil
	}
	return strings.Join(positionals, " "), nil
}

// ── stats ────────────────────────────────────────────────────────────────────

// statsSummary is the structured shape `stats --format=json` writes and that
// the waybar formatter renders into its expected schema.
type statsSummary struct {
	Active              int `json:"active"`
	Overdue             int `json:"overdue"`
	DueToday            int `json:"due_today"`
	DueThisWeek         int `json:"due_this_week"`
	DoneToday           int `json:"done_today"`
	DoneThisWeek        int `json:"done_this_week"`
	TrackedTodayMinutes int `json:"tracked_today_minutes"`
}

func computeStats(todos []todo.Todo, now time.Time) statsSummary {
	today := startOfDay(now)
	tomorrow := today.AddDate(0, 0, 1)
	weekAhead := today.AddDate(0, 0, 7)
	weekAgo := today.AddDate(0, 0, -7)
	var s statsSummary
	for _, t := range todos {
		if t.ParentID != "" {
			continue
		}
		if t.Status == todo.Done {
			if !t.CompletedAt.IsZero() {
				if !t.CompletedAt.Before(today) {
					s.DoneToday++
				}
				if !t.CompletedAt.Before(weekAgo) {
					s.DoneThisWeek++
				}
			}
			continue
		}
		s.Active++
		if t.IsOverdue() {
			s.Overdue++
		} else if !t.DueDate.IsZero() && t.DueDate.Before(tomorrow) {
			s.DueToday++
		} else if !t.DueDate.IsZero() && t.DueDate.Before(weekAhead) {
			s.DueThisWeek++
		}
	}
	// Time tracking spans all todos (including subtasks and completed) since
	// time entries are work that happened today regardless of the parent
	// task's lifecycle. Minutes as an int keeps the JSON shape boring; the
	// text renderer formats it for humans.
	s.TrackedTodayMinutes = int(trackedTodayDuration(todos, now).Minutes())
	return s
}

func cliStats(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	format := fs.String("format", "text", "output format: text | json | waybar")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	s := computeStats(todos, time.Now())
	switch strings.ToLower(*format) {
	case "json":
		return emitJSON(s)
	case "waybar":
		// Waybar custom modules expect this JSON shape on stdout. `class`
		// drives the CSS state — warning when there's anything overdue or
		// due today, ok otherwise. Tooltip carries the breakdown.
		class := "ok"
		switch {
		case s.Overdue > 0:
			class = "critical"
		case s.DueToday > 0:
			class = "warning"
		}
		text := fmt.Sprintf("%d active", s.Active)
		if s.Overdue > 0 {
			text = fmt.Sprintf("%d overdue · %d active", s.Overdue, s.Active)
		} else if s.DueToday > 0 {
			text = fmt.Sprintf("%d due today · %d active", s.DueToday, s.Active)
		}
		tracked := formatDurationCompact(time.Duration(s.TrackedTodayMinutes) * time.Minute)
		tooltip := fmt.Sprintf("active %d · overdue %d · due today %d · due this week %d · done today %d · tracked today %s",
			s.Active, s.Overdue, s.DueToday, s.DueThisWeek, s.DoneToday, tracked)
		return emitJSON(map[string]string{"text": text, "tooltip": tooltip, "class": class})
	default:
		tracked := formatDurationCompact(time.Duration(s.TrackedTodayMinutes) * time.Minute)
		fmt.Printf("active %d · overdue %d · due today %d · due this week %d · done today %d · tracked today %s\n",
			s.Active, s.Overdue, s.DueToday, s.DueThisWeek, s.DoneToday, tracked)
		return 0
	}
}

// ── start / stop ─────────────────────────────────────────────────────────────

func cliStart(args []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr start <ref>")
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	target, err := findTaskByRef(todos, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	// Idempotent re-start: if the target is already the running task, don't
	// rotate the timer entry. todo.StartTimer unconditionally calls StopTimer
	// then appends a fresh entry, so a stray repeat would split one session
	// into two zero-gap entries — silent data drift the user would only
	// notice on export.
	if target.IsTimerRunning() {
		fmt.Fprintf(os.Stderr, "already tracking: %s  %s\n", target.ID[:8], target.Title)
		return 0
	}
	// Stop any other running timer first — the TUI enforces single-task
	// time tracking and the CLI should preserve that invariant. Collect
	// all touched tasks so they're flushed in one Save.
	dirty := []*todo.Todo{target}
	for i := range todos {
		if todos[i].ID == target.ID {
			continue
		}
		if todos[i].IsTimerRunning() {
			t := todos[i]
			t.StopTimer()
			dirty = append(dirty, &t)
			fmt.Printf("stopped: %s  %s\n", t.ID[:8], t.Title)
		}
	}
	target.StartTimer()
	if err := repo.Save(dirty, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("started: %s  %s\n", target.ID[:8], target.Title)
	return 0
}

func cliStop(args []string) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	var target *todo.Todo
	if fs.NArg() == 1 {
		// Explicit ref: stop the named task if it's actually running.
		t, err := findTaskByRef(todos, fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if !t.IsTimerRunning() {
			fmt.Fprintf(os.Stderr, "task %s is not currently tracking\n", t.ID[:8])
			return 2
		}
		target = t
	} else {
		// No ref: stop whichever task is running. Zero or two-plus is an error.
		var running []*todo.Todo
		for i := range todos {
			if todos[i].IsTimerRunning() {
				running = append(running, &todos[i])
			}
		}
		switch len(running) {
		case 0:
			fmt.Fprintln(os.Stderr, "no task is currently tracking")
			return 0
		case 1:
			target = running[0]
		default:
			fmt.Fprintln(os.Stderr, "multiple tasks tracking — pass a <ref> to disambiguate")
			return 2
		}
	}
	// Capture the elapsed time before StopTimer wipes the running entry's
	// in-progress state, so we can report it.
	var elapsed time.Duration
	if e := target.RunningEntry(); e != nil {
		elapsed = time.Since(e.StartedAt)
	}
	target.StopTimer()
	if err := repo.Save([]*todo.Todo{target}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("stopped: %s  %s  (elapsed %s)\n", target.ID[:8], target.Title, formatDuration(elapsed))
	return 0
}

// ── export ───────────────────────────────────────────────────────────────────

func cliExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	includeDone := fs.Bool("include-done", false, "include completed tasks (default: only pending live tasks)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	out := make([]todo.Todo, 0, len(todos))
	for _, t := range todos {
		if !*includeDone && t.Status == todo.Done {
			continue
		}
		out = append(out, t)
	}
	return emitJSON(out)
}

// ── subtask ──────────────────────────────────────────────────────────────────

func cliSubtask(args []string) int {
	fs := flag.NewFlagSet("subtask", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	each := fs.Bool("each", false, "treat each remaining positional as a separate subtask title")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage:
  taskr subtask <parent-ref> "title"                   one subtask (args after parent are joined)
  taskr subtask <parent-ref> --each "title1" "title2"  one subtask per remaining positional`)
	}
	flagArgs, positionals := splitFlagsAndPositionals(args, nil)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) < 2 {
		fs.Usage()
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	parent, err := findTaskByRef(todos, positionals[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var titles []string
	if *each {
		titles = positionals[1:]
	} else {
		// Default mode preserves the historical behavior of joining everything
		// after the parent ref into one title — that's how unquoted
		// `subtask P Buy milk` has always worked.
		titles = []string{strings.Join(positionals[1:], " ")}
	}
	subs := make([]*todo.Todo, 0, len(titles))
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		s := todo.NewSubtask(title, parent.ID)
		subs = append(subs, &s)
	}
	if len(subs) == 0 {
		fmt.Fprintln(os.Stderr, "taskr subtask: no non-empty titles")
		return 2
	}
	if err := repo.Save(subs, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	for _, s := range subs {
		fmt.Printf("subtask of %s: %s  %s\n", parent.ID[:8], s.ID[:8], s.Title)
	}
	return 0
}

// ── help ─────────────────────────────────────────────────────────────────────

func cliHelp() int {
	fmt.Fprintln(os.Stderr, `taskr — keyboard-driven task manager

Usage:
  taskr                                launch the TUI (no args)

Tasks:
  taskr add "title" [flags]            add a new task (--like <ref> clones, --start tracks)
  taskr list [flags]                   list pending top-level tasks (filters below)
  taskr search "term" [flags]          title-substring search (includes done by default)
  taskr top [-n=N] [--json] [--wide]   show top-N by sequence score
  taskr show <ref> [--json]            full detail (incl. score breakdown + subtask IDs)
  taskr edit <ref> [flags]             change fields on one task
  taskr done <ref>... [--comment=...]  mark one or more tasks done (optional comment per task)
  taskr delete <ref>                   soft-delete a task (alias: rm)
  taskr subtask <parent> "title"       create a subtask (--each for multiple titles)

Discovery:
  taskr tags [--json]                  pending tags with counts
  taskr projects [--json]              pending projects with counts

Tracking:
  taskr start <ref>                    start the time tracker (no-op if already tracking ref)
  taskr stop [<ref>]                   stop the tracker (no ref = whichever's running)

Comments:
  taskr comment <ref> "text"           append a comment
  taskr comment <ref> -                read comment text from stdin (for long/heredoc input)
  taskr comment <ref> --edit=N "text"  edit comment N (1-based)
  taskr comment <ref> --delete=N       delete comment N

Reporting / backup:
  taskr stats [--format=text|json|waybar]   one-line health summary (default text)
  taskr export [--include-done]             JSON snapshot of every live task to stdout

Meta:
  taskr --version                      print build version
  taskr help                           this message

Task references can be a UUID prefix (` + "`347e`" + `) OR a case-insensitive
substring of the title (` + "`milk`" + `). ID-prefix wins on hex-shaped queries
so scripts stay deterministic. Ambiguous refs fail with exit code 2 and
list each match with its short ID.

Flags (add):
  --due=DATE      today|tomorrow|+3d|dd-mm-yy|monday|...
  --p=h|m|l       priority (default m, or copied from --like)
  --size=s|m|l    task size (default m, or copied from --like)
  --project=NAME  project
  --tag=t1,t2     comma-separated tags
  --like=REF      clone priority/size/project/tags from existing task (flags above override)
  --start         start the time tracker on the new task (stops any other running timer first)

Flags (list / search):
  --json          emit JSON
  --all           include completed tasks (list only; search includes by default)
  --pending       exclude completed (search only; inverts default)
  --focus         only today + overdue (list only)
  --tag=NAME      only tasks carrying this tag
  --project=NAME  only tasks in this project
  --search=TERM   only tasks whose title contains TERM (list; redundant with 'search' verb)
  --limit=N       cap rows

Flags (top):
  --n=N           rows to show (default 10)
  --json          emit JSON (includes tags, priority, due)
  --wide          table with priority, due, tags columns

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
    TUI usage is safe for reads; writes serialize via SQLite's busy-timeout.
    A running TUI live-reloads on external writes via a filesystem watcher,
    so CLI changes appear without restarting it.
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

func printTaskTable(rows []todo.Todo) {
	if len(rows) == 0 {
		fmt.Println("(no tasks)")
		return
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
	if projW > 0 {
		fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %-*s  %s\n",
			"ID", "ST", "SIZE", "PRI", "DUE", projW, "PROJ", "TITLE")
	} else {
		fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s\n", "ID", "ST", "SIZE", "PRI", "DUE", "TITLE")
	}
	for _, t := range rows {
		st := "[ ]"
		if t.Status == todo.Done {
			st = "[✓]"
		}
		due := ""
		if !t.DueDate.IsZero() {
			due = t.DueDate.Format("02-01-06")
		}
		if projW > 0 {
			fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %-*s  %s\n",
				t.ID[:8], st, t.Size.Letter(), priorityLetter(t.Priority), due, projW, truncate(t.Project, projW), t.Title)
		} else {
			fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s\n",
				t.ID[:8], st, t.Size.Letter(), priorityLetter(t.Priority), due, t.Title)
		}
	}
}
