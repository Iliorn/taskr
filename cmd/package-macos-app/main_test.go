package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageAppCreatesDoubleClickableBundle(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "taskr-darwin")
	wantBinary := []byte("fake Mach-O taskr binary")
	if err := os.WriteFile(binaryPath, wantBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "taskr-macos-app.zip")
	if err := packageApp(binaryPath, archivePath, "v1.28.1"); err != nil {
		t.Fatalf("packageApp: %v", err)
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	entries := make(map[string]*zip.File)
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	bin := entries["Taskr.app/Contents/MacOS/taskr"]
	if bin == nil {
		t.Fatal("archive missing Taskr.app executable")
	}
	if bin.Mode().Perm() != 0o755 {
		t.Errorf("app executable mode = %o, want 755", bin.Mode().Perm())
	}
	if got := string(readZipFile(t, bin)); got != string(wantBinary) {
		t.Errorf("bundled executable = %q, want %q", got, wantBinary)
	}

	plist := entries["Taskr.app/Contents/Info.plist"]
	if plist == nil {
		t.Fatal("archive missing Info.plist")
	}
	plistText := string(readZipFile(t, plist))
	for _, want := range []string{"<string>taskr</string>", "<string>com.iliorn.taskr</string>", "<string>1.28.1</string>"} {
		if !strings.Contains(plistText, want) {
			t.Errorf("Info.plist missing %q", want)
		}
	}
}

func TestPackageAppRejectsInvalidVersion(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "taskr")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := packageApp(binaryPath, filepath.Join(t.TempDir(), "out.zip"), "latest"); err == nil {
		t.Fatal("packageApp should reject a non-semantic version")
	}
}

func readZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()
	r, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
