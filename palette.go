package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// palette.go — the command palette (ctrl+k): every action in the app findable
// by name instead of by remembered letter, which is the difference between a
// keyboard-driven app you already know and one you are still learning.
//
// Entries are generated from the keymap registry, and running one *presses its
// key* rather than calling the action directly. That is the whole design: the
// palette cannot drift from what the keys do, cannot grow a second code path
// for an action, and inherits every guard the key handler already has. An entry
// whose key belongs to another tab switches there first.

// paletteCmd is one entry: what it's called, the key that performs it, and the
// tab that key means it on.
type paletteCmd struct {
	label   string // human name, from the binding's description
	key     string // the key dispatch expects
	section string // where it came from, shown as context ("Tasks", "Board", …)
	tab     tab    // the tab the key belongs to
	anyTab  bool   // true when the key works from anywhere (no switch needed)
}

// paletteTabs maps the list-level contexts onto their tab. The pane- and
// level-scoped contexts (the task detail, the calendar timeline, the two
// drill-in lists) are deliberately absent: their keys only mean anything once
// you have opened or selected something, so a palette entry for them would
// either do nothing or act on a row the user never chose.
var paletteTabs = map[keyCtx]tab{
	ctxTasksList: tabTasks,
	ctxCalendar:  tabCalendar,
	ctxProjects:  tabProjects,
	ctxTags:      tabTags,
	ctxBoard:     tabBoard,
	ctxStats:     tabStats,
	ctxSettings:  tabSettings,
}

// paletteTabName is the tab's name without the leading number, used as the
// entry's context column: two tabs can share a binding (rename globally lives
// on both Tags and Projects), so the row has to say which one it will run on.
func paletteTabName(tb tab) string {
	full := [numTabs]string{tr("1 Tasks"), tr("2 Calendar"), tr("3 Projects"),
		tr("4 Tags"), tr("5 Board"), tr("6 Stats"), tr("7 Settings")}
	if tb < 0 || int(tb) >= len(full) {
		return ""
	}
	_, name, _ := strings.Cut(full[tb], " ")
	return name
}

// paletteExtras covers the actions whose registry key is a pair or a range, so
// paletteSendable rejects it, but which are worth finding by name anyway. One
// entry per direction, because "move card between stages" is two commands once
// you can no longer see which key you pressed.
var paletteExtras = []paletteCmd{
	// The two board entries are dropped with the board — see paletteCommands.
	{label: "move card to the next stage", key: "L", tab: tabBoard},
	{label: "move card to the previous stage", key: "H", tab: tabBoard},
	{label: "next month", key: "]", tab: tabCalendar},
	{label: "previous month", key: "[", tab: tabCalendar},
}

// paletteSendable reports whether a registry key can be pressed as one key.
// The registry's display forms cover ranges and pairs ("←/→", "H/L",
// "home/end · pgup/pgdn"); those are navigation, which nobody looks up by
// name, so they stay out rather than get a made-up single key.
func paletteSendable(key string) bool {
	switch key {
	case "enter", "esc", "tab", "delete":
		return true
	}
	return len([]rune(key)) == 1
}

// paletteCommands is the full entry list, in a stable order: the tab jumps
// first (the most common reason to reach for a palette), then each tab's own
// actions in registry order, then the global ones.
func paletteCommands() []paletteCmd {
	out := make([]paletteCmd, 0, 48)
	for _, t := range []struct {
		label string
		key   string
		tb    tab
	}{
		{tr("Go to Tasks"), "1", tabTasks},
		{tr("Go to Calendar"), "2", tabCalendar},
		{tr("Go to Projects"), "3", tabProjects},
		{tr("Go to Tags"), "4", tabTags},
		{tr("Go to Board"), "5", tabBoard},
		{tr("Go to Stats"), "6", tabStats},
		{tr("Go to Settings"), "7", tabSettings},
	} {
		// A hidden tab is not somewhere the palette can send you — its digit
		// does nothing, so the entry would be a command that silently fails.
		if !tabVisible(t.tb) {
			continue
		}
		out = append(out, paletteCmd{label: t.label, key: t.key, section: tr(secNavigation), anyTab: true})
	}

	// Per-tab actions, tab by tab so the list reads in tab order rather than
	// registry order (which interleaves the sections a binding belongs to).
	for _, tb := range []tab{tabTasks, tabCalendar, tabProjects, tabTags, tabBoard, tabStats, tabSettings} {
		if !tabVisible(tb) {
			continue // its keys are unreachable, so they are not commands
		}
		for ctx, mapped := range paletteTabs {
			if mapped != tb {
				continue
			}
			for i := range keymap {
				bd := &keymap[i]
				if bd.ctx&ctx == 0 || bd.ctx == ctxAll || !paletteSendable(bd.key) {
					continue
				}
				out = append(out, paletteCmd{
					label:   tr(bd.desc),
					key:     effectiveKey(bd.action, bd.key),
					section: paletteTabName(tb),
					tab:     tb,
				})
			}
		}
	}

	for _, extra := range paletteExtras {
		if !tabVisible(extra.tab) {
			continue
		}
		extra.label = tr(extra.label)
		extra.section = paletteTabName(extra.tab)
		out = append(out, extra)
	}

	// Two globals are worth a name. Undo, and the help overlay — the palette
	// is one of the two doors into the app for someone who knows nothing about
	// it, and a door that cannot lead to the map is a worse door. (The help
	// overlay lists ctrl+k, so with this the two point at each other.) Quit is
	// deliberately not here: a fuzzy list is the wrong place to keep something
	// irreversible one keystroke away.
	out = append(out,
		paletteCmd{label: tr("undo last change"), key: "u", section: tr(secApp), anyTab: true},
		paletteCmd{label: tr("keyboard shortcuts and help"), key: effectiveKey("help", "?"), section: tr(secApp), anyTab: true})
	return out
}

// paletteResults filters the entries against the query, fuzzily — the same
// subsequence rule the task search uses, so one habit covers both. Matching
// runs over the label and the section, so "board" and "stage" both find the
// card move.
func paletteResults(query string) []paletteCmd {
	all := paletteCommands()
	q := strings.TrimSpace(query)
	if q == "" {
		return all
	}
	lower := strings.ToLower(q)
	words := strings.Fields(lower)
	// Ranking, best first: the whole query as a substring, then every word of it
	// as a substring ("tag rename" → rename globally · Tags), then — only for
	// queries short enough to be an abbreviation — a subsequence ("gtb" → Go to
	// Board). Subsequence matching threads through almost anything, so letting
	// it answer a five-letter query fills the list with noise instead of
	// admitting there is no match.
	var whole, perWord, abbrev []paletteCmd
	for _, c := range all {
		hay := strings.ToLower(c.label + " " + c.section)
		switch {
		case strings.Contains(hay, lower):
			whole = append(whole, c)
		case allWordsPresent(hay, words):
			perWord = append(perWord, c)
		case len([]rune(q)) <= paletteAbbrevLen && subsequenceFold(hay, q):
			abbrev = append(abbrev, c)
		}
	}
	return append(append(whole, perWord...), abbrev...)
}

// paletteAbbrevLen is the longest query still treated as an abbreviation.
const paletteAbbrevLen = 3

func allWordsPresent(haystack string, words []string) bool {
	if len(words) < 2 {
		return false // a single word was already tried as a whole-query substring
	}
	for _, w := range words {
		if !strings.Contains(haystack, w) {
			return false
		}
	}
	return true
}

// keyMsgFor turns a key name into the KeyMsg dispatch expects. Shared by the
// palette (which runs an action by pressing its key) and the rebinding
// translation in dispatch.
func keyMsgFor(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// openPalette enters the palette with an empty query.
func (m *model) openPalette() tea.Cmd {
	m.mode = modePalette
	m.paletteCursor = 0
	m.paletteInput.SetValue("")
	m.paletteInput.Focus()
	return nil
}

// runPaletteCommand leaves the palette and performs the entry: switch to its
// tab if the key belongs to another one, then press the key through the normal
// dispatch path.
func (m model) runPaletteCommand(c paletteCmd) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	m.paletteInput.SetValue("")
	m.paletteCursor = 0
	if !c.anyTab && m.tab != c.tab {
		m.switchTab(c.tab)
		// A tab remembers whether its detail pane was open; the entry names a
		// list-level action, so make sure the list is what receives the key.
		m.pane = paneList
	}
	return m.Update(keyMsgFor(c.key))
}

// paletteSelection clamps the stored cursor against the live result count.
func (m model) paletteSelection(n int) int {
	if n <= 0 {
		return 0
	}
	if m.paletteCursor < 0 || m.paletteCursor >= n {
		return 0
	}
	return m.paletteCursor
}

// updatePalette handles the palette's own keys: ↑/↓ pick, enter runs, esc
// closes, everything else types into the query.
func (m model) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyMsg); ok {
		results := paletteResults(m.paletteInput.Value())
		switch key.String() {
		case "esc":
			m.mode = modeNormal
			m.paletteInput.SetValue("")
			m.paletteCursor = 0
			return m, nil
		case "enter":
			if len(results) == 0 {
				return m, nil
			}
			return m.runPaletteCommand(results[m.paletteSelection(len(results))])
		case "up":
			if n := len(results); n > 0 {
				m.paletteCursor = (m.paletteSelection(n) - 1 + n) % n
			}
			return m, nil
		case "down":
			if n := len(results); n > 0 {
				m.paletteCursor = (m.paletteSelection(n) + 1) % n
			}
			return m, nil
		}
	}
	before := m.paletteInput.Value()
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	if m.paletteInput.Value() != before {
		m.paletteCursor = 0 // the list changed under the cursor
	}
	return m, cmd
}
