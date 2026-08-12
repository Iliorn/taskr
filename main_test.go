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
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
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
	for _, path := range []string{taskrDir(), dbPath(), getStoragePath(), settingsPath()} {
		if !strings.HasPrefix(path, testHome+string(filepath.Separator)) {
			t.Errorf("%q is outside the test home %q", path, testHome)
		}
	}
}
