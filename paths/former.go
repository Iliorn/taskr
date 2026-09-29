package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// FormerName is what tjek was called before. Its directories hold the tasks,
// settings and sync state of every install made under that name.
const FormerName = "taskr"

// Move is one directory AdoptFormerDirs renamed.
type Move struct{ From, To string }

// AdoptFormerDirs gives the former name's directories tjek's names: ~/.taskr
// becomes ~/.tjek, and each platform directory's taskr folder becomes a tjek
// one (~/.config/taskr → ~/.config/tjek, %APPDATA%\taskr → %APPDATA%\tjek, …).
// Every later lookup then finds the data where it has always been, under the
// new name, so there is no second set of paths to read.
//
// A directory moves only when the new name is free, so a second run, or an
// install that already has tjek data, moves nothing and loses nothing. Each
// move is a rename within one parent directory, which is atomic. TJEK_HOME is
// not touched: it names the directory itself, whatever it is called.
func AdoptFormerDirs() ([]Move, error) {
	var candidates []Move
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, Move{
			filepath.Join(home, "."+FormerName), filepath.Join(home, LegacyDirName),
		})
	}
	seen := make(map[string]bool)
	for _, kind := range []Kind{Config, Data, State, Cache} {
		base, err := platformBase(kind)
		if err != nil || seen[base] {
			continue // macOS keeps config, data and state under one parent
		}
		seen[base] = true
		candidates = append(candidates, Move{
			filepath.Join(base, FormerName), filepath.Join(base, AppDirName),
		})
	}

	var moved []Move
	var errs []error
	for _, c := range candidates {
		fi, err := os.Stat(c.From)
		if err != nil || !fi.IsDir() {
			continue
		}
		if _, err := os.Lstat(c.To); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Rename(c.From, c.To); err != nil {
			errs = append(errs, err)
			continue
		}
		moved = append(moved, c)
	}
	return moved, errors.Join(errs...)
}

// FormerExecutable reports whether path is a binary installed under the
// former command name: taskr, or taskr.exe on Windows.
func FormerExecutable(path string) bool {
	name := filepath.Base(path)
	if runtime.GOOS == "windows" {
		return name == FormerName+".exe"
	}
	return name == FormerName
}
