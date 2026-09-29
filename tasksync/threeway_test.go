package tasksync

import (
	"reflect"
	"testing"
	"time"

	"github.com/Iliorn/tjek/todo"
)

// threeWayFixture is a base task and two edits of it made on different
// devices, remote the later, so Merge alone keeps remote whole.
func threeWayFixture() (base, local, remote todo.Todo) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	base = todo.Todo{ID: "t1", Title: "Pay rent", Priority: todo.PriorityMedium,
		Tags: []string{"home"}, CreatedAt: t0, ModifiedAt: t0}
	local, remote = base, base
	local.Tags = append([]string(nil), base.Tags...)
	remote.Tags = append([]string(nil), base.Tags...)
	local.ModifiedAt = t0.Add(time.Hour)
	remote.ModifiedAt = t0.Add(2 * time.Hour)
	return base, local, remote
}

func threeWay(t *testing.T, base, local, remote todo.Todo) (todo.Todo, bool) {
	t.Helper()
	merged := Merge([]todo.Todo{remote}, []todo.Todo{local})
	if len(merged) != 1 {
		t.Fatalf("Merge returned %d tasks, want 1", len(merged))
	}
	return ThreeWay(base, local, remote, merged[0], time.Now())
}

// The case the whole file is for: a due date set on one device and a priority
// on another both survive, where Merge alone kept only the later device's.
func TestThreeWayKeepsEditsToDifferentFields(t *testing.T) {
	base, local, remote := threeWayFixture()
	due := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	local.DueDate = due
	remote.Priority = todo.PriorityHigh

	got, ok := threeWay(t, base, local, remote)
	if !ok {
		t.Fatal("ThreeWay had nothing to add, but Merge dropped the local due date")
	}
	if !got.DueDate.Equal(due) || got.Priority != todo.PriorityHigh {
		t.Errorf("due %v priority %v; want both edits kept", got.DueDate, got.Priority)
	}
	if !got.ModifiedAt.After(remote.ModifiedAt) || !got.ModifiedAt.After(local.ModifiedAt) {
		t.Errorf("ModifiedAt %v is not after both sides, so the next sync would not spread it", got.ModifiedAt)
	}
}

// A field both devices changed is a real conflict: it keeps Merge's verdict.
func TestThreeWayLeavesAFieldBothChangedToMerge(t *testing.T) {
	base, local, remote := threeWayFixture()
	local.Title = "Pay rent today"
	remote.Title = "Pay the rent"

	got, ok := threeWay(t, base, local, remote)
	if ok || got.Title != "Pay the rent" {
		t.Errorf("title %q ok=%v; want the later edit's title and nothing added", got.Title, ok)
	}
}

// Closing a task moves its status, completion time and rank together, so the
// three come from one side even when the other side touched one of them.
func TestThreeWayMovesTheCompletionFieldsTogether(t *testing.T) {
	base, local, remote := threeWayFixture()
	done := base.ModifiedAt.Add(30 * time.Minute)
	local.Status, local.CompletedAt, local.SeqRankAtDone = todo.Done, done, 2
	remote.Notes = "landlord changed account"

	got, ok := threeWay(t, base, local, remote)
	if !ok || got.Status != todo.Done || !got.CompletedAt.Equal(done) || got.SeqRankAtDone != 2 {
		t.Errorf("got status %v completed %v rank %d ok=%v; want the local close whole",
			got.Status, got.CompletedAt, got.SeqRankAtDone, ok)
	}
	if got.Notes != remote.Notes {
		t.Errorf("notes %q; want the remote description kept", got.Notes)
	}
}

// Tags merge as sets: one added on each device keeps both, and one removed on
// either device stays removed.
func TestThreeWayMergesTagsAsSets(t *testing.T) {
	base, local, remote := threeWayFixture()
	base.Tags = []string{"home", "money"}
	local.Tags = []string{"home", "money", "urgent"}
	remote.Tags = []string{"home", "bills"}

	got, ok := threeWay(t, base, local, remote)
	if !ok || !sameSet(got.Tags, []string{"home", "urgent", "bills"}) {
		t.Errorf("tags %v ok=%v; want home, urgent and bills, without money", got.Tags, ok)
	}
}

// Nothing to add when one side made every change: Merge already has it.
func TestThreeWayAddsNothingToAOneSidedEdit(t *testing.T) {
	base, local, remote := threeWayFixture()
	remote.Priority = todo.PriorityHigh

	if _, ok := threeWay(t, base, local, remote); ok {
		t.Error("only remote changed anything, yet ThreeWay rewrote Merge's result")
	}
}

// The local edit also survives when the remote version won Merge on its
// timestamp alone, having changed no field the base did not already have.
func TestThreeWayKeepsALocalEditARemoteTimestampOutranks(t *testing.T) {
	base, local, remote := threeWayFixture()
	local.Project = "flat"

	got, ok := threeWay(t, base, local, remote)
	if !ok || got.Project != "flat" {
		t.Errorf("project %q ok=%v; want the local project kept", got.Project, ok)
	}
}

// A deletion stays Merge's business, as does a base that is another task.
func TestThreeWayStaysOutOfDeletesAndStrangers(t *testing.T) {
	base, local, remote := threeWayFixture()
	local.Priority = todo.PriorityHigh
	remote.Deleted, remote.DeletedAt = true, remote.ModifiedAt
	if _, ok := threeWay(t, base, local, remote); ok {
		t.Error("ThreeWay rewrote a deleted task")
	}

	base, local, remote = threeWayFixture()
	local.Priority = todo.PriorityHigh
	base.ID = "someone else"
	if _, ok := threeWay(t, base, local, remote); ok {
		t.Error("ThreeWay used another task as the base")
	}
}

// Every field a user can edit is either decided by ThreeWay or listed here as
// deliberately left to Merge. A new field on todo.Todo fails this test until
// it is one or the other, so it cannot quietly fall back to last-writer-wins.
func TestThreeWayCoversEveryField(t *testing.T) {
	leftToMerge := map[string]bool{
		"ID": true, "CreatedAt": true, "ModifiedAt": true,
		"Comments": true, "TimeEntries": true, // merged by their own IDs
		"Deleted": true, "DeletedAt": true, // a delete is Merge's call
		"Stamps": true,
	}
	typ := reflect.TypeOf(todo.Todo{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if leftToMerge[name] {
			continue
		}
		base, local, remote := threeWayFixture()
		v := reflect.ValueOf(&local).Elem().Field(i)
		switch v.Kind() {
		case reflect.String:
			v.SetString("changed")
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v.SetInt(v.Int() + 1)
		case reflect.Slice:
			v.Set(reflect.Append(v, reflect.ValueOf("added")))
		case reflect.Struct:
			v.Set(reflect.ValueOf(time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)))
		default:
			t.Fatalf("field %s: kind %s is not handled by this test", name, v.Kind())
		}
		// The other device edits some other field, so Merge keeps its version.
		if name == "Notes" {
			remote.Priority = todo.PriorityHigh
		} else {
			remote.Notes = "remote edit"
		}

		got, ok := threeWay(t, base, local, remote)
		want := reflect.ValueOf(local).Field(i).Interface()
		have := reflect.ValueOf(got).Field(i).Interface()
		if !ok || !reflect.DeepEqual(have, want) {
			t.Errorf("field %s: a local edit beside a remote one came out %v, want %v; add it to taskFields", name, have, want)
		}
	}
}
