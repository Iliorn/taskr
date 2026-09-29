package tasksync

import (
	"time"

	"github.com/Iliorn/tjek/todo"
)

// threeway.go keeps both halves of a conflict whose halves do not overlap.
//
// Merge orders two versions of a task by their modification time and keeps the
// later one whole, so an edit made on one device (the due date, say) is lost
// when another device edited a different field of the same task (its priority)
// before the two synced. A client that remembers the version it last agreed on
// with the server (the base) can tell which fields each side changed, and
// ThreeWay puts together the version that keeps both. The server's merge is
// untouched: the client pushes the combined version on its next sync, with a
// modification time later than either side's, and it wins there as any newer
// edit does.

// ThreeWay combines local and remote, two edits of base, field by field. A
// field changed on one side only takes that side's value; a field changed on
// both keeps merged's, which is Merge's last-writer-wins verdict. Tags and
// dependencies merge as sets, so a tag added on each side keeps both. Children
// (comments, time entries) are merged's, since Merge already merges them by
// their own IDs.
//
// ok is false when there is nothing to add to merged: a side is deleted, base
// is another task, or the fields come out as merged has them. Otherwise the
// result carries a modification time after every version's (StampAt against
// now), so the next sync makes it the fleet's.
func ThreeWay(base, local, remote, merged todo.Todo, now time.Time) (todo.Todo, bool) {
	if base.ID == "" || base.ID != local.ID || local.ID != remote.ID || remote.ID != merged.ID ||
		local.Deleted || remote.Deleted || merged.Deleted {
		return merged, false
	}
	out := merged
	for _, f := range taskFields {
		lc, rc := !f.same(local, base), !f.same(remote, base)
		switch {
		case lc && !rc:
			f.take(&out, local)
		case rc && !lc:
			f.take(&out, remote)
		}
	}
	out.Tags = mergeSet(base.Tags, local.Tags, remote.Tags)
	out.Dependencies = mergeSet(base.Dependencies, local.Dependencies, remote.Dependencies)

	if sameFields(out, merged) {
		return merged, false
	}
	latest := local.ModifiedAt
	for _, t := range []time.Time{remote.ModifiedAt, merged.ModifiedAt} {
		if t.After(latest) {
			latest = t
		}
	}
	out.ModifiedAt = todo.StampAt(now, latest)
	return out, true
}

// KeepsLocalEdits reports whether final carries every change local made to
// base: each field local changed has local's value, each tag or dependency
// local added is there and each one it removed is not. It is what tells a
// combined edit from a lost one, which DroppedLocalEdits alone cannot see
// without the base.
func KeepsLocalEdits(base, local, final todo.Todo) bool {
	if final.Deleted && !local.Deleted {
		return false
	}
	for _, f := range taskFields {
		if !f.same(local, base) && !f.same(final, local) {
			return false
		}
	}
	for _, sets := range [][3][]string{
		{base.Tags, local.Tags, final.Tags},
		{base.Dependencies, local.Dependencies, final.Dependencies},
	} {
		inBase, inLocal, inFinal := setOf(sets[0]), setOf(sets[1]), setOf(sets[2])
		for s := range inLocal {
			if !inBase[s] && !inFinal[s] {
				return false
			}
		}
		for s := range inBase {
			if !inLocal[s] && inFinal[s] {
				return false
			}
		}
	}
	return true
}

// taskField is one unit ThreeWay decides on: a field, or fields that change
// together (closing a task sets its status, completion time and rank).
type taskField struct {
	same func(a, b todo.Todo) bool
	take func(dst *todo.Todo, src todo.Todo)
}

// taskFields is every field a user edits, bar the two sets. A field missing
// here would always come out as merged has it, which is last-writer-wins for
// that field and no worse than before; TestThreeWayCoversEveryField keeps
// the list complete.
var taskFields = []taskField{
	{func(a, b todo.Todo) bool { return a.Title == b.Title }, func(d *todo.Todo, s todo.Todo) { d.Title = s.Title }},
	{func(a, b todo.Todo) bool { return a.Notes == b.Notes }, func(d *todo.Todo, s todo.Todo) { d.Notes = s.Notes }},
	{func(a, b todo.Todo) bool { return a.Priority == b.Priority }, func(d *todo.Todo, s todo.Todo) { d.Priority = s.Priority }},
	{func(a, b todo.Todo) bool { return a.Size == b.Size }, func(d *todo.Todo, s todo.Todo) { d.Size = s.Size }},
	{func(a, b todo.Todo) bool { return a.Project == b.Project }, func(d *todo.Todo, s todo.Todo) { d.Project = s.Project }},
	{func(a, b todo.Todo) bool { return a.ParentID == b.ParentID }, func(d *todo.Todo, s todo.Todo) { d.ParentID = s.ParentID }},
	{func(a, b todo.Todo) bool { return a.Recurrence == b.Recurrence }, func(d *todo.Todo, s todo.Todo) { d.Recurrence = s.Recurrence }},
	{func(a, b todo.Todo) bool { return a.Stage == b.Stage }, func(d *todo.Todo, s todo.Todo) { d.Stage = s.Stage }},
	{func(a, b todo.Todo) bool { return a.DueDate.Equal(b.DueDate) }, func(d *todo.Todo, s todo.Todo) { d.DueDate = s.DueDate }},
	{func(a, b todo.Todo) bool { return a.StartDate.Equal(b.StartDate) }, func(d *todo.Todo, s todo.Todo) { d.StartDate = s.StartDate }},
	{
		func(a, b todo.Todo) bool {
			return a.Status == b.Status && a.CompletedAt.Equal(b.CompletedAt) && a.SeqRankAtDone == b.SeqRankAtDone
		},
		func(d *todo.Todo, s todo.Todo) {
			d.Status, d.CompletedAt, d.SeqRankAtDone = s.Status, s.CompletedAt, s.SeqRankAtDone
		},
	},
}

// sameFields reports whether a and b agree on every field ThreeWay decides.
func sameFields(a, b todo.Todo) bool {
	for _, f := range taskFields {
		if !f.same(a, b) {
			return false
		}
	}
	return sameSet(a.Tags, b.Tags) && sameSet(a.Dependencies, b.Dependencies)
}

// mergeSet is a three-way merge of a set: base, plus what either side added,
// less what either side removed. Order is local's, then what remote added.
func mergeSet(base, local, remote []string) []string {
	inBase, inLocal, inRemote := setOf(base), setOf(local), setOf(remote)
	removed := func(s string) bool {
		return inBase[s] && (!inLocal[s] || !inRemote[s])
	}
	var out []string
	seen := map[string]bool{}
	for _, list := range [][]string{local, remote} {
		for _, s := range list {
			if !seen[s] && !removed(s) {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func setOf(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, s := range list {
		m[s] = true
	}
	return m
}

func sameSet(a, b []string) bool {
	sa, sb := setOf(a), setOf(b)
	if len(sa) != len(sb) {
		return false
	}
	for s := range sa {
		if !sb[s] {
			return false
		}
	}
	return true
}
