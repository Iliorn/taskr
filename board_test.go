package main

import (
	"reflect"
	"testing"
)

// withStages runs fn with the active stage list swapped, restoring the
// original after — the applyTheme/applyLang test pattern for globals.
func withStages(t *testing.T, stages []string, fn func()) {
	t.Helper()
	prev := activeStages
	applyStages(stages)
	defer applyStages(prev)
	fn()
}

func TestStagesFromSettings(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty falls back to defaults", nil, defaultStages()},
		{"all-blank falls back to defaults", []string{"", "  "}, defaultStages()},
		{"trims and keeps order", []string{" Todo ", "Doing"}, []string{"Todo", "Doing"}},
		{"dedupes case-insensitively onto first spelling", []string{"Todo", "todo", "Done-ish"}, []string{"Todo", "Done-ish"}},
	}
	for _, c := range cases {
		if got := stagesFromSettings(appSettings{Stages: c.in}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: stagesFromSettings(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestStageIndexAndCanonical(t *testing.T) {
	withStages(t, []string{"Todo", "Doing", "Waiting"}, func() {
		for stage, want := range map[string]int{
			"":        0, // fresh task → first column
			"Todo":    0,
			"doing":   1, // case-insensitive
			"Waiting": 2,
			"Review":  0, // renamed-away stage strands visibly in the first column
		} {
			if got := stageIndex(stage); got != want {
				t.Errorf("stageIndex(%q) = %d, want %d", stage, got, want)
			}
		}

		if name, ok := canonicalStage(" doing "); !ok || name != "Doing" {
			t.Errorf("canonicalStage(' doing ') = %q/%v, want Doing/true", name, ok)
		}
		if _, ok := canonicalStage("nope"); ok {
			t.Error("canonicalStage('nope') should not resolve")
		}
	})
}
