package main

import (
	"errors"
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

	// Fail closed when the store was written by a newer taskr. Every other
	// load error is reported inside the TUI, which is right for "the disk is
	// full" but wrong here: the app would come up showing an empty list over a
	// full database, and the first save would write this build's older columns
	// back over it (migrator.go).
	if err := openStore(); errors.Is(err, errSchemaTooNew) {
		fmt.Fprintf(os.Stderr, "taskr: %v\n", err)
		os.Exit(1)
	}

	// Opt-in latency trace (TASKR_TRACE=1). Started before the model so the
	// startup load is in the log too.
	stopTrace := startTrace()
	defer stopTrace()

	m := initialModel(newSQLiteRepo())
	// Live reload of out-of-process writes. Started here, not in initialModel:
	// it holds a file descriptor, and only a running program wants one.
	startModelWatcher(&m)
	// Render one frame before handing over. The first View of a session is the
	// expensive one — it builds every derived cache and fills the string-builder
	// pool — and paying for it here means the user's first keystroke doesn't.
	_ = m.View()
	// The renderer paints on a ticker, so the frame rate is also the worst-case
	// delay between a keystroke and seeing it. Bubble Tea's default is 60 FPS
	// (up to ~17ms of waiting); 120 is its maximum and halves that. The frames
	// themselves are line-diffed and small, so the extra ticks cost nothing
	// when nothing changed.
	opts := append([]tea.ProgramOption{tea.WithAltScreen(), tea.WithFPS(120)}, inputProgramOptions()...)
	p := tea.NewProgram(m, opts...)
	// Windows reads the console through its own file, which costs the resize
	// events the console-event reader would have delivered; see input.go.
	stopResize := startResizePoller(p)
	defer stopResize()
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// Last, so it survives Bubble Tea's stack dump on screen.
		noteCrashToUser()
		os.Exit(1)
	}
	// Final best-effort sync on exit so the session's last edits propagate
	// immediately (no-op unless sync is configured).
	maybeAutoSyncCLI()
	checkpointStore()
}
