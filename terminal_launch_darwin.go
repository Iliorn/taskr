//go:build darwin

package main

import (
	"os"
	"os/exec"

	"github.com/mattn/go-isatty"
)

// relaunchInTerminalIfNeeded opens the current executable in Terminal when
// macOS Launch Services started it without a TTY (for example by double-
// clicking Taskr.app in Finder). It returns true only when the handoff was
// accepted, so a failed handoff falls through to Bubble Tea's normal error.
func relaunchInTerminalIfNeeded() bool {
	if isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd()) {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return exec.Command("/usr/bin/open", "-a", "Terminal", executable).Run() == nil
}
