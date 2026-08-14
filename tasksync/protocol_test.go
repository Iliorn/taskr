package tasksync

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"taskr/todo"
)

// protocol_test.go covers the wire: the /v1/sync round trip, authentication,
// the SSE stream, and the conflict detection that feeds sync.log. The merge
// fold was already well covered; the transport around it was not, and it is
// the half where a bug loses user data rather than mis-ranking a list.

// fakeStore is a Store that folds with the real Merge, so a round trip through
// the handler exercises the same semantics the app gets.
type fakeStore struct {
	tasks   []todo.Todo
	err     error
	calls   int
	changed bool
}

func (f *fakeStore) MergeIn(incoming []todo.Todo) ([]todo.Todo, bool, error) {
	f.calls++
	if f.err != nil {
		return nil, false, f.err
	}
	merged := Merge(f.tasks, incoming)
	changed := StoreDigest(merged) != StoreDigest(f.tasks)
	f.tasks = merged
	f.changed = changed
	return merged, changed, nil
}

func testServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs
}

func newTask(title string, modified time.Time) todo.Todo {
	task := todo.New(title)
	task.ModifiedAt = modified
	return task
}

// The round trip: a client pushes a task the server has never seen, gets the
// merged authoritative set back, and the server keeps it.
func TestPostSyncRoundTrip(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	store := &fakeStore{tasks: []todo.Todo{newTask("on the server", now)}}
	srv := &Server{Token: "s3cret", Store: store}
	hs := testServer(t, srv)

	local := newTask("from the client", now)
	resp, err := PostSync(hs.URL, "s3cret", []todo.Todo{local}, 5*time.Second)
	if err != nil {
		t.Fatalf("PostSync: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("merged set has %d tasks, want both", len(resp.Tasks))
	}
	titles := map[string]bool{}
	for _, task := range resp.Tasks {
		titles[task.Title] = true
	}
	if !titles["On the server"] || !titles["From the client"] {
		t.Errorf("merged titles = %v, want both sides", titles)
	}
	// ServerTime is what the clock-skew check reads; a zero value disables it
	// silently, so the server must actually send one.
	if resp.ServerTime.IsZero() {
		t.Error("response carried no ServerTime")
	}
	if store.calls != 1 {
		t.Errorf("store merged %d times, want 1", store.calls)
	}

	// A second, identical push must not report a change — that is what stops
	// idle syncs from broadcasting each other in a loop.
	if _, err := PostSync(hs.URL, "s3cret", []todo.Todo{local}, 5*time.Second); err != nil {
		t.Fatalf("second PostSync: %v", err)
	}
	if store.changed {
		t.Error("an unchanged re-push reported the store as changed")
	}
}

// A trailing slash on the configured URL is the kind of thing users type.
func TestPostSyncTrimsTrailingSlash(t *testing.T) {
	store := &fakeStore{}
	hs := testServer(t, &Server{Token: "t", Store: store})
	if _, err := PostSync(hs.URL+"/", "t", nil, 5*time.Second); err != nil {
		t.Fatalf("PostSync with a trailing slash: %v", err)
	}
	if store.calls != 1 {
		t.Error("the request did not reach /v1/sync")
	}
}

func TestSyncRejectsBadAuth(t *testing.T) {
	store := &fakeStore{}
	hs := testServer(t, &Server{Token: "right", Store: store})

	for _, c := range []struct {
		name, header string
	}{
		{"no header", ""},
		{"wrong token", "Bearer wrong"},
		{"no bearer prefix", "right"},
		{"lowercase prefix", "bearer right"},
		{"empty token", "Bearer "},
	} {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, hs.URL+"/v1/sync", strings.NewReader(`{"tasks":[]}`))
			if err != nil {
				t.Fatal(err)
			}
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %s, want 401", resp.Status)
			}
		})
	}
	if store.calls != 0 {
		t.Errorf("an unauthorized request reached the store %d times", store.calls)
	}

	// And the client surfaces the rejection rather than reporting an empty sync.
	if _, err := PostSync(hs.URL, "wrong", nil, 5*time.Second); err == nil {
		t.Error("PostSync with a bad token returned no error")
	}
}

func TestSyncRejectsBadRequests(t *testing.T) {
	store := &fakeStore{}
	hs := testServer(t, &Server{Token: "t", Store: store})

	// Wrong method.
	resp, err := http.Get(hs.URL + "/v1/sync")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/sync = %s, want 405", resp.Status)
	}

	// Malformed body, correctly authenticated: a decode failure must be a 400,
	// never a successful merge of nothing.
	req, _ := http.NewRequest(http.MethodPost, hs.URL+"/v1/sync", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer t")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body = %s, want 400", resp.Status)
	}
	if store.calls != 0 {
		t.Error("a malformed request still reached the store")
	}
}

// A store error must surface as a 500 and leave the client with an error, not
// an empty task set it might then treat as authoritative.
func TestSyncStoreErrorSurfaces(t *testing.T) {
	store := &fakeStore{err: fmt.Errorf("disk on fire")}
	hs := testServer(t, &Server{Token: "t", Store: store})

	_, err := PostSync(hs.URL, "t", []todo.Todo{newTask("x", time.Now())}, 5*time.Second)
	if err == nil {
		t.Fatal("a failing store returned no error to the client")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to carry the status", err)
	}
}

func TestHealthEndpoint(t *testing.T) {
	hs := testServer(t, &Server{Token: "t", Store: &fakeStore{}})
	resp, err := http.Get(hs.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health = %s, want 200", resp.Status)
	}
	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body.String()) != "ok" {
		t.Errorf("health body = %q", body.String())
	}
	// Health is the liveness probe — it must not require the token, or a
	// monitor would need the secret to check the process is up.
}

// OnClientSync is what `sync --status` reads; it has to fire for a real sync.
func TestOnClientSyncFires(t *testing.T) {
	var got time.Time
	srv := &Server{Token: "t", Store: &fakeStore{}, OnClientSync: func(at time.Time) { got = at }}
	hs := testServer(t, srv)
	if _, err := PostSync(hs.URL, "t", nil, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if got.IsZero() {
		t.Error("OnClientSync never fired for an authenticated sync")
	}
}

// ── The SSE stream ────────────────────────────────────────────────────────────

// readSSE reads server-sent events off a live stream until it sees want or the
// deadline passes.
func readSSE(t *testing.T, body *bufio.Reader, want string, within time.Duration) bool {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		for {
			line, err := body.ReadString('\n')
			if err != nil {
				done <- false
				return
			}
			if strings.Contains(line, want) {
				done <- true
				return
			}
		}
	}()
	select {
	case ok := <-done:
		return ok
	case <-time.After(within):
		return false
	}
}

// A change on the server has to reach a connected client as an event — that is
// the whole point of the stream, and nothing else in the protocol tells a
// client to pull promptly.
func TestEventsStreamDeliversChanges(t *testing.T) {
	hub := NewHub()
	store := &fakeStore{}
	srv := &Server{Token: "t", Store: store, Hub: hub}
	hs := testServer(t, srv)

	req, _ := http.NewRequest(http.MethodGet, hs.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events = %s, want 200", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want an event stream", ct)
	}
	body := bufio.NewReader(resp.Body)
	if !readSSE(t, body, ": ok", 2*time.Second) {
		t.Fatal("the stream never opened with its priming comment")
	}

	// Wait for the subscription to register, then push a change through the
	// real sync path.
	deadline := time.Now().Add(2 * time.Second)
	for hub.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hub.SubscriberCount() != 1 {
		t.Fatalf("hub has %d subscribers, want the connected client", hub.SubscriberCount())
	}
	if _, err := PostSync(hs.URL, "t", []todo.Todo{newTask("new work", time.Now().UTC())}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if !readSSE(t, body, "event: changed", 3*time.Second) {
		t.Error("a store change did not reach the connected client")
	}

	// Disconnecting must free the slot; leaking subscribers would eventually
	// hit the hub's capacity and start refusing clients.
	resp.Body.Close()
	for i := 0; i < 200 && hub.SubscriberCount() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.SubscriberCount() != 0 {
		t.Error("the subscriber slot was not released on disconnect")
	}
}

func TestEventsRequiresAuthAndHub(t *testing.T) {
	hs := testServer(t, &Server{Token: "t", Store: &fakeStore{}, Hub: NewHub()})
	resp, err := http.Get(hs.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated events = %s, want 401", resp.Status)
	}

	// Without a hub there is nothing to stream, and the server must say so
	// rather than hold the connection open forever.
	noHub := testServer(t, &Server{Token: "t", Store: &fakeStore{}})
	req, _ := http.NewRequest(http.MethodGet, noHub.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer t")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("events without a hub = %s, want 503", resp.Status)
	}
}

// The client half of the stream: the listener turns events into nudges on C,
// and Close ends it.
func TestListenerReceivesNudges(t *testing.T) {
	hub := NewHub()
	hs := testServer(t, &Server{Token: "t", Store: &fakeStore{}, Hub: hub})

	ls := StartListener(hs.URL, "t")
	defer ls.Close()

	deadline := time.Now().Add(3 * time.Second)
	for hub.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hub.SubscriberCount() == 0 {
		t.Fatal("the listener never connected")
	}

	hub.Broadcast()
	select {
	case <-ls.C:
	case <-time.After(3 * time.Second):
		t.Fatal("the listener did not surface a broadcast as a nudge")
	}

	ls.Close()
	select {
	case <-ls.Done():
	case <-time.After(2 * time.Second):
		t.Error("Close did not signal Done")
	}
	// Close is idempotent — the TUI calls it on teardown and on config change.
	ls.Close()
}

// A listener pointed at a server that rejects it must not spin: it backs off,
// and Close still ends it promptly.
func TestListenerStopsWhileReconnecting(t *testing.T) {
	hs := testServer(t, &Server{Token: "right", Store: &fakeStore{}, Hub: NewHub()})
	ls := StartListener(hs.URL, "wrong")
	time.Sleep(100 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		ls.Close()
		<-ls.Done()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("Close did not stop a reconnecting listener")
	}
}

// ── Wire version negotiation ────────────────────────────────────────────────

func TestNegotiateAcceptsCurrentAndLegacy(t *testing.T) {
	if err := negotiate(ProtocolVersion); err != nil {
		t.Errorf("current version rejected: %v", err)
	}
	// Zero is a client from before the field existed. It speaks v1, and
	// treating it as an error would break every deployed client at once.
	if err := negotiate(0); err != nil {
		t.Errorf("absent version must be accepted as v1: %v", err)
	}
}

func TestNegotiateRejectsNewerPeer(t *testing.T) {
	err := negotiate(ProtocolVersion + 1)
	if err == nil {
		t.Fatal("a newer peer must be refused, not decoded")
	}
	// The message has to name which side to upgrade, or the user is left
	// with a mismatch and no next step.
	if !strings.Contains(err.Error(), "upgrade this end") {
		t.Errorf("error should say which side is behind, got %q", err)
	}
}

func TestNegotiateRejectsUnsupportedOlderPeer(t *testing.T) {
	if MinProtocolVersion <= 1 {
		t.Skip("nothing below the floor to test while MinProtocolVersion is 1")
	}
	err := negotiate(MinProtocolVersion - 1)
	if err == nil {
		t.Fatal("a peer below the floor must be refused")
	}
}

func TestServerRefusesNewerClientWire(t *testing.T) {
	store := &fakeStore{}
	hs := testServer(t, &Server{Token: "t", Store: store})

	body, err := json.Marshal(Request{Protocol: ProtocolVersion + 1})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, hs.URL+"/v1/sync", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %s, want 409 Conflict", resp.Status)
	}
	// The merge must not have run: refusing after folding the payload in
	// would defeat the point of checking at all.
	if store.calls != 0 {
		t.Errorf("store was merged into %d times despite the version refusal", store.calls)
	}
}

func TestServerAcceptsVersionlessClient(t *testing.T) {
	// The compatibility guarantee, pinned: a client that predates the "v"
	// field must still sync.
	hs := testServer(t, &Server{Token: "t", Store: &fakeStore{}})

	body := []byte(`{"tasks":[]}`)
	req, err := http.NewRequest(http.MethodPost, hs.URL+"/v1/sync", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %s, want 200 for a versionless client", resp.Status)
	}
}

func TestPostSyncSendsAndReportsVersion(t *testing.T) {
	var got Request
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{Protocol: ProtocolVersion})
	}))
	defer hs.Close()

	if _, err := PostSync(hs.URL, "t", nil, 5*time.Second); err != nil {
		t.Fatalf("PostSync: %v", err)
	}
	if got.Protocol != ProtocolVersion {
		t.Errorf("client sent v%d, want v%d", got.Protocol, ProtocolVersion)
	}
}

func TestPostSyncRefusesNewerServerWire(t *testing.T) {
	// The direction the server-side check cannot cover: the server accepted
	// our request but answers in a wire we would misread.
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{Protocol: ProtocolVersion + 1})
	}))
	defer hs.Close()

	_, err := PostSync(hs.URL, "t", nil, 5*time.Second)
	if err == nil {
		t.Fatal("a newer server wire must be refused")
	}
	if !strings.Contains(err.Error(), "protocol mismatch") {
		t.Errorf("error = %v, want a protocol mismatch", err)
	}
}

func TestPostSyncSurfacesMismatchMessagePlainly(t *testing.T) {
	// A 409 body is already a complete sentence; wrapping it in the status
	// line buries the actionable half.
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sync protocol mismatch: upgrade this end", http.StatusConflict)
	}))
	defer hs.Close()

	_, err := PostSync(hs.URL, "t", nil, 5*time.Second)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "409") {
		t.Errorf("mismatch error should not be wrapped in the status: %v", err)
	}
	if !strings.Contains(err.Error(), "upgrade this end") {
		t.Errorf("error lost the server's message: %v", err)
	}
}

func TestHealthAdvertisesProtocolRange(t *testing.T) {
	hs := testServer(t, &Server{Token: "t", Store: &fakeStore{}})
	resp, err := http.Get(hs.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	want := fmt.Sprintf("%d-%d", MinProtocolVersion, ProtocolVersion)
	if got := resp.Header.Get(ProtocolHeader); got != want {
		t.Errorf("%s = %q, want %q", ProtocolHeader, got, want)
	}
}
