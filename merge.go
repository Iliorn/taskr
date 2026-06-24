package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"time"

	"taskr/todo"
)

// merge.go is the heart of taskr's cross-device sync: a pure, I/O-free fold of
// two task sets into one authoritative set. `taskr serve` (and the `taskr sync`
// client) call into it; everything around it — HTTP, storage — is plumbing.
//
// Resolution rules:
//   - Tasks are matched by their UUID.
//   - Scalar fields, tags and dependencies go to the last writer, ordered by
//     eventTime (modification time, or deletion time for a tombstone); an exact
//     timestamp tie breaks on the higher content hash so the result is stable
//     and independent of argument order.
//   - Child collections (comments, learnings, time entries) merge independently
//     by their own UUIDs, so a child added or deleted on the side that loses the
//     scalar race is not silently dropped. A child tombstone is sticky.
//   - Tombstones (task and child) are retained, never pruned, so deletions keep
//     propagating instead of a stale device resurrecting the row.
//   - A task whose parent ends up missing or deleted is re-homed to top level.

// eventTime is the moment a task version was last acted on: deletion time for a
// tombstone, otherwise modification time. Two versions of the same task are
// ordered by it — later wins.
func eventTime(t todo.Todo) time.Time {
	if t.Deleted {
		return t.DeletedAt
	}
	return t.ModifiedAt
}

// hashGreater reports whether x sorts after y by the SHA-256 of its canonical
// JSON. It is the deterministic tiebreaker when two versions carry the same
// timestamp, keeping merges stable and order-independent.
func hashGreater[T any](x, y T) bool {
	bx, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	hx := sha256.Sum256(bx)
	hy := sha256.Sum256(by)
	return bytes.Compare(hx[:], hy[:]) > 0
}

// laterWins returns the surviving scalar version of two same-ID tasks.
func laterWins(a, b todo.Todo) todo.Todo {
	ta, tb := eventTime(a), eventTime(b)
	switch {
	case ta.After(tb):
		return a
	case tb.After(ta):
		return b
	default:
		if hashGreater(a, b) {
			return a
		}
		return b
	}
}

// mergeTask resolves two versions of the same task. Scalars, tags and deps come
// from the last writer; child collections merge independently so a comment,
// learning or time entry touched on the losing side survives.
func mergeTask(a, b todo.Todo) todo.Todo {
	out := laterWins(a, b)
	out.Comments = mergeComments(a.Comments, b.Comments)
	out.Learnings = mergeLearnings(a.Learnings, b.Learnings)
	out.TimeEntries = mergeTimeEntries(a.TimeEntries, b.TimeEntries)
	return out
}

// mergeChildren unions two child slices by ID. A tombstone on either side wins
// and is retained so the deletion keeps propagating; among live versions the
// higher-hash one is kept for a stable result. Order follows first appearance.
func mergeChildren[T any](a, b []T, id func(T) string, deleted func(T) bool) []T {
	type slot struct {
		v        T
		isDel    bool
		haveLive bool
	}
	order := make([]string, 0, len(a)+len(b))
	slots := make(map[string]*slot, len(a)+len(b))
	consume := func(x T) {
		k := id(x)
		s, ok := slots[k]
		if !ok {
			s = &slot{}
			slots[k] = s
			order = append(order, k)
		}
		switch {
		case deleted(x):
			s.isDel = true
			s.v = x // keep the tombstone so deleted_at persists
		case s.isDel:
			// a deletion on either side is sticky — ignore the live version
		case !s.haveLive:
			s.v = x
			s.haveLive = true
		case hashGreater(x, s.v):
			s.v = x
		}
	}
	for _, x := range a {
		consume(x)
	}
	for _, x := range b {
		consume(x)
	}
	var out []T
	for _, k := range order {
		out = append(out, slots[k].v)
	}
	return out
}

func mergeComments(a, b []todo.Comment) []todo.Comment {
	return mergeChildren(a, b,
		func(c todo.Comment) string { return c.ID },
		func(c todo.Comment) bool { return !c.DeletedAt.IsZero() },
	)
}

func mergeLearnings(a, b []todo.Learning) []todo.Learning {
	return mergeChildren(a, b,
		func(l todo.Learning) string { return l.ID },
		func(l todo.Learning) bool { return !l.DeletedAt.IsZero() },
	)
}

func mergeTimeEntries(a, b []todo.TimeEntry) []todo.TimeEntry {
	return mergeChildren(a, b,
		func(e todo.TimeEntry) string { return e.ID },
		func(e todo.TimeEntry) bool { return !e.DeletedAt.IsZero() },
	)
}

// Merge folds two task sets into one authoritative set. It is symmetric in its
// arguments. Tombstones are retained; a task whose parent is missing or deleted
// is re-homed to top level; output is sorted by ID for a stable wire order.
func Merge(server, client []todo.Todo) []todo.Todo {
	merged := make(map[string]todo.Todo, len(server)+len(client))
	for _, t := range server {
		merged[t.ID] = t
	}
	for _, t := range client {
		if existing, ok := merged[t.ID]; ok {
			merged[t.ID] = mergeTask(existing, t)
		} else {
			merged[t.ID] = t
		}
	}
	for id, t := range merged {
		if t.Deleted || t.ParentID == "" {
			continue
		}
		if parent, ok := merged[t.ParentID]; !ok || parent.Deleted {
			t.ParentID = ""
			merged[id] = t
		}
	}
	out := make([]todo.Todo, 0, len(merged))
	for _, t := range merged {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
