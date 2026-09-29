package main

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/Iliorn/tjek/rank"
	"github.com/Iliorn/tjek/todo"
)

var anna = editor{name: "Anna"}

// historySave saves tasks as the app does, signed by, and returns the saved
// copies with the history the save handed back.
func historySave(t *testing.T, h *sql.DB, at time.Time, by editor, tasks []todo.Todo, tombstones ...string) []*todo.Todo {
	t.Helper()
	ptrs := make([]*todo.Todo, len(tasks))
	for i := range tasks {
		cp := copyTodo(tasks[i])
		ptrs[i] = &cp
	}
	var dead map[string]time.Time
	if len(tombstones) > 0 {
		dead = map[string]time.Time{}
		for _, id := range tombstones {
			dead[id] = at
		}
	}
	if err := saveStamped(h, ptrs, dead, rank.Default().Score, at, by); err != nil {
		t.Fatalf("saveStamped: %v", err)
	}
	return ptrs
}

func storedHistory(t *testing.T, h *sql.DB, id string) []todo.Event {
	t.Helper()
	all, err := loadTodosForSync(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, x := range all {
		if x.ID == id {
			return x.History
		}
	}
	t.Fatalf("task %s missing", id)
	return nil
}

func lastEvent(t *testing.T, h *sql.DB, id string) todo.Event {
	t.Helper()
	hist := storedHistory(t, h, id)
	if len(hist) == 0 {
		t.Fatal("no history recorded")
	}
	return hist[len(hist)-1]
}

// Each kind of change a person makes leaves one event naming it, signed by
// the device's person.
func TestSaveRecordsWhatHappenedToATask(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Pay rent")
	historySave(t, h, s0, anna, []todo.Todo{task})
	if e := lastEvent(t, h, task.ID); e.Action != todo.ActionCreated || e.Author != "Anna" || e.Source != "" {
		t.Fatalf("new task: %+v, want created by Anna", e)
	}

	task.SetPriority(todo.PriorityHigh)
	task.SetDueDate(s0.Add(48 * time.Hour))
	task.Tags = []string{"home"}
	historySave(t, h, s0.Add(time.Minute), anna, []todo.Todo{task})
	e := lastEvent(t, h, task.ID)
	if e.Action != todo.ActionEdited || !slices.Equal(e.Fields, []string{"priority", "due", todo.FieldTags}) {
		t.Fatalf("edit: %+v, want edited priority, due, tags", e)
	}

	task.Toggle()
	historySave(t, h, s0.Add(2*time.Minute), anna, []todo.Todo{task})
	if e := lastEvent(t, h, task.ID); e.Action != todo.ActionClosed || len(e.Fields) != 0 {
		t.Fatalf("close: %+v, want closed and nothing else", e)
	}

	task.Toggle()
	historySave(t, h, s0.Add(3*time.Minute), anna, []todo.Todo{task})
	if e := lastEvent(t, h, task.ID); e.Action != todo.ActionReopened {
		t.Fatalf("reopen: %+v", e)
	}

	historySave(t, h, s0.Add(4*time.Minute), anna, nil, task.ID)
	if e := lastEvent(t, h, task.ID); e.Action != todo.ActionDeleted || !e.At.Equal(s0.Add(4*time.Minute)) {
		t.Fatalf("delete: %+v", e)
	}

	historySave(t, h, s0.Add(5*time.Minute), anna, []todo.Todo{task})
	if e := lastEvent(t, h, task.ID); e.Action != todo.ActionRestored {
		t.Fatalf("restore: %+v", e)
	}
	if n := len(storedHistory(t, h, task.ID)); n != 6 {
		t.Fatalf("%d events, want one per save (6)", n)
	}
}

// A save that changes nothing a person would call a change (a timer tick, a
// new score) adds no line. The task is already started: a first timer sets
// the start date, which is a change.
func TestTimerOnlySaveRecordsNoEvent(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Write report")
	task.SetStartDate(s0.Add(-time.Hour))
	historySave(t, h, s0, anna, []todo.Todo{task})
	task.StartTimer()
	historySave(t, h, s0.Add(time.Minute), anna, []todo.Todo{task})
	task.StopTimer()
	historySave(t, h, s0.Add(2*time.Minute), anna, []todo.Todo{task})
	if n := len(storedHistory(t, h, task.ID)); n != 1 {
		t.Fatalf("%d events, want only the creation", n)
	}
}

// A change tjek made itself is recorded as automatic, and the CLI's are
// marked as the CLI's.
func TestEventSourceSaysHowTheChangeWasMade(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Parent")
	task.Auto = true
	historySave(t, h, s0, anna, []todo.Todo{task})
	if e := lastEvent(t, h, task.ID); e.Source != todo.SourceAuto || e.Author != "Anna" {
		t.Fatalf("auto: %+v, want automatic on Anna's device", e)
	}
	task.Auto = false
	task.SetNotes("Parent task")
	historySave(t, h, s0.Add(time.Minute), editor{name: "Anna", source: todo.SourceCLI}, []todo.Todo{task})
	if e := lastEvent(t, h, task.ID); e.Source != todo.SourceCLI {
		t.Fatalf("cli: %+v", e)
	}
}

// The save hands each task back with its whole stored history, which is what
// lets the app show the new line without reloading, including lines its copy
// never had (a task restored from an undo snapshot).
func TestSaveHandsBackTheFullHistory(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Plan trip")
	historySave(t, h, s0, anna, []todo.Todo{task})
	task.SetPriority(todo.PriorityLow)
	historySave(t, h, s0.Add(time.Minute), anna, []todo.Todo{task})

	stale := task // as an undo snapshot would hold it: no history at all
	stale.History = nil
	stale.SetPriority(todo.PriorityHigh)
	saved := historySave(t, h, s0.Add(2*time.Minute), anna, []todo.Todo{stale})
	if n := len(saved[0].History); n != 3 {
		t.Fatalf("handed back %d events, want all 3", n)
	}
}

// A sync merge writes the events it receives and records none of its own:
// the change already has its line, from the device that made it.
func TestMergeKeepsIncomingEventsAndAddsNone(t *testing.T) {
	h := openTestDB(t)
	task := todo.New("Shared task")
	historySave(t, h, s0, anna, []todo.Todo{task})

	remote := storedHistory(t, h, task.ID)
	all, err := loadTodosForSync(h)
	if err != nil {
		t.Fatal(err)
	}
	theirs := all[0]
	theirs.SetNotes("renamed elsewhere")
	theirs.History = append(slices.Clone(remote), todo.Event{
		ID: "from-mark", At: s0.Add(time.Minute), Author: "Mark", Action: todo.ActionEdited, Fields: []string{"notes"},
	})
	if _, _, err := mergeIntoStore(h, []todo.Todo{theirs}, rank.Biases{}); err != nil {
		t.Fatal(err)
	}
	hist := storedHistory(t, h, task.ID)
	if len(hist) != 2 || hist[1].ID != "from-mark" || hist[1].Author != "Mark" {
		t.Fatalf("history after merge: %+v, want Anna's creation and Mark's edit", hist)
	}
}

// The name a device signs with: TJEK_AUTHOR first, so a script or an agent
// can sign as itself, then the Settings name.
func TestAuthorNamePrefersTheEnvironment(t *testing.T) {
	t.Setenv("TJEK_AUTHOR", "")
	if got := authorName(appSettings{Name: " Mark "}); got != "Mark" {
		t.Errorf("settings name: %q", got)
	}
	t.Setenv("TJEK_AUTHOR", "Claude")
	if got := authorName(appSettings{Name: "Mark"}); got != "Claude" {
		t.Errorf("TJEK_AUTHOR: %q", got)
	}
}

// The CLI signs its changes as the CLI's, under the Settings name.
func TestCLIChangesAreSignedAsTheCLI(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("TJEK_AUTHOR", "")
	if err := saveSettings(appSettings{Name: "Mark"}); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() { cliAdd([]string{"Water plants"}) })
	_, todos, err := loadForCLI()
	if err != nil || len(todos) != 1 {
		t.Fatalf("load: %v, %d tasks", err, len(todos))
	}
	captureStdout(t, func() { cliDone([]string{todos[0].ID}) })
	_, todos, _ = loadForCLI()
	hist := todos[0].History
	if len(hist) != 2 {
		t.Fatalf("history %+v, want created and closed", hist)
	}
	for _, e := range hist {
		if e.Author != "Mark" || e.Source != todo.SourceCLI {
			t.Errorf("event %+v, want Mark via the CLI", e)
		}
	}
}

// Edits one person makes close together share a row, and edits right after
// a task was created are part of its creation; anyone else's edit, or a
// close, starts a new row.
func TestHistoryRowsFoldRunsOfEdits(t *testing.T) {
	at := func(min int) time.Time { return s0.Add(time.Duration(min) * time.Minute) }
	events := []todo.Event{
		{ID: "1", At: at(0), Author: "Anna", Action: todo.ActionCreated},
		{ID: "2", At: at(1), Author: "Anna", Action: todo.ActionEdited, Fields: []string{"due"}},
		{ID: "3", At: at(60), Author: "Anna", Action: todo.ActionEdited, Fields: []string{"due"}},
		{ID: "4", At: at(65), Author: "Anna", Action: todo.ActionEdited, Fields: []string{"priority", "due"}},
		{ID: "5", At: at(66), Author: "Mark", Action: todo.ActionEdited, Fields: []string{"title"}},
		{ID: "6", At: at(67), Author: "Mark", Action: todo.ActionClosed},
	}
	rows := historyRows(events)
	var got []string
	for _, r := range rows {
		got = append(got, r.author+": "+historyWhat(r))
	}
	want := []string{
		"Mark: closed",
		"Mark: changed title",
		"Anna: changed due date, priority",
		"Anna: created",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rows:\n%q\nwant\n%q", got, want)
	}
}

// The "who" column says how a change was made when it was not by hand in
// the app.
func TestHistoryWhoNamesTheSource(t *testing.T) {
	for _, c := range []struct {
		r    historyRow
		want string
	}{
		{historyRow{author: "Anna"}, "Anna"},
		{historyRow{author: "Anna", source: todo.SourceCLI}, "Anna · cli"},
		{historyRow{author: "Anna", source: todo.SourceAuto}, "automatic"},
	} {
		if got := historyWho(c.r); got != c.want {
			t.Errorf("historyWho(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}

// In the app, the row a save records shows on the task as soon as the save
// is done, without a reload, and is signed with the Settings name.
func TestAppShowsTheSavedEventWithoutAReload(t *testing.T) {
	setTestHome(t, t.TempDir())
	testStore(t)
	t.Setenv("TJEK_AUTHOR", "")
	if err := saveSettings(appSettings{Name: "Anna"}); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() { cliAdd([]string{"Water plants"}) })
	m := initialModel(newSQLiteRepo())
	m.termWidth, m.termHeight = 120, 40
	task := m.currentTodo()
	if task == nil || len(task.History) != 1 {
		t.Fatalf("loaded task %+v, want its creation in the history", task)
	}

	m = sendKey(t, m, "d")
	m, cmd := send(t, m, saveTickMsg{})
	for _, msg := range runCmd(cmd) {
		if done, ok := msg.(saveDoneMsg); ok {
			m, _ = send(t, m, done)
		}
	}
	hist := m.get(task.ID).History
	if len(hist) != 2 {
		t.Fatalf("history %+v, want the close added", hist)
	}
	if e := hist[1]; e.Action != todo.ActionClosed || e.Author != "Anna" || e.Source != "" {
		t.Errorf("close event %+v, want closed by Anna in the app", e)
	}
}
