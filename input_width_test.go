package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A footer field keeps its height while the caret blinks, at every width and
// whatever the length of its value: a long value scrolls sideways inside the
// box rather than wrapping.
func TestFooterFieldHeightIsSteadyThroughTheBlink(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(before)

	value := "Backlog, In progress, Review, Waiting on others, Done"
	for width := 20; width <= 80; width++ {
		m := newTestModel()
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = next.(model)
		m.mode = modeEditStages
		m.textInput.SetValue(value)
		m.textInput.Focus()

		render := func() string {
			return inputStyle.Width(m.termWidth - 6).Render(m.textInput.View())
		}
		shown := render()
		m.textInput.Cursor.Blink = !m.textInput.Cursor.Blink
		hidden := render()

		if a, b := strings.Count(shown, "\n"), strings.Count(hidden, "\n"); a != 2 || b != 2 {
			t.Fatalf("width %d: the field is %d lines with the caret shown and %d hidden, want 3 both times", width, a+1, b+1)
		}
		for _, line := range strings.Split(shown, "\n") {
			if w := ansi.StringWidth(line); w > m.termWidth-2 {
				t.Fatalf("width %d: field line is %d cells: %q", width, w, line)
			}
		}
	}
}

// A field edited inside a list row draws only its text: the footer's width
// would pad it past the panel, and the row would end in a clipped ellipsis.
func TestInlineRenameRowIsNotPaddedToTheFooter(t *testing.T) {
	m := smallTermModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = next.(model)
	m.tab = tabTags
	m.editingTagName = "home"
	m.mode = modeEditTag
	m.textInput.SetValue("home")
	m.textInput.Focus()
	m.ensureCache()
	for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(line, "#home > home") && strings.Contains(line, ellipsis) {
			t.Fatalf("the rename row is clipped: %q", line)
		}
	}
}
