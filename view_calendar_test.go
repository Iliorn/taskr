package main

import (
	"strings"
	"testing"
)

// TestRenderTimelineSubSkipsBareEntries asserts the sub-line is skipped when
// the entry has neither project nor tags (so bare entries stay 1-line), and
// that the project + tags appear in that order otherwise.
func TestRenderTimelineSubSkipsBareEntries(t *testing.T) {
	m := newTestModel()

	bare := dayActivity{title: "bare"}
	if got := m.renderTimelineSub(bare, 80); got != "" {
		t.Errorf("bare entry: want empty sub line, got %q", got)
	}

	withProj := dayActivity{title: "x", project: "alpha"}
	if got := m.renderTimelineSub(withProj, 80); !strings.Contains(got, "[alpha]") {
		t.Errorf("project-only: missing [alpha] in %q", got)
	}

	withTags := dayActivity{title: "x", tags: []string{"a", "b"}}
	if got := m.renderTimelineSub(withTags, 80); !strings.Contains(got, "#a") || !strings.Contains(got, "#b") {
		t.Errorf("tag-only: missing tags in %q", got)
	}

	both := dayActivity{title: "x", project: "alpha", tags: []string{"a"}}
	got := m.renderTimelineSub(both, 80)
	pIdx := strings.Index(got, "[alpha]")
	tIdx := strings.Index(got, "#a")
	if pIdx < 0 || tIdx < 0 || pIdx > tIdx {
		t.Errorf("project must precede tags: pIdx=%d tIdx=%d in %q", pIdx, tIdx, got)
	}
}

// TestRenderTimelineSubDropsTagsOnNarrow asserts the sub-line drops tags
// before project when the combined width would overflow innerW. This keeps
// the no-wrap contract from the renderTimelineLines panel honest.
func TestRenderTimelineSubDropsTagsOnNarrow(t *testing.T) {
	m := newTestModel()
	long := dayActivity{
		title:   "x",
		project: "shorty",
		tags:    []string{"a-very-long-tag-name-that-eats-the-width"},
	}
	got := m.renderTimelineSub(long, 16) // 16 - 4 indent = 12 avail
	if !strings.Contains(got, "[shorty]") {
		t.Errorf("project should survive narrow width: %q", got)
	}
	if strings.Contains(got, "a-very-long-tag-name") {
		t.Errorf("tag should be dropped when it doesn't fit: %q", got)
	}
}
