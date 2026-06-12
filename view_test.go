package main

import (
    "strings"
    "testing"
    "time"

    "github.com/charmbracelet/x/ansi"
    "taskr/todo"
)

// TestNarrowNoWrap ensures no rendered line ever exceeds the terminal width,
// which would cause ugly wrapping inside the bordered panels.
func TestNarrowNoWrap(t *testing.T) {
    for _, width := range []int{40, 50, 60, 70, 80, 120} {
        m := initialModel()
        m.termWidth = width
        m.termHeight = 30
        for i := 0; i < 5; i++ {
            task := todo.New("A fairly long task title that could overflow easily here")
            task.DueDate = time.Now().AddDate(0, 0, i)
            task.Tags = []string{"alpha", "beta"}
            m.todos = append(m.todos, task)
        }
        m.refreshCaches()
        out := m.View()
        for n, line := range strings.Split(out, "\n") {
            if w := ansi.StringWidth(line); w > width {
                t.Errorf("width=%d: line %d is %d cells wide: %q", width, n, w, line)
            }
        }
    }
}
