package main

import (
    "fmt"
    "os"

    tea "github.com/charmbracelet/bubbletea"
)

func main() {
    // Remove leftover binary from a previous Windows self-update, if any.
    if execPath, err := os.Executable(); err == nil {
        _ = os.Remove(execPath + ".old")
    }

    p := tea.NewProgram(initialModel(), tea.WithAltScreen())
    if _, err := p.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}
