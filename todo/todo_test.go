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

	// Remove non-existent — should not panic or change length
	task.RemoveTag("nonexistent")
	if len(task.Tags) != 1 {
		t.Fatal("removing nonexistent tag changed length")
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

	// Remove non-existent
	task.RemoveDependency("nonexistent")
	if len(task.Dependencies) != 1 {
		t.Fatal("removing nonexistent dep changed length")
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
	date := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	task.SetStartDate(date)
	if !task.StartDate.Equal(date) {
		t.Errorf("StartDate = %v, want %v", task.StartDate, date)
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
