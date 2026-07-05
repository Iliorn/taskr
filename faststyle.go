package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// faststyle.go bypasses lipgloss's per-call render machinery for the simple
// foreground(+bold) styles painted on every task-list row. lipgloss.Style.Render
// runs each string through cellbuf wrapping + display-width measurement even when
// no width/border/padding is set, and re-derives the SGR colour sequence on every
// call — together the largest remaining CPU and allocation cost of a frame once
// the list is windowed.
//
// For these plain styles Render(s) is exactly prefix+s+suffix, where prefix is
// the style's opening SGR and suffix the reset. The only cases where that does
// not hold are a literal tab (lipgloss expands it to spaces) and an embedded
// escape (lipgloss reflows the colour around the reset); render() defers to the
// real Render whenever the content contains either, so its output is
// byte-identical to lipgloss in every case. TestFastStyleMatchesLipgloss locks
// that equivalence.
type fastStyle struct {
	style  lipgloss.Style
	prefix string
	suffix string
	usable bool
}

// newFastStyle captures a style's opening/closing SGR by rendering a sentinel and
// splitting around it. Recomputed whenever the theme (and thus the colours)
// changes. If the split fails for any reason usable stays false, so render()
// always defers to lipgloss.
func newFastStyle(s lipgloss.Style) fastStyle {
	const sentinel = "\x01\x02" // control bytes lipgloss passes through untouched
	r := s.Render(sentinel)
	i := strings.Index(r, sentinel)
	if i < 0 {
		return fastStyle{style: s}
	}
	return fastStyle{style: s, prefix: r[:i], suffix: r[i+len(sentinel):], usable: true}
}

// render returns the same bytes as f.style.Render(s) but skips lipgloss for the
// common case — content with no tab and no embedded escape — by wrapping the
// string in the cached SGR prefix/suffix.
func (f fastStyle) render(s string) string {
	if !f.usable || strings.IndexByte(s, '\t') >= 0 || strings.IndexByte(s, 0x1b) >= 0 {
		return f.style.Render(s)
	}
	return f.prefix + s + f.suffix
}

// Fast variants of the per-row task-list styles, rebuilt by rebuildFastStyles
// from the package-level styles at the end of every applyTheme.
var (
	fastNormal     fastStyle
	fastSelected   fastStyle
	fastOverdue    fastStyle
	fastDepOverdue fastStyle
	fastDim        fastStyle
	fastTimer      fastStyle
	fastCheckDone  fastStyle

	fastSelectedRow        fastStyle
	fastSelectedOverdue    fastStyle
	fastSelectedDepOverdue fastStyle
	fastSelectedTimer      fastStyle
)

func rebuildFastStyles() {
	fastNormal = newFastStyle(normalStyle)
	fastSelected = newFastStyle(selectedStyle)
	fastOverdue = newFastStyle(overdueStyle)
	fastDepOverdue = newFastStyle(depOverdueStyle)
	fastDim = newFastStyle(dimStyle)
	fastTimer = newFastStyle(timerStyle)
	fastCheckDone = newFastStyle(checkDoneStyle)

	fastSelectedRow = newFastStyle(selectedRowStyle)
	fastSelectedOverdue = newFastStyle(selectedOverdueRowStyle)
	fastSelectedDepOverdue = newFastStyle(selectedDepOverdueRowStyle)
	fastSelectedTimer = newFastStyle(selectedTimerRowStyle)
}
