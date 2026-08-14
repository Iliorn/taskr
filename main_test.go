package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testHome is the fake home directory TestMain redirects the whole binary into.
// Kept in a package var so TestStorageStaysInsideTheTestHome can assert the
// redirect actually took, rather than trusting that it did.
var testHome string

// TestMain isolates the entire test binary from the real ~/.taskr. Storage
// paths derive from os.UserHomeDir (getStoragePath/dbPath/taskrDir), and several
// tests build a model via initialModel — which opens the store — so without
// this redirect a plain `go test` would read and create files under the
// developer's real task directory.
//
// os.UserHomeDir reads a *different* variable per platform: $HOME on unix,
// %USERPROFILE% on Windows. Setting only HOME therefore left Windows test runs
// pointed at the real home — the exact accident this function exists to
// prevent — so both are set.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "taskr-test-home")
	if err != nil {
		panic(err)
	}
	testHome = tmp
	os.Setenv("HOME", tmp)
	os.Setenv("USERPROFILE", tmp) // os.UserHomeDir on Windows
	// Paths now resolve through XDG (paths.go), and an XDG_* variable exported
	// in the developer's shell is absolute — it would send the whole suite to
	// the real ~/.local/share while HOME pointed somewhere harmless. Redirect
	// them into the temp home rather than unsetting them, so the XDG branch is
	// what the tests actually exercise.
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		os.Unsetenv(v)
	}
	os.Unsetenv("TASKR_HOME")
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// setTestHome points os.UserHomeDir at dir for the duration of one test,
// restoring the previous value afterwards. Both variables, for the reason
// TestMain sets both: t.Setenv("HOME", …) alone is a no-op on Windows, where
// os.UserHomeDir reads %USERPROFILE% — so a test "isolating" itself that way
// was still writing into the shared test home, and the files it left behind
// (a legacy tasks.json, a settings.json) leaked into every later test.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	// Same reason TestMain clears these: an absolute XDG_* or TASKR_HOME would
	// override the home this function just redirected, and the test would write
	// outside its own directory.
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "TASKR_HOME"} {
		t.Setenv(v, "")
	}
}

// The redirect is load-bearing: if it silently stops working on some platform,
// the suite starts writing to the developer's real ~/.taskr. Assert every
// storage path lands inside the temp home instead of finding out the hard way.
func TestStorageStaysInsideTheTestHome(t *testing.T) {
	if testHome == "" {
		t.Fatal("TestMain did not record the temp home")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	if home != testHome {
		t.Fatalf("os.UserHomeDir() = %q, want the temp home %q — on %s it reads a variable TestMain does not set",
			home, testHome, runtime.GOOS)
	}
	// Every path the app can write, not just the database: config, state and
	// cache resolve through different roots now, and each is a way out of the
	// temp home if the redirect misses one.
	for _, path := range []string{
		taskrDir(), dbPath(), getStoragePath(), settingsPath(),
		syncConfigPath(), syncStatePath(), syncLogPath(), serveStatePath(),
		undoPersistPath(), lastAddedPath(), notesFilePath("some-task-id"),
	} {
		if !strings.HasPrefix(path, testHome+string(filepath.Separator)) {
			t.Errorf("%q is outside the test home %q", path, testHome)
		}
	}
}

// A test that redirects the home directory must do it for every platform. The
// bare t.Setenv("HOME", …) form silently does nothing on Windows, where
// os.UserHomeDir reads %USERPROFILE% — the test then writes into the shared
// test home and leaves files behind for everything that runs after it. That is
// exactly how three Windows-only failures appeared: a legacy tasks.json from an
// import test seeded every database opened afterwards.
func TestNoBareHomeRedirectsInTests(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") || e.Name() == "main_test.go" {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `Setenv("HOME"`) {
			t.Errorf(`%s redirects HOME directly — use setTestHome(t, dir), which also sets `+
				`USERPROFILE so the redirect works on Windows`, e.Name())
		}
	}
}

// mustMkdirFor creates the directory a path lives in. Tests that write one of
// taskr's files directly need it now that config, data and state resolve to
// three different directories, only one of which the app creates eagerly.
func mustMkdirFor(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
}
