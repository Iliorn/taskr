
package main

// ── Layout & rendering constants ──────────────────────────────────────────────
//
// Centralising all magic numbers here makes it easy to tune the UI without
// hunting through render functions.

const (
    // ── Ratio / percentage-based widths ──────────────────────────────────

    // ganttBarWidthDivisor: termWidth / ganttBarWidthDivisor = Gantt bar width
    ganttBarWidthDivisor = 3

    // ganttLabelWidthDivisor: termWidth / ganttLabelWidthDivisor = label column width (≈20%)
    ganttLabelWidthDivisor = 5

    // detailMaxHeightPct: maximum percentage of terminal height the detail panel may occupy
    detailMaxHeightPct = 55

    // projectColWidthPct: percentage of terminal width used for the project name column
    projectColWidthPct = 30

    // overlayWidthPct: percentage of terminal width used for the help overlay
    overlayWidthPct = 60

    // titleColMaxWidthPct: maximum percentage of terminal width the title column may use
    titleColMaxWidthPct = 40

    // ── Minimum / maximum column widths (characters) ─────────────────────

    minGanttBarWidth   = 10
    maxGanttBarWidth   = 60
    minGanttLabelWidth = 20
    maxGanttLabelWidth = 40
    minChartWidth      = 10
    minOverlayWidth    = 50
    minTitleColWidth   = 20
    minProjColWidth    = 20
    maxProjColWidth    = 50
    minTagBarWidth     = 10
    maxTagBarWidth     = 60
    minInnerWidth      = 20

    // ── Fixed column widths (characters) ─────────────────────────────────

    ganttSuffixWidth   = 16
    ganttChartPadding  = 8  // border + padding offset subtracted from chartW
    tagLabelColWidth   = 24
    projCountColWidth  = 10
    projDoneColWidth   = 10
    commentPrefixLen   = 22
    detailLabelColWidth = 14
    titleColFixedCols  = 42

    // ── Search result list limits ─────────────────────────────────────────

    maxDepSearchResults  = 5
    maxTagSearchResults  = 5
    maxProjSearchResults = 5

    // ── Layout line counts ────────────────────────────────────────────────

    footerHeight      = 1
    minHeaderLines    = 2
    detailBorderLines = 2
    minListPanelLines = 3
    minDetailHeight   = 3
    minListHeight     = 1
)
