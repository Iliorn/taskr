package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"taskr/tasksync"
	"taskr/todo"
)

// modelWithTasks builds a fakeRepo-backed model pre-seeded with `tasks` so
// tests can exercise list-pane behavior without touching real storage. Sets
// a sane terminal size so layout-dependent paths (cursor clamping, list
// height) have realistic bounds.
func modelWithTasks(t *testing.T, tasks ...todo.Todo) model {
	t.Helper()
	m := initialModel(&fakeRepo{todos: tasks})
	m.termWidth = 120
	m.termHeight = 40
	m.ensureCache()
	return m
}

// sendKey is the test analogue of "the user pressed this key". Tea's
// KeyMsg uses Runes for printable chars and a typed Type for special keys
// (Tab, Enter, arrows, …) — wrap both shapes here so test bodies stay
// readable.
func sendKey(t *testing.T, m model, k string) model {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.Update(msg)
	return next.(model)
}

// ── Sort cycle ────────────────────────────────────────────────────────────────

func TestSortCycleVisitsAllThreeModes(t *testing.T) {
	m := modelWithTasks(t)

	if m.taskSort != taskSortSequence {
		t.Fatalf("initial sort = %v, want Sequence", m.taskSort)
	}
	m = sendKey(t, m, "s")
	if m.taskSort != taskSortDueDate {
		t.Errorf("after 1×s: sort = %v, want DueDate", m.taskSort)
	}
	m = sendKey(t, m, "s")
	if m.taskSort != taskSortSize {
		t.Errorf("after 2×s: sort = %v, want Size", m.taskSort)
	}
	m = sendKey(t, m, "s")
	if m.taskSort != taskSortSequence {
		t.Errorf("after 3×s: sort = %v, want Sequence (wrap)", m.taskSort)
	}
}

func TestSortCycleResetsCursorAndOffset(t *testing.T) {
	m := modelWithTasks(t, todo.New("a"), todo.New("b"), todo.New("c"))
	m.cursor = 2
	m.listOffset = 1

	m = sendKey(t, m, "s")
	if m.cursor != 0 || m.listOffset != 0 {
		t.Errorf("sort cycle should reset cursor/offset, got cursor=%d offset=%d",
			m.cursor, m.listOffset)
	}
}

// ── Tab switching ────────────────────────────────────────────────────────────

func TestTabSwitchByNumberKey(t *testing.T) {
	m := modelWithTasks(t)

	cases := []struct {
		key  string
		want tab
	}{
		{"1", tabTasks},
		{"2", tabCalendar},
		{"3", tabProjects},
		{"4", tabTags},
		{"5", tabLearnings},
		{"6", tabStats},
		{"7", tabSettings},
	}
	for _, c := range cases {
		m = sendKey(t, m, c.key)
		if m.tab != c.want {
			t.Errorf("key %q: tab = %v, want %v", c.key, m.tab, c.want)
		}
	}
}

func TestTabKeyAdvancesThroughTabs(t *testing.T) {
	m := modelWithTasks(t)
	if m.tab != tabTasks {
		t.Fatalf("initial tab = %v, want Tasks", m.tab)
	}
	m = sendKey(t, m, "tab")
	if m.tab != tabCalendar {
		t.Errorf("after 1 tab: %v, want Calendar", m.tab)
	}
	// numTabs presses from Tasks bring us back to Tasks (full wrap). We've
	// already sent one, so send numTabs-1 more.
	for i := 0; i < numTabs-1; i++ {
		m = sendKey(t, m, "tab")
	}
	if m.tab != tabTasks {
		t.Errorf("after %d tabs: %v, want Tasks (full wrap)", numTabs, m.tab)
	}
}

// ── Cursor navigation ────────────────────────────────────────────────────────

func TestCursorArrowsMoveCursor(t *testing.T) {
	m := modelWithTasks(t, todo.New("a"), todo.New("b"), todo.New("c"))

	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m = sendKey(t, m, "down")
	if m.cursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", m.cursor)
	}
	m = sendKey(t, m, "down")
	if m.cursor != 2 {
		t.Errorf("after 2×down: cursor = %d, want 2", m.cursor)
	}
	m = sendKey(t, m, "up")
	if m.cursor != 1 {
		t.Errorf("after up: cursor = %d, want 1", m.cursor)
	}
}

func TestCursorWrapsAroundListBounds(t *testing.T) {
	m := modelWithTasks(t, todo.New("a"), todo.New("b"))

	// up at top: cursor wraps to the last row.
	m = sendKey(t, m, "up")
	if m.cursor != 1 {
		t.Errorf("up at top: cursor = %d, want 1 (wraps to bottom)", m.cursor)
	}
	// down at bottom: cursor wraps back to the top.
	m = sendKey(t, m, "down")
	if m.cursor != 0 {
		t.Errorf("down at bottom: cursor = %d, want 0 (wraps to top)", m.cursor)
	}
}

// ── Reopen confirm ───────────────────────────────────────────────────────────

// Un-marking a done task must go through a "Move to active?" confirm rather
// than reopening on a single stray 'd'. Marking done stays immediate.
func TestReopenDoneTaskConfirms(t *testing.T) {
	task := todo.New("finished")
	task.ID = "f"
	task.Status = todo.Done
	m := modelWithTasks(t, task)
	m.showHistory = true // done tasks live in the history list
	m.cursor = 0

	m = sendKey(t, m, "d")
	if m.mode != modeConfirmReopen {
		t.Fatalf("d on a done task should open the reopen confirm; got mode %v", m.mode)
	}
	if got := m.get("f"); got == nil || got.Status != todo.Done {
		t.Fatal("task must stay done until the prompt is confirmed")
	}

	// n leaves it done.
	m = sendKey(t, m, "n")
	if m.mode != modeNormal {
		t.Fatalf("n should dismiss the prompt; got mode %v", m.mode)
	}
	if m.get("f").Status != todo.Done {
		t.Error("n must leave the task done")
	}

	// d then y reopens it.
	m = sendKey(t, m, "d")
	m = sendKey(t, m, "y")
	if m.mode != modeNormal {
		t.Fatalf("y should close the prompt; got mode %v", m.mode)
	}
	if m.get("f").Status != todo.Pending {
		t.Error("y must move the task back to active (pending)")
	}
}

// ── Cascade delete ───────────────────────────────────────────────────────────

// Deleting a parent must also delete its subtasks (and their subtasks).
// Without the cascade the children stayed in storage with a dangling ParentID,
// so they reappeared as headless rows on the next reload.
func TestDeleteCascadesToSubtasks(t *testing.T) {
	now := time.Now()
	parent := todo.New("parent")
	parent.ID = "p"
	parent.CreatedAt = now
	c1 := todo.New("c1")
	c1.ID = "c1"
	c1.ParentID = "p"
	c1.CreatedAt = now.Add(time.Second)
	g1 := todo.New("g1")
	g1.ID = "g1"
	g1.ParentID = "c1"
	g1.CreatedAt = now.Add(2 * time.Second)
	other := todo.New("other")
	other.ID = "o"
	other.CreatedAt = now.Add(3 * time.Second)

	m := modelWithTasks(t, parent, c1, g1, other)
	for i, vt := range m.visibleActiveTasks() {
		if vt.ID == "p" {
			m.cursor = i
			break
		}
	}

	m = sendKey(t, m, "x")
	if m.mode != modeConfirmDelete {
		t.Fatalf("x should open the delete-confirm prompt; got mode %v", m.mode)
	}
	m = sendKey(t, m, "y")

	for _, id := range []string{"p", "c1", "g1"} {
		if m.get(id) != nil {
			t.Errorf("%s should be removed from the store after cascade-delete", id)
		}
		if _, ok := m.tombstones[id]; !ok {
			t.Errorf("%s should be tombstoned so the deletion persists", id)
		}
	}
	if m.get("o") == nil {
		t.Errorf("unrelated task should survive the cascade")
	}
}

// ── Undo vs. sync ────────────────────────────────────────────────────────────

// Undoing a delete must survive the next sync: the restored task carries a
// fresh ModifiedAt so it out-times the delete's tombstone in the last-writer-
// wins merge. Without the bump the tombstone (already propagated to the
// server) wins and silently re-deletes the task.
func TestUndoDeleteSurvivesSyncMerge(t *testing.T) {
	task := todo.New("keep me")
	task.ID = "k"
	task.ModifiedAt = time.Now().Add(-time.Hour) // last edited well before the delete
	m := modelWithTasks(t, task)

	m = sendKey(t, m, "x")
	m = sendKey(t, m, "y")
	if m.get("k") != nil {
		t.Fatal("task should be deleted after x/y")
	}
	deletedAt := time.Now()

	m = sendKey(t, m, "u")
	got := m.get("k")
	if got == nil {
		t.Fatal("undo should restore the task")
	}
	if !got.ModifiedAt.After(deletedAt) {
		t.Errorf("restored ModifiedAt = %v, want after the deletion (%v) so the undo wins the merge",
			got.ModifiedAt, deletedAt)
	}

	// The server still holds the tombstone from before the undo. The restored
	// version must win the merge or the next sync would re-delete it.
	tombstone := task
	tombstone.Deleted = true
	tombstone.DeletedAt = deletedAt
	merged := tasksync.Merge([]todo.Todo{tombstone}, []todo.Todo{*got})
	for _, mt := range merged {
		if mt.ID == "k" {
			if mt.Deleted {
				t.Error("merge picked the tombstone over the undo-restored task")
			}
			return
		}
	}
	t.Error("restored task missing from merge output")
}

// ── Undo vs. the subtaskOf index ─────────────────────────────────────────────

// Undoing an edit on a parent restores it via remove+add, and remove() wipes
// subtaskOf[parent]. The restore must re-attach the bucket, or the subtasks
// stay live in the map but vanish from every subtask view until restart.
func TestUndoEditKeepsSubtaskIndex(t *testing.T) {
	now := time.Now()
	parent := todo.New("parent")
	parent.ID = "p"
	parent.CreatedAt = now
	s1 := todo.New("s1")
	s1.ID = "s1"
	s1.ParentID = "p"
	s1.CreatedAt = now.Add(time.Second)
	s2 := todo.New("s2")
	s2.ID = "s2"
	s2.ParentID = "p"
	s2.CreatedAt = now.Add(2 * time.Second)
	m := modelWithTasks(t, parent, s1, s2)

	origTitle := parent.Title
	m.pushUndo("edit title", "p")
	m.get("p").Title = "renamed"
	m.performUndo()

	if got := m.get("p").Title; got != origTitle {
		t.Errorf("undo should restore the title to %q, got %q", origTitle, got)
	}
	if got := m.subtaskIDs("p"); len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Errorf("subtaskOf[p] after undo = %v, want [s1 s2]", got)
	}
}

// Undoing a subtask deletion captures the parent too (its subtask list
// changed), so the parent's restore must not drop the surviving siblings
// from the index while re-inserting the deleted subtask.
func TestUndoDeleteSubtaskKeepsSiblingIndex(t *testing.T) {
	now := time.Now()
	parent := todo.New("parent")
	parent.ID = "p"
	parent.CreatedAt = now
	s1 := todo.New("s1")
	s1.ID = "s1"
	s1.ParentID = "p"
	s1.CreatedAt = now.Add(time.Second)
	s2 := todo.New("s2")
	s2.ID = "s2"
	s2.ParentID = "p"
	s2.CreatedAt = now.Add(2 * time.Second)
	m := modelWithTasks(t, parent, s1, s2)

	// Mirror updateConfirmDeleteSubtask: undo entry = parent + deleted subtree.
	m.pushUndo("delete subtask", "p", "s1")
	m.markTombstone("s1")
	m.remove("s1")
	if got := m.subtaskIDs("p"); len(got) != 1 || got[0] != "s2" {
		t.Fatalf("precondition: subtaskOf[p] after delete = %v, want [s2]", got)
	}

	m.performUndo()

	if m.get("s1") == nil {
		t.Fatal("undo should restore the deleted subtask")
	}
	if got := m.subtaskIDs("p"); len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Errorf("subtaskOf[p] after undo = %v, want [s1 s2]", got)
	}
}

// An external reload (watcher/sync) rebuilds the Store from a DB snapshot.
// It must not wipe what only exists in memory: the undo stack, an edit still
// inside the save debounce, a pending tombstone, or a just-created task the
// snapshot predates.
func TestReloadPreservesUndoAndUnsavedChanges(t *testing.T) {
	a := todo.New("alpha")
	a.ID = "a"
	b := todo.New("beta")
	b.ID = "b"
	m := modelWithTasks(t, a, b)
	// initialModel may have loaded persisted delete-undo entries written by
	// other tests in this binary's shared temp HOME — measure relative to it.
	baseUndo := len(m.undoStack)

	// Unflushed local state: an undoable title edit on a, a pending delete of
	// b, and a brand-new task c — none of it in the snapshot below.
	m.pushUndo("edit title", "a")
	m.get("a").Title = "alpha edited"
	m.markDirty("a")
	m.remove("b")
	m.markTombstone("b")
	created := todo.New("gamma")
	created.ID = "c"
	m.add(created)
	m.markDirty("c")

	// Snapshot as the DB looked before any of that flushed, plus a task d
	// written externally (the reason the reload fired).
	external := todo.New("delta")
	external.ID = "d"
	next, _ := m.Update(reloadedMsg{todos: []todo.Todo{a, b, external}})
	m = next.(model)

	if len(m.undoStack) != baseUndo+1 {
		t.Errorf("undo stack len = %d after reload, want %d", len(m.undoStack), baseUndo+1)
	}
	if got := m.get("a"); got == nil || got.Title != "alpha edited" {
		t.Errorf("unsaved edit lost: a = %+v, want title %q", got, "alpha edited")
	}
	if _, ok := m.dirtyIDs["a"]; !ok {
		t.Error("a should stay dirty so the scheduled save still flushes the edit")
	}
	if m.get("b") != nil {
		t.Error("pending-tombstoned b resurrected by the reload snapshot")
	}
	if _, ok := m.tombstones["b"]; !ok {
		t.Error("b's tombstone should survive the reload so the delete flushes")
	}
	if m.get("c") == nil {
		t.Error("locally-created unflushed c lost in the reload")
	}
	if m.get("d") == nil {
		t.Error("externally-written d should appear after the reload")
	}
}

// ── Toggle done ──────────────────────────────────────────────────────────────

func TestDKeyTogglesDone(t *testing.T) {
	task := todo.New("flip me")
	m := modelWithTasks(t, task)

	m = sendKey(t, m, "d")
	got := m.get(task.ID)
	if got == nil {
		t.Fatal("task vanished after d")
	}
	if got.Status != todo.Done {
		t.Errorf("after d: status = %v, want Done", got.Status)
	}
}

// ── Recurrence: spawn on done ────────────────────────────────────────────────

func TestDKeySpawnsNextRecurrence(t *testing.T) {
	task := todo.New("daily standup")
	task.Recurrence = "daily"
	m := modelWithTasks(t, task)

	m = sendKey(t, m, "d")

	// Original should be Done and stay in the store.
	orig := m.get(task.ID)
	if orig == nil || orig.Status != todo.Done {
		t.Fatalf("original: got %+v, want Done", orig)
	}

	// A second task should now exist with the same title, still pending,
	// and carrying the same recurrence rule for the next cycle.
	var spawned *todo.Todo
	for id, candidate := range m.tasks {
		if id != task.ID && candidate.Title == task.Title {
			spawned = candidate
			break
		}
	}
	if spawned == nil {
		t.Fatal("expected a spawned next instance, found none")
	}
	if spawned.Status != todo.Pending {
		t.Errorf("spawned status = %v, want Pending", spawned.Status)
	}
	if spawned.Recurrence != "daily" {
		t.Errorf("spawned recurrence = %q, want daily", spawned.Recurrence)
	}
	if spawned.DueDate.IsZero() {
		t.Error("spawned task should have a due date set from the recurrence rule")
	}
}

func TestDKeyOnNonRecurringDoesNotSpawn(t *testing.T) {
	task := todo.New("one-shot")
	m := modelWithTasks(t, task)
	before := len(m.tasks)

	m = sendKey(t, m, "d")

	if len(m.tasks) != before {
		t.Errorf("task count = %d, want %d (no spawn expected)", len(m.tasks), before)
	}
}

// ── Priority cycle ───────────────────────────────────────────────────────────

func TestPKeyCyclesPriority(t *testing.T) {
	task := todo.New("priority dance")
	// task.New defaults Priority to Medium.
	m := modelWithTasks(t, task)

	m = sendKey(t, m, "p")
	if got := m.get(task.ID); got == nil || got.Priority != todo.PriorityHigh {
		t.Errorf("after p (Medium → High): got %v", got)
	}
	m = sendKey(t, m, "p")
	if got := m.get(task.ID); got == nil || got.Priority != todo.PriorityLow {
		t.Errorf("after 2×p (High → Low): got %v", got)
	}
	m = sendKey(t, m, "p")
	if got := m.get(task.ID); got == nil || got.Priority != todo.PriorityMedium {
		t.Errorf("after 3×p (Low → Medium, wrap): got %v", got)
	}
}

// ── Bias cycle on Settings tab ───────────────────────────────────────────────

func TestBiasCycleOnSettingsTab(t *testing.T) {
	// Reset to a known starting point so prior tests' bias mutations don't
	// leak in. applyBiases is global state — explicit reset is the safest
	// guard for parallel-safety even with -race off.
	applyBiases(biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true})
	defer applyBiases(biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true})

	m := modelWithTasks(t)
	m.tab = tabSettings
	m.settingsCursor = settingBiasDeadline

	m = sendKey(t, m, "right")
	if activeBiases.Deadline != biasIntense {
		t.Errorf("after right on Deadline row: %v, want Intense (Balanced → next)", activeBiases.Deadline)
	}
	m = sendKey(t, m, "left")
	if activeBiases.Deadline != biasBalanced {
		t.Errorf("after left: %v, want Balanced", activeBiases.Deadline)
	}

	// Other rows are not touched by this row's cycle.
	if activeBiases.Priority != biasBalanced || activeBiases.Momentum != biasBalanced {
		t.Errorf("siblings should be untouched: Priority=%v Momentum=%v",
			activeBiases.Priority, activeBiases.Momentum)
	}

	// Move cursor to the Momentum row and confirm the cycle hits that one.
	m.settingsCursor = settingBiasMomentum
	m = sendKey(t, m, "right")
	if activeBiases.Momentum != biasIntense {
		t.Errorf("after right on Momentum row: %v, want Intense", activeBiases.Momentum)
	}
}

// ── Quit flushes pending writes ───────────────────────────────────────────────

// Regression: pressing q within the 300ms save-debounce window used to drop
// the most recent mutation. tea.Quit fires immediately, tea.Tick(300ms) loses
// the race, and the user comes back to find their just-added task gone.
// flushPendingWrites on the q/ctrl+c handler closes the window.
func TestQuitFlushesPendingWrites(t *testing.T) {
	repo := &fakeRepo{}
	m := initialModel(repo)
	m.termWidth = 120
	m.termHeight = 40
	m.ensureCache()

	// Mimic the user's flow: 'a' opens the input, type a title, Enter adds.
	m = sendKey(t, m, "a")
	if m.mode != modeInput {
		t.Fatalf("after 'a': mode = %v, want modeInput", m.mode)
	}
	m.textInput.SetValue("Buy milk")
	m = sendKey(t, m, "enter")
	if m.mode != modeNormal {
		t.Fatalf("after enter: mode = %v, want modeNormal", m.mode)
	}
	if m.Store.len() != 1 {
		t.Fatalf("after enter: store has %d tasks, want 1", m.Store.len())
	}

	// Press q. The flush must persist the task before tea.Quit takes the
	// program down, otherwise the next launch loads an empty repo.
	m = sendKey(t, m, "q")
	if len(repo.todos) != 1 {
		t.Fatalf("after q: repo has %d tasks, want 1 — pending write was dropped",
			len(repo.todos))
	}
	if repo.todos[0].Title != "Buy milk" {
		t.Errorf("saved title = %q, want %q", repo.todos[0].Title, "Buy milk")
	}
}

// Regression: modal handlers (add, edit-title, confirm-delete, …) used to
// return from dispatch before the common dirty-check tail, so the save tick
// wasn't armed until the next keystroke. A panic / SIGKILL between the modal
// Enter and any subsequent key would lose the mutation. Now every handler
// flows through the tail, so savePending + saveScheduled flip immediately.
func TestModalMutationSchedulesSaveImmediately(t *testing.T) {
	repo := &fakeRepo{}
	m := initialModel(repo)
	m.termWidth = 120
	m.termHeight = 40
	m.ensureCache()

	m = sendKey(t, m, "a")
	m.textInput.SetValue("Buy milk")
	m = sendKey(t, m, "enter")

	if !m.savePending {
		t.Errorf("savePending = false after modal enter, want true")
	}
	if !m.saveScheduled {
		t.Errorf("saveScheduled = false after modal enter, want true")
	}
	if m.dirty {
		t.Errorf("dirty still set after dispatch tail, want false")
	}
}

// ── Detail-pane wrap-around ──────────────────────────────────────────────────

// up at the top of a detail page should jump to the bottom of that same page,
// and down at the bottom should jump back to the top. Backlog item 37e22859.
func TestDetailCursorWrapsPage0(t *testing.T) {
	task := todo.New("wrap p0")
	task.AddTag("alpha")
	task.AddTag("beta")
	m := modelWithTasks(t, task)
	m.cursor = 0
	m.pane = paneDetail
	m.detail = detailState{field: fieldStartDate}

	m.detailCursorUp()
	if m.detail.field != fieldTags {
		t.Fatalf("up at StartDate: field = %v, want fieldTags", m.detail.field)
	}
	if m.detail.tagCursor != 1 {
		t.Errorf("tagCursor = %d, want 1 (last tag)", m.detail.tagCursor)
	}

	m.detail = detailState{field: fieldTags, tagCursor: 1}
	m.detailCursorDown()
	if m.detail.field != fieldStartDate {
		t.Errorf("down at last tag: field = %v, want fieldStartDate", m.detail.field)
	}
}

func TestDetailCursorWrapsPage0NoTags(t *testing.T) {
	task := todo.New("wrap p0 no tags")
	m := modelWithTasks(t, task)
	m.cursor = 0
	m.pane = paneDetail
	m.detail = detailState{field: fieldStartDate}

	m.detailCursorUp()
	if m.detail.field != fieldTags || m.detail.tagCursor != 0 {
		t.Errorf("up with no tags: field=%v cursor=%d, want fieldTags / 0",
			m.detail.field, m.detail.tagCursor)
	}
}

func TestDetailCursorWrapsPage1(t *testing.T) {
	task := todo.New("wrap p1")
	task.AddLearning("first lesson")
	task.AddLearning("second lesson")
	m := modelWithTasks(t, task)
	m.cursor = 0
	m.pane = paneDetail
	m.detail = detailState{page: 1, field: fieldSubtasks, subtaskCursor: 0}

	m.detailCursorUp()
	if m.detail.field != fieldLearnings {
		t.Fatalf("up at Subtasks#0: field = %v, want fieldLearnings", m.detail.field)
	}
	if m.detail.learningCursor != 1 {
		t.Errorf("learningCursor = %d, want 1 (last learning)", m.detail.learningCursor)
	}

	m.detail = detailState{page: 1, field: fieldLearnings, learningCursor: 1}
	m.detailCursorDown()
	if m.detail.field != fieldSubtasks || m.detail.subtaskCursor != 0 {
		t.Errorf("down at last learning: field=%v cursor=%d, want fieldSubtasks / 0",
			m.detail.field, m.detail.subtaskCursor)
	}
}

func TestDetailCursorWrapsPage2(t *testing.T) {
	task := todo.New("wrap p2")
	task.AddComment("c1")
	task.AddComment("c2")
	task.AddComment("c3")
	m := modelWithTasks(t, task)
	m.cursor = 0
	m.pane = paneDetail
	m.detail = detailState{commentCursor: 0}
	// Page 2 is the comments page.
	m.detail.page = 2

	m.detailCursorUp()
	if m.detail.commentCursor != 2 {
		t.Errorf("up at commentCursor 0: cursor = %d, want 2 (last)", m.detail.commentCursor)
	}

	m.detail.commentCursor = 2
	m.detailCursorDown()
	if m.detail.commentCursor != 0 {
		t.Errorf("down at last comment: cursor = %d, want 0", m.detail.commentCursor)
	}
}

// Enter on a dependency that isn't in the active list can't scroll to it, so
// it should explain why via an info toast instead of silently no-oping.
// Backlog item 1ca152f4.
func TestEnterOnDoneDependencyFlashesToast(t *testing.T) {
	dep := todo.New("build the widget")
	dep.Status = todo.Done
	dependent := todo.New("ship it")
	dependent.Dependencies = []string{dep.ID}

	m := modelWithTasks(t, dependent, dep)
	m.cursor = 0
	m.pane = paneDetail
	m.detail = detailState{field: fieldDependencies, depCursor: 0}

	updated, _ := m.startEditing()
	m2 := updated.(model)
	want := "Dependency 'Build the widget' is done"
	if m2.errKind != toastInfo || m2.err != want {
		t.Errorf("got (%q, %d), want (%q, info)", m2.err, m2.errKind, want)
	}
	if m2.pane != paneDetail {
		t.Errorf("pane = %v, want paneDetail (should not jump)", m2.pane)
	}
}

func TestEnterOnMissingDependencyFlashesToast(t *testing.T) {
	dependent := todo.New("ship it")
	dependent.Dependencies = []string{"no-such-task"}

	m := modelWithTasks(t, dependent)
	m.cursor = 0
	m.pane = paneDetail
	m.detail = detailState{field: fieldDependencies, depCursor: 0}

	updated, _ := m.startEditing()
	m2 := updated.(model)
	want := "Dependency no longer exists"
	if m2.errKind != toastInfo || m2.err != want {
		t.Errorf("got (%q, %d), want (%q, info)", m2.err, m2.errKind, want)
	}
}
