package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// board_flash.go — the landing glow on a card moved between Board columns.
// A move used to be a card vanishing from one column and a plain row appearing
// in the next, which reads as a redraw rather than as something you did. The
// card now lands lit in the Board's own colour (green, with a ✓, when it lands
// in Done) and fades back to its resting look over a few frames.
//
// It is presentation only: the move has already happened and been saved when
// the first frame draws, so a keypress mid-glow acts on the moved card as
// usual, and a second move simply restarts the glow on the new card.

const (
	boardFlashFrames   = 8
	boardFlashInterval = 40 * time.Millisecond
)

func boardFlashTick(seq int) tea.Cmd {
	return tea.Tick(boardFlashInterval, func(time.Time) tea.Msg { return boardFlashMsg{seq: seq} })
}

// startBoardFlash lights the card with the given ID and returns the tick that
// fades it.
func (m *model) startBoardFlash(id string, done bool) tea.Cmd {
	m.board.flashSeq++
	m.board.flashID, m.board.flashDone = id, done
	m.board.flashFrames = boardFlashFrames
	return boardFlashTick(m.board.flashSeq)
}

// advanceBoardFlash steps the glow one frame. A tick from an animation a later
// move replaced is dropped, or two overlapping moves would fade at double speed.
func (m *model) advanceBoardFlash(seq int) tea.Cmd {
	if seq != m.board.flashSeq || m.board.flashFrames == 0 {
		return nil
	}
	m.board.flashFrames--
	if m.board.flashFrames == 0 {
		m.board.flashID = ""
		return nil
	}
	return boardFlashTick(seq)
}

// boardFlashLevel is how lit a card is, from 1 on the frame it lands down to 0
// once the glow is over; 0 for every card but the one that moved.
func (m model) boardFlashLevel(id string) float64 {
	if id == "" || id != m.board.flashID || m.board.flashFrames <= 0 {
		return 0
	}
	f := float64(m.board.flashFrames) / boardFlashFrames
	return f * f // ease out: a bright landing, then a quick settle
}

func (m model) boardFlashColor() lipgloss.Color {
	if m.board.flashDone {
		return currentTheme.green
	}
	return currentTheme.yellow
}

// boardFlashStyle is the card's style at glow level f: the background fades
// from the glow colour to the selection background, and the text flips from
// dark-on-bright to its resting colour once the background has dimmed enough
// for that to read.
func (m model) boardFlashStyle(f float64) lipgloss.Style {
	t := currentTheme
	fg := t.green
	if m.board.flashDone {
		fg = t.dim
	}
	if f > 0.35 {
		fg = t.bg
	}
	return lipgloss.NewStyle().Bold(true).Foreground(fg).Background(blendHex(t.sel, m.boardFlashColor(), f))
}

// blendHex mixes two #rrggbb colours, f=0 giving a and f=1 giving b. A colour
// that is not six-digit hex (a theme using ANSI numbers) is returned as the
// nearer end rather than guessed at.
func blendHex(a, b lipgloss.Color, f float64) lipgloss.Color {
	var ar, ag, ab, br, bg, bb int
	if _, err := fmt.Sscanf(string(a), "#%02x%02x%02x", &ar, &ag, &ab); err != nil {
		return nearer(a, b, f)
	}
	if _, err := fmt.Sscanf(string(b), "#%02x%02x%02x", &br, &bg, &bb); err != nil {
		return nearer(a, b, f)
	}
	mix := func(x, y int) int { return x + int(float64(y-x)*f+0.5) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb)))
}

func nearer(a, b lipgloss.Color, f float64) lipgloss.Color {
	if f >= 0.5 {
		return b
	}
	return a
}
