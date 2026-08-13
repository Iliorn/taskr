package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs_test.go guards the architecture notes against the drift that made them
// misleading: CLAUDE.md described prepareSave and markModifiedNoUndo as central
// mechanisms long after both had been refactored away, and a reader — human or
// a future Claude session — has no way to tell a stale name from a real one.
//
// The rule is narrow on purpose: every backticked, symbol-shaped word in the
// docs must appear somewhere in the source. It cannot check that the prose is
// *true*, but it does catch the failure that actually happened.

// symbolShaped matches identifiers with an internal case change (markModified,
// refreshCaches, sqliteRepo). Single lowercase words are prose ("dirty",
// "tasks"), and all-caps ones are acronyms ("MVU", "SQL").
var symbolShaped = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func looksLikeSymbol(s string) bool {
	if !symbolShaped.MatchString(s) {
		return false
	}
	// An internal lower→upper transition is the giveaway.
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z' {
			return true
		}
	}
	return false
}

// docsProseExceptions are symbol-shaped words that are prose, a Go stdlib or
// third-party name, or a file/asset name rather than something this tree
// declares.
var docsProseExceptions = map[string]bool{
	"TokyoNight": true, "GitHub": true, "SQLite": true, "JetBrains": true,
	"KeyMsg": true, "SliceStable": true, "MaxOpenConns": true, "SetMaxOpenConns": true,
	"WriteFile": true, "ReadFile": true, "UserHomeDir": true, "StringWidth": true,
	"TempDir": true, "MkdirTemp": true, "PrintDefaults": true, "FlagSet": true,
	"NewRequest": true, "LimitReader": true, "GOOS": true, "GOARCH": true,
	"EnvironmentFile": true, // systemd directive
}

func docSymbols(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	seen := map[string]bool{}
	for _, span := range regexp.MustCompile("`([^`\n]+)`").FindAllStringSubmatch(string(data), -1) {
		for _, word := range regexp.MustCompile(`[^A-Za-z0-9_]+`).Split(span[1], -1) {
			if looksLikeSymbol(word) && !docsProseExceptions[word] {
				seen[word] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func sourceText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, dir := range []string{".", "todo", "tasksync"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			b.Write(data)
		}
	}
	return b.String()
}

func TestDocsNameRealSymbols(t *testing.T) {
	src := sourceText(t)
	for _, doc := range []string{"CLAUDE.md", "README.md"} {
		for _, sym := range docSymbols(t, doc) {
			if !strings.Contains(src, sym) {
				t.Errorf("%s references %q, which no longer exists in the source — "+
					"rename it, drop it, or add it to docsProseExceptions if it isn't ours",
					doc, sym)
			}
		}
	}
}

// The Go badge in the README is read as a support claim; go.mod is the truth.
func TestReadmeGoVersionMatchesGoMod(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go (\d+\.\d+)`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod has no go directive")
	}
	want := string(m[1])

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	badge := regexp.MustCompile(`Go-(\d+\.\d+)-`).FindSubmatch(readme)
	if badge == nil {
		t.Fatal("README has no Go version badge")
	}
	if got := string(badge[1]); got != want {
		t.Errorf("README badge says Go %s, go.mod says %s", got, want)
	}
}
