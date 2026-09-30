package app

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/rank"
	"github.com/Iliorn/tjek/tasksync"
	"github.com/Iliorn/tjek/todo"
)

// Two devices edit different fields of one task before either syncs: the
// laptop sets its due date, the desktop, a little later, its priority. Both
// edits must end up on the server and on both devices, whichever device syncs
// first: each field carries its own stamp, and the merge keeps each from the
// later edit of it.
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

			now := time.Now()
			task := todo.New("Pay rent")
			stampSave(t, devices["laptop"], now, []todo.Todo{task})
			sync("laptop")
			sync("desktop")

			due := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
			edit := func(name string, change func(*todo.Todo), at time.Time) {
				t.Helper()
				x := taskIn(t, devices[name], task.ID)
				change(&x)
				x.ModifiedAt = at
				stampSave(t, devices[name], at, []todo.Todo{x})
			}
			edit("laptop", func(x *todo.Todo) { x.DueDate = due }, now.Add(time.Second))
			edit("desktop", func(x *todo.Todo) { x.Priority = todo.PriorityHigh }, now.Add(2*time.Second))

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

	now := time.Now()
	task := todo.New("Pay rent")
	stampSave(t, laptop, now, []todo.Todo{task})
	sync(laptop)
	sync(desktop)

	l := taskIn(t, laptop, task.ID)
	l.Title, l.Project, l.ModifiedAt = "Pay rent today", "flat", now.Add(time.Second)
	stampSave(t, laptop, l.ModifiedAt, []todo.Todo{l})
	d := taskIn(t, desktop, task.ID)
	d.Title, d.ModifiedAt = "Pay the rent", now.Add(2*time.Second)
	stampSave(t, desktop, d.ModifiedAt, []todo.Todo{d})

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

// A fleet at random: three devices with clocks up to a minute apart edit the
// same few tasks through the app's save and sync with one server in random
// order. Once everyone has synced, every device holds exactly the server's
// set, and each field holds the value of the last edit made to it, by stamp:
// no edit is lost except to a later edit of the same field.
func TestSyncFleetConvergesOnTheLastEditOfEachField(t *testing.T) {
	type fieldEdit struct {
		stamp hlc.Stamp
		value string
	}
	for seed := int64(1); seed <= 25; seed++ {
		r := rand.New(rand.NewSource(seed))
		srv, ts := newTestServer(t)
		cfg := syncConfig{URL: ts.URL, Token: "tok"}
		names := []string{"laptop", "desktop", "work"}
		devices := map[string]*sql.DB{}
		homes := map[string]string{}
		skew := map[string]time.Duration{}
		for _, n := range names {
			devices[n], homes[n] = openTestDB(t), t.TempDir()
			skew[n] = time.Duration(r.Intn(120)-60) * time.Second
		}
		sync := func(n string) {
			t.Helper()
			setTestHome(t, homes[n])
			if _, err := runClientSync(devices[n], cfg, 5*time.Second, rank.DefaultBiases(), nil); err != nil {
				t.Fatalf("seed %d: %s sync: %v", seed, n, err)
			}
		}

		start := time.Now()
		var ids []string
		for i := 0; i < 3; i++ {
			task := todo.New(fmt.Sprintf("task %d", i))
			stampSave(t, devices["laptop"], start, []todo.Todo{task})
			ids = append(ids, task.ID)
		}
		for _, n := range names {
			sync(n)
		}

		last := map[string]fieldEdit{} // id/field → the latest edit of it
		for step := 0; step < 30; step++ {
			n := names[r.Intn(len(names))]
			if r.Intn(3) == 0 {
				sync(n)
				continue
			}
			id := ids[r.Intn(len(ids))]
			x := taskIn(t, devices[n], id)
			now := start.Add(time.Duration(step)*time.Second + skew[n])
			var field, value string
			switch r.Intn(3) {
			case 0:
				field, value = "title", fmt.Sprintf("Title %s %d", n, step)
				x.Title = value
			case 1:
				field, value = "notes", fmt.Sprintf("notes %s %d", n, step)
				x.Notes = value
			case 2:
				p := todo.Priority(r.Intn(3))
				field, value = "priority", fmt.Sprint(p)
				x.Priority = p
			}
			x.ModifiedAt = now
			stampSave(t, devices[n], now, []todo.Todo{x})
			stamp := storedStamps(t, devices[n], id)[field]
			if k := id + "/" + field; stamp.After(last[k].stamp) {
				last[k] = fieldEdit{stamp, value}
			}
		}
		for round := 0; round < 2; round++ {
			for _, n := range names {
				sync(n)
			}
		}

		want, err := loadTodosForSync(srv.db)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			got, err := loadTodosForSync(devices[n])
			if err != nil {
				t.Fatal(err)
			}
			if tasksync.StoreDigest(got) != tasksync.StoreDigest(want) {
				t.Fatalf("seed %d: %s did not converge on the server's set", seed, n)
			}
		}
		for k, e := range last {
			id, field, _ := strings.Cut(k, "/")
			x := taskIn(t, srv.db, id)
			have := map[string]string{"title": x.Title, "notes": x.Notes, "priority": fmt.Sprint(x.Priority)}[field]
			if have != e.value {
				t.Errorf("seed %d: task %s %s = %q, want the last edit's %q", seed, id[:8], field, have, e.value)
			}
		}
	}
}

// An edit made after seeing another device's edit wins, even on a device
// whose clock is a minute behind: the merge moves the device's clock past
// every stamp it brings in. Ordered by wall clocks, the slow device's newer
// title would lose to the one it replaced.
func TestSyncAnEditAfterSeeingAnotherWinsOnASlowClock(t *testing.T) {
	_, ts := newTestServer(t)
	cfg := syncConfig{URL: ts.URL, Token: "tok"}
	fast, slow := openTestDB(t), openTestDB(t)
	homes := map[*sql.DB]string{fast: t.TempDir(), slow: t.TempDir()}
	sync := func(h *sql.DB) {
		t.Helper()
		setTestHome(t, homes[h])
		if _, err := runClientSync(h, cfg, 5*time.Second, rank.DefaultBiases(), nil); err != nil {
			t.Fatalf("sync: %v", err)
		}
	}
	now := time.Now()
	task := todo.New("Pay rent")
	stampSave(t, fast, now, []todo.Todo{task})
	sync(fast)
	sync(slow)

	f := taskIn(t, fast, task.ID)
	f.Title = "Fast title"
	stampSave(t, fast, now.Add(time.Minute), []todo.Todo{f})
	sync(fast)
	sync(slow) // the slow device sees the fast one's title

	s := taskIn(t, slow, task.ID)
	s.Title = "Slow title, made later"
	stampSave(t, slow, now.Add(-time.Minute), []todo.Todo{s})
	sync(slow)
	sync(fast)

	for name, h := range map[string]*sql.DB{"fast": fast, "slow": slow} {
		if got := taskIn(t, h, task.ID).Title; got != "Slow title, made later" {
			t.Errorf("%s: title %q, want the edit made after seeing the other", name, got)
		}
	}
}
