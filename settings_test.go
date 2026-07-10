package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// TestMain (main_test.go) already redirects $HOME to a temp dir, so
// settingsPath() points into a sandbox and these tests can write/read
// settings.json freely without touching the user's real config.

func TestLoadSettingsMissingFileNoError(t *testing.T) {
	// Brand-new install: settings.json doesn't exist yet.
	os.Remove(settingsPath())
	s, err := loadSettings()
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if s != (appSettings{}) {
		t.Errorf("expected zero appSettings on missing file, got %+v", s)
	}
}

func TestLoadSettingsCorruptFileErrors(t *testing.T) {
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(settingsPath(), []byte("{not json"), 0644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	defer os.Remove(settingsPath())

	_, err := loadSettings()
	if err == nil {
		t.Fatal("corrupt JSON should return an error, got nil")
	}
}

// TestLoadSettingsMigratesLegacyVersion0 mirrors the task-file pattern: a
// settings.json written before the Version field existed has Version=0 in
// Go's decoder, and migrateSettings must bring it up to current without
// dropping the user's preferences.
func TestLoadSettingsMigratesLegacyVersion0(t *testing.T) {
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := `{"theme":"tokyonight","task_sort":1}`
	if err := os.WriteFile(settingsPath(), []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	defer os.Remove(settingsPath())

	s, err := loadSettings()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.Version != currentSettingsVersion {
		t.Errorf("migrated Version = %d, want %d", s.Version, currentSettingsVersion)
	}
	if s.Theme != "tokyonight" {
		t.Errorf("Theme lost during migration: %q", s.Theme)
	}
	if s.TaskSort != taskSortDueDate {
		t.Errorf("TaskSort lost: %v", s.TaskSort)
	}
}

// TestSaveSettingsStampsVersion confirms saveSettings always writes the
// current schema version, even if the caller forgot to set it.
func TestSaveSettingsStampsVersion(t *testing.T) {
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.Remove(settingsPath())

	in := appSettings{Theme: "test", SeqBiasDeadline: biasIntense}
	if err := saveSettings(in); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Re-read the raw JSON to confirm the version is on disk (not just an
	// in-memory side effect of loadSettings).
	raw, err := os.ReadFile(settingsPath())
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	var onDisk appSettings
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if onDisk.Version != currentSettingsVersion {
		t.Errorf("on-disk version = %d, want %d", onDisk.Version, currentSettingsVersion)
	}
	if onDisk.Theme != "test" || onDisk.SeqBiasDeadline != biasIntense {
		t.Errorf("payload lost on round-trip: %+v", onDisk)
	}
}

// TestSettingsTopPreviewAppearsInView checks that the bias-knob preview block
// is present when the Settings tab is rendered with at least one pending task.
func TestSettingsTopPreviewAppearsInView(t *testing.T) {
	tasks := []todo.Todo{
		mkTodo("t1", "Alpha task", todo.Pending),
		mkTodo("t2", "Beta task", todo.Pending),
	}
	m := modelWithTasks(t, tasks...)
	m.tab = tabSettings
	m.taskSort = taskSortSequence
	applyBiases(defaultBiases())
	m.ensureCache()

	out := m.renderSettingsList()
	if !strings.Contains(out, "Top 5 with these weights:") {
		t.Errorf("Settings view should contain preview header, got:\n%s", out)
	}
}

// TestSettingsTopPreviewEmptyWhenNoTasks checks that the preview is absent
// (no header, no rows) when there are no pending tasks.
func TestSettingsTopPreviewEmptyWhenNoTasks(t *testing.T) {
	m := modelWithTasks(t)
	m.tab = tabSettings
	applyBiases(defaultBiases())
	m.ensureCache()

	out := m.renderSettingsList()
	if strings.Contains(out, "Top 5 with these weights:") {
		t.Errorf("Settings view should not show preview header when there are no tasks:\n%s", out)
	}
}

// TestSettingsTopPreviewRespectsWeights verifies that the preview ranks tasks
// in the order the biases imply: with Priority=Intense and Deadline=Relaxed,
// a high-priority task with no due date should rank above a medium-priority
// task with an imminent due date when the preview is computed directly via
// rankTopBySequenceWith.
func TestSettingsTopPreviewRespectsWeights(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	highPri := mkTodo("hp", "High priority, no deadline", todo.Pending)
	highPri.Priority = todo.PriorityHigh
	// No due date — Deadline dimension contributes 0.

	dueUrgent := mkTodo("du", "Medium priority, urgent deadline", todo.Pending)
	dueUrgent.Priority = todo.PriorityMedium
	dueUrgent.DueDate = now.AddDate(0, 0, 1) // due tomorrow (7 days window → ~8.9 urgency)

	todos := []todo.Todo{highPri, dueUrgent}
	heat := activityHeat{
		tasks:    make(map[string]bool),
		projects: make(map[string]bool),
		tags:     make(map[string]bool),
	}

	// With Priority=Intense (2×) and Deadline=Relaxed (0.5×):
	// highPri:  Priority(10) × 2.0 = 20 + small age
	// dueUrgent: Priority(5) × 2.0 + Urgency(~8.9) × 0.5 = 10 + ~4.45 ≈ 14.45
	// → highPri should rank first.
	intensePri := biases{
		Priority: biasIntense,
		Deadline: biasRelaxed,
		Momentum: biasBalanced,
		Aging:    true,
	}
	ranked := rankTopBySequenceWith(todos, intensePri, heat, now)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked tasks, got %d", len(ranked))
	}
	if ranked[0].ID != "hp" {
		t.Errorf("Priority=Intense ranking: expected high-priority task first, got %q (score order: %s, %s)",
			ranked[0].ID,
			fmt.Sprintf("%.2f", sequenceComponentsAt(now, &ranked[0], intensePri, heat).Total),
			fmt.Sprintf("%.2f", sequenceComponentsAt(now, &ranked[1], intensePri, heat).Total),
		)
	}

	// With Deadline=Intense (2×) and Priority=Relaxed (0.5×):
	// highPri:  Priority(10) × 0.5 = 5 + small age
	// dueUrgent: Priority(5) × 0.5 + Urgency(~8.9) × 2.0 = 2.5 + ~17.8 ≈ 20.3
	// → dueUrgent should rank first.
	intenseDeadline := biases{
		Priority: biasRelaxed,
		Deadline: biasIntense,
		Momentum: biasBalanced,
		Aging:    true,
	}
	ranked2 := rankTopBySequenceWith(todos, intenseDeadline, heat, now)
	if len(ranked2) != 2 {
		t.Fatalf("expected 2 ranked tasks, got %d", len(ranked2))
	}
	if ranked2[0].ID != "du" {
		t.Errorf("Deadline=Intense ranking: expected urgent-deadline task first, got %q (score order: %s, %s)",
			ranked2[0].ID,
			fmt.Sprintf("%.2f", sequenceComponentsAt(now, &ranked2[0], intenseDeadline, heat).Total),
			fmt.Sprintf("%.2f", sequenceComponentsAt(now, &ranked2[1], intenseDeadline, heat).Total),
		)
	}
}

// TestSettingsTopPreviewNoWrap guards the no-wrap contract: the Settings view
// output must not exceed termWidth on any line, even with the preview block.
func TestSettingsTopPreviewNoWrap(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 120} {
		tasks := []todo.Todo{
			mkTodo("t1", "Task one with a rather long name for testing layout", todo.Pending),
			mkTodo("t2", "Task two also somewhat verbose", todo.Pending),
			mkTodo("t3", "Task three", todo.Pending),
		}
		m := modelWithTasks(t, tasks...)
		m.tab = tabSettings
		m.termWidth = width
		m.termHeight = 40
		applyBiases(defaultBiases())
		m.ensureCache()

		out := m.View()
		for n, line := range strings.Split(out, "\n") {
			if lw := ansi.StringWidth(line); lw > width {
				t.Errorf("width=%d: line %d is %d cells wide: %q", width, n, lw, line)
			}
		}
	}
}
