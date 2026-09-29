package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Iliorn/tjek/rank"
	"github.com/Iliorn/tjek/todo"
)

// sharer is one person's device in a shared-project test: a store of their
// own and their shared.json, both in memory.
type sharer struct {
	h   *sql.DB
	cfg sharedConfig
	by  editor
}

func newSharer(t *testing.T, name string) *sharer {
	return &sharer{h: openTestDB(t), by: editor{name: name}}
}

func (s *sharer) save(t *testing.T, at time.Time, tasks ...todo.Todo) {
	t.Helper()
	historySave(t, s.h, at, s.by, tasks)
}

func (s *sharer) share(t *testing.T, name, folder string) sharedProject {
	t.Helper()
	p, err := startSharing(&s.cfg, name, folder)
	if err != nil {
		t.Fatalf("%s sharing %q: %v", s.by.name, name, err)
	}
	return p
}

func (s *sharer) join(t *testing.T, path string) sharedProject {
	t.Helper()
	p, err := joinShared(&s.cfg, path)
	if err != nil {
		t.Fatalf("%s joining %s: %v", s.by.name, path, err)
	}
	return p
}

func (s *sharer) sync(t *testing.T, name string) sharedResult {
	t.Helper()
	p, ok := s.cfg.find(name)
	if !ok {
		t.Fatalf("%s does not share %q", s.by.name, name)
	}
	res, err := syncShared(s.h, p, rank.Biases{})
	if err != nil {
		t.Fatalf("%s syncing %q: %v", s.by.name, name, err)
	}
	return res
}

func (s *sharer) task(t *testing.T, id string) (todo.Todo, bool) {
	t.Helper()
	all, err := loadTodosForSync(s.h)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range all {
		if x.ID == id {
			return x, true
		}
	}
	return todo.Todo{}, false
}

// tripTask is a task in the project the tests share.
func tripTask(title string) todo.Todo {
	t := todo.New(title)
	t.Project = "Trip"
	return t
}

// Anna shares a project in a file and Mark joins it: he gets its tasks and
// none of her others, and each one's changes reach the other with the
// history saying who made them.
func TestTwoPeopleShareAProjectInOneFile(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	trip, private := tripTask("Book the ferry"), todo.New("Dentist")
	anna.save(t, s0, trip, private)

	p := anna.share(t, "Trip", folder)
	if p.File != filepath.Join(folder, "Trip.tjek") {
		t.Fatalf("shared in %s, want Trip.tjek in the folder", p.File)
	}
	anna.sync(t, "Trip")
	entries, _ := os.ReadDir(folder)
	if len(entries) != 1 {
		t.Fatalf("the folder holds %d entries, want the one file", len(entries))
	}

	mark.join(t, p.File)
	mark.sync(t, "Trip")
	got, ok := mark.task(t, trip.ID)
	if !ok || got.Title != "Book the ferry" {
		t.Fatalf("Mark did not receive the shared task: %+v", got)
	}
	if _, ok := mark.task(t, private.ID); ok {
		t.Fatal("a task outside the shared project reached Mark")
	}

	got.Toggle()
	mark.save(t, s0.Add(time.Minute), got)
	mark.sync(t, "Trip")
	anna.sync(t, "Trip")
	mine, _ := anna.task(t, trip.ID)
	if mine.Status != todo.Done {
		t.Fatal("Mark's close did not reach Anna")
	}
	if last := mine.History[len(mine.History)-1]; last.Action != todo.ActionClosed || last.Author != "Mark" {
		t.Errorf("Anna's history ends %+v, want closed by Mark", last)
	}
}

// Two people editing different fields of one task at once both keep their
// edit, whichever of them syncs first.
func TestSharedEditsToDifferentFieldsBothSurvive(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	task := tripTask("Plan the route")
	anna.save(t, s0, task)
	p := anna.share(t, "Trip", folder)
	anna.sync(t, "Trip")
	mark.join(t, p.File)
	mark.sync(t, "Trip")

	a, _ := anna.task(t, task.ID)
	a.SetDueDate(s0.Add(72 * time.Hour))
	anna.save(t, s0.Add(time.Minute), a)
	m, _ := mark.task(t, task.ID)
	m.SetPriority(todo.PriorityHigh)
	mark.save(t, s0.Add(2*time.Minute), m)

	anna.sync(t, "Trip")
	mark.sync(t, "Trip")
	anna.sync(t, "Trip")
	for _, s := range []*sharer{anna, mark} {
		got, _ := s.task(t, task.ID)
		if got.DueDate.IsZero() || got.Priority != todo.PriorityHigh {
			t.Errorf("%s has due %v, priority %v; want both edits", s.by.name, got.DueDate, got.Priority)
		}
	}
}

// Two devices that wrote the file at once leave a conflict copy beside it,
// the way cloud services do; the next pass merges the copy in, writes the
// result, and removes the copy.
func TestConflictCopiesAreMergedAndRemoved(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	task := tripTask("Pack")
	anna.save(t, s0, task)
	p := anna.share(t, "Trip", folder)
	anna.sync(t, "Trip")
	mark.join(t, p.File)
	mark.sync(t, "Trip")

	// Both edit; Mark's write lands as the conflict copy, and the file keeps
	// the version they both started from.
	base, err := os.ReadFile(p.File)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := anna.task(t, task.ID)
	a.AddComment("passports")
	anna.save(t, s0.Add(time.Minute), a)
	m, _ := mark.task(t, task.ID)
	m.SetPriority(todo.PriorityHigh)
	mark.save(t, s0.Add(time.Minute), m)
	mark.sync(t, "Trip")
	copyPath := filepath.Join(folder, "Trip (Mark's conflicted copy 2026-09-29).tjek")
	if err := os.Rename(p.File, copyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.File, base, 0o644); err != nil {
		t.Fatal(err)
	}
	anna.sync(t, "Trip")

	got, _ := anna.task(t, task.ID)
	if got.Priority != todo.PriorityHigh || len(got.Comments) != 1 {
		t.Fatalf("Anna has priority %v and %d comment(s), want both edits", got.Priority, len(got.Comments))
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Error("the conflict copy is still there")
	}
	f, err := readSharedFile(p.File)
	if err != nil || len(f.Tasks) != 1 || f.Tasks[0].Priority != todo.PriorityHigh || len(f.Tasks[0].Comments) != 1 {
		t.Errorf("the file does not hold both edits after the merge (%v)", err)
	}
}

// A cloud service that lets a later write replace an earlier one loses that
// write only for a moment: the device that made it still holds it, sees the
// file lacks it, and writes it back.
func TestAnOverwrittenChangeComesBack(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	task := tripTask("Pack")
	anna.save(t, s0, task)
	p := anna.share(t, "Trip", folder)
	anna.sync(t, "Trip")
	mark.join(t, p.File)
	mark.sync(t, "Trip")
	stale, err := os.ReadFile(p.File)
	if err != nil {
		t.Fatal(err)
	}

	a, _ := anna.task(t, task.ID)
	a.SetNotes("the big suitcase")
	anna.save(t, s0.Add(time.Minute), a)
	anna.sync(t, "Trip")
	// Mark's client, not having seen it, writes his older copy over it.
	if err := os.WriteFile(p.File, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := anna.sync(t, "Trip"); !res.wrote {
		t.Fatal("Anna did not put her change back into the file")
	}
	mark.sync(t, "Trip")
	if got, _ := mark.task(t, task.ID); got.Notes != "the big suitcase" {
		t.Errorf("Mark has notes %q, want Anna's change", got.Notes)
	}
}

// Once two devices agree, neither rewrites the file: each would otherwise
// see the other's write as a change and answer it, forever.
func TestAgreeingDevicesStopWriting(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	task := tripTask("Pack")
	task.AddTag("bags")
	task.AddTag("abroad")
	task.AddComment("first")
	task.AddComment("second")
	anna.save(t, s0, task)
	p := anna.share(t, "Trip", folder)
	anna.sync(t, "Trip")
	mark.join(t, p.File)
	for i := 0; i < 2; i++ {
		mark.sync(t, "Trip")
		anna.sync(t, "Trip")
	}
	if mark.sync(t, "Trip").wrote || anna.sync(t, "Trip").wrote {
		t.Error("a device rewrote a file that already said what it holds")
	}
}

// Leaving removes the project's tasks from this device outright and leaves
// the file to the others; joining again brings every task back, history and
// all, and deletes nothing for anyone.
func TestLeavingRemovesTheTasksAndRejoiningBringsThemBack(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	task, private := tripTask("Pack"), todo.New("Dentist")
	anna.save(t, s0, task)
	mark.save(t, s0, private)
	p := anna.share(t, "Trip", folder)
	anna.sync(t, "Trip")
	mark.join(t, p.File)
	mark.sync(t, "Trip")

	_, n, err := leaveShared(mark.h, &mark.cfg, "Trip", rank.Biases{})
	if err != nil || n != 1 {
		t.Fatalf("leave: removed %d, %v; want the one task", n, err)
	}
	if len(mark.cfg.Projects) != 0 || len(mark.cfg.Left) != 1 {
		t.Fatalf("after leaving: shared %+v, left %+v", mark.cfg.Projects, mark.cfg.Left)
	}
	if _, ok := mark.task(t, task.ID); ok {
		t.Error("the project's task is still on Mark's device, tombstone or not")
	}
	if _, ok := mark.task(t, private.ID); !ok {
		t.Error("leaving removed a task outside the project")
	}
	if _, err := os.Stat(p.File); err != nil {
		t.Errorf("leaving took the file away from the others: %v", err)
	}
	if _, _, err := leaveShared(mark.h, &mark.cfg, "Trip", rank.Biases{}); err == nil {
		t.Error("leaving a project that is not shared should say so")
	}

	a, _ := anna.task(t, task.ID)
	a.AddComment("don't forget the charger")
	anna.save(t, s0.Add(time.Minute), a)
	anna.sync(t, "Trip")
	mark.join(t, p.File)
	if len(mark.cfg.Left) != 0 {
		t.Error("rejoining did not forget the removed tasks")
	}
	mark.sync(t, "Trip")
	anna.sync(t, "Trip")
	got, ok := mark.task(t, task.ID)
	if !ok || len(got.Comments) != 1 || len(got.History) == 0 {
		t.Fatalf("rejoined copy %+v, want the task with Anna's comment and its history", got)
	}
	if still, _ := anna.task(t, task.ID); still.Deleted {
		t.Fatal("Mark's leave and rejoin deleted the task for Anna")
	}
}

// The sync server neither gets nor gives a shared project's tasks, nor the
// ones this device removed by leaving: those travel through the file.
func TestSyncServerSkipsSharedProjects(t *testing.T) {
	shared, left, mine := tripTask("In the shared project"), todo.New("Removed by leaving"), todo.New("Private")
	c := sharedConfig{
		Projects: []sharedProject{{ID: "trip", Name: "Trip"}},
		Left:     []sharedLeft{{ID: "work", Name: "Work", Tasks: []string{left.ID}}},
	}
	got := c.withoutShared([]todo.Todo{shared, left, mine})
	if len(got) != 1 || got[0].ID != mine.ID {
		t.Fatalf("kept %d task(s), want only the private one", len(got))
	}
}

// Sharing and joining refuse what would go wrong later: joining twice, a
// missing folder, a folder holding more than one project to choose from, a
// file of a newer format; and a folder that already holds the project's file
// joins it rather than starting another.
func TestSharedFileGuards(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	p := anna.share(t, "Trip", folder)
	if q, err := startSharing(&mark.cfg, "Trip", folder); err != nil || q.ID != p.ID {
		t.Errorf("sharing the folder's own project should join it: %+v, %v", q, err)
	}
	if _, err := joinShared(&mark.cfg, p.File); err == nil {
		t.Error("joining a project twice was accepted")
	}
	if _, err := startSharing(&anna.cfg, "Work", filepath.Join(folder, "missing")); err == nil {
		t.Error("a folder that does not exist was accepted")
	}
	anna.share(t, "Work", folder)
	carl := newSharer(t, "Carl")
	if _, err := joinShared(&carl.cfg, folder); err == nil {
		t.Error("a folder with two projects was joined without saying which")
	}
	if q := carl.join(t, filepath.Join(folder, "Work.tjek")); q.Name != "Work" {
		t.Errorf("joined %q, want Work", q.Name)
	}

	newer := `{"format": 99, "id": "` + p.ID + `", "name": "Trip", "tasks": []}`
	if err := os.WriteFile(p.File, []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := syncShared(anna.h, p, rank.Biases{}); err == nil || !strings.Contains(err.Error(), "newer tjek") {
		t.Errorf("a file from a newer format: %v, want it refused", err)
	}
}

// A project's file name is its name, less what some system cannot hold.
func TestSharedFileName(t *testing.T) {
	for name, want := range map[string]string{
		"Trip":         "Trip.tjek",
		"Work: Q3/Q4?": "Work- Q3-Q4-.tjek",
		" .. ":         "project.tjek",
	} {
		if got := sharedFileName(name); got != want {
			t.Errorf("sharedFileName(%q) = %q, want %q", name, got, want)
		}
	}
}

// The copies each cloud service makes are found, and neither the file
// itself nor another project whose name merely starts the same.
func TestSharedConflictCopiesAreRecognised(t *testing.T) {
	folder := t.TempDir()
	for _, name := range []string{
		"Trip.tjek", "Trip-LAPTOP.tjek", "Trip (Mark's conflicted copy 2026-09-29).tjek",
		"Trip (1).tjek", "Tripod.tjek", "Trip.tjek.bak",
	} {
		if err := os.WriteFile(filepath.Join(folder, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, c := range sharedConflictCopies(filepath.Join(folder, "Trip.tjek")) {
		got = append(got, filepath.Base(c))
	}
	want := "Trip (1).tjek|Trip (Mark's conflicted copy 2026-09-29).tjek|Trip-LAPTOP.tjek"
	if strings.Join(got, "|") != want {
		t.Errorf("copies %q, want %q", got, want)
	}
}

// A pass with nothing new leaves the file alone: every write is an upload.
func TestUnchangedSharedProjectIsNotRewritten(t *testing.T) {
	folder := t.TempDir()
	anna := newSharer(t, "Anna")
	anna.save(t, s0, tripTask("Book the ferry"))
	anna.share(t, "Trip", folder)
	if !anna.sync(t, "Trip").wrote {
		t.Fatal("the first pass wrote nothing")
	}
	if anna.sync(t, "Trip").wrote {
		t.Error("an unchanged project rewrote its file")
	}
}

// writeSharedFile puts an empty shared project's file at path, as sharing
// from another device would have.
func writeSharedFile(t *testing.T, path, id, name string) {
	t.Helper()
	data, err := encodeSharedFile(sharedFile{Format: sharedFormat, ID: id, Name: name, Tasks: []todo.Todo{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// `tjek share join` asks before handing over tasks this device already files
// under the project's name, and --merge goes ahead.
func TestCLIJoinAsksBeforeMergingALocalProject(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Trip.tjek")
	setTestHome(t, t.TempDir())
	testStore(t)
	t.Setenv("TJEK_AUTHOR", "")
	captureStdout(t, func() { cliAdd([]string{"Mine already", "--project", "Trip"}) })
	writeSharedFile(t, file, "trip-id", "Trip")

	var code int
	captureStderr(t, func() { code = cliShare([]string{"join", file}) })
	if code != 2 {
		t.Fatalf("join over a local project of the same name: exit %d, want 2", code)
	}
	if c, _ := loadSharedConfig(); len(c.Projects) != 0 {
		t.Fatal("the refused join was recorded anyway")
	}
	captureStdout(t, func() { code = cliShare([]string{"join", file, "--merge"}) })
	if code != 0 {
		t.Fatalf("join --merge: exit %d", code)
	}
	f, err := readSharedFile(file)
	if err != nil || len(f.Tasks) != 1 {
		t.Errorf("the file holds %d task(s) after joining with --merge (%v), want the local one", len(f.Tasks), err)
	}
	captureStdout(t, func() { code = cliShare([]string{"leave", "Trip"}) })
	if c, _ := loadSharedConfig(); code != 0 || len(c.Projects) != 0 {
		t.Errorf("leave: exit %d, still shared: %+v", code, c.Projects)
	}
}

// S on a Projects row shares it in a file; S on a shared one asks, and y
// leaves it and removes its tasks. The row carries the shared mark between.
func TestScriptShareAndLeaveFromTheProjectsTab(t *testing.T) {
	folder := t.TempDir()
	setTestHome(t, t.TempDir())
	testStore(t)
	captureStdout(t, func() { cliAdd([]string{"Book the ferry", "--project", "Trip"}) })
	m := initialModel(newSQLiteRepo())
	m.termWidth, m.termHeight = 120, 40
	m.tab = tabProjects
	m.refreshCaches()

	m = sendKey(t, m, "S")
	if m.mode != modeShareFolder {
		t.Fatalf("S: mode = %v, want modeShareFolder", m.mode)
	}
	m = script(t, m, folder, "enter")
	if m.mode != modeNormal {
		t.Fatalf("after enter: mode = %v (%s)", m.mode, m.err)
	}
	if _, ok := m.shared.find("Trip"); !ok {
		t.Fatal("the project is not shared after enter")
	}
	if c, _ := loadSharedConfig(); len(c.Projects) != 1 {
		t.Fatal("shared.json does not record the project")
	}
	if _, err := os.Stat(filepath.Join(folder, "Trip.tjek")); err != nil {
		t.Fatalf("no Trip.tjek in the folder: %v", err)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Trip"+sharedMark) {
		t.Error("the Projects row does not show the shared mark")
	}

	m = sendKey(t, m, "S")
	if m.mode != modeConfirm {
		t.Fatalf("S on a shared project: mode = %v, want the leave prompt", m.mode)
	}
	m, cmd := sendKeyCmd(t, m, keyMsgFor("y"))
	if _, ok := m.shared.find("Trip"); ok {
		t.Fatal("still shared after y")
	}
	for _, msg := range runCmd(cmd) {
		if r, ok := msg.(reloadedMsg); ok {
			m, _ = send(t, m, r)
		}
	}
	if n := len(m.allTodos()); n != 0 {
		t.Errorf("%d task(s) left after leaving, want the project's removed", n)
	}
}

// Settings → Join a project asks before sharing tasks already filed under
// the project's name, and joins on y.
func TestScriptJoinFromSettingsAsksFirst(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Trip.tjek")
	writeSharedFile(t, file, "trip-id", "Trip")
	mine := tripTask("Mine already")
	m := settingsModel(t)
	m.Store.add(mine)
	m.refreshCaches()

	m = openSetting(t, m, settingShareJoin)
	if m.mode != modeShareJoin {
		t.Fatalf("mode = %v, want modeShareJoin", m.mode)
	}
	m = script(t, m, file, "enter")
	if m.mode != modeConfirm {
		t.Fatalf("join over a local project: mode = %v, want the question", m.mode)
	}
	m = sendKey(t, m, "y")
	if _, ok := m.shared.find("Trip"); !ok {
		t.Fatal("not joined after y")
	}
}
