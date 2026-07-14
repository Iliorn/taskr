package main

import (
	"strings"
	"testing"
)

// On a terminal too short to show the whole overlay, the last section ("Date
// input") must be hidden at the top of the scroll and revealed once scrolled
// to the bottom — the bug this feature fixes was that it silently vanished.
func TestHelpOverlayScrollRevealsLowerSections(t *testing.T) {
	m := modelWithTasks(t)
	m.mode = modeHelp
	m.termHeight = 14 // deliberately short: body can't fit in the viewport

	body := m.helpBodyLines()
	if len(body) <= m.helpViewportH() {
		t.Fatalf("test needs a body taller than the viewport: %d body vs %d viewport",
			len(body), m.helpViewportH())
	}

	// The final date-input row is the last content line — off-screen at the
	// top, reachable at the bottom.
	const lastRow = "+3d / +2w / +1m"

	atTop := m.renderHelpFullscreen()
	if strings.Contains(atTop, lastRow) {
		t.Fatal("last row should be below the fold at scroll=0")
	}
	if !strings.Contains(atTop, "↓ scroll down") {
		t.Fatal("footer should advertise that more content is below")
	}

	// Scroll to the bottom (clamped) and re-render.
	m.helpScroll = clampHelpScroll(len(body), len(body), m.helpViewportH())
	atBottom := m.renderHelpFullscreen()
	if !strings.Contains(atBottom, lastRow) {
		t.Fatal("last row should be visible after scrolling to the bottom")
	}
	if !strings.Contains(atBottom, "↑ scroll up") {
		t.Fatal("footer should advertise that content is above once at the bottom")
	}
}

// clampHelpScroll never lets the offset leave [0, total-viewport].
func TestClampHelpScrollBounds(t *testing.T) {
	if got := clampHelpScroll(-5, 100, 20); got != 0 {
		t.Fatalf("negative offset should clamp to 0, got %d", got)
	}
	if got := clampHelpScroll(999, 100, 20); got != 80 {
		t.Fatalf("offset past the end should clamp to total-viewport (80), got %d", got)
	}
	if got := clampHelpScroll(5, 10, 20); got != 0 {
		t.Fatalf("body shorter than viewport should pin to 0, got %d", got)
	}
}

// The height floor keeps a usable viewport even on absurdly short terminals.
func TestHelpViewportFloor(t *testing.T) {
	m := modelWithTasks(t)
	m.termHeight = 4
	if got := m.helpViewportH(); got < 3 {
		t.Fatalf("viewport should floor at 3, got %d", got)
	}
}

// The help overlay documents the task-row annotation glyphs so users don't have
// to read source to decode them. Backlog item d33d9e6e.
func TestHelpOverlayListsRowSymbols(t *testing.T) {
	m := modelWithTasks(t)
	body := strings.Join(m.helpBodyLines(), "\n")

	if !strings.Contains(body, "Row symbols") {
		t.Fatal("help body should contain a 'Row symbols' section")
	}
	for _, want := range []string{"⧗", "↥", "↧", "↻", "recurring task", "timer running"} {
		if !strings.Contains(body, want) {
			t.Errorf("Row symbols section missing %q", want)
		}
	}
}
