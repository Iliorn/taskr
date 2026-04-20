package todo

import (
    "time"
)

type Status int

const (
    Pending Status = iota
    Done
)

type Priority int

const (
    PriorityLow Priority = iota
    PriorityMedium
    PriorityHigh
)

func (p Priority) Icon() string {
    switch p {
    case PriorityLow:
        return "↓"
    case PriorityMedium:
        return "→"
    case PriorityHigh:
        return "↑"
    }
    return "→"
}

func (p Priority) String() string {
    switch p {
    case PriorityLow:
        return "Low"
    case PriorityMedium:
        return "Medium"
    case PriorityHigh:
        return "High"
    }
    return "Medium"
}

type Comment struct {
    Text      string    `json:"text"`
    CreatedAt time.Time `json:"created_at"`
}

type Learning struct {
    ID        string    `json:"id"`
    Text      string    `json:"text"`
    TaskID    string    `json:"task_id"`
    TaskTitle string    `json:"task_title"`
    Tags      []string  `json:"tags"`
    CreatedAt time.Time `json:"created_at"`
}

type Todo struct {
    ID           string     `json:"id"`
    Title        string     `json:"title"`
    Status       Status     `json:"status"`
    Priority     Priority   `json:"priority"`
    StartDate    time.Time  `json:"start_date"`
    DueDate      time.Time  `json:"due_date"`
    CompletedAt  time.Time  `json:"completed_at"`
    Project      string     `json:"project"`
    Tags         []string   `json:"tags"`
    Dependencies []string   `json:"dependencies"`
    Comments     []Comment  `json:"comments"`
    Learnings    []Learning `json:"learnings"`
    CreatedAt    time.Time  `json:"created_at"`
    ModifiedAt   time.Time  `json:"modified_at"`
}

func New(title string) Todo {
    now := time.Now()
    return Todo{
        ID:           generateID(),
        Title:        title,
        Status:       Pending,
        Priority:     PriorityMedium,
        Tags:         []string{},
        Dependencies: []string{},
        Comments:     []Comment{},
        Learnings:    []Learning{},
        CreatedAt:    now,
        ModifiedAt:   now,
    }
}

func generateID() string {
    return time.Now().Format("20060102150405.000000000")
}

func (t *Todo) Toggle() {
    now := time.Now()
    if t.Status == Pending {
        t.Status = Done
        t.CompletedAt = now
    } else {
        t.Status = Pending
        t.CompletedAt = time.Time{}
    }
    t.ModifiedAt = now
}

func (t *Todo) SetPriority(p Priority) {
    t.Priority = p
    t.ModifiedAt = time.Now()
}

func (t *Todo) AddComment(text string) {
    t.Comments = append(t.Comments, Comment{
        Text:      text,
        CreatedAt: time.Now(),
    })
    t.ModifiedAt = time.Now()
}

func (t *Todo) DeleteComment(index int) {
    if index >= 0 && index < len(t.Comments) {
        t.Comments = append(t.Comments[:index], t.Comments[index+1:]...)
        t.ModifiedAt = time.Now()
    }
}

func (t *Todo) UpdateComment(index int, text string) {
    if index >= 0 && index < len(t.Comments) {
        t.Comments[index].Text = text
        t.ModifiedAt = time.Now()
    }
}

func (t *Todo) AddLearning(text string) {
    // Snapshot the task's current tags at creation time
    tags := make([]string, len(t.Tags))
    copy(tags, t.Tags)
    t.Learnings = append(t.Learnings, Learning{
        ID:        generateID(),
        Text:      text,
        TaskID:    t.ID,
        TaskTitle: t.Title,
        Tags:      tags,
        CreatedAt: time.Now(),
    })
    t.ModifiedAt = time.Now()
}

func (t *Todo) DeleteLearning(index int) {
    if index >= 0 && index < len(t.Learnings) {
        t.Learnings = append(t.Learnings[:index], t.Learnings[index+1:]...)
        t.ModifiedAt = time.Now()
    }
}

func (t *Todo) UpdateLearning(index int, text string) {
    if index >= 0 && index < len(t.Learnings) {
        t.Learnings[index].Text = text
        t.ModifiedAt = time.Now()
    }
}

func (t *Todo) SetStartDate(d time.Time) {
    t.StartDate = d
    t.ModifiedAt = time.Now()
}

func (t *Todo) SetDueDate(d time.Time) {
    t.DueDate = d
    t.ModifiedAt = time.Now()
}

func (t *Todo) SetProject(project string) {
    t.Project = project
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
    for i, existing := range t.Tags {
        if existing == tag {
            t.Tags = append(t.Tags[:i], t.Tags[i+1:]...)
            t.ModifiedAt = time.Now()
            return
        }
    }
}

func (t *Todo) AddDependency(id string) {
    for _, d := range t.Dependencies {
        if d == id {
            return
        }
    }
    t.Dependencies = append(t.Dependencies, id)
    t.ModifiedAt = time.Now()
}

func (t *Todo) RemoveDependency(id string) {
    for i, d := range t.Dependencies {
        if d == id {
            t.Dependencies = append(t.Dependencies[:i], t.Dependencies[i+1:]...)
            t.ModifiedAt = time.Now()
            return
        }
    }
}

func (t *Todo) IsOverdue() bool {
    if t.DueDate.IsZero() || t.Status == Done {
        return false
    }
    due := time.Date(t.DueDate.Year(), t.DueDate.Month(), t.DueDate.Day()+1, 0, 0, 0, 0, t.DueDate.Location())
    return time.Now().After(due)
}

func (t *Todo) HasOverdueDependency(todos []Todo) bool {
    for _, depID := range t.Dependencies {
        for _, dep := range todos {
            if dep.ID == depID && dep.IsOverdue() {
                return true
            }
        }
    }
    return false
}
