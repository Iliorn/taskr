//go:build !windows

package main

import tea "github.com/charmbracelet/bubbletea"

// inputProgramOptions is empty off Windows: the default input path is already
// event-driven (epoll on Linux, kqueue on BSD/macOS) and resizes arrive as
// SIGWINCH.
func inputProgramOptions() []tea.ProgramOption { return nil }

// startResizePoller is a no-op off Windows, where SIGWINCH does the job.
func startResizePoller(*tea.Program) (stop func()) { return func() {} }

// inputPathName describes the input path for `taskr doctor`.
func inputPathName() string { return "default (event-driven)" }
