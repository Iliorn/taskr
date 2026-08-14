package main

import (
	"os"
	"strings"
)

// input.go picks how the TUI reads the keyboard. Everywhere but Windows the
// answer is "the default", and these are one-line stubs (see input_windows.go).
//
// Windows is the exception, and it is worth the file. Bubble Tea has two input
// paths there:
//
//   - the console-event reader, used when the input is stdin itself. It calls
//     PeekNConsoleInputs in a loop with time.Sleep(16ms) between attempts, so a
//     keystroke arriving while the loop sleeps waits for the rest of that sleep
//     before the program hears about it. During a burst the loop spins on
//     pending events and the wait disappears — which is exactly the reported
//     symptom: the first key after a pause is late, the rest are not.
//   - a plain blocking read, used when the input is any other file. Windows
//     puts the console in ENABLE_VIRTUAL_TERMINAL_INPUT mode (Bubble Tea does
//     this itself in initInput), so the console hands us escape sequences the
//     same parser reads everywhere else, and the read returns the moment a key
//     arrives.
//
// tea.WithInputTTY() opens CONIN$ as its own file, which selects the second
// path. The cost is that console resize events only arrive on the first path,
// so on Windows we poll the size instead (startResizePoller).

// consoleInputForced reports whether TASKR_WIN_CONSOLE_INPUT asks for Bubble
// Tea's console-event reader — the polling one. It exists as a way back if the
// escape-sequence path misbehaves on a particular console; on every other
// platform it does nothing.
func consoleInputForced() bool {
	v := strings.TrimSpace(os.Getenv("TASKR_WIN_CONSOLE_INPUT"))
	return v != "" && v != "0" && v != "false"
}
