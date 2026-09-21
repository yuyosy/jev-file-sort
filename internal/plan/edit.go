package plan

import (
	"fmt"
	"os"
	"path/filepath"

	"jev-file-sort/internal/model"
)

func SetCategory(value *Plan, index int, categoryID string) error {
	if index < 0 || index >= len(value.Operations) {
		return fmt.Errorf("operation index out of range")
	}
	category, exists := value.Config.Category(categoryID)
	if !exists {
		return fmt.Errorf("unknown category %q", categoryID)
	}
	operation := &value.Operations[index]
	if operation.Kind != model.EntryFile && operation.Kind != model.EntryFolder {
		return fmt.Errorf("entry cannot be categorized")
	}
	if operation.Fingerprint.SHA256 == "" {
		fingerprint, err := FingerprintPath(operation.Source, operation.Kind)
		if err != nil {
			return fmt.Errorf("fingerprint entry: %w", err)
		}
		operation.Fingerprint = fingerprint
	}
	operation.Decision.Kind, operation.Decision.CategoryID, operation.Decision.RuleID = model.DecisionCategory, categoryID, ""
	operation.Manual = true
	operation.Destination = filepath.Join(value.OutputRoot, category.Directory, filepath.Base(operation.Source))
	operation.Status, operation.Reason = "planned", ""
	reserved := map[string]struct{}{}
	for i := range value.Operations {
		candidate := &value.Operations[i]
		if candidate.Status != "planned" || candidate.Destination == "" {
			continue
		}
		destination := candidate.Destination
		_, already := reserved[canonical(destination)]
		_, statErr := os.Lstat(destination)
		if already || statErr == nil {
			if value.Config.Output.Collision == "number" {
				destination = availableDestination(destination, reserved)
				candidate.Destination = destination
			} else {
				candidate.Status, candidate.Reason = "skipped", "destination collision after manual edit"
				continue
			}
		}
		reserved[canonical(destination)] = struct{}{}
	}
	return nil
}
