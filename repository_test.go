package main

import "taskr/todo"

// fakeRepo is an in-memory Repository for tests — no database, no filesystem.
// This is the payoff of the port: model tests build with a fakeRepo instead of
// touching real storage.
type fakeRepo struct{ todos []todo.Todo }

func (r *fakeRepo) Load() ([]todo.Todo, error) { return r.todos, nil }

func (r *fakeRepo) Save(todos []todo.Todo) error {
	r.todos = copyTodos(todos)
	return nil
}

// newTestModel builds a model backed by an empty in-memory repo.
func newTestModel() model { return initialModel(&fakeRepo{}) }
