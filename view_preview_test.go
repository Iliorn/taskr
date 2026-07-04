package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestQuickAddPreviewParsedFields(t *testing.T) {
	applyLang(string(langEN))
	got := ansi.Strip(renderQuickAddPreview("Buy milk #shopping due:tomorrow p:high s:l", 120))
	for _, want := range []string{`"Buy milk"`, "#shopping", "due ", "p:high", "s:L"} {
		if !strings.Contains(got, want) {
			t.Errorf("preview %q missing %q", got, want)
		}
	}
}

// A mistyped token must not become a chip — it stays inside the title quotes,
// which is exactly the typo-catching signal the preview exists for.
func TestQuickAddPreviewKeepsBadTokensInTitle(t *testing.T) {
	applyLang(string(langEN))
	got := ansi.Strip(renderQuickAddPreview("Buy milk due:tomorow p:hgih", 120))
	if !strings.Contains(got, `"Buy milk due:tomorow p:hgih"`) {
		t.Errorf("bad tokens should stay in the title; got %q", got)
	}
	if strings.Contains(got, "due 0") || strings.Contains(got, "p:high") {
		t.Errorf("no field chip should render for mistyped tokens; got %q", got)
	}
}

func TestSearchPreviewFiltersAndTitle(t *testing.T) {
	applyLang(string(langEN))
	got := ansi.Strip(renderSearchPreview("#shop @home p:high due:<friday milk", 120))
	for _, want := range []string{"#shop", "@home", "p:high", "due <", `title~ "milk"`} {
		if !strings.Contains(got, want) {
			t.Errorf("search preview %q missing %q", got, want)
		}
	}
}

// A mistyped due: filter falls into the fuzzy title-match run rather than
// vanishing silently.
func TestSearchPreviewBadDueFallsToTitle(t *testing.T) {
	applyLang(string(langEN))
	got := ansi.Strip(renderSearchPreview("due:tomorow overdue grcry", 120))
	if !strings.Contains(got, "overdue") {
		t.Errorf("recognised 'overdue' should be a chip; got %q", got)
	}
	if !strings.Contains(got, `title~ "due:tomorow grcry"`) {
		t.Errorf("mistyped due token should land in the title run; got %q", got)
	}
}

// Both previews honour the width budget (no-wrap contract).
func TestPreviewsRespectWidth(t *testing.T) {
	applyLang(string(langEN))
	long := "A very long task title here #alpha #beta #gamma @someproject due:tomorrow p:high s:large r:weekly dep:^"
	for _, w := range []int{20, 40, 80} {
		if got := ansi.StringWidth(renderQuickAddPreview(long, w)); got > w {
			t.Errorf("quick-add preview width %d exceeds budget %d", got, w)
		}
		if got := ansi.StringWidth(renderSearchPreview(long, w)); got > w {
			t.Errorf("search preview width %d exceeds budget %d", got, w)
		}
	}
}
