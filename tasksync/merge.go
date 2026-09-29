package tasksync

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/todo"
)

// merge.go is the heart of tjek's cross-device sync: a pure, I/O-free fold of
// two task sets into one authoritative set. `tjek serve` (and the `tjek sync`
// client) call into it; everything around it — HTTP, storage — is plumbing.
//
// Resolution rules:
//   - Tasks are matched by their UUID.
//   - Each merge unit (todo.Fields: a scalar field, or fields that change
//     together) goes to the version with the later stamp for it, so edits to
//     different fields of one task on two devices both survive. Each tag and
//     dependency is a unit of its own, present or removed, so a tag added on
//     one device and another added elsewhere are both kept. An equal stamp on
//     different values (two versions from before stamps, modified in the same
//     millisecond) breaks on the unit's own content, so the result does not
//     depend on argument order or on any other field.
//   - A task saved before stamps existed reads its stamps from its
//     modification and deletion times (todo.Stamp), which is the whole-task
//     last-writer-wins those versions were merged by.
//   - Child collections (comments, time entries) merge by their own UUIDs.
//     A child tombstone is sticky. History events are never changed, so
//     they merge as a plain union by ID.
//   - Tombstones (task and child) are retained, never pruned, so deletions keep
//     propagating instead of a stale device resurrecting the row. Deletion is
//     the "deleted" unit, so an edit elsewhere does not undo it; only a later
//     restore does.
//   - Parent links are kept as the devices set them, a link to a deleted or
//     missing parent included: the app shows such a task at the top level
//     (ResolveParents), and a parent restored later finds it under it again.
//
// Merge is commutative, associative and idempotent (TestMergeIsACRDT), which
// is what lets any device sync with the server in any order, any number of
// times, and every device end with the same set.

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

// unitGreater breaks an equal-stamp tie on unit f between a and b by the
// unit's own content alone.
func unitGreater(f todo.Field, a, b *todo.Todo) bool {
	var pa, pb todo.Todo
	f.Copy(&pa, a)
	f.Copy(&pb, b)
	return hashGreater(pa, pb)
}

// mergeTask resolves two versions of the same task unit by unit. The result
// records every stamp either side had, reconstructed ones included, so it
// says as much about each unit as both sides together; two identical
// versions come back unchanged, which keeps an unchanged re-sync a no-op.
func mergeTask(a, b todo.Todo) todo.Todo {
	if bytes.Equal(CanonicalJSON(a), CanonicalJSON(b)) {
		return a
	}
	out := a
	stamps := make(map[string]hlc.Stamp, len(todo.Fields)+len(a.Tags)+len(b.Tags))
	for _, f := range todo.Fields {
		sa, sb := a.Stamp(f.Key), b.Stamp(f.Key)
		if sb.After(sa) || (sb == sa && !f.Same(&a, &b) && unitGreater(f, &b, &a)) {
			f.Copy(&out, &b)
		}
		if s := hlc.Max(sa, sb); s != "" {
			stamps[f.Key] = s
		}
	}

	out.Tags, out.Dependencies = nil, nil
	seen := map[string]bool{}
	for _, keys := range [][]string{a.SetKeys(), b.SetKeys()} {
		for _, k := range keys {
			if seen[k] {
				continue
			}
			seen[k] = true
			sa, sb := a.Stamp(k), b.Stamp(k)
			var present bool
			switch {
			case sa.After(sb):
				present = a.HasMember(k)
			case sb.After(sa):
				present = b.HasMember(k)
			default: // an equal stamp: an add beats a removal
				present = a.HasMember(k) || b.HasMember(k)
			}
			if s := hlc.Max(sa, sb); s != "" {
				stamps[k] = s
			}
			if !present {
				continue
			}
			if name, ok := strings.CutPrefix(k, todo.TagKeyPrefix); ok {
				out.Tags = append(out.Tags, name)
			} else if id, ok := strings.CutPrefix(k, todo.DepKeyPrefix); ok {
				out.Dependencies = append(out.Dependencies, id)
			}
		}
	}

	if s := hlc.Max(a.Stamp(todo.SetsKey), b.Stamp(todo.SetsKey)); s != "" {
		stamps[todo.SetsKey] = s
	}
	out.Stamps = stamps
	if b.ModifiedAt.After(out.ModifiedAt) {
		out.ModifiedAt = b.ModifiedAt
	}
	if out.CreatedAt.IsZero() || (!b.CreatedAt.IsZero() && b.CreatedAt.Before(out.CreatedAt)) {
		out.CreatedAt = b.CreatedAt
	}
	out.Comments = mergeComments(a.Comments, b.Comments)
	out.TimeEntries = mergeTimeEntries(a.TimeEntries, b.TimeEntries)
	out.History = todo.MergeHistory(a.History, b.History)
	return out
}

// mergeChildren unions two child slices by ID. A tombstone on either side wins
// and is retained so the deletion keeps propagating; among live versions the
// later-modified one wins (an edit on one device beats the stale copy on the
// other), with the higher-hash one as the tiebreak so records written before
// ModifiedAt existed (both zero) still resolve stably. Order follows first
// appearance.
func mergeChildren[T any](a, b []T, id func(T) string, deletedAt func(T) time.Time, modified func(T) time.Time) []T {
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
		case !deletedAt(x).IsZero():
			if s.isDel {
				// Both sides are tombstones: the later DeletedAt wins, with a
				// hash tiebreak, so the result
				// is independent of argument order. Without this the second
				// argument always won, and two devices that each deleted the
				// same child (different DeletedAt) would flip the server's
				// store on every sync, ping-ponging forever.
				dx, ds := deletedAt(x), deletedAt(s.v)
				if dx.After(ds) || (dx.Equal(ds) && hashGreater(x, s.v)) {
					s.v = x
				}
				return
			}
			s.isDel = true
			s.v = x // keep the tombstone so deleted_at persists
		case s.isDel:
			// a deletion on either side is sticky — ignore the live version
		case !s.haveLive:
			s.v = x
			s.haveLive = true
		case modified(x).After(modified(s.v)):
			s.v = x
		case modified(x).Before(modified(s.v)):
			// keep s.v — it is the later edit
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
	out := mergeChildren(a, b,
		func(c todo.Comment) string { return c.ID },
		func(c todo.Comment) time.Time { return c.DeletedAt },
		func(c todo.Comment) time.Time { return c.ModifiedAt },
	)
	authors := childAuthors(a, b, func(c todo.Comment) (string, string) { return c.ID, c.Author })
	for i := range out {
		if out[i].Author == "" {
			out[i].Author = authors[out[i].ID]
		}
	}
	return out
}

// childAuthors maps each child ID on either side to its author. An author is
// set once, by the save that first stores the record, so a copy without one
// has not heard of it yet rather than cleared it: the merge keeps it whichever
// version wins.
func childAuthors[T any](a, b []T, idAuthor func(T) (string, string)) map[string]string {
	out := make(map[string]string)
	for _, list := range [][]T{a, b} {
		for _, x := range list {
			if id, author := idAuthor(x); author != "" {
				out[id] = author
			}
		}
	}
	return out
}

// Time entries order by ModifiedAt like the others, but with a fallback for
// entries from before the field existed: a stopped entry beats a running copy
// of itself when neither carries a ModifiedAt, since a stop is always the
// later event.
func mergeTimeEntries(a, b []todo.TimeEntry) []todo.TimeEntry {
	out := mergeChildren(a, b,
		func(e todo.TimeEntry) string { return e.ID },
		func(e todo.TimeEntry) time.Time { return e.DeletedAt },
		func(e todo.TimeEntry) time.Time {
			if !e.ModifiedAt.IsZero() {
				return e.ModifiedAt
			}
			return e.StoppedAt // zero for a running legacy entry → stop wins
		},
	)
	authors := childAuthors(a, b, func(e todo.TimeEntry) (string, string) { return e.ID, e.Author })
	for i := range out {
		if out[i].Author == "" {
			out[i].Author = authors[out[i].ID]
		}
	}
	return out
}

// Merge folds two task sets into one authoritative set. It is symmetric in its
// arguments. Tombstones are retained; output is sorted by ID for a stable
// wire order.
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
	out := make([]todo.Todo, 0, len(merged))
	for _, t := range merged {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ResolveParents makes every parent link in tasks one the app can follow: a
// live task whose parent is missing from tasks or deleted becomes top level,
// and a chain that loops back on itself (A→B→A) is cut at its highest ID, so
// the parent-chain walkers (sequence-score rollup, ancestor auto-close) never
// spin. A loop cannot be made through the UI, but two devices each moving one
// task under the other before they sync can, as can a corrupt store.
//
// It is for the task list the app shows, not for Merge, which keeps links as
// the devices set them so that merging stays independent of order and a
// restored parent finds its subtasks again. A task the app then saves keeps
// the resolved link.
func ResolveParents(tasks []todo.Todo) {
	byID := make(map[string]todo.Todo, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}
	for id, t := range byID {
		if t.Deleted || t.ParentID == "" {
			continue
		}
		if parent, ok := byID[t.ParentID]; !ok || parent.Deleted {
			t.ParentID = ""
			byID[id] = t
		}
	}
	breakParentCycles(byID)
	for i := range tasks {
		tasks[i].ParentID = byID[tasks[i].ID].ParentID
	}
}

// breakParentCycles cuts every loop in the parent links of merged at its
// highest ID. Each task has exactly one parent, so the parent graph is
// functional and every component holds at most one cycle; re-homing the
// highest-ID member of each (deterministic, independent of map order) is
// enough to make the whole set acyclic.
func breakParentCycles(merged map[string]todo.Todo) {
	for id := range merged {
		seen := make(map[string]bool)
		for cur := id; cur != ""; {
			if seen[cur] {
				cut := highestInCycle(merged, cur)
				t := merged[cut]
				t.ParentID = ""
				merged[cut] = t
				break
			}
			seen[cur] = true
			t, ok := merged[cur]
			if !ok {
				break
			}
			cur = t.ParentID
		}
	}
}

// highestInCycle returns the greatest task ID on the cycle that contains start,
// walking the loop once from start back to itself. start is assumed to lie on a
// cycle (it's the node a chain walk reached twice).
func highestInCycle(merged map[string]todo.Todo, start string) string {
	hi := start
	for cur := merged[start].ParentID; cur != start && cur != ""; {
		if cur > hi {
			hi = cur
		}
		t, ok := merged[cur]
		if !ok {
			break
		}
		cur = t.ParentID
	}
	return hi
}
