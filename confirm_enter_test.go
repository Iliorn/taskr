package main

import (
	"testing"

	"taskr/todo"
)

// Enter is an accept on confirm prompts, matching y (they're already explicit
// opt-ins). Exercised through the real Update dispatch on a delete confirm.
func TestEnterConfirmsDelete(t *testing.T) {
	task := todo.New("delete me")
	m := newTagModel(task)
	m.tab = tabTasks
	m.mode = modeConfirmDelete
	m.pendingDeleteID = task.ID

	next := sendKey(t, m, "enter")

	if next.mode != modeNormal {
		t.Errorf("enter should resolve the confirm; mode = %v", next.mode)
	}
	if next.get(task.ID) != nil {
		t.Error("enter should confirm the delete, but the task is still present")
	}
}

// esc still cancels — Enter accepting must not remove the negative path.
func TestEscStillCancelsDelete(t *testing.T) {
	task := todo.New("keep me")
	m := newTagModel(task)
	m.tab = tabTasks
	m.mode = modeConfirmDelete
	m.pendingDeleteID = task.ID

	next := sendKey(t, m, "esc")

	if next.mode != modeNormal {
		t.Errorf("esc should close the confirm; mode = %v", next.mode)
	}
	if next.get(task.ID) == nil {
		t.Error("esc should cancel the delete, but the task was removed")
	}
}
