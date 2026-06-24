package todo

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// CapitalizeTitle uppercases the first rune of s if it is a lowercase letter,
// leaving the rest of s untouched. Empty strings and titles that start with a
// non-letter (digit, emoji, punctuation) or are already uppercase are returned
// as-is.
func CapitalizeTitle(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	if !unicode.IsLetter(r) || !unicode.IsLower(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// ── Status ────────────────────────────────────────────────────────────────────

type Status int

const (
	Pending Status = iota
	Done
)

// ── Priority ──────────────────────────────────────────────────────────────────

type Priority int

const (
	PriorityLow Priority = iota
	PriorityMedium
	PriorityHigh
)

func (p Priority) String() string {
	switch p {
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	default:
		return "low"
	}
}

func (p Priority) Icon() string {
	switch p {
	case PriorityHigh:
		return "↑"
	case PriorityMedium:
		return "→"
	default:
		return "↓"
	}
}

// ── Size ──────────────────────────────────────────────────────────────────────

// Size is the user's coarse estimate of how much effort a task will take. It
// feeds the Momentum dimension of the sequencing score: Small tasks rank
// highest (the "small-task floor") so a 5-minute win can outrank a large
// project sitting at the same priority/deadline.
//
// SizeMedium is the zero value so existing JSON blobs and pre-migration SQLite
// rows (added with DEFAULT 0) deserialize as Medium — a neutral default that
// matches "no opinion".
type Size int

const (
	SizeMedium Size = iota
	SizeSmall
	SizeLarge
)

func (s Size) String() string {
	switch s {
	case SizeSmall:
		return "small"
	case SizeLarge:
		return "large"
	default:
		return "medium"
	}
}

// Letter is the one-character form used in compact UI columns.
func (s Size) Letter() string {
	switch s {
	case SizeSmall:
		return "S"
	case SizeLarge:
		return "L"
	default:
		return "M"
	}
}

// ── Comment ───────────────────────────────────────────────────────────────────

type Comment struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	// DeletedAt tombstones the record for cross-device sync (see merge.go); the
	// zero value means live. Kept rather than removed so a deletion propagates.
	DeletedAt time.Time `json:"deleted_at,omitempty"`
}

// ── Learning ──────────────────────────────────────────────────────────────────

// Learning is a takeaway saved on a task. It has no tags of its own — a
// learning's tags are derived from its parent task's current tags at display
// time (see learningView), so they always reflect the task rather than a frozen
// snapshot.
type Learning struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	DeletedAt time.Time `json:"deleted_at,omitempty"` // sync tombstone; see Comment.DeletedAt
}

// ── TimeEntry ─────────────────────────────────────────────────────────────────

type TimeEntry struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
	StoppedAt time.Time `json:"stopped_at,omitempty"`
	DeletedAt time.Time `json:"deleted_at,omitempty"` // sync tombstone; see Comment.DeletedAt
}

func (te TimeEntry) Duration() time.Duration {
	if te.StoppedAt.IsZero() {
		return time.Since(te.StartedAt)
	}
	return te.StoppedAt.Sub(te.StartedAt)
}

func (te TimeEntry) IsRunning() bool {
	return te.StoppedAt.IsZero()
}

// ── Todo ──────────────────────────────────────────────────────────────────────

type Todo struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Status       Status      `json:"status"`
	Priority     Priority    `json:"priority"`
	Size         Size        `json:"size,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	ModifiedAt   time.Time   `json:"modified_at"`
	CompletedAt  time.Time   `json:"completed_at,omitempty"`
	StartDate    time.Time   `json:"start_date,omitempty"`
	DueDate      time.Time   `json:"due_date,omitempty"`
	Project      string      `json:"project,omitempty"`
	Tags         []string    `json:"tags,omitempty"`
	Dependencies []string    `json:"dependencies,omitempty"`
	Comments     []Comment   `json:"comments,omitempty"`
	Learnings    []Learning  `json:"learnings,omitempty"`
	TimeEntries  []TimeEntry `json:"time_entries,omitempty"`
	Notes        string      `json:"notes,omitempty"`
	ParentID     string      `json:"parent_id,omitempty"`
	Recurrence   string      `json:"recurrence,omitempty"`

	// Tombstone fields for cross-device sync: a deleted task is retained as a
	// tombstone (Deleted=true, DeletedAt set) so the deletion propagates during
	// sync instead of reappearing from another device. Storage already keeps
	// soft-deleted rows; these surface that state on the struct and the wire.
	Deleted   bool      `json:"deleted,omitempty"`
	DeletedAt time.Time `json:"deleted_at,omitempty"`
}

func New(title string) Todo {
	now := time.Now()
	return Todo{
		ID:         uuid.New().String(),
		Title:      CapitalizeTitle(title),
		Status:     Pending,
		Priority:   PriorityMedium,
		CreatedAt:  now,
		ModifiedAt: now,
	}
}

func NewSubtask(title string, parentID string) Todo {
	t := New(title)
	t.ParentID = parentID
	t.Size = SizeSmall
	return t
}

// InheritContextFrom copies the parent's Project, Tags, and DueDate into t.
// Callers use this when creating a subtask so it picks up the same context
// (project board, tag filters, deadline) as the parent without the user having
// to retype it.
func (t *Todo) InheritContextFrom(parent *Todo) {
	if parent == nil {
		return
	}
	if parent.Project != "" {
		t.Project = parent.Project
	}
	for _, tag := range parent.Tags {
		t.AddTag(tag)
	}
	if !parent.DueDate.IsZero() {
		t.DueDate = parent.DueDate
	}
}

func (t *Todo) Toggle() {
	if t.Status == Pending {
		t.Status = Done
		t.CompletedAt = time.Now()
	} else {
		t.Status = Pending
		t.CompletedAt = time.Time{}
	}
	t.ModifiedAt = time.Now()
}

func (t *Todo) SetDueDate(d time.Time) {
	t.DueDate = d
	t.ModifiedAt = time.Now()
}

func (t *Todo) SetStartDate(d time.Time) {
	t.StartDate = d
	t.ModifiedAt = time.Now()
}

func (t *Todo) SetPriority(p Priority) {
	t.Priority = p
	t.ModifiedAt = time.Now()
}

func (t *Todo) SetSize(s Size) {
	t.Size = s
	t.ModifiedAt = time.Now()
}

func (t *Todo) SetProject(p string) {
	t.Project = p
	t.ModifiedAt = time.Now()
}

// NormalizeTag canonicalizes a tag so that "#Work", "work ", and "work" all
// collapse to a single tag. Returns "" for input that isn't a usable tag.
func NormalizeTag(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "#")
	return strings.ToLower(strings.TrimSpace(tag))
}

func (t *Todo) AddTag(tag string) {
	tag = NormalizeTag(tag)
	if tag == "" {
		return
	}
	for _, existing := range t.Tags {
		if existing == tag {
			return
		}
	}
	t.Tags = append(t.Tags, tag)
	t.ModifiedAt = time.Now()
}

func (t *Todo) RemoveTag(tag string) {
	tag = NormalizeTag(tag)
	tags := t.Tags[:0]
	for _, existing := range t.Tags {
		if existing != tag {
			tags = append(tags, existing)
		}
	}
	t.Tags = tags
	t.ModifiedAt = time.Now()
}

func (t *Todo) AddDependency(id string) {
	for _, dep := range t.Dependencies {
		if dep == id {
			return
		}
	}
	t.Dependencies = append(t.Dependencies, id)
	t.ModifiedAt = time.Now()
}

func (t *Todo) RemoveDependency(id string) {
	deps := t.Dependencies[:0]
	for _, dep := range t.Dependencies {
		if dep != id {
			deps = append(deps, dep)
		}
	}
	t.Dependencies = deps
	t.ModifiedAt = time.Now()
}

func (t *Todo) AddComment(text string) {
	t.Comments = append(t.Comments, Comment{
		ID:        uuid.New().String(),
		Text:      text,
		CreatedAt: time.Now(),
	})
	t.ModifiedAt = time.Now()
}

func (t *Todo) UpdateComment(index int, text string) {
	if index >= 0 && index < len(t.Comments) {
		t.Comments[index].Text = text
		t.ModifiedAt = time.Now()
	}
}

func (t *Todo) DeleteComment(index int) {
	if index >= 0 && index < len(t.Comments) {
		t.Comments = append(t.Comments[:index], t.Comments[index+1:]...)
		t.ModifiedAt = time.Now()
	}
}

func (t *Todo) AddLearning(text string) {
	l := Learning{
		ID:        uuid.New().String(),
		Text:      text,
		CreatedAt: time.Now(),
	}
	t.Learnings = append(t.Learnings, l)
	t.ModifiedAt = time.Now()
}

func (t *Todo) UpdateLearning(index int, text string) {
	if index >= 0 && index < len(t.Learnings) {
		t.Learnings[index].Text = text
		t.ModifiedAt = time.Now()
	}
}

func (t *Todo) DeleteLearning(index int) {
	if index >= 0 && index < len(t.Learnings) {
		t.Learnings = append(t.Learnings[:index], t.Learnings[index+1:]...)
		t.ModifiedAt = time.Now()
	}
}

// ── Time tracking ─────────────────────────────────────────────────────────────

func (t *Todo) StartTimer() {
	t.StopTimer()
	t.TimeEntries = append(t.TimeEntries, TimeEntry{
		ID:        uuid.New().String(),
		StartedAt: time.Now(),
	})
	t.ModifiedAt = time.Now()
}

// AddTimeEntry appends a completed entry for [start, stop) and returns its
// generated ID. Used by the "manual time entry" flow when the user wants to
// log work that wasn't captured by the live timer.
func (t *Todo) AddTimeEntry(start, stop time.Time) string {
	id := uuid.New().String()
	t.TimeEntries = append(t.TimeEntries, TimeEntry{
		ID:        id,
		StartedAt: start,
		StoppedAt: stop,
	})
	t.ModifiedAt = time.Now()
	return id
}

func (t *Todo) StopTimer() {
	for i := range t.TimeEntries {
		if t.TimeEntries[i].IsRunning() {
			t.TimeEntries[i].StoppedAt = time.Now()
		}
	}
	t.ModifiedAt = time.Now()
}

func (t *Todo) IsTimerRunning() bool {
	for i := range t.TimeEntries {
		if t.TimeEntries[i].IsRunning() {
			return true
		}
	}
	return false
}

func (t *Todo) RunningEntry() *TimeEntry {
	for i := range t.TimeEntries {
		if t.TimeEntries[i].IsRunning() {
			return &t.TimeEntries[i]
		}
	}
	return nil
}

func (t *Todo) TotalTimeSpent() time.Duration {
	var total time.Duration
	for _, entry := range t.TimeEntries {
		total += entry.Duration()
	}
	return total
}

func (t *Todo) DeleteTimeEntry(index int) {
	if index >= 0 && index < len(t.TimeEntries) {
		t.TimeEntries = append(t.TimeEntries[:index], t.TimeEntries[index+1:]...)
		t.ModifiedAt = time.Now()
	}
}

// Subtasks: the parent→child link is stored only on the child as ParentID (the
// single source of truth). A parent's subtask list is derived from it — see
// model.subtaskIDs — rather than duplicated on the parent.

// ── Notes ─────────────────────────────────────────────────────────────────────

func (t *Todo) SetNotes(notes string) {
	t.Notes = notes
	t.ModifiedAt = time.Now()
}

// ── Recurrence ────────────────────────────────────────────────────────────────
//
// A task with a non-empty Recurrence respawns when marked Done: a fresh
// pending copy with a new ID is added to the store, and the original keeps
// its completion history. ParseRecurrence is the input validator; canonical
// rules are: "daily", "weekly", "monthly", "yearly", "weekdays", and
// "every:Nd|Nw|Nm|Ny" (N ≥ 1). NextRecurrenceFrom computes the next instance's
// date given the rule and a base time (typically the previous DueDate, or
// CompletedAt when no due date is set).

func (t *Todo) IsRecurring() bool { return t.Recurrence != "" }

func (t *Todo) SetRecurrence(rule string) {
	t.Recurrence = rule
	t.ModifiedAt = time.Now()
}

func (t *Todo) ClearRecurrence() {
	t.Recurrence = ""
	t.ModifiedAt = time.Now()
}

// ParseRecurrence canonicalizes a user-supplied recurrence string. Returns the
// canonical form and true if recognized. The empty string parses as ("", true)
// — a way to clear an existing rule via the same path.
func ParseRecurrence(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", true
	}
	switch s {
	case "daily", "day":
		return "daily", true
	case "weekly", "week":
		return "weekly", true
	case "monthly", "month":
		return "monthly", true
	case "yearly", "year", "annual", "annually":
		return "yearly", true
	case "weekdays", "weekday":
		return "weekdays", true
	}
	// "every:Nd|Nw|Nm|Ny" and the shorthand "Nd|Nw|Nm|Ny".
	spec := strings.TrimPrefix(s, "every:")
	if len(spec) >= 2 {
		unit := spec[len(spec)-1]
		numStr := spec[:len(spec)-1]
		n, ok := parsePositiveInt(numStr)
		if ok && n >= 1 {
			switch unit {
			case 'd', 'w', 'm', 'y':
				if n == 1 {
					switch unit {
					case 'd':
						return "daily", true
					case 'w':
						return "weekly", true
					case 'm':
						return "monthly", true
					case 'y':
						return "yearly", true
					}
				}
				return fmt.Sprintf("every:%d%c", n, unit), true
			}
		}
	}
	return "", false
}

// NextRecurrenceFrom returns the next instance date for rule, computed from
// base. Returns (zero, false) when rule is invalid or empty. The result keeps
// the wall-clock time of base (so "daily" with a base at 09:00 lands on the
// next day at 09:00). "weekdays" advances to the next Mon–Fri; if base is
// itself a weekday, it advances by one weekday.
func NextRecurrenceFrom(rule string, base time.Time) (time.Time, bool) {
	if rule == "" || base.IsZero() {
		return time.Time{}, false
	}
	switch rule {
	case "daily":
		return base.AddDate(0, 0, 1), true
	case "weekly":
		return base.AddDate(0, 0, 7), true
	case "monthly":
		return base.AddDate(0, 1, 0), true
	case "yearly":
		return base.AddDate(1, 0, 0), true
	case "weekdays":
		next := base.AddDate(0, 0, 1)
		for {
			wd := next.Weekday()
			if wd != time.Saturday && wd != time.Sunday {
				return next, true
			}
			next = next.AddDate(0, 0, 1)
		}
	}
	if strings.HasPrefix(rule, "every:") {
		spec := strings.TrimPrefix(rule, "every:")
		if len(spec) >= 2 {
			unit := spec[len(spec)-1]
			n, ok := parsePositiveInt(spec[:len(spec)-1])
			if ok && n >= 1 {
				switch unit {
				case 'd':
					return base.AddDate(0, 0, n), true
				case 'w':
					return base.AddDate(0, 0, n*7), true
				case 'm':
					return base.AddDate(0, n, 0), true
				case 'y':
					return base.AddDate(n, 0, 0), true
				}
			}
		}
	}
	return time.Time{}, false
}

func parsePositiveInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// ── Query helpers ─────────────────────────────────────────────────────────────

func (t *Todo) IsOverdue() bool {
	return t.IsOverdueAt(time.Now())
}

// IsOverdueAt is the clock-injectable form. Callers that need deterministic
// behavior (stats buckets, tests, anything pinned to a specific moment)
// should pass `now` explicitly so the result doesn't drift with the wall
// clock between invocations.
func (t *Todo) IsOverdueAt(now time.Time) bool {
	if t.Status == Done {
		return false
	}
	if t.DueDate.IsZero() {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return t.DueDate.Before(today)
}

func (t *Todo) HasOverdueDependencyFast(overdueSet map[string]bool) bool {
	for _, depID := range t.Dependencies {
		if overdueSet[depID] {
			return true
		}
	}
	return false
}

func (t *Todo) IsDueToday() bool {
	if t.DueDate.IsZero() || t.Status == Done {
		return false
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrow := today.AddDate(0, 0, 1)
	return !t.DueDate.Before(today) && t.DueDate.Before(tomorrow)
}

func (t *Todo) IsDueSoon(days int) bool {
	if t.DueDate.IsZero() || t.Status == Done {
		return false
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	deadline := today.AddDate(0, 0, days)
	return !t.DueDate.Before(today) && t.DueDate.Before(deadline)
}

func (t *Todo) IsTopLevel() bool {
	return t.ParentID == ""
}
