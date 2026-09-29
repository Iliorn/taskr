package tasksync

import (
	"testing"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/todo"
)

// A client clock running far ahead must not own the merge: every
// merge-ordering timestamp (task + child ModifiedAt/DeletedAt) beyond the
// skew allowance is pulled back to now, while domain dates (DueDate) and
// timestamps within the allowance pass through untouched.
func TestClampFutureEventTimes(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	farFuture := now.Add(48 * time.Hour)
	slightlyAhead := now.Add(time.Minute) // within allowance

	skewed := todo.New("skewed clock")
	skewed.ModifiedAt = farFuture
	skewed.DueDate = farFuture // domain date: must survive
	skewed.Comments = []todo.Comment{{ID: "c1", Text: "hi", ModifiedAt: farFuture}}
	skewed.TimeEntries = []todo.TimeEntry{{ID: "e1", DeletedAt: farFuture}}
	farStamp := hlc.New("fast", "").Now(farFuture)
	okStamp := hlc.New("ok", "").Now(slightlyAhead)
	skewed.Stamps = map[string]hlc.Stamp{"title": farStamp, "notes": okStamp}
	ok := todo.New("healthy clock")
	ok.ModifiedAt = slightlyAhead

	tasks := []todo.Todo{skewed, ok}
	clampFutureEventTimes(tasks, now)

	if !tasks[0].ModifiedAt.Equal(now) {
		t.Errorf("task ModifiedAt = %v, want clamped to %v", tasks[0].ModifiedAt, now)
	}
	if !tasks[0].DueDate.Equal(farFuture) {
		t.Errorf("DueDate was clamped to %v — future due dates are data, not skew", tasks[0].DueDate)
	}
	if !tasks[0].Comments[0].ModifiedAt.Equal(now) {
		t.Errorf("comment ModifiedAt = %v, want clamped", tasks[0].Comments[0].ModifiedAt)
	}
	if !tasks[0].TimeEntries[0].DeletedAt.Equal(now) {
		t.Errorf("entry DeletedAt = %v, want clamped", tasks[0].TimeEntries[0].DeletedAt)
	}
	if got := tasks[0].Stamps["title"]; got != hlc.At(now) {
		t.Errorf("future stamp = %q, want clamped to %q", got, hlc.At(now))
	}
	if got := tasks[0].Stamps["notes"]; got != okStamp {
		t.Errorf("within-allowance stamp = %q, want untouched %q", got, okStamp)
	}
	if !tasks[1].ModifiedAt.Equal(slightlyAhead) {
		t.Errorf("within-allowance ModifiedAt = %v, want untouched %v", tasks[1].ModifiedAt, slightlyAhead)
	}
}
