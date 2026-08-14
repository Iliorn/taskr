package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// paths.go decides where taskr keeps its files. There are four kinds and they
// belong in four places, because that is what the platform conventions say:
//
//	config  settings.json, sync.json          — yours to edit, worth backing up
//	data    tasks.db (+ WAL sidecars)         — the tasks themselves
//	state   undo.json, sync-state.json, logs  — recreatable, machine-local
//	cache   the $EDITOR scratch directory     — throwaway
//
// The resolution order is the same for every kind:
//
//  1. TASKR_HOME, if set — one directory for everything, for people who would
//     rather have a single thing to back up than a tidy split.
//  2. An existing ~/.taskr — every install before this change put everything
//     there, and moving a user's database out from under them to satisfy a
//     specification is not an upgrade. It keeps working, forever.
//  3. The platform convention: XDG on Linux/BSD (and anywhere the XDG_*
//     variables are set deliberately), %APPDATA%/%LOCALAPPDATA% on Windows,
//     ~/Library/Application Support on macOS.
//
// Rule 3 only ever applies to a fresh install, which is why rule 2 has no
// migration step and no prompt: nothing moves, nothing needs to be told.

const appDirName = "taskr"

// legacyDirName is the single directory every version before the XDG split
// used. Its presence is what pins an existing install to it.
const legacyDirName = ".taskr"

// pathKind is which of the four directories a file belongs in.
type pathKind int

const (
	pathConfig pathKind = iota
	pathData
	pathState
	pathCache
)

// taskrHomeOverride returns TASKR_HOME, expanded, or "" when unset. It collapses
// all four kinds into one directory.
func taskrHomeOverride() string {
	return strings.TrimSpace(os.Getenv("TASKR_HOME"))
}

// legacyHome returns ~/.taskr when it already exists as a directory.
func legacyHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, legacyDirName)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return dir
	}
	return ""
}

// appDir resolves one of the four directories. It does not create anything —
// callers that write go through ensureDir.
func appDir(kind pathKind) (string, error) {
	if override := taskrHomeOverride(); override != "" {
		return override, nil
	}
	if legacy := legacyHome(); legacy != "" {
		return legacy, nil
	}
	base, err := platformBase(kind)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDirName), nil
}

// platformBase is the parent directory for a kind, before the "taskr" element.
// An explicitly set XDG variable wins on every platform: someone who exports
// XDG_DATA_HOME on macOS or Windows means it.
func platformBase(kind pathKind) (string, error) {
	if dir := xdgFromEnv(kind); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		// Roaming for config (it follows the user between machines), Local for
		// everything else (a database and a log do not belong in a roaming
		// profile).
		if kind == pathConfig {
			if v := os.Getenv("APPDATA"); v != "" {
				return v, nil
			}
			return filepath.Join(home, "AppData", "Roaming"), nil
		}
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return v, nil
		}
		return filepath.Join(home, "AppData", "Local"), nil
	case "darwin":
		// macOS has no state directory of its own; Application Support is
		// where both config and data live, and Caches is separate.
		if kind == pathCache {
			return filepath.Join(home, "Library", "Caches"), nil
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		return filepath.Join(append([]string{home}, xdgDefault(kind)...)...), nil
	}
}

func xdgFromEnv(kind pathKind) string {
	var name string
	switch kind {
	case pathConfig:
		name = "XDG_CONFIG_HOME"
	case pathData:
		name = "XDG_DATA_HOME"
	case pathState:
		name = "XDG_STATE_HOME"
	case pathCache:
		name = "XDG_CACHE_HOME"
	}
	v := strings.TrimSpace(os.Getenv(name))
	// The spec says a relative value is invalid and must be ignored, which
	// also keeps a stray "." from scattering files across the working
	// directory.
	if v == "" || !filepath.IsAbs(v) {
		return ""
	}
	return v
}

// xdgDefault is the spec's fallback for each kind, relative to $HOME.
func xdgDefault(kind pathKind) []string {
	switch kind {
	case pathConfig:
		return []string{".config"}
	case pathData:
		return []string{".local", "share"}
	case pathState:
		return []string{".local", "state"}
	default:
		return []string{".cache"}
	}
}

// ensureDir resolves a kind and creates it, so writers can just ask for a path.
func ensureDir(kind pathKind) (string, error) {
	dir, err := appDir(kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// pathFor is the read-side helper: the full path of a file, whether or not its
// directory exists yet. An unresolvable home yields "", which every caller
// already treats as "no such file".
func pathFor(kind pathKind, name ...string) string {
	dir, err := appDir(kind)
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{dir}, name...)...)
}

// usingLegacyLayout reports whether taskr is reading and writing the old single
// ~/.taskr directory. The doctor says so, since it explains why the XDG paths
// are not in use.
func usingLegacyLayout() bool {
	return taskrHomeOverride() == "" && legacyHome() != ""
}
