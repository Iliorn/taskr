package main

import (
	"strings"
	"time"

	"github.com/Iliorn/taskr/todo"
)

// board.go — the kanban stage configuration. A "stage" is a named board
// column a pending top-level task moves through (todo.Todo.Stage), and the
// **last entry of the list is the Done column**: it is Status==Done rather
// than a stored stage name, so completion still has exactly one source of
// truth, but its heading is a name like any other and can be renamed.
//
// Everything downstream reads the list through boardConfig.pending and
// boardConfig.doneColumn rather than slicing it by hand, which is what keeps
// "the last one is Done" a single decision instead of a convention every call
// site has to remember.

// defaultDoneStage is the shipped name of the final column. Only a default —
// once the list is user-edited it is whatever they last named it.
const defaultDoneStage = "Done"

// defaultStages is the column set the board boots into before settings.json
// is read, and the fallback when the configured list is empty or all-blank.
// The trailing entry is the Done column.
func defaultStages() []string {
	return []string{"Backlog", "In progress", "Review", defaultDoneStage}
}

// boardConfig is every board-shaped preference, read together: the column
// list, whether the surface is shown at all, when the list was last edited on
// this device, and whether it is shared with the fleet. It is a value — the
// model holds one (m.boardCfg), the CLI reads one from settings.json
// (storedBoard), and a sync is handed a copy — so the background sync never
// reads a list the Update loop is replacing.
type boardConfig struct {
	// stages is the column list; the last entry is the Done column. Set it
	// through setStages, which keeps that invariant.
	stages []string
	// modifiedAt is when this device last edited its column list, and the
	// only thing that decides whether its list beats another's. Zero means
	// never edited here — the shipped defaults — and a zero stamp never wins,
	// so a fresh install joining a fleet takes the fleet's columns instead of
	// resetting them. Stamped only by applyStageEdit.
	modifiedAt time.Time
	// shown gates the whole kanban surface: the Board tab and the detail
	// pane's Stage row. Stages are a workflow some people run their tasks
	// through and others never touch, and for the second group the tab is a
	// permanent wrong turn and the field a row to skip past. Defaults on: a
	// fresh install should show what the README describes.
	shown bool
	// sync shares the column list with the fleet. Negative in settings.json
	// (`sync_board_disabled`) like the other opt-outs, so the zero value
	// shares: a device that has never edited its columns cannot overwrite
	// anyone, and one that has is the one whose names the fleet wants.
	sync bool
}

// defaultBoardConfig is the board before settings.json is read.
func defaultBoardConfig() boardConfig {
	return boardConfig{stages: defaultStages(), shown: true, sync: true}
}

// boardConfigFromSettings reads the board preferences out of settings.json,
// sanitizing the column list on the way in.
func boardConfigFromSettings(s appSettings) boardConfig {
	return boardConfig{
		stages:     stagesFromSettings(s),
		modifiedAt: s.StagesModifiedAt,
		shown:      !s.BoardDisabled,
		sync:       !s.SyncBoardDisabled,
	}
}

// storedBoard reads the board preferences from settings.json, for the CLI,
// which has no model to hold them. An unreadable file reads as the defaults.
func storedBoard() boardConfig {
	s, _ := loadSettings()
	return boardConfigFromSettings(s)
}

// setStages installs a stage list, normalizing it first so the board's one
// structural invariant — a Done column at the end, with at least one working
// column before it — holds no matter which entry point set the list (settings,
// the Settings editor, a sync, a test).
func (c *boardConfig) setStages(stages []string) { c.stages = ensureDoneColumn(stages) }

// ensureDoneColumn upholds "the last column is Done" for a list that may not
// have enough columns to say so. An empty list is the defaults. A single name
// is ambiguous — one column cannot be both the work and the finish line — so
// it keeps what was typed and supplies the missing half: "Doing" becomes
// Doing → Done, and a lone "Done" gets a working column in front of it.
func ensureDoneColumn(stages []string) []string {
	switch len(stages) {
	case 0:
		return defaultStages()
	case 1:
		if strings.EqualFold(stages[0], defaultDoneStage) {
			return []string{defaultStages()[0], stages[0]}
		}
		return []string{stages[0], defaultDoneStage}
	}
	return stages
}

// pending is the list without its Done column: the columns a *pending*
// task's Stage can name. Every lookup of a stored stage goes through this, so
// no pending task can ever be filed under the Done heading.
func (c boardConfig) pending() []string { return pendingOf(c.stages) }

// doneColumn is the index of the final column — the one holding the completed
// tasks, whatever it has been named.
func (c boardConfig) doneColumn() int { return len(c.stages) - 1 }

// stagesFromSettings sanitizes the persisted list: entries are trimmed, blanks
// dropped, and duplicates (case-insensitive) collapsed onto their first
// occurrence. An empty result falls back to the defaults so a broken hand-edit
// degrades to a working board instead of a zero-column one, and a too-short
// one gains its missing column (ensureDoneColumn).
func stagesFromSettings(s appSettings) []string {
	seen := make(map[string]bool, len(s.Stages))
	out := make([]string, 0, len(s.Stages))
	for _, raw := range s.Stages {
		name := strings.TrimSpace(raw)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return ensureDoneColumn(out)
}

// stageIndex maps a task's stored stage name onto a column of the active
// list, case-insensitively. It searches the *pending* columns only, so the
// answer is never the Done column: a task is done because of its status, never
// because of a name it happens to carry. Empty or unknown names — a fresh
// task, or a stage later renamed in settings — land in the first column, where
// a stranded task is visible rather than hidden.
func (c boardConfig) stageIndex(stage string) int {
	if stage == "" {
		return 0
	}
	for i, s := range c.pending() {
		if strings.EqualFold(s, stage) {
			return i
		}
	}
	return 0
}

// stageDisplay is the column heading a pending task's stored stage belongs
// under — the detail pane's Stage row and `taskr show`. Empty only on a board
// with no working columns at all, which ensureDoneColumn prevents.
func (c boardConfig) stageDisplay(stage string) string {
	p := c.pending()
	if len(p) == 0 {
		return ""
	}
	return p[c.stageIndex(stage)]
}

// canonicalStage resolves user input (CLI --stage, board moves) to the
// configured spelling of a stage name, so the stored value always matches the
// settings list letter-for-letter. ok=false when the name isn't configured.
// The Done column is deliberately not resolvable: --stage Done would be a
// second way to complete a task, bypassing everything closePendingTask does.
func (c boardConfig) canonicalStage(input string) (string, bool) {
	return canonicalStageIn(c.pending(), input)
}

// canonicalStageIn is canonicalStage against an arbitrary list — used while
// editing the stage list, where the new list isn't live yet.
func canonicalStageIn(stages []string, input string) (string, bool) {
	name := strings.TrimSpace(input)
	for _, s := range stages {
		if strings.EqualFold(s, name) {
			return s, true
		}
	}
	return "", false
}

// stagesDisplay is the Settings-row rendering (and the pre-fill of its editor)
// of the active stage list: the same comma-separated form the editor parses.
// The Done column is in it — that is how renaming it is discoverable.
func (c boardConfig) stagesDisplay() string {
	return strings.Join(c.stages, ", ")
}

// parseStagesInput turns the Settings editor's comma-separated line into a
// stage list, running it through the same sanitizer a hand-edited
// settings.json goes through — so both entry points accept exactly the same
// input and degrade the same way (all-blank falls back to the defaults).
func parseStagesInput(line string) []string {
	parts := strings.Split(line, ",")
	return stagesFromSettings(appSettings{Stages: parts})
}

// stageRemap describes where the cards of each dropped stage should go when
// the stage list is edited, keyed by the lower-cased old name. Mapping is by
// position: a stage renamed in place (old index i → new index i) keeps its
// cards in the same column, and a stage that fell off a shortened list hands
// its cards to the last remaining column. Names still present in the new list
// are absent from the map — there is nothing to move.
//
// Both lists are read as full column lists and compared over their *pending*
// portions, so a card can never be remapped onto the Done column — and
// renaming the Done column moves nothing, because its cards are found by
// status rather than by name.
//
// Without this, stageIndex's unknown-name fallback would dump every card of a
// renamed column into the first one, so renaming "Review" to "QA" would look
// like the board had lost its layout.
func stageRemap(oldStages, newStages []string) map[string]string {
	oldPending, newPending := pendingOf(oldStages), pendingOf(newStages)
	if len(newPending) == 0 {
		return nil
	}
	out := make(map[string]string)
	for i, old := range oldPending {
		if _, ok := canonicalStageIn(newPending, old); ok {
			continue
		}
		j := i
		if j >= len(newPending) {
			j = len(newPending) - 1
		}
		out[strings.ToLower(strings.TrimSpace(old))] = newPending[j]
	}
	return out
}

// pendingOf is boardConfig.pending for an arbitrary list — used while editing
// the stage list, where the new list isn't live yet.
func pendingOf(stages []string) []string {
	if len(stages) < 2 {
		return nil
	}
	return stages[:len(stages)-1]
}

// ── The detail pane's Stage field ────────────────────────────────────────────

// stageFieldVisible reports whether the detail pane shows a Stage row for this
// task. A subtask never reaches the board, and Done is a status rather than a
// stage — offering to move either between columns would describe something the
// board does not do. The Settings toggle turns the row off for anyone not
// using the board at all.
func (c boardConfig) stageFieldVisible(t *todo.Todo) bool {
	return c.shown && t != nil && t.Status == todo.Pending && t.ParentID == ""
}

// cycleStage moves a task one column along the configured stages, wrapping at
// both ends. It deliberately cannot reach the last column: that column is
// Status==Done, and completing a task from a field labelled "Stage" would be a
// second, hidden path into the one transition that carries timer, subtask and
// recurrence semantics (closePendingTask). Done stays a d away.
func (c boardConfig) cycleStage(current string, dir int) string {
	p := c.pending()
	if len(p) == 0 {
		return current
	}
	next := (c.stageIndex(current) + dir + len(p)) % len(p)
	return p[next]
}
