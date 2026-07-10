package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"taskr/todo"
)

// exportEnvelope is the versioned wrapper emitted by `taskr export`.
// Version 1 is the only defined version; readers must reject any version > 1
// with a clear error so future format changes do not silently corrupt data.
type exportEnvelope struct {
	Version    int         `json:"version"`
	ExportedAt time.Time   `json:"exported_at"`
	Tasks      []todo.Todo `json:"tasks"`
}

// ── import ────────────────────────────────────────────────────────────────────

func cliImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: taskr import <file>
       taskr import -              read from stdin

Merges the tasks in the export file into the local store. Both the versioned
envelope produced by 'taskr export' and the legacy bare JSON array are accepted.
Import is idempotent: running it a second time with the same file changes nothing.`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	src := fs.Arg(0)
	var data []byte
	var err error
	if src == "-" {
		// io.ReadAll, not a line scanner: a JSON export is one long line, and
		// any per-token buffer cap would fail large imports with "token too long".
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(filepath.Clean(src))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr import: read: %v\n", err)
		return 1
	}

	tasks, err := parseExportData(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr import: %v\n", err)
		return 1
	}

	// Get the DB handle through the same path that sync uses: openStore sets
	// up the package-level `db` singleton and applies all migrations.
	if err := openStore(); err != nil {
		fmt.Fprintf(os.Stderr, "taskr import: open store: %v\n", err)
		return 1
	}

	// Snapshot the pre-merge set so the summary can report how many tasks the
	// merge actually changed (new or edited), not just a live-count delta —
	// an import that only edits existing tasks leaves the count unchanged but
	// still did work.
	before, err := loadTodosForSync(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr import: load current: %v\n", err)
		return 1
	}

	merged, changed, err := mergeIntoStore(db, tasks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskr import: merge: %v\n", err)
		return 1
	}

	nChanged := 0
	if changed {
		nChanged = len(changedTasks(before, merged))
	}
	fmt.Printf("imported %d task(s), %d changed\n", len(tasks), nChanged)
	return 0
}

// parseExportData sniffs the leading non-whitespace byte to distinguish a
// bare JSON array (legacy) from the versioned envelope object.
func parseExportData(data []byte) ([]todo.Todo, error) {
	// Skip leading whitespace to find the first structural byte.
	first := byte(0)
	for _, b := range data {
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			first = b
			break
		}
	}
	switch first {
	case '[':
		// Legacy bare array.
		var tasks []todo.Todo
		if err := json.Unmarshal(data, &tasks); err != nil {
			return nil, fmt.Errorf("malformed JSON array: %w", err)
		}
		return tasks, nil
	case '{':
		// Versioned envelope.
		var env exportEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("malformed export envelope: %w", err)
		}
		if env.Version > 1 {
			return nil, fmt.Errorf("unsupported export version %d (this build only knows version 1; upgrade taskr to import this file)", env.Version)
		}
		return env.Tasks, nil
	case 0:
		return nil, fmt.Errorf("empty input")
	default:
		return nil, fmt.Errorf("unrecognised format (expected JSON object or array, got %q)", string(first))
	}
}
