package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func Write(path string, value Plan) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create plan directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".jev-sort-plan-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary plan: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish plan: %w", err)
	}
	return nil
}

func Read(path string) (Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("read plan: %w", err)
	}
	var value Plan
	if err := json.Unmarshal(data, &value); err != nil {
		return Plan{}, fmt.Errorf("decode plan: %w", err)
	}
	if value.Version != FormatVersion {
		return Plan{}, fmt.Errorf("unsupported plan version %d", value.Version)
	}
	return value, nil
}
