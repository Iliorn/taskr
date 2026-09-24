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

// A card carried over Done previews the landing with a ✓, and the lit frame
// still keeps the no-wrap contract.
func TestBoardCarryOverDoneShowsACheck(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(before)

	m, id := boardWithOneCard(t)
	keys := []string{"enter"}
	for i := 0; i < doneColumn(); i++ {
		keys = append(keys, "right")
	}
	m = script(t, m, keys...)
	if !m.carrying(id) || !m.carryGlowDone() {
		t.Fatalf("the card should be held over Done: mode %v col %d", m.mode, m.board.carryCol)
	}
	out := m.View()
	if !strings.Contains(ansi.Strip(out), "✓ Draft the budget") {
		t.Fatalf("a card held over Done should show a ✓:\n%s", ansi.Strip(out))
	}
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > m.termWidth {
			t.Fatalf("carry frame line is %d wide, window is %d", w, m.termWidth)
		}
	}

	m = script(t, m, "enter")
	if m.carryGlowDone() || m.get(id).Status != todo.Done {
		t.Fatalf("putting it down should close it and end the carry: mode %v", m.mode)
	}
	if strings.Contains(ansi.Strip(m.View()), "✓ Draft the budget") {
		t.Fatal("the ✓ is a carry preview and should go once the card is down")
	}
}

// The selected card's title takes its border's colour, so the whole card
// lights up and not only its frame; the other cards' titles stay plain.
func TestBoardSelectedCardTextLightsUp(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer func() {
		lipgloss.SetColorProfile(before)
		applyTheme(themes[0])
	}()
	applyTheme(themes[0])

	card := todo.New("Draft the budget")
	m := newTagModel(card)
	lit := lipgloss.NewStyle().Foreground(currentTheme.green).Bold(true).Render("Draft the budget")
	if got := strings.Join(m.renderBoardBox(m.get(card.ID), false, true, 30, 2), "\n"); !strings.Contains(got, lit) {
		t.Errorf("selected card title is not lit in the selection colour:\n%q", got)
	}
	if got := strings.Join(m.renderBoardBox(m.get(card.ID), false, false, 30, 2), "\n"); strings.Contains(got, lit) {
		t.Errorf("an unselected card's title should not be lit:\n%q", got)
	}
}
