package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"jev-file-sort/internal/plan"
)

func Apply(ctx context.Context, value plan.Plan) (Result, error) {
	store, err := NewStore(value.Config.History.Directory)
	if err != nil {
		return Result{}, err
	}
	lock, err := store.Lock()
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	if err := invalidateRedo(store); err != nil {
		return Result{}, err
	}
	run := Run{Version: historyVersion, ID: newID(), PlanID: value.ID, CreatedAt: time.Now().UTC(), Status: "running", HistoryEnabled: value.Config.History.Enabled == nil || *value.Config.History.Enabled, Plan: value}
	for _, operation := range value.Operations {
		if operation.Status == "planned" {
			run.Operations = append(run.Operations, RunOperation{PlanOperation: operation, State: "pending"})
		}
	}
	if err := validateOperations(value, run.Operations); err != nil {
		return Result{}, err
	}
	if err := store.Save(&run); err != nil {
		return Result{}, err
	}
	result := Result{RunID: run.ID, Status: "completed", Counts: map[string]int{}}
	for index := range run.Operations {
		if err := ctx.Err(); err != nil {
			run.Status = "cancelled"
			result.Status = "cancelled"
			break
		}
		op := &run.Operations[index]
		destination, prepareErr := prepare(op.PlanOperation, value.Config.Output.Collision)
		if prepareErr != nil {
			op.State = "failed"
			op.Error = prepareErr.Error()
			result.Counts["failed"]++
			continue
		}
		op.ActualDestination, op.State = destination, "copying"
		if err := store.Save(&run); err != nil {
			return result, err
		}
		if err := copyAndRemove(op.PlanOperation.Source, destination, op.PlanOperation.Kind); err != nil {
			op.State = "failed"
			op.Error = err.Error()
			result.Counts["failed"]++
			store.Save(&run)
			continue
		}
		op.State = "completed"
		result.Counts["completed"]++
		if err := store.Save(&run); err != nil {
			return result, err
		}
	}
	if result.Counts["failed"] > 0 {
		run.Status = "partial"
		result.Status = "partial"
	} else if run.Status == "running" {
		run.Status = "completed"
	}
	if err := store.Save(&run); err != nil {
		return result, err
	}
	if !run.HistoryEnabled && run.Status == "completed" {
		_ = store.Remove(run.ID)
	}
	return result, nil
}

func Undo(ctx context.Context, id, historyDirectory string) (Result, error) {
	store, err := NewStore(historyDirectory)
	if err != nil {
		return Result{}, err
	}
	lock, err := store.Lock()
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	run, err := store.Load(id)
	if err != nil {
		return Result{}, err
	}
	if run.Status == "redo_invalid" {
		return Result{}, fmt.Errorf("redo was invalidated by a newer apply")
	}
	if !run.HistoryEnabled {
		return Result{}, fmt.Errorf("run %s has no retained history", id)
	}
	result := Result{RunID: id, Status: "undone", Counts: map[string]int{}}
	for index := len(run.Operations) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			result.Status = "cancelled"
			run.Status = "undo_partial"
			break
		}
		op := &run.Operations[index]
		if op.State != "completed" {
			continue
		}
		if _, err := os.Lstat(op.PlanOperation.Source); err == nil {
			op.State = "undo_failed"
			op.Error = "original source path is occupied"
			result.Counts["failed"]++
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			op.State = "undo_failed"
			op.Error = err.Error()
			result.Counts["failed"]++
			continue
		}
		actual, err := plan.FingerprintPath(op.ActualDestination, op.PlanOperation.Kind)
		if err != nil || actual.SHA256 != op.PlanOperation.Fingerprint.SHA256 {
			op.State = "undo_failed"
			op.Error = "destination changed after apply"
			result.Counts["failed"]++
			continue
		}
		op.State = "undoing"
		store.Save(&run)
		if err := copyAndRemove(op.ActualDestination, op.PlanOperation.Source, op.PlanOperation.Kind); err != nil {
			op.State = "undo_failed"
			op.Error = err.Error()
			result.Counts["failed"]++
			continue
		}
		op.State = "undone"
		op.Error = ""
		result.Counts["undone"]++
		pruneEmpty(filepath.Dir(op.ActualDestination), run.Plan.OutputRoot)
		store.Save(&run)
	}
	if result.Counts["failed"] > 0 {
		run.Status = "undo_partial"
		result.Status = "partial"
	} else if result.Status != "cancelled" {
		run.Status = "undone"
	}
	if err := store.Save(&run); err != nil {
		return result, err
	}
	return result, nil
}

func Recover(id, historyDirectory string) (Result, error) {
	store, err := NewStore(historyDirectory)
	if err != nil {
		return Result{}, err
	}
	lock, err := store.Lock()
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	run, err := store.Load(id)
	if err != nil {
		return Result{}, err
	}
	result := Result{RunID: id, Status: run.Status, Counts: map[string]int{}}
	for index := range run.Operations {
		op := &run.Operations[index]
		if op.State != "copying" && op.State != "undoing" && op.State != "redoing" {
			continue
		}
		sourceExists := exists(op.PlanOperation.Source)
		destinationExists := exists(op.ActualDestination)
		if op.State == "undoing" {
			switch {
			case sourceExists && !destinationExists && matchesFingerprint(op.PlanOperation.Source, op.PlanOperation):
				op.State, op.Error = "undone", ""
				result.Counts["undone"]++
			case !sourceExists && destinationExists && matchesFingerprint(op.ActualDestination, op.PlanOperation):
				op.State, op.Error = "completed", "undo stopped before restoring the source"
				result.Counts["completed"]++
			default:
				op.State, op.Error = "attention", "filesystem state does not match the saved undo operation"
				result.Counts["attention"]++
			}
			continue
		}
		switch {
		case !sourceExists && destinationExists && matchesFingerprint(op.ActualDestination, op.PlanOperation):
			op.State, op.Error = "completed", ""
			result.Counts["completed"]++
		case sourceExists && !destinationExists && matchesFingerprint(op.PlanOperation.Source, op.PlanOperation):
			op.State, op.Error = "failed", "operation stopped before destination was published"
			result.Counts["failed"]++
		case sourceExists && destinationExists:
			op.State, op.Error = "attention", "source and destination both exist; no automatic deletion was performed"
			result.Counts["attention"]++
		default:
			op.State, op.Error = "attention", "filesystem state does not match the saved operation"
			result.Counts["attention"]++
		}
	}
	if result.Counts["attention"] > 0 || result.Counts["failed"] > 0 {
		run.Status, result.Status = "recovery_attention", "attention"
	} else {
		run.Status, result.Status = "completed", "completed"
	}
	if err := store.Save(&run); err != nil {
		return result, err
	}
	return result, nil
}

func Redo(ctx context.Context, id, historyDirectory string) (Result, error) {
	store, err := NewStore(historyDirectory)
	if err != nil {
		return Result{}, err
	}
	lock, err := store.Lock()
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	run, err := store.Load(id)
	if err != nil {
		return Result{}, err
	}
	result := Result{RunID: id, Status: "completed", Counts: map[string]int{}}
	for index := range run.Operations {
		if err := ctx.Err(); err != nil {
			run.Status = "redo_partial"
			result.Status = "cancelled"
			break
		}
		op := &run.Operations[index]
		if op.State != "undone" {
			continue
		}
		destination, err := prepare(op.PlanOperation, run.Plan.Config.Output.Collision)
		if err != nil {
			op.State = "redo_failed"
			op.Error = err.Error()
			result.Counts["failed"]++
			continue
		}
		op.ActualDestination, op.State = destination, "redoing"
		store.Save(&run)
		if err := copyAndRemove(op.PlanOperation.Source, destination, op.PlanOperation.Kind); err != nil {
			op.State = "redo_failed"
			op.Error = err.Error()
			result.Counts["failed"]++
			continue
		}
		op.State = "completed"
		op.Error = ""
		result.Counts["completed"]++
		store.Save(&run)
	}
	if result.Counts["failed"] > 0 {
		run.Status = "redo_partial"
		result.Status = "partial"
	} else if result.Status != "cancelled" {
		run.Status = "completed"
	}
	if err := store.Save(&run); err != nil {
		return result, err
	}
	return result, nil
}

func prepare(operation plan.Operation, collision string) (string, error) {
	actual, err := plan.FingerprintPath(operation.Source, operation.Kind)
	if err != nil {
		return "", err
	}
	if actual.SHA256 != operation.Fingerprint.SHA256 || actual.Size != operation.Fingerprint.Size || actual.EntryCount != operation.Fingerprint.EntryCount || !actual.ModifiedAt.Equal(operation.Fingerprint.ModifiedAt) {
		return "", fmt.Errorf("source changed after plan creation")
	}
	destination := operation.Destination
	if _, err := os.Lstat(destination); err == nil {
		if collision == "skip" {
			return "", fmt.Errorf("destination already exists")
		}
		destination = numbered(destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return destination, nil
}

func validateOperations(value plan.Plan, operations []RunOperation) error {
	seenSources, seenDestinations := map[string]struct{}{}, map[string]struct{}{}
	for _, operation := range operations {
		source, destination := canonical(operation.PlanOperation.Source), canonical(operation.PlanOperation.Destination)
		if _, exists := seenSources[source]; exists {
			return fmt.Errorf("duplicate source %s", operation.PlanOperation.Source)
		}
		if _, exists := seenDestinations[destination]; exists {
			return fmt.Errorf("duplicate destination %s", operation.PlanOperation.Destination)
		}
		if !contained(operation.PlanOperation.Destination, value.OutputRoot) {
			return fmt.Errorf("destination escapes output root: %s", operation.PlanOperation.Destination)
		}
		for previous := range seenSources {
			if contained(source, previous) || contained(previous, source) {
				return fmt.Errorf("overlapping source operations")
			}
		}
		seenSources[source], seenDestinations[destination] = struct{}{}, struct{}{}
	}
	return nil
}

func invalidateRedo(store Store) error {
	runs, err := store.List()
	if err != nil {
		return err
	}
	for index := range runs {
		if runs[index].Status != "undone" && runs[index].Status != "undo_partial" {
			continue
		}
		runs[index].Status = "redo_invalid"
		if err := store.Save(&runs[index]); err != nil {
			return err
		}
	}
	return nil
}

func matchesFingerprint(path string, operation plan.Operation) bool {
	actual, err := plan.FingerprintPath(path, operation.Kind)
	return err == nil && actual.SHA256 == operation.Fingerprint.SHA256 && actual.Size == operation.Fingerprint.Size && actual.EntryCount == operation.Fingerprint.EntryCount
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func numbered(path string) string {
	directory, name := filepath.Split(path)
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	for index := 1; ; index++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s (%d)%s", base, index, extension))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}
func pruneEmpty(directory, stop string) {
	for contained(directory, stop) {
		if err := os.Remove(directory); err != nil {
			return
		}
		if canonical(directory) == canonical(stop) {
			return
		}
		directory = filepath.Dir(directory)
	}
}
func canonical(path string) string {
	absolute, _ := filepath.Abs(path)
	clean := filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}
func contained(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func newID() string { return fmt.Sprintf("run-%d-%d", time.Now().UTC().UnixNano(), os.Getpid()) }
