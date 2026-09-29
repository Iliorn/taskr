package main

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Iliorn/tjek/rank"
	"github.com/Iliorn/tjek/todo"
)

// Two devices edit different fields of one task before either syncs: the
// laptop sets its due date, the desktop, a little later, its priority. Both
// edits must end up on the server and on both devices, whichever device syncs
// first. Merge alone keeps the later edit's task whole, so the laptop's due
// date was lost in both orders: overwritten on the server when the desktop
// pushed second, and on the laptop when it pulled second.
func TestSyncKeepsTwoDevicesEditsToOneTask(t *testing.T) {
	for _, c := range []struct {
		name  string
		order []string // devices in sync order after the edits
	}{
		{"earlier edit syncs first", []string{"laptop", "desktop", "laptop"}},
		{"later edit syncs first", []string{"desktop", "laptop", "desktop"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			setTestHome(t, t.TempDir())
			srv, ts := newTestServer(t)
			devices := map[string]*sql.DB{"laptop": openTestDB(t), "desktop": openTestDB(t)}
			sync := func(name string) {
				t.Helper()
				cfg := syncConfig{URL: ts.URL, Token: "tok"}
				if _, err := runClientSync(devices[name], cfg, 5*time.Second, rank.DefaultBiases(), nil); err != nil {
					t.Fatalf("%s sync: %v", name, err)
				}
			}

			task := todo.New("Pay rent")
			saveTodos(t, devices["laptop"], []todo.Todo{task})
			sync("laptop")
			sync("desktop")
			for name, h := range devices {
				if pull, err := hasUnsyncedEdits(h); err != nil || pull {
					t.Fatalf("%s: unsynced edits right after a sync (%v, %v), so every sync would pull first", name, pull, err)
				}
			}

			due := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
			edit := func(name string, change func(*todo.Todo), at time.Time) {
				t.Helper()
				x := taskIn(t, devices[name], task.ID)
				change(&x)
				x.ModifiedAt = at
				saveTodos(t, devices[name], []todo.Todo{x})
			}
			now := time.Now()
			edit("laptop", func(x *todo.Todo) { x.DueDate = due }, now)
			edit("desktop", func(x *todo.Todo) { x.Priority = todo.PriorityHigh }, now.Add(time.Second))

			for _, name := range c.order {
				sync(name)
			}

			for name, h := range map[string]*sql.DB{"server": srv.db, "laptop": devices["laptop"], "desktop": devices["desktop"]} {
				got := taskIn(t, h, task.ID)
				if !got.DueDate.Equal(due) || got.Priority != todo.PriorityHigh {
					t.Errorf("%s: due %v, priority %v; want the laptop's due date and the desktop's priority",
						name, got.DueDate, got.Priority)
				}
			}
		})
	}
}

// A field both devices changed is still a conflict: the later edit wins it,
// the earlier device's version goes to the recovery log, and a field only one
// of them changed is kept beside it.
func TestSyncLeavesAFieldBothDevicesChangedToTheLaterEdit(t *testing.T) {
	_, ts := newTestServer(t)
	laptop, desktop := openTestDB(t), openTestDB(t)
	// Each device keeps its own sync state (the last-sync baseline the
	// recovery log reads), as separate machines do.
	homes := map[*sql.DB]string{laptop: t.TempDir(), desktop: t.TempDir()}
	cfg := syncConfig{URL: ts.URL, Token: "tok"}
	syncSum := func(h *sql.DB) syncSummary {
		t.Helper()
		setTestHome(t, homes[h])
		sum, err := runClientSync(h, cfg, 5*time.Second, rank.DefaultBiases(), nil)
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		return sum
	}
	sync := func(h *sql.DB) { t.Helper(); syncSum(h) }

	task := todo.New("Pay rent")
	saveTodos(t, laptop, []todo.Todo{task})
	sync(laptop)
	sync(desktop)

	now := time.Now()
	l := taskIn(t, laptop, task.ID)
	l.Title, l.Project, l.ModifiedAt = "Pay rent today", "flat", now
	saveTodos(t, laptop, []todo.Todo{l})
	d := taskIn(t, desktop, task.ID)
	d.Title, d.ModifiedAt = "Pay the rent", now.Add(time.Second)
	saveTodos(t, desktop, []todo.Todo{d})

	sync(desktop)
	sum := syncSum(laptop)
	got := taskIn(t, laptop, task.ID)
	if got.Title != "Pay the rent" || got.Project != "flat" {
		t.Errorf("title %q project %q; want the later title and the laptop's project", got.Title, got.Project)
	}
	if sum.conflicts != 1 {
		t.Errorf("conflicts = %d, want the laptop's title reported as the one lost edit", sum.conflicts)
	}
}

func taskIn(t *testing.T, h *sql.DB, id string) todo.Todo {
	t.Helper()
	all, err := loadTodosFromDB(h)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, x := range all {
		if x.ID == id {
			return x
		}
	}
	t.Fatalf("task %s missing", id)
	return todo.Todo{}
}
