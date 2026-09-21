package execute

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Store struct{ Directory string }

func NewStore(configured string) (Store, error) {
	if configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return Store{}, err
		}
		return Store{Directory: absolute}, nil
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Directory: filepath.Join(directory, "jev-file-sort", "history")}, nil
}

func (s Store) Save(run *Run) error {
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return err
	}
	run.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := s.path(run.ID)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open history: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (s Store) Load(id string) (Run, error) {
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return Run{}, fmt.Errorf("decode history: %w", err)
	}
	if run.Version != historyVersion {
		return Run{}, fmt.Errorf("unsupported history version %d", run.Version)
	}
	return run, nil
}

func (s Store) List() ([]Run, error) {
	entries, err := os.ReadDir(s.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []Run
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		run, loadErr := s.Load(entry.Name()[:len(entry.Name())-5])
		if loadErr == nil && run.HistoryEnabled {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.After(runs[j].CreatedAt) })
	return runs, nil
}

func (s Store) Remove(id string) error {
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s Store) path(id string) string { return filepath.Join(s.Directory, id+".json") }

type Lock struct {
	path string
	file *os.File
}

func (s Store) Lock() (*Lock, error) {
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Directory, "mutation.lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("another apply, undo, or redo is active; remove %s only after confirming no process is running", path)
		}
		return nil, err
	}
	fmt.Fprintf(file, "pid=%d\ncreated_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	file.Sync()
	return &Lock{path: path, file: file}, nil
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	closeErr := l.file.Close()
	removeErr := os.Remove(l.path)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}
