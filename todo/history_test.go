package todo

import (
	"testing"
	"time"
)

// Events sort by time, then ID, so two devices holding the same events list
// them the same way.
func TestSortHistoryOrdersByTimeThenID(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	h := []Event{
		{ID: "c", At: at.Add(time.Minute)},
		{ID: "b", At: at},
		{ID: "a", At: at},
	}
	SortHistory(h)
	if h[0].ID != "a" || h[1].ID != "b" || h[2].ID != "c" {
		t.Fatalf("order %s %s %s, want a b c", h[0].ID, h[1].ID, h[2].ID)
	}
}

// A merge is the union by ID, sorted, whichever side an event came from and
// however many sides held it.
func TestMergeHistoryIsAUnionByID(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	mine := []Event{{ID: "1", At: at}, {ID: "3", At: at.Add(2 * time.Minute)}}
	theirs := []Event{{ID: "2", At: at.Add(time.Minute)}, {ID: "3", At: at.Add(2 * time.Minute)}}

	for _, got := range [][]Event{MergeHistory(mine, theirs), MergeHistory(theirs, mine)} {
		if len(got) != 3 || got[0].ID != "1" || got[1].ID != "2" || got[2].ID != "3" {
			t.Fatalf("merged %+v, want 1 2 3", got)
		}
	}
	if got := MergeHistory(mine, nil); len(got) != 2 {
		t.Errorf("merge with nothing changed the history: %+v", got)
	}
	if got := MergeHistory(nil, theirs); len(got) != 2 {
		t.Errorf("merge into nothing lost events: %+v", got)
	}
}
