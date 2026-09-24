package main

import (
	"github.com/charmbracelet/lipgloss"
)

// board_carry.go — how a card held up in modeBoardCarry looks. It is lit in
// the Board's own colour for as long as it is carried, green with a ✓ while it
// is over Done, which previews what putting it down there will do.
//
// A moved card used to also play a landing glow that faded back over a third
// of a second. It went: the card that lands is the selected one, so the fade
// ran from yellow into the selection green the eye was already on — and into
// Done from green to green — and nobody could see it happen. The selection
// following the card is what shows where it went.

// carryGlowDone reports whether the held card is over the Done column.
func (m model) carryGlowDone() bool {
	return m.mode == modeBoardCarry && m.board.carryCol == doneColumn()
}

func (m model) carryColor() lipgloss.Color {
	if m.carryGlowDone() {
		return currentTheme.green
	}
	return currentTheme.yellow
}

// carryRowStyle is a held card drawn as a plain row: dark text on the carry
// colour, so it reads as lifted off the column.
func (m model) carryRowStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(currentTheme.bg).Background(m.carryColor())
}
