package todo

import (
	"testing"
	"time"
)

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew(t *testing.T) {
	task := New("Test task")

	if task.Title != "Test task" {
		t.Errorf("Title = %q, want %q", task.Title, "Test task")
	}
	if task.Status != Pending {
		t.Errorf("Status = %v, want Pending", task.Status)
	}
	if task.Priority != PriorityMedium {
		t.Errorf("Priority = %v, want PriorityMedium", task.Priority)
	}
	if task.Size != SizeMedium {
		t.Errorf("Size = %v, want SizeMedium (zero value)", task.Size)
	}
	if task.ID == "" {
		t.Error("ID should not be empty")
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if task.ModifiedAt.IsZero() {
		t.Error("ModifiedAt should be set")
	}
}

// TestSizeStringerAndLetter covers the two display helpers and confirms the
// zero value renders as Medium.
func TestSizeStringerAndLetter(t *testing.T) {
	cases := []struct {
		size   Size
		str    string
		letter string
	}{
		{SizeMedium, "medium", "M"},
		{SizeSmall, "small", "S"},
		{SizeLarge, "large", "L"},
	}
	for _, c := range cases {
		if c.size.String() != c.str {
			t.Errorf("Size(%d).String() = %q, want %q", c.size, c.size.String(), c.str)
		}
		if c.size.Letter() != c.letter {
			t.Errorf("Size(%d).Letter() = %q, want %q", c.size, c.size.Letter(), c.letter)
		}
	}
}

// ── NewSubtask ────────────────────────────────────────────────────────────────

func TestNewSubtask(t *testing.T) {
	sub := NewSubtask("Child task", "parent-123")

	if sub.ParentID != "parent-123" {
		t.Errorf("ParentID = %q, want %q", sub.ParentID, "parent-123")
	}
	if sub.Title != "Child task" {
		t.Errorf("Title = %q, want %q", sub.Title, "Child task")
	}
	if sub.ID == "" {
		t.Error("subtask ID should not be empty")
	}
	if sub.Size != SizeSmall {
		t.Errorf("Size = %v, want SizeSmall (subtasks default small)", sub.Size)
	}
}

// ── Toggle ────────────────────────────────────────────────────────────────────

func TestToggle(t *testing.T) {
	task := New("Toggle test")

	// Pending → Done
	task.Toggle()
	if task.Status != Done {
		t.Fatal("expected Done after first toggle")
	}
	if task.CompletedAt.IsZero() {
		t.Fatal("CompletedAt should be set")
	}

	// Done → Pending
	task.Toggle()
	if task.Status != Pending {
		t.Fatal("expected Pending after second toggle")
	}
	if !task.CompletedAt.IsZero() {
		t.Fatal("CompletedAt should be cleared")
	}
}

// ── Tags ──────────────────────────────────────────────────────────────────────

func TestAddTag(t *testing.T) {
	task := New("Tag test")

	task.AddTag("work")
	task.AddTag("urgent")
	task.AddTag("work") // duplicate — should be ignored

	if len(task.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %v", len(task.Tags), task.Tags)
	}
	if task.Tags[0] != "work" || task.Tags[1] != "urgent" {
		t.Errorf("tags = %v, want [work urgent]", task.Tags)
	}
}

func TestAddTagNormalizes(t *testing.T) {
	task := New("Normalize test")

	// Different casing, surrounding whitespace, and a leading '#' all collapse
	// to a single normalized tag.
	task.AddTag("#Work")
	task.AddTag("work ")
	task.AddTag("  WORK")

	if len(task.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d: %v", len(task.Tags), task.Tags)
	}
	if task.Tags[0] != "work" {
		t.Errorf("tag = %q, want %q", task.Tags[0], "work")
	}

	// Empty / punctuation-only input is rejected.
	task.AddTag("   ")
	task.AddTag("#")
	if len(task.Tags) != 1 {
		t.Errorf("empty tags should be ignored, got %v", task.Tags)
	}

	// RemoveTag normalizes its argument too.
	task.RemoveTag("#WORK ")
	if len(task.Tags) != 0 {
		t.Errorf("expected tag removed, got %v", task.Tags)
	}
}

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"#Work":  "work",
		" work ": "work",
		"WORK":   "work",
		"#":      "",
		"   ":    "",
		"a-b":    "a-b",
	}
	for in, want := range cases {
		if got := NormalizeTag(in); got != want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapitalizeTitle(t *testing.T) {
	cases := map[string]string{
		"buy milk":   "Buy milk",
		"Buy milk":   "Buy milk",
		"BUY MILK":   "BUY MILK",
		"":           "",
		"123 do it":  "123 do it",
		"æbleskiver": "Æbleskiver",
		"é-trail":    "É-trail",
		// Don't touch the rest of the string — only the first rune flips case.
		"hELLO": "HELLO",
	}
	for in, want := range cases {
		if got := CapitalizeTitle(in); got != want {
			t.Errorf("CapitalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoveTag(t *testing.T) {
	task := New("Tag remove test")
	task.AddTag("work")
	task.AddTag("urgent")

	task.RemoveTag("work")
	if len(task.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d: %v", len(task.Tags), task.Tags)
	}
	if task.Tags[0] != "urgent" {
		t.Errorf("remaining tag = %q, want %q", task.Tags[0], "urgent")
	}

	// Remove non-existent — should not panic, change length, or bump ModifiedAt
	// (a no-op must not win a last-writer-wins sync race).
	before := task.ModifiedAt
	task.RemoveTag("nonexistent")
	if len(task.Tags) != 1 {
		t.Fatal("removing nonexistent tag changed length")
	}
	if !task.ModifiedAt.Equal(before) {
		t.Error("removing nonexistent tag bumped ModifiedAt")
	}
}

// ── Dependencies ──────────────────────────────────────────────────────────────

func TestAddDependency(t *testing.T) {
	task := New("Dep test")

	task.AddDependency("dep-1")
	task.AddDependency("dep-2")
	task.AddDependency("dep-1") // duplicate

	if len(task.Dependencies) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(task.Dependencies))
	}
}

func TestRemoveDependency(t *testing.T) {
	task := New("Dep remove test")
	task.AddDependency("dep-1")
	task.AddDependency("dep-2")

	task.RemoveDependency("dep-1")
	if len(task.Dependencies) != 1 || task.Dependencies[0] != "dep-2" {
		t.Fatalf("expected [dep-2], got %v", task.Dependencies)
	}

	// Remove non-existent — must not change length or bump ModifiedAt.
	before := task.ModifiedAt
	task.RemoveDependency("nonexistent")
	if len(task.Dependencies) != 1 {
		t.Fatal("removing nonexistent dep changed length")
	}
	if !task.ModifiedAt.Equal(before) {
		t.Error("removing nonexistent dependency bumped ModifiedAt")
	}
}

// ── Comments ──────────────────────────────────────────────────────────────────

func TestAddComment(t *testing.T) {
	task := New("Comment test")

	task.AddComment("First comment")
	task.AddComment("Second comment")

	if len(task.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(task.Comments))
	}
	if task.Comments[0].Text != "First comment" {
		t.Errorf("comment[0] = %q, want %q", task.Comments[0].Text, "First comment")
	}
	if task.Comments[0].ID == "" {
		t.Error("comment ID should not be empty")
	}
	if task.Comments[0].CreatedAt.IsZero() {
		t.Error("comment CreatedAt should be set")
	}
}

func TestUpdateComment(t *testing.T) {
	task := New("Comment update test")
	task.AddComment("Original")

	task.UpdateComment(0, "Updated")
	if task.Comments[0].Text != "Updated" {
		t.Errorf("after update = %q, want %q", task.Comments[0].Text, "Updated")
	}

	// Out of bounds — should not panic
	task.UpdateComment(99, "nope")
	task.UpdateComment(-1, "nope")
}

func TestDeleteComment(t *testing.T) {
	task := New("Comment delete test")
	task.AddComment("First")
	task.AddComment("Second")
	task.AddComment("Third")

	task.DeleteComment(1) // delete "Second"
	if len(task.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(task.Comments))
	}
	if task.Comments[0].Text != "First" || task.Comments[1].Text != "Third" {
		t.Errorf("comments = [%q, %q], want [First, Third]",
			task.Comments[0].Text, task.Comments[1].Text)
	}

	// Out of bounds — should not panic
	task.DeleteComment(99)
	task.DeleteComment(-1)
}

// ── Learnings ─────────────────────────────────────────────────────────────────

func TestAddLearning(t *testing.T) {
	task := New("Learning test")
	task.Tags = []string{"go", "testing"}

	task.AddLearning("Tests are useful")

	if len(task.Learnings) != 1 {
		t.Fatalf("expected 1 learning, got %d", len(task.Learnings))
	}
	if task.Learnings[0].Text != "Tests are useful" {
		t.Errorf("learning text = %q", task.Learnings[0].Text)
	}
	if task.Learnings[0].ID == "" {
		t.Error("learning ID should not be empty")
	}
	// A learning no longer stores tags — they are derived from the parent task
	// at display time (see learningView), so AddLearning records none.
}

func TestUpdateLearning(t *testing.T) {
	task := New("Learning update")
	task.AddLearning("Original")

	task.UpdateLearning(0, "Updated")
	if task.Learnings[0].Text != "Updated" {
		t.Errorf("after update = %q", task.Learnings[0].Text)
	}

	// Out of bounds
	task.UpdateLearning(99, "nope")
	task.UpdateLearning(-1, "nope")
}

func TestDeleteLearning(t *testing.T) {
	task := New("Learning delete")
	task.AddLearning("First")
	task.AddLearning("Second")

	task.DeleteLearning(0)
	if len(task.Learnings) != 1 {
		t.Fatalf("expected 1 learning, got %d", len(task.Learnings))
	}
	if task.Learnings[0].Text != "Second" {
		t.Errorf("remaining = %q, want %q", task.Learnings[0].Text, "Second")
	}

	// Out of bounds
	task.DeleteLearning(99)
	task.DeleteLearning(-1)
}

// ── IsOverdue ─────────────────────────────────────────────────────────────────

func TestIsOverdue(t *testing.T) {
	task := New("Overdue test")

	// No due date — not overdue
	if task.IsOverdue() {
		t.Error("task without due date should not be overdue")
	}

	// Due yesterday — overdue
	task.DueDate = time.Now().AddDate(0, 0, -1)
	if !task.IsOverdue() {
		t.Error("task due yesterday should be overdue")
	}

	// Due tomorrow — not overdue
	task.DueDate = time.Now().AddDate(0, 0, 1)
	if task.IsOverdue() {
		t.Error("task due tomorrow should not be overdue")
	}

	// Done tasks are never overdue
	task.DueDate = time.Now().AddDate(0, 0, -1)
	task.Toggle()
	if task.IsOverdue() {
		t.Error("completed task should not be overdue")
	}
}

// IsOverdueAt is the clock-injectable form. A task should be overdue
// relative to the passed-in `now`, not the wall clock — that's what lets
// stats buckets stay deterministic across a day rollover.
func TestIsOverdueAt(t *testing.T) {
	pinned := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	task := New("clock test")
	task.DueDate = time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)

	if !task.IsOverdueAt(pinned) {
		t.Error("due yesterday should be overdue at the pinned now")
	}
	// Roll the pinned clock back to the day before the due date — the same
	// task is no longer overdue, proving the function used the argument
	// rather than time.Now().
	earlier := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	if task.IsOverdueAt(earlier) {
		t.Error("at an earlier `now`, the same DueDate should not be overdue")
	}
}

// ── IsDueToday ────────────────────────────────────────────────────────────────

func TestIsDueToday(t *testing.T) {
	task := New("Today test")

	// No due date
	if task.IsDueToday() {
		t.Error("task without due date should not be due today")
	}

	// Due today
	now := time.Now()
	task.DueDate = time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	if !task.IsDueToday() {
		t.Error("task due today should return true")
	}

	// Due tomorrow
	task.DueDate = now.AddDate(0, 0, 1)
	if task.IsDueToday() {
		t.Error("task due tomorrow should not be due today")
	}

	// Due yesterday
	task.DueDate = now.AddDate(0, 0, -1)
	if task.IsDueToday() {
		t.Error("task due yesterday should not be due today")
	}

	// Done task due today
	task.DueDate = time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	task.Toggle()
	if task.IsDueToday() {
		t.Error("completed task should not be due today")
	}
}

// ── IsDueSoon ─────────────────────────────────────────────────────────────────

func TestIsDueSoon(t *testing.T) {
	task := New("Soon test")

	// No due date
	if task.IsDueSoon(7) {
		t.Error("task without due date should not be due soon")
	}

	// Due in 3 days, 7 day window
	task.DueDate = time.Now().AddDate(0, 0, 3)
	if !task.IsDueSoon(7) {
		t.Error("task due in 3 days should be due soon (7 day window)")
	}

	// Due in 10 days, 7 day window
	task.DueDate = time.Now().AddDate(0, 0, 10)
	if task.IsDueSoon(7) {
		t.Error("task due in 10 days should not be due soon (7 day window)")
	}

	// Due yesterday (overdue, not "soon")
	task.DueDate = time.Now().AddDate(0, 0, -1)
	if task.IsDueSoon(7) {
		t.Error("overdue task should not be due soon")
	}

	// Done task
	task.DueDate = time.Now().AddDate(0, 0, 1)
	task.Toggle()
	if task.IsDueSoon(7) {
		t.Error("completed task should not be due soon")
	}
}

// ── HasOverdueDependencyFast ──────────────────────────────────────────────────

func TestHasOverdueDependencyFast(t *testing.T) {
	task := New("Dep check")
	task.Dependencies = []string{"dep-1", "dep-2", "dep-3"}

	// No overdue deps
	overdueSet := map[string]bool{}
	if task.HasOverdueDependencyFast(overdueSet) {
		t.Error("should not have overdue dep with empty set")
	}

	// One dep is overdue
	overdueSet["dep-2"] = true
	if !task.HasOverdueDependencyFast(overdueSet) {
		t.Error("should detect overdue dependency")
	}

	// Overdue ID not in our deps
	overdueSet = map[string]bool{"other-task": true}
	if task.HasOverdueDependencyFast(overdueSet) {
		t.Error("should not flag unrelated overdue tasks")
	}

	// No dependencies at all
	empty := New("No deps")
	if empty.HasOverdueDependencyFast(map[string]bool{"x": true}) {
		t.Error("task with no deps should never have overdue dep")
	}
}

// ── Notes ─────────────────────────────────────────────────────────────────────

func TestSetNotes(t *testing.T) {
	task := New("Notes test")

	task.SetNotes("Some notes here")
	if task.Notes != "Some notes here" {
		t.Errorf("Notes = %q, want %q", task.Notes, "Some notes here")
	}

	task.SetNotes("")
	if task.Notes != "" {
		t.Error("Notes should be empty after clearing")
	}
}

// ── SetProject ────────────────────────────────────────────────────────────────

func TestSetProject(t *testing.T) {
	task := New("Project test")

	task.SetProject("my-project")
	if task.Project != "my-project" {
		t.Errorf("Project = %q, want %q", task.Project, "my-project")
	}

	task.SetProject("")
	if task.Project != "" {
		t.Error("Project should be empty after clearing")
	}
}

// ── SetPriority ───────────────────────────────────────────────────────────────

func TestSetPriority(t *testing.T) {
	task := New("Priority test")

	task.SetPriority(PriorityHigh)
	if task.Priority != PriorityHigh {
		t.Errorf("Priority = %v, want PriorityHigh", task.Priority)
	}

	task.SetPriority(PriorityMedium)
	if task.Priority != PriorityMedium {
		t.Errorf("Priority = %v, want PriorityMedium", task.Priority)
	}
}

// ── SetDueDate / SetStartDate ─────────────────────────────────────────────────

func TestSetDueDate(t *testing.T) {
	task := New("Due date test")
	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	task.SetDueDate(date)
	if !task.DueDate.Equal(date) {
		t.Errorf("DueDate = %v, want %v", task.DueDate, date)
	}
}

func TestSetStartDate(t *testing.T) {
	task := New("Start date test")
	// A non-today date has no entry time, so it lands at 09:00 local.
	date := time.Date(2025, 6, 1, 0, 0, 0, 0, time.Local)

	task.SetStartDate(date)
	want := time.Date(2025, 6, 1, 9, 0, 0, 0, time.Local)
	if !task.StartDate.Equal(want) {
		t.Errorf("StartDate = %v, want %v (non-today defaults to 09:00)", task.StartDate, want)
	}
}

// Setting the start date to today records the moment of entry (a real time of
// day), so start→done cycle time is minute-precise rather than day-rounded.
func TestSetStartDateTodayRecordsTime(t *testing.T) {
	task := New("start today")
	n := time.Now()
	todayMidnight := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
	before := time.Now()
	task.SetStartDate(todayMidnight) // mirrors parseDueDate("today")
	after := time.Now()

	if task.StartDate.Before(before) || task.StartDate.After(after) {
		t.Errorf("StartDate = %v, want a timestamp between %v and %v", task.StartDate, before, after)
	}
	if !sameDay(task.StartDate, before) {
		t.Errorf("StartDate = %v, want today", task.StartDate)
	}
}

// ── Time tracking ─────────────────────────────────────────────────────────────

func TestTimeTracking(t *testing.T) {
	task := New("Timer test")

	if task.IsTimerRunning() {
		t.Error("timer should not be running initially")
	}

	task.StartTimer()
	if !task.IsTimerRunning() {
		t.Error("timer should be running after start")
	}
	if len(task.TimeEntries) != 1 {
		t.Fatalf("expected 1 time entry, got %d", len(task.TimeEntries))
	}
	if task.TimeEntries[0].ID == "" {
		t.Error("time entry ID should not be empty")
	}

	time.Sleep(10 * time.Millisecond)
	task.StopTimer()
	if task.IsTimerRunning() {
		t.Error("timer should not be running after stop")
	}

	spent := task.TotalTimeSpent()
	if spent < 10*time.Millisecond {
		t.Errorf("total time = %v, expected >= 10ms", spent)
	}
}

func TestStartTimerBackfillsStartDate(t *testing.T) {
	task := New("no start date yet")
	if !task.StartDate.IsZero() {
		t.Fatal("a fresh task should have no start date")
	}

	before := time.Now()
	task.StartTimer()
	after := time.Now()

	if task.StartDate.IsZero() {
		t.Fatal("starting the timer should backfill StartDate")
	}
	// Backfilled with the actual start moment (time of day, not midnight).
	if task.StartDate.Before(before) || task.StartDate.After(after) {
		t.Errorf("StartDate = %v, want a timestamp between %v and %v", task.StartDate, before, after)
	}
}

func TestStartTimerKeepsExistingStartDate(t *testing.T) {
	task := New("already scheduled")
	task.SetStartDate(time.Date(2026, 1, 15, 0, 0, 0, 0, time.Local))
	stored := task.StartDate // 2026-01-15 09:00 after SetStartDate's normalization

	task.StartTimer()

	if !task.StartDate.Equal(stored) {
		t.Errorf("StartDate = %v, a manually-set start date must not be overwritten (want %v)", task.StartDate, stored)
	}
}

func TestStartTimerStopsPrevious(t *testing.T) {
	task := New("Timer overlap")

	task.StartTimer()
	time.Sleep(5 * time.Millisecond)
	task.StartTimer() // should stop the first one

	if len(task.TimeEntries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(task.TimeEntries))
	}
	// First entry should be stopped
	if task.TimeEntries[0].IsRunning() {
		t.Error("first entry should be stopped")
	}
	// Second entry should be running
	if !task.TimeEntries[1].IsRunning() {
		t.Error("second entry should be running")
	}
}

func TestDeleteTimeEntry(t *testing.T) {
	task := New("Timer delete")
	task.StartTimer()
	task.StopTimer()
	task.StartTimer()
	task.StopTimer()

	if len(task.TimeEntries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(task.TimeEntries))
	}

	task.DeleteTimeEntry(0)
	if len(task.TimeEntries) != 1 {
		t.Fatalf("expected 1 entry after delete, got %d", len(task.TimeEntries))
	}

	// Out of bounds
	task.DeleteTimeEntry(99)
	task.DeleteTimeEntry(-1)
	if len(task.TimeEntries) != 1 {
		t.Fatal("out of bounds delete changed length")
	}
}

// ── Priority String/Icon ──────────────────────────────────────────────────────

func TestPriorityString(t *testing.T) {
	tests := []struct {
		prio Priority
		str  string
		icon string
	}{
		{PriorityLow, "low", "↓"},
		{PriorityMedium, "medium", "→"},
		{PriorityHigh, "high", "↑"},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			if got := tt.prio.String(); got != tt.str {
				t.Errorf("String() = %q, want %q", got, tt.str)
			}
			if got := tt.prio.Icon(); got != tt.icon {
				t.Errorf("Icon() = %q, want %q", got, tt.icon)
			}
		})
	}
}

// ── IsTopLevel ────────────────────────────────────────────────────────────────

func TestIsTopLevel(t *testing.T) {
	parent := New("Parent")
	if !parent.IsTopLevel() {
		t.Error("task without ParentID should be top-level")
	}

	child := NewSubtask("Child", parent.ID)
	if child.IsTopLevel() {
		t.Error("subtask should not be top-level")
	}
}

// ── ModifiedAt updates ────────────────────────────────────────────────────────

func TestModifiedAtUpdates(t *testing.T) {
	task := New("Modified test")
	original := task.ModifiedAt

	time.Sleep(1 * time.Millisecond)
	task.SetPriority(PriorityHigh)
	if !task.ModifiedAt.After(original) {
		t.Error("ModifiedAt should update after SetPriority")
	}

	original = task.ModifiedAt
	time.Sleep(1 * time.Millisecond)
	task.SetProject("test-project")
	if !task.ModifiedAt.After(original) {
		t.Error("ModifiedAt should update after SetProject")
	}

	original = task.ModifiedAt
	time.Sleep(1 * time.Millisecond)
	task.AddTag("test")
	if !task.ModifiedAt.After(original) {
		t.Error("ModifiedAt should update after AddTag")
	}

	original = task.ModifiedAt
	time.Sleep(1 * time.Millisecond)
	task.AddComment("test")
	if !task.ModifiedAt.After(original) {
		t.Error("ModifiedAt should update after AddComment")
	}

	original = task.ModifiedAt
	time.Sleep(1 * time.Millisecond)
	task.SetNotes("test")
	if !task.ModifiedAt.After(original) {
		t.Error("ModifiedAt should update after SetNotes")
	}

	original = task.ModifiedAt
	time.Sleep(1 * time.Millisecond)
	task.Toggle()
	if !task.ModifiedAt.After(original) {
		t.Error("ModifiedAt should update after Toggle")
	}
}

// ── TimeEntry Duration ────────────────────────────────────────────────────────

func TestTimeEntryDuration(t *testing.T) {
	start := time.Now().Add(-1 * time.Hour)
	stop := time.Now()

	entry := TimeEntry{
		ID:        "te-1",
		StartedAt: start,
		StoppedAt: stop,
	}

	d := entry.Duration()
	if d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("Duration() = %v, expected ~1 hour", d)
	}
}

func TestTimeEntryRunning(t *testing.T) {
	running := TimeEntry{
		ID:        "te-1",
		StartedAt: time.Now(),
	}
	if !running.IsRunning() {
		t.Error("entry without StoppedAt should be running")
	}

	stopped := TimeEntry{
		ID:        "te-2",
		StartedAt: time.Now().Add(-1 * time.Hour),
		StoppedAt: time.Now(),
	}
	if stopped.IsRunning() {
		t.Error("entry with StoppedAt should not be running")
	}
}

func TestInheritContextFrom(t *testing.T) {
	parent := New("Parent")
	parent.Project = "alpha"
	parent.AddTag("Work")
	parent.AddTag("urgent")
	parent.DueDate = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	child := NewSubtask("Child", parent.ID)
	child.InheritContextFrom(&parent)

	if child.Project != "alpha" {
		t.Errorf("project = %q, want alpha", child.Project)
	}
	if len(child.Tags) != 2 || child.Tags[0] != "work" || child.Tags[1] != "urgent" {
		t.Errorf("tags = %v, want [work urgent]", child.Tags)
	}
	if !child.DueDate.Equal(parent.DueDate) {
		t.Errorf("due date = %v, want %v (inherited)", child.DueDate, parent.DueDate)
	}

	// Nil parent is a no-op.
	orphan := NewSubtask("Orphan", "no-parent")
	orphan.InheritContextFrom(nil)
	if orphan.Project != "" || len(orphan.Tags) != 0 {
		t.Errorf("nil parent should leave subtask untouched, got project=%q tags=%v",
			orphan.Project, orphan.Tags)
	}

	// Parent with no due date leaves child undated.
	undatedParent := New("Undated parent")
	undatedChild := NewSubtask("Child", undatedParent.ID)
	undatedChild.InheritContextFrom(&undatedParent)
	if !undatedChild.DueDate.IsZero() {
		t.Errorf("undated parent should leave subtask undated, got %v", undatedChild.DueDate)
	}
}

func TestParseRecurrence(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", "", true},
		{"daily", "daily", true},
		{"Daily ", "daily", true},
		{"day", "daily", true},
		{"weekly", "weekly", true},
		{"week", "weekly", true},
		{"monthly", "monthly", true},
		{"yearly", "yearly", true},
		{"annual", "yearly", true},
		{"weekdays", "weekdays", true},
		{"weekday", "weekdays", true},
		{"every:3d", "every:3d", true},
		{"every:2w", "every:2w", true},
		{"every:6m", "every:6m", true},
		{"every:1d", "daily", true}, // collapsed to canonical
		{"every:1w", "weekly", true},
		{"every:1m", "monthly", true},
		{"every:1y", "yearly", true},
		{"3d", "every:3d", true}, // shorthand
		{"2w", "every:2w", true},
		{"every:0d", "", false},
		{"every:d", "", false},
		{"every:3x", "", false},
		{"nonsense", "", false},
	}
	for _, c := range cases {
		got, ok := ParseRecurrence(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("ParseRecurrence(%q) = (%q,%v), want (%q,%v)",
				c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestNextRecurrenceFrom(t *testing.T) {
	base := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC) // Monday
	cases := []struct {
		rule string
		want time.Time
	}{
		{"daily", time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)},
		{"weekly", time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)},
		{"monthly", time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)},
		{"yearly", time.Date(2027, 6, 15, 9, 0, 0, 0, time.UTC)},
		{"every:3d", time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)},
		{"every:2w", time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)},
		{"every:2m", time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)},
		{"weekdays", time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)}, // Mon → Tue
	}
	for _, c := range cases {
		got, ok := NextRecurrenceFrom(c.rule, base)
		if !ok || !got.Equal(c.want) {
			t.Errorf("NextRecurrenceFrom(%q, base) = (%v,%v), want (%v,true)",
				c.rule, got, ok, c.want)
		}
	}

	// Friday → Monday for weekdays.
	fri := time.Date(2026, 6, 19, 9, 0, 0, 0, time.UTC)
	if got, ok := NextRecurrenceFrom("weekdays", fri); !ok ||
		!got.Equal(time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("weekdays from Friday = %v, want next Monday", got)
	}

	// Empty rule and zero base return false.
	if _, ok := NextRecurrenceFrom("", base); ok {
		t.Error("empty rule should return false")
	}
	if _, ok := NextRecurrenceFrom("daily", time.Time{}); ok {
		t.Error("zero base should return false")
	}
	if _, ok := NextRecurrenceFrom("garbage", base); ok {
		t.Error("garbage rule should return false")
	}
}

func TestRecurrenceMutators(t *testing.T) {
	td := New("recurring")
	if td.IsRecurring() {
		t.Error("new task should not be recurring")
	}
	td.SetRecurrence("daily")
	if !td.IsRecurring() || td.Recurrence != "daily" {
		t.Errorf("after SetRecurrence: IsRecurring=%v, Recurrence=%q",
			td.IsRecurring(), td.Recurrence)
	}
	td.ClearRecurrence()
	if td.IsRecurring() || td.Recurrence != "" {
		t.Errorf("after ClearRecurrence: IsRecurring=%v, Recurrence=%q",
			td.IsRecurring(), td.Recurrence)
	}
}

// ── StampModified ─────────────────────────────────────────────────────────────

// TestStampModifiedNormalClock checks that, with a normal wall clock (prev well
// behind now), StampModified returns approximately now (not the zero-time floor).
func TestStampModifiedNormalClock(t *testing.T) {
	before := time.Now()
	got := StampModified(time.Time{}) // zero prev → plain now
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("StampModified(zero) = %v, want a value between %v and %v", got, before, after)
	}
}

// TestStampModifiedFuturePrev checks the slow-clock guard: when prev is in the
// future (as if stamped by a faster device), the result is prev+1ms so the new
// edit is strictly later than the version it replaces.
func TestStampModifiedFuturePrev(t *testing.T) {
	future := time.Now().Add(10 * time.Second)
	got := StampModified(future)
	want := future.Add(time.Millisecond)
	if !got.Equal(want) {
		t.Errorf("StampModified(future) = %v, want prev+1ms = %v", got, want)
	}
}

// TestStampModifiedZeroPrev checks that a zero prev (brand-new record with no
// history) returns a normal wall-clock value and not epoch+1ms.
func TestStampModifiedZeroPrev(t *testing.T) {
	before := time.Now()
	got := StampModified(time.Time{})
	after := time.Now()
	epoch := time.Time{}.Add(time.Millisecond) // 0001-01-01 00:00:00.001 UTC
	if got.Equal(epoch) {
		t.Errorf("StampModified(zero) returned epoch+1ms; want a real now")
	}
	if got.Before(before) || got.After(after) {
		t.Errorf("StampModified(zero) = %v, want a value between %v and %v", got, before, after)
	}
}
