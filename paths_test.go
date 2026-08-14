package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// clearPathEnv puts one test in a known state: a fresh home, no XDG variables,
// no TASKR_HOME, no legacy directory.
func clearPathEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	setTestHome(t, dir)
	return dir
}

// An existing ~/.taskr keeps everything where it was. This is the promise that
// makes the whole change safe: nobody's database moves because taskr grew an
// opinion about the XDG spec.
func TestLegacyDirectoryKeepsItsFiles(t *testing.T) {
	home := clearPathEnv(t)
	legacy := filepath.Join(home, legacyDirName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"database": dbPath(),
		"settings": settingsPath(),
		"undo":     undoPersistPath(),
		"sync":     syncConfigPath(),
	} {
		if filepath.Dir(got) != legacy {
			t.Errorf("%s = %q, want it left in the legacy directory %q", name, got, legacy)
		}
	}
	if !usingLegacyLayout() {
		t.Error("usingLegacyLayout() = false with ~/.taskr present")
	}
}

// A file called .taskr is not a legacy install.
func TestLegacyDetectionIgnoresAFile(t *testing.T) {
	home := clearPathEnv(t)
	if err := os.WriteFile(filepath.Join(home, legacyDirName), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if usingLegacyLayout() {
		t.Error("a regular file named .taskr was treated as the legacy directory")
	}
}

// TASKR_HOME collapses the split back into one directory, for people who would
// rather back up a single path.
func TestTaskrHomeOverridesEverything(t *testing.T) {
	clearPathEnv(t)
	one := t.TempDir()
	t.Setenv("TASKR_HOME", one)
	for _, got := range []string{dbPath(), settingsPath(), undoPersistPath(), syncLogPath(), notesFilePath("x")} {
		if !strings.HasPrefix(got, one) {
			t.Errorf("%q ignored TASKR_HOME=%q", got, one)
		}
	}
	if usingLegacyLayout() {
		t.Error("TASKR_HOME should not report the legacy layout")
	}
}

// An explicitly exported XDG variable wins on every platform, including the two
// that have conventions of their own.
func TestExplicitXDGWinsEverywhere(t *testing.T) {
	clearPathEnv(t)
	data, config, state, cache := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CACHE_HOME", cache)

	cases := []struct{ got, wantBase, name string }{
		{dbPath(), data, "database"},
		{settingsPath(), config, "settings"},
		{syncConfigPath(), config, "sync config"},
		{undoPersistPath(), state, "undo"},
		{syncLogPath(), state, "sync log"},
		{notesFilePath("id"), cache, "editor scratch"},
	}
	for _, c := range cases {
		want := filepath.Join(c.wantBase, appDirName)
		if !strings.HasPrefix(c.got, want) {
			t.Errorf("%s = %q, want it under %q", c.name, c.got, want)
		}
	}
}

// A relative XDG value is invalid per the spec and must be ignored rather than
// scattering files across the working directory.
func TestRelativeXDGValueIsIgnored(t *testing.T) {
	home := clearPathEnv(t)
	t.Setenv("XDG_DATA_HOME", "relative/path")
	got := dbPath()
	if !strings.HasPrefix(got, home) {
		t.Errorf("database = %q, want it back under the home %q", got, home)
	}
}

// The defaults, on a machine with nothing set.
func TestPlatformDefaults(t *testing.T) {
	home := clearPathEnv(t)
	db, settings, undo := dbPath(), settingsPath(), undoPersistPath()

	switch runtime.GOOS {
	case "windows":
		// The test home has no APPDATA/LOCALAPPDATA of its own, so assert the
		// shape rather than the exact root: config roams, data does not.
		if !strings.Contains(db, appDirName) || !strings.Contains(settings, appDirName) {
			t.Errorf("windows paths missing the app directory: %q / %q", db, settings)
		}
	case "darwin":
		want := filepath.Join(home, "Library", "Application Support", appDirName)
		if filepath.Dir(db) != want {
			t.Errorf("database = %q, want %q", db, want)
		}
	default:
		for _, c := range []struct{ got, want, name string }{
			{db, filepath.Join(home, ".local", "share", appDirName), "database"},
			{settings, filepath.Join(home, ".config", appDirName), "settings"},
			{undo, filepath.Join(home, ".local", "state", appDirName), "undo"},
		} {
			if filepath.Dir(c.got) != c.want {
				t.Errorf("%s = %q, want it in %q", c.name, c.got, c.want)
			}
		}
	}
}

// The four kinds must not collapse into one directory by accident — that is the
// whole point of splitting them.
func TestTheFourKindsAreDistinct(t *testing.T) {
	clearPathEnv(t)
	if runtime.GOOS == "darwin" {
		t.Skip("macOS deliberately shares Application Support between config, data and state")
	}
	seen := map[string]pathKind{}
	for _, kind := range []pathKind{pathConfig, pathData, pathState, pathCache} {
		dir, err := appDir(kind)
		if err != nil {
			t.Fatal(err)
		}
		if prev, dup := seen[dir]; dup {
			t.Errorf("kinds %d and %d both resolve to %q", prev, kind, dir)
		}
		seen[dir] = kind
	}
}
