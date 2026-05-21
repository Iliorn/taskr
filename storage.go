package main

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "time"

    tea "github.com/charmbracelet/bubbletea"
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

// OPTIMIZATION: use json.Marshal (no indentation) for faster serialization.
// The file is still valid JSON, just compact. Saves ~30-40% marshalling time
// and produces smaller files.
func marshalTodos(todos []todo.Todo) ([]byte, error) {
    return json.Marshal(todos)
}

// marshalTodosPretty is used only for initial save or export if needed.
func marshalTodosPretty(todos []todo.Todo) ([]byte, error) {
    return json.MarshalIndent(todos, "", "  ")
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

func sortTodos(todos []todo.Todo) {
    sortTodosByMode(todos, taskSortDueDate)
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

// parseDueDate accepts dd-mm-yy, dd-mm-yyyy, and natural language shortcuts.
func parseDueDate(s string) (time.Time, error) {
    lower := strings.ToLower(strings.TrimSpace(s))
    now := time.Now()
    today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

    switch lower {
    case "today":
        return today, nil
    case "tomorrow":
        return today.AddDate(0, 0, 1), nil
    case "yesterday":
        return today.AddDate(0, 0, -1), nil
    case "next week":
        return today.AddDate(0, 0, 7), nil
    case "next month":
        return today.AddDate(0, 1, 0), nil
    }

    if strings.HasPrefix(lower, "next ") {
        dayName := strings.TrimPrefix(lower, "next ")
        if weekday, ok := parseWeekday(dayName); ok {
            return nextWeekday(today, weekday), nil
        }
    }

    if weekday, ok := parseWeekday(lower); ok {
        return nextWeekday(today, weekday), nil
    }

    if strings.HasPrefix(lower, "+") && len(lower) > 2 {
        unit := lower[len(lower)-1]
        numStr := lower[1 : len(lower)-1]
        if n, ok := parsePositiveInt(numStr); ok && n > 0 {
            switch unit {
            case 'd':
                return today.AddDate(0, 0, n), nil
            case 'w':
                return today.AddDate(0, 0, n*7), nil
            case 'm':
                return today.AddDate(0, n, 0), nil
            }
        }
    }

    if t, err := time.Parse("02-01-06", s); err == nil {
        return t, nil
    }
    if t, err := time.Parse("02-01-2006", s); err == nil {
        return t, nil
    }
    return time.Time{}, fmt.Errorf("invalid date: use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+Nd/+Nw/+Nm'")
}

func parseWeekday(s string) (time.Weekday, bool) {
    days := map[string]time.Weekday{
        "monday":    time.Monday,
        "tuesday":   time.Tuesday,
        "wednesday": time.Wednesday,
        "thursday":  time.Thursday,
        "friday":    time.Friday,
        "saturday":  time.Saturday,
        "sunday":    time.Sunday,
        "mon":       time.Monday,
        "tue":       time.Tuesday,
        "wed":       time.Wednesday,
        "thu":       time.Thursday,
        "fri":       time.Friday,
        "sat":       time.Saturday,
        "sun":       time.Sunday,
    }
    if wd, ok := days[s]; ok {
        return wd, true
    }
    return 0, false
}

func nextWeekday(today time.Time, target time.Weekday) time.Time {
    current := today.Weekday()
    daysAhead := int(target) - int(current)
    if daysAhead <= 0 {
        daysAhead += 7
    }
    return today.AddDate(0, 0, daysAhead)
}

func parsePositiveInt(s string) (int, bool) {
    if len(s) == 0 {
        return 0, false
    }
    n := 0
    for _, ch := range s {
        if ch < '0' || ch > '9' {
            return 0, false
        }
        n = n*10 + int(ch-'0')
    }
    return n, true
}
