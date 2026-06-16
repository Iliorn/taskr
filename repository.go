package main

import "taskr/todo"

// Repository is the persistence port. The app depends on this contract rather
// than on any concrete store: SQLite fulfils it in production (sqliteRepo), an
// in-memory fake fulfils it in tests. This keeps storage details out of the
// domain/UI layer and makes the store swappable (e.g. a future sync adapter).
type Repository interface {
	Load() ([]todo.Todo, error)
	Save(todos []todo.Todo) error
}
