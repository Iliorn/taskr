package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"taskr/todo"
)

func newTestServer(t *testing.T) (*syncServer, *httptest.Server) {
	t.Helper()
	srv := &syncServer{db: openTestDB(t), token: "tok"}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sync", srv.handleSync)
	mux.HandleFunc("/v1/health", srv.handleHealth)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestSyncHandlerAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	const body = `{"tasks":[]}`

	// No token → 401.
	w := httptest.NewRecorder()
	srv.handleSync(w, httptest.NewRequest(http.MethodPost, "/v1/sync", strings.NewReader(body)))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing token: want 401, got %d", w.Code)
	}

	// Wrong token → 401.
	r := httptest.NewRequest(http.MethodPost, "/v1/sync", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer nope")
	w = httptest.NewRecorder()
	srv.handleSync(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: want 401, got %d", w.Code)
	}

	// Right token → 200.
	r = httptest.NewRequest(http.MethodPost, "/v1/sync", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer tok")
	w = httptest.NewRecorder()
	srv.handleSync(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("right token: want 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestSyncClientServerRoundTrip(t *testing.T) {
	srv, ts := newTestServer(t)

	// Server starts with task A.
	a := todo.New("from server")
	saveTodos(t, srv.db, []todo.Todo{a})

	// Client starts with a different task B.
	ch := openTestDB(t)
	b := todo.New("from client")
	saveTodos(t, ch, []todo.Todo{b})

	cfg := syncConfig{URL: ts.URL, Token: "tok"}
	sum, err := runClientSync(ch, cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("client sync: %v", err)
	}
	if sum.received != 2 {
		t.Errorf("expected 2 tasks back, got %d", sum.received)
	}

	// Both sides now hold both tasks.
	clientLive, _ := loadTodosFromDB(ch)
	serverLive, _ := loadTodosFromDB(srv.db)
	if len(clientLive) != 2 {
		t.Errorf("client should have 2 tasks, got %d", len(clientLive))
	}
	if len(serverLive) != 2 {
		t.Errorf("server should have 2 tasks, got %d", len(serverLive))
	}
}

func TestSyncServerPropagatesDeletion(t *testing.T) {
	srv, ts := newTestServer(t)

	a := todo.New("doomed")
	saveTodos(t, srv.db, []todo.Todo{a})

	// Client holds the same task but as a tombstone deleted after the server's edit.
	ch := openTestDB(t)
	saveTodos(t, ch, []todo.Todo{a})
	tomb := a
	tomb.Deleted = true
	tomb.DeletedAt = a.ModifiedAt.Add(time.Hour)
	saveTodos(t, ch, []todo.Todo{tomb})

	cfg := syncConfig{URL: ts.URL, Token: "tok"}
	if _, err := runClientSync(ch, cfg, 5*time.Second); err != nil {
		t.Fatalf("client sync: %v", err)
	}

	serverLive, _ := loadTodosFromDB(srv.db)
	if len(serverLive) != 0 {
		t.Errorf("deletion should propagate to server, still %d live", len(serverLive))
	}
	serverAll, _ := loadTodosForSync(srv.db)
	if len(serverAll) != 1 || !serverAll[0].Deleted {
		t.Errorf("server should retain the tombstone, got %+v", serverAll)
	}
}

// TestSyncMergesChildCollections drives a concurrent comment add on the *same*
// task through the full client↔server round trip (SQLite encode/decode + HTTP +
// merge + write-back), not just the in-memory Merge. Both sides must end up
// holding the union of the two comments. This guards the child-collection path
// that the recent merge fixes touched, where a child tombstone or row could be
// lost across the storage boundary rather than in the merge function itself.
func TestSyncMergesChildCollections(t *testing.T) {
	srv, ts := newTestServer(t)

	// Same task ID on both sides, each with a different comment added offline.
	sx := mkTask("x", "server wording", at(time.Hour))
	sx.Comments = []todo.Comment{mkComment("c1", "from server", at(0))}
	saveTodos(t, srv.db, []todo.Todo{sx})

	ch := openTestDB(t)
	cx := mkTask("x", "client wording", at(0)) // older scalar, loses LWW
	cx.Comments = []todo.Comment{mkComment("c2", "from client", at(0))}
	saveTodos(t, ch, []todo.Todo{cx})

	cfg := syncConfig{URL: ts.URL, Token: "tok"}
	if _, err := runClientSync(ch, cfg, 5*time.Second); err != nil {
		t.Fatalf("client sync: %v", err)
	}

	// Both stores converge: server wording wins the scalar (newer), but the two
	// comments union — neither side's offline note is lost.
	for _, tc := range []struct {
		name string
		db   *sql.DB
	}{{"client", ch}, {"server", srv.db}} {
		live, _ := loadTodosFromDB(tc.db)
		got := indexByID(live)
		x, ok := got["x"]
		if !ok {
			t.Fatalf("%s: task x missing after sync", tc.name)
		}
		if x.Title != "server wording" {
			t.Errorf("%s: Title = %q, want server wording", tc.name, x.Title)
		}
		ids := map[string]bool{}
		for _, c := range x.Comments {
			ids[c.ID] = true
		}
		if len(x.Comments) != 2 || !ids["c1"] || !ids["c2"] {
			t.Errorf("%s: want comments c1+c2 unioned, got %+v", tc.name, x.Comments)
		}
	}
}

// TestSyncMultiClientConvergence checks that two clients syncing in sequence
// against one server all converge to the union of every task — i.e. the second
// client's sync doesn't clobber the first client's contribution, and a client
// that synced before the others later picks up everything.
func TestSyncMultiClientConvergence(t *testing.T) {
	srv, ts := newTestServer(t)
	cfg := syncConfig{URL: ts.URL, Token: "tok"}

	// Server starts with A; client 1 brings B; client 2 brings C.
	saveTodos(t, srv.db, []todo.Todo{mkTask("a", "A", at(0))})

	c1 := openTestDB(t)
	saveTodos(t, c1, []todo.Todo{mkTask("b", "B", at(0))})
	c2 := openTestDB(t)
	saveTodos(t, c2, []todo.Todo{mkTask("c", "C", at(0))})

	// Client 1 syncs: server gains B; client 1 gains A.
	if _, err := runClientSync(c1, cfg, 5*time.Second); err != nil {
		t.Fatalf("client 1 sync: %v", err)
	}
	// Client 2 syncs: server gains C; client 2 gains A and B.
	if _, err := runClientSync(c2, cfg, 5*time.Second); err != nil {
		t.Fatalf("client 2 sync: %v", err)
	}
	// Client 1 syncs again: now picks up C too.
	if _, err := runClientSync(c1, cfg, 5*time.Second); err != nil {
		t.Fatalf("client 1 re-sync: %v", err)
	}

	want := map[string]bool{"a": true, "b": true, "c": true}
	for _, tc := range []struct {
		name string
		db   *sql.DB
	}{{"server", srv.db}, {"client 1", c1}, {"client 2", c2}} {
		live, _ := loadTodosFromDB(tc.db)
		got := indexByID(live)
		if len(got) != len(want) {
			t.Errorf("%s: have %d tasks, want %d (%v)", tc.name, len(got), len(want), got)
		}
		for id := range want {
			if _, ok := got[id]; !ok {
				t.Errorf("%s: missing task %q after convergence", tc.name, id)
			}
		}
	}
}

func TestSyncConflictLogged(t *testing.T) {
	srv, ts := newTestServer(t)

	// Server has the authoritative (newer) version of task X.
	x := todo.New("server wording")
	x.ModifiedAt = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	saveTodos(t, srv.db, []todo.Todo{x})

	// Client has an older edit of the same task — it will lose LWW.
	ch := openTestDB(t)
	stale := x
	stale.Title = "client wording"
	stale.ModifiedAt = x.ModifiedAt.Add(-time.Hour)
	saveTodos(t, ch, []todo.Todo{stale})

	logPath := syncLogPath()
	_ = os.Remove(logPath)

	cfg := syncConfig{URL: ts.URL, Token: "tok"}
	sum, err := runClientSync(ch, cfg, 5*time.Second)
	if err != nil {
		t.Fatalf("client sync: %v", err)
	}
	if sum.conflicts != 1 {
		t.Fatalf("expected 1 conflict, got %d", sum.conflicts)
	}

	// Client converges to the server's wording…
	live, _ := loadTodosFromDB(ch)
	if len(live) != 1 || live[0].Title != "Server wording" {
		t.Fatalf("client should adopt server wording, got %+v", live)
	}
	// …and the dropped local edit is in the recovery log.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read sync log: %v", err)
	}
	if !strings.Contains(string(data), "client wording") {
		t.Errorf("sync log should record the dropped local edit, got: %s", data)
	}
}

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
	if !tasks[1].ModifiedAt.Equal(slightlyAhead) {
		t.Errorf("within-allowance ModifiedAt = %v, want untouched %v", tasks[1].ModifiedAt, slightlyAhead)
	}
}
