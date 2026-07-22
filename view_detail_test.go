package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// ── Border-title helpers ──────────────────────────────────────────────────────

// renderDetailPanel renders the stacked detail box for the current task in m,
// returning the raw multi-line string including the bordered panel. This helper
// mirrors the logic in view.View that produces the stacked panel.
func renderStandaloneDetailPanel(m model) string {
	w := m.termWidth - 6
	content := m.buildDetailContent()
	if content == "" {
		return ""
	}
	focused := m.pane == paneDetail
	dst := detailPanelStyle
	if focused {
		dst = detailPanelFocusedStyle
	}
	rendered := dst.Width(w).Render(m.applyDetailScroll(content))
	return withBorderTitle(rendered, m.detailPanelTitle(), w, focused)
}

// TestRenderGanttNarrowNoPanic guards the Gantt "today:" marker against an
// out-of-bounds write: when the localized label is wider than the (floored)
// chart, the insert position goes negative. It must clip rather than panic, in
// every language. See the insertPos clamp in renderGantt.
//
// The Gantt is a fixed two-panel chart with its own minimum width, so (like the
// Projects tab in TestNarrowNoWrapDanish) the no-wrap contract is only asserted
// at widths where it is designed to fit; the no-panic guarantee holds at every
// width down to the smallest terminals.
func TestRenderGanttNarrowNoPanic(t *testing.T) {
	for _, lang := range []language{langEN, langDA} {
		applyLang(string(lang))
		for _, width := range []int{16, 20, 24, 30, 40, 50, 70, 80, 120} {
			m := newTestModel()
			m.termWidth = width
			m.termHeight = 30
			// Tasks whose start/due straddle "today" so the marker is placed.
			tasks := []todo.Todo{
				todo.New("Task one"),
				todo.New("Task two"),
			}
			tasks[0].StartDate = m.frameTime.AddDate(0, 0, -3)
			tasks[0].DueDate = m.frameTime.AddDate(0, 0, 3)
			tasks[1].StartDate = m.frameTime.AddDate(0, 0, -1)
			tasks[1].DueDate = m.frameTime.AddDate(0, 0, 5)

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("lang=%s width=%d: renderGantt panicked: %v", lang, width, r)
					}
				}()
				out := m.renderGantt(tasks)
				if width < 70 {
					return // below the chart's minimum fit width
				}
				for _, line := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(line); w > width {
						t.Errorf("lang=%s width=%d: line %d cells exceeds width: %q", lang, width, w, line)
					}
				}
			}()
		}
	}
	applyLang(string(langEN))
}

// Backlog item ceea44fe: detail page 1 must surface the task's short ID so
// the user can read it (and pass it to the CLI) without leaving the TUI.
func TestRenderDetailPage1ShowsShortID(t *testing.T) {
	task := todo.New("show me my id")
	m := newTestModel()
	m.termWidth = 120
	m.termHeight = 40
	m.Store.add(task)
	m.ensureCache()
	m.cursor = 0
	m.pane = paneDetail

	got := m.renderDetailPage1(&task)
	short := shortID(task.ID)
	if !strings.Contains(got, short) {
		t.Errorf("detail page 1 missing short ID %q in output:\n%s", short, got)
	}
	if !strings.Contains(got, "ID:") {
		t.Errorf("detail page 1 missing 'ID:' label in output:\n%s", got)
	}
}

// TestDetailPagesNoWrap guards the detail pane's no-wrap contract: the detail
// renders one column at every width, and no rendered line may exceed
// (termWidth-8) cells, in any language. Long titles/values exist deliberately
// to exercise the value-truncation paths in renderField.
func TestDetailPagesNoWrap(t *testing.T) {
	t.Cleanup(func() { applyLang(string(langEN)) })

	parent := todo.New("a deliberately long parent title that would otherwise wrap on narrow terminals")
	parent.Project = "very-long-project-name-that-could-easily-overflow"
	parent.Notes = "first note line that should be truncated when the value column is narrow\nsecond"
	parent.Tags = []string{"alpha", "beta", "gamma-with-a-long-suffix"}
	parent.AddLearning("a learning that is also quite long and should be safely truncated to the column width")

	sub := todo.New("a subtask title which is deliberately verbose so it must truncate")
	sub.ParentID = parent.ID

	dep := todo.New("a dependency task with another long title that the dep list must truncate")
	parent.Dependencies = []string{dep.ID}

	for _, lang := range []language{langEN, langDA} {
		applyLang(string(lang))
		for _, width := range []int{60, 79, 80, 100, 140} {
			m := newTestModel()
			m.termWidth = width
			m.termHeight = 40
			m.Store.add(parent)
			m.Store.add(sub)
			m.Store.add(dep)
			m.ensureCache()
			m.cursor = 0
			m.pane = paneDetail

			for page, render := range []func(*todo.Todo) string{
				m.renderDetailPage1,
				m.renderDetailPage2,
			} {
				out := render(&parent)
				inner := width - 8
				for _, line := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(line); w > inner {
						t.Errorf("lang=%s width=%d page=%d: line %d cells exceeds inner %d: %q",
							lang, width, page+1, w, inner, line)
					}
				}
			}
		}
	}
}

// ── Border-title tests ────────────────────────────────────────────────────────

// TestDetailPanelTitleOnBorder verifies that after the border-title change,
// the task title appears on the top border line of the detail panel, and the
// old in-box title line is absent from the content (renderDetailPage1).
func TestDetailPanelTitleOnBorder(t *testing.T) {
	task := todo.New("My Important Task")
	m := initialModel(&fakeRepo{todos: []todo.Todo{task}})
	m.termWidth = 120
	m.termHeight = 40
	m.cursor = 0
	m.tab = tabTasks
	m.pane = paneDetail

	panel := renderStandaloneDetailPanel(m)
	if panel == "" {
		t.Fatal("renderStandaloneDetailPanel returned empty string")
	}
	lines := strings.Split(panel, "\n")
	if len(lines) == 0 {
		t.Fatal("panel has no lines")
	}

	// The first line (top border) must contain the task title.
	topBorder := lines[0]
	if !strings.Contains(topBorder, "My Important Task") {
		t.Errorf("task title not found in top border line: %q", topBorder)
	}

	// The top border must begin with "╭" (after stripping ANSI).
	plain := ansi.Strip(topBorder)
	plain = strings.TrimLeft(plain, " ") // strip leading margin spaces
	if !strings.HasPrefix(plain, "╭") {
		t.Errorf("top border line does not start with ╭ after stripping margin: %q", plain)
	}

	// The in-box title (detailTitleStyle on its own line) is gone: renderDetailPage1
	// no longer emits it, so the first content line inside the box must be a field
	// row (not a styled task-title line). Check that a field label appears early.
	content := m.renderDetailPage1(&task)
	if strings.Contains(content, "My Important Task") {
		t.Errorf("task title should not appear inside detail page 1 content any more: %q", content[:min(len(content), 200)])
	}
}

// Pane titles live in the top border. Keep one blank interior row beneath
// them so the title does not crowd the first column heading or detail field.
func TestPanelsLeaveSpaceBelowBorderTitle(t *testing.T) {
	task := todo.New("Spacing check")
	m := initialModel(&fakeRepo{todos: []todo.Todo{task}})
	m.termWidth = 120
	m.termHeight = 30
	m.cursor = 0
	m.tab = tabTasks
	m.pane = paneDetail
	lm := m
	lm.termWidth = sideBySideMinWidth - 10
	lm.pane = paneList

	panels := map[string]string{
		"detail": renderStandaloneDetailPanel(m),
		"list":   lm.buildListContent(lm.termWidth-6, 12),
	}
	for name, panel := range panels {
		lines := strings.Split(panel, "\n")
		if len(lines) < 2 {
			t.Fatalf("%s panel has fewer than two lines: %q", name, panel)
		}
		row := strings.TrimSpace(ansi.Strip(lines[1]))
		if !strings.HasPrefix(row, "│") || !strings.HasSuffix(row, "│") {
			t.Fatalf("%s first interior row is not bordered: %q", name, row)
		}
		inside := strings.TrimSuffix(strings.TrimPrefix(row, "│"), "│")
		if strings.TrimSpace(inside) != "" {
			t.Errorf("%s panel has content directly below its title: %q", name, row)
		}
	}
}

func TestDanishDetailLabels(t *testing.T) {
	t.Cleanup(func() { applyLang(string(langEN)) })
	applyLang(string(langDA))

	cases := map[string]string{
		"Recurrence":    "Gentagelse",
		"Size":          "Størrelse",
		"Time entries:": "Tidsregistreringer:",
	}
	for source, want := range cases {
		if got := tr(source); got != want {
			t.Errorf("tr(%q) = %q, want %q", source, got, want)
		}
	}
	if got := trRecurrence("weekly"); got != "ugentligt" {
		t.Errorf("trRecurrence(weekly) = %q, want ugentligt", got)
	}
	if got := trSize(todo.SizeLarge); got != "stor" {
		t.Errorf("trSize(SizeLarge) = %q, want stor", got)
	}
}

// TestDetailBorderTitleNoWrap guards the no-wrap contract for the full stacked
// detail panel (border included) at several widths.
func TestDetailBorderTitleNoWrap(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 120, 140} {
		task := todo.New("A very long task title that could potentially overflow the border of the detail panel on narrow terminals")
		m := initialModel(&fakeRepo{todos: []todo.Todo{task}})
		m.termWidth = width
		m.termHeight = 40
		m.tab = tabTasks
		m.pane = paneDetail
		m.cursor = 0

		panel := renderStandaloneDetailPanel(m)
		for n, line := range strings.Split(panel, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width=%d: panel line %d is %d cells (exceeds terminal width): %q",
					width, n, w, line)
			}
		}
	}
}

// TestDetailBorderTitleTagsTab checks that the Tags tab detail panel puts the
// tag name on the border and no longer renders it as the first content line.
func TestDetailBorderTitleTagsTab(t *testing.T) {
	task := todo.New("A task with a tag")
	task.Tags = []string{"projectx"}
	m := initialModel(&fakeRepo{todos: []todo.Todo{task}})
	m.termWidth = 120
	m.termHeight = 40
	m.tab = tabTags
	m.tagTabCursor = 0 // first tag in list

	w := m.termWidth - 6
	content := m.buildDetailContent()
	panel := detailPanelStyle.Width(w).Render(m.applyDetailScroll(content))
	panel = withBorderTitle(panel, m.detailPanelTitle(), w, false)

	lines := strings.Split(panel, "\n")
	if len(lines) == 0 {
		t.Fatal("panel has no lines")
	}

	// Border title must contain the tag name.
	topBorder := lines[0]
	if !strings.Contains(topBorder, "#projectx") {
		t.Errorf("tag name not found in top border: %q", topBorder)
	}

	// No-wrap contract: every line must fit within the terminal width.
	for n, line := range lines {
		if w2 := ansi.StringWidth(line); w2 > m.termWidth {
			t.Errorf("line %d is %d cells (exceeds termWidth=%d): %q", n, w2, m.termWidth, line)
		}
	}
}

// TestWithBorderTitleWidth verifies that withBorderTitle produces a top line
// whose cell width exactly equals the original (undecorated) top border width
// for various box widths and title lengths.
func TestWithBorderTitleWidth(t *testing.T) {
	cases := []struct {
		boxW  int
		title string
	}{
		{30, "Short"},
		{30, "A title that is exactly the right length"},
		{40, "Medium title"},
		{60, "A very long title that must be truncated because it exceeds the available space in the box"},
		{10, "x"},
		{8, "ab"},
	}
	for _, tc := range cases {
		// Build a fake rendered box: the real top border line width is boxW+4
		// (2 margin + 1 corner + boxW dashes + 1 corner).
		rawTopLine := "  ╭" + strings.Repeat("─", tc.boxW) + "╮"
		fakeRendered := rawTopLine + "\n  │ content │\n  ╰" + strings.Repeat("─", tc.boxW) + "╯"
		wantW := ansi.StringWidth(rawTopLine)

		result := withBorderTitle(fakeRendered, tc.title, tc.boxW, false)
		gotTopLine := strings.SplitN(result, "\n", 2)[0]
		gotW := ansi.StringWidth(gotTopLine)

		if gotW != wantW {
			t.Errorf("boxW=%d title=%q: top line width=%d want %d: %q",
				tc.boxW, tc.title, gotW, wantW, gotTopLine)
		}
	}
}

// TestDetailScrollEstimatesAfterTitleRemoval checks that the scroll cursor
// estimate (estimateDetailCursorLine) still points to the first field row
// (fieldStartDate) at line 0 now that the title is on the border.
func TestDetailScrollEstimatesAfterTitleRemoval(t *testing.T) {
	task := todo.New("scroll estimate test")
	m := initialModel(&fakeRepo{todos: []todo.Todo{task}})
	m.termWidth = 120
	m.termHeight = 40
	m.cursor = 0
	m.tab = tabTasks
	m.pane = paneDetail
	m.detail.field = fieldStartDate

	line := m.estimateDetailCursorLine()
	if line != 0 {
		t.Errorf("fieldStartDate cursor line = %d, want 0 (title now on border)", line)
	}

	m.detail.field = fieldDueDate
	line = m.estimateDetailCursorLine()
	if line != 1 {
		t.Errorf("fieldDueDate cursor line = %d, want 1", line)
	}
}

// min is a local helper (Go 1.21+ has it in the stdlib but to be safe).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
