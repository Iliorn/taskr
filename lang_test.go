package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Iliorn/taskr/todo"
)

// lang_test.go pins the one way a fallback-to-English translation scheme fails
// quietly: a new UI string ships without a translation and nothing breaks —
// the screen still renders, just half of it in the wrong language. That is
// exactly what had happened; 110 strings (the whole sync and server section of
// Settings, the stats labels, the sort names, most of the help reference) were
// English on a Danish install.
//
// The check is one-directional on purpose. Every UI string must have an entry
// in every shipped table; a table entry with no call site is not an error,
// since a string can be reached from a value this test does not enumerate.
//
// Two kinds of call site have to be collected differently. Most are literal —
// tr("Settings") — and come from scanning the sources. The rest reach tr()
// through a variable (the keymap's descriptions, the bias levels,
// the enum words behind trPriority/trSize/trRecurrence); a source scan cannot
// see those, so they are enumerated from the tables that hold them. Both
// families had gaps: the sync half of Settings was untranslated because nobody
// noticed the fallback, and the Sequencer pane was English inside an otherwise
// Danish screen because its words never went through tr() at all.

var trLiteral = regexp.MustCompile(`\btr\(\s*"((?:[^"\\]|\\.)*)"`)

// uiStrings collects every string literal passed to tr() outside the tests.
func uiStrings(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, dir := range []string{".", "todo", "tasksync"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if dir == "." && name == "lang.go" {
				continue // the tables themselves
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			for _, m := range trLiteral.FindAllStringSubmatch(string(data), -1) {
				// The capture is source text: unquote it so escaped quotes
				// match the key the running program actually looks up.
				s, err := strconv.Unquote(`"` + m[1] + `"`)
				if err != nil {
					t.Fatalf("%s: cannot unquote tr literal %q: %v", name, m[1], err)
				}
				seen[s] = true
			}
		}
	}
	if len(seen) < 100 {
		t.Fatalf("found only %d tr() literals — the scan is broken, not the translations", len(seen))
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// dynamicUIStrings enumerates the UI strings that reach tr() through a
// variable, so the source scan cannot find them.
func dynamicUIStrings() []string {
	var out []string
	for i := range keymap {
		out = append(out, keymap[i].desc)
	}
	for _, s := range shortLabel {
		out = append(out, s)
	}
	// The footer relabels the toggle keys from what the selected task's state
	// makes them mean, so these reach tr() through hintLabelOverrides' map.
	out = append(out, "stop", "reopen")
	out = append(out, helpSectionOrder...)
	out = append(out, secDrill)
	for _, d := range []biasLevel{biasRelaxed, biasBalanced, biasIntense} {
		out = append(out, d.String())
	}
	for _, p := range []todo.Priority{todo.PriorityHigh, todo.PriorityMedium, todo.PriorityLow} {
		out = append(out, p.String())
	}
	for _, sz := range []todo.Size{todo.SizeSmall, todo.SizeMedium, todo.SizeLarge} {
		out = append(out, sz.String())
	}
	out = append(out, "daily", "weekly", "monthly", "yearly", "weekdays")
	// The explain overlay labels its rows with the dimension names the scoring
	// code holds, so they reach tr() through a slice rather than a literal.
	out = append(out, seqDimNames[:]...)
	// Parser keywords reach tr() through inputWord(), indexed off this slice —
	// a source scan sees the helper, not the words. Their translations are what
	// the grammars accept, so a new keyword without one is a keyword nobody can
	// type in that language (lang_input.go).
	out = append(out, inputKeywords...)

	seen := map[string]bool{}
	uniq := out[:0]
	for _, s := range out {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		uniq = append(uniq, s)
	}
	sort.Strings(uniq)
	return uniq
}

func TestEveryLanguageTranslatesEveryUIString(t *testing.T) {
	strs := append(uiStrings(t), dynamicUIStrings()...)
	for lang, table := range translations {
		var missing []string
		for _, s := range strs {
			if _, ok := table[s]; !ok {
				missing = append(missing, s)
			}
		}
		if len(missing) == 0 {
			continue
		}
		shown := missing
		if len(shown) > 20 {
			shown = shown[:20]
		}
		t.Errorf("%s is missing %d translation(s); a missing key renders in English:\n\t%s",
			lang, len(missing), strings.Join(quoteAll(shown), "\n\t"))
	}
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strconv.Quote(s)
	}
	return out
}

// A translation that drops or invents a format verb turns a rendered line into
// "%!d(MISSING)" the moment it is used. Comparing the verb sequence catches a
// typo in the table without having to render anything.
func TestTranslationsKeepTheirFormatVerbs(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z%]`)
	for lang, table := range translations {
		for src, dst := range table {
			want := verbs.FindAllString(src, -1)
			got := verbs.FindAllString(dst, -1)
			if strings.Join(want, " ") != strings.Join(got, " ") {
				t.Errorf("%s: %q → %q changes the format verbs (%v → %v)", lang, src, dst, want, got)
			}
		}
	}
}
