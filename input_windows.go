//go:build windows

package main

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// resizePollInterval is how often the Windows build checks whether the console
// was resized. Resize is not a keystroke — a fraction of a second late is
// invisible — and the check is one syscall against a handle we already hold.
const resizePollInterval = 250 * time.Millisecond

func inputProgramOptions() []tea.ProgramOption {
	if consoleInputForced() {
		return nil
	}
	// Opening CONIN$ as its own file is what moves Bubble Tea off the polling
	// console-event reader and onto a blocking read of the VT input stream.
	return []tea.ProgramOption{tea.WithInputTTY()}
}

// startResizePoller replaces the resize events that the console-event reader
// would have delivered. Windows has no SIGWINCH and the VT input stream says
// nothing about the window, so the size has to be asked for.
func startResizePoller(p *tea.Program) (stop func()) {
	if consoleInputForced() {
		return func() {} // the console reader still reports resizes itself
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(resizePollInterval)
		defer t.Stop()
		lastW, lastH := 0, 0
		for {
			select {
			case <-done:
				return
			case <-t.C:
				w, h, err := term.GetSize(os.Stdout.Fd())
				if err != nil || w <= 0 || h <= 0 {
					continue
				}
				if w != lastW || h != lastH {
					lastW, lastH = w, h
					p.Send(tea.WindowSizeMsg{Width: w, Height: h})
				}
			}
		}
	}()
	var stopped bool
	return func() {
		if !stopped {
			stopped = true
			close(done)
		}
	}
}

func inputPathName() string {
	if consoleInputForced() {
		return "console events (TASKR_WIN_CONSOLE_INPUT, polls every 16ms)"
	}
	return "CONIN$ escape sequences (blocking read)"
}
