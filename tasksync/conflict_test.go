package tasksync

import (
	"strings"
	"testing"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/todo"
)

// conflict_test.go covers DroppedLocalEdits — the recovery net. When the merge
// overwrites a local edit, the losing version is appended to sync.log so the
// user can get it back. Missing a real drop loses work silently; reporting a
// drop that never happened trains people to ignore the warning.

func edited(id, title string, at time.Time) todo.Todo {
	task := todo.New(title)
	task.ID = id
	task.ModifiedAt = at
	return task
}

func TestDroppedLocalEdits(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	since := base                    // last successful sync
	localEdit := base.Add(time.Hour) // edited here afterwards
	remoteEdit := base.Add(2 * time.Hour)

	t.Run("a local edit overwritten by the merge is reported", func(t *testing.T) {
		local := []todo.Todo{edited("a", "my version", localEdit)}
		merged := []todo.Todo{edited("a", "their version", remoteEdit)}
		got := DroppedLocalEdits(local, merged, since)
		if len(got) != 1 || got[0].Local.Title != "My version" || len(got[0].Units) != 1 || got[0].Units[0] != "title" {
			t.Fatalf("dropped = %+v, want the local version with its title", got)
		}
	})

	t.Run("a task untouched since the last sync is not a conflict", func(t *testing.T) {
		// The local copy differs from the merged one, but only because someone
		// else's edit arrived — nothing of ours was lost. Reporting this was
		// the bug that made the log nag on every inbound change.
		local := []todo.Todo{edited("a", "old", base.Add(-time.Hour))}
		merged := []todo.Todo{edited("a", "their version", remoteEdit)}
		if got := DroppedLocalEdits(local, merged, since); len(got) != 0 {
			t.Errorf("dropped = %+v, want nothing", got)
		}
	})

	t.Run("an identical scalar set is not a conflict", func(t *testing.T) {
		local := []todo.Todo{edited("a", "same", localEdit)}
		same := local[0]
		same.ModifiedAt = remoteEdit // the merge kept our content
		if got := DroppedLocalEdits(local, []todo.Todo{same}, since); len(got) != 0 {
			t.Errorf("dropped = %+v, want nothing — only the timestamp moved", got)
		}
	})

	t.Run("an edit that lost to a remote delete is reported", func(t *testing.T) {
		local := []todo.Todo{edited("a", "still working on it", localEdit)}
		tomb := edited("a", "gone", base)
		tomb.Deleted = true
		tomb.DeletedAt = base.Add(30 * time.Minute) // deleted before our edit
		got := DroppedLocalEdits(local, []todo.Todo{tomb}, since)
		if len(got) != 1 {
			t.Fatalf("dropped = %+v, want the edit that lost to the delete", got)
		}
	})

	t.Run("a plain remote delete is not a conflict", func(t *testing.T) {
		// Deleted after our last edit: the deletion is simply propagating.
		local := []todo.Todo{edited("a", "done with it", localEdit)}
		tomb := edited("a", "gone", base)
		tomb.Deleted = true
		tomb.DeletedAt = localEdit.Add(time.Minute)
		if got := DroppedLocalEdits(local, []todo.Todo{tomb}, since); len(got) != 0 {
			t.Errorf("dropped = %+v, want nothing — the delete came after our edit", got)
		}
	})

	t.Run("our own tombstones are never conflicts", func(t *testing.T) {
		local := []todo.Todo{edited("a", "we deleted it", localEdit)}
		local[0].Deleted = true
		merged := []todo.Todo{edited("a", "their version", remoteEdit)}
		if got := DroppedLocalEdits(local, merged, since); len(got) != 0 {
			t.Errorf("dropped = %+v, want nothing", got)
		}
	})

	t.Run("a task the merge never returned is skipped", func(t *testing.T) {
		local := []todo.Todo{edited("ghost", "only here", localEdit)}
		if got := DroppedLocalEdits(local, nil, since); len(got) != 0 {
			t.Errorf("dropped = %+v, want nothing to report for an absent task", got)
		}
	})

	t.Run("with no sync recorded yet, over-log rather than miss one", func(t *testing.T) {
		local := []todo.Todo{edited("a", "mine", base.Add(-time.Hour))}
		merged := []todo.Todo{edited("a", "theirs", remoteEdit)}
		if got := DroppedLocalEdits(local, merged, time.Time{}); len(got) != 1 {
			t.Errorf("dropped = %+v, want the edit logged when there is no baseline", got)
		}
	})
}

// With stamps, an edit is only lost where both devices set the same unit:
// the local description beside another device's later priority is kept by
// the merge, and nothing is reported. A tag removed here and added back
// later elsewhere is reported as that tag alone.
func TestDroppedLocalEditsAreUnitsBothSidesSet(t *testing.T) {
	since := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	here, there := hlc.New("here", ""), hlc.New("there", "")
	s1 := here.Now(since.Add(time.Hour))
	s2 := there.Now(since.Add(2 * time.Hour))

	local := todo.New("rent")
	local.ID = "a"
	local.Notes = "mine"
	local.Stamps = map[string]hlc.Stamp{"notes": s1, "priority": hlc.At(since), todo.TagKey("home"): s1}

	remote := local
	remote.Notes = ""
	remote.Priority = todo.PriorityHigh
	remote.Tags = []string{"home"}
	remote.Stamps = map[string]hlc.Stamp{"notes": hlc.At(since), "priority": s2, todo.TagKey("home"): s2}

	merged := Merge([]todo.Todo{remote}, []todo.Todo{local})
	got := DroppedLocalEdits([]todo.Todo{local}, merged, since)
	if len(got) != 1 || len(got[0].Units) != 1 || got[0].Units[0] != todo.TagKey("home") {
		t.Fatalf("dropped = %+v, want only the removed tag the other device re-added later", got)
	}
	if merged[0].Notes != "mine" || merged[0].Priority != todo.PriorityHigh {
		t.Errorf("merge kept notes %q priority %v; want both sides' edits", merged[0].Notes, merged[0].Priority)
	}
}

// ── The two warnings the client shows ─────────────────────────────────────────

func TestClockSkewWarning(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if got := ClockSkewWarning(time.Time{}, now); got != "" {
		t.Errorf("a server with no clock reading should not warn, got %q", got)
	}
	if got := ClockSkewWarning(now.Add(-time.Minute), now); got != "" {
		t.Errorf("ordinary drift should not warn, got %q", got)
	}
	for _, skew := range []time.Duration{2 * time.Hour, -2 * time.Hour} {
		got := ClockSkewWarning(now.Add(skew), now)
		if got == "" {
			t.Errorf("a %s skew should warn in both directions", skew)
		}
		if !strings.Contains(got, "clock") {
			t.Errorf("warning = %q, want it to name the problem", got)
		}
	}
}

func TestInsecureURLWarning(t *testing.T) {
	quiet := []string{
		"https://example.com",         // encrypted
		"http://localhost:8765",       // loopback by name
		"http://127.0.0.1:8765",       // loopback by address
		"http://192.168.1.10:8765",    // RFC1918
		"http://10.1.2.3:8765",        // RFC1918
		"http://100.101.102.103:8765", // Tailscale CGNAT
		"http://my-box.ts.net:8765",   // Tailscale name
		"",                            // unset: reachability errors surface later
		"://nonsense",                 // unparseable
	}
	for _, u := range quiet {
		if got := InsecureURLWarning(u); got != "" {
			t.Errorf("InsecureURLWarning(%q) = %q, want silence", u, got)
		}
	}
	loud := []string{"http://example.com:8765", "http://203.0.113.7:8765"}
	for _, u := range loud {
		got := InsecureURLWarning(u)
		if got == "" {
			t.Errorf("InsecureURLWarning(%q) said nothing — the token travels in cleartext", u)
		}
		if !strings.Contains(got, u) {
			t.Errorf("warning = %q, want it to name the URL", got)
		}
	}
}
