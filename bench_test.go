package main

import (
	"fmt"
	"testing"
	"time"

	"taskr/todo"
)

// benchTodos builds a realistic task set: a mix of pending/done, varied tags
// and projects, due dates spread around now, plus some subtasks — the kind of
// shape that exercises the active/done split, tag stats, and project rollups.
func benchTodos(n int) []todo.Todo {
	now := time.Now()
	tags := []string{"work", "home", "urgent", "later", "errand", "health", "reading", "code"}
	projects := []string{"alpha", "beta", "gamma", "", "delta", ""}
	out := make([]todo.Todo, 0, n)
	for i := 0; i < n; i++ {
		t := todo.New(fmt.Sprintf("Task number %d with a reasonably long title", i))
		t.ID = fmt.Sprintf("t%d", i)
		if i%3 == 0 {
			t.Status = todo.Done
		}
		t.Priority = todo.Priority(i % 3)
		t.Project = projects[i%len(projects)]
		// 0–2 tags per task.
		t.Tags = append(t.Tags, tags[i%len(tags)])
		if i%2 == 0 {
			t.Tags = append(t.Tags, tags[(i+3)%len(tags)])
		}
		t.DueDate = now.AddDate(0, 0, (i%14)-5)
		out = append(out, t)
	}
	// Make every 10th task a subtask of the one before it (link lives on the
	// child as ParentID).
	for i := 10; i < len(out); i += 10 {
		out[i].ParentID = out[i-1].ID
	}
	return out
}

func benchModel(n int) model {
	m := initialModel(&fakeRepo{todos: benchTodos(n)})
	m.termWidth = 120
	m.termHeight = 40
	m.ensureCache()
	return m
}

// BenchmarkView measures a full frame render with caches already warm — the
// common case (a keypress that doesn't change data, e.g. moving the cursor).
func BenchmarkView(b *testing.B) {
	for _, n := range []int{100, 500, 2000} {
		m := benchModel(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.View()
			}
		})
	}
}

// BenchmarkSearchKeystroke measures the per-keystroke search path: a filter
// change followed by the frame that renders it.
func BenchmarkSearchKeystroke(b *testing.B) {
	for _, n := range []int{100, 500, 2000} {
		m := benchModel(n)
		queries := []string{"task", "task number 1", "#work", "#urgent", ""}
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m.searchQuery = queries[i%len(queries)]
				m.markFilterDirty()
				_ = m.View()
			}
		})
	}
}

// BenchmarkRefreshCaches measures a full derived-state rebuild — what every
// data mutation pays for.
func BenchmarkRefreshCaches(b *testing.B) {
	for _, n := range []int{100, 500, 2000} {
		m := benchModel(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m.refreshCaches()
			}
		})
	}
}
