package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Iliorn/tjek/paths"
	"github.com/Iliorn/tjek/rank"
	"github.com/Iliorn/tjek/tasksync"
	"github.com/Iliorn/tjek/todo"
)

// sharedproject.go shares one project with other people through one file they
// can all reach, in a OneDrive or Dropbox folder most often: Trip.tjek holds
// the project's name and ID and every task of it, tombstones, stamps and
// history included. There is no server.
//
// Every device reads the file and writes it. A pass folds the file's tasks
// into the store with the sync merge (mergeIntoStore), then writes the
// project as the store now holds it back to the file, when that differs from
// what the file says. The merge is a CRDT (tasksync.Merge), which is what
// makes one file shared by several writers safe:
//
//   - Two devices writing at once: the cloud service keeps both, one as a
//     conflict copy beside the file ("Trip (Mark's conflicted copy).tjek").
//     A pass reads the copies with the file, merges them all, writes the
//     result and removes the copies (sharedConflictCopies).
//   - A service that lets the later write replace the earlier instead: the
//     device whose write was lost still holds its change in its store, sees
//     the file lacks it on its next pass, and writes it again.
//
// Either way a change can arrive late but not be lost, and every device ends
// with the same project.
//
// Which projects this device shares, and in which file, is local:
// shared.json in the config directory.
//
// A shared project lives on the devices that joined its file and nowhere
// else: the sync server neither gets nor gives its tasks (keepsOutOfSync), so
// each machine joins by itself, and leaving on one touches no other.
// Leaving removes the project's tasks from the device outright, with no
// tombstones: a tombstone would travel back into the file on a later join
// and delete the tasks for everyone. The removed IDs are remembered until
// then, so a sync server that still holds the tasks cannot bring them back.

const (
	sharedExt = ".tjek"
	// sharedFormat is the version of the file's shape. A file from a newer
	// format is refused rather than half-read.
	sharedFormat = 1
)

// sharedFile is a shared project's file.
type sharedFile struct {
	Format  int         `json:"format"`
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Created time.Time   `json:"created"`
	Tasks   []todo.Todo `json:"tasks"`
}

// sharedProject is one project this device shares.
type sharedProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	File string `json:"file"`
}

// sharedLeft is a project this device left: the tasks it removed, which a
// sync must not bring back until the project is joined again.
type sharedLeft struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tasks []string `json:"tasks"`
}

// sharedConfig is shared.json.
type sharedConfig struct {
	Projects []sharedProject `json:"projects,omitempty"`
	Left     []sharedLeft    `json:"left,omitempty"`
}

func sharedConfigPath() string { return paths.For(paths.Config, "shared.json") }

// loadSharedConfig reads shared.json; a missing file is no shared projects.
func loadSharedConfig() (sharedConfig, error) {
	var c sharedConfig
	data, err := os.ReadFile(sharedConfigPath())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return c, err
	default:
		if err := json.Unmarshal(data, &c); err != nil {
			return c, fmt.Errorf("%s: %w", sharedConfigPath(), err)
		}
	}
	return c, nil
}

func saveSharedConfig(c sharedConfig) error {
	if _, err := paths.Ensure(paths.Config); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(sharedConfigPath(), append(data, '\n'), 0o600)
}

// clone is a copy the share, join and leave edits can change without
// touching c, so a failed save leaves the loaded list as it was.
func (c sharedConfig) clone() sharedConfig {
	c.Projects = slices.Clone(c.Projects)
	c.Left = slices.Clone(c.Left)
	return c
}

// keepsOutOfSync reports whether the sync server must neither get t nor
// give it: a task of a project shared here, which travels through its file,
// or one this device removed by leaving a project.
func (c sharedConfig) keepsOutOfSync(t *todo.Todo) bool {
	if t.Project != "" {
		if _, ok := c.find(t.Project); ok {
			return true
		}
	}
	for _, l := range c.Left {
		if slices.Contains(l.Tasks, t.ID) {
			return true
		}
	}
	return false
}

// withoutShared is tasks less the ones keepsOutOfSync keeps from the sync
// server. The slice is new; tasks is left as it was.
func (c sharedConfig) withoutShared(tasks []todo.Todo) []todo.Todo {
	if len(c.Projects) == 0 && len(c.Left) == 0 {
		return tasks
	}
	out := make([]todo.Todo, 0, len(tasks))
	for i := range tasks {
		if !c.keepsOutOfSync(&tasks[i]) {
			out = append(out, tasks[i])
		}
	}
	return out
}

// find is the shared project named name, if this device shares one.
func (c sharedConfig) find(name string) (sharedProject, bool) {
	for _, p := range c.Projects {
		if p.Name == name {
			return p, true
		}
	}
	return sharedProject{}, false
}

// readSharedFile reads a project's file. It may be the project's own file or
// a conflict copy of it; either way a file from a newer format is refused.
func readSharedFile(path string) (sharedFile, error) {
	var f sharedFile
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return f, fmt.Errorf("%s is gone", path)
		}
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("%s is not a shared project: %w", filepath.Base(path), err)
	}
	if f.ID == "" {
		return f, fmt.Errorf("%s is not a shared project", filepath.Base(path))
	}
	if f.Format > sharedFormat {
		return f, fmt.Errorf("%s was written by a newer tjek; update to keep sharing it", filepath.Base(path))
	}
	return f, nil
}

// sharedFileName is the file a project is shared in: its name, with the
// characters a file name cannot hold on some system replaced.
func sharedFileName(project string) string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < ' ' {
			return '-'
		}
		return r
	}, project)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "project"
	}
	return name + sharedExt
}

// sharedTarget resolves a path as typed, a folder or a file, and says which.
func sharedTarget(typed string) (path string, isDir bool, err error) {
	path, err = filepath.Abs(expandHome(strings.TrimSpace(typed)))
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("no such file or folder: %s", typed)
	}
	return path, info.IsDir(), nil
}

// startSharing shares project name in a new file in folder, and records it
// here. A folder that already holds the project's file joins it instead.
func startSharing(c *sharedConfig, name, typedFolder string) (sharedProject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return sharedProject{}, errors.New("no project named")
	}
	if p, ok := c.find(name); ok {
		return p, fmt.Errorf("%q is already shared in %s", p.Name, p.File)
	}
	folder, isDir, err := sharedTarget(typedFolder)
	if err != nil {
		return sharedProject{}, err
	}
	if !isDir {
		return sharedProject{}, fmt.Errorf("not a folder: %s", typedFolder)
	}
	path := filepath.Join(folder, sharedFileName(name))
	if f, err := readSharedFile(path); err == nil {
		if f.Name != name {
			return sharedProject{}, fmt.Errorf("%s already shares the project %q", path, f.Name)
		}
		return joinShared(c, path)
	} else if _, statErr := os.Stat(path); statErr == nil {
		return sharedProject{}, fmt.Errorf("%s is in the way: %w", path, err)
	}
	data, err := encodeSharedFile(sharedFile{
		Format: sharedFormat, ID: uuid.NewString(), Name: name, Created: time.Now(), Tasks: []todo.Todo{},
	})
	if err != nil {
		return sharedProject{}, err
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return sharedProject{}, err
	}
	return joinShared(c, path)
}

// joinShared records the project a file holds as shared here. The path may
// also be a folder holding one shared project's file.
func joinShared(c *sharedConfig, typed string) (sharedProject, error) {
	path, isDir, err := sharedTarget(typed)
	if err != nil {
		return sharedProject{}, err
	}
	if isDir {
		files, _ := filepath.Glob(filepath.Join(path, "*"+sharedExt))
		if len(files) != 1 {
			return sharedProject{}, fmt.Errorf("%s holds %d shared project files; name the one to join", typed, len(files))
		}
		path = files[0]
	}
	f, err := readSharedFile(path)
	if err != nil {
		return sharedProject{}, err
	}
	for _, p := range c.Projects {
		if p.ID == f.ID {
			return p, fmt.Errorf("already joined %q", p.Name)
		}
	}
	if p, ok := c.find(f.Name); ok {
		return p, fmt.Errorf("a shared project named %q is already joined from %s", p.Name, p.File)
	}
	p := sharedProject{ID: f.ID, Name: f.Name, File: path}
	c.Projects = append(c.Projects, p)
	// Joining again lets the file bring back what leaving removed.
	c.Left = slices.DeleteFunc(c.Left, func(l sharedLeft) bool { return l.ID == f.ID })
	return p, nil
}

// leaveShared stops sharing the project named name here and removes its
// tasks from this device. It first brings the file up to date, so nothing
// made here is lost to the others; a file it cannot reach does not stop the
// leave. The removal is outright, with no tombstones (see the top of the
// file), and returns how many tasks went.
func leaveShared(h *sql.DB, c *sharedConfig, name string, b rank.Biases) (sharedProject, int, error) {
	p, ok := c.find(name)
	if !ok {
		return p, 0, fmt.Errorf("%q is not a shared project", name)
	}
	_, _ = syncShared(h, p, b)
	ids, err := removeProjectTasks(h, p.Name)
	if err != nil {
		return p, 0, err
	}
	c.Projects = slices.DeleteFunc(c.Projects, func(x sharedProject) bool { return x.ID == p.ID })
	c.Left = append(c.Left, sharedLeft{ID: p.ID, Name: p.Name, Tasks: ids})
	return p, len(ids), nil
}

// removeProjectTasks deletes every row of the project's tasks, tombstones
// included, with their children, history and dependency links, and returns
// their IDs. Nothing marks them deleted: to the store they were never here.
func removeProjectTasks(h *sql.DB, name string) ([]string, error) {
	tx, err := h.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id FROM todos WHERE project = ?`, name)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		for _, q := range []string{
			`DELETE FROM task_tags WHERE task_id = ?1`,
			`DELETE FROM task_dependencies WHERE task_id = ?1 OR depends_on_id = ?1`,
			`DELETE FROM task_comments WHERE task_id = ?1`,
			`DELETE FROM task_time_entries WHERE task_id = ?1`,
			`DELETE FROM task_events WHERE task_id = ?1`,
			`DELETE FROM todos WHERE id = ?1`,
		} {
			if _, err := tx.Exec(q, id); err != nil {
				return nil, err
			}
		}
	}
	return ids, tx.Commit()
}

// localProjectTasks counts the live tasks this device already files under
// name: what joining a project of that name would hand to everyone in it.
func localProjectTasks(todos []todo.Todo, name string) int {
	n := 0
	for i := range todos {
		if !todos[i].Deleted && todos[i].Project == name {
			n++
		}
	}
	return n
}

// sharedConflictCopies is the copies a cloud service made of the project's
// file when two devices wrote it at once. Each names it differently
// ("Trip-LAPTOP.tjek", "Trip (Mark's conflicted copy 2026-09-29).tjek",
// "Trip (1).tjek"), but all keep the name and the extension around what they
// add; a file of another project that happens to match is left alone by the
// ID check in syncShared.
func sharedConflictCopies(file string) []string {
	dir, base := filepath.Split(file)
	stem := strings.TrimSuffix(base, sharedExt)
	matches, _ := filepath.Glob(filepath.Join(dir, "*"+sharedExt))
	var out []string
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), sharedExt)
		if name == stem || !strings.HasPrefix(name, stem) {
			continue
		}
		if next := name[len(stem)]; next == ' ' || next == '-' || next == '_' || next == '(' {
			out = append(out, m)
		}
	}
	return out
}

// sharedResult is what one pass over a shared project did.
type sharedResult struct {
	changed bool // the merge changed the store
	wrote   bool // the file was rewritten
}

// syncShared runs one pass over p's file: fold the file and any conflict
// copies of it into the store, then write the project back when the file
// does not already say what the store holds, and remove the copies it took.
func syncShared(h *sql.DB, p sharedProject, b rank.Biases) (sharedResult, error) {
	var res sharedResult
	f, err := readSharedFile(p.File)
	if err != nil {
		return res, err
	}
	if f.ID != p.ID {
		return res, fmt.Errorf("%s now holds another project (%q)", p.File, f.Name)
	}
	incoming := f.Tasks
	var copies []string
	for _, cp := range sharedConflictCopies(p.File) {
		cf, err := readSharedFile(cp)
		if err != nil || cf.ID != p.ID {
			continue // not a copy of this project, or not one tjek can read
		}
		incoming = append(incoming, cf.Tasks...)
		copies = append(copies, cp)
	}
	if len(incoming) > 0 {
		if _, res.changed, err = mergeIntoStore(h, incoming, b); err != nil {
			return res, err
		}
	}

	all, err := loadTodosForSync(h)
	if err != nil {
		return res, err
	}
	f.Tasks = []todo.Todo{}
	for i := range all {
		if all[i].Project == p.Name {
			f.Tasks = append(f.Tasks, all[i])
		}
	}
	f.Format = sharedFormat
	data, err := encodeSharedFile(f)
	if err != nil {
		return res, err
	}
	// Every write is an upload to everyone, and two devices that each
	// rewrote an unchanged file would make conflict copies of nothing.
	if old, err := os.ReadFile(p.File); err != nil || !bytes.Equal(old, data) {
		if err := writeFileAtomic(p.File, data, 0o644); err != nil {
			return res, err
		}
		res.wrote = true
	}
	for _, cp := range copies {
		_ = os.Remove(cp) // merged and written; one left behind is merged again
	}
	return res, nil
}

// encodeSharedFile writes the file in one canonical form: tasks in ID order,
// each in the sync digest's form (tasksync.CanonicalJSON: its sets and
// records sorted, its times in UTC). Two devices holding the same project
// must write the same bytes, or each would see the other's file as
// different and rewrite it on every pass, for good; the store's own order of
// a task's comments or tags, and the device's time zone, are not the
// project's content. The order also keeps an unchanged task in its place,
// so a synced folder uploads a small diff.
func encodeSharedFile(f sharedFile) ([]byte, error) {
	sort.Slice(f.Tasks, func(i, j int) bool { return f.Tasks[i].ID < f.Tasks[j].ID })
	tasks := make([]json.RawMessage, len(f.Tasks))
	for i := range f.Tasks {
		tasks[i] = tasksync.CanonicalJSON(f.Tasks[i])
	}
	data, err := json.MarshalIndent(struct {
		Format  int               `json:"format"`
		ID      string            `json:"id"`
		Name    string            `json:"name"`
		Created time.Time         `json:"created"`
		Tasks   []json.RawMessage `json:"tasks"`
	}{f.Format, f.ID, f.Name, f.Created.UTC(), tasks}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// syncAllShared runs syncShared for every project this device shares. A
// project that fails does not stop the others; the errors come back together.
func syncAllShared(h *sql.DB, b rank.Biases) (changed bool, err error) {
	c, err := loadSharedConfig()
	if err != nil || len(c.Projects) == 0 {
		return false, err
	}
	var errs []error
	for _, p := range c.Projects {
		res, err := syncShared(h, p, b)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name, err))
			continue
		}
		changed = changed || res.changed
	}
	return changed, errors.Join(errs...)
}
