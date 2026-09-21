package plan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"jev-file-sort/internal/config"
	"jev-file-sort/internal/model"
	matchpattern "jev-file-sort/internal/pattern"
)

type Classifier interface {
	Classify(ctx context.Context, entry Entry) (Classification, error)
}

type Classification struct {
	Decision          model.Decision
	ContentSent       bool
	FolderSummarySent bool
}

type Entry struct {
	Path     string
	Relative string
	Name     string
	Kind     model.EntryKind
	Depth    int
	Info     os.FileInfo
	Children []Child
}

type Child struct {
	Name      string          `json:"name"`
	Kind      model.EntryKind `json:"kind"`
	Extension string          `json:"extension,omitempty"`
	Size      int64           `json:"size,omitempty"`
}

type Builder struct {
	Config     config.Config
	Classifier Classifier
}

func (b Builder) Build(ctx context.Context, root string) (Plan, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve target: %w", err)
	}
	info, err := os.Lstat(absoluteRoot)
	if err != nil {
		return Plan{}, fmt.Errorf("inspect target: %w", err)
	}
	outputRoot := b.Config.Output.Root
	if !filepath.IsAbs(outputRoot) {
		base := absoluteRoot
		if !info.IsDir() {
			base = filepath.Dir(absoluteRoot)
		}
		outputRoot = filepath.Join(base, outputRoot)
	}
	outputRoot, err = filepath.Abs(outputRoot)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve output: %w", err)
	}
	if samePath(absoluteRoot, outputRoot) {
		return Plan{}, fmt.Errorf("output root must differ from target root")
	}

	value := Plan{Version: FormatVersion, ID: randomID(), CreatedAt: time.Now().UTC(), Root: absoluteRoot, OutputRoot: outputRoot, Mode: b.Config.Mode, Config: b.Config}
	if b.Config.Mode == "jev" {
		value.Model = b.Config.Jev.Model
	}
	reserved := map[string]struct{}{}
	if info.IsDir() {
		entries, readErr := os.ReadDir(absoluteRoot)
		if readErr != nil {
			return Plan{}, readErr
		}
		for _, child := range entries {
			if err := b.visit(ctx, &value, filepath.Join(absoluteRoot, child.Name()), child.Name(), 0, outputRoot, reserved); err != nil {
				return Plan{}, err
			}
		}
	} else {
		if err := b.visit(ctx, &value, absoluteRoot, filepath.Base(absoluteRoot), 0, outputRoot, reserved); err != nil {
			return Plan{}, err
		}
	}
	return value, nil
}

func (b Builder) visit(ctx context.Context, value *Plan, path, relative string, depth int, outputRoot string, reserved map[string]struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if within(path, outputRoot) {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		value.Operations = append(value.Operations, errorOperation(path, relative, err))
		return nil
	}
	kind := model.EntryFile
	if info.Mode()&os.ModeSymlink != 0 {
		kind = model.EntrySymlink
	} else if info.IsDir() {
		kind = model.EntryFolder
	}
	entry := Entry{Path: path, Relative: filepath.ToSlash(relative), Name: info.Name(), Kind: kind, Depth: depth, Info: info}
	if kind == model.EntryFolder {
		entry.Children, _ = readChildren(path)
	}

	if hidden(entry.Name) && !config.Enabled(b.Config.Scan.IncludeHidden) {
		value.Operations = append(value.Operations, excludedOperation(entry, "hidden entry"))
		return nil
	}
	excluded, err := matchesAny(b.Config.Selection.Exclude, entry.Relative, entry.Name)
	if err != nil {
		return err
	}
	if excluded {
		value.Operations = append(value.Operations, excludedOperation(entry, "excluded by pattern"))
		return nil
	}
	if kind == model.EntrySymlink {
		value.Operations = append(value.Operations, excludedOperation(entry, "symlinks are not followed"))
		return nil
	}
	included := len(b.Config.Selection.Include) == 0
	if !included {
		included, err = matchesAny(b.Config.Selection.Include, entry.Relative, entry.Name)
		if err != nil {
			return err
		}
	}

	if included {
		classification, classifyErr := b.Classifier.Classify(ctx, entry)
		decision := classification.Decision
		if classifyErr != nil {
			value.Operations = append(value.Operations, errorOperation(path, relative, classifyErr))
			if kind != model.EntryFolder {
				return nil
			}
		} else if decision.Kind == model.DecisionCategory || (kind != model.EntryFolder && decision.Kind == model.DecisionUncategorized) {
			operation, planErr := b.operation(entry, classification, outputRoot, reserved)
			if planErr != nil {
				return planErr
			}
			value.Operations = append(value.Operations, operation)
			if kind == model.EntryFolder {
				return nil
			}
		} else if kind != model.EntryFolder {
			operation, planErr := b.operation(entry, classification, outputRoot, reserved)
			if planErr != nil {
				return planErr
			}
			value.Operations = append(value.Operations, operation)
		} else if classification.FolderSummarySent {
			value.Operations = append(value.Operations, Operation{ID: randomID(), Source: entry.Path, RelativeSource: entry.Relative, Kind: entry.Kind, Decision: decision, Status: "unchanged", Reason: "folder contents will be classified individually", FolderSummarySent: true})
		}
	}
	if kind != model.EntryFolder {
		return nil
	}
	if !config.Enabled(b.Config.Scan.Recursive) {
		return nil
	}
	if b.Config.Scan.MaxDepth != nil && depth >= *b.Config.Scan.MaxDepth {
		return nil
	}
	children, err := os.ReadDir(path)
	if err != nil {
		value.Operations = append(value.Operations, errorOperation(path, relative, err))
		return nil
	}
	for _, child := range children {
		childRelative := filepath.Join(relative, child.Name())
		if err := b.visit(ctx, value, filepath.Join(path, child.Name()), childRelative, depth+1, outputRoot, reserved); err != nil {
			return err
		}
	}
	return nil
}

func (b Builder) operation(entry Entry, classification Classification, outputRoot string, reserved map[string]struct{}) (Operation, error) {
	decision := classification.Decision
	fingerprint, err := FingerprintPath(entry.Path, entry.Kind)
	if err != nil {
		return errorOperation(entry.Path, entry.Relative, err), nil
	}
	op := Operation{ID: randomID(), Source: entry.Path, RelativeSource: entry.Relative, Kind: entry.Kind, Decision: decision, Status: "unchanged", Fingerprint: fingerprint, ContentSent: classification.ContentSent, FolderSummarySent: classification.FolderSummarySent}
	directory := ""
	if decision.Kind == model.DecisionCategory {
		category, ok := b.Config.Category(decision.CategoryID)
		if !ok {
			op.Status = "error"
			op.Reason = "selected category no longer exists"
			return op, nil
		}
		directory = category.Directory
	} else if decision.Kind == model.DecisionUncategorized && b.Config.Uncategorized.Action == "move" {
		directory = b.Config.Uncategorized.Directory
	} else {
		op.Reason = "left in place by policy"
		return op, nil
	}
	destination := destinationPath(outputRoot, directory, entry.Relative, entry.Name, b.Config.Output.Layout)
	if samePath(entry.Path, destination) || within(destination, entry.Path) {
		op.Status = "error"
		op.Reason = "destination overlaps source"
		return op, nil
	}
	if b.Config.Output.Collision == "number" {
		destination = availableDestination(destination, reserved)
	}
	if _, exists := reserved[canonical(destination)]; exists {
		op.Status = "skipped"
		op.Reason = "destination is already reserved"
		return op, nil
	}
	if _, err := os.Lstat(destination); err == nil {
		op.Status = "skipped"
		op.Reason = "destination already exists"
		return op, nil
	} else if !os.IsNotExist(err) {
		return Operation{}, err
	}
	reserved[canonical(destination)] = struct{}{}
	op.Destination = destination
	op.Status = "planned"
	return op, nil
}

func readChildren(directory string) ([]Child, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	children := make([]Child, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		kind := model.EntryFile
		if info.Mode()&os.ModeSymlink != 0 {
			kind = model.EntrySymlink
		} else if info.IsDir() {
			kind = model.EntryFolder
		}
		children = append(children, Child{Name: entry.Name(), Kind: kind, Extension: strings.ToLower(filepath.Ext(entry.Name())), Size: info.Size()})
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
	return children, nil
}

func FingerprintPath(path string, kind model.EntryKind) (model.Fingerprint, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return model.Fingerprint{}, err
	}
	if kind == model.EntryFile {
		file, err := os.Open(path)
		if err != nil {
			return model.Fingerprint{}, err
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return model.Fingerprint{}, err
		}
		return model.Fingerprint{SHA256: hex.EncodeToString(hash.Sum(nil)), Size: info.Size(), ModifiedAt: info.ModTime().UTC()}, nil
	}
	if kind == model.EntryFolder {
		hash := sha256.New()
		count := 0
		err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if current == path {
				return nil
			}
			count++
			relative, _ := filepath.Rel(path, current)
			fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(relative), entry.Type())
			if entry.Type()&os.ModeSymlink != 0 {
				target, err := os.Readlink(current)
				if err != nil {
					return err
				}
				fmt.Fprint(hash, target)
				return nil
			}
			if !entry.IsDir() {
				file, err := os.Open(current)
				if err != nil {
					return err
				}
				_, copyErr := io.Copy(hash, file)
				file.Close()
				return copyErr
			}
			return nil
		})
		if err != nil {
			return model.Fingerprint{}, err
		}
		return model.Fingerprint{SHA256: hex.EncodeToString(hash.Sum(nil)), ModifiedAt: info.ModTime().UTC(), EntryCount: count}, nil
	}
	return model.Fingerprint{Size: info.Size(), ModifiedAt: info.ModTime().UTC()}, nil
}

func matchesAny(patterns []string, relative, name string) (bool, error) {
	for _, candidate := range patterns {
		matched, err := matchpattern.Match(candidate, relative, name)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func randomID() string {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}
func hidden(name string) bool { return strings.HasPrefix(name, ".") }
func canonical(path string) string {
	absolute, _ := filepath.Abs(path)
	cleaned := filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}
func samePath(a, b string) bool { return canonical(a) == canonical(b) }
func destinationPath(outputRoot, categoryDirectory, relative, name, layout string) string {
	if layout == "preserve" {
		return filepath.Join(outputRoot, categoryDirectory, filepath.FromSlash(relative))
	}
	return filepath.Join(outputRoot, categoryDirectory, name)
}
func within(path, parent string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func availableDestination(path string, reserved map[string]struct{}) string {
	if _, exists := reserved[canonical(path)]; !exists {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path
		}
	}
	directory, name := filepath.Split(path)
	extension := compoundExtension(name)
	base := strings.TrimSuffix(name, extension)
	for index := 1; ; index++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s (%d)%s", base, index, extension))
		_, reservedAlready := reserved[canonical(candidate)]
		_, statErr := os.Lstat(candidate)
		if !reservedAlready && os.IsNotExist(statErr) {
			return candidate
		}
	}
}
func compoundExtension(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tar.xz"} {
		if strings.HasSuffix(lower, ext) {
			return name[len(name)-len(ext):]
		}
	}
	return filepath.Ext(name)
}
func excludedOperation(entry Entry, reason string) Operation {
	return Operation{ID: randomID(), Source: entry.Path, RelativeSource: entry.Relative, Kind: entry.Kind, Decision: model.Decision{Kind: model.DecisionSkip}, Status: "excluded", Reason: reason}
}
func errorOperation(path, relative string, err error) Operation {
	return Operation{ID: randomID(), Source: path, RelativeSource: filepath.ToSlash(relative), Decision: model.Decision{Kind: model.DecisionError}, Status: "error", Reason: err.Error()}
}
