package main

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "time"

    "taskr/todo"
)

func getStoragePath() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".taskr", "tasks.json")
}

func ensureStorageDir() error {
    path := getStoragePath()
    dir := filepath.Dir(path)
    return os.MkdirAll(dir, 0755)
}

func saveTodos(todos []todo.Todo) error {
    if err := ensureStorageDir(); err != nil {
        return err
    }

    data, err := json.MarshalIndent(todos, "", "  ")
    if err != nil {
        return err
    }

    storagePath := getStoragePath()
    tmpPath    := storagePath + ".tmp"
    backupPath := storagePath + ".bak"

    // Write new data to a temp file first
    if err := os.WriteFile(tmpPath, data, 0644); err != nil {
        return err
    }

    // Best-effort: rotate the current save file into the backup slot.
    // If tasks.json does not exist yet this will fail silently, which is fine.
    _ = os.Rename(storagePath, backupPath)

    // Atomically promote the temp file to the live save file
    return os.Rename(tmpPath, storagePath)
}

// loadBackup attempts to load the most recent backup file.
// It is called automatically by loadTodos() when the primary file is corrupt.
func loadBackup() ([]todo.Todo, error) {
    backupPath := getStoragePath() + ".bak"
    data, err := os.ReadFile(backupPath)
    if err != nil {
        if os.IsNotExist(err) {
            return []todo.Todo{}, nil
        }
        return nil, err
    }
    var todos []todo.Todo
    if err := json.Unmarshal(data, &todos); err != nil {
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

    var todos []todo.Todo
    if err := json.Unmarshal(data, &todos); err != nil {
        // Primary file is corrupt — attempt transparent recovery from backup
        fmt.Fprintf(os.Stderr, "warning: tasks.json is corrupt (%v), attempting backup...\n", err)
        return loadBackup()
    }

    sortTodosByMode(todos, taskSortDueDate)
    return todos, nil
}

// sortTodosByMode sorts todos according to the given sort mode.
func sortTodosByMode(todos []todo.Todo, mode taskSortMode) {
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

// sortTodos sorts using the default due date mode.
func sortTodos(todos []todo.Todo) {
    sortTodosByMode(todos, taskSortDueDate)
}

// sortTodosByStartDate sorts by start date for the Gantt waterfall view.
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

// getProjects returns a sorted list of unique project names.
func getProjects(todos []todo.Todo) []string {
    seen := make(map[string]bool)
    var projects []string
    for _, t := range todos {
        if t.Project != "" && !seen[t.Project] {
            seen[t.Project] = true
            projects = append(projects, t.Project)
        }
    }
    sort.Strings(projects)
    return projects
}

// getTasksForProject returns all tasks for a given project sorted by start date.
func getTasksForProject(todos []todo.Todo, project string) []todo.Todo {
    var result []todo.Todo
    for _, t := range todos {
        if t.Project == project {
            result = append(result, t)
        }
    }
    return sortTodosByStartDate(result)
}

// parseDueDate accepts both dd-mm-yy (2-digit year) and dd-mm-yyyy (4-digit year).
func parseDueDate(s string) (time.Time, error) {
    if t, err := time.Parse("02-01-06", s); err == nil {
        return t, nil
    }
    if t, err := time.Parse("02-01-2006", s); err == nil {
        return t, nil
    }
    return time.Time{}, fmt.Errorf("invalid date format, use dd-mm-yy")
}
