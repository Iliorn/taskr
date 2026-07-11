package main

// The keymap registry is the single source of truth for keyboard controls.
// Both the footer hint line (renderKeyHints) and the full help overlay
// (renderHelpFullscreen) are generated from it, so the two can never drift
// from each other — the old failure mode where working keys (T, m) were
// simply missing from the help.
//
// It is also the surface where cross-page control consistency is enforced:
// every binding carries a canonical action id, and TestKeymapActionsAre
// Consistent asserts that one action always uses the same key everywhere
// (delete is always x, edit is always r, …). Add a new list tab that binds
// "delete" to some other key and the test fails by construction.

// keyCtx is a bitmask of the UI contexts a binding is live in. A context is a
// tab, or a distinct pane/mode within a tab (the Tasks detail pane and the
// Calendar timeline have their own keysets).
type keyCtx uint16

const (
	ctxTasksList keyCtx = 1 << iota
	ctxTasksDetail
	ctxProjects
	ctxTags
	ctxLearnings
	ctxStats
	ctxCalendar
	ctxCalendarTimeline
	ctxSettings

	// ctxAll marks the global bindings (navigation, help, undo, quit) that are
	// live in every context.
	ctxAll = ctxTasksList | ctxTasksDetail | ctxProjects | ctxTags |
		ctxLearnings | ctxStats | ctxCalendar | ctxCalendarTimeline | ctxSettings
)

// binding is one row of the registry.
type binding struct {
	ctx     keyCtx // where the key is live
	key     string // display form: "x", "←/→", "tab / 1-7"
	action  string // canonical id — consistency is enforced per action
	desc    string // human description (English; tr()'d at render)
	section string // help-overlay grouping
	inHint  bool   // show in the context's footer hint
	primary bool   // keep in the curated short hint when the full line won't fit
}

// Help sections are rendered in this order; a binding's section must be one of
// these. Navigation and App collect the global bindings.
var helpSectionOrder = []string{
	secNavigation, secTasks, secDetail, secTagsProjects,
	secLearnings, secCalendar, secStats, secSettings, secApp,
}

const (
	secNavigation   = "Navigation"
	secTasks        = "Tasks"
	secDetail       = "Detail view"
	secTagsProjects = "Tags & Projects"
	secLearnings    = "Learnings"
	secCalendar     = "Calendar"
	secStats        = "Stats"
	secSettings     = "Settings"
	secApp          = "App"
)

// keymap is the registry. Grouped by section for readability; render order
// within a section follows this slice.
var keymap = []binding{
	// ── Navigation (global) ──────────────────────────────────────────────
	{ctxAll, "↑/↓", "navigate", "navigate list", secNavigation, false, false},
	{ctxAll, "home/end · pgup/pgdn", "listpage", "jump to ends / page through list", secNavigation, false, false},
	// enter has no global meaning — each context defines its own (open details,
	// edit field, activate, cycle) — so it is registered per context, not here.
	{ctxAll, "esc", "back", "go back", secNavigation, false, false},
	{ctxAll, "tab / 1-8", "tabs", "switch tabs", secNavigation, false, false},
	{ctxAll, "?", "help", "toggle this help", secNavigation, false, false},

	// ── Tasks list ───────────────────────────────────────────────────────
	{ctxTasksList, "enter", "detail", "open details", secTasks, true, false},
	{ctxTasksList, "a", "add", "add task (#tag due:date p:high @proj s:M)", secTasks, true, true},
	{ctxTasksList, "d", "done", "toggle done", secTasks, true, true},
	{ctxTasksList, "t", "track", "start/stop time tracking", secTasks, true, true},
	{ctxTasksList, "T", "timeentry", "add manual time entry", secTasks, true, false},
	{ctxTasksList, "p", "priority", "cycle priority low/med/high", secTasks, true, false},
	{ctxTasksList, "r", "edit", "rename task", secTasks, true, false},
	{ctxTasksList, "x", "delete", "delete", secTasks, true, true},
	{ctxTasksList, "n", "notes", "edit notes (opens $EDITOR)", secTasks, true, false},
	{ctxTasksList, "f", "focus", "focus: today + overdue only", secTasks, true, false},
	{ctxTasksList, "s", "sort", "cycle sort order", secTasks, true, true},
	{ctxTasksList, "h", "history", "toggle history", secTasks, true, false},
	{ctxTasksList, "←/→", "foldsub", "expand/collapse subtasks", secTasks, true, false},
	{ctxTasksList, "/", "search", "search", secTasks, true, true},

	// ── Tasks detail pane ────────────────────────────────────────────────
	{ctxTasksDetail, "←/→", "detailsection", "jump section", secDetail, true, false},
	{ctxTasksDetail, "enter", "editfield", "edit field / toggle subtask", secDetail, true, false},
	{ctxTasksDetail, "a", "add", "add tag / dep / comment / learning / subtask", secDetail, true, false},
	{ctxTasksDetail, "d", "done", "toggle subtask done", secDetail, true, false},
	{ctxTasksDetail, "t", "track", "start/stop subtask timer", secDetail, false, false},
	{ctxTasksDetail, "T", "timeentry", "add manual time entry", secDetail, false, false},
	{ctxTasksDetail, "n", "notes", "edit notes (opens $EDITOR)", secDetail, false, false},
	{ctxTasksDetail, "x", "delete", "remove field / delete subtask", secDetail, true, false},
	{ctxTasksDetail, "esc", "back", "back to list", secDetail, true, false},

	// ── Tags & Projects ──────────────────────────────────────────────────
	{ctxProjects | ctxTags, "r", "edit", "rename globally", secTagsProjects, true, false},
	{ctxTags, "m", "merge", "merge tags (Tags tab)", secTagsProjects, true, false},
	{ctxProjects | ctxTags, "x", "delete", "delete globally", secTagsProjects, true, false},
	{ctxTags, "s", "sort", "cycle sort order", secTagsProjects, true, false},
	{ctxProjects | ctxTags, "/", "search", "filter", secTagsProjects, true, true},

	// ── Learnings ────────────────────────────────────────────────────────
	{ctxLearnings, "r", "edit", "edit learning", secLearnings, true, false},
	{ctxLearnings, "x", "delete", "delete learning", secLearnings, true, false},
	{ctxLearnings, "s", "sort", "sort date/alpha", secLearnings, true, false},
	{ctxLearnings, "/", "search", "search", secLearnings, true, true},

	// ── Calendar ─────────────────────────────────────────────────────────
	{ctxCalendar, "←/→ ↑/↓", "calnav", "move by day / week", secCalendar, true, false},
	{ctxCalendar, "[ / ]", "calmonth", "previous / next month", secCalendar, true, false},
	{ctxCalendar, "t", "today", "jump to today", secCalendar, true, false},
	{ctxCalendar, "enter", "calfocus", "focus the day's entries", secCalendar, true, false},
	{ctxCalendarTimeline, "↑/↓", "navigate", "select entry", secCalendar, true, false},
	{ctxCalendarTimeline, "r", "edit", "edit entry times (09:12-10:00 or 45m)", secCalendar, true, false},
	{ctxCalendarTimeline, "x", "delete", "delete selected entry", secCalendar, true, false},
	{ctxCalendarTimeline, "esc", "back", "back", secCalendar, true, false},

	// ── Stats ────────────────────────────────────────────────────────────
	{ctxStats, "enter", "statscycle", "cycle activity range", secStats, true, false},

	// ── Settings ─────────────────────────────────────────────────────────
	{ctxSettings, "↑/↓", "navigate", "select setting", secSettings, true, false},
	{ctxSettings, "←/→", "setchange", "change value / theme", secSettings, true, false},
	{ctxSettings, "enter", "setapply", "apply theme / check for updates", secSettings, true, false},
	{ctxSettings, "y / n", "confirmupdate", "confirm update when one is offered", secSettings, false, false},

	// ── App (global) ─────────────────────────────────────────────────────
	{ctxAll, "u", "undo", "undo last change", secApp, false, false},
	{ctxAll, "q", "quit", "quit", secApp, false, false},
}

// currentKeyCtx maps the live tab/pane/mode to a keyCtx. Only the Tasks tab
// has a distinct detail-pane keyset; the Calendar timeline has its own.
func (m model) currentKeyCtx() keyCtx {
	switch m.tab {
	case tabTasks:
		if m.pane == paneDetail {
			return ctxTasksDetail
		}
		return ctxTasksList
	case tabProjects:
		return ctxProjects
	case tabTags:
		return ctxTags
	case tabLearnings:
		return ctxLearnings
	case tabStats:
		return ctxStats
	case tabCalendar:
		if m.calendar.focusTimeline {
			return ctxCalendarTimeline
		}
		return ctxCalendar
	case tabSettings:
		return ctxSettings
	}
	return ctxTasksList
}

// shortLabel is the terse form used in the curated short hint (the width
// fallback), keyed by action. The full descriptions are written for the help
// overlay and are too long to survive a narrow footer, so primary keys carry a
// one-word label here instead.
var shortLabel = map[string]string{
	"add":    "add",
	"done":   "done",
	"track":  "track",
	"delete": "del",
	"sort":   "sort",
	"search": "search",
}

// hintString renders the footer hint for a context from the registry. With
// primaryOnly it emits just the curated short set (used when the full line
// won't fit the terminal width), with terse labels.
func hintString(ctx keyCtx, primaryOnly bool) string {
	var b []byte
	first := true
	for i := range keymap {
		bd := &keymap[i]
		if bd.ctx&ctx == 0 || !bd.inHint {
			continue
		}
		if primaryOnly && !bd.primary {
			continue
		}
		label := bd.desc
		if primaryOnly {
			if s, ok := shortLabel[bd.action]; ok {
				label = s
			}
		}
		if !first {
			b = append(b, " · "...)
		}
		first = false
		b = append(b, bd.key...)
		b = append(b, ' ')
		b = append(b, tr(label)...)
	}
	return string(b)
}
