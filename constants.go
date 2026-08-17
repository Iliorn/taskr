package main

import "time"

// ── Layout & rendering constants ──────────────────────────────────────────────

const (
	ganttLabelWidthDivisor = 5
	detailMaxHeightPct     = 55
	overlayWidthPct        = 60

	// cursorMark is the one marker for "the row you are on", and cursorGap the
	// blank of the same width for the rows you are not. Every list the app
	// draws uses them — task rows, history, subtasks, tags, projects, the
	// calendar timeline, every section of the detail pane, board cards, the
	// Settings rows, the pickers and the palette.
	//
	// They were once three different glyphs (▶ in the lists, > on the board, →
	// in Settings and the pickers), which read as three different kinds of
	// selection rather than one idea drawn three ways. ▶ won on numbers and on
	// one hard fact: → is already the medium-priority icon (todo.Priority.Icon),
	// so a → cursor would sit in the same row as a → that means something else.
	cursorMark = "▶ "
	cursorGap  = "  "

	// headerHintGap is the least blank space between the tab bar and the "?"
	// in the header, so the hint reads as its own thing rather than as another
	// tab. The bar's width budget subtracts it, which is what keeps a bar that
	// exactly fills its budget from costing the hint a column it needed.
	headerHintGap = 2

	// detailScrollMargin is how many rendered lines of the detail document stay
	// visible past the cursor before the pane scrolls. It is what makes the
	// scroll gradual: the window holds still while the cursor moves inside it
	// and then follows by a line at a time, instead of re-anchoring under a
	// cursor pinned to a fixed row. On a pane too short to hold both margins
	// the two clamps meet and it degrades to the centred anchor.
	detailScrollMargin = 2

	// Shared "name" column (task title, project name, tag) so the
	// gap before the next column follows one rule on every list tab and all four
	// reflow identically on resize.
	nameColWidthPct = 30
	nameColMinWidth = 20
	nameColMaxWidth = 50

	// Activity-chart bar height: never squashed below the floor even on a
	// short window, never past the ceiling even on a tall one (statsGradient
	// runs out around there, and the stats list wants the rows more).
	statsChartMinH = 5
	statsChartMaxH = 12

	minGanttBarWidth   = 10
	maxGanttBarWidth   = 60
	minGanttLabelWidth = 20
	maxGanttLabelWidth = 40
	minChartWidth      = 10
	minOverlayWidth    = 50
	minTitleColWidth   = 20
	minTagBarWidth     = 10
	minInnerWidth      = 20

	// List-tab side-by-side layout (Tasks/Tags): at or above
	// sideBySideMinWidth the list keeps full height on the left and the detail
	// pane becomes an always-on preview column on the right; below it each tab
	// falls back to its stacked layout. The detail column takes sideDetailColPct
	// of the content width, clamped so neither column gets unusably narrow.
	sideBySideMinWidth = 110
	// projDrillMinWidth is the narrowest window where the drilled-in project
	// view can hold its Gantt column beside the task list (the Gantt floor plus
	// the list floor plus the borders between them). Below it the Gantt is
	// dropped and the task list takes the window, the same way the Calendar
	// drops its month grid — two floored columns joined on a narrow window is
	// how lines end up wider than the terminal.
	projDrillMinWidth = sideDetailColMin + minInnerWidth + 4
	sideDetailColPct  = 38
	sideDetailColMin  = 36
	sideDetailColMax  = 56

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

	// Release lookup / self-update. The asset cap is generous next to a ~20 MB
	// binary but bounded, so a wrong URL can't stream forever; the download
	// timeout has to cover a slow connection pulling those megabytes.
	releaseAPITimeout      = 15 * time.Second
	releaseDownloadTimeout = 10 * time.Minute
	maxReleaseJSONBytes    = 1 << 20  // 1 MB of release JSON is already absurd
	maxReleaseAssetBytes   = 96 << 20 // 96 MB

	maxDepSearchResults  = 5
	maxTagSearchResults  = 5
	maxProjSearchResults = 5

	// maxPaletteResults is how many command-palette rows are shown at once. The
	// palette replaces the footer hint while it is open, so the budget is what
	// fits there comfortably rather than the whole list.
	maxPaletteResults = 7

	// Quick-add completions render as one chip row under the input, so the cap
	// is what fits a line comfortably rather than the pickers' window height.
	maxQuickAddSuggestions = 5

	// listColGap is the gap between any two columns of the Tasks list — the only
	// spacing number in the layout. Every column is sized by hugColW as "the
	// wider of its header and its widest value, plus this", so no column is ever
	// padded for a value it does not have. Two is what separates two columns
	// legibly; the cells it saves go to the title and the tags, which are the
	// columns whose content is actually open-ended. The old layout spent five
	// here and assembled it out of asymmetric per-column pads, which only summed
	// to five for the widest value in each column.
	listColGap = 2

	// scoreValW is the widest Score value: "100%". The header is wider, so it is
	// the header that sizes the column — but a shorter translated header must
	// not clip the values, which is what this floor is for. Values are
	// right-aligned in the field: they are numbers, and left-aligning them
	// floated the % sign one cell left on every two-digit score.
	scoreValW = 4

	// dueValMaxW caps the Due column's value field at the full-date worst case,
	// "DD-MM-YY". Below it the column hugs whatever this frame's widest due
	// actually renders to, so an all-"3d" list costs three cells and not eight.
	dueValMaxW = 8

	// projectColCompactW is the Project column's compact baseline on the Tasks
	// list. At wider widths the column can grow past this to reveal the full
	// project name; narrow layouts retain the familiar compact footprint.
	projectColCompactW = 14

	footerHeight   = 1
	minHeaderLines = 2
	// Two border rows plus the shared blank row below a pane's border title.
	detailBorderLines = 3
	minListPanelLines = 3
	minDetailHeight   = 3
	minListHeight     = 1

	statsBarWidth   = 30
	statsLabelWidth = 22
	statsValueWidth = 12

	calPanelWidth = 22

	// calSideBySideMinWidth is the narrowest window that fits the fixed-width
	// month grid beside a usable day timeline (grid + timeline floor + the
	// borders/gap between them). Below it buildCalendarContent drops the grid
	// rather than emitting lines wider than the terminal.
	calSideBySideMinWidth = calPanelWidth + minInnerWidth + 4
)
