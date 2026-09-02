//go:build !windows

package main

// Off Windows a terminal reads and writes UTF-8 as a matter of course — there
// is no per-console code page to set, and the locale does not reach the bytes
// we write. See console_windows.go for what this stands in for.

func useUTF8Console() (restore func()) { return func() {} }

func consoleEncodingNote() string { return "" }
