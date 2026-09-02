package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Terminals disagree about how wide a symbol is when the font falls back to an
// emoji face for it: taskr counts one cell (ansi.StringWidth follows wcwidth),
// the terminal draws two, and everything to the right of the glyph is off by
// one — a tab-bar badge that swallows its own count, a due cell that pushes the
// row past the border. The disagreement is not ours to settle per terminal, so
// the UI stays out of the ranges where it happens.
//
// The list is the glyphs that actually turned up in this app or are one
// autocomplete away from it; it is not a Unicode-wide ban, and a symbol that
// renders at one cell everywhere (arrows, box drawing, ✓, ▶) is fine.
func TestUIAvoidsGlyphsTerminalsDrawDoubleWide(t *testing.T) {
	banned := map[rune]string{
		0x26A0: "warning sign",
		0x26A1: "high voltage",
		0x2757: "heavy exclamation",
		0x2705: "white heavy check",
		0x274C: "cross mark",
		0x2B50: "star",
		0x23F0: "alarm clock",
		0x2728: "sparkles",
	}
	// Only the app's own strings: a test may well feed the app an emoji on
	// purpose (fuzz_test.go does), and user data is allowed to contain
	// anything — it is what taskr *prints of its own accord* that has to be
	// one cell wide.
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for line, text := range strings.Split(string(data), "\n") {
			for _, r := range text {
				if name, bad := banned[r]; bad {
					t.Errorf("%s:%d uses %q (%s) — terminals draw it two cells wide; use an ASCII marker",
						path, line+1, r, name)
				}
				if r >= 0x1F000 {
					t.Errorf("%s:%d uses the emoji %q — same problem, and no terminal font is required to have it",
						path, line+1, r)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The console helper has to be safe to call where there is no console at all —
// `taskr export > file`, a pipe, the test binary — and safe to call its undo
// twice, since main runs it both on the deferred path and before os.Exit.
func TestUTF8ConsoleIsSafeWithoutAConsole(t *testing.T) {
	restore := useUTF8Console()
	if restore == nil {
		t.Fatal("useUTF8Console returned no restore func")
	}
	restore()
	restore()
}
