package main

import (
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
