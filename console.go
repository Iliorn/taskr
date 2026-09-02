package main

import (
	"os"

	"github.com/charmbracelet/x/term"
)

// console.go is the terminal's *encoding*, as opposed to input.go's keyboard.
// Two different terminals on Windows get UTF-8 wrong in two different ways,
// and neither is anything to do with the text taskr stores:
//
//   - A real console (conhost, Windows Terminal, cmd, PowerShell) decodes what
//     a program writes with its code page, still CP850 on a Danish install.
//     That is console_windows.go's job.
//   - mintty — Git Bash, MSYS2 — is not a console at all: it is its own
//     terminal on the far end of a pipe, so no code page reaches it. Its
//     charset comes from the locale it was started in, and Git Bash started
//     without LANG falls back to an 8-bit one. Same mojibake, other road.
//
// mintty takes OSC 701 to change its locale at runtime, which is the one
// handle a program inside the pipe has on it. That is what retuneMintty
// sends, and it is the whole reason this file exists: the alternative is
// asking every user to edit ~/.bashrc before the app is readable.

// prepareConsole makes the terminal UTF-8 for the length of the run and
// returns the undo. Called first thing in main, before anything can print —
// including the CLI path, which prints and exits.
func prepareConsole() (restore func()) {
	restoreCP := useUTF8Console()
	if seq := minttyUTF8Sequence(os.Getenv("MSYSTEM"), os.Getenv("TERM_PROGRAM"),
		term.IsTerminal(os.Stdout.Fd())); seq != "" {
		_, _ = os.Stdout.WriteString(seq)
		minttyRetuned = true
	}
	return restoreCP
}

// minttyRetuned records that the OSC went out, for `taskr doctor`. Whether
// mintty acted on it is not something we can read back, and saying which of
// the two Windows paths taskr took is most of the answer when the next
// screenshot of garbled text arrives.
var minttyRetuned bool

// terminalCharsetNote is the doctor line about encoding, empty when there is
// nothing to say (a Unix terminal, where UTF-8 needs no arranging).
func terminalCharsetNote() string {
	switch {
	case minttyRetuned && consoleEncodingNote() != "":
		return "mintty: asked for UTF-8 (OSC 701) · console " + consoleEncodingNote()
	case minttyRetuned:
		return "mintty: asked for UTF-8 (OSC 701)"
	default:
		return consoleEncodingNote()
	}
}

// minttyUTF8Sequence is the OSC 701 to send, or "" when this is not mintty or
// stdout is not a terminal at all. Kept pure and separate from the write so
// the gate is testable off Windows, where the environment it reads never says
// yes: `taskr export > file` must not get an escape sequence in the file, and
// a terminal that has never heard of OSC 701 should not be sent one on the
// off chance — an unknown OSC is swallowed, but only by terminals that parse
// it correctly.
func minttyUTF8Sequence(msystem, termProgram string, stdoutIsTTY bool) string {
	if !stdoutIsTTY {
		return ""
	}
	if msystem == "" && termProgram != "mintty" {
		return ""
	}
	// Not restored on exit: the charset belongs to the shell session, and
	// UTF-8 is what the rest of it wants too — putting an 8-bit charset back
	// would break the next program for the sake of symmetry.
	return "\x1b]701;C.UTF-8\x07"
}
