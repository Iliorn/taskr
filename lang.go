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
	langDE language = "de"
)

// availableLanguages is the cycle order used by the settings toggle.
var availableLanguages = []language{langEN, langDA, langDE}

func (l language) displayName() string {
	switch l {
	case langDA:
		return "Dansk"
	case langDE:
		return "Deutsch"
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
	langDE: {"Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember"},
}

var monthAbbrevs = map[language][12]string{
	langDA: {"Jan", "Feb", "Mar", "Apr", "Maj", "Jun",
		"Jul", "Aug", "Sep", "Okt", "Nov", "Dec"},
	langDE: {"Jan", "Feb", "Mär", "Apr", "Mai", "Jun",
		"Jul", "Aug", "Sep", "Okt", "Nov", "Dez"},
}

// Weekday tables are indexed by time.Weekday (Sunday = 0).
var weekdayNames = map[language][7]string{
	langDA: {"Søndag", "Mandag", "Tirsdag", "Onsdag", "Torsdag", "Fredag", "Lørdag"},
	langDE: {"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
}

var weekdayAbbrevs = map[language][7]string{
	langDA: {"Søn", "Man", "Tir", "Ons", "Tor", "Fre", "Lør"},
	langDE: {"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
}

var weekdayInitials = map[language][7]rune{
	langEN: {'S', 'M', 'T', 'W', 'T', 'F', 'S'},
	langDA: {'S', 'M', 'T', 'O', 'T', 'F', 'L'},
	langDE: {'S', 'M', 'D', 'M', 'D', 'F', 'S'},
}

// Monday-first two-letter column header for the month grid.
var weekdayHeader = map[language]string{
	langEN: "Mo Tu We Th Fr Sa Su",
	langDA: "Ma Ti On To Fr Lø Sø",
	langDE: "Mo Di Mi Do Fr Sa So",
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

// trRecurrence renders a recurrence rule for display. Canonical rules
// (daily/weekly/monthly/yearly/weekdays) are translated; "every:Nd|w|m|y" is
// kept as-is, since the prefix is a recognizable English keyword and the
// number+unit is locale-neutral.
func trRecurrence(rule string) string {
	switch rule {
	case "daily", "weekly", "monthly", "yearly", "weekdays":
		return tr(rule)
	}
	return rule
}

// ── Translation tables ──────────────────────────────────────────────────────
//
// Keyed by English source string. Keep entries grouped by where they appear so
// new strings are easy to slot in. Missing keys fall back to English.

var translations = map[language]map[string]string{
	langDA: daTranslations,
	langDE: deTranslations,
}

var daTranslations = map[string]string{
	// Header / chrome
	"? shortcuts":                               "? genveje",
	"? shortcuts · ctrl+k commands":             "? genveje · ctrl+k kommandoer",
	"Type a command…":                           "Skriv en kommando…",
	"No command matches that.":                  "Ingen kommando matcher det.",
	"command palette — find any action by name": "kommandopalet — find enhver handling ved navn",
	"Go to Tasks":                               "Gå til Opgaver",
	"Go to Calendar":                            "Gå til Kalender",
	"Go to Projects":                            "Gå til Projekter",
	"Go to Tags":                                "Gå til Mærker",
	"Go to Board":                               "Gå til Tavle",
	"Go to Stats":                               "Gå til Statistik",
	"Go to Settings":                            "Gå til Indstillinger",
	"⚡ FOCUS: today + overdue only (f to toggle)": "⚡ FOKUS: kun i dag + forfaldne (f for at skifte)",
	"(untagged)": "(uden mærke)",

	// Tab labels (number prefix kept; only the word is translated)
	"1 Tasks":    "1 Opgaver",
	"2 Calendar": "2 Kalender",
	"3 Projects": "3 Projekter",
	"4 Tags":     "4 Mærker",
	"5 Board":    "5 Tavle",
	"6 Stats":    "6 Statistik",
	"7 Settings": "7 Indstillinger",

	// Key hints (footer)
	"←/→ pages · enter edit · a add · d toggle · x remove · n notes · esc back":                                               "←/→ sider · enter rediger · a tilføj · d skift · x fjern · n noter · esc tilbage",
	"←/→ pages · enter edit · a add · x remove · esc back":                                                                    "←/→ sider · enter rediger · a tilføj · x fjern · esc tilbage",
	"a add · d done · t track · x del · s sort · / search · ? more":                                                           "a tilføj · d færdig · t tid · x slet · s sortér · / søg · ? mere",
	"enter detail · a add · d done · t track · p prio · r rename · x del · n notes · f focus · s sort · h history · / search": "enter detalje · a tilføj · d færdig · t tid · p prio · r omdøb · x slet · n noter · f fokus · s sortér · h historik · / søg",
	"j/k nav · r rename · x delete · / filter":                                                                                "j/k navigér · r omdøb · x slet · / filtrér",
	"j/k nav · r rename · m merge · x delete · s sort · / filter":                                                             "j/k navigér · r omdøb · m flet · x slet · s sortér · / filtrér",
	"j/k nav · r edit · x delete · s sort · / search":                                                                         "j/k navigér · r rediger · x slet · s sortér · / søg",
	"enter · cycle activity range":                                                                                            "enter · skift aktivitetsperiode",
	"j/k select entry · r edit times · x delete · esc back":                                                                   "j/k vælg post · r rediger tider · x slet · esc tilbage",
	"←/→ day · ↑/↓ week · [ ] month · t today · enter entries":                                                                "←/→ dag · ↑/↓ uge · [ ] måned · t i dag · enter poster",
	"↑/↓ select · ←/→ change theme · enter activate":                                                                          "↑/↓ vælg · ←/→ skift · enter aktivér",

	// Footer / timer
	"#tag @project due:tomorrow p:high s:l r:weekly dep:^": "#mærke @projekt due:tomorrow p:high s:l r:weekly dep:^",
	" · t to stop":         " · t for at stoppe",
	"create new tag: ":     "opret nyt mærke: ",
	"create new project: ": "opret nyt projekt: ",

	// Help screen
	"Keyboard shortcuts":                          "Tastaturgenveje",
	"Press ? or esc to close":                     "Tryk ? eller esc for at lukke",
	"/ filter  ·  ? or esc to close":              "/ filtrér  ·  ? eller esc for at lukke",
	"type to filter  ·  enter keep  ·  esc clear": "skriv for at filtrere  ·  enter behold  ·  esc ryd",
	"/ filter  ·  esc clear  ·  ? to close":       "/ filtrér  ·  esc ryd  ·  ? for at lukke",
	"No shortcut matches that.":                   "Ingen genvej matcher det.",
	"Quick-add syntax":                            "Hurtig-tilføj syntaks",
	"Search filters":                              "Søgefiltre",
	"Navigation":                                  "Navigation",
	"navigate list":                               "navigér liste",
	"open details":                                "åbn detaljer",
	"go back":                                     "gå tilbage",
	"switch tabs (forward / back / direct)":       "skift faneblad (frem / tilbage / direkte)",
	"Board":                                       "Tavle",
	"focus previous/next column":                  "fokusér forrige/næste kolonne",
	"move card between stages (into Done completes it)": "flyt kort mellem faser (ind i Færdige afslutter)",
	"empty":       "tom",
	"more":        "flere",
	"close help":  "luk hjælp",
	"Tasks":       "Opgaver",
	"Workflow":    "Arbejdsgang",
	"Summary":     "Overblik",
	"Preferences": "Præferencer",
	"Sequencer":   "Sekventering",
	"Overview":    "Oversigt",
	"History":     "Historik",
	"Timeline":    "Tidslinje",
	"Activity":    "Aktivitet",
	"add task (quick-add: #tag due:date p:high @proj s:M)": "tilføj opgave (hurtig: #mærke due:dato p:high @proj s:M)",
	"rename task":                 "omdøb opgave",
	"toggle done":                 "skift færdig",
	"start/stop time tracking":    "start/stop tidsregistrering",
	"cycle priority low/med/high": "skift prioritet lav/mel/høj",
	"delete":                      "slet",
	"edit notes (opens $EDITOR)":  "rediger noter (åbner $EDITOR)",
	"ctrl+e  edit in $EDITOR":     "ctrl+e  rediger i $EDITOR",
	"focus: today + overdue only": "fokus: kun i dag + forfaldne",
	"toggle history":              "skift historik",
	"cycle sort order":            "skift sorteringsrækkefølge",
	"expand/collapse subtasks":    "fold delopgaver ud/ind",
	"search":                      "søg",
	"Detail view":                 "Detaljevisning",
	"jump section":                "hop til sektion",
	"edit field / open subtask":   "rediger felt / åbn delopgave",
	"add tag / dep / comment / learning / subtask": "tilføj mærke / afh. / kommentar / læring / delopgave",
	"quick add tag":                         "tilføj hurtigt mærke",
	"quick add / change project":            "tilføj / skift projekt hurtigt",
	"toggle subtask done":                   "skift delopgave færdig",
	"rename subtask / edit time entry":      "omdøb delopgave / rediger tidsregistrering",
	"remove field / delete subtask":         "fjern felt / slet delopgave",
	"Tags & Projects":                       "Mærker & Projekter",
	"Inside a tag / project":                "Inde i et mærke / projekt",
	"open the tasks in it":                  "åbn opgaverne i det",
	"new task in it":                        "ny opgave i det",
	"show its tasks on the Tasks tab":       "vis opgaverne på Opgaver-fanen",
	"delete task":                           "slet opgave",
	"back to the list":                      "tilbage til listen",
	"rename globally":                       "omdøb globalt",
	"delete globally":                       "slet globalt",
	"filter":                                "filtrér",
	"Learnings":                             "Læring",
	"edit learning":                         "rediger læring",
	"delete learning":                       "slet læring",
	"sort date/alpha":                       "sortér dato/alfabetisk",
	"Calendar (tab 2)":                      "Kalender (fane 2)",
	"move by day / week":                    "flyt med dag / uge",
	"previous / next month":                 "forrige / næste måned",
	"jump to today":                         "hop til i dag",
	"focus the day's entries":               "fokusér dagens poster",
	"edit entry times (09:12-10:00 or 45m)": "rediger posttider (09:12-10:00 eller 45m)",
	"delete selected entry":                 "slet valgt post",
	"Stats (tab 6)":                         "Statistik (fane 6)",
	"switch to stats view":                  "skift til statistik",
	"Settings (tab 7)":                      "Indstillinger (fane 7)",
	"select setting":                        "vælg indstilling",
	"change theme":                          "skift tema",
	"activate / edit the selected setting":  "aktivér / redigér den valgte indstilling",
	"confirm update when one is offered":    "bekræft opdatering når en tilbydes",
	"App":                                   "App",
	"undo last change":                      "fortryd sidste ændring",
	"quit":                                  "afslut",
	"Date input":                            "Datoindtastning",
	"exact date (e.g. 15-06-25)":            "præcis dato (f.eks. 15-06-25)",
	"today's date":                          "dagens dato",
	"tomorrow":                              "i morgen",
	"7 days from now":                       "7 dage fra nu",
	"1 month from now":                      "1 måned fra nu",
	"next occurrence of weekday":            "næste forekomst af ugedag",
	"relative days/weeks/months":            "relative dage/uger/måneder",

	// Stats detail
	"Last 30 days":                  "Sidste 30 dage",
	"Last 26 weeks":                 "Sidste 26 uger",
	"Last 7 days":                   "Sidste 7 dage",
	"%d done":                       "%d færdige",
	"No completions in this range.": "Ingen afsluttede i denne periode.",
	": 1 block = 1 completed task":  ": 1 blok = 1 afsluttet opgave",

	// Stats list — sections & labels
	"  Workload":            "  Arbejdsbyrde",
	"Overdue":               "Forfaldne",
	"Due today":             "Forfalder i dag",
	"Due this week":         "Forfalder denne uge",
	"Active total":          "Aktive i alt",
	"Seq hit (top-5)":       "Sekvens-hit (top-5)",
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
	"today":                 "i dag", // tasks-list Due column (formatDueShort)
	"This week":             "Denne uge",
	"This month":            "Denne måned",
	"  Avg (7d)":            "  Gns. (7d)",
	"%.1f tasks/day":        "%.1f opgaver/dag",

	// Priority, size, and recurrence words
	"high":     "høj",
	"medium":   "mellem",
	"low":      "lav",
	"small":    "lille",
	"large":    "stor",
	"daily":    "dagligt",
	"weekly":   "ugentligt",
	"monthly":  "månedligt",
	"yearly":   "årligt",
	"weekdays": "hverdage",

	// List headers / sort
	"Task":              "Opgave",
	"Completed tasks":   "Afsluttede opgaver",
	">Completed tasks<": ">Afsluttede opgaver<",
	">Completed<":       ">Afsluttet<",
	"Start":             "Start",
	"Due":               "Forfald",
	"Priority":          "Prioritet",
	">Due<":             ">Forfald<",
	">Start<":           ">Start<",
	">Priority<":        ">Prioritet<",
	"Tags":              "Mærker",
	"sort:":             "sortér:",

	// Tag list / detail
	"  No tags match your filter.":                                 "  Ingen mærker matcher dit filter.",
	"  No tags yet. Add tags to tasks in the detail view.":         "  Ingen mærker endnu. Tilføj mærker til opgaver i detaljevisningen.",
	"  Tags group related tasks; this tab shows progress per tag.": "  Mærker grupperer beslægtede opgaver; denne fane viser fremskridt per mærke.",
	"  Tag":                       "  Mærke",
	"Progress":                    "Fremskridt",
	"Age":                         "Alder",
	"Time":                        "Tid",
	"  No tag selected.":          "  Intet mærke valgt.",
	"  (untagged)":                "  (uden mærke)",
	"%d task":                     "%d opgave",
	"%d tasks":                    "%d opgaver",
	" · enter: open · f: filter)": " · enter: åbn · f: filtrér)",
	" · enter: open · f: filter · r: rename)":             " · enter: åbn · f: filtrér · r: omdøb)",
	" · d: done · t: track · enter: details · esc: back)": " · d: færdig · t: tid · enter: detaljer · esc: tilbage)",
	"  %d active · %d done · %d overdue":                  "  %d aktive · %d færdige · %d forfaldne",
	"  often with: ":                                      "  ofte med: ",
	"  No tasks carry this tag.":                          "  Ingen opgaver har dette mærke.",
	"  … and %d more":                                     "  … og %d mere",
	"  due: ":                                             "  forfald: ",

	// Learnings list / detail
	"  No learnings match your search.":                                "  Ingen læring matcher din søgning.",
	"  No learnings yet. Add learnings from a task's detail view.":     "  Ingen læring endnu. Tilføj læring fra en opgaves detaljevisning.",
	"  A learning is a takeaway you save on a task to keep for later.": "  En læring er en indsigt du gemmer på en opgave til senere.",
	"Learning":                "Læring",
	"Date":                    "Dato",
	"  No learning selected.": "  Ingen læring valgt.",
	"Source task:  ":          "Kildeopgave:  ",
	"[done]":                  "[færdig]",
	"[task removed]":          "[opgave fjernet]",
	"Date:         ":          "Dato:         ",
	"Tags:         ":          "Mærker:       ",
	"none":                    "ingen",

	// Task list empty states
	"  No tasks match your search.":          "  Ingen opgaver matcher din søgning.",
	"  No tasks due today or overdue. Nice!": "  Ingen opgaver forfalder i dag eller er forfaldne. Flot!",
	"  No tasks yet. Press 'a' to add one.":  "  Ingen opgaver endnu. Tryk 'a' for at tilføje en.",
	"  Try:  ":                               "  Prøv:  ",
	// Free text (title/tag/project) is localized; the quick-add keywords
	// due:/friday/p:high stay English because the parser only accepts English.
	"Buy milk #shopping due:friday p:high @home": "Køb mælk #indkøb due:friday p:high @hjem",
	"  Press ? for all keyboard shortcuts.":      "  Tryk ? for alle tastaturgenveje.",
	"  No completed tasks match your search.":    "  Ingen afsluttede opgaver matcher din søgning.",
	"  No completed tasks yet.":                  "  Ingen afsluttede opgaver endnu.",

	// Projects
	"  No projects match your search.":                          "  Ingen projekter matcher din søgning.",
	"  No projects yet. Add a project to a task first.":         "  Ingen projekter endnu. Tilføj et projekt til en opgave først.",
	"  A project groups its tasks into a timeline on this tab.": "  Et projekt samler dets opgaver i en tidslinje på denne fane.",
	"Project":                     "Projekt",
	"Active":                      "Aktive",
	"Done":                        "Færdige",
	"  Timeline":                  "  Tidslinje",
	"today:":                      "i dag:",
	"  No tasks in this project.": "  Ingen opgaver i dette projekt.",
	"%d active":                   "%d aktive",
	"%d overdue":                  "%d forfaldne", // "%d done" shared with the stats section above

	// Detail pages
	"not set":                            "ikke sat",
	" ⚠ overdue":                         " ⚠ forfalden",
	"Start date":                         "Startdato",
	"Due date":                           "Forfaldsdato",
	"Recurrence":                         "Gentagelse",
	"Size":                               "Størrelse",
	"Notes":                              "Noter",
	"none (press enter or 'n' to edit)":  "ingen (tryk enter eller 'n' for at redigere)",
	"Created:":                           "Oprettet:",
	"Modified:":                          "Ændret:",
	"%s (%d entries)":                    "%s (%d poster)",
	" ◉ tracking":                        " ◉ registrerer",
	"Time spent:":                        "Tid brugt:",
	"Completed on:":                      "Afsluttet:",
	"Tags:":                              "Mærker:",
	"No tags. Press 'a' to add one.":     "Ingen mærker. Tryk 'a' for at tilføje et.",
	"Subtasks:":                          "Delopgaver:",
	"No subtasks. Press 'a' to add one.": "Ingen delopgaver. Tryk 'a' for at tilføje en.",
	"%s[?] unknown subtask":              "%s[?] ukendt delopgave",
	"Dependencies:":                      "Afhængigheder:",
	"Inbound dependency — remove it from the other task": "Indgående afhængighed — fjern den fra den anden opgave",
	"No dependencies. Press 'a' to add one.":             "Ingen afhængigheder. Tryk 'a' for at tilføje en.",
	"%s[?] unknown task":                                 "%s[?] ukendt opgave",
	"Learnings:":                                         "Læring:",
	"No learnings yet. Press 'a' to add one.":            "Ingen læring endnu. Tryk 'a' for at tilføje en.",
	"Comments:":                              "Kommentarer:",
	"No comments yet. Press 'a' to add one.": "Ingen kommentarer endnu. Tryk 'a' for at tilføje en.",
	"Time entries:":                          "Tidsregistreringer:",
	"No time entries. Press 'T' to add one.": "Ingen tidsregistreringer. Tryk 'T' for at tilføje en.",

	// Calendar
	"month ":                     "måned ",
	"day ":                       "dag ",
	"%d entries · %s":            "%d poster · %s",
	"1 entry · ":                 "1 post · ",
	"  No activity on this day.": "  Ingen aktivitet på denne dag.",
	"  Press t on a task (tab 1) to start tracking.": "  Tryk t på en opgave (fane 1) for at starte registrering.",
	" now ": " nu ",

	// Settings
	"Theme":                          "Tema",
	"Language":                       "Sprog",
	"tab insert · ↑/↓ pick":          "tab indsæt · ↑/↓ vælg",
	"Board columns":                  "Tavlekolonner",
	"Board columns, comma-separated": "Tavlekolonner, adskilt af komma",
	"Comma-separated column names · Done is always the last column": "Kolonnenavne adskilt af komma · Færdig er altid sidste kolonne",
	"Version":              "Version",
	"Check for updates":    "Søg efter opdateringer",
	"press enter to check": "tryk enter",
	"Settings":             "Indstillinger",

	// Update / status / errors
	"Update failed":                                   "Opdatering mislykkedes",
	"Updated! Restart taskr to apply.":                "Opdateret! Genstart taskr for at anvende.",
	"Error saving settings: %v":                       "Kunne ikke gemme indstillinger: %v",
	"Updated — restart to apply":                      "Opdateret — genstart for at anvende",
	"Check failed":                                    "Søgning mislykkedes",
	"Up to date (":                                    "Opdateret (",
	"Latest release: ":                                "Seneste udgivelse: ",
	" — this is a local build (":                      " — dette er en lokal bygning (",
	"Update available: ":                              "Opdatering tilgængelig: ",
	" — run `brew upgrade taskr`":                     " — kør `brew upgrade taskr`",
	" — install with `brew install iliorn/tap/taskr`": " — installér med `brew install iliorn/tap/taskr`",
	" is available — update now? (y/n)":               " er tilgængelig — opdatér nu? (y/n)",
	"Checking…":                                       "Søger…",
	"Updating…":                                       "Opdaterer…",
	"Nothing to undo":                                 "Intet at fortryde",
	"Undid: %s":                                       "Fortrød: %s",
	"No editor found — set EDITOR permanently, e.g: setx EDITOR notepad (then restart taskr)":                               "Ingen editor fundet — sæt EDITOR permanent, f.eks: setx EDITOR notepad (genstart derefter taskr)",
	"No editor found — set $EDITOR permanently, e.g: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish": "Ingen editor fundet — sæt $EDITOR permanent, f.eks: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish",
	"Editor failed — falling back to notepad":                                                                               "Editor fejlede — falder tilbage til notepad",
	"Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'":                                     "Ugyldig dato - brug dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday' eller '+3d'",
	"Dependency not linked":                         "Afhængighed ikke koblet",
	"Remove project '%s' from ALL its tasks? (y/n)": "Fjern projektet '%s' fra ALLE dets opgaver? (j/n)",

	// Confirm prompts
	"Delete '%s'? (y/n)":                        "Slet '%s'? (y/n)",
	"Delete learning '%s'? (y/n)":               "Slet læring '%s'? (y/n)",
	"Delete tag '#%s' from ALL tasks? (y/n)":    "Slet mærke '#%s' fra ALLE opgaver? (y/n)",
	"Delete this comment? (y/n)":                "Slet denne kommentar? (y/n)",
	"Remove project '%s' from this task? (y/n)": "Fjern projekt '%s' fra denne opgave? (y/n)",
	"Remove tag '#%s' from this task? (y/n)":    "Fjern mærke '#%s' fra denne opgave? (y/n)",
	"Remove this dependency? (y/n)":             "Fjern denne afhængighed? (y/n)",
	"Delete subtask '%s'? (y/n)":                "Slet delopgave '%s'? (y/n)",
	"Delete %s entry for '%s'? (y/n)":           "Slet %s-post for '%s'? (y/n)",

	// Input placeholders
	"Search... (#tag @project p:high due:<fri)":    "Søg... (#mærke @projekt p:high due:<fre)",
	"Search for task to add as dependency...":      "Søg efter opgave at tilføje som afhængighed...",
	"Search or create tag...":                      "Søg eller opret mærke...",
	"Search or create project...":                  "Søg eller opret projekt...",
	"Filter tags...":                               "Filtrér mærker...",
	"Search learnings... (use # to filter by tag)": "Søg i læring... (brug # for at filtrere på mærke)",

	// Inline edit/add placeholders (set when a text-entry mode opens)
	"New task...": "Ny opgave...",
	"HH:MM-HH:MM or duration (45m, 1h30m)...": "TT:MM-TT:MM eller varighed (45m, 1t30m)...",
	"Edit tag name...":                        "Rediger mærkenavn...",
	"Edit task title...":                      "Rediger opgavetitel...",
	"Edit learning...":                        "Rediger læring...",
	"Add comment...":                          "Tilføj kommentar...",
	"Add learning...":                         "Tilføj læring...",
	"Add subtask...":                          "Tilføj delopgave...",
	"Edit comment...":                         "Rediger kommentar...",
	"Start date (dd-mm-yy, 'today', 'next week', '+3d')...": "Startdato (dd-mm-yy, 'today', 'next week', '+3d')...",
	"Due date (dd-mm-yy, 'today', 'next week', '+3d')...":   "Forfaldsdato (dd-mm-yy, 'today', 'next week', '+3d')...",

	// ── Sync + server (Settings rows, status line, toasts) ──
	"Sync":                            "Synkronisering",
	"Sync server":                     "Synk.server",
	"Sync token":                      "Synk.token",
	"Sync now":                        "Synkronisér nu",
	"Server":                          "Server",
	"Listen":                          "Lytteadresse",
	"Server token":                    "Servertoken",
	"press enter to sync":             "tryk enter",
	"needs server":                    "kræver server",
	"needs token":                     "kræver token",
	"external":                        "ekstern",
	"set":                             "sat",
	"On":                              "Til",
	"Off":                             "Fra",
	"Syncing…":                        "Synkroniserer…",
	"Server stopped":                  "Server stoppet",
	"Server: ":                        "Server: ",
	"Serving on ":                     "Betjener på ",
	"✕ sync":                          "✕ synk.",
	"Set a server token first":        "Sæt en servertoken først",
	"Set sync server + token first":   "Sæt synk.server + token først",
	"Last sync failed: ":              "Seneste synk. mislykkedes: ",
	"Last sync: sent %d, received %d": "Seneste synk.: sendt %d, modtaget %d",
	"Sync failing — devices may be diverging (see Settings)":           "Synk. mislykkes — enhederne kan være ved at glide fra hinanden (se Indstillinger)",
	"Sync: %d conflict(s) resolved — see ~/.taskr/sync.log":            "Synk.: %d konflikt(er) løst — se ~/.taskr/sync.log",
	"Sync server URL, e.g. http://100.x.y.z:8765":                      "URL til synk.server, fx http://100.x.y.z:8765",
	"Sync token (clear the field to remove it)":                        "Synk.token (ryd feltet for at fjerne den)",
	"Server token clients must present (clear the field to remove it)": "Servertoken som klienter skal vise (ryd feltet for at fjerne den)",
	"Bind address, e.g. 100.x.y.z:8765 or 127.0.0.1:8765":              "Bind-adresse, fx 100.x.y.z:8765 eller 127.0.0.1:8765",
	"Plain http to a public host — token travels unencrypted":          "Almindelig http til en offentlig vært — token sendes ukrypteret",

	// ── Sequencer / Settings rows ──
	"Deadline pressure":         "Deadlinepres",
	"Priority focus":            "Prioritetsfokus",
	"Momentum bias":             "Momentumvægt",
	"Aging increases score":     "Alder øger scoren",
	"Auto-close parent":         "Luk forælder automatisk",
	"Auto-close subtasks":       "Luk delopgaver automatisk",
	"Top 5 with these weights:": "Top 5 med disse vægte:",
	"Score resync failed: %v":   "Genberegning af score mislykkedes: %v",

	// ── List header / panel titles ──
	"Active tasks":        "Aktive opgaver",
	"Detail":              "Detalje",
	"Tag":                 "Mærke",
	"  No task selected.": "  Ingen opgave valgt.",

	// ── Sort labels (drawn in the panel border, keep them short) ──
	"alpha":     "alfa",
	"completed": "afsluttet",
	"due":       "forfald",
	"size":      "størrelse",
	"count":     "antal",
	"progress":  "fremdrift",
	"recent":    "nyeste",

	// ── Quick-add preview ──
	"(no title yet)": "(ingen titel endnu)",
	"due ":           "forfald ",
	"overdue":        "forfalden",
	"title~":         "titel~",

	// ── Calendar entries ──
	"✓ done at ": "✓ færdig kl. ",
	"⧗ due":      "⧗ forfalder",
	"⧗ overdue":  "⧗ forfalden",

	// ── Task detail ──
	"  +%s subtasks = %s": "  +%s delopgaver = %s",
	"now":                 "nu",

	// ── Stats ──
	"  Cycle time by size":      "  Gennemløbstid efter størrelse",
	"  Projected backlog clear": "  Forventet tømning af backlog",
	"  Projected clear":         "  Forventet tømt",
	"%d pending, no pace":       "%d udestående, intet tempo",
	"Small":                     "Lille",
	"Medium":                    "Mellem",
	"Large":                     "Stor",

	// ── Confirm prompts and toasts ──
	"Close '%s' with %d open subtask(s)? (y/n)":       "Luk '%s' med %d åbne delopgave(r)? (y/n)",
	"Move '%s' to active? (y/n)":                      "Flyt '%s' til aktiv? (y/n)",
	"Delete '%s' and %d subtask(s)? (y/n)":            "Slet '%s' og %d delopgave(r)? (y/n)",
	"Delete %s entry? (y/n)":                          "Slet %s-post? (y/n)",
	"Merge #%s into…":                                 "Flet #%s ind i…",
	"Edit subtask title...":                           "Rediger delopgavetitel...",
	"Time spent (45m, 1h30m) or HH:MM-HH:MM…":         "Brugt tid (45m, 1t30m) eller TT:MM-TT:MM…",
	"Stop the timer before deleting a running entry":  "Stop tidtagningen, før du sletter en kørende post",
	"Stop the timer before editing a running entry":   "Stop tidtagningen, før du redigerer en kørende post",
	"Dependency '%s' is done":                         "Afhængigheden '%s' er færdig",
	"Dependency '%s' is hidden by the current filter": "Afhængigheden '%s' er skjult af det aktuelle filter",
	"Dependency no longer exists":                     "Afhængigheden findes ikke længere",

	// ── Help: quick-add tokens (the token itself stays English — it is syntax) ──
	"add a tag (existing tags are suggested; tab inserts)": "tilføj et mærke (eksisterende mærker foreslås; tab indsætter)",
	"put it in a project":                                  "læg den i et projekt",
	"set a due date (see Date input below)":                "sæt en forfaldsdato (se Datoinput nedenfor)",
	"priority: high / medium / low (p:h, p:m, p:l)":        "prioritet: high / medium / low (p:h, p:m, p:l)",
	"size: s / m / l (also size:large)":                    "størrelse: s / m / l (også size:large)",
	"repeat: daily / weekdays / weekly / monthly / yearly": "gentagelse: daily / weekdays / weekly / monthly / yearly",
	"block on the last added task (or dep:<id prefix>)":    "bloker på den senest tilføjede opgave (eller dep:<id-præfiks>)",

	// ── Help: search filters ──
	"only tasks carrying the tag":                                 "kun opgaver med mærket",
	"only tasks in the project":                                   "kun opgaver i projektet",
	"only that priority":                                          "kun den prioritet",
	"due before a date (also due:>, due:<=, due:>=, due:date)":    "forfalder før en dato (også due:>, due:<=, due:>=, due:date)",
	"only overdue tasks":                                          "kun forfaldne opgaver",
	"anything else fuzzy-matches the title (→ \"Buy groceries\")": "alt andet fuzzy-matcher titlen (→ \"Buy groceries\")",

	// ── Help: row symbols and scroll hints ──
	"Row symbols":           "Rækkesymboler",
	"high priority":         "høj prioritet",
	"timer running":         "tidtagning kører",
	"recurring task":        "gentagende opgave",
	"subtasks done / total": "delopgaver færdige / i alt",
	"blocked — waiting on an unfinished dependency":             "blokeret — venter på en uafsluttet afhængighed",
	"blocked — waiting on an unfinished dependency (ST column)": "blokeret — venter på en uafsluttet afhængighed (ST-kolonnen)",
	"others depend on this — finishing it unblocks them":        "andre afhænger af denne — at afslutte den frigør dem",
	"↑ scroll up":   "↑ rul op",
	"↓ scroll down": "↓ rul ned",
	"↑/↓ scroll":    "↑/↓ rul",
	"⚡FOCUS":        "⚡FOKUS",

	// ── Unchanged in Danish, listed so the completeness check stays honest ──
	"Score":      "Score",
	"Score:":     "Score:",
	"score":      "score",
	"ID:":        "ID:",
	"Sequence: ": "Sekvens: ",
	" ◉":         " ◉",
	"%.1f  (D %s · P %s · M %s · S %s · A %s)": "%.1f  (D %s · P %s · M %s · S %s · A %s)",

	// ── Sequencer personalities (names and taglines, sequence.go) ──
	"Drill Sergeant":    "Sergent",
	"Zen Garden":        "Zenhave",
	"Copilot":           "Andenpilot",
	"Custom":            "Tilpasset",
	"Deadline Hawk":     "Deadlinehøg",
	"Deadline Cruise":   "Afslappet deadline",
	"Importance First":  "Vigtighed først",
	"Importance Casual": "Vigtighed i baggrunden",
	"Flow State":        "Flowtilstand",
	"Fresh Eyes":        "Friske øjne",
	"High-Reactive: expect frequent shuffling as deadlines approach.":            "Meget reaktiv: forvent hyppig omrokering, når deadlines nærmer sig.",
	"Stable: tasks stay mostly in the order they were created.":                  "Stabil: opgaver bliver stort set i den rækkefølge, de blev oprettet.",
	"Balanced: equally weighs priorities, deadlines, and recent activity.":       "Balanceret: vægter prioriteter, deadlines og nylig aktivitet lige.",
	"Mixed biases — score reflects your tuned weights.":                          "Blandede vægte — scoren afspejler dine egne indstillinger.",
	"Tasks closest to their due date dominate the ranking.":                      "Opgaver tættest på deres forfaldsdato dominerer rangeringen.",
	"Due dates barely move the ranking.":                                         "Forfaldsdatoer rykker næsten ikke ved rangeringen.",
	"High priorities outweigh everything else.":                                  "Høje prioriteter vejer tungere end alt andet.",
	"Priority is treated as a hint, not a driver.":                               "Prioritet behandles som et hint, ikke en drivkraft.",
	"Projects with recent activity dominate — ride the streak.":                  "Projekter med nylig aktivitet dominerer — hold stimen i gang.",
	"Recent activity barely moves the ranking; cold projects get equal footing.": "Nylig aktivitet rykker næsten ikke ved rangeringen; kolde projekter stilles lige.",

	// ── Bias levels (shown in the ‹ … › pickers) ──
	"relaxed":  "afslappet",
	"balanced": "balanceret",
	"intense":  "intens",

	// ── Keymap descriptions the footer and help overlay render ──
	"jump to ends / page through list":          "spring til start/slut · bladr i listen",
	"toggle this help":                          "slå denne hjælp til/fra",
	"add task (#tag due:date p:high @proj s:M)": "tilføj opgave (#mærke due:date p:high @projekt s:M)",
	"add manual time entry":                     "tilføj manuel tidsregistrering",
	"start/stop subtask timer":                  "start/stop tidtagning på delopgave",
	"back to list":                              "tilbage til listen",
	"merge tags (Tags tab)":                     "flet mærker (fanen Mærker)",
	"select entry":                              "vælg post",
	"back":                                      "tilbage",
	"cycle activity range":                      "skift aktivitetsperiode",
	"change value / theme":                      "skift værdi / tema",
	"set / clear due date":                      "sæt / ryd forfaldsdato",

	// ── Help section titles ──
	"Calendar": "Kalender",
	"Stats":    "Statistik",

	// ── Short labels (the terse footer hint on a narrow window) ──
	"add":   "tilføj",
	"done":  "færdig",
	"track": "tid",
	"del":   "slet",
	"sort":  "sortér",
}
var deTranslations = map[string]string{
	// Header / chrome
	"? shortcuts":                               "? Tastenkürzel",
	"? shortcuts · ctrl+k commands":             "? Kürzel · ctrl+k Befehle",
	"Type a command…":                           "Befehl eingeben…",
	"No command matches that.":                  "Kein Befehl passt dazu.",
	"command palette — find any action by name": "Befehlspalette — jede Aktion per Name finden",
	"Go to Tasks":                               "Zu Aufgaben",
	"Go to Calendar":                            "Zum Kalender",
	"Go to Projects":                            "Zu Projekten",
	"Go to Tags":                                "Zu Schlagwörtern",
	"Go to Board":                               "Zum Board",
	"Go to Stats":                               "Zur Statistik",
	"Go to Settings":                            "Zu Einstellungen",
	"⚡ FOCUS: today + overdue only (f to toggle)": "⚡ FOKUS: nur heute + überfällig (f schaltet um)",
	"(untagged)": "(ohne Schlagwort)",

	// Tab labels (number prefix kept; only the word is translated)
	"1 Tasks":    "1 Aufgaben",
	"2 Calendar": "2 Kalender",
	"3 Projects": "3 Projekte",
	"4 Tags":     "4 Schlagwörter",
	"5 Board":    "5 Board",
	"6 Stats":    "6 Statistik",
	"7 Settings": "7 Einstell.",

	// Key hints (footer)
	"←/→ pages · enter edit · a add · d toggle · x remove · n notes · esc back":                                               "←/→ Seiten · enter bearb. · a neu · d fertig · x weg · n Notiz · esc zurück",
	"←/→ pages · enter edit · a add · x remove · esc back":                                                                    "←/→ Seiten · enter bearb. · a neu · x weg · esc zurück",
	"a add · d done · t track · x del · s sort · / search · ? more":                                                           "a neu · d fertig · t Zeit · x weg · s Sort. · / Suche · ? mehr",
	"enter detail · a add · d done · t track · p prio · r rename · x del · n notes · f focus · s sort · h history · / search": "enter Detail · a neu · d fertig · t Zeit · p Prio · r umben. · x weg · n Notiz · f Fokus · s Sort. · h Verlauf · / Suche",
	"j/k nav · r rename · x delete · / filter":                                                                                "j/k Nav · r umben. · x löschen · / Filter",
	"j/k nav · r rename · m merge · x delete · s sort · / filter":                                                             "j/k Nav · r umben. · m verein. · x löschen · s Sort. · / Filter",
	"j/k nav · r edit · x delete · s sort · / search":                                                                         "j/k Nav · r bearb. · x löschen · s Sort. · / Suche",
	"enter · cycle activity range":                                                                                            "enter · Zeitraum wechseln",
	"j/k select entry · r edit times · x delete · esc back":                                                                   "j/k Eintrag · r Zeiten · x löschen · esc zurück",
	"←/→ day · ↑/↓ week · [ ] month · t today · enter entries":                                                                "←/→ Tag · ↑/↓ Woche · [ ] Monat · t heute · enter Einträge",
	"↑/↓ select · ←/→ change theme · enter activate":                                                                          "↑/↓ wählen · ←/→ Farbschema · enter aktivieren",

	// Footer / timer
	"#tag @project due:tomorrow p:high s:l r:weekly dep:^": "#tag @projekt due:tomorrow p:high s:l r:weekly dep:^",
	" · t to stop":         " · t zum Stoppen",
	"create new tag: ":     "neues Schlagwort: ",
	"create new project: ": "neues Projekt: ",

	// Help screen
	"Keyboard shortcuts":                          "Tastenkürzel",
	"Press ? or esc to close":                     "? oder esc zum Schließen",
	"/ filter  ·  ? or esc to close":              "/ Filter  ·  ? oder esc schließt",
	"type to filter  ·  enter keep  ·  esc clear": "tippen filtert  ·  enter übernimmt  ·  esc löscht",
	"/ filter  ·  esc clear  ·  ? to close":       "/ Filter  ·  esc löscht  ·  ? schließt",
	"No shortcut matches that.":                   "Kein Kürzel passt dazu.",
	"Quick-add syntax":                            "Schnellerfassung",
	"Search filters":                              "Suchfilter",
	"Navigation":                                  "Navigation",
	"navigate list":                               "Liste navigieren",
	"open details":                                "Details öffnen",
	"go back":                                     "zurück",
	"switch tabs (forward / back / direct)":       "Reiter wechseln (vor / zurück / direkt)",
	"Board":                                       "Board",
	"focus previous/next column":                  "vorherige/nächste Spalte",
	"move card between stages (into Done completes it)": "Karte verschieben (nach Fertig schließt sie ab)",
	"empty":       "leer",
	"more":        "mehr",
	"close help":  "Hilfe schließen",
	"Tasks":       "Aufgaben",
	"Workflow":    "Arbeitsablauf",
	"Summary":     "Übersicht",
	"Preferences": "Einstellungen",
	"Sequencer":   "Sequenzer",
	"Overview":    "Überblick",
	"History":     "Verlauf",
	"Timeline":    "Zeitleiste",
	"Activity":    "Aktivität",
	"add task (quick-add: #tag due:date p:high @proj s:M)": "Aufgabe anlegen (#tag due:datum p:high @proj s:M)",
	"rename task":                 "Aufgabe umbenennen",
	"toggle done":                 "fertig umschalten",
	"start/stop time tracking":    "Zeiterfassung starten/stoppen",
	"cycle priority low/med/high": "Priorität niedrig/mittel/hoch",
	"delete":                      "löschen",
	"edit notes (opens $EDITOR)":  "Notizen bearbeiten (öffnet $EDITOR)",
	"ctrl+e  edit in $EDITOR":     "ctrl+e  in $EDITOR bearbeiten",
	"focus: today + overdue only": "Fokus: nur heute + überfällig",
	"toggle history":              "Verlauf umschalten",
	"cycle sort order":            "Sortierung wechseln",
	"expand/collapse subtasks":    "Teilaufgaben auf/zu",
	"search":                      "suchen",
	"Detail view":                 "Detailansicht",
	"jump section":                "Abschnitt wechseln",
	"edit field / open subtask":   "Feld bearbeiten / Teilaufgabe",
	"add tag / dep / comment / learning / subtask": "Schlagwort / Abhäng. / Kommentar / Erkenntnis / Teilaufgabe",
	"quick add tag":                         "Schlagwort schnell hinzufügen",
	"quick add / change project":            "Projekt schnell setzen/ändern",
	"toggle subtask done":                   "Teilaufgabe fertig",
	"rename subtask / edit time entry":      "Teilaufgabe umbenennen / Zeit bearbeiten",
	"remove field / delete subtask":         "Feld leeren / Teilaufgabe löschen",
	"Tags & Projects":                       "Schlagwörter & Projekte",
	"Inside a tag / project":                "In einem Schlagwort / Projekt",
	"open the tasks in it":                  "enthaltene Aufgaben öffnen",
	"new task in it":                        "neue Aufgabe darin",
	"show its tasks on the Tasks tab":       "seine Aufgaben im Reiter Aufgaben",
	"delete task":                           "Aufgabe löschen",
	"back to the list":                      "zurück zur Liste",
	"rename globally":                       "global umbenennen",
	"delete globally":                       "global löschen",
	"filter":                                "filtern",
	"Learnings":                             "Erkenntnisse",
	"edit learning":                         "Erkenntnis bearbeiten",
	"delete learning":                       "Erkenntnis löschen",
	"sort date/alpha":                       "Sortierung Datum/A-Z",
	"Calendar (tab 2)":                      "Kalender (Reiter 2)",
	"move by day / week":                    "Tag / Woche wechseln",
	"previous / next month":                 "voriger / nächster Monat",
	"jump to today":                         "zu heute springen",
	"focus the day's entries":               "Einträge des Tages",
	"edit entry times (09:12-10:00 or 45m)": "Zeiten bearbeiten (09:12-10:00 oder 45m)",
	"delete selected entry":                 "gewählten Eintrag löschen",
	"Stats (tab 6)":                         "Statistik (Reiter 6)",
	"switch to stats view":                  "zur Statistikansicht",
	"Settings (tab 7)":                      "Einstellungen (Reiter 7)",
	"select setting":                        "Einstellung wählen",
	"change theme":                          "Farbschema ändern",
	"activate / edit the selected setting":  "gewählte Einstellung aktivieren/bearbeiten",
	"confirm update when one is offered":    "Aktualisierung bestätigen",
	"App":                                   "App",
	"undo last change":                      "letzte Änderung rückgängig",
	"quit":                                  "beenden",
	"Date input":                            "Datumseingabe",
	"exact date (e.g. 15-06-25)":            "genaues Datum (z. B. 15-06-25)",
	"today's date":                          "heutiges Datum",
	"tomorrow":                              "morgen",
	"7 days from now":                       "in 7 Tagen",
	"1 month from now":                      "in 1 Monat",
	"next occurrence of weekday":            "nächster dieser Wochentage",
	"relative days/weeks/months":            "relative Tage/Wochen/Monate",

	// Stats detail
	"Last 30 days":                  "Letzte 30 Tage",
	"Last 26 weeks":                 "Letzte 26 Wochen",
	"Last 7 days":                   "Letzte 7 Tage",
	"%d done":                       "%d erledigt",
	"No completions in this range.": "Keine Abschlüsse in diesem Zeitraum.",
	": 1 block = 1 completed task":  ": 1 Block = 1 erledigte Aufgabe",

	// Stats list — sections & labels
	"  Workload":            "  Auslastung",
	"Overdue":               "Überfällig",
	"Due today":             "Heute fällig",
	"Due this week":         "Diese Woche",
	"Active total":          "Aktiv gesamt",
	"Seq hit (top-5)":       "Seq-Treffer (Top 5)",
	"Created":               "Erstellt",
	"Completed":             "Erledigt",
	"  Net backlog":         "  Netto-Rückstand",
	"+%d ▲ growing":         "+%d ▲ wächst",
	"%d ▼ shrinking":        "%d ▼ schrumpft",
	"±0 → steady":           "±0 → stabil",
	"%d done vs %d  %s":     "%d erledigt vs. %d  %s",
	"Flow (last 7 days)":    "Fluss (7 Tage)",
	"vs last week":          "vs. Vorwoche",
	"Flow (last 30 days)":   "Fluss (30 Tage)",
	"vs prior 30d":          "vs. vorige 30 T",
	"  Throughput":          "  Durchsatz",
	"  Time to done (30d)":  "  Dauer bis fertig (30 T)",
	"median ":               "Median ",
	"none yet":              "noch keine",
	"  Median active age":   "  Medianalter aktiv",
	"  Oldest active":       "  Älteste aktive",
	"  Active by priority":  "  Aktiv nach Priorität",
	"↑ High":                "↑ Hoch",
	"→ Medium":              "→ Mittel",
	"↓ Low":                 "↓ Niedrig",
	"  Completion velocity": "  Abschlusstempo",
	"Today":                 "Heute",
	"today":                 "heute",
	"This week":             "Diese Woche",
	"This month":            "Dieser Monat",
	"  Avg (7d)":            "  Ø (7 T)",
	"%.1f tasks/day":        "%.1f Aufgaben/Tag",

	// Priority, size, and recurrence words
	"high":     "hoch",
	"medium":   "mittel",
	"low":      "niedrig",
	"small":    "klein",
	"large":    "groß",
	"daily":    "täglich",
	"weekly":   "wöchentlich",
	"monthly":  "monatlich",
	"yearly":   "jährlich",
	"weekdays": "werktags",

	// List headers / sort
	"Task":              "Aufgabe",
	"Completed tasks":   "Erledigte Aufgaben",
	">Completed tasks<": ">Erledigte Aufgaben<",
	">Completed<":       ">Erledigt<",
	"Start":             "Start",
	"Due":               "Fällig",
	"Priority":          "Priorität",
	">Due<":             ">Fällig<",
	">Start<":           ">Start<",
	">Priority<":        ">Priorität<",
	"Tags":              "Schlagwörter",
	"sort:":             "Sort.:",

	// Tag list / detail
	"  No tags match your filter.":                                 "  Keine Schlagwörter passen zum Filter.",
	"  No tags yet. Add tags to tasks in the detail view.":         "  Noch keine Schlagwörter. In der Detailansicht hinzufügen.",
	"  Tags group related tasks; this tab shows progress per tag.": "  Schlagwörter bündeln Aufgaben; hier der Fortschritt je Schlagwort.",
	"  Tag":                       "  Schlagwort",
	"Progress":                    "Fortschritt",
	"Age":                         "Alter",
	"Time":                        "Zeit",
	"  No tag selected.":          "  Kein Schlagwort gewählt.",
	"  (untagged)":                "  (ohne Schlagwort)",
	"%d task":                     "%d Aufgabe",
	"%d tasks":                    "%d Aufgaben",
	" · enter: open · f: filter)": " · enter: öffnen · f: filtern)",
	" · enter: open · f: filter · r: rename)":             " · enter: öffnen · f: filtern · r: umbenennen)",
	" · d: done · t: track · enter: details · esc: back)": " · d: fertig · t: Zeit · enter: Details · esc: zurück)",
	"  %d active · %d done · %d overdue":                  "  %d aktiv · %d fertig · %d überfällig",
	"  often with: ":                                      "  oft mit: ",
	"  No tasks carry this tag.":                          "  Keine Aufgabe trägt dieses Schlagwort.",
	"  … and %d more":                                     "  … und %d weitere",
	"  due: ":                                             "  fällig: ",

	// Learnings list / detail
	"  No learnings match your search.":                                "  Keine Erkenntnisse passen zur Suche.",
	"  No learnings yet. Add learnings from a task's detail view.":     "  Noch keine Erkenntnisse. In der Detailansicht hinzufügen.",
	"  A learning is a takeaway you save on a task to keep for later.": "  Eine Erkenntnis ist eine Notiz, die du an einer Aufgabe aufhebst.",
	"Learning":                "Erkenntnis",
	"Date":                    "Datum",
	"  No learning selected.": "  Keine Erkenntnis gewählt.",
	"Source task:  ":          "Aufgabe:      ",
	"[done]":                  "[fertig]",
	"[task removed]":          "[Aufgabe entfernt]",
	"Date:         ":          "Datum:        ",
	"Tags:         ":          "Schlagw.:     ",
	"none":                    "keine",

	// Task list empty states
	"  No tasks match your search.":          "  Keine Aufgaben passen zur Suche.",
	"  No tasks due today or overdue. Nice!": "  Nichts heute fällig oder überfällig. Stark!",
	"  No tasks yet. Press 'a' to add one.":  "  Noch keine Aufgaben. 'a' legt eine an.",
	"  Try:  ":                               "  Beispiel:  ",
	// Free text (title/tag/project) is localized; the quick-add keywords
	// due:/friday/p:high stay English because the parser only accepts English.
	"Buy milk #shopping due:friday p:high @home": "Milch kaufen #einkauf due:friday p:high @zuhause",
	"  Press ? for all keyboard shortcuts.":      "  ? zeigt alle Tastenkürzel.",
	"  No completed tasks match your search.":    "  Keine erledigten Aufgaben passen zur Suche.",
	"  No completed tasks yet.":                  "  Noch keine erledigten Aufgaben.",

	// Projects
	"  No projects match your search.":                          "  Keine Projekte passen zur Suche.",
	"  No projects yet. Add a project to a task first.":         "  Noch keine Projekte. Erst einer Aufgabe eines zuweisen.",
	"  A project groups its tasks into a timeline on this tab.": "  Ein Projekt bündelt seine Aufgaben hier zu einer Zeitleiste.",
	"Project":                     "Projekt",
	"Active":                      "Aktiv",
	"Done":                        "Fertig",
	"  Timeline":                  "  Zeitleiste",
	"today:":                      "heute:",
	"  No tasks in this project.": "  Keine Aufgaben in diesem Projekt.",
	"%d active":                   "%d aktiv",
	"%d overdue":                  "%d überfällig",

	// Detail pages
	"not set":                            "nicht gesetzt",
	" ⚠ overdue":                         " ⚠ überfällig",
	"Start date":                         "Startdatum",
	"Due date":                           "Fälligkeit",
	"Recurrence":                         "Wiederholung",
	"Size":                               "Größe",
	"Notes":                              "Notizen",
	"none (press enter or 'n' to edit)":  "keine (enter oder 'n' bearbeitet)",
	"Created:":                           "Erstellt:",
	"Modified:":                          "Geändert:",
	"%s (%d entries)":                    "%s (%d Einträge)",
	" ◉ tracking":                        " ◉ läuft",
	"Time spent:":                        "Aufgewendet:",
	"Completed on:":                      "Erledigt am:",
	"Tags:":                              "Schlagwörter:",
	"No tags. Press 'a' to add one.":     "Keine Schlagwörter. 'a' fügt eines hinzu.",
	"Subtasks:":                          "Teilaufgaben:",
	"No subtasks. Press 'a' to add one.": "Keine Teilaufgaben. 'a' fügt eine hinzu.",
	"%s[?] unknown subtask":              "%s[?] unbekannte Teilaufgabe",
	"Dependencies:":                      "Abhängigkeiten:",
	"Inbound dependency — remove it from the other task": "Eingehende Abhängigkeit — an der anderen Aufgabe entfernen",
	"No dependencies. Press 'a' to add one.":             "Keine Abhängigkeiten. 'a' fügt eine hinzu.",
	"%s[?] unknown task":                                 "%s[?] unbekannte Aufgabe",
	"Learnings:":                                         "Erkenntnisse:",
	"No learnings yet. Press 'a' to add one.":            "Noch keine Erkenntnisse. 'a' fügt eine hinzu.",
	"Comments:":                              "Kommentare:",
	"No comments yet. Press 'a' to add one.": "Noch keine Kommentare. 'a' fügt einen hinzu.",
	"Time entries:":                          "Zeiteinträge:",
	"No time entries. Press 'T' to add one.": "Keine Zeiteinträge. 'T' fügt einen hinzu.",

	// Calendar
	"month ":                     "Monat ",
	"day ":                       "Tag ",
	"%d entries · %s":            "%d Einträge · %s",
	"1 entry · ":                 "1 Eintrag · ",
	"  No activity on this day.": "  Keine Aktivität an diesem Tag.",
	"  Press t on a task (tab 1) to start tracking.": "  t auf einer Aufgabe (Reiter 1) startet die Erfassung.",
	" now ": " jetzt ",

	// Settings
	"Theme":                          "Farbschema",
	"Language":                       "Sprache",
	"tab insert · ↑/↓ pick":          "tab einfügen · ↑/↓ wählen",
	"Board columns":                  "Board-Spalten",
	"Board columns, comma-separated": "Board-Spalten, kommagetrennt",
	"Comma-separated column names · Done is always the last column": "Spaltennamen kommagetrennt · Fertig ist immer die letzte Spalte",
	"Version":              "Version",
	"Check for updates":    "Nach Updates suchen",
	"press enter to check": "enter prüft",
	"Settings":             "Einstellungen",

	// Update / status / errors
	"Update failed":                                   "Update fehlgeschlagen",
	"Updated! Restart taskr to apply.":                "Aktualisiert! taskr neu starten.",
	"Error saving settings: %v":                       "Fehler beim Speichern der Einstellungen: %v",
	"Updated — restart to apply":                      "Aktualisiert — Neustart nötig",
	"Check failed":                                    "Prüfung fehlgeschlagen",
	"Up to date (":                                    "Aktuell (",
	"Latest release: ":                                "Neueste Version: ",
	" — this is a local build (":                      " — dies ist ein lokaler Build (",
	"Update available: ":                              "Update verfügbar: ",
	" — run `brew upgrade taskr`":                     " — `brew upgrade taskr` ausführen",
	" — install with `brew install iliorn/tap/taskr`": " — mit `brew install iliorn/tap/taskr` installieren",
	" is available — update now? (y/n)":               " ist verfügbar — jetzt aktualisieren? (y/n)",
	"Checking…":                                       "Prüfe…",
	"Updating…":                                       "Aktualisiere…",
	"Nothing to undo":                                 "Nichts rückgängig zu machen",
	"Undid: %s":                                       "Rückgängig: %s",
	"No editor found — set EDITOR permanently, e.g: setx EDITOR notepad (then restart taskr)":                               "Kein Editor gefunden — EDITOR dauerhaft setzen, z. B.: setx EDITOR notepad (dann taskr neu starten)",
	"No editor found — set $EDITOR permanently, e.g: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish": "Kein Editor gefunden — $EDITOR dauerhaft setzen, z. B.: echo 'set -Ux EDITOR /usr/lib/helix/hx' >> ~/.config/fish/config.fish",
	"Editor failed — falling back to notepad":                                                                               "Editor fehlgeschlagen — nutze notepad",
	"Invalid date - use dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday', or '+3d'":                                     "Ungültiges Datum - nutze dd-mm-yy, 'today', 'tomorrow', 'next week', 'monday' oder '+3d'",
	"Dependency not linked":                         "Abhängigkeit nicht verknüpft",
	"Remove project '%s' from ALL its tasks? (y/n)": "Projekt '%s' von ALLEN seinen Aufgaben entfernen? (y/n)",

	// Confirm prompts
	"Delete '%s'? (y/n)":                        "'%s' löschen? (y/n)",
	"Delete learning '%s'? (y/n)":               "Erkenntnis '%s' löschen? (y/n)",
	"Delete tag '#%s' from ALL tasks? (y/n)":    "Schlagwort '#%s' von ALLEN Aufgaben löschen? (y/n)",
	"Delete this comment? (y/n)":                "Diesen Kommentar löschen? (y/n)",
	"Remove project '%s' from this task? (y/n)": "Projekt '%s' von dieser Aufgabe entfernen? (y/n)",
	"Remove tag '#%s' from this task? (y/n)":    "Schlagwort '#%s' von dieser Aufgabe entfernen? (y/n)",
	"Remove this dependency? (y/n)":             "Diese Abhängigkeit entfernen? (y/n)",
	"Delete subtask '%s'? (y/n)":                "Teilaufgabe '%s' löschen? (y/n)",
	"Delete %s entry for '%s'? (y/n)":           "Eintrag %s für '%s' löschen? (y/n)",

	// Input placeholders
	"Search... (#tag @project p:high due:<fri)":    "Suchen… (#tag @projekt p:high due:<fri)",
	"Search for task to add as dependency...":      "Aufgabe als Abhängigkeit suchen…",
	"Search or create tag...":                      "Schlagwort suchen oder anlegen…",
	"Search or create project...":                  "Projekt suchen oder anlegen…",
	"Filter tags...":                               "Schlagwörter filtern…",
	"Search learnings... (use # to filter by tag)": "Erkenntnisse suchen… (# filtert nach Schlagwort)",

	// Inline edit/add placeholders (set when a text-entry mode opens)
	"New task...": "Neue Aufgabe…",
	"HH:MM-HH:MM or duration (45m, 1h30m)...": "HH:MM-HH:MM oder Dauer (45m, 1h30m)…",
	"Edit tag name...":                        "Schlagwortnamen bearbeiten…",
	"Edit task title...":                      "Aufgabentitel bearbeiten…",
	"Edit learning...":                        "Erkenntnis bearbeiten…",
	"Add comment...":                          "Kommentar hinzufügen…",
	"Add learning...":                         "Erkenntnis hinzufügen…",
	"Add subtask...":                          "Teilaufgabe hinzufügen…",
	"Edit comment...":                         "Kommentar bearbeiten…",
	"Start date (dd-mm-yy, 'today', 'next week', '+3d')...": "Startdatum (dd-mm-yy, 'today', 'next week', '+3d')…",
	"Due date (dd-mm-yy, 'today', 'next week', '+3d')...":   "Fälligkeit (dd-mm-yy, 'today', 'next week', '+3d')…",

	// ── Sync + server (Settings rows, status line, toasts) ──
	"Sync":                            "Sync",
	"Sync server":                     "Sync-Server",
	"Sync token":                      "Sync-Token",
	"Sync now":                        "Jetzt syncen",
	"Server":                          "Server",
	"Listen":                          "Adresse",
	"Server token":                    "Server-Token",
	"press enter to sync":             "enter synct",
	"needs server":                    "Server fehlt",
	"needs token":                     "Token fehlt",
	"external":                        "extern",
	"set":                             "gesetzt",
	"On":                              "An",
	"Off":                             "Aus",
	"Syncing…":                        "Synce…",
	"Server stopped":                  "Server gestoppt",
	"Server: ":                        "Server: ",
	"Serving on ":                     "Läuft auf ",
	"✕ sync":                          "✕ Sync",
	"Set a server token first":        "Erst ein Server-Token setzen",
	"Set sync server + token first":   "Erst Sync-Server + Token setzen",
	"Last sync failed: ":              "Letzter Sync fehlgeschlagen: ",
	"Last sync: sent %d, received %d": "Letzter Sync: %d gesendet, %d empfangen",
	"Sync failing — devices may be diverging (see Settings)":           "Sync schlägt fehl — Geräte laufen evtl. auseinander (siehe Einstellungen)",
	"Sync: %d conflict(s) resolved — see ~/.taskr/sync.log":            "Sync: %d Konflikt(e) gelöst — siehe ~/.taskr/sync.log",
	"Sync server URL, e.g. http://100.x.y.z:8765":                      "Sync-Server-URL, z. B. http://100.x.y.z:8765",
	"Sync token (clear the field to remove it)":                        "Sync-Token (Feld leeren zum Entfernen)",
	"Server token clients must present (clear the field to remove it)": "Token, das Clients vorzeigen müssen (Feld leeren zum Entfernen)",
	"Bind address, e.g. 100.x.y.z:8765 or 127.0.0.1:8765":              "Bind-Adresse, z. B. 100.x.y.z:8765 oder 127.0.0.1:8765",
	"Plain http to a public host — token travels unencrypted":          "Klartext-HTTP zu öffentlichem Host — Token wird unverschlüsselt übertragen",

	// ── Sequencer / Settings rows ──
	"Deadline pressure":         "Fristendruck",
	"Priority focus":            "Prioritätsfokus",
	"Momentum bias":             "Momentum-Gewicht",
	"Aging increases score":     "Alter erhöht Punktzahl",
	"Auto-close parent":         "Eltern autom. schließen",
	"Auto-close subtasks":       "Teilaufg. autom. schließen",
	"Top 5 with these weights:": "Top 5 mit diesen Gewichten:",
	"Score resync failed: %v":   "Punkte-Neuberechnung fehlgeschlagen: %v",

	// ── List header / panel titles ──
	"Active tasks":        "Aktive Aufgaben",
	"Detail":              "Detail",
	"Tag":                 "Schlagwort",
	"  No task selected.": "  Keine Aufgabe gewählt.",

	// ── Sort labels (drawn in the panel border, keep them short) ──
	"alpha":     "A-Z",
	"completed": "erledigt",
	"due":       "fällig",
	"size":      "Größe",
	"count":     "Anzahl",
	"progress":  "Fortschritt",
	"recent":    "neueste",

	// ── Quick-add preview ──
	"(no title yet)": "(noch ohne Titel)",
	"due ":           "fällig ",
	"overdue":        "überfällig",
	"title~":         "Titel~",

	// ── Calendar entries ──
	"✓ done at ": "✓ fertig am ",
	"⧗ due":      "⧗ fällig",
	"⧗ overdue":  "⧗ überfällig",

	// ── Task detail ──
	"  +%s subtasks = %s": "  +%s Teilaufgaben = %s",
	"now":                 "jetzt",

	// ── Stats ──
	"  Cycle time by size":      "  Durchlaufzeit nach Größe",
	"  Projected backlog clear": "  Rückstand voraussichtl. frei",
	"  Projected clear":         "  Voraussichtl. frei",
	"%d pending, no pace":       "%d offen, kein Tempo",
	"Small":                     "Klein",
	"Medium":                    "Mittel",
	"Large":                     "Groß",

	// ── Confirm prompts and toasts ──
	"Close '%s' with %d open subtask(s)? (y/n)":       "'%s' mit %d offenen Teilaufgabe(n) schließen? (y/n)",
	"Move '%s' to active? (y/n)":                      "'%s' wieder aktivieren? (y/n)",
	"Delete '%s' and %d subtask(s)? (y/n)":            "'%s' und %d Teilaufgabe(n) löschen? (y/n)",
	"Delete %s entry? (y/n)":                          "Eintrag %s löschen? (y/n)",
	"Merge #%s into…":                                 "#%s vereinen mit…",
	"Edit subtask title...":                           "Titel der Teilaufgabe bearbeiten…",
	"Time spent (45m, 1h30m) or HH:MM-HH:MM…":         "Aufgewendet (45m, 1h30m) oder HH:MM-HH:MM…",
	"Stop the timer before deleting a running entry":  "Erst die Uhr stoppen, dann den laufenden Eintrag löschen",
	"Stop the timer before editing a running entry":   "Erst die Uhr stoppen, dann den laufenden Eintrag bearbeiten",
	"Dependency '%s' is done":                         "Abhängigkeit '%s' ist erledigt",
	"Dependency '%s' is hidden by the current filter": "Abhängigkeit '%s' ist vom aktuellen Filter verdeckt",
	"Dependency no longer exists":                     "Abhängigkeit existiert nicht mehr",

	// ── Help: quick-add tokens (the token itself stays English — it is syntax) ──
	"add a tag (existing tags are suggested; tab inserts)": "Schlagwort setzen (Vorschläge; tab fügt ein)",
	"put it in a project":                                  "einem Projekt zuordnen",
	"set a due date (see Date input below)":                "Fälligkeit setzen (siehe Datumseingabe)",
	"priority: high / medium / low (p:h, p:m, p:l)":        "Priorität: hoch / mittel / niedrig (p:h, p:m, p:l)",
	"size: s / m / l (also size:large)":                    "Größe: s / m / l (auch size:large)",
	"repeat: daily / weekdays / weekly / monthly / yearly": "Wiederholung: daily / weekdays / weekly / monthly / yearly",
	"block on the last added task (or dep:<id prefix>)":    "auf zuletzt angelegte Aufgabe warten (oder dep:<ID-Präfix>)",

	// ── Help: search filters ──
	"only tasks carrying the tag":                                 "nur Aufgaben mit dem Schlagwort",
	"only tasks in the project":                                   "nur Aufgaben im Projekt",
	"only that priority":                                          "nur diese Priorität",
	"due before a date (also due:>, due:<=, due:>=, due:date)":    "fällig vor einem Datum (auch due:>, due:<=, due:>=, due:datum)",
	"only overdue tasks":                                          "nur überfällige Aufgaben",
	"anything else fuzzy-matches the title (→ \"Buy groceries\")": "alles andere trifft den Titel unscharf (→ \"Buy groceries\")",

	// ── Help: row symbols and scroll hints ──
	"Row symbols":           "Zeilensymbole",
	"high priority":         "hohe Priorität",
	"timer running":         "Uhr läuft",
	"recurring task":        "wiederkehrende Aufgabe",
	"subtasks done / total": "Teilaufgaben fertig / gesamt",
	"blocked — waiting on an unfinished dependency":             "blockiert — wartet auf offene Abhängigkeit",
	"blocked — waiting on an unfinished dependency (ST column)": "blockiert — wartet auf offene Abhängigkeit (Spalte ST)",
	"others depend on this — finishing it unblocks them":        "andere hängen daran — Abschluss gibt sie frei",
	"↑ scroll up":   "↑ nach oben",
	"↓ scroll down": "↓ nach unten",
	"↑/↓ scroll":    "↑/↓ scrollen",
	"⚡FOCUS":        "⚡FOKUS",

	// ── Unchanged in Danish, listed so the completeness check stays honest ──
	"Score":      "Punkte",
	"Score:":     "Punkte:",
	"score":      "Punkte",
	"ID:":        "ID:",
	"Sequence: ": "Sequenz: ",
	" ◉":         " ◉",
	"%.1f  (D %s · P %s · M %s · S %s · A %s)": "%.1f  (F %s · P %s · M %s · G %s · A %s)",

	// ── Sequencer personalities (names and taglines, sequence.go) ──
	"Drill Sergeant":    "Ausbilder",
	"Zen Garden":        "Zengarten",
	"Copilot":           "Kopilot",
	"Custom":            "Eigen",
	"Deadline Hawk":     "Fristenfalke",
	"Deadline Cruise":   "Fristen gelassen",
	"Importance First":  "Wichtigkeit zuerst",
	"Importance Casual": "Wichtigkeit locker",
	"Flow State":        "Flow",
	"Fresh Eyes":        "Frischer Blick",
	"High-Reactive: expect frequent shuffling as deadlines approach.":            "Sehr reaktiv: die Reihenfolge ändert sich häufig, je näher Fristen rücken.",
	"Stable: tasks stay mostly in the order they were created.":                  "Stabil: Aufgaben bleiben meist in ihrer Erstellungsreihenfolge.",
	"Balanced: equally weighs priorities, deadlines, and recent activity.":       "Ausgewogen: wichtet Priorität, Fristen und Aktivität gleich.",
	"Mixed biases — score reflects your tuned weights.":                          "Gemischt — die Punktzahl folgt deinen eigenen Gewichten.",
	"Tasks closest to their due date dominate the ranking.":                      "Aufgaben kurz vor der Frist bestimmen die Reihenfolge.",
	"Due dates barely move the ranking.":                                         "Fälligkeiten bewegen die Reihenfolge kaum.",
	"High priorities outweigh everything else.":                                  "Hohe Priorität schlägt alles andere.",
	"Priority is treated as a hint, not a driver.":                               "Priorität gilt als Hinweis, nicht als Treiber.",
	"Projects with recent activity dominate — ride the streak.":                  "Projekte mit frischer Aktivität dominieren — bleib in der Serie.",
	"Recent activity barely moves the ranking; cold projects get equal footing.": "Frische Aktivität zählt kaum; ruhende Projekte sind gleichgestellt.",

	// ── Bias levels (shown in the ‹ … › pickers) ──
	"relaxed":  "gelassen",
	"balanced": "ausgewogen",
	"intense":  "intensiv",

	// ── Keymap descriptions the footer and help overlay render ──
	"jump to ends / page through list":          "an den Rand / seitenweise blättern",
	"toggle this help":                          "diese Hilfe umschalten",
	"add task (#tag due:date p:high @proj s:M)": "Aufgabe anlegen (#tag due:datum p:high @proj s:M)",
	"add manual time entry":                     "Zeiteintrag von Hand",
	"start/stop subtask timer":                  "Uhr der Teilaufgabe starten/stoppen",
	"back to list":                              "zurück zur Liste",
	"merge tags (Tags tab)":                     "Schlagwörter vereinen (Reiter Schlagwörter)",
	"select entry":                              "Eintrag wählen",
	"back":                                      "zurück",
	"cycle activity range":                      "Zeitraum wechseln",
	"change value / theme":                      "Wert / Farbschema ändern",
	"set / clear due date":                      "Fälligkeit setzen / löschen",

	// ── Help section titles ──
	"Calendar": "Kalender",
	"Stats":    "Statistik",

	// ── Short labels (the terse footer hint on a narrow window) ──
	"add":   "neu",
	"done":  "fertig",
	"track": "Zeit",
	"del":   "weg",
	"sort":  "Sort.",
}
