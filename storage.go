package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
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

// writeTodosData writes pre-marshalled JSON data to disk atomically.
func writeTodosData(data []byte) error {
	if err := ensureStorageDir(); err != nil {
		return err
	}

	storagePath := getStoragePath()
	tmpPath := storagePath + ".tmp"
	backupPath := storagePath + ".bak"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	_ = os.Rename(storagePath, backupPath)
	return os.Rename(tmpPath, storagePath)
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

// marshalTodosPretty is used only for initial save or export if needed.
func marshalTodosPretty(todos []todo.Todo) ([]byte, error) {
	return json.MarshalIndent(taskFile{Version: currentTaskFileVersion, Todos: todos}, "", "  ")
}

// saveTodos marshals and writes synchronously (used for initial load path).
func saveTodos(todos []todo.Todo) error {
	data, err := marshalTodos(todos)
	if err != nil {
		return err
	}
	return writeTodosData(data)
}

// saveDataCmd returns a tea.Cmd that writes pre-marshalled data to disk asynchronously.
func saveDataCmd(data []byte) tea.Cmd {
	return func() tea.Msg {
		if err := writeTodosData(data); err != nil {
			return saveErrMsg{err}
		}
		return saveDoneMsg{}
	}
}

// prepareSave marshals todos to JSON (fast, CPU-bound) and returns a Cmd for async disk write.
// OPTIMIZATION: uses compact JSON for speed.
func prepareSave(todos []todo.Todo) (tea.Cmd, error) {
	data, err := marshalTodos(todos)
	if err != nil {
		return nil, err
	}
	return saveDataCmd(data), nil
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

func loadTodos() ([]todo.Todo, error) {
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

	sortTodosByMode(todos, taskSortDueDate)
	return todos, nil
}

// sortTodosByMode sorts todos according to the given sort mode.
func sortTodosByMode(todos []todo.Todo, mode taskSortMode) {
	if len(todos) <= 1 {
		return
	}
	switch mode {
	case taskSortPriority:
		sort.SliceStable(todos, func(i, j int) bool {
			pi := todos[i].Priority
			pj := todos[j].Priority
			if pi != pj {
				return pi > pj
			}
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
	case taskSortStartDate:
		sort.SliceStable(todos, func(i, j int) bool {
			iZero := todos[i].StartDate.IsZero()
			jZero := todos[j].StartDate.IsZero()
			if iZero && jZero {
				return todos[i].CreatedAt.Before(todos[j].CreatedAt)
			}
			if iZero {
				return false
			}
			if jZero {
				return true
			}
			return todos[i].StartDate.Before(todos[j].StartDate)
		})
	case taskSortCreated:
		sort.SliceStable(todos, func(i, j int) bool {
			return todos[i].CreatedAt.Before(todos[j].CreatedAt)
		})
	default: // taskSortDueDate
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

func getProjects(todos []todo.Todo) []string {
	seen := make(map[string]bool, len(todos)/4)
	projects := make([]string, 0, 8)
	for i := range todos {
		if p := todos[i].Project; p != "" && !seen[p] {
			seen[p] = true
			projects = append(projects, p)
		}
	}
	sort.Strings(projects)
	return projects
}

func getTasksForProject(todos []todo.Todo, project string) []todo.Todo {
	var result []todo.Todo
	for i := range todos {
		if todos[i].Project == project {
			result = append(result, todos[i])
		}
	}
	return sortTodosByStartDate(result)
}
