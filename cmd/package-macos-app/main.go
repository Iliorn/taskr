// Command package-macos-app wraps a macOS taskr executable in a Finder-
// launchable Taskr.app ZIP while preserving executable permissions.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

const infoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDisplayName</key>
  <string>Taskr</string>
  <key>CFBundleExecutable</key>
  <string>taskr</string>
  <key>CFBundleIdentifier</key>
  <string>com.iliorn.taskr</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>Taskr</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>{{VERSION}}</string>
  <key>CFBundleVersion</key>
  <string>{{VERSION}}</string>
</dict>
</plist>
`

var zipTimestamp = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

func main() {
	if len(os.Args) != 4 {
		log.Fatalf("usage: %s <macos-binary> <output.zip> <version>", os.Args[0])
	}
	if err := packageApp(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		log.Fatal(err)
	}
}

func packageApp(binaryPath, outputPath, version string) error {
	version = strings.TrimPrefix(version, "v")
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		return fmt.Errorf("version %q must be a three-part semantic version", version)
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("read macOS binary: %w", err)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create app archive: %w", err)
	}
	zw := zip.NewWriter(out)
	plist := strings.ReplaceAll(infoPlist, "{{VERSION}}", version)
	writeErr := writeAppBundle(zw, []byte(plist), binary)
	zipErr := zw.Close()
	closeErr := out.Close()
	if writeErr != nil {
		_ = os.Remove(outputPath)
		return writeErr
	}
	if zipErr != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("close app archive: %w", zipErr)
	}
	if closeErr != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("close output file: %w", closeErr)
	}
	return nil
}

func writeAppBundle(zw *zip.Writer, plist, binary []byte) error {
	for _, dir := range []string{
		"Taskr.app/",
		"Taskr.app/Contents/",
		"Taskr.app/Contents/MacOS/",
	} {
		if err := addZipEntry(zw, dir, os.ModeDir|0o755, zip.Store, nil); err != nil {
			return err
		}
	}
	if err := addZipEntry(zw, "Taskr.app/Contents/Info.plist", 0o644, zip.Deflate, plist); err != nil {
		return err
	}
	if err := addZipEntry(zw, "Taskr.app/Contents/MacOS/taskr", 0o755, zip.Deflate, binary); err != nil {
		return err
	}
	return nil
}

func addZipEntry(zw *zip.Writer, name string, mode os.FileMode, method uint16, data []byte) error {
	header := &zip.FileHeader{Name: name, Method: method}
	header.SetMode(mode)
	header.SetModTime(zipTimestamp)
	w, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create archive entry %s: %w", name, err)
	}
	if len(data) == 0 {
		return nil
	}
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("write archive entry %s: %w", name, err)
	}
	return nil
}
