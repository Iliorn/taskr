package main

// ── Layout & rendering constants ──────────────────────────────────────────────

const (
	ganttBarWidthDivisor   = 3
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
	maxTagBarWidth     = 60
	minInnerWidth      = 20

	ganttSuffixWidth    = 16
	ganttChartPadding   = 8
	projCountColWidth   = 10
	projDoneColWidth    = 10
	commentPrefixLen    = 22
	detailLabelColWidth = 14

	maxDepSearchResults  = 5
	maxTagSearchResults  = 5
	maxProjSearchResults = 5

	// sizeColW is the Size column on the Tasks list. Was 12 (held "S small" +
	// padding) but the column now shows just a single lowercase letter, so 6
	// (fits the ">Size<" sort indicator) is the minimum that still keeps the
	// header readable.
	sizeColW = 6

	// projectColW is the Project column on the Tasks list. Holds a short
	// project name; longer ones truncate.
	projectColW = 14

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
