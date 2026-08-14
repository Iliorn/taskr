package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// appVersion is the current build version. Override at build time with:
//
//	go build -ldflags "-X main.appVersion=v1.8.0" -o taskr .
//
// When no version is injected, init() in version.go replaces this default
// with whatever the Go toolchain recorded — the module version for a
// `go install module@version`, or the git revision for a local build.
var appVersion = "dev"

func main() {
	// Remove leftover binary from a previous Windows self-update, if any.
	if execPath, err := os.Executable(); err == nil {
		_ = os.Remove(execPath + ".old")
	}

	// CLI mode: when the first arg names a subcommand, run the non-TUI
	// dispatcher and exit. Bare `taskr` (no args, or only flags meant for the
	// TUI) still launches the Bubble Tea program below.
	if len(os.Args) > 1 && isCLICommand(os.Args[1]) {
		code := runCLI(os.Args[1:])
		checkpointStore()
		os.Exit(code)
	}

	// Opt-in latency trace (TASKR_TRACE=1). Started before the model so the
	// startup load is in the log too.
	stopTrace := startTrace()
	defer stopTrace()

	m := initialModel(newSQLiteRepo())
	// Live reload of out-of-process writes. Started here, not in initialModel:
	// it holds a file descriptor, and only a running program wants one.
	startModelWatcher(&m)
	// The renderer paints on a ticker, so the frame rate is also the worst-case
	// delay between a keystroke and seeing it. Bubble Tea's default is 60 FPS
	// (up to ~17ms of waiting); 120 is its maximum and halves that. The frames
	// themselves are line-diffed and small, so the extra ticks cost nothing
	// when nothing changed.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithFPS(120))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Final best-effort sync on exit so the session's last edits propagate
	// immediately (no-op unless sync is configured).
	maybeAutoSyncCLI()
	checkpointStore()
}
