package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// appVersion is the current build version. Override at build time with:
//
//	go build -ldflags "-X main.appVersion=v1.8.0" -o taskr .
var appVersion = "dev"

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
