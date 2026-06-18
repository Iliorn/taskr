package main

import (
	"database/sql"
	"testing"

	"taskr/todo"
)

// assertSequenceColumn reads the persisted sequence column for taskID and
// reports any drift from the live in-memory formula at the current biases.
// Tolerance matches approxEq (1e-3) — float comparisons must not be exact.
func assertSequenceColumn(t *testing.T, h *sql.DB, taskID string, live *todo.Todo, label string) {
	t.Helper()
	var col float64
	if err := h.QueryRow(`SELECT sequence FROM todos WHERE id=?`, taskID).Scan(&col); err != nil {
		t.Fatalf("%s: query sequence: %v", label, err)
	}
	want := sequenceScore(live)
	if !approxEq(col, want) {
		t.Errorf("%s: column=%v, formula=%v, drift=%v", label, col, want, want-col)
	}
}

// TestSequenceColumnTracksFormulaAfterSave is the baseline: a freshly-saved
// row's `sequence` column must equal the score the live formula produces.
func TestSequenceColumnTracksFormulaAfterSave(t *testing.T) {
	h := openTestDB(t)
	applyBiases(biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true})

	task := todo.New("ranked task")
	task.Priority = todo.PriorityHigh
	task.Size = todo.SizeSmall
	saveTodos(t, h, []todo.Todo{task})

	assertSequenceColumn(t, h, task.ID, &task, "after save")
}

// TestSequenceColumnDriftsWithoutResync documents the bug: when activeBiases
// changes between writes, the column reflects the *old* weights until
// something resaves the row. This test pins the drift in place so any
// "optimization" that removes resyncSequenceColumn fails CI.
func TestSequenceColumnDriftsWithoutResync(t *testing.T) {
	h := openTestDB(t)
	applyBiases(biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true})

	task := todo.New("drift demo")
	task.Priority = todo.PriorityHigh
	task.Size = todo.SizeSmall
	saveTodos(t, h, []todo.Todo{task})

	var before float64
	if err := h.QueryRow(`SELECT sequence FROM todos WHERE id=?`, task.ID).Scan(&before); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Move every bias to Intense (×2). The in-memory formula now reports a
	// score ~roughly double; the column should *not* have changed because
	// nothing resaved this row.
	applyBiases(biases{Deadline: biasIntense, Priority: biasIntense, Momentum: biasIntense, Aging: true})
	live := sequenceScore(&task)

	var after float64
	if err := h.QueryRow(`SELECT sequence FROM todos WHERE id=?`, task.ID).Scan(&after); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !approxEq(before, after) {
		t.Errorf("column changed without a save (before=%v, after=%v) — somebody resaved silently", before, after)
	}
	if approxEq(after, live) {
		t.Errorf("expected drift between stale column (%v) and live formula (%v)", after, live)
	}
}

// TestSequenceColumnAfterResync proves the fix: resyncSequenceColumn must
// rewrite the column for every live row so it matches the formula under the
// current biases. This is the test the resync implementation has to satisfy.
func TestSequenceColumnAfterResync(t *testing.T) {
	h := openTestDB(t)
	applyBiases(biases{Deadline: biasBalanced, Priority: biasBalanced, Momentum: biasBalanced, Aging: true})

	a := todo.New("alpha")
	a.Priority = todo.PriorityHigh
	a.Size = todo.SizeSmall
	b := todo.New("beta")
	b.Priority = todo.PriorityLow
	b.Size = todo.SizeLarge
	saveTodos(t, h, []todo.Todo{a, b})

	applyBiases(biases{Deadline: biasIntense, Priority: biasIntense, Momentum: biasIntense, Aging: true})
	if err := resyncSequenceColumn(h); err != nil {
		t.Fatalf("resync: %v", err)
	}

	assertSequenceColumn(t, h, a.ID, &a, "alpha after resync")
	assertSequenceColumn(t, h, b.ID, &b, "beta after resync")
}
