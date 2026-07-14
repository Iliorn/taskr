package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestFastStyleMatchesLipgloss is the safety net for the per-row render bypass:
// fastStyle.render must be byte-identical to lipgloss.Style.Render for every
// style and input, including the tab / embedded-escape cases it falls back on.
// Run under TrueColor so the colour-wrapping path is actually exercised (a
// non-TTY test profile would emit no SGR and make the comparison trivial).
func TestFastStyleMatchesLipgloss(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	applyTheme(themes[0]) // rebuild styles + fast styles under TrueColor
	defer func() {
		lipgloss.SetColorProfile(before)
		applyTheme(themes[0])
	}()

	pairs := []struct {
		name string
		st   lipgloss.Style
		fast fastStyle
	}{
		{"normal", normalStyle, fastNormal},
		{"selected", selectedStyle, fastSelected},
		{"overdue", overdueStyle, fastOverdue},
		{"depOverdue", depOverdueStyle, fastDepOverdue},
		{"dim", dimStyle, fastDim},
		{"timer", timerStyle, fastTimer},
		{"checkDone", checkDoneStyle, fastCheckDone},
		{"selectedRow", selectedRowStyle, fastSelectedRow},
		{"selectedOverdue", selectedOverdueRowStyle, fastSelectedOverdue},
		{"selectedDepOverdue", selectedDepOverdueRowStyle, fastSelectedDepOverdue},
		{"selectedTimer", selectedTimerRowStyle, fastSelectedTimer},
	}
	samples := []string{
		"",
		"a",
		"  [ ] Buy milk          12.3",
		"▶ [✓] nested task ↻ ¶ (2/5)",
		"⏱ running timer",
		strings.Repeat("x", 200),
		"münchen café — 日本語タスク",
		"tab\there", // must fall back (lipgloss expands the tab)
		"emb" + dimStyle.Render("(…)") + "edded escape", // must fall back
	}
	for _, p := range pairs {
		for _, s := range samples {
			want := p.st.Render(s)
			got := p.fast.render(s)
			if got != want {
				t.Errorf("%s.render(%q):\n got=%q\nwant=%q", p.name, s, got, want)
			}
		}
	}
}
