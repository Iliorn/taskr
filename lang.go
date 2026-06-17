package main

import (
	"time"

	"taskr/todo"
)

// ── Localization ──────────────────────────────────────────────────────────────
//
// Translation follows the gettext convention: the English string is the lookup
// key, so call sites stay readable (tr("Settings")) and any string that lacks a
// translation simply falls back to its English source. Adding a new language is
// therefore just a new entry in `translations` plus its date-name tables — no
// call sites change. The active language is a package-level global, mirroring
// the theme pattern (applyTheme); rendering code reads it through the helpers
// below rather than receiving it as a parameter.

type language string

const (
	langEN language = "en"
	langDA language = "da"
)

// availableLanguages is the cycle order used by the settings toggle.
var availableLanguages = []language{langEN, langDA}

func (l language) displayName() string {
	switch l {
	case langDA:
		return "Dansk"
	default:
		return "English"
	}
}

// activeLang is the language all rendering reads from. English is the source
// language, so it needs no translation table.
var activeLang = langEN

// applyLang sets the active language from a stored code, defaulting to English
// for empty or unknown values.
func applyLang(code string) {
	for _, l := range availableLanguages {
		if string(l) == code {
			activeLang = l
			return
		}
	}
	activeLang = langEN
}

// tr translates an English source string into the active language, falling back
// to the source itself when no translation exists. Format strings are translated
// by their template (e.g. tr("%d active")) and then fed to fmt.Sprintf.
func tr(s string) string {
	if activeLang == langEN {
		return s
	}
	if table, ok := translations[activeLang]; ok {
		if t, ok := table[s]; ok {
			return t
		}
	}
	return s
}

// ── Date names ────────────────────────────────────────────────────────────────
//
// Go's time package has no locale support, so the few date layouts the UI shows
// with month/weekday *names* are composed by hand from these tables. Purely
// numeric layouts ("02-01-06", "15:04", "02-01") need no translation.

var monthNames = map[language][12]string{
	langDA: {"Januar", "Februar", "Marts", "April", "Maj", "Juni",
		"Juli", "August", "September", "Oktober", "November", "December"},
}

var monthAbbrevs = map[language][12]string{
	langDA: {"Jan", "Feb", "Mar", "Apr", "Maj", "Jun",
		"Jul", "Aug", "Sep", "Okt", "Nov", "Dec"},
}

// Weekday tables are indexed by time.Weekday (Sunday = 0).
var weekdayNames = map[language][7]string{
	langDA: {"Søndag", "Mandag", "Tirsdag", "Onsdag", "Torsdag", "Fredag", "Lørdag"},
}

var weekdayAbbrevs = map[language][7]string{
	langDA: {"Søn", "Man", "Tir", "Ons", "Tor", "Fre", "Lør"},
}

var weekdayInitials = map[language][7]rune{
	langEN: {'S', 'M', 'T', 'W', 'T', 'F', 'S'},
	langDA: {'S', 'M', 'T', 'O', 'T', 'F', 'L'},
}

// Monday-first two-letter column header for the month grid.
var weekdayHeader = map[language]string{
	langEN: "Mo Tu We Th Fr Sa Su",
	langDA: "Ma Ti On To Fr Lø Sø",
}

// localizedMonthYear renders the calendar title ("January 2006" equivalent).
func localizedMonthYear(t time.Time) string {
	if activeLang == langEN {
		return t.Format("January 2006")
	}
	names := monthNames[activeLang]
	return names[int(t.Month())-1] + t.Format(" 2006")
}

// localizedDayDateAbbrev renders the timeline header ("Mon 02 Jan 2006" equiv).
func localizedDayDateAbbrev(t time.Time) string {
	if activeLang == langEN {
		return t.Format("Mon 02 Jan 2006")
	}
	wd := weekdayAbbrevs[activeLang][int(t.Weekday())]
	mon := monthAbbrevs[activeLang][int(t.Month())-1]
	return wd + t.Format(" 02 ") + mon + t.Format(" 2006")
}

// localizedWeekday returns the full weekday name (stats wide bars).
func localizedWeekday(wd time.Weekday) string {
	if activeLang == langEN {
		return wd.String()
	}
	return weekdayNames[activeLang][int(wd)]
}

// localizedWeekdayShort returns the 3-letter weekday abbreviation (stats bars).
func localizedWeekdayShort(wd time.Weekday) string {
	if activeLang == langEN {
		return wd.String()[:3]
	}
	return weekdayAbbrevs[activeLang][int(wd)]
}

// localizedWeekdayInitial returns the single-letter weekday label (narrow bars).
func localizedWeekdayInitial(wd time.Weekday) rune {
	if init, ok := weekdayInitials[activeLang]; ok {
		return init[int(wd)]
	}
	return weekdayInitials[langEN][int(wd)]
}

// localizedWeekdayHeader returns the Monday-first month-grid column header.
func localizedWeekdayHeader() string {
	if h, ok := weekdayHeader[activeLang]; ok {
		return h
	}
	return weekdayHeader[langEN]
}

// trPriority localizes a task priority word at the view layer; the todo domain
// package stays framework- and locale-free.
func trPriority(p todo.Priority) string {
	return tr(p.String())
}

// trSize localizes the size word for the same reason — the todo package keeps
// only the English source word.
func trSize(s todo.Size) string {
	return tr(s.String())
}

// ── Translation tables ──────────────────────────────────────────────────────
//
// Keyed by English source string. Keep entries grouped by where they appear so
// new strings are easy to slot in. Missing keys fall back to English.

var translations = map[language]map[string]string{
	langDA: daTranslations,
}

var daTranslations = map[string]string{
	// Header / chrome
	"? shortcuts":                                "? genveje",
	"⚡ FOCUS: today + overdue only (f to toggle)": "⚡ FOKUS: kun i dag + forfaldne (f for at skifte)",
	"(untagged)": "(uden mærke)",

	// Tab labels (number prefix kept; only the word is translated)
	"1:Tasks":     "1:Opgaver",
	"2:Calendar":  "2:Kalender",
	"3:Projects":  "3:Projekter",
	"4:Tags":      "4:Mærker",
	"5:Learnings": "5:Læring",
	"6:Stats":     "6:Statistik",
	"7:Settings":  "7:Indstillinger",

	// Key hints (footer)
	"←/→ pages · enter edit · a add · d toggle · x remove · n notes · esc back":                                          "←/→ sider · enter rediger · a tilføj · d skift · x fjern · n noter · esc tilbage",
	"enter detail · a add · d done · t track · p prio · r rename · x del · n notes · f focus · s sort · h history · / search": "enter detalje · a tilføj · d færdig · t tid · p prio · r omdøb · x slet · n noter · f fokus · s sortér · h historik · / søg",
	"j/k nav · r rename · x delete · / filter":                                                                            "j/k navigér · r omdøb · x slet · / filtrér",
	"j/k nav · r rename · x delete · s sort · / filter":                                                                   "j/k navigér · r omdøb · x slet · s sortér · / filtrér",
	"j/k nav · r edit · x delete · s sort · / search":                                                                     "j/k navigér · r rediger · x slet · s sortér · / søg",
	"enter · cycle activity range":                                                                                        "enter · skift aktivitetsperiode",
	"j/k select entry · r edit times · x delete · esc back":                                                              "j/k vælg post · r rediger tider · x slet · esc tilbage",
	"←/→ day · ↑/↓ week · [ ] month · t today · enter entries":                                                            "←/→ dag · ↑/↓ uge · [ ] måned · t i dag · enter poster",
	"↑/↓ select · ←/→ change theme · enter activate":                                                                      "↑/↓ vælg · ←/→ skift · enter aktivér",

	// Footer / timer
	" · t to stop":            " · t for at stoppe",
	"create new tag: ":        "opret nyt mærke: ",
	"create new project: ":    "opret nyt projekt: ",

	// Help screen
	"Keyboard shortcuts":     "Tastaturgenveje",
	"Press ? or esc to close": "Tryk ? eller esc for at lukke",
	"Navigation":             "Navigation",
	"navigate list":          "navigér liste",
	"open details":           "åbn detaljer",
	"go back":                "gå tilbage",
	"switch tabs":            "skift faneblade",
	"close help":             "luk hjælp",
	"Tasks":                  "Opgaver",
	"add task (quick-add: #tag due:date p:high @proj)": "tilføj opgave (hurtig: #mærke due:dato p:high @proj)",
	"rename task":            "omdøb opgave",
	"toggle done":            "skift færdig",
	"start/stop time tracking": "start/stop tidsregistrering",
	"cycle priority low/med/high": "skift prioritet lav/mel/høj",
	"delete":                 "slet",
	"edit notes (opens $EDITOR)": "rediger noter (åbner $EDITOR)",
	"focus: today + overdue only": "fokus: kun i dag + forfaldne",
	"toggle history":         "skift historik",
	"cycle sort order":       "skift sorteringsrækkefølge",
	"expand/collapse subtasks": "fold deludtryk ud/ind",
	"search":                 "søg",
	"Detail view":            "Detaljevisning",
	"switch pages":           "skift sider",
	"edit field / toggle subtask": "rediger felt / skift delopgave",
	"add tag / dep / comment / learning / subtask": "tilføj mærke / afh. / kommentar / læring / delopgave",
	"toggle subtask done":    "skift delopgave færdig",
	"remove field / delete subtask": "fjern felt / slet delopgave",
	"Tags & Projects":        "Mærker & Projekter",
	"rename globally":        "omdøb globalt",
	"delete globally":        "slet globalt",
	"toggle sort":            "skift sortering",
	"filter":                 "filtrér",
	"Learnings":              "Læring",
	"edit learning":          "rediger læring",
	"delete learning":        "slet læring",
	"sort date/alpha":        "sortér dato/alfabetisk",
	"Calendar (tab 2)":       "Kalender (fane 2)",
	"move by day / week":     "flyt med dag / uge",
	"previous / next month":  "forrige / næste måned",
	"jump to today":          "hop til i dag",
	"focus the day's entries": "fokusér dagens poster",
	"edit entry times (09:12-10:00 or 45m)": "rediger posttider (09:12-10:00 eller 45m)",
	"delete selected entry":  "slet valgt post",
	"Stats (tab 6)":          "Statistik (fane 6)",
	"switch to stats view":   "skift til statistik",
	"Settings (tab 7)":       "Indstillinger (fane 7)",
	"select setting":         "vælg indstilling",
	"change theme":           "skift tema",
	"apply theme / check for updates": "anvend tema / søg opdateringer",
	"confirm update when one is offered": "bekræft opdatering når en tilbydes",
	"App":                    "App",
	"undo last change":       "fortryd sidste ændring",
	"quit":                   "afslut",
	"Date input":             "Datoindtastning",
	"exact date (e.g. 15-06-25)": "præcis dato (f.eks. 15-06-25)",
	"today's date":           "dagens dato",
	"tomorrow":               "i morgen",
	"7 days from now":        "7 dage fra nu",
	"1 month from now":       "1 måned fra nu",
	"next occurrence of weekday": "næste forekomst af ugedag",
	"relative days/weeks/months": "relative dage/uger/måneder",

	// Stats detail
	"Last 30 days":               "Sidste 30 dage",
	"Last 26 weeks":              "Sidste 26 uger",
	"Last 7 days":                "Sidste 7 dage",
	"%d done":                    "%d færdige",
	"No completions in this range.": "Ingen afsluttede i denne periode.",
	": 1 block = 1 completed task": ": 1 blok = 1 afsluttet opgave",

	// Stats list — sections & labels
	"  Workload":            "  Arbejdsbyrde",
	"Overdue":               "Forfaldne",
	"Due today":             "Forfalder i dag",
	"Due this week":         "Forfalder denne uge",
	"Active total":          "Aktive i alt",
	"Created":               "Oprettet",
	"Completed":             "Afsluttet",
	"  Net backlog":         "  Netto-efterslæb",
	"+%d ▲ growing":         "+%d ▲ vokser",
	"%d ▼ shrinking":        "%d ▼ skrumper",
	"±0 → steady":           "±0 → stabil",
	"%d done vs %d  %s":     "%d færdige mod %d  %s",
	"Flow (last 7 days)":    "Flow (sidste 7 dage)",
	"vs last week":          "mod sidste uge",
	"Flow (last 30 days)":   "Flow (sidste 30 dage)",
	"vs prior 30d":          "mod forrige 30d",
	"  Throughput":          "  Gennemløb",
	"  Time to done (30d)":  "  Færdigtid (30d)",
	"median ":               "median ",
	"none yet":              "ingen endnu",
	"  Median active age":   "  Median aktiv alder",
	"  Oldest active":       "  Ældste aktive",
	"  Active by priority":  "  Aktive efter prioritet",
	"↑ High":                "↑ Høj",
	"→ Medium":              "→ Mellem",
	"↓ Low":                 "↓ Lav",
	"  Completion velocity": "  Afslutningshastighed",
	"Today":                 "I dag",
	"This week":             "Denne uge",
	"This month":            "Denne måned",
	"  Avg (7d)":            "  Gns. (7d)",
	"%.1f tasks/day":        "%.1f opgaver/dag",

	// Priority words (todo.Priority.String)
	"high":   "høj",
	"medium": "mellem",
	"low":    "lav",

	// List headers / sort
	"Task":            "Opgave",
	"Completed tasks": "Afsluttede opgaver",
	"Start":           "Start",
	"Due":             "Forfald",
	"Priority":        "Prioritet",
	">Due<":           ">Forfald<",
	">Start<":         ">Start<",
	">Priority<":      ">Prioritet<",
	"Tags":            "Mærker",
	"sort:":           "sortér:",

	// Tag list / detail
	"  No tags match your filter.":                  "  Ingen mærker matcher dit filter.",
	"  No tags yet. Add tags to tasks in the detail view.": "  Ingen mærker endnu. Tilføj mærker til opgaver i detaljevisningen.",
	"  Tags group related tasks; this tab shows progress per tag.": "  Mærker grupperer beslægtede opgaver; denne fane viser fremskridt per mærke.",
	"  Tag":      "  Mærke",
	"Progress":   "Fremskridt",
	" %3d%% (%d done / %d total)": " %3d%% (%d færdige / %d i alt)",
	"avg age ":   "gns. alder ",
	"⏱ time spent ": "⏱ tid brugt ",
	"  No tag selected.":      "  Intet mærke valgt.",
	"  (untagged)":            "  (uden mærke)",
	"%d task":                 "%d opgave",
	"%d tasks":                "%d opgaver",
	" · enter: filter)":       " · enter: filtrér)",
	" · enter: filter · r: rename)": " · enter: filtrér · r: omdøb)",
	"  %d active · %d done · %d overdue": "  %d aktive · %d færdige · %d forfaldne",
	"  often with: ":          "  ofte med: ",
	"  No tasks carry this tag.": "  Ingen opgaver har dette mærke.",
	"  … and %d more":         "  … og %d mere",
	"  due: ":                 "  forfald: ",

	// Learnings list / detail
	"  No learnings match your search.": "  Ingen læring matcher din søgning.",
	"  No learnings yet. Add learnings from a task's detail view.": "  Ingen læring endnu. Tilføj læring fra en opgaves detaljevisning.",
	"  A learning is a takeaway you save on a task to keep for later.": "  En læring er en indsigt du gemmer på en opgave til senere.",
	"Learning":        "Læring",
	"Date":            "Dato",
	"  No learning selected.": "  Ingen læring valgt.",
	"Source task:  ":  "Kildeopgave:  ",
	"[done]":          "[færdig]",
	"[task removed]":  "[opgave fjernet]",
	"Date:         ":  "Dato:         ",
	"Tags:         ":  "Mærker:       ",
	"none":            "ingen",

	// Task list empty states
	"  No tasks match your search.":      "  Ingen opgaver matcher din søgning.",
	"  No tasks due today or overdue. Nice!": "  Ingen opgaver forfalder i dag eller er forfaldne. Flot!",
	"  No tasks yet. Press 'a' to add one.": "  Ingen opgaver endnu. Tryk 'a' for at tilføje en.",
	"  Try:  ": "  Prøv:  ",
	// Free text (title/tag/project) is localized; the quick-add keywords
	// due:/friday/p:high stay English because the parser only accepts English.
	"Buy milk #shopping due:friday p:high @home": "Køb mælk #indkøb due:friday p:high @hjem",
	"  Press ? for all keyboard shortcuts.": "  Tryk ? for alle tastaturgenveje.",
	"  No completed tasks match your search.": "  Ingen afsluttede opgaver matcher din søgning.",
	"  No completed tasks yet.": "  Ingen afsluttede opgaver endnu.",

	// Projects
	"  No projects match your search.":          "  Ingen projekter matcher din søgning.",
	"  No projects yet. Add a project to a task first.": "  Ingen projekter endnu. Tilføj et projekt til en opgave først.",
	"  A project groups its tasks into a timeline on this tab.": "  Et projekt samler dets opgaver i en tidslinje på denne fane.",
	"Project":      "Projekt",
	"Active":       "Aktive",
	"Done":         "Færdige",
	"  Timeline":   "  Tidslinje",
	"today:":       "i dag:",
	"  No tasks in this project.": "  Ingen opgaver i dette projekt.",
	"%d active":    "%d aktive",
	"%d overdue":   "%d forfaldne", // "%d done" shared with the stats section above

	// Detail pages
	"not set":     "ikke sat",
	" ⚠ overdue":  " ⚠ forfalden",
	"Start date":  "Startdato",
	"Due date":    "Forfaldsdato",
	"Notes":       "Noter",
	"none (press enter or 'n' to edit)": "ingen (tryk enter eller 'n' for at redigere)",
	"Created:":    "Oprettet:",
	"Modified:":   "Ændret:",
	"%s (%d entries)": "%s (%d poster)",
	" ◉ tracking": " ◉ registrerer",
	"Time spent:": "Tid brugt:",
	"Completed on:": "Afsluttet:",
	"Tags:":       "Mærker:",
	"No tags. Press 'a' to add one.": "Ingen mærker. Tryk 'a' for at tilføje et.",
	"Subtasks:":   "Delopgaver:",
	"No subtasks. Press 'a' to add one.": "Ingen delopgaver. Tryk 'a' for at tilføje en.",
	"%s[?] unknown subtask": "%s[?] ukendt delopgave",
	"Dependencies:": "Afhængigheder:",
	"No dependencies. Press 'a' to add one.": "Ingen afhængigheder. Tryk 'a' for at tilføje en.",
	"%s[?] unknown task": "%s[?] ukendt opgave",
	"Learnings:":  "Læring:",
	"No learnings yet. Press 'a' to add one.": "Ingen læring endnu. Tryk 'a' for at tilføje en.",
	"Comments:":   "Kommentarer:",
	"No comments yet. Press 'a' to add one.": "Ingen kommentarer endnu. Tryk 'a' for at tilføje en.",

	// Calendar
	"month ":      "måned ",
	"day ":        "dag ",
	"%d entries · %s": "%d poster · %s",
	"1 entry · ":  "1 post · ",
	"  No activity on this day.": "  Ingen aktivitet på denne dag.",
	"  Press t on a task (tab 1) to start tracking.": "  Tryk t på en opgave (fane 1) for at starte registrering.",
	" now ":       " nu ",

	// Settings
	"Theme":             "Tema",
	"Language":          "Sprog",
	"Version":           "Version",
	"Check for updates": "Søg efter opdateringer",
	"press enter to check": "tryk enter for at søge",
	"Settings":          "Indstillinger",

	// Update / status / errors
	"Update failed":               "Opdatering mislykkedes",
	"Updated! Restart taskr to apply.": "Opdateret! Genstart taskr for at anvende.",
	"Error saving settings: %v": "Kunne ikke gemme indstillinger: %v",
	"Updated — restart to apply":  "Opdateret — genstart for at anvende",
	"Check failed":                "Søgning mislykkedes",
	"Up to date (":                "Opdateret (",
	"Update available: ":          "Opdatering tilgængelig: ",
	" is available — update now? (y/n)": " er tilgængelig — opdatér nu? (y/n)",
	"Checking…":                   "Søger…",
	"Updating…":                   "Opdaterer…",
	"Nothing to undo":             "Intet at fortryde",
	"Undid: %s":                   "Fortrød: %s",
	"No editor found — set EDITOR permanently, e.g: setx EDITOR notepad (then restart taskr)": "Ingen editor fundet — sæt EDITOR permanent, f.eks: setx EDITOR notepad (genstart derefter taskr)",
	"No editor found — set $EDITOR permanently, e.g: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish": "Ingen editor fundet — sæt $EDITOR permanent, f.eks: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish",
	"Editor failed — falling back to notepad": "Editor fejlede — falder tilbage til notepad",
	"Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'": "Ugyldig dato - brug dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday' eller '+3d'",

	// Confirm prompts
	"Delete '%s'? (y/n)":                          "Slet '%s'? (y/n)",
	"Delete learning '%s'? (y/n)":                 "Slet læring '%s'? (y/n)",
	"Delete tag '#%s' from ALL tasks? (y/n)":      "Slet mærke '#%s' fra ALLE opgaver? (y/n)",
	"Delete this comment? (y/n)":                  "Slet denne kommentar? (y/n)",
	"Remove project '%s' from this task? (y/n)":   "Fjern projekt '%s' fra denne opgave? (y/n)",
	"Remove tag '#%s' from this task? (y/n)":      "Fjern mærke '#%s' fra denne opgave? (y/n)",
	"Remove this dependency? (y/n)":               "Fjern denne afhængighed? (y/n)",
	"Delete subtask '%s'? (y/n)":                  "Slet delopgave '%s'? (y/n)",
	"Delete %s entry for '%s'? (y/n)":             "Slet %s-post for '%s'? (y/n)",

	// Input placeholders
	"Search... (use # to filter by tag)":          "Søg... (brug # for at filtrere på mærke)",
	"Search for task to add as dependency...":     "Søg efter opgave at tilføje som afhængighed...",
	"Search or create tag...":                     "Søg eller opret mærke...",
	"Search or create project...":                 "Søg eller opret projekt...",
	"Filter tags...":                              "Filtrér mærker...",
	"Search learnings... (use # to filter by tag)": "Søg i læring... (brug # for at filtrere på mærke)",

	// Inline edit/add placeholders (set when a text-entry mode opens)
	"New task (use #tag due:date p:high @project)...": "Ny opgave (brug #mærke due:dato p:high @projekt)...",
	"HH:MM-HH:MM or duration (45m, 1h30m)...":         "TT:MM-TT:MM eller varighed (45m, 1t30m)...",
	"Edit tag name...":   "Rediger mærkenavn...",
	"Edit task title...": "Rediger opgavetitel...",
	"Edit learning...":   "Rediger læring...",
	"Add comment...":     "Tilføj kommentar...",
	"Add learning...":    "Tilføj læring...",
	"Add subtask...":     "Tilføj delopgave...",
	"Edit comment...":    "Rediger kommentar...",
	"Start date (dd-mm-yy, 'today', 'next week', '+3d')...": "Startdato (dd-mm-yy, 'today', 'next week', '+3d')...",
	"Due date (dd-mm-yy, 'today', 'next week', '+3d')...":   "Forfaldsdato (dd-mm-yy, 'today', 'next week', '+3d')...",
}
