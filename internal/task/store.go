package task

import (
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var filePattern = regexp.MustCompile(`^T-(\d+)\.md$`)

// Path returns the file path of a task inside a tasks directory.
func Path(dir, id string) string { return filepath.Join(dir, id+".md") }

// Load reads the task with the given id from dir.
func Load(dir, id string) (Task, error) {
	data, err := os.ReadFile(Path(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return Task{}, fmt.Errorf(idioma.T("no existe la tarea %s"), id)
	}
	if err != nil {
		return Task{}, err
	}
	t, err := Parse(data)
	if err != nil {
		return Task{}, fmt.Errorf("%s.md: %s", id, err)
	}
	return t, nil
}

// Save writes the task to dir/<id>.md.
func Save(dir string, t Task) error {
	return os.WriteFile(Path(dir, t.ID), t.Marshal(), 0o644)
}

// Remove deletes the task file.
func Remove(dir, id string) error {
	err := os.Remove(Path(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(idioma.T("no existe la tarea %s"), id)
	}
	return err
}

// List returns every task in dir, sorted by ID. A malformed file fails
// the whole listing: the tasks directory must stay clean.
func List(dir string) ([]Task, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		t, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %s", e.Name(), err)
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// NextID scans dir and returns the first free correlative ID.
func NextID(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "T-001", nil
	}
	if err != nil {
		return "", err
	}
	max := 0
	for _, e := range entries {
		m := filePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("T-%03d", max+1), nil
}
