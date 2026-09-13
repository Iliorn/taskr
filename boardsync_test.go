package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Iliorn/taskr/tasksync"
	"github.com/Iliorn/taskr/todo"
)

// boardHome gives the test a private home with a known column list and edit
// stamp, and puts the globals back afterwards — they are package-level, like
// the theme and the language.
func boardHome(t *testing.T, stages []string, edited time.Time) {
	t.Helper()
	setTestHome(t, t.TempDir())
	prevStages, prevAt, prevShare := activeStages, stagesModifiedAt, syncBoardColumns
	t.Cleanup(func() {
		applyStages(prevStages)
		applyStagesModifiedAt(prevAt)
		applySyncBoardColumns(prevShare)
	})
	applyStages(stages)
	applyStagesModifiedAt(edited)
	applySyncBoardColumns(true)
}

func TestBoardStoreRoundTrip(t *testing.T) {
	setTestHome(t, t.TempDir())
	var store boardStore
	// A server that has never been told anything has nothing to say, and that
	// is not an error — it is what lets the first client's list become the
	// fleet's.
	got, err := store.LoadBoard()
	if err != nil {
		t.Fatalf("load with no file: %v", err)
	}
	if len(got.Stages) != 0 {
		t.Errorf("a missing file should read as the zero board, got %v", got.Stages)
	}
	want := tasksync.Board{Stages: []string{"Analysis", "Completed"}, ModifiedAt: time.Now().UTC().Truncate(time.Second)}
	if err := store.SaveBoard(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err = store.LoadBoard()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !tasksync.SameBoard(got, want) || !got.ModifiedAt.Equal(want.ModifiedAt) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestLocalBoardHonoursThePreference(t *testing.T) {
	boardHome(t, []string{"Analysis", "Completed"}, time.Now())
	if b := localBoard(); b == nil || len(b.Stages) != 2 {
		t.Fatalf("a sharing device should offer its list, got %+v", b)
	}
	applySyncBoardColumns(false)
	if b := localBoard(); b != nil {
		t.Errorf("with sharing off the device must send no board, got %+v", b)
	}
}

func TestApplyBoardFromSync(t *testing.T) {
	edited := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	mine := []string{"Analysis", "Completed"}

	boardHome(t, mine, edited)
	if applyBoardFromSync(nil) {
		t.Error("a sync with no board should change nothing")
	}
	// The fleet's list is older than this device's edit: ours stands.
	older := &tasksync.Board{Stages: []string{"Backlog", "Done"}, ModifiedAt: edited.Add(-time.Hour)}
	if applyBoardFromSync(older) || activeStages[0] != "Analysis" {
		t.Errorf("an older list overwrote a newer local edit: %v", activeStages)
	}
	// Newer: adopted, and written to settings.json so it survives a restart.
	newer := &tasksync.Board{Stages: []string{"Todo", "Doing", "Shipped"}, ModifiedAt: edited.Add(time.Hour)}
	if !applyBoardFromSync(newer) {
		t.Fatal("a newer list should be adopted")
	}
	if len(activeStages) != 3 || activeStages[2] != "Shipped" {
		t.Fatalf("columns = %v, want the fleet's", activeStages)
	}
	s, err := loadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if len(s.Stages) != 3 || s.Stages[2] != "Shipped" || !s.StagesModifiedAt.Equal(newer.ModifiedAt) {
		t.Errorf("settings.json kept %v @%v, want the adopted list and its stamp", s.Stages, s.StagesModifiedAt)
	}
	// The same names with a newer stamp are recorded but redraw nothing —
	// otherwise two devices trade an identical list forever.
	again := &tasksync.Board{Stages: []string{"Todo", "Doing", "Shipped"}, ModifiedAt: newer.ModifiedAt.Add(time.Minute)}
	if applyBoardFromSync(again) {
		t.Error("an unchanged list should not report a change")
	}
	if !stagesModifiedAt.Equal(again.ModifiedAt) {
		t.Error("the newer stamp should still be recorded")
	}
	// Opted out: nothing arrives, whatever the server says.
	applySyncBoardColumns(false)
	if applyBoardFromSync(&tasksync.Board{Stages: []string{"No", "Thanks"}, ModifiedAt: time.Now()}) {
		t.Error("a device that opted out took the fleet's columns anyway")
	}
}

// End to end over HTTP: the client offers its edited list, the server keeps
// it, and a list edited more recently elsewhere comes back and lands in
// settings.json — the whole point being that a synced Stage field means the
// same thing on both machines.
func TestClientSyncExchangesTheBoard(t *testing.T) {
	edited := time.Now().UTC().Add(-time.Hour)
	boardHome(t, []string{"Analysis", "Completed"}, edited)
	h := openTestDB(t)

	fleet := tasksync.Board{Stages: []string{"Analysis", "Implementation", "On hold", "Completed"}, ModifiedAt: time.Now().UTC()}
	var offered *tasksync.Board
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sync", func(w http.ResponseWriter, r *http.Request) {
		var req tasksync.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		offered = req.Board
		if err := json.NewEncoder(w).Encode(tasksync.Response{Board: &fleet, ServerTime: time.Now()}); err != nil {
			t.Errorf("encode: %v", err)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	sum, err := runClientSync(h, syncConfig{URL: ts.URL, Token: "tok"}, 5*time.Second)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if offered == nil || len(offered.Stages) != 2 || !offered.ModifiedAt.Equal(edited) {
		t.Fatalf("the client offered %+v, want its own list and edit time", offered)
	}
	// runClientSync deliberately does not install it — the renderer's globals
	// belong to the loop that owns them.
	if activeStages[1] != "Completed" {
		t.Errorf("the sync goroutine installed the list itself: %v", activeStages)
	}
	if !applyBoardFromSync(sum.board) {
		t.Fatal("the fleet's newer list should be adopted when applied")
	}
	if strings.Join(activeStages, ",") != strings.Join(fleet.Stages, ",") {
		t.Errorf("columns = %v, want %v", activeStages, fleet.Stages)
	}
}

// A card whose stage names a column the new list does not have falls into the
// first column — the same rule as any unknown name, and the behaviour asked
// for. Nothing re-stages the cards behind the user's back.
func TestAdoptedBoardLeavesCardsAlone(t *testing.T) {
	boardHome(t, []string{"Analysis", "Implementation", "Completed"}, time.Now().Add(-time.Hour))
	task := todo.New("a card")
	task.SetStage("Implementation")
	if got := stageDisplay(task.Stage); got != "Implementation" {
		t.Fatalf("stage display = %q before the change", got)
	}
	applyBoardFromSync(&tasksync.Board{Stages: []string{"Todo", "Doing", "Shipped"}, ModifiedAt: time.Now()})
	if task.Stage != "Implementation" {
		t.Errorf("the card's stored stage was rewritten to %q", task.Stage)
	}
	if got := stageDisplay(task.Stage); got != "Todo" {
		t.Errorf("an unknown stage should render in the first column, got %q", got)
	}
}
