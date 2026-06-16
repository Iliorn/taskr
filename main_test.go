package main

import (
	"os"
	"testing"
)

// TestMain isolates the entire test binary from the real ~/.taskr. Storage
// paths derive from $HOME (getStoragePath/dbPath), and several tests build a
// model via initialModel — which opens the store — so without this redirect a
// plain `go test` would read and create files under the developer's real task
// directory.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "taskr-test-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", tmp)
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
