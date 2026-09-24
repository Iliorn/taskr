package main

import (
	"strings"
	"testing"

	"github.com/Iliorn/taskr/todo"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// boardWithOneCard is a Board with a single Backlog card, focused.
func boardWithOneCard(t *testing.T) (model, string) {
	t.Helper()
	card := todo.New("Draft the budget")
	m := newTagModel(card)
	m.tab = tabBoard
	m.termWidth, m.termHeight = 120, 30
	m.refreshCaches()
	return m, card.ID
}

// A moved card lands lit, fades one step per tick, and is back to rest after
// boardFlashFrames ticks with no tick left pending.
func TestBoardMoveLightsTheCardAndFades(t *testing.T) {
	m, id := boardWithOneCard(t)
	cmd := m.boardMoveCard(1)
	if cmd == nil || m.board.flashID != id || m.board.flashDone {
		t.Fatalf("a stage move should start the glow on the moved card: id=%q done=%v cmd=%v",
			m.board.flashID, m.board.flashDone, cmd != nil)
	}
	if f := m.boardFlashLevel(id); f != 1 {
		t.Fatalf("landing frame glow = %v, want 1", f)
	}
	prev := 1.0
	for i := 1; i < boardFlashFrames; i++ {
		if m.advanceBoardFlash(m.board.flashSeq) == nil {
			t.Fatalf("glow stopped after %d of %d frames", i, boardFlashFrames)
		}
		f := m.boardFlashLevel(id)
		if f <= 0 || f >= prev {
			t.Fatalf("frame %d glow = %v, want it strictly fading from %v", i, f, prev)
		}
		prev = f
	}
	if m.advanceBoardFlash(m.board.flashSeq) != nil || m.boardFlashLevel(id) != 0 || m.board.flashID != "" {
		t.Fatalf("glow should be over after %d frames", boardFlashFrames)
	}
}

// A second move restarts the glow; the first animation's ticks must not keep
// stepping it, or overlapping moves would fade at double speed.
func TestBoardFlashIgnoresTicksFromAReplacedMove(t *testing.T) {
	m, _ := boardWithOneCard(t)
	m.boardMoveCard(1)
	stale := m.board.flashSeq
	m.boardMoveCard(1)
	if m.advanceBoardFlash(stale) != nil || m.board.flashFrames != boardFlashFrames {
		t.Fatalf("a replaced move's tick stepped the new glow: %d frames left", m.board.flashFrames)
	}
}

// Moving into Done lands the card with a ✓, and the glowing frame still keeps
// the no-wrap contract (the lit card is padded to its column).
func TestBoardMoveIntoDoneLandsWithACheck(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(before)

	m, id := boardWithOneCard(t)
	for i := 0; i < doneColumn(); i++ {
		m.boardMoveCard(1)
	}
	if !m.board.flashDone || m.board.flashID != id {
		t.Fatalf("closing a card from the Board should glow it as done: %+v", m.board)
	}
	out := m.View()
	if !strings.Contains(ansi.Strip(out), "✓ Draft the budget") {
		t.Fatalf("done card should land with a ✓:\n%s", ansi.Strip(out))
	}
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > m.termWidth {
			t.Fatalf("glowing frame line is %d wide, window is %d", w, m.termWidth)
		}
	}
}

func TestBlendHex(t *testing.T) {
	cases := []struct {
		a, b lipgloss.Color
		f    float64
		want lipgloss.Color
	}{
		{"#000000", "#ffffff", 0, "#000000"},
		{"#000000", "#ffffff", 1, "#ffffff"},
		{"#000000", "#ff8040", 0.5, "#804020"},
		{"12", "#ffffff", 0.2, "12"}, // not hex: the nearer end
		{"12", "#ffffff", 0.8, "#ffffff"},
	}
	for _, c := range cases {
		if got := blendHex(c.a, c.b, c.f); got != c.want {
			t.Errorf("blendHex(%s, %s, %v) = %s, want %s", c.a, c.b, c.f, got, c.want)
		}
	}
}
