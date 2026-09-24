package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Atomic file replacement.
//
// Everything taskr keeps outside SQLite (settings.json, sync.json, sync state,
// the undo stack, task notes) goes through here. A plain os.WriteFile
// truncates first, so a crash mid-write leaves a half-written file. Instead:
// write a temp file beside the target, flush it, rename it over the target.
// Rename is atomic, so a reader sees the whole old file or the whole new one.

// writeFileAtomic writes data to path via a temporary file in the same
// directory, replacing path only once the new contents are safely on disk.
// The temporary file is removed on any failure, so a failed write leaves the
// previous contents intact rather than a stray fragment next to them.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	// Same directory, so the rename cannot cross a filesystem boundary — a
	// temp file in os.TempDir() would fail with EXDEV on any setup where
	// /tmp is its own mount, which is most of them.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("create temp file next to %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// From here on every failure has to clean up after itself; the deferred
	// Remove is a no-op once the rename has succeeded and the name is gone.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	// Flush the file's own contents before the rename. Without this the
	// rename can reach the disk first, leaving the target name pointing at
	// a file whose data has not landed — the exact corruption this is here
	// to prevent, just moved one step later.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	// CreateTemp always makes the file 0600; restore the caller's intent.
	// Done before the rename so the file is never visible under its real
	// name with the wrong mode.
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	syncDir(dir)
	return nil
}

// syncDir flushes the directory entry so the rename itself survives a power
// loss, not just the file contents. Best-effort by design: Windows cannot
// open a directory as a file at all, and some filesystems reject the fsync,
// but on those the rename is either already durable or nothing we do here
// changes that. A failure must never fail the write — the data is on disk
// either way.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
