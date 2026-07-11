package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"taskr/todo"
)

// update_keyscript_test.go drives the real Update dispatch with scripted key
// sequences — the closest thing to "a user at the keyboard" that runs without
// a terminal. These flows live in update_modes.go / update_detail.go, which
// hold the app's most convention-enforced invariants (pushUndo before mutate,
// markModified after, cursor clamps, mode resets) and historically had almost
// no coverage. Each test asserts the store/undo/save bookkeeping, not the
// rendering.

// script sends each entry through the full Update path. Multi-rune strings
// arrive as one KeyRunes msg (how Bubble Tea delivers a paste), which the
// textinput consumes the same as typed characters.
func script(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		m = sendKey(t, m, k)
	}
	return m
}

// sendKeyCmd is sendKey but keeps the returned command, for asserting on
// tea.Quit and friends.
func sendKeyCmd(t *testing.T, m model, msg tea.KeyMsg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func TestScriptQuickAddCreatesTaskAndUndoRemoves(t *testing.T) {
	m := modelWithTasks(t)

	m = sendKey(t, m, "a")
	if m.mode != modeInput {
		t.Fatalf("after 'a': mode = %v, want modeInput", m.mode)
	}
	m = script(t, m, "fix the boiler", "enter")

	if m.mode != modeNormal {
		t.Fatalf("after enter: mode = %v, want modeNormal", m.mode)
	}
	if m.len() != 1 {
		t.Fatalf("store has %d tasks, want 1", m.len())
	}
	created := m.currentTodo()
	if created == nil || created.Title != "Fix the boiler" {
		t.Fatalf("created = %+v, want capitalized title", created)
	}
	if !m.savePending && !m.saveScheduled {
		t.Error("mutation did not schedule a save (savePending/saveScheduled both false)")
	}
	if len(m.undoStack) != 1 {
		t.Fatalf("undoStack len = %d, want 1", len(m.undoStack))
	}

	// Undo of a creation must remove the task AND tombstone it, so the
	// pending save can't resurrect it.
	id := created.ID
	m = sendKey(t, m, "u")
	if m.len() != 0 {
		t.Errorf("after undo: store has %d tasks, want 0", m.len())
	}
	if _, ok := m.tombstones[id]; !ok {
		t.Error("after undo of a create: no tombstone recorded for the removed task")
	}
}

// TestScriptQuickAddOpensDetailOnNewTask asserts that pressing Enter in the
// add-task input opens the detail pane of the newly created task with the
// cursor/selection positioned on that task. This is the "open detail after add"
// flow from backlog item fa2261e1.
func TestScriptQuickAddOpensDetailOnNewTask(t *testing.T) {
	// Start with one existing task so we can verify the cursor moves to the
	// newly added task rather than staying on whatever row it was on.
	existing := todo.New("existing task")
	m := modelWithTasks(t, existing)

	// Cursor starts on the existing task.
	if m.pane != paneList {
		t.Fatalf("initial pane = %v, want paneList", m.pane)
	}

	m = script(t, m, "a", "brand new task", "enter")

	// The app must switch to the detail pane.
	if m.pane != paneDetail {
		t.Errorf("after add: pane = %v, want paneDetail", m.pane)
	}
	// The detail must be reset to the first field.
	if m.detail.field != fieldStartDate {
		t.Errorf("after add: detail.field = %v, want fieldStartDate", m.detail.field)
	}
	// The cursor (and currentTodo) must point at the newly added task.
	got := m.currentTodo()
	if got == nil {
		t.Fatal("currentTodo is nil after add — cursor not on any task")
	}
	if got.Title != "Brand new task" {
		t.Errorf("currentTodo.Title = %q, want %q", got.Title, "Brand new task")
	}
}

// TestScriptQuickAddOpensDetailOnFirstTask covers the edge case where the list
// was empty before the add: the newly created task must still land in the
// detail pane.
func TestScriptQuickAddOpensDetailOnFirstTask(t *testing.T) {
	m := modelWithTasks(t) // empty list

	m = script(t, m, "a", "very first task", "enter")

	if m.pane != paneDetail {
		t.Errorf("after first add: pane = %v, want paneDetail", m.pane)
	}
	got := m.currentTodo()
	if got == nil {
		t.Fatal("currentTodo is nil after add into an empty list")
	}
	if got.Title != "Very first task" {
		t.Errorf("currentTodo.Title = %q, want %q", got.Title, "Very first task")
	}
}

// TestScriptQuickAddFilteredOutStaysInList covers adding a task while a search
// filter hides it: the cursor can't follow the new task, so the app must stay
// in the list pane instead of opening the detail of whatever task the cursor
// was on.
func TestScriptQuickAddFilteredOutStaysInList(t *testing.T) {
	existing := todo.New("alpha task")
	m := modelWithTasks(t, existing)

	m = script(t, m, "/", "alpha", "enter")
	if m.searchQuery != "alpha" {
		t.Fatalf("searchQuery = %q, want alpha", m.searchQuery)
	}

	m = script(t, m, "a", "bravo task", "enter")

	if m.len() != 2 {
		t.Fatalf("store has %d tasks, want 2", m.len())
	}
	if m.pane != paneList {
		t.Errorf("after filtered add: pane = %v, want paneList", m.pane)
	}
	if got := m.currentTodo(); got == nil || got.Title != "Alpha task" {
		t.Errorf("currentTodo = %+v, want cursor to stay on the visible task", got)
	}
}

func TestScriptQuickAddParsesInlineSyntax(t *testing.T) {
	m := modelWithTasks(t)
	m = script(t, m, "a", "ship release p:high due:tomorrow #work @taskr", "enter")

	created := m.currentTodo()
	if created == nil {
		t.Fatal("no task created")
	}
	if created.Title != "Ship release" {
		t.Errorf("title = %q, want syntax tokens stripped", created.Title)
	}
	if created.Priority != todo.PriorityHigh {
		t.Errorf("priority = %v, want High", created.Priority)
	}
	if created.Project != "taskr" {
		t.Errorf("project = %q, want taskr", created.Project)
	}
	if len(created.Tags) != 1 || created.Tags[0] != "work" {
		t.Errorf("tags = %v, want [work]", created.Tags)
	}
	wantDue := startOfDay(time.Now()).AddDate(0, 0, 1)
	if !created.DueDate.Equal(wantDue) {
		t.Errorf("due = %v, want %v", created.DueDate, wantDue)
	}
}

func TestScriptDeleteCascadesAndUndoRestoresSubtree(t *testing.T) {
	parent := todo.New("Parent job")
	sub := todo.NewSubtask("Child step", parent.ID)
	m := modelWithTasks(t, parent, sub)

	// Cursor sits on the only top-level task. Delete must stage a confirm
	// that mentions the cascade, then tombstone parent + child on 'y'.
	m = sendKey(t, m, "x")
	if m.mode != modeConfirm {
		t.Fatalf("after 'x': mode = %v, want modeConfirm", m.mode)
	}
	m = sendKey(t, m, "y")
	if m.len() != 0 {
		t.Fatalf("after confirm: %d tasks left, want 0 (cascade)", m.len())
	}
	if len(m.tombstones) != 2 {
		t.Fatalf("tombstones = %d, want 2 (parent + subtask)", len(m.tombstones))
	}

	// Undo must restore both, clear their tombstones, and rebuild the
	// subtaskOf index (the de72a1a bug class: parent restored but its
	// subtask bucket wiped, children unreachable until restart).
	m = sendKey(t, m, "u")
	if m.len() != 2 {
		t.Fatalf("after undo: %d tasks, want 2", m.len())
	}
	if _, dead := m.tombstones[parent.ID]; dead {
		t.Error("after undo: parent still tombstoned — pending save would re-delete it")
	}
	if _, dead := m.tombstones[sub.ID]; dead {
		t.Error("after undo: subtask still tombstoned")
	}
	if got := m.subtaskCount(parent.ID); got != 1 {
		t.Errorf("after undo: subtaskCount = %d, want 1 (index rebuilt)", got)
	}
	// The restore must count as the latest write or the next sync would
	// re-apply the deletion (tombstone carries a newer DeletedAt).
	if restored := m.get(parent.ID); restored != nil && time.Since(restored.ModifiedAt) > time.Minute {
		t.Errorf("restored parent ModifiedAt = %v — undo must stamp now so it wins the merge", restored.ModifiedAt)
	}
}

func TestScriptRenameTaskViaR(t *testing.T) {
	m := modelWithTasks(t, todo.New("Old name"))
	target := m.currentTodo()

	m = sendKey(t, m, "r")
	if m.mode != modeEditTitle {
		t.Fatalf("after 'r': mode = %v, want modeEditTitle", m.mode)
	}
	// The editor pre-fills the current title; typed runes append.
	m = script(t, m, " v2", "enter")
	if got := m.get(target.ID); got.Title != "Old name v2" {
		t.Errorf("title = %q, want %q", got.Title, "Old name v2")
	}
	if _, dirty := m.dirtyIDs[target.ID]; !dirty {
		t.Error("renamed task not in dirtyIDs — rename would never persist")
	}
	if len(m.undoStack) != 1 {
		t.Errorf("undoStack len = %d, want 1", len(m.undoStack))
	}
}

func TestScriptAddCommentOnDetailComments(t *testing.T) {
	m := modelWithTasks(t, todo.New("Task with comments"))
	target := m.currentTodo()

	// Detail pane → jump sections to comments (tags → subtasks → deps →
	// learnings → comments).
	m = script(t, m, "enter", "right", "right", "right", "right", "right")
	if m.pane != paneDetail || m.detail.field != fieldComments {
		t.Fatalf("pane=%v field=%v, want detail fieldComments", m.pane, m.detail.field)
	}
	m = script(t, m, "a", "looks good", "enter")

	got := m.get(target.ID)
	if len(got.Comments) != 1 || got.Comments[0].Text != "looks good" {
		t.Fatalf("comments = %+v, want one 'looks good'", got.Comments)
	}
	if m.detail.commentCursor != 0 {
		t.Errorf("commentCursor = %d, want 0 (on the new comment)", m.detail.commentCursor)
	}
	if len(m.undoStack) != 1 {
		t.Errorf("undoStack len = %d, want 1", len(m.undoStack))
	}
}

func TestScriptAddSubtaskFromDetail(t *testing.T) {
	m := modelWithTasks(t, todo.New("Parent task"))
	target := m.currentTodo()

	m = script(t, m, "enter", "right", "right") // sections: tags → subtasks
	if m.mode != modeNormal || m.detail.field != fieldSubtasks {
		t.Fatalf("field = %v, want fieldSubtasks", m.detail.field)
	}
	m = script(t, m, "a", "child step", "enter")

	if got := m.subtaskCount(target.ID); got != 1 {
		t.Fatalf("subtaskCount = %d, want 1", got)
	}
	subID := m.subtaskIDs(target.ID)[0]
	sub := m.get(subID)
	if sub.Title != "Child step" || sub.ParentID != target.ID {
		t.Errorf("subtask = %+v, want capitalized title under parent", sub)
	}
	if _, dirty := m.dirtyIDs[subID]; !dirty {
		t.Error("new subtask not in dirtyIDs")
	}
}

func TestScriptManualTimeEntryEndsNow(t *testing.T) {
	m := modelWithTasks(t, todo.New("Tracked work"))
	target := m.currentTodo()

	m = script(t, m, "T", "45m", "enter")

	got := m.get(target.ID)
	if len(got.TimeEntries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.TimeEntries))
	}
	e := got.TimeEntries[0]
	if e.IsRunning() {
		t.Fatal("manual entry is running — must be closed")
	}
	if d := e.StoppedAt.Sub(e.StartedAt); d != 45*time.Minute {
		t.Errorf("duration = %v, want 45m", d)
	}
	// "I just spent 45m on this": the entry must end now, not start now.
	if drift := time.Since(e.StoppedAt); drift < 0 || drift > 5*time.Second {
		t.Errorf("entry ends %v from now, want ≈now", drift)
	}
}

func TestScriptSearchFiltersAndEscRestores(t *testing.T) {
	m := modelWithTasks(t, todo.New("Alpha work"), todo.New("Beta work"))

	m = script(t, m, "/", "alpha", "enter")
	if m.searchQuery != "alpha" {
		t.Fatalf("searchQuery = %q, want alpha", m.searchQuery)
	}
	// The filtered rebuild is lazy — View calls ensureCache before reading.
	m.ensureCache()
	if got := m.visibleActiveLen(); got != 1 {
		t.Errorf("filtered list len = %d, want 1", got)
	}

	// Re-enter search and cancel: the filter must clear entirely.
	m = script(t, m, "/", "esc")
	if m.searchQuery != "" {
		t.Errorf("after esc: searchQuery = %q, want empty", m.searchQuery)
	}
	m.ensureCache()
	if got := m.visibleActiveLen(); got != 2 {
		t.Errorf("after esc: list len = %d, want 2", got)
	}
}

func TestScriptTimerKeepsSingleRunningInvariant(t *testing.T) {
	m := modelWithTasks(t, todo.New("First"), todo.New("Second"))

	m = sendKey(t, m, "t")
	first := m.currentTodo()
	if !first.IsTimerRunning() {
		t.Fatal("first task's timer not running after 't'")
	}
	m = script(t, m, "down", "t")
	second := m.currentTodo()
	if second.ID == first.ID {
		t.Fatal("cursor did not move — test setup wrong")
	}
	if !second.IsTimerRunning() {
		t.Error("second task's timer not running")
	}
	if m.get(first.ID).IsTimerRunning() {
		t.Error("first task still running — single-timer invariant broken")
	}
	if len(m.runningTimers) != 1 {
		t.Errorf("runningTimers = %d, want 1", len(m.runningTimers))
	}
}

func TestScriptCtrlCQuitsFromAnyMode(t *testing.T) {
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}

	assertQuits := func(t *testing.T, m model, where string) {
		t.Helper()
		_, cmd := sendKeyCmd(t, m, ctrlC)
		if cmd == nil {
			t.Fatalf("%s: ctrl+c returned nil cmd, want tea.Quit", where)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s: ctrl+c cmd = %T, want tea.QuitMsg", where, cmd())
		}
	}

	m := modelWithTasks(t, todo.New("Something"))
	assertQuits(t, m, "list pane")
	assertQuits(t, sendKey(t, m, "enter"), "detail pane")
	assertQuits(t, sendKey(t, m, "a"), "quick-add modal")
	assertQuits(t, sendKey(t, m, "x"), "confirm-delete modal")
}

func TestScriptEscCancelsModalWithoutMutation(t *testing.T) {
	m := modelWithTasks(t, todo.New("Existing"))

	m = script(t, m, "a", "abandoned draft", "esc")
	if m.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", m.mode)
	}
	if m.len() != 1 {
		t.Errorf("store len = %d, want 1 (nothing created)", m.len())
	}
	if len(m.undoStack) != 0 {
		t.Errorf("undoStack len = %d, want 0 (nothing to undo)", len(m.undoStack))
	}
	if m.savePending || m.saveScheduled {
		t.Error("cancelled modal scheduled a save")
	}
}

func TestScriptToggleDoneStopsTimerAndMarksDone(t *testing.T) {
	m := modelWithTasks(t, todo.New("Close me"))
	target := m.currentTodo()

	// Start its timer, then close it: the TUI must not leave a dangling
	// open time entry on a done task.
	m = script(t, m, "t", "d")
	got := m.get(target.ID)
	if got.Status != todo.Done {
		t.Fatalf("status = %v, want Done", got.Status)
	}
	if got.IsTimerRunning() {
		t.Error("timer still running on a done task")
	}
	if got.CompletedAt.IsZero() {
		t.Error("CompletedAt not stamped")
	}
}

// ── Time-entries section in the detail pane ───────────────────────────────────

func navToTimeEntries(t *testing.T, m model) model {
	t.Helper()
	// enter → detail pane; right×3 → fieldTimeEntries
	// sections order: startDate(0) → tags(1) → subtasks(2) → timeEntries(3 rights)
	// The section jump algorithm picks cur by last-matching iota, so the path
	// from fieldSubtasks(10) skips fieldDependencies(8) and fieldLearnings(9)
	// (lower iota values) and lands directly on fieldTimeEntries(11).
	return script(t, m, "enter", "right", "right", "right")
}

func TestScriptDetailTimeEntriesNavigate(t *testing.T) {
	task := todo.New("Tracked task")
	now := time.Now()
	task.AddTimeEntry(now.Add(-2*time.Hour), now.Add(-time.Hour))
	task.AddTimeEntry(now.Add(-30*time.Minute), now.Add(-10*time.Minute))
	m := modelWithTasks(t, task)

	m = navToTimeEntries(t, m)
	if m.pane != paneDetail {
		t.Fatalf("pane = %v, want paneDetail", m.pane)
	}
	if m.detail.field != fieldTimeEntries {
		t.Fatalf("field = %v, want fieldTimeEntries", m.detail.field)
	}
	if m.detail.timeEntryCursor != 0 {
		t.Fatalf("timeEntryCursor = %d, want 0", m.detail.timeEntryCursor)
	}

	// Navigate down to the second entry.
	m = sendKey(t, m, "down")
	if m.detail.timeEntryCursor != 1 {
		t.Fatalf("timeEntryCursor after down = %d, want 1", m.detail.timeEntryCursor)
	}
	// Navigate back up.
	m = sendKey(t, m, "up")
	if m.detail.timeEntryCursor != 0 {
		t.Fatalf("timeEntryCursor after up = %d, want 0", m.detail.timeEntryCursor)
	}
}

func TestScriptDetailTimeEntryEdit(t *testing.T) {
	task := todo.New("Editable entry")
	now := time.Now()
	task.AddTimeEntry(now.Add(-2*time.Hour), now.Add(-time.Hour))
	m := modelWithTasks(t, task)
	target := m.get(task.ID)
	entryID := target.TimeEntries[0].ID

	m = navToTimeEntries(t, m)
	if m.detail.field != fieldTimeEntries {
		t.Fatalf("field = %v, want fieldTimeEntries", m.detail.field)
	}

	// 'r' must open modeEditTimeEntry with the entry pre-filled.
	m = sendKey(t, m, "r")
	if m.mode != modeEditTimeEntry {
		t.Fatalf("after 'r': mode = %v, want modeEditTimeEntry", m.mode)
	}
	if m.pendingEntryID != entryID {
		t.Fatalf("pendingEntryID = %q, want %q", m.pendingEntryID, entryID)
	}

	// Submit a new time range.
	m = script(t, m, "esc") // cancel the input
	if m.mode != modeNormal {
		t.Fatalf("after esc: mode = %v, want modeNormal", m.mode)
	}

	// Now open and confirm an edit.
	m = sendKey(t, m, "r")
	m.textInput.SetValue("08:00-09:30")
	m = sendKey(t, m, "enter")

	got := m.get(task.ID)
	if len(got.TimeEntries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.TimeEntries))
	}
	d := got.TimeEntries[0].StoppedAt.Sub(got.TimeEntries[0].StartedAt)
	if d != 90*time.Minute {
		t.Errorf("edited duration = %v, want 90m", d)
	}
	if len(m.undoStack) == 0 {
		t.Error("edit must push an undo entry")
	}
}

func TestScriptDetailTimeEntryDeleteAndUndoStack(t *testing.T) {
	task := todo.New("Deletable entry")
	now := time.Now()
	task.AddTimeEntry(now.Add(-time.Hour), now)
	m := modelWithTasks(t, task)
	target := m.get(task.ID)
	if len(target.TimeEntries) != 1 {
		t.Fatalf("setup: %d entries, want 1", len(target.TimeEntries))
	}

	m = navToTimeEntries(t, m)
	if m.detail.field != fieldTimeEntries {
		t.Fatalf("field = %v, want fieldTimeEntries", m.detail.field)
	}

	// 'x' must open a confirm prompt.
	m = sendKey(t, m, "x")
	if m.mode != modeConfirm {
		t.Fatalf("after 'x': mode = %v, want modeConfirm", m.mode)
	}

	// Confirm deletion with 'y'.
	m = sendKey(t, m, "y")
	if m.mode != modeNormal {
		t.Fatalf("after 'y': mode = %v, want modeNormal", m.mode)
	}
	got := m.get(task.ID)
	if len(got.TimeEntries) != 0 {
		t.Fatalf("after delete: %d entries, want 0", len(got.TimeEntries))
	}
	if !m.savePending && !m.saveScheduled {
		t.Error("deletion did not schedule a save")
	}
	// Deletion must push an undo entry so it can be reversed.
	if len(m.undoStack) == 0 {
		t.Fatal("delete must push an undo entry")
	}
	// The undo entry must name the parent task's ID so a partial restore
	// knows which task to snapshot and mark dirty.
	top := m.undoStack[len(m.undoStack)-1]
	if len(top.ids) == 0 || top.ids[0] != task.ID {
		t.Errorf("undo entry ids = %v, want [%s]", top.ids, task.ID)
	}
	// The undo snapshot must capture the pre-delete state (1 entry).
	if len(top.partial) == 0 {
		t.Error("undo entry must carry a partial snapshot")
	} else if len(top.partial[0].TimeEntries) != 1 {
		t.Errorf("undo snapshot has %d entries, want 1 (pre-delete state)",
			len(top.partial[0].TimeEntries))
	}
	// Cursor must clamp: with 0 entries left, timeEntryCursor must be 0.
	if m.detail.timeEntryCursor != 0 {
		t.Errorf("timeEntryCursor after delete = %d, want 0 (clamped)", m.detail.timeEntryCursor)
	}

	// Undo must restore the deleted entry.
	m = sendKey(t, m, "u")
	restored := m.get(task.ID)
	if restored == nil || len(restored.TimeEntries) != 1 {
		t.Fatalf("after undo: entry not restored (task = %+v)", restored)
	}
}

func TestScriptDetailTimeEntryRunningEntryGuard(t *testing.T) {
	// A running timer entry must be protected from edit/delete in the detail
	// pane — the user must stop the timer first, just like the calendar.
	task := todo.New("Running timer")
	m := modelWithTasks(t, task)

	// Start the timer via 't' in list pane.
	m = sendKey(t, m, "t")
	if !m.get(task.ID).IsTimerRunning() {
		t.Fatal("timer not running after 't'")
	}

	m = navToTimeEntries(t, m)
	if m.detail.field != fieldTimeEntries {
		t.Fatalf("field = %v, want fieldTimeEntries", m.detail.field)
	}

	// 'r' on running entry must flash info, not open edit mode.
	initialStack := len(m.undoStack)
	m = sendKey(t, m, "r")
	if m.mode != modeNormal {
		t.Errorf("after 'r' on running entry: mode = %v, want modeNormal (guarded)", m.mode)
	}
	if len(m.undoStack) != initialStack {
		t.Error("guard must not push undo")
	}

	// 'x' on running entry must flash info, not open confirm.
	m = sendKey(t, m, "x")
	if m.mode != modeNormal {
		t.Errorf("after 'x' on running entry: mode = %v, want modeNormal (guarded)", m.mode)
	}
}
