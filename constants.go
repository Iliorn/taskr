package main

// ── Layout & rendering constants ──────────────────────────────────────────────

const (
	ganttLabelWidthDivisor = 5
	detailMaxHeightPct     = 55
	overlayWidthPct        = 60

	// Shared "name" column (task title, project name, tag, learning text) so the
	// gap before the next column follows one rule on every list tab and all four
	// reflow identically on resize.
	nameColWidthPct = 30
	nameColMinWidth = 20
	nameColMaxWidth = 50

	minGanttBarWidth   = 10
	maxGanttBarWidth   = 60
	minGanttLabelWidth = 20
	maxGanttLabelWidth = 40
	minChartWidth      = 10
	minOverlayWidth    = 50
	minTitleColWidth   = 20
	minTagBarWidth     = 10
	minInnerWidth      = 20

	// List-tab side-by-side layout (Tasks/Learnings/Tags): at or above
	// sideBySideMinWidth the list keeps full height on the left and the detail
	// pane becomes an always-on preview column on the right; below it each tab
	// falls back to its stacked layout. The detail column takes sideDetailColPct
	// of the content width, clamped so neither column gets unusably narrow.
	sideBySideMinWidth = 110
	sideDetailColPct   = 38
	sideDetailColMin   = 36
	sideDetailColMax   = 56

	ganttSuffixWidth  = 16
	ganttChartPadding = 8

	// Projects tab count columns. Sized so the typical 1-2-digit count leaves a
	// ~5-char visible gap before the next column, matching the Tasks tab's
	// score→due→size rhythm. Active column holds "N active" (max 10 chars for
	// "999 active"); Done column holds "N done" (max 8 chars for "999 done").
	projCountColWidth = 13
	projDoneColWidth  = 11

	commentPrefixLen    = 22
	detailLabelColWidth = 14

	maxDepSearchResults  = 5
	maxTagSearchResults  = 5
	maxProjSearchResults = 5

	// sizeColW is the Size column on the Tasks list. Rendered as 2-left + letter
	// + 5-right asymmetric pad: the 2 left spaces extend the Due column's
	// 3-trailing into the 5-char inter-column gap; the 5 right spaces form the
	// same gap on the way to the Project column. Header uses padCenter at this
	// width so "Size" / ">Size<" stay centered.
	sizeColW = 8

	// projectColW is the Project column on the Tasks list. Holds a short
	// project name; longer ones truncate.
	projectColW = 14

	// scoreColW is the Score column on the active Tasks list. Wide enough for
	// the ">Score<" sort indicator (7 runes) plus a one-char buffer so it never
	// butts up against the Due column. The history view keeps the wider 12-col
	// width since its "Completed" header is longer.
	scoreColW = 8

	// dueColW is the maximum width of the Due column on the active Tasks list;
	// taskListCols sizes the column to its actual content and caps it here. The
	// cap is the full-date worst case: "DD-MM-YY" (8) plus the 3-trailing-space
	// gap which, with the 2-space left pad of the centered Size column, equals
	// the 5-space gap a typical "X.X" score leaves before Due. History keeps a
	// fixed 12-col width to match its Completed column.
	dueColW = 11

	footerHeight      = 1
	minHeaderLines    = 2
	detailBorderLines = 2
	minListPanelLines = 3
	minDetailHeight   = 3
	minListHeight     = 1

	statsBarWidth   = 30
	statsLabelWidth = 22
	statsValueWidth = 12

	calPanelWidth = 22
)
