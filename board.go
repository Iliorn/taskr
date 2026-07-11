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
	name := strings.TrimSpace(input)
	for _, s := range activeStages {
		if strings.EqualFold(s, name) {
			return s, true
		}
	}
	return "", false
}
