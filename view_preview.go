package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"taskr/todo"
)

// view_preview.go renders the live parse preview shown under the quick-add and
// search inputs. Both inputs use the same token vocabulary but silently drop a
// mistyped token (due:tomorow, p:hgih) into the free text — the title for
// quick-add, the fuzzy title match for search. Echoing the parsed result as the
// user types makes that visible: a token that didn't take shows up in the
// quoted free-text run instead of as its own chip.

// renderQuickAddPreview shows what parseQuickAdd extracts from the current
// quick-add input: the title in quotes, then a chip per recognised field.
// Anything that failed to parse stays inside the title quotes, which is the
// tell that a token was mistyped.
func renderQuickAddPreview(input string, w int) string {
	p := parseQuickAdd(input)

	title := strings.TrimSpace(p.title)
	if title == "" {
		title = tr("(no title yet)")
	}
	parts := []string{selectedStyle.Render(`"` + title + `"`)}
	for _, tag := range p.tags {
		parts = append(parts, tagStyle.Render("#"+tag))
	}
	if p.project != "" {
		parts = append(parts, projLabelStyle.Render("@"+p.project))
	}
	if !p.dueDate.IsZero() {
		parts = append(parts, normalStyle.Render(tr("due ")+p.dueDate.Format("02-01")))
	}
	if p.priority != todo.PriorityMedium {
		parts = append(parts, normalStyle.Render("p:"+trPriority(p.priority)))
	}
	if p.size != todo.SizeMedium {
		parts = append(parts, normalStyle.Render("s:"+p.size.Letter()))
	}
	if p.recurrence != "" {
		parts = append(parts, normalStyle.Render("⟳"+trRecurrence(p.recurrence)))
	}
	if len(p.deps) > 0 {
		parts = append(parts, normalStyle.Render("dep:"+strings.Join(p.deps, ",")))
	}

	line := helpStyle.Render("    → ") + strings.Join(parts, "  ")
	return ansi.Truncate(line, w, "…")
}

// renderSearchPreview mirrors compileSearch's tokenisation to show the active
// filters as the user types: recognised tokens become chips, and any leftover
// (including a mistyped p:/due:) is shown as the fuzzy title-match query.
func renderSearchPreview(query string, w int) string {
	var filters []string
	var titleWords []string

	for _, tok := range strings.Fields(query) {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(tok, "#") && len(tok) > 1:
			filters = append(filters, tagStyle.Render(tok))
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			filters = append(filters, projLabelStyle.Render(tok))
		case strings.HasPrefix(lower, "p:"):
			if p, ok := parsePriorityFilter(strings.TrimPrefix(lower, "p:")); ok {
				filters = append(filters, normalStyle.Render("p:"+trPriority(p)))
			} else {
				titleWords = append(titleWords, tok)
			}
		case strings.HasPrefix(lower, "due:"):
			if desc, ok := describeDueFilter(strings.TrimPrefix(lower, "due:")); ok {
				filters = append(filters, normalStyle.Render(desc))
			} else {
				titleWords = append(titleWords, tok)
			}
		case lower == "overdue":
			filters = append(filters, overdueStyle.Render(tr("overdue")))
		default:
			titleWords = append(titleWords, tok)
		}
	}

	parts := filters
	if len(titleWords) > 0 {
		parts = append(parts, dimStyle.Render(tr("title~")+` "`+strings.Join(titleWords, " ")+`"`))
	}

	line := helpStyle.Render("    → ") + strings.Join(parts, "  ")
	return ansi.Truncate(line, w, "…")
}

// describeDueFilter renders a due: comparison token for the search preview
// (e.g. "<tomorrow" → "due <05-07"), reporting false for an unparseable date so
// the caller falls the token back into the title-match run. Mirrors the op
// parsing in parseDueFilter.
func describeDueFilter(spec string) (string, bool) {
	op, rest := "", spec
	switch {
	case strings.HasPrefix(spec, "<="):
		op, rest = "≤", spec[2:]
	case strings.HasPrefix(spec, ">="):
		op, rest = "≥", spec[2:]
	case strings.HasPrefix(spec, "<"):
		op, rest = "<", spec[1:]
	case strings.HasPrefix(spec, ">"):
		op, rest = ">", spec[1:]
	}
	d, err := parseDueDate(rest)
	if err != nil {
		return "", false
	}
	return tr("due ") + op + d.Format("02-01"), true
}
