package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"taskr/todo"
)

func getStoragePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "tasks.json")
}

// ── Settings ──────────────────────────────────────────────────────────────────

type appSettings struct {
	TaskSort     taskSortMode     `json:"task_sort"`
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
}

// biasesFromSettings is the small adapter between the persisted appSettings
// shape and the in-memory biases the score functions read.
func biasesFromSettings(s appSettings) biases {
	return biases{
		Deadline: s.SeqBiasDeadline,
		Priority: s.SeqBiasPriority,
		Momentum: s.SeqBiasMomentum,
	}
}

func settingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".taskr", "settings.json")
}

func loadSettings() appSettings {
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return appSettings{}
	}
	var s appSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return appSettings{}
	}
	return s
}

func saveSettings(s appSettings) error {
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
				return todos[i].CreatedAt.Before(todos[j].CreatedAt)
			}
			if iZero {
				return false
			}
			if jZero {
				return true
			}
			return todos[i].DueDate.Before(todos[j].DueDate)
		})
	case taskSortSize:
		// Small first, then Medium, then Large — matches the Momentum axis
		// (Small=10, Medium=5, Large=0) so "sort by Size" is the same intent
		// as "show me the quick wins". Ties break by CreatedAt for stability.
		sizeRank := func(s todo.Size) int {
			switch s {
			case todo.SizeSmall:
				return 0
			case todo.SizeMedium:
				return 1
			default: // Large
				return 2
			}
		}
		sort.SliceStable(todos, func(i, j int) bool {
			ri, rj := sizeRank(todos[i].Size), sizeRank(todos[j].Size)
			if ri != rj {
				return ri < rj
			}
			return todos[i].CreatedAt.Before(todos[j].CreatedAt)
		})
	default: // taskSortSequence
		// Precompute scores once per task — urgency() is O(1) but the sort
		// closure runs O(N log N) times, and reading by ID from the map keeps
		// the comparator stable across swaps.
		scores := make(map[string]float64, len(todos))
		for i := range todos {
			scores[todos[i].ID] = urgency(&todos[i])
		}
		sort.SliceStable(todos, func(i, j int) bool {
			si, sj := scores[todos[i].ID], scores[todos[j].ID]
			if si != sj {
				return si > sj
			}
			return todos[i].CreatedAt.Before(todos[j].CreatedAt)
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

