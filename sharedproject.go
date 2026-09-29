package main

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
	"github.com/Iliorn/tjek/todo"
)

// sharedproject.go shares one project with other people through a folder they
// all can reach, a OneDrive or Dropbox folder most often. There is no server:
// the folder holds a manifest naming the project (sharedManifestName) and one
// file per device that has joined (sharedMemberFile), each written only by its
// own device, so a synced folder never sees two devices write the same file
// and never makes a conflict copy of it.
//
// A device's file is every task of the project it holds, tombstones, stamps
// and history included. Syncing reads the other files and folds their tasks in
// with the sync merge (mergeIntoStore), then writes the device's own file when
// its tasks changed. The merge is a CRDT (tasksync.Merge), so the files can be
// read in any order, any number of times, however late a cloud client
// delivers them, and every device ends with the same project.
//
// Which projects this device shares, and where, is local: shared.json in the
// config directory, beside the device's own ID that names its file.

const (
	sharedManifestName = "tjek-project.json"
	sharedMemberPrefix = "tjek-member-"
	// sharedFormat is the version of both file shapes. A file from a newer
	// format is refused rather than half-read.
	sharedFormat = 1
)

// sharedManifest is the folder's tjek-project.json: which project it holds.
type sharedManifest struct {
	Format  int       `json:"format"`
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

// sharedMemberFile is one device's file in the folder.
type sharedMemberFile struct {
	Format  int         `json:"format"`
	Project string      `json:"project"` // the manifest's ID
	Device  string      `json:"device"`
	Author  string      `json:"author,omitempty"`
	Tasks   []todo.Todo `json:"tasks"`
}

// sharedProject is one project this device shares.
type sharedProject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// sharedConfig is shared.json.
type sharedConfig struct {
	// Device names this device's file in every shared folder.
	Device   string          `json:"device"`
	Projects []sharedProject `json:"projects,omitempty"`
}

func sharedConfigPath() string { return paths.For(paths.Config, "shared.json") }

// loadSharedConfig reads shared.json; a missing file is no shared projects.
// The device ID is made on first use and kept from then on.
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
	if c.Device == "" {
		c.Device = uuid.NewString()[:8]
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
	return c
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

func (c sharedConfig) memberPath(p sharedProject) string {
	return filepath.Join(p.Folder, sharedMemberPrefix+c.Device+".json")
}

func readSharedManifest(folder string) (sharedManifest, error) {
	var m sharedManifest
	data, err := os.ReadFile(filepath.Join(folder, sharedManifestName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, fmt.Errorf("%s holds no shared project (no %s)", folder, sharedManifestName)
		}
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("%s: %w", sharedManifestName, err)
	}
	if m.Format > sharedFormat {
		return m, fmt.Errorf("%s was written by a newer tjek; update to join it", sharedManifestName)
	}
	return m, nil
}

// sharedFolder resolves a folder as typed and checks it is one.
func sharedFolder(typed string) (string, error) {
	folder, err := filepath.Abs(expandHome(strings.TrimSpace(typed)))
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return "", fmt.Errorf("not a folder: %s", typed)
	}
	return folder, nil
}

// startSharing shares project name in folder: it writes the manifest and
// records the project here. A folder that already holds a project is joined
// instead when it is this one, and refused when it is another.
func startSharing(c *sharedConfig, name, typedFolder string) (sharedProject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return sharedProject{}, errors.New("no project named")
	}
	if p, ok := c.find(name); ok {
		return p, fmt.Errorf("%q is already shared in %s", p.Name, p.Folder)
	}
	folder, err := sharedFolder(typedFolder)
	if err != nil {
		return sharedProject{}, err
	}
	if m, err := readSharedManifest(folder); err == nil {
		if m.Name != name {
			return sharedProject{}, fmt.Errorf("%s already holds the shared project %q", folder, m.Name)
		}
		return joinShared(c, folder)
	}
	m := sharedManifest{Format: sharedFormat, ID: uuid.NewString(), Name: name, Created: time.Now()}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return sharedProject{}, err
	}
	if err := writeFileAtomic(filepath.Join(folder, sharedManifestName), append(data, '\n'), 0o644); err != nil {
		return sharedProject{}, err
	}
	p := sharedProject{ID: m.ID, Name: name, Folder: folder}
	c.Projects = append(c.Projects, p)
	return p, nil
}

// joinShared records the project a folder holds as shared here.
func joinShared(c *sharedConfig, typedFolder string) (sharedProject, error) {
	folder, err := sharedFolder(typedFolder)
	if err != nil {
		return sharedProject{}, err
	}
	m, err := readSharedManifest(folder)
	if err != nil {
		return sharedProject{}, err
	}
	for _, p := range c.Projects {
		if p.ID == m.ID {
			return p, fmt.Errorf("already joined %q", p.Name)
		}
	}
	if p, ok := c.find(m.Name); ok {
		return p, fmt.Errorf("a shared project named %q is already joined from %s", p.Name, p.Folder)
	}
	p := sharedProject{ID: m.ID, Name: m.Name, Folder: folder}
	c.Projects = append(c.Projects, p)
	return p, nil
}

// leaveShared stops sharing the project named name here. Its tasks stay, as
// ordinary tasks of a project by that name; this device's file leaves the
// folder, and the others keep what it had already given them.
func leaveShared(c *sharedConfig, name string) (sharedProject, error) {
	p, ok := c.find(name)
	if !ok {
		return p, fmt.Errorf("%q is not a shared project", name)
	}
	c.Projects = slices.DeleteFunc(c.Projects, func(x sharedProject) bool { return x.ID == p.ID })
	if err := os.Remove(c.memberPath(p)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	return p, nil
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

// sharedResult is what one pass over a shared folder did.
type sharedResult struct {
	received int  // tasks the other files brought in
	changed  bool // the merge changed the store
	wrote    bool // this device's file was rewritten
}

// syncShared runs one pass over p's folder: fold in the other devices' files,
// then write this device's own when the project's tasks differ from it.
func syncShared(h *sql.DB, c sharedConfig, p sharedProject, author string, b rank.Biases) (sharedResult, error) {
	var res sharedResult
	m, err := readSharedManifest(p.Folder)
	if err != nil {
		return res, err
	}
	if m.ID != p.ID {
		return res, fmt.Errorf("%s now holds another project (%q)", p.Folder, m.Name)
	}
	own := c.memberPath(p)
	files, err := filepath.Glob(filepath.Join(p.Folder, sharedMemberPrefix+"*.json"))
	if err != nil {
		return res, err
	}
	var incoming []todo.Todo
	for _, f := range files {
		if f == own {
			continue
		}
		mf, err := readMemberFile(f)
		if err != nil {
			return res, err
		}
		if mf.Project != p.ID {
			continue // a stray file from another project
		}
		incoming = append(incoming, mf.Tasks...)
	}
	res.received = len(incoming)
	if len(incoming) > 0 {
		if _, res.changed, err = mergeIntoStore(h, incoming, b); err != nil {
			return res, err
		}
	}

	all, err := loadTodosForSync(h)
	if err != nil {
		return res, err
	}
	mine := sharedMemberFile{Format: sharedFormat, Project: p.ID, Device: c.Device, Author: author, Tasks: []todo.Todo{}}
	for i := range all {
		if all[i].Project == p.Name {
			mine.Tasks = append(mine.Tasks, all[i])
		}
	}
	// By ID, so an unchanged task keeps its place and a synced folder
	// uploads a small diff.
	sort.Slice(mine.Tasks, func(i, j int) bool { return mine.Tasks[i].ID < mine.Tasks[j].ID })
	data, err := json.MarshalIndent(mine, "", "  ")
	if err != nil {
		return res, err
	}
	data = append(data, '\n')
	// Every write is an upload to everyone in the folder, so an unchanged
	// file is left alone.
	if old, err := os.ReadFile(own); err == nil && bytes.Equal(old, data) {
		return res, nil
	}
	if err := writeFileAtomic(own, data, 0o644); err != nil {
		return res, err
	}
	res.wrote = true
	return res, nil
}

func readMemberFile(path string) (sharedMemberFile, error) {
	var mf sharedMemberFile
	data, err := os.ReadFile(path)
	if err != nil {
		return mf, err
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		return mf, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if mf.Format > sharedFormat {
		return mf, fmt.Errorf("%s was written by a newer tjek; update to keep syncing it", filepath.Base(path))
	}
	return mf, nil
}

// syncAllShared runs syncShared for every project this device shares. A
// project that fails does not stop the others; the errors come back together.
func syncAllShared(h *sql.DB, author string, b rank.Biases) (changed bool, err error) {
	c, err := loadSharedConfig()
	if err != nil || len(c.Projects) == 0 {
		return false, err
	}
	var errs []error
	for _, p := range c.Projects {
		res, err := syncShared(h, c, p, author, b)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name, err))
			continue
		}
		changed = changed || res.changed
	}
	return changed, errors.Join(errs...)
}
