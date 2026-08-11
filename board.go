package main

import "strings"

// board.go — the kanban stage configuration. A "stage" is a named board
// column a pending top-level task moves through (todo.Todo.Stage); the final
// board column is Status==Done itself and deliberately not part of this list,
// so completion never has a second source of truth.

// defaultStages is the column set the board boots into before settings.json
// is read, and the fallback when the configured list is empty or all-blank.
func defaultStages() []string {
	return []string{"Backlog", "In progress", "Review"}
}

// activeStages is the package-level stage list the board reads, following the
// applyTheme/applyLang/applyBiases pattern: set from settings at startup
// (initialModel, loadForCLI) and persisted back verbatim by persistSettings,
// so a hand-edited settings.json "stages" array survives the round trip.
var activeStages = defaultStages()

func applyStages(stages []string) { activeStages = stages }

// stagesFromSettings sanitizes the persisted list: entries are trimmed, blanks
// dropped, and duplicates (case-insensitive) collapsed onto their first
// occurrence. An empty result falls back to the defaults so a broken hand-edit
// degrades to a working board instead of a zero-column one.
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
	if len(out) == 0 {
		return defaultStages()
	}
	return out
}

// stageIndex maps a task's stored stage name onto a column of the active
// list, case-insensitively. Empty or unknown names — a fresh task, or a stage
// later renamed in settings — land in the first column, where a stranded task
// is visible rather than hidden.
func stageIndex(stage string) int {
	if stage == "" {
		return 0
	}
	for i, s := range activeStages {
		if strings.EqualFold(s, stage) {
			return i
		}
	}
	return 0
}

// canonicalStage resolves user input (CLI --stage, board moves) to the
// configured spelling of a stage name, so the stored value always matches the
// settings list letter-for-letter. ok=false when the name isn't configured.
func canonicalStage(input string) (string, bool) {
	return canonicalStageIn(activeStages, input)
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
func stagesDisplay() string {
	return strings.Join(activeStages, ", ")
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
// Without this, stageIndex's unknown-name fallback would dump every card of a
// renamed column into the first one, so renaming "Review" to "QA" would look
// like the board had lost its layout.
func stageRemap(oldStages, newStages []string) map[string]string {
	if len(newStages) == 0 {
		return nil
	}
	out := make(map[string]string)
	for i, old := range oldStages {
		if _, ok := canonicalStageIn(newStages, old); ok {
			continue
		}
		j := i
		if j >= len(newStages) {
			j = len(newStages) - 1
		}
		out[strings.ToLower(strings.TrimSpace(old))] = newStages[j]
	}
	return out
}
