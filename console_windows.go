//go:build windows

package main

import (
	"strconv"
	"syscall"
)

// console_windows.go puts the Windows console into UTF-8 for the length of the
// run. Everything taskr writes is UTF-8 — the box-drawing borders, the chips,
// the user's own task titles — but a console decodes the bytes it is handed
// with its *code page*, and on a Danish Windows install that is still CP850 by
// default. The result is the classic mojibake: "på" arrives as bytes C3 A5 and
// is drawn as "Ã¥", one character per byte. Nothing is wrong with the stored
// text, so the fix belongs at the boundary, once, rather than in any renderer.
//
// The input code page is set with it. The Windows build reads CONIN$ as an
// escape-sequence stream (see input.go), so a typed æ arrives as bytes encoded
// by the input code page — under CP850 that is a single byte the UTF-8 parser
// cannot use, and the key is dropped or mangled on its way into a title.
//
// Both are restored on exit: the code page belongs to the console, not to us,
// and it outlives the process that changed it.

const cpUTF8 = 65001

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleCP       = kernel32.NewProc("GetConsoleCP")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	procGetConsoleOutputCP = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

// consoleNote is what `taskr doctor` reports, filled in by useUTF8Console.
// The code page is invisible until it is wrong, and then it explains every
// garbled character on screen at once — worth a line in the diagnostics.
var consoleNote string

func getConsoleCP(p *syscall.LazyProc) uint32 {
	r, _, _ := p.Call()
	return uint32(r)
}

func setConsoleCP(p *syscall.LazyProc, cp uint32) bool {
	r, _, _ := p.Call(uintptr(cp))
	return r != 0
}

// useUTF8Console switches the console to UTF-8 and returns the undo. A failure
// is not fatal: output redirected to a file or a pipe has no console to
// configure, which is the normal case for `taskr export > file` and for CI.
func useUTF8Console() (restore func()) {
	oldOut, oldIn := getConsoleCP(procGetConsoleOutputCP), getConsoleCP(procGetConsoleCP)
	outOK := oldOut != cpUTF8 && setConsoleCP(procSetConsoleOutputCP, cpUTF8)
	inOK := oldIn != cpUTF8 && setConsoleCP(procSetConsoleCP, cpUTF8)

	switch {
	case oldOut == cpUTF8:
		consoleNote = "UTF-8 (65001), already set"
	case outOK:
		consoleNote = "UTF-8 (65001), was " + strconv.Itoa(int(oldOut))
	case oldOut == 0:
		consoleNote = "" // not a console — redirected output, nothing to say
	default:
		consoleNote = "code page " + strconv.Itoa(int(oldOut)) + " — could not switch to UTF-8; non-ASCII characters will be garbled"
	}

	var done bool
	return func() {
		if done {
			return
		}
		done = true
		if outOK {
			setConsoleCP(procSetConsoleOutputCP, oldOut)
		}
		if inOK {
			setConsoleCP(procSetConsoleCP, oldIn)
		}
	}
}

// consoleEncodingNote is the doctor line, empty where there is nothing to say.
func consoleEncodingNote() string { return consoleNote }
