package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
