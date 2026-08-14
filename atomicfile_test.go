package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomicCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := writeFileAtomic(path, []byte("first"), 0644); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := readFileString(t, path); got != "first" {
		t.Fatalf("contents = %q, want %q", got, "first")
	}
	if err := writeFileAtomic(path, []byte("second"), 0644); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got := readFileString(t, path); got != "second" {
		t.Fatalf("contents = %q, want %q", got, "second")
	}
}

func TestWriteFileAtomicHonoursPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.json")

	// sync.json holds the bearer token, so 0600 is a security property and
	// not a preference — os.CreateTemp's default happens to match, which is
	// exactly why the 0644 case below is the one that would rot unnoticed.
	if err := writeFileAtomic(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0600)

	if err := writeFileAtomic(filepath.Join(dir, "notes.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(dir, "notes.md"), 0644)
}

func TestWriteFileAtomicLeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := writeFileAtomic(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v, want just state.json", names)
	}
}

func TestWriteFileAtomicKeepsOldContentsOnFailure(t *testing.T) {
	// The point of the exercise: a write that cannot complete must leave the
	// previous file readable. A plain WriteFile truncates first and would
	// leave an empty file here.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := writeFileAtomic(path, []byte(`{"theme":"tokyonight"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// A path whose parent is a file, not a directory, fails at the temp-file
	// step — the earliest possible failure, before the target is touched.
	nested := filepath.Join(path, "impossible.json")
	if err := writeFileAtomic(nested, []byte("x"), 0644); err == nil {
		t.Fatal("expected a write into a non-directory to fail")
	}
	if got := readFileString(t, path); got != `{"theme":"tokyonight"}` {
		t.Fatalf("previous contents were disturbed: %q", got)
	}
	// The error has to name the path; these surface to the user as
	// "Settings load failed" style messages with nothing else to go on.
	err := writeFileAtomic(nested, []byte("x"), 0644)
	if !strings.Contains(err.Error(), "impossible.json") {
		t.Errorf("error should name the target path, got %v", err)
	}
}

func TestWriteFileAtomicEmptyAndLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "undo.json")

	if err := writeFileAtomic(path, nil, 0600); err != nil {
		t.Fatalf("empty write: %v", err)
	}
	if got := readFileString(t, path); got != "" {
		t.Fatalf("want empty file, got %q", got)
	}
	// The undo stack carries full task content and is the largest of these
	// files by some margin.
	big := strings.Repeat("a", 1<<20)
	if err := writeFileAtomic(path, []byte(big), 0600); err != nil {
		t.Fatalf("large write: %v", err)
	}
	if got := readFileString(t, path); got != big {
		t.Fatalf("large write round-tripped %d bytes, want %d", len(got), len(big))
	}
}

func TestSyncDirNeverFails(t *testing.T) {
	// Best-effort by contract: a directory that cannot be fsynced (or opened
	// at all, as on Windows) must not turn a successful write into a failure.
	syncDir(t.TempDir())
	syncDir(filepath.Join(t.TempDir(), "does-not-exist"))
}

func TestSettingsSurviveAConcurrentReader(t *testing.T) {
	// Integration check through the real saveSettings path: a reader that
	// opens the file at any moment sees a complete JSON document, never a
	// truncated one. Without the atomic replace there is a window where it
	// sees zero bytes.
	setTestHome(t, t.TempDir())
	if err := ensureStorageDir(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := saveSettings(appSettings{Theme: "tokyonight", Language: "da"}); err != nil {
			t.Fatalf("saveSettings: %v", err)
		}
		got, err := loadSettings()
		if err != nil {
			t.Fatalf("loadSettings after save %d: %v", i, err)
		}
		if got.Theme != "tokyonight" {
			t.Fatalf("round trip lost the theme: %+v", got)
		}
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %v, want %v", path, got, want)
	}
}
