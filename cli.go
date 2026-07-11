package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
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
//
// Stream contract: stdout carries only a command's primary result (the thing
// a pipe or $(...) capture wants); side-effect notices — another timer being
// stopped, ancestor due dates bumped, recovery hints — go to stderr.

// isCLICommand reports whether the first arg names a subcommand main should
// route to runCLI instead of launching the TUI.
func isCLICommand(arg string) bool {
	switch arg {
	case "add", "list", "ls", "done", "top",
		"show", "edit", "delete", "rm", "undelete", "comment",
		"stats", "start", "stop", "log", "export", "import", "subtask",
		"search", "tags", "projects", "serve", "sync", "undo",
		"doctor", "help", "-h", "--help", "--version":
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
	case "add", "done", "edit", "delete", "rm", "undelete", "comment", "start", "stop", "log", "subtask", "undo", "doctor", "import":
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
	case "top":
		return cliTop(rest)
	case "show":
		return cliShow(rest)
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
	case "doctor":
		return cliDoctor(rest)
	case "--version":
		fmt.Println(appVersion)
		return 0
	default: // help, -h, --help
		return cliHelp()
	}
}

// ── add ──────────────────────────────────────────────────────────────────────

func cliAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	due := fs.String("due", "", "due date (today|tomorrow|+3d|dd-mm-yy|...)")
	// Defaults stay empty so --like can fill them; without --like, todo.New
	// already sets Medium for both, so the user-visible behavior is unchanged.
	// --priority is an alias for --p; both share the same destination.
	var priorityVal string
	fs.StringVar(&priorityVal, "p", "", "priority: h|m|l (default m, or copied from --like)")
	fs.StringVar(&priorityVal, "priority", "", "priority: h|m|l (alias for --p)")
	size := fs.String("size", "", "size: s|m|l (default m, or copied from --like)")
	project := fs.String("project", "", "project name")
	tags := fs.String("tag", "", "comma-separated tags")
	recur := fs.String("recur", "", "recurrence rule: daily|weekly|monthly|yearly|weekdays|Nd|Nw|Nm|Ny")
	depends := fs.String("depends", "", "make the new task depend on an existing task ref, or ^ for the last-added task")
	chain := fs.Bool("chain", false, "batch add (-) only: each line depends on the previous line's task")
	like := fs.String("like", "", "clone priority/size/project/tags from an existing task ref")
	note := fs.String("note", "", "set the task's notes field (freeform body; '-' reads from stdin)")
	comment := fs.String("comment", "", "add an initial timestamped comment to the task")
	startNow := fs.Bool("start", false, "start the time tracker on the new task (stops any other running timer first)")
	asJSON := fs.Bool("json", false, "emit the created task as JSON (includes its id) instead of the human line")
	quietID := fs.Bool("quiet-id", false, "print only the new task's full id (for scripting / shell capture)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: taskr add \"title\" [flags]   (or `taskr add -` to read one title per line from stdin)")
		fmt.Fprintln(os.Stderr, "  the title accepts the same quick-add tokens as the TUI: #tag @project due:friday p:high s:l r:weekly dep:^  (flags override tokens)")
		fs.PrintDefaults()
	}
	priority := &priorityVal
	flagArgs, titleParts := splitFlagsAndPositionals(fs, args)
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

	// Resolve everything that's shared across all created tasks exactly once
	// (parsing and ref lookups), so batch add doesn't re-do it per line.
	var dueDate time.Time
	if *due != "" {
		d, err := parseDueDate(*due)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid due date %q: %v\n", *due, err)
			return 2
		}
		dueDate = d
	}
	var recurRule string
	if *recur != "" {
		canonical, ok := todo.ParseRecurrence(*recur)
		if !ok {
			fmt.Fprintf(os.Stderr, "invalid recurrence %q: use daily|weekly|monthly|yearly|weekdays|Nd|Nw|Nm|Ny\n", *recur)
			return 2
		}
		recurRule = canonical
	}

	// Titles: the joined positionals, or — when the sole positional is "-" —
	// one per non-empty stdin line (batch add). Batch shares all flags across
	// every task and writes them in a single transaction (one save, one sync).
	batch := len(titleParts) == 1 && titleParts[0] == "-"
	titles := []string{strings.Join(titleParts, " ")}
	if *chain && !batch {
		fmt.Fprintln(os.Stderr, "taskr add: --chain only applies to batch stdin add (-); for a single task use --depends ^")
		return 2
	}
	// Detect the stdin conflict: `add -` (batch titles from stdin) + `--note -`
	// (note body from stdin) can't both read the same stream.
	if batch && *note == "-" {
		fmt.Fprintln(os.Stderr, "taskr add: --note - and batch stdin add (-) both need stdin; use --note=TEXT or drop one")
		return 2
	}
	if batch {
		if *startNow {
			fmt.Fprintln(os.Stderr, "taskr add: --start can't be combined with batch stdin add (-)")
			return 2
		}
		lines, err := readTitlesFromStdin(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
			return 1
		}
		if len(lines) == 0 {
			fmt.Fprintln(os.Stderr, "taskr add: no titles on stdin")
			return 2
		}
		titles = lines
	}
	// Resolve --note value once (before buildTask, so stdin is only consumed
	// once even though buildTask is called in a loop). For '-', read from stdin
	// — same helper as `edit --note -` / `edit --append-note -`.
	var noteText string
	if *note != "" {
		text, terr := noteFlagText(*note, os.Stdin)
		if terr != nil {
			fmt.Fprintf(os.Stderr, "stdin: %v\n", terr)
			return 1
		}
		noteText = text
	}

	// Parse quick-add tokens (#tag @project due: p: s: r: dep:) out of every
	// title so the CLI understands the same grammar as the TUI — a line is
	// copy-pasteable between them. Explicit flags below still win; a dep: token
	// needs the existing set, exactly like --depends.
	parsedTitles := make([]parsedTask, len(titles))
	anyTokenDeps := false
	for i, ti := range titles {
		parsedTitles[i] = parseQuickAdd(ti)
		if len(parsedTitles[i].deps) > 0 {
			anyTokenDeps = true
		}
	}

	// --like / --depends / --start (and any dep: token) need the existing set;
	// load it once.
	var (
		existing []todo.Todo
		likeSrc  *todo.Todo
		depID    string
		depTask  *todo.Todo // kept for confirmation output after save
	)
	if *like != "" || *depends != "" || *startNow || anyTokenDeps {
		loaded, err := repo.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "load: %v\n", err)
			return 1
		}
		existing = loaded
	}
	if *like != "" {
		src, err := findTaskByRef(existing, *like)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		likeSrc = src
	}
	if *depends != "" {
		dep, err := resolveDepRef(existing, *depends)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		depID = dep.ID
		depTask = dep
	}
	// Resolve any dep: token refs up front (mirroring --depends), so a bad ref
	// fails cleanly before anything is written.
	tokenDepIDs := make([][]string, len(titles))
	for i := range parsedTitles {
		for _, ref := range parsedTitles[i].deps {
			dep, err := resolveDepRef(existing, ref)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			tokenDepIDs[i] = append(tokenDepIDs[i], dep.ID)
		}
	}

	// buildTask stamps one task, layering precedence low→high: --like clone,
	// then the title's quick-add tokens, then explicit flags. --note is the
	// freeform body and --comment a timestamped log entry (distinct purposes).
	buildTask := func(i int) todo.Todo {
		parsed := parsedTitles[i]
		t := todo.New(parsed.title)
		if likeSrc != nil {
			t.Priority = likeSrc.Priority
			t.Size = likeSrc.Size
			t.Project = likeSrc.Project
			for _, tag := range likeSrc.Tags {
				t.AddTag(tag)
			}
		}
		// Quick-add tokens override the --like clone.
		if parsed.hasPriority {
			t.Priority = parsed.priority
		}
		if parsed.hasSize {
			t.Size = parsed.size
		}
		if !parsed.dueDate.IsZero() {
			t.DueDate = parsed.dueDate
		}
		if parsed.project != "" {
			t.Project = parsed.project
		}
		for _, tag := range parsed.tags {
			t.AddTag(tag)
		}
		if parsed.recurrence != "" {
			t.Recurrence = parsed.recurrence
		}
		for _, id := range tokenDepIDs[i] {
			t.AddDependency(id)
		}
		// Explicit flags win over both the clone and the tokens.
		if *priority != "" {
			t.Priority = parsePriorityFlag(*priority)
		}
		if *size != "" {
			t.Size = parseSizeFlag(*size)
		}
		if *due != "" {
			t.DueDate = dueDate
		}
		if *project != "" {
			t.Project = *project
		}
		if *tags != "" {
			for _, tag := range strings.Split(*tags, ",") {
				t.AddTag(tag)
			}
		}
		if recurRule != "" {
			t.Recurrence = recurRule
		}
		if depID != "" {
			t.AddDependency(depID)
		}
		if noteText != "" {
			t.SetNotes(noteText)
		}
		if *comment != "" {
			t.AddComment(*comment)
		}
		return t
	}

	created := make([]todo.Todo, len(titles))
	for i := range titles {
		created[i] = buildTask(i)
	}
	// --chain turns a batch brain-dump into a recorded sequence: a decomposed
	// plan is usually typed in execution order, so each line blocks on the
	// one before it (--depends, if also given, still applies to every line).
	if *chain {
		for i := 1; i < len(created); i++ {
			created[i].AddDependency(created[i-1].ID)
		}
	}
	dirty := make([]*todo.Todo, 0, len(created)+1)
	for i := range created {
		dirty = append(dirty, &created[i])
	}

	// --start collapses the common "add then start tracking" two-call dance
	// into one (single-task only; rejected above for batch). Stop any other
	// running timer to keep the single-timer invariant, then start + save.
	started := false
	if *startNow {
		for _, x := range stopOtherRunningTimers(existing, created[0].ID) {
			dirty = append(dirty, x)
			fmt.Fprintf(os.Stderr, "stopped: %s  %s\n", x.ID[:8], x.Title)
		}
		created[0].StartTimer()
		started = true
	}

	if err := repo.Save(dirty, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	// Record the newest task so the next add can chain onto it via ^.
	saveLastAddedID(created[len(created)-1].ID)
	if batch {
		return emitAddResultsBatch(created, *asJSON, *quietID)
	}
	return emitAddResult(&created[0], started, depTask, *asJSON, *quietID)
}

// readTitlesFromStdin returns one trimmed, non-empty title per line of r. Used
// by `taskr add -` for batch creation from a pipe or heredoc.
func readTitlesFromStdin(r io.Reader) ([]string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var titles []string
	for _, line := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			titles = append(titles, s)
		}
	}
	return titles, nil
}

// emitAddResultsBatch renders the outcome of a batch `taskr add -`: a JSON array
// under --json, one bare id per line under --quiet-id, else one human line each.
func emitAddResultsBatch(tasks []todo.Todo, asJSON, quietID bool) int {
	switch {
	case asJSON:
		return emitJSON(tasks)
	case quietID:
		for i := range tasks {
			fmt.Println(tasks[i].ID)
		}
	default:
		for i := range tasks {
			fmt.Printf("added %s  %s\n", tasks[i].ID[:8], tasks[i].Title)
		}
	}
	return 0
}

// emitAddResult renders the outcome of `taskr add`. --json emits the full
// created task so a script can read .id (or any other field); --quiet-id prints
// only the full UUID for shell capture (id=$(taskr add … --quiet-id)); otherwise
// the usual human line. started selects the --start variant's message. dep, when
// non-nil, triggers a follow-up confirmation line so the user can see that the
// dependency link took effect. Any --start "stopped:" notices already went to
// stderr, so stdout stays clean for the machine-readable modes.
func emitAddResult(t *todo.Todo, started bool, dep *todo.Todo, asJSON, quietID bool) int {
	switch {
	case asJSON:
		return emitJSON(t)
	case quietID:
		fmt.Println(t.ID)
	case started:
		fmt.Printf("added + started: %s  %s\n", t.ID[:8], t.Title)
	default:
		fmt.Printf("added %s  %s\n", t.ID[:8], t.Title)
	}
	if dep != nil && !asJSON && !quietID {
		fmt.Printf("blocked on %s  %s\n", dep.ID[:8], dep.Title)
	}
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
	tag := fs.String("tag", "", "only tasks carrying this tag (case-insensitive)")
	project := fs.String("project", "", "only tasks in this project")
	search := fs.String("search", "", "only tasks whose title contains this substring")
	ready := fs.Bool("ready", false, "only actionable pending tasks (no unfinished dependencies)")
	blocked := fs.Bool("blocked", false, "only tasks blocked by at least one unfinished dependency")
	flagArgs, _ := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	opts := listFilterOpts{
		includeDone: *all,
		focus:       *focus,
		tag:         *tag,
		project:     *project,
		search:      *search,
		onlyReady:   *ready,
		onlyBlocked: *blocked,
	}
	rows := filterTopLevel(todos, opts)
	sortTodosByMode(rows, taskSortSequence)
	if *limit > 0 && len(rows) > *limit {
		rows = rows[:*limit]
	}
	if *asJSON {
		return emitJSON(rows)
	}
	// A bare "(no tasks)" after finishing a project is indistinguishable from
	// a typo'd filter. When completed tasks match the same filters, say so —
	// the project exists, the work is just done.
	if len(rows) == 0 && !*all {
		withDone := opts
		withDone.includeDone = true
		if hidden := len(filterTopLevel(todos, withDone)); hidden > 0 {
			fmt.Printf("(no pending tasks — %d done match; use --all to see them)\n", hidden)
			return 0
		}
	}
	blockedSet := buildBlockedSet(todos)
	printTaskTable(rows, blockedSet)
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
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
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
	blockedSet := buildBlockedSet(todos)
	printTaskTable(rows, blockedSet)
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

// pendingDescendants returns every still-pending, non-deleted subtask beneath
// rootID (transitive). `done` uses it to avoid silently orphaning open subtasks
// when their parent is closed — they'd otherwise vanish from every list and the
// stats until only `export` could see them (e71788f0).
func pendingDescendants(children func(string) []string, get func(string) *todo.Todo, rootID string) []*todo.Todo {
	var out []*todo.Todo
	for _, id := range descendantIDsFrom(children, rootID)[1:] { // [0] is rootID itself
		if s := get(id); s != nil && s.Status == todo.Pending && !s.Deleted {
			out = append(out, s)
		}
	}
	return out
}

func cliDone(args []string) int {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var comment string
	fs.StringVar(&comment, "comment", "", "append this comment to each task transitioned to done")
	fs.StringVar(&comment, "m", "", "shorthand for --comment (git muscle memory)")
	cascade := fs.Bool("cascade", false, "also close pending subtasks of each target (default: prompt on a TTY, else warn and leave them open)")
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr done <ref> [<ref>...] [-m \"why\"] [--cascade]")
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
	// The auto-close-subtasks preference makes cascade the default, matching
	// the TUI toggle; --cascade forces it regardless.
	settings, _ := loadSettings()
	autoCascade := *cascade || settings.AutoCloseSubtasks
	children, get := sliceTaskLookups(todos)
	var dirty, cascaded, skipped, stopped []*todo.Todo
	var spawned []todo.Todo
	closed := make(map[string]bool)
	var stdinScan *bufio.Scanner
	for _, t := range targets {
		if t.Status == todo.Done || closed[t.ID] {
			skipped = append(skipped, t)
			continue
		}
		// Guard (e71788f0): closing a parent with open subtasks hides them
		// everywhere but `export`. Decide whether to cascade, close the parent
		// only, or abort — mirroring the TUI's confirm-close-parent prompt.
		pending := pendingDescendants(children, get, t.ID)
		doCascade := autoCascade
		if len(pending) > 0 && !autoCascade {
			if stdinIsTTY() {
				if stdinScan == nil {
					stdinScan = bufio.NewScanner(os.Stdin)
				}
				fmt.Fprintf(os.Stderr, "%s %q has %d pending subtask(s). Close them too? [y]es / [n]o parent only / [a]bort: ",
					t.ID[:8], t.Title, len(pending))
				ans := ""
				if stdinScan.Scan() {
					ans = strings.ToLower(strings.TrimSpace(stdinScan.Text()))
				}
				switch ans {
				case "y", "yes":
					doCascade = true
				case "n", "no":
					// close the parent only
				default:
					fmt.Fprintf(os.Stderr, "aborted: %s left open\n", t.ID[:8])
					continue
				}
			} else {
				// Non-interactive: don't break scripts, but surface it loudly
				// so the subtasks aren't silently orphaned.
				fmt.Fprintf(os.Stderr, "warning: closing %s %q leaves %d pending subtask(s) hidden under it — rerun with --cascade to close them, or 'taskr done <subtask>'\n",
					t.ID[:8], t.Title, len(pending))
			}
		}
		// Comment lands BEFORE Toggle so the timeline reads "added the why,
		// then closed it" — and the comment timestamp matches the close.
		if comment != "" {
			t.AddComment(comment)
		}
		// Closing a task while its timer is running would leave a dangling
		// open entry; the TUI auto-stops, so the CLI matches.
		if t.IsTimerRunning() {
			t.StopTimer()
			stopped = append(stopped, t)
		}
		captureSeqRankAtDone(todos, t)
		t.Toggle()
		closed[t.ID] = true
		dirty = append(dirty, t)
		if t.IsRecurring() {
			if next, ok := buildNextRecurrence(*t); ok {
				spawned = append(spawned, next)
				// Clone the subtree onto the new parent so a recurring
				// "weekly review" keeps its checklist on each spawn. Same
				// delta-shifted semantics as the TUI path.
				var delta time.Duration
				if !t.DueDate.IsZero() && !next.DueDate.IsZero() {
					delta = next.DueDate.Sub(t.DueDate)
				}
				spawned = append(spawned, cloneSubtreeResetFrom(children, get, t.ID, next.ID, delta)...)
			}
		}
		if doCascade {
			for _, s := range pending {
				if s.Status == todo.Done || closed[s.ID] {
					continue
				}
				if s.IsTimerRunning() {
					s.StopTimer()
					stopped = append(stopped, s)
				}
				captureSeqRankAtDone(todos, s)
				s.Toggle()
				closed[s.ID] = true
				cascaded = append(cascaded, s)
			}
		}
	}
	saveSet := make([]*todo.Todo, 0, len(dirty)+len(cascaded)+len(spawned))
	saveSet = append(saveSet, dirty...)
	saveSet = append(saveSet, cascaded...)
	for i := range spawned {
		saveSet = append(saveSet, &spawned[i])
	}
	if len(saveSet) > 0 {
		if err := repo.Save(saveSet, nil); err != nil {
			fmt.Fprintf(os.Stderr, "save: %v\n", err)
			return 1
		}
	}
	for _, t := range stopped {
		fmt.Fprintf(os.Stderr, "stopped: %s  %s\n", t.ID[:8], t.Title)
	}
	for _, t := range dirty {
		fmt.Printf("done  %s  %s\n", t.ID[:8], t.Title)
	}
	for _, t := range cascaded {
		fmt.Printf("done  %s  %s  (subtask)\n", t.ID[:8], t.Title)
	}
	for _, t := range spawned {
		due := ""
		if !t.DueDate.IsZero() {
			due = "  due " + t.DueDate.Format("02-01-06")
		}
		fmt.Printf("recur %s  %s%s\n", t.ID[:8], t.Title, due)
	}
	for _, t := range skipped {
		fmt.Fprintf(os.Stderr, "already done: %s\n", t.Title)
	}
	return 0
}

// ── top ──────────────────────────────────────────────────────────────────────

// rankTopBySequence returns the top-level pending tasks ranked exactly as the
// TUI's Sequence sort ranks them (selectActiveDone): each task's base score
// lifted by the subtask and dependency critical-path rollups, so a parent
// inherits its subtasks' urgency and a blocker inherits the urgency of the work
// it holds up. The rollup is computed from the full set — it needs subtasks and
// dependency targets, not just the top-level rows. Pure; the caller applies any
// -n limit. `taskr top`'s displayed SCORE stays each task's own score (matching
// the TUI); only the ordering reflects the boost.
func rankTopBySequence(todos []todo.Todo) []todo.Todo {
	return rankTopBySequenceBy(todos, sequenceScore)
}

// rankTopBySequenceBy is the shared implementation behind rankTopBySequence and
// rankTopBySequenceWith. It accepts an arbitrary score function so callers can
// supply explicit biases/clock (the preview path) or the live globals (the CLI
// and TUI paths). The rollup and sort logic — subtask inheritance, critical-path
// dependency boost, fan-out bonus, cycle-safe DFS — is identical for both.
func rankTopBySequenceBy(todos []todo.Todo, score func(*todo.Todo) float64) []todo.Todo {
	rows := make([]todo.Todo, 0, len(todos))
	for _, t := range todos {
		if t.ParentID == "" && t.Status == todo.Pending {
			rows = append(rows, t)
		}
	}
	rollup := descendantScoreRollupWith(todos, score)
	rollup = dependencyScoreRollupWith(todos, rollup, score)
	sortTodosBySequenceWithRollupBy(rows, rollup, score)
	return rows
}

func cliTop(args []string) int {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	n := fs.Int("n", 10, "rows to show")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	wide := fs.Bool("wide", false, "include priority, due date, and tags columns")
	flagArgs, _ := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	rows := rankTopBySequence(todos)
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
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
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
	printTaskDetail(t, subs, todos)
	return 0
}

func printTaskDetail(t *todo.Todo, subs []todo.Todo, todos []todo.Todo) {
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
		layout := "2006-01-02"
		if !t.StartDate.Equal(startOfDay(t.StartDate)) {
			layout = "2006-01-02 15:04"
		}
		fmt.Printf("Start:    %s\n", t.StartDate.Format(layout))
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
		fmt.Printf("Score:    %.1f  (Deadline %.1f · Priority %.1f · Momentum %.1f · Size %.1f · Age %.1f)\n",
			sc.Total, sc.Urgency, sc.Importance, sc.Momentum, sc.Size, sc.Age)
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
	// One merged dependency list, direction carried by the glyph: ↧ = this
	// task waits on it (outbound, stored on t), ↥ = it waits on this task
	// (inbound, derived). Mirrors the TUI detail pane.
	inbound := dependentsOf(todos, t.ID)
	if len(t.Dependencies) > 0 || len(inbound) > 0 {
		fmt.Printf("\nDependencies (%d):\n", len(t.Dependencies)+len(inbound))
		for _, dep := range t.Dependencies {
			// Resolve to a title where possible; fall back to the raw id for
			// dangling references.
			line := "  - ↧ " + dep
			for i := range todos {
				if todos[i].ID == dep {
					marker := "[ ]"
					if todos[i].Status == todo.Done {
						marker = "[✓]"
					}
					line = fmt.Sprintf("  %.8s  %s ↧ %s", dep, marker, todos[i].Title)
					break
				}
			}
			fmt.Println(line)
		}
		for _, d := range inbound {
			fmt.Printf("  %.8s      ↥ %s\n", d.ID, d.Title)
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

func cliEdit(args []string) int {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	title := fs.String("title", "", "new title")
	// --priority is an alias for --p; both share the same destination.
	var editPriorityVal string
	fs.StringVar(&editPriorityVal, "p", "", "new priority: h|m|l")
	fs.StringVar(&editPriorityVal, "priority", "", "new priority: h|m|l (alias for --p)")
	priority := &editPriorityVal
	size := fs.String("size", "", "new size: s|m|l")
	due := fs.String("due", "", "set due date (today|tomorrow|+3d|dd-mm-yy|...)")
	clearDue := fs.Bool("clear-due", false, "drop the due date")
	start := fs.String("start", "", "set start date")
	clearStart := fs.Bool("clear-start", false, "drop the start date")
	project := fs.String("project", "", "set project name")
	clearProject := fs.Bool("clear-project", false, "drop the project")
	addTag := fs.String("add-tag", "", "comma-separated tags to add")
	removeTag := fs.String("remove-tag", "", "comma-separated tags to remove")
	addDep := fs.String("add-dep", "", "add a dependency (ref to an existing task; refused if it would loop)")
	removeDep := fs.String("remove-dep", "", "remove a dependency (ref to a currently-depended-on task)")
	note := fs.String("note", "", "replace the task's notes ('-' reads from stdin)")
	appendNote := fs.String("append-note", "", "append a paragraph to the task's notes ('-' reads from stdin)")
	clearNote := fs.Bool("clear-note", false, "drop the notes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: taskr edit <id-prefix> [flags]")
		fs.PrintDefaults()
	}
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
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
		t.ModifiedAt = todo.StampModified(t.ModifiedAt)
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
		t.ModifiedAt = todo.StampModified(t.ModifiedAt)
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
		t.ModifiedAt = todo.StampModified(t.ModifiedAt)
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
	if *addDep != "" {
		dep, err := findTaskByRef(todos, *addDep)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		// Refuse a dependency that would close a loop: dep already (transitively)
		// depends on t, or is t itself. Same rule the TUI picker filters by.
		byID := make(map[string]*todo.Todo, len(todos))
		for i := range todos {
			byID[todos[i].ID] = &todos[i]
		}
		if loopingDepCandidates(byID, t.ID)[dep.ID] {
			fmt.Fprintf(os.Stderr, "taskr edit: %q can't depend on %q — it would create a dependency loop\n", t.Title, dep.Title)
			return 2
		}
		t.AddDependency(dep.ID)
		changed = true
	}
	if *removeDep != "" {
		dep, err := findTaskByRef(todos, *removeDep)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		t.RemoveDependency(dep.ID)
		changed = true
	}
	// Notes: clear wins, then replace, then append (a new paragraph). Notes
	// were previously settable only at `add` and editable only via the TUI's
	// $EDITOR flow. '-' reads the text from stdin, mirroring `comment -`.
	switch {
	case *clearNote:
		t.SetNotes("")
		changed = true
	case *note != "":
		text, terr := noteFlagText(*note, os.Stdin)
		if terr != nil {
			fmt.Fprintf(os.Stderr, "stdin: %v\n", terr)
			return 1
		}
		t.SetNotes(text)
		changed = true
	case *appendNote != "":
		text, terr := noteFlagText(*appendNote, os.Stdin)
		if terr != nil {
			fmt.Fprintf(os.Stderr, "stdin: %v\n", terr)
			return 1
		}
		if t.Notes == "" {
			t.SetNotes(text)
		} else {
			t.SetNotes(t.Notes + "\n\n" + text)
		}
		changed = true
	}
	if !changed {
		fmt.Fprintln(os.Stderr, "taskr edit: no fields changed (nothing to save)")
		return 0
	}
	saveSet := []*todo.Todo{t}
	// If a subtask's due moved later, walk up and extend every ancestor
	// whose due falls short of the child's — mirrors the TUI flow.
	if *due != "" && t.ParentID != "" {
		_, get := sliceTaskLookups(todos)
		saveSet = append(saveSet, extendAncestorsDue(get, t)...)
	}
	if err := repo.Save(saveSet, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("edited  %s  %s\n", t.ID[:8], t.Title)
	// Ancestor due-date bumps are side-effects of the edit, not its result —
	// stderr, so `edit --json`-style scripting on stdout stays clean.
	for _, a := range saveSet[1:] {
		fmt.Fprintf(os.Stderr, "bumped  %s  %s  due → %s\n", a.ID[:8], a.Title, a.DueDate.Format("02-01-06"))
	}
	return 0
}

// ── delete ───────────────────────────────────────────────────────────────────

func cliDelete(args []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	force := fs.Bool("f", false, "skip the confirmation prompt (title-substring matches only; id refs never prompt)")
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) != 1 {
		fmt.Fprintln(os.Stderr, "usage: taskr delete <ref> [-f]")
		return 2
	}
	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	t, kind, err := findTaskByRefKind(todos, positionals[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	children, get := sliceTaskLookups(todos)
	ids := descendantIDsFrom(children, t.ID)
	// A title-substring ref is fuzzy — the match may not be the task the user
	// meant, and delete is the one verb where that's expensive. Confirm on the
	// fuzzy path only; exact id/prefix refs stay script-fast, and -f opts out.
	if kind == refMatchTitle && !*force {
		what := fmt.Sprintf("delete %s  %q", t.ID[:8], t.Title)
		if extra := len(ids) - 1; extra > 0 {
			what += fmt.Sprintf(" (+%d subtask(s))", extra)
		}
		if !stdinIsTTY() {
			fmt.Fprintf(os.Stderr, "taskr delete: %q matched by title substring and confirmation needs a terminal — use the id prefix %s, or -f\n",
				positionals[0], t.ID[:8])
			return 2
		}
		if !confirmStdin(what + "?") {
			fmt.Fprintln(os.Stderr, "aborted")
			return 1
		}
	}
	// Soft delete via the Repository contract — the row is tombstoned and
	// will not load again. Matches the TUI's delete semantics: cascade to
	// every descendant so subtasks don't get stranded with a parent_id
	// pointing at a tombstone.
	if err := repo.Save(nil, ids); err != nil {
		fmt.Fprintf(os.Stderr, "delete: %v\n", err)
		return 1
	}
	// Record the pre-delete states in the undo sidecar so `taskr undo` (and
	// the TUI, which seeds its undo stack from the same file) can restore
	// this. Best-effort: a persist failure must not fail the delete — but the
	// "(recoverable…)" hint below is printed only when recording succeeded.
	entry := undoEntry{desc: undoDescDeleteTask, ids: ids}
	for _, id := range ids {
		if x := get(id); x != nil {
			entry.partial = append(entry.partial, copyTodo(*x))
		}
	}
	recorded := recordDeleteUndo(entry)
	if extra := len(ids) - 1; extra > 0 {
		noun := "subtask"
		if extra != 1 {
			noun = "subtasks"
		}
		fmt.Printf("deleted %s  %s  (+%d %s)\n", t.ID[:8], t.Title, extra, noun)
	} else {
		fmt.Printf("deleted %s  %s\n", t.ID[:8], t.Title)
	}
	if recorded {
		fmt.Fprintln(os.Stderr, "(recoverable with `taskr undo`)")
	}
	return 0
}

// ── undelete ─────────────────────────────────────────────────────────────────

// cliUndelete restores a soft-deleted task (and any deleted descendants) from
// the tombstones the delete verb leaves in SQLite. Where `undo` pops the most
// recent deletion LIFO, undelete targets a specific task by ref and `--list`
// browses what's recoverable. Tombstones live for tombstoneRetention (180 days),
// so anything inside that window can be brought back.
func cliUndelete(args []string) int {
	fs := flag.NewFlagSet("undelete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	list := fs.Bool("list", false, "list the deleted tasks that can be restored instead of restoring one")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: taskr undelete <ref>   |   taskr undelete --list")
		fs.PrintDefaults()
	}
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	if err := openStore(); err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		return 1
	}
	all, err := loadTodosForSync(db) // includes tombstones (Deleted/DeletedAt set)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	var deleted []todo.Todo
	for i := range all {
		if all[i].Deleted {
			deleted = append(deleted, all[i])
		}
	}

	if *list || len(positionals) == 0 {
		if len(deleted) == 0 {
			fmt.Println("(no deleted tasks to restore)")
			return 0
		}
		sort.Slice(deleted, func(i, j int) bool { // newest deletions first
			return deleted[i].DeletedAt.After(deleted[j].DeletedAt)
		})
		for i := range deleted {
			when := "unknown"
			if !deleted[i].DeletedAt.IsZero() {
				when = deleted[i].DeletedAt.Format("2006-01-02 15:04")
			}
			fmt.Printf("%s  %s  (deleted %s)\n", deleted[i].ID[:8], deleted[i].Title, when)
		}
		return 0
	}

	t, err := findTaskByRef(deleted, positionals[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	// Restore the task and any of its deleted descendants — delete cascades, so a
	// subtree went down together and comes back together. Bump ModifiedAt so the
	// un-delete wins over the tombstone in a later sync merge. StampModified is
	// clamped against the tombstone's DeletedAt so a slow local clock can't stamp
	// an older ModifiedAt that loses to the deletion in the merge.
	children, get := sliceTaskLookups(all)
	restored := make([]todo.Todo, 0, 1)
	for _, id := range descendantIDsFrom(children, t.ID) {
		x := get(id)
		if x == nil || !x.Deleted {
			continue
		}
		c := copyTodo(*x)
		c.Deleted = false
		c.ModifiedAt = todo.StampModified(x.DeletedAt)
		c.DeletedAt = time.Time{}
		restored = append(restored, c)
	}
	// If the restored root's parent is itself still deleted, detach it so it
	// doesn't come back stranded under a tombstone (descendantIDsFrom yields the
	// root first, so restored[0] is it).
	if len(restored) > 0 && restored[0].ParentID != "" {
		if p := get(restored[0].ParentID); p == nil || p.Deleted {
			restored[0].ParentID = ""
		}
	}

	ptrs := make([]*todo.Todo, len(restored))
	for i := range restored {
		ptrs[i] = &restored[i]
	}
	repo := newSQLiteRepo()
	if err := repo.Save(ptrs, nil); err != nil {
		fmt.Fprintf(os.Stderr, "undelete: %v\n", err)
		return 1
	}
	if extra := len(restored) - 1; extra > 0 {
		noun := "subtask"
		if extra != 1 {
			noun = "subtasks"
		}
		fmt.Printf("restored %s  %s  (+%d %s)\n", t.ID[:8], t.Title, extra, noun)
	} else {
		fmt.Printf("restored %s  %s\n", t.ID[:8], t.Title)
	}
	return 0
}

// ── undo ─────────────────────────────────────────────────────────────────────

// cliUndo restores the most recent persisted deletion (task or subtask, with
// its captured descendants) from the undo sidecar — the same file the TUI
// seeds its cross-restart undo stack from, so a delete made in either surface
// is recoverable from either. Only deletions are persisted; everything else
// stays TUI-session undo.
func cliUndo(args []string) int {
	fs := flag.NewFlagSet("undo", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	list := fs.Bool("list", false, "show the restorable deletions and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	entries, err := loadPersistedUndoEntries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr undo: %v\n", err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "taskr undo: nothing to restore — only deletions are undoable from the CLI (last 5 kept)")
		return 1
	}
	if *list {
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			title := "(no captured tasks)"
			if len(e.partial) > 0 {
				title = e.partial[0].Title
			}
			marker := " "
			if i == len(entries)-1 {
				marker = "*" // next `taskr undo` restores this one
			}
			fmt.Printf("%s %s  %q  (%d task(s))\n", marker, e.desc, title, len(e.partial))
		}
		return 0
	}

	repo, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	live := make(map[string]bool, len(todos))
	for i := range todos {
		live[todos[i].ID] = true
	}
	entry := entries[len(entries)-1]
	var restore []*todo.Todo
	for i := range entry.partial {
		t := &entry.partial[i]
		if live[t.ID] {
			// Already back (restored in the TUI, or re-created): overwriting
			// the live row with the old snapshot would destroy newer edits.
			fmt.Fprintf(os.Stderr, "skipping %s  %s — already exists\n", t.ID[:8], t.Title)
			continue
		}
		// Stamp the restore as the latest write. The tombstone in the store
		// (and on other devices) carries a newer DeletedAt than the captured
		// pre-delete state, so without the bump the deletion would win the
		// next sync merge and quietly re-apply itself. Clamp against the live
		// tombstone's deleted_at, not just the snapshot's ModifiedAt: after a
		// slow-clock delete both the tombstone's stamp and
		// StampModified(pre-delete ModifiedAt) land on the same prev+1ms, and
		// an exact event-time tie resolves by content hash (laterWins) — a
		// coin flip the restore could lose.
		prev := t.ModifiedAt
		if d := tombstoneDeletedAt(db, t.ID); d.After(prev) {
			prev = d
		}
		t.ModifiedAt = todo.StampModified(prev)
		restore = append(restore, t)
	}
	if len(restore) == 0 {
		fmt.Fprintln(os.Stderr, "taskr undo: every task in the newest entry already exists — nothing to do")
		if perr := savePersistedUndoEntries(entries[:len(entries)-1]); perr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update undo history: %v\n", perr)
		}
		return 0
	}
	if err := repo.Save(restore, nil); err != nil {
		fmt.Fprintf(os.Stderr, "restore: %v\n", err)
		return 1
	}
	// Pop the consumed entry so the next undo reaches the one before it.
	if perr := savePersistedUndoEntries(entries[:len(entries)-1]); perr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update undo history: %v\n", perr)
	}
	for _, t := range restore {
		fmt.Printf("restored %s  %s\n", t.ID[:8], t.Title)
	}
	return 0
}

// stdinIsTTY reports whether stdin is an interactive terminal — the gate for
// asking a y/N question at all.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// confirmStdin prompts on stderr (stdout stays machine-clean) and accepts
// y/yes case-insensitively; anything else, or EOF, declines.
func confirmStdin(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
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
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
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
	// Sequence hit rate inputs: of the last seqHitWindow rank-stamped
	// completions, how many closed while in the engine's top seqHitTopN.
	SeqHitsRecent  int `json:"seq_hits_recent"`
	SeqRatedRecent int `json:"seq_rated_recent"`
	// Seq carries the --seq miss analysis in JSON output; nil (and omitted)
	// unless the flag was passed.
	Seq *seqAnalysis `json:"seq,omitempty"`
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
		if t.IsOverdueAt(now) {
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

// scopeForStats narrows the full task set to the top-level tasks matching the
// filters plus every descendant of a match — descendants carry the time
// entries the tracked-today scan reads, and computeStats skips them for the
// top-level counts anyway.
func scopeForStats(todos []todo.Todo, opts listFilterOpts) []todo.Todo {
	matched := make(map[string]bool)
	for _, t := range filterTopLevel(todos, opts) {
		matched[t.ID] = true
	}
	parentOf := make(map[string]string, len(todos))
	for i := range todos {
		parentOf[todos[i].ID] = todos[i].ParentID
	}
	inScope := func(id string) bool {
		for cur := id; cur != ""; cur = parentOf[cur] {
			if matched[cur] {
				return true
			}
		}
		return false
	}
	out := make([]todo.Todo, 0, len(todos))
	for i := range todos {
		if inScope(todos[i].ID) {
			out = append(out, todos[i])
		}
	}
	return out
}

func cliStats(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	format := fs.String("format", "text", "output format: text | json | waybar")
	seq := fs.Bool("seq", false, "append the sequence miss analysis (why completions closed outside the top-5)")
	tag := fs.String("tag", "", "restrict stats to tasks carrying this tag (case-insensitive)")
	project := fs.String("project", "", "restrict stats to tasks in this project")
	search := fs.String("search", "", "restrict stats to tasks whose title contains this substring")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_, todos, err := loadForCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	scoped := todos
	if *tag != "" || *project != "" || *search != "" {
		scoped = scopeForStats(todos, listFilterOpts{
			includeDone: true, // the done/tracked buckets need completed rows
			tag:         *tag,
			project:     *project,
			search:      *search,
		})
	}
	s := computeStats(scoped, time.Now())
	s.SeqHitsRecent, s.SeqRatedRecent = sequenceHitStats(scoped, seqHitWindow)
	if *seq {
		// Heat always reconstructs from the full set: completions outside the
		// filter still warmed their projects/tags at the time.
		a := analyzeSeqMisses(scoped, todos, seqHitWindow, activeBiases)
		s.Seq = &a
	}
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
		line := fmt.Sprintf("active %d · overdue %d · due today %d · due this week %d · done today %d · tracked today %s",
			s.Active, s.Overdue, s.DueToday, s.DueThisWeek, s.DoneToday, tracked)
		// Hidden until rank-stamped completions exist, so a fresh install
		// doesn't advertise a 0/0 metric.
		if s.SeqRatedRecent > 0 {
			line += fmt.Sprintf(" · seq hit %d%% (%d/%d top-%d)",
				100*s.SeqHitsRecent/s.SeqRatedRecent, s.SeqHitsRecent, s.SeqRatedRecent, seqHitTopN)
		}
		fmt.Println(line)
		if s.Seq != nil {
			fmt.Print("\n" + renderSeqAnalysisText(*s.Seq, activeBiases))
		}
		return 0
	}
}

// seqMissDisplayCap bounds the per-miss listing in the --seq text output; the
// full set is always in the JSON form.
const seqMissDisplayCap = 5

// renderSeqAnalysisText renders the stats --seq block: the hit/miss dimension
// table, the bias suggestion, and the most recent misses. The hit rate itself
// is already on the stats summary line above it, so it isn't repeated here.
func renderSeqAnalysisText(a seqAnalysis, b biases) string {
	if a.Rated == 0 {
		return "no rank-stamped completions yet — the analysis needs a few finished tasks\n"
	}
	misses := a.Rated - a.Hits
	if misses == 0 {
		return fmt.Sprintf("no misses in the last %d rated completions — every one closed as a top-%d pick\n", a.Rated, a.TopN)
	}
	var sb strings.Builder
	if a.Hits == 0 {
		fmt.Fprintf(&sb, "all %d rated completions closed outside the top-%d — no hits to compare against\n\n", a.Rated, a.TopN)
	}
	largest, largestAbs := -1, 0.0
	for d := range a.Gap {
		if abs := math.Abs(a.Gap[d]); abs > largestAbs {
			largest, largestAbs = d, abs
		}
	}
	sb.WriteString("             avg contribution at completion\n")
	fmt.Fprintf(&sb, "%-10s  %6s  %6s  %6s\n", "dimension", "hits", "misses", "gap")
	for d := range seqDimNames {
		marker := ""
		if d == largest && largestAbs >= 0.05 {
			marker = "  ◂ largest gap"
		}
		fmt.Fprintf(&sb, "%-10s  %6.1f  %6.1f  %+6.1f%s\n",
			seqDimNames[d], a.HitAvg[d], a.MissAvg[d], a.Gap[d], marker)
	}
	if hint := seqSuggestion(a, b); hint != "" {
		sb.WriteString("\n" + hint + "\n")
	}
	sb.WriteString("\nrecent misses:\n")
	for i, r := range a.Misses {
		if i == seqMissDisplayCap {
			fmt.Fprintf(&sb, "  … %d more (use --format=json for all)\n", len(a.Misses)-i)
			break
		}
		title := r.Title
		if len([]rune(title)) > 44 {
			title = string([]rune(title)[:43]) + "…"
		}
		line := fmt.Sprintf("  rank %3d  %-44s", r.Rank, title)
		if r.Weakest != "" {
			line += "  weakest: " + r.Weakest
		}
		sb.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	sb.WriteString("\n(dimensions recomputed at each completion's timestamp from current task fields and biases)\n")
	return sb.String()
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
	// time tracking and the CLI preserves that invariant via the shared
	// helper. Collect all touched tasks so they're flushed in one Save.
	dirty := []*todo.Todo{target}
	for _, t := range stopOtherRunningTimers(todos, target.ID) {
		dirty = append(dirty, t)
		// Side-effect notice → stderr, like add --start: stdout carries only
		// the primary result so scripts can parse it.
		fmt.Fprintf(os.Stderr, "stopped: %s  %s\n", t.ID[:8], t.Title)
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
			// Distinguish "nothing is tracking anywhere" from "a different
			// task is tracking" — the first is the common case (user just
			// typo'd or forgot a timer wasn't running) and deserves the same
			// message as the no-ref form.
			anyRunning := false
			for i := range todos {
				if todos[i].IsTimerRunning() {
					anyRunning = true
					break
				}
			}
			if !anyRunning {
				fmt.Fprintln(os.Stderr, "no task is currently tracking")
				return 0
			}
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

// ── log ──────────────────────────────────────────────────────────────────────

// cliLog backfills a closed time entry on a task — work the live timer didn't
// capture (forgot to start it, or the stale-timer recovery under-logged a
// session). Same input semantics as the TUI's 'T' shortcut via
// parseManualEntry: a bare duration ends now, a clock range is literal today.
func cliLog(args []string) int {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: taskr log <ref> <45m|1h30m|HH:MM-HH:MM>
  duration form ends now ("I just spent 45m on this")
  range form is taken literally on today (crosses midnight if end < start)`)
	}
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) != 2 {
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
	start, stop, err := parseManualEntry(positionals[1], time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr log: %v\n", err)
		return 2
	}
	t.AddTimeEntry(start, stop)
	if err := repo.Save([]*todo.Todo{t}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		return 1
	}
	fmt.Printf("logged %s on %s  %s  (%s–%s)\n", formatDuration(stop.Sub(start)),
		t.ID[:8], t.Title, start.Format("15:04"), stop.Format("15:04"))
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
	return emitJSON(exportEnvelope{
		Version:    1,
		ExportedAt: time.Now().UTC(),
		Tasks:      out,
	})
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
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
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
		s.InheritContextFrom(parent)
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
  taskr add "title" [flags]            add a new task (--like <ref> clones, --depends <ref>|^ blocks on, --start tracks)
  taskr add -                          batch add: one task per stdin line (flags apply to all; --chain links each
                                       line as depending on the previous — a plan typed in execution order)
  taskr list [flags]                   list pending top-level tasks (ST: [ ] ready, [~] blocked, [✓] done)
  taskr search "term" [flags]          title-substring search (includes done by default)
  taskr top [-n=N] [--json] [--wide]   show top-N by sequence score
  taskr show <ref> [--json]            full detail (incl. score breakdown + subtask IDs)
  taskr edit <ref> [flags]             change fields on one task (incl. --note/--append-note/--clear-note)
  taskr done <ref>... [-m "why"]       mark one or more tasks done, stopping any running timer on them
                                       (--cascade also closes pending subtasks; without it a parent with
                                       open subtasks prompts on a TTY, else warns and leaves them open)
                                       (-m/--comment adds a closing comment to each)
  taskr delete <ref> [-f]              soft-delete a task (alias: rm; substring matches confirm first)
  taskr undo [--list]                  restore the most recent deletion (task + subtasks)
  taskr undelete <ref> | --list        restore a specific deleted task by ref (browse with --list)
  taskr subtask <parent> "title"       create a subtask (--each for multiple titles)

Discovery:
  taskr tags [--json]                  pending tags with counts
  taskr projects [--json]              pending projects with counts
  taskr doctor [--list]                suggest dependency links from note refs + related titles (interactive)

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

Reporting / backup:
  taskr stats [--format=text|json|waybar]   one-line health summary (default text)
                                            (--tag/--project/--search scope the stats to matching tasks;
                                             --seq appends the sequence miss analysis: which score dimension
                                             buried the tasks you finished anyway, and a bias hint)
  taskr export [--include-done]             JSON snapshot (versioned envelope) to stdout
  taskr import <file>|-                     merge an export file into the local store (- = stdin)

Sync (cross-device):
  taskr serve [--listen=ADDR] [--token=T]   run the sync server (self-hosted; binds 127.0.0.1:8765 by default)
  taskr sync [--url=U] [--token=T] [--save]  push/pull once against a sync server (--save stores config)
                                            auto-sync runs on its own once configured (set "auto_sync":false in
                                            ~/.taskr/sync.json to disable); conflicts log to ~/.taskr/sync.log
  taskr sync --status                        print the last sync time/result (local only, no network)
  taskr sync --accept-stale                  rejoin after being offline past the deletion-memory window
                                            (~6 months; auto-sync pauses then so deleted tasks can't resurrect)
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
  --blocked       only tasks waiting on an unfinished dependency — ST [~] (list only)
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
// Which flags consume the next arg when written without an embedded `=`
// (e.g. `--due tomorrow`) is derived from fs itself: every registered flag
// whose Value does not implement the stdlib's boolFlag interface takes a
// value. Deriving it kills the old hand-maintained per-command maps, which
// had to mirror each FlagSet and would silently mis-parse when they drifted.
// Callers must therefore define all flags on fs BEFORE calling this.
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

// printTaskTable renders rows as a fixed-column table. blockedSet maps task IDs
// to true when the task is waiting on at least one unfinished dependency; those
// tasks receive the [~] glyph in the ST column instead of [ ].
func printTaskTable(rows []todo.Todo, blockedSet map[string]bool) {
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
		switch {
		case t.Status == todo.Done:
			st = "[✓]"
		case blockedSet[t.ID]:
			st = "[~]" // blocked: waiting on an unfinished dependency
		}
		due := ""
		if !t.DueDate.IsZero() {
			due = t.DueDate.Format("02-01-06")
		}
		// Lowercase the size letter to match the TUI list column — uppercase
		// "M" looked like a hotkey hint.
		sz := strings.ToLower(t.Size.Letter())
		if projW > 0 {
			fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %-*s  %s\n",
				t.ID[:8], st, sz, priorityLetter(t.Priority), due, projW, truncate(t.Project, projW), t.Title)
		} else {
			fmt.Printf("%-8s  %-3s  %-4s  %-3s  %-10s  %s\n",
				t.ID[:8], st, sz, priorityLetter(t.Priority), due, t.Title)
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
