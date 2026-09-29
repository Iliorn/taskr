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
	return &sharer{h: openTestDB(t), cfg: sharedConfig{Device: strings.ToLower(name)}, by: editor{name: name}}
}

func (s *sharer) save(t *testing.T, at time.Time, tasks ...todo.Todo) {
	t.Helper()
	historySave(t, s.h, at, s.by, tasks)
}

func (s *sharer) sync(t *testing.T, name string) sharedResult {
	t.Helper()
	p, ok := s.cfg.find(name)
	if !ok {
		t.Fatalf("%s does not share %q", s.by.name, name)
	}
	res, err := syncShared(s.h, s.cfg, p, s.by.name, rank.Biases{})
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

// Anna shares a project through a folder and Mark joins it: he gets its
// tasks and none of her others, and each one's changes reach the other with
// the history saying who made them.
func TestTwoPeopleShareAProjectThroughAFolder(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")

	trip := todo.New("Book the ferry")
	trip.Project = "Trip"
	private := todo.New("Dentist")
	anna.save(t, s0, trip, private)

	if _, err := startSharing(&anna.cfg, "Trip", folder); err != nil {
		t.Fatal(err)
	}
	if res := anna.sync(t, "Trip"); !res.wrote {
		t.Fatal("sharing wrote no file for Anna's device")
	}

	if _, err := joinShared(&mark.cfg, folder); err != nil {
		t.Fatal(err)
	}
	mark.sync(t, "Trip")
	got, ok := mark.task(t, trip.ID)
	if !ok || got.Title != "Book the ferry" {
		t.Fatalf("Mark did not receive the shared task: %+v", got)
	}
	if _, ok := mark.task(t, private.ID); ok {
		t.Fatal("a task outside the shared project reached Mark")
	}

	// Mark closes it; Anna sees it closed, and by whom.
	got.Toggle()
	mark.save(t, s0.Add(time.Minute), got)
	mark.sync(t, "Trip")
	anna.sync(t, "Trip")
	mine, _ := anna.task(t, trip.ID)
	if mine.Status != todo.Done {
		t.Fatal("Mark's close did not reach Anna")
	}
	last := mine.History[len(mine.History)-1]
	if last.Action != todo.ActionClosed || last.Author != "Mark" {
		t.Errorf("Anna's history ends %+v, want closed by Mark", last)
	}
}

// Two people editing different fields of one task at once both keep their
// edit, whichever file is read first.
func TestSharedEditsToDifferentFieldsBothSurvive(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	task := todo.New("Plan the route")
	task.Project = "Trip"
	anna.save(t, s0, task)
	startSharing(&anna.cfg, "Trip", folder)
	anna.sync(t, "Trip")
	joinShared(&mark.cfg, folder)
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

// Leaving removes the project's tasks from this device outright and keeps
// its file in the folder, so the others lose nothing; joining again brings
// every task back, history and all, and deletes nothing for anyone.
func TestLeavingRemovesTheTasksAndRejoiningBringsThemBack(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	task := todo.New("Pack")
	task.Project = "Trip"
	private := todo.New("Dentist")
	anna.save(t, s0, task)
	mark.save(t, s0, private)
	startSharing(&anna.cfg, "Trip", folder)
	anna.sync(t, "Trip")
	joinShared(&mark.cfg, folder)
	mark.sync(t, "Trip")
	own := mark.cfg.memberPath(mark.cfg.Projects[0])

	p, n, err := leaveShared(mark.h, &mark.cfg, "Trip", "Mark", rank.Biases{})
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
	if _, err := os.Stat(own); err != nil {
		t.Errorf("Mark's file left the folder (%v); it is kept so nothing is lost", err)
	}
	if _, _, err := leaveShared(mark.h, &mark.cfg, "Trip", "Mark", rank.Biases{}); err == nil {
		t.Error("leaving a project that is not shared should say so")
	}

	// Anna keeps working; Mark rejoins and gets it all, and she loses nothing.
	a, _ := anna.task(t, task.ID)
	a.AddComment("don't forget the charger")
	anna.save(t, s0.Add(time.Minute), a)
	anna.sync(t, "Trip")
	if _, err := joinShared(&mark.cfg, p.Folder); err != nil {
		t.Fatal(err)
	}
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
// ones this device removed by leaving: those travel through the folder.
func TestSyncServerSkipsSharedProjects(t *testing.T) {
	shared := todo.New("In the shared project")
	shared.Project = "Trip"
	left := todo.New("Removed by leaving")
	mine := todo.New("Private")
	c := sharedConfig{
		Projects: []sharedProject{{ID: "trip", Name: "Trip"}},
		Left:     []sharedLeft{{ID: "work", Name: "Work", Tasks: []string{left.ID}}},
	}
	got := c.withoutShared([]todo.Todo{shared, left, mine})
	if len(got) != 1 || got[0].ID != mine.ID {
		t.Fatalf("kept %d task(s), want only the private one", len(got))
	}
}

// A folder holds one project: sharing another there is refused, sharing the
// same name joins it, and a file from a newer tjek is refused rather than
// half-read.
func TestSharedFolderGuards(t *testing.T) {
	folder := t.TempDir()
	anna, mark := newSharer(t, "Anna"), newSharer(t, "Mark")
	if _, err := startSharing(&anna.cfg, "Trip", folder); err != nil {
		t.Fatal(err)
	}
	if _, err := startSharing(&mark.cfg, "Work", folder); err == nil {
		t.Error("a second project in the same folder was accepted")
	}
	if p, err := startSharing(&mark.cfg, "Trip", folder); err != nil || p.ID != anna.cfg.Projects[0].ID {
		t.Errorf("sharing the folder's own project should join it: %+v, %v", p, err)
	}
	if _, err := joinShared(&mark.cfg, folder); err == nil {
		t.Error("joining a project twice was accepted")
	}
	if _, err := startSharing(&anna.cfg, "Trip", t.TempDir()+"/missing"); err == nil {
		t.Error("a folder that does not exist was accepted")
	}

	newer := `{"format": 99, "project": "` + anna.cfg.Projects[0].ID + `", "tasks": []}`
	if err := os.WriteFile(filepath.Join(folder, sharedMemberPrefix+"future.json"), []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := anna.cfg.find("Trip")
	if _, err := syncShared(anna.h, anna.cfg, p, "Anna", rank.Biases{}); err == nil || !strings.Contains(err.Error(), "newer tjek") {
		t.Errorf("a file from a newer format: %v, want it refused", err)
	}
}

// A sync with nothing new leaves the device's file alone: in a synced folder
// every write is an upload to everyone.
func TestUnchangedSharedProjectIsNotRewritten(t *testing.T) {
	folder := t.TempDir()
	anna := newSharer(t, "Anna")
	task := todo.New("Book the ferry")
	task.Project = "Trip"
	anna.save(t, s0, task)
	startSharing(&anna.cfg, "Trip", folder)
	if !anna.sync(t, "Trip").wrote {
		t.Fatal("the first sync wrote nothing")
	}
	if anna.sync(t, "Trip").wrote {
		t.Error("an unchanged project rewrote its file")
	}
}

// `tjek share join` asks before handing over tasks this device already files
// under the project's name, and --merge goes ahead.
func TestCLIJoinAsksBeforeMergingALocalProject(t *testing.T) {
	folder := t.TempDir()
	setTestHome(t, t.TempDir())
	testStore(t)
	t.Setenv("TJEK_AUTHOR", "")
	captureStdout(t, func() { cliAdd([]string{"Mine already", "--project", "Trip"}) })

	manifest := `{"format": 1, "id": "trip-id", "name": "Trip", "created": "2026-09-29T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(folder, sharedManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	var code int
	captureStderr(t, func() { code = cliShare([]string{"join", folder}) })
	if code != 2 {
		t.Fatalf("join over a local project of the same name: exit %d, want 2", code)
	}
	if c, _ := loadSharedConfig(); len(c.Projects) != 0 {
		t.Fatal("the refused join was recorded anyway")
	}
	captureStdout(t, func() { code = cliShare([]string{"join", folder, "--merge"}) })
	if code != 0 {
		t.Fatalf("join --merge: exit %d", code)
	}
	c, _ := loadSharedConfig()
	if _, err := os.Stat(c.memberPath(c.Projects[0])); err != nil {
		t.Errorf("joining wrote no member file: %v", err)
	}
	captureStdout(t, func() { code = cliShare([]string{"leave", "Trip"}) })
	if c, _ := loadSharedConfig(); code != 0 || len(c.Projects) != 0 {
		t.Errorf("leave: exit %d, still shared: %+v", code, c.Projects)
	}
}

// S on a Projects row shares it through a folder; S on a shared one asks,
// and y leaves it and removes its tasks. The row carries the shared mark in
// between.
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
	if _, err := os.Stat(filepath.Join(folder, sharedManifestName)); err != nil {
		t.Fatalf("no manifest in the folder: %v", err)
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
	folder := t.TempDir()
	manifest := `{"format": 1, "id": "trip-id", "name": "Trip", "created": "2026-09-29T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(folder, sharedManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := todo.New("Mine already")
	mine.Project = "Trip"
	m := settingsModel(t)
	m.Store.add(mine)
	m.refreshCaches()

	m = openSetting(t, m, settingShareJoin)
	if m.mode != modeShareJoin {
		t.Fatalf("mode = %v, want modeShareJoin", m.mode)
	}
	m = script(t, m, folder, "enter")
	if m.mode != modeConfirm {
		t.Fatalf("join over a local project: mode = %v, want the question", m.mode)
	}
	m = sendKey(t, m, "y")
	if _, ok := m.shared.find("Trip"); !ok {
		t.Fatal("not joined after y")
	}
}
