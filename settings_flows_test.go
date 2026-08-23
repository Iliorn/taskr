package main

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/x/ansi"
)

// Scripted flows for the Settings tab: the four inline sync editors and the
// preference toggles.
//
// These are modals, and ARCHITECTURE.md asks for a flow per modal for the usual
// reason — each one has to write config, reset the mode, and leave the shared
// text input in a state the *next* modal can use. The sync pair carries an
// extra obligation the others do not: it holds the bearer token, which
// SECURITY.md names as the only secret taskr stores. So the masking and the
// file mode are pinned here, not left to inspection.

// settingsModel builds a model on the Settings tab with a private HOME, so the
// editors write a real sync.json that the assertions can stat.
func settingsModel(t *testing.T) model {
	t.Helper()
	setTestHome(t, t.TempDir())
	if err := ensureStorageDir(); err != nil {
		t.Fatal(err)
	}
	m := modelWithTasks(t)
	m.tab = tabSettings
	return m
}

// openSetting puts the settings cursor on a row and activates it.
func openSetting(t *testing.T, m model, row int) model {
	t.Helper()
	m.settingsCursor = row
	return sendKey(t, m, "enter")
}

// ── The sync server URL editor ──────────────────────────────────────────────

func TestScriptEditSyncURL(t *testing.T) {
	m := settingsModel(t)
	m = openSetting(t, m, settingSyncServer)
	if m.mode != modeEditSyncURL {
		t.Fatalf("mode = %v, want modeEditSyncURL", m.mode)
	}
	m = script(t, m, "https://sync.example.com", "enter")
	if m.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", m.mode)
	}
	if m.syncCfg.URL != "https://sync.example.com" {
		t.Errorf("URL = %q", m.syncCfg.URL)
	}
	// It has to reach disk, not just the model — the whole point of the
	// editor is that the setting survives a restart.
	if got := loadSyncConfigFile().URL; got != "https://sync.example.com" {
		t.Errorf("sync.json URL = %q, want it persisted", got)
	}
}

func TestScriptEditSyncURLBlankIsADeliberateClear(t *testing.T) {
	m := settingsModel(t)
	m.syncCfg.URL = "https://old.example.com"
	m = openSetting(t, m, settingSyncServer)
	// The field arrives pre-filled, so emptying it is an instruction, not an
	// accident — treating blank as "no change" would make the URL
	// unremovable from the UI.
	m.textInput.SetValue("")
	m = sendKey(t, m, "enter")
	if m.syncCfg.URL != "" {
		t.Errorf("URL = %q, want it cleared", m.syncCfg.URL)
	}
}

func TestScriptEditSyncURLEscapeDiscards(t *testing.T) {
	m := settingsModel(t)
	m.syncCfg.URL = "https://keep.example.com"
	m = openSetting(t, m, settingSyncServer)
	m = script(t, m, "typed-but-abandoned", "esc")
	if m.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", m.mode)
	}
	if m.syncCfg.URL != "https://keep.example.com" {
		t.Errorf("URL = %q, want it unchanged after esc", m.syncCfg.URL)
	}
}

func TestScriptEditSyncURLWarnsOnPlainHTTPToAPublicHost(t *testing.T) {
	m := settingsModel(t)
	m = openSetting(t, m, settingSyncServer)
	m = script(t, m, "http://sync.example.com", "enter")
	if !strings.Contains(m.syncStatus, "unencrypted") {
		t.Errorf("syncStatus = %q, want a warning that the token travels in the clear", m.syncStatus)
	}

	// Loopback is the normal local-hub setup and must not nag — the token
	// never leaves the machine. This also covers the harder half: the
	// warning has to be *cleared* when the URL stops being insecure, not
	// just set when it starts. A stale security notice is worse than none.
	m = openSetting(t, m, settingSyncServer)
	m.textInput.SetValue("")
	m = script(t, m, "http://127.0.0.1:8765", "enter")
	if strings.Contains(m.syncStatus, "unencrypted") {
		t.Errorf("loopback should not warn, got %q", m.syncStatus)
	}
}

func TestScriptEditSyncURLLeavesOtherStatusesAlone(t *testing.T) {
	// Clearing the warning must not clear whatever else put a message
	// there — a sync result is not this editor's to erase.
	m := settingsModel(t)
	m.syncStatus = "Last sync: sent 3, received 1"
	m = openSetting(t, m, settingSyncServer)
	m = script(t, m, "https://sync.example.com", "enter")
	if m.syncStatus != "Last sync: sent 3, received 1" {
		t.Errorf("syncStatus = %q, want the unrelated message preserved", m.syncStatus)
	}
}

// ── The token editors ───────────────────────────────────────────────────────

func TestScriptEditSyncTokenMasksAndUnmasks(t *testing.T) {
	m := settingsModel(t)
	m.syncCfg.Token = "existing-secret"
	m = openSetting(t, m, settingSyncToken)
	if m.mode != modeEditSyncToken {
		t.Fatalf("mode = %v, want modeEditSyncToken", m.mode)
	}
	// The pre-filled secret must not be echoed back in plaintext.
	if m.textInput.EchoMode != textinput.EchoPassword {
		t.Error("the token editor is not masked")
	}

	m = script(t, m, "", "enter")
	// And the mask has to come back off: the text input is shared, so a
	// leftover EchoPassword would render the *next* modal — quick-add, a
	// rename — as bullets.
	if m.textInput.EchoMode != textinput.EchoNormal {
		t.Error("the shared input stayed masked after the token editor closed")
	}
}

func TestScriptEditSyncTokenEscapeAlsoUnmasks(t *testing.T) {
	// The path most likely to be forgotten, and the one a user hits most:
	// open the token row by accident, press esc.
	m := settingsModel(t)
	m = openSetting(t, m, settingSyncToken)
	m = sendKey(t, m, "esc")
	if m.mode != modeNormal {
		t.Fatalf("mode = %v, want modeNormal", m.mode)
	}
	if m.textInput.EchoMode != textinput.EchoNormal {
		t.Error("escaping the token editor left the shared input masked")
	}
}

func TestScriptSyncTokenIsStoredPrivately(t *testing.T) {
	m := settingsModel(t)
	m = openSetting(t, m, settingSyncToken)
	m = script(t, m, "super-secret-token", "enter")

	if m.syncCfg.Token != "super-secret-token" {
		t.Fatalf("token = %q", m.syncCfg.Token)
	}
	info, err := os.Stat(syncConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	// SECURITY.md states this file is 0600. It is the only secret taskr
	// stores, so the mode is a documented property, not an accident of
	// whichever call happened to create the file. Windows has no POSIX mode
	// bits — Go reports 0666 for every file it creates there, and the file is
	// protected by the ACL on the user's profile instead — so the assertion
	// runs where the mode is real.
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0600 {
			t.Errorf("sync.json mode = %v, want 0600", got)
		}
	}
}

func TestScriptEditServerListenAndToken(t *testing.T) {
	m := settingsModel(t)

	m = openSetting(t, m, settingServerListen)
	if m.mode != modeEditServerListen {
		t.Fatalf("mode = %v, want modeEditServerListen", m.mode)
	}
	m.textInput.SetValue("")
	m = script(t, m, "127.0.0.1:9999", "enter")
	if m.syncCfg.ServerListen != "127.0.0.1:9999" {
		t.Errorf("ServerListen = %q", m.syncCfg.ServerListen)
	}

	m = openSetting(t, m, settingServerToken)
	if m.mode != modeEditServerToken {
		t.Fatalf("mode = %v, want modeEditServerToken", m.mode)
	}
	if m.textInput.EchoMode != textinput.EchoPassword {
		t.Error("the server-token editor is not masked")
	}
	// A real token: the server-token editor refuses weak ones outright (see
	// TestServerTokenEditorRefusesWeakTokens), so a toy value here would be
	// testing the rejection path by accident.
	const serverToken = "kR7-server-side-secret-9fQ2xL"
	m = script(t, m, serverToken, "enter")
	if m.syncCfg.ServerToken != serverToken {
		t.Errorf("ServerToken = %q", m.syncCfg.ServerToken)
	}
	if m.textInput.EchoMode != textinput.EchoNormal {
		t.Error("the shared input stayed masked after the server-token editor")
	}
	if got := loadSyncConfigFile(); got.ServerListen != "127.0.0.1:9999" || got.ServerToken != serverToken {
		t.Errorf("server config did not persist: %+v", got)
	}
}

// ── Preference toggles ──────────────────────────────────────────────────────

func TestToggleAutoClosePreferencesPersist(t *testing.T) {
	m := settingsModel(t)

	before := m.autoCloseParent
	m.toggleAutoCloseParent()
	if m.autoCloseParent == before {
		t.Fatal("toggleAutoCloseParent did not flip the preference")
	}
	if got, err := loadSettings(); err != nil {
		t.Fatal(err)
	} else if got.AutoCloseParent != m.autoCloseParent {
		t.Errorf("settings.json AutoCloseParent = %v, want %v", got.AutoCloseParent, m.autoCloseParent)
	}

	beforeSubs := m.autoCloseSubtasks
	m.toggleAutoCloseSubtasks()
	if m.autoCloseSubtasks == beforeSubs {
		t.Fatal("toggleAutoCloseSubtasks did not flip the preference")
	}
	if got, err := loadSettings(); err != nil {
		t.Fatal(err)
	} else if got.AutoCloseSubtasks != m.autoCloseSubtasks {
		t.Errorf("settings.json AutoCloseSubtasks = %v", got.AutoCloseSubtasks)
	}
}

func TestToggleAgingFlipsAndPersists(t *testing.T) {
	m := settingsModel(t)
	before := activeBiases.Aging
	m.toggleAging()
	defer func() { activeBiases.Aging = before }()

	if activeBiases.Aging == before {
		t.Fatal("toggleAging did not flip the bias")
	}
	// Aging changes the ranking, so the derived caches must be invalidated
	// or the list keeps showing the old order until something else dirties
	// them.
	if !m.cache.dirty {
		t.Error("toggleAging left the derived caches warm")
	}
	// Stored as the inverse, so the zero value means "aging on" — which is
	// exactly the kind of inversion a test should pin rather than trust.
	if got, err := loadSettings(); err != nil {
		t.Fatal(err)
	} else if got.SeqAgingDisabled == activeBiases.Aging {
		t.Errorf("settings.json SeqAgingDisabled = %v with Aging = %v; they must be inverses",
			got.SeqAgingDisabled, activeBiases.Aging)
	}
}

func TestCycleThemeWrapsAndPersists(t *testing.T) {
	m := settingsModel(t)
	start := m.themeName

	m.cycleTheme(1)
	if m.themeName == start {
		t.Fatal("cycleTheme did not advance")
	}
	if got, err := loadSettings(); err != nil {
		t.Fatal(err)
	} else if got.Theme != m.themeName {
		t.Errorf("settings.json Theme = %q, want %q", got.Theme, m.themeName)
	}

	// Stepping back must land where it started — an off-by-one in the
	// modulo shows up as a theme you cannot get back to.
	m.cycleTheme(-1)
	if m.themeName != start {
		t.Errorf("theme = %q after +1/-1, want %q", m.themeName, start)
	}

	// A full lap returns to the start rather than running off the end.
	for range themes {
		m.cycleTheme(1)
	}
	if m.themeName != start {
		t.Errorf("theme = %q after a full lap, want %q", m.themeName, start)
	}
}

func TestCycleLangVisitsEveryLanguageAndWraps(t *testing.T) {
	m := settingsModel(t)
	defer applyLang(string(langEN))

	applyLang(string(langEN))
	seen := map[language]bool{activeLang: true}
	for range availableLanguages {
		m.cycleLang(1)
		seen[activeLang] = true
	}
	// Every shipped language has to be reachable from the Settings toggle;
	// a language in the table that the cycle skips is one nobody can select.
	for _, want := range availableLanguages {
		if !seen[want] {
			t.Errorf("cycleLang never reached %q", want)
		}
	}
	// A full lap wraps back to English.
	if activeLang != langEN {
		t.Errorf("after a full lap activeLang = %q, want en", activeLang)
	}

	m.cycleLang(1)
	if got, err := loadSettings(); err != nil {
		t.Fatal(err)
	} else if got.Language != string(activeLang) {
		t.Errorf("settings.json Language = %q, want %q", got.Language, activeLang)
	}
	// Switching language must re-place the input placeholders, or the
	// prompts stay in the previous language until a restart.
	if m.searchInput.Placeholder != searchHint() {
		t.Errorf("placeholder = %q, not refreshed for %q", m.searchInput.Placeholder, activeLang)
	}
}

// ── Grouped Preferences pane ────────────────────────────────────────────────

// The cursor walks the whole list without stopping on Version. Enter on that
// row did nothing, so a stop there was a keypress that looked broken; the skip
// lives in settingsNavOrder so every caller inherits it.
func TestSettingsCursorSkipsTheVersionRow(t *testing.T) {
	m := settingsModel(t)
	m.settingsCursor = settingAutoCloseParent
	seen := map[int]bool{m.settingsCursor: true}
	for i := 0; i < numSettingsRows*2; i++ {
		m = sendKey(t, m, "down")
		if m.settingsCursor == settingVersion {
			t.Fatalf("the cursor stopped on the Version row after %d presses", i+1)
		}
		seen[m.settingsCursor] = true
	}
	for _, row := range []int{settingCheckUpdate, settingAging, settingServerOn} {
		if !seen[row] {
			t.Errorf("row %d is unreachable going down", row)
		}
	}
	// And the same on the way back up.
	for i := 0; i < numSettingsRows*2; i++ {
		m = sendKey(t, m, "up")
		if m.settingsCursor == settingVersion {
			t.Fatalf("the cursor stopped on the Version row going up, after %d presses", i+1)
		}
	}
}

// settingsPreferencesLine is what scrolls the pane on a short terminal, and the
// pane no longer draws one line per row — the headings and the blank line
// between groups sit in between. Hunt the cursor mark in the rendered pane and
// compare, the way the detail pane's estimate is pinned to its document.
func TestSettingsPreferencesLineMatchesTheRenderedPane(t *testing.T) {
	m := settingsModel(t)
	for _, g := range settingsPreferenceGroups {
		for _, row := range g.rows {
			if !settingsSelectable(row) || !m.settingsRowVisible(row) {
				continue
			}
			m.settingsCursor = row
			preferences, _ := m.renderSettingsSections(60, 60)
			lines := strings.Split(strings.TrimRight(ansi.Strip(preferences), "\n"), "\n")
			drawn := -1
			for i, line := range lines {
				if strings.HasPrefix(line, strings.TrimSpace(cursorMark)) || strings.HasPrefix(line, cursorMark) {
					drawn = i
					break
				}
			}
			if drawn < 0 {
				t.Fatalf("row %d: no cursor mark in the rendered pane:\n%s", row, preferences)
			}
			if got := m.settingsPreferencesLine(row); got != drawn {
				t.Errorf("row %d: settingsPreferencesLine = %d, drawn on line %d", row, got, drawn)
			}
		}
	}
	if got := m.settingsPreferencesLine(settingBiasDeadline); got != -1 {
		t.Errorf("a Sequencer row should not report a Preferences line, got %d", got)
	}
}

// A row whose enter opens a text editor is marked, and a row that cycles on
// ←/→ is not — the two used to look identical, so which key a row answered was
// only discoverable by pressing one.
func TestSettingsMarksTheRowsThatOpenAnEditor(t *testing.T) {
	m := settingsModel(t)
	preferences, _ := m.renderSettingsSections(60, 60)
	for _, line := range strings.Split(ansi.Strip(preferences), "\n") {
		marked := strings.Contains(line, strings.TrimSpace(settingsEditMark))
		cycled := strings.Contains(line, "‹")
		if marked && cycled {
			t.Errorf("a row cannot both cycle and open an editor: %q", line)
		}
	}
	for _, tc := range []struct {
		row  int
		want bool
	}{
		{settingSyncServer, true},
		{settingServerListen, true},
		{settingStages, true},
		{settingTheme, false},
		{settingSyncNow, false},
		{settingVersion, false},
	} {
		if got := settingsEditsText(tc.row); got != tc.want {
			t.Errorf("settingsEditsText(%d) = %v, want %v", tc.row, got, tc.want)
		}
	}
}

// → and enter are one table now. They were two hand-kept chains, and a toggle
// that answered one key but not the other is what that cost.
func TestSettingsEnterAndRightAgreeOnEveryToggleRow(t *testing.T) {
	// Half of these rows write package-level globals; put them back so the
	// rest of the suite sees the state it started with.
	base := settingsModel(t)
	lang, biasesBefore, board, themeBefore := activeLang, activeBiases, showBoard, base.themeName
	t.Cleanup(func() {
		applyLang(string(lang))
		applyBiases(biasesBefore)
		applyShowBoard(board)
		applyTheme(themeByName(themeBefore))
	})

	for _, row := range []int{
		settingAutoCloseParent, settingAutoCloseSubtasks, settingShowBoard,
		settingTheme, settingLanguage, settingAging,
		settingBiasDeadline, settingBiasPriority, settingBiasMomentum,
	} {
		byEnter := settingsModel(t)
		byEnter.settingsCursor = row
		byEnter = sendKey(t, byEnter, "enter")

		byArrow := settingsModel(t)
		byArrow.settingsCursor = row
		byArrow = sendKey(t, byArrow, "right")

		enterPane, enterSeq := byEnter.renderSettingsSections(60, 60)
		arrowPane, arrowSeq := byArrow.renderSettingsSections(60, 60)
		if enterPane != arrowPane || enterSeq != arrowSeq {
			t.Errorf("row %d: enter and → left different values:\nenter:\n%s%s\nright:\n%s%s",
				row, enterPane, enterSeq, arrowPane, arrowSeq)
		}
	}
}

// Theme and Language open the pane. They are the settings someone changes on
// day one, and they sat six rows down behind the auto-close toggles.
func TestSettingsOpensWithAppearance(t *testing.T) {
	first := settingsPreferenceGroups[0]
	if got := []int{settingTheme, settingLanguage}; len(first.rows) != len(got) ||
		first.rows[0] != settingTheme || first.rows[1] != settingLanguage {
		t.Fatalf("the first Preferences group is %q %v, want Theme then Language", first.title, first.rows)
	}
	m := settingsModel(t)
	if got := m.settingsPreferencesLine(settingTheme); got != 1 {
		t.Errorf("Theme renders on pane line %d, want 1 (the line under the first heading)", got)
	}
	// And the pane's first heading is the one the cursor starts under, not a
	// group the reader has to scroll past.
	m.settingsCursor = settingTheme
	preferences, _ := m.renderSettingsSections(60, 60)
	lines := strings.Split(ansi.Strip(preferences), "\n")
	if !strings.Contains(lines[0], tr("Appearance")) {
		t.Errorf("first pane line is %q, want the Appearance heading", lines[0])
	}
}
