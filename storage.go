package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"taskr/todo"
)

func getStoragePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "tasks.json")
}

// taskrDir is the storage directory (~/.taskr) holding tasks.db, settings, and
// sync state.
func taskrDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr")
}

// ── Settings ──────────────────────────────────────────────────────────────────

// currentSettingsVersion is stamped into newly written settings.json. Bump it
// when the on-disk shape changes in a way migrateSettings must handle.
const currentSettingsVersion = 1

type appSettings struct {
	// Version is the schema marker for migrateSettings. Zero means "legacy
	// pre-versioning file" (read as-is); newly saved settings always have
	// the current version.
	Version int `json:"version"`

	TaskSort     taskSortMode     `json:"task_sort"`
	HistorySort  historySortMode  `json:"history_sort"`
	TagSort      tagSortMode      `json:"tag_sort"`
	LearningSort learningSortMode `json:"learning_sort"`
	Theme        string           `json:"theme"`
	Language     string           `json:"language"`

	// Sequencing biases: ints 0/1/2 mapping to biasBalanced/Relaxed/Intense.
	// Stored as ints (not enum names) to match the existing convention used by
	// TaskSort and friends. Zero value = biasBalanced, which is the neutral
	// default so an unset settings.json keeps the engine "Balanced" out of the
	// box without explicit migration.
	SeqBiasDeadline biasLevel `json:"seq_bias_deadline"`
	SeqBiasPriority biasLevel `json:"seq_bias_priority"`
	SeqBiasMomentum biasLevel `json:"seq_bias_momentum"`

	// SeqAgingDisabled gates the per-day Age contribution. Stored as the
	// inverse of the user-facing "Aging" toggle so the zero value (=false)
	// keeps aging on by default — matches pre-toggle behaviour without
	// migration.
	SeqAgingDisabled bool `json:"seq_aging_disabled"`

	// AutoCloseParent: when on, a parent task is auto-marked Done the moment
	// its last open subtask closes. Off by default because the parent often
	// represents review/sign-off work that survives the children. Opt-in
	// for users who prefer parents as folders.
	AutoCloseParent bool `json:"auto_close_parent"`

	// AutoCloseSubtasks is the mirror of AutoCloseParent: when on, marking a
	// parent Done also closes its still-open subtasks, so a done parent never
	// strands pending children (invisible in every list but export). Off by
	// default so closing a parent doesn't silently finish work you meant to
	// keep open — the confirm/prompt path stays the default.
	AutoCloseSubtasks bool `json:"auto_close_subtasks"`
}

// migrateSettings brings settings saved under an older schema version up to
// the current one. No-op today; future breaking field changes get a case
// here so old files are converted rather than silently misread.
func migrateSettings(version int, s appSettings) appSettings {
	_ = version
	s.Version = currentSettingsVersion
	return s
}

// biasesFromSettings is the small adapter between the persisted appSettings
// shape and the in-memory biases the score functions read.
func biasesFromSettings(s appSettings) biases {
	return biases{
		Deadline: s.SeqBiasDeadline,
		Priority: s.SeqBiasPriority,
		Momentum: s.SeqBiasMomentum,
		Aging:    !s.SeqAgingDisabled,
	}
}

func settingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "settings.json")
}

// loadSettings reads ~/.taskr/settings.json and applies any schema migration.
// Missing file is *not* an error — a brand-new install legitimately has no
// settings yet and gets all-zero defaults. Any other failure (corrupt JSON,
// permissions, partial write) is returned so the caller can surface it
// instead of silently resetting the user's preferences.
func loadSettings() (appSettings, error) {
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return appSettings{}, nil
		}
		return appSettings{}, err
	}
	var s appSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return appSettings{}, fmt.Errorf("settings.json is corrupt: %w", err)
	}
	if s.Version != currentSettingsVersion {
		s = migrateSettings(s.Version, s)
	}
	return s, nil
}

func saveSettings(s appSettings) error {
	s.Version = currentSettingsVersion
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0644)
}

func ensureStorageDir() error {
	path := getStoragePath()
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0755)
}

// currentTaskFileVersion is the schema version stamped into newly written task
// files. Bump it when the on-disk shape changes in a way migrate() must handle.
const currentTaskFileVersion = 1

// taskFile is the versioned on-disk envelope. Older releases wrote a bare
// []todo.Todo with no version; decodeTaskFile reads both shapes so existing
// users' data keeps loading.
type taskFile struct {
	Version int         `json:"version"`
	Todos   []todo.Todo `json:"todos"`
}

// migrate brings todos saved under an older schema version up to the current
// one. It is a no-op today; future breaking field changes get a case here so
// data is converted rather than silently dropped.
func migrate(version int, todos []todo.Todo) []todo.Todo {
	return todos
}

// decodeTaskFile unmarshals either the versioned envelope or the legacy
// bare-array format. A bare array fails to decode into the struct, which cleanly
// routes it to the legacy path.
func decodeTaskFile(data []byte) ([]todo.Todo, error) {
	var tf taskFile
	if err := json.Unmarshal(data, &tf); err == nil && tf.Version > 0 {
		return migrate(tf.Version, tf.Todos), nil
	}
	var todos []todo.Todo
	if err := json.Unmarshal(data, &todos); err != nil {
		return nil, err
	}
	return todos, nil
}

// OPTIMIZATION: use json.Marshal (no indentation) for faster serialization.
// The file is still valid JSON, just compact. Saves ~30-40% marshalling time
// and produces smaller files.
func marshalTodos(todos []todo.Todo) ([]byte, error) {
	return json.Marshal(taskFile{Version: currentTaskFileVersion, Todos: todos})
}

// loadBackup attempts to load the most recent backup file.
func loadBackup() ([]todo.Todo, error) {
	backupPath := getStoragePath() + ".bak"
	data, err := os.ReadFile(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []todo.Todo{}, nil
		}
		return nil, err
	}
	todos, err := decodeTaskFile(data)
	if err != nil {
		return nil, fmt.Errorf("backup file is also corrupt: %w", err)
	}
	return todos, nil
}

// loadTodosJSON reads the legacy JSON file. It is no longer the live store —
// SQLite (storage_sqlite.go) is — but remains the one-time import source and a
// corruption fallback.
func loadTodosJSON() ([]todo.Todo, error) {
	path := getStoragePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []todo.Todo{}, nil
		}
		return nil, err
	}

	todos, err := decodeTaskFile(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: tasks.json is corrupt (%v), attempting backup...\n", err)
		return loadBackup()
	}

	sortTodosByMode(todos, taskSortSequence)
	return todos, nil
}

// sortTodosByMode sorts todos by the given mode. After the sequencing engine
// only two modes exist; any other value falls through to Sequence.
func sortTodosByMode(todos []todo.Todo, mode taskSortMode) {
	if len(todos) <= 1 {
		return
	}
	switch mode {
	case taskSortDueDate:
		sort.SliceStable(todos, func(i, j int) bool {
			iZero := todos[i].DueDate.IsZero()
			jZero := todos[j].DueDate.IsZero()
			if iZero && jZero {
				if !todos[i].CreatedAt.Equal(todos[j].CreatedAt) {
					return todos[i].CreatedAt.Before(todos[j].CreatedAt)
				}
				return todos[i].ID < todos[j].ID
			}
			if iZero {
				return false
			}
			if jZero {
				return true
			}
			if !todos[i].DueDate.Equal(todos[j].DueDate) {
				return todos[i].DueDate.Before(todos[j].DueDate)
			}
			if !todos[i].CreatedAt.Equal(todos[j].CreatedAt) {
				return todos[i].CreatedAt.Before(todos[j].CreatedAt)
			}
			return todos[i].ID < todos[j].ID
		})
	case taskSortSize:
		// Small first, then Medium, then Large — "sort by Size" is the same
		// intent as "show me the quick wins". Ties break by CreatedAt for
		// stability.
		sort.SliceStable(todos, func(i, j int) bool {
			ri, rj := sizeRank(todos[i].Size), sizeRank(todos[j].Size)
			if ri != rj {
				return ri < rj
			}
			if !todos[i].CreatedAt.Equal(todos[j].CreatedAt) {
				return todos[i].CreatedAt.Before(todos[j].CreatedAt)
			}
			return todos[i].ID < todos[j].ID
		})
	default: // taskSortSequence
		sortTodosBySequenceWithRollup(todos, nil)
	}
}

// sortTodosBySequenceWithRollup is the sequence-mode sort, with an optional
// per-ID rollup map that boosts each task's effective score to
// max(own, rollup[id]). The boost is how a parent inherits the urgency of
// its highest-priority subtask — so a "high" subtask buried under a "low"
// parent doesn't disappear into the bottom of the list. Passing nil
// preserves the original behaviour (used by callers that don't have the
// child set on hand, e.g. on-disk loads).
func sortTodosBySequenceWithRollup(todos []todo.Todo, rollup map[string]float64) {
	if len(todos) <= 1 {
		return
	}
	scores := make(map[string]float64, len(todos))
	for i := range todos {
		s := sequenceScore(&todos[i])
		if rollup != nil {
			if boost, ok := rollup[todos[i].ID]; ok && boost > s {
				s = boost
			}
		}
		scores[todos[i].ID] = s
	}
	sort.SliceStable(todos, func(i, j int) bool {
		si, sj := scores[todos[i].ID], scores[todos[j].ID]
		if si != sj {
			return si > sj
		}
		// Tie on score → the documented tie-break chain, each key meaningful
		// rather than arbitrary:
		//   1. due proximity (a real due date beats none; sooner beats later)
		//   2. size ascending (the quick win first)
		//   3. CreatedAt ascending (tasks entered as a burst — a decomposed
		//      plan typed in execution order — keep that entry order)
		//   4. ID — the absolute backstop so tasks identical on every key
		//      (common in the Done list, where score is uniformly 0) don't
		//      inherit the random order they came out of Store.allTodos()
		//      in, which would otherwise reshuffle them on every cache
		//      rebuild (e.g. while the search-input cursor blinks).
		iZero, jZero := todos[i].DueDate.IsZero(), todos[j].DueDate.IsZero()
		if iZero != jZero {
			return jZero
		}
		if !iZero && !todos[i].DueDate.Equal(todos[j].DueDate) {
			return todos[i].DueDate.Before(todos[j].DueDate)
		}
		if ri, rj := sizeRank(todos[i].Size), sizeRank(todos[j].Size); ri != rj {
			return ri < rj
		}
		if !todos[i].CreatedAt.Equal(todos[j].CreatedAt) {
			return todos[i].CreatedAt.Before(todos[j].CreatedAt)
		}
		return todos[i].ID < todos[j].ID
	})
}

// sizeRank orders sizes Small < Medium < Large for sorting — smaller first
// matches the quick-win reading everywhere sizes break a tie.
func sizeRank(s todo.Size) int {
	switch s {
	case todo.SizeSmall:
		return 0
	case todo.SizeMedium:
		return 1
	default: // Large
		return 2
	}
}

// sortHistory orders the completed-tasks list by its own mode, independent of
// the active-task taskSort. Completed mode is most-recent-first (the usual
// "what did I just finish" view); Alpha is case-insensitive title A→Z. Ties
// break by ID for a stable order across cache rebuilds.
func sortHistory(todos []todo.Todo, mode historySortMode) {
	if len(todos) <= 1 {
		return
	}
	switch mode {
	case historySortAlpha:
		sort.SliceStable(todos, func(i, j int) bool {
			ti := strings.ToLower(todos[i].Title)
			tj := strings.ToLower(todos[j].Title)
			if ti != tj {
				return ti < tj
			}
			return todos[i].ID < todos[j].ID
		})
	default: // historySortCompleted — most recent first
		sort.SliceStable(todos, func(i, j int) bool {
			if !todos[i].CompletedAt.Equal(todos[j].CompletedAt) {
				return todos[i].CompletedAt.After(todos[j].CompletedAt)
			}
			return todos[i].ID < todos[j].ID
		})
	}
}

func sortTodosByStartDate(todos []todo.Todo) []todo.Todo {
	result := make([]todo.Todo, len(todos))
	copy(result, todos)
	sort.SliceStable(result, func(i, j int) bool {
		iZero := result[i].StartDate.IsZero()
		jZero := result[j].StartDate.IsZero()
		if iZero && jZero {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		if iZero {
			return false
		}
		if jZero {
			return true
		}
		return result[i].StartDate.Before(result[j].StartDate)
	})
	return result
}
