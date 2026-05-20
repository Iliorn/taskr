package todo

import (
    "time"

    "github.com/google/uuid"
)

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
// ── Comment ───────────────────────────────────────────────────────────────────

type Comment struct {
    ID        string    `json:"id"`
    Text      string    `json:"text"`
    CreatedAt time.Time `json:"created_at"`
}

// ── Learning ──────────────────────────────────────────────────────────────────

type Learning struct {
    ID        string    `json:"id"`
    Text      string    `json:"text"`
    Tags      []string  `json:"tags,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

// ── TimeEntry ─────────────────────────────────────────────────────────────────

type TimeEntry struct {
    ID        string    `json:"id"`
    StartedAt time.Time `json:"started_at"`
    StoppedAt time.Time `json:"stopped_at,omitempty"`
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
    SubtaskIDs   []string    `json:"subtask_ids,omitempty"`
}

func New(title string) Todo {
    now := time.Now()
    return Todo{
        ID:         uuid.New().String(),
        Title:      title,
        Status:     Pending,
        Priority:   PriorityLow,
        CreatedAt:  now,
        ModifiedAt: now,
    }
}

func NewSubtask(title string, parentID string) Todo {
    t := New(title)
    t.ParentID = parentID
    return t
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

func (t *Todo) SetProject(p string) {
    t.Project = p
    t.ModifiedAt = time.Now()
}

func (t *Todo) AddTag(tag string) {
    for _, existing := range t.Tags {
        if existing == tag {
            return
        }
    }
    t.Tags = append(t.Tags, tag)
    t.ModifiedAt = time.Now()
}

func (t *Todo) RemoveTag(tag string) {
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
    if len(t.Tags) > 0 {
        l.Tags = make([]string, len(t.Tags))
        copy(l.Tags, t.Tags)
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

// ── Subtasks ──────────────────────────────────────────────────────────────────

func (t *Todo) AddSubtaskID(id string) {
    for _, existing := range t.SubtaskIDs {
        if existing == id {
            return
        }
    }
    t.SubtaskIDs = append(t.SubtaskIDs, id)
    t.ModifiedAt = time.Now()
}

func (t *Todo) RemoveSubtaskID(id string) {
    ids := t.SubtaskIDs[:0]
    for _, existing := range t.SubtaskIDs {
        if existing != id {
            ids = append(ids, existing)
        }
    }
    t.SubtaskIDs = ids
    t.ModifiedAt = time.Now()
}

// ── Notes ─────────────────────────────────────────────────────────────────────

func (t *Todo) SetNotes(notes string) {
    t.Notes = notes
    t.ModifiedAt = time.Now()
}

// ── Query helpers ─────────────────────────────────────────────────────────────

func (t *Todo) IsOverdue() bool {
    if t.Status == Done {
        return false
    }
    if t.DueDate.IsZero() {
        return false
    }
    now := time.Now()
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
