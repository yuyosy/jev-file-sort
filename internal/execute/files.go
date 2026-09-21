package execute

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"jev-file-sort/internal/model"
)

func copyAndRemove(source, destination string, kind model.EntryKind) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if kind == model.EntryFolder {
		return copyDirectoryAndRemove(source, destination)
	}
	if kind != model.EntryFile {
		return fmt.Errorf("unsupported entry kind %q", kind)
	}
	return copyFileAndRemove(source, destination)
}

func copyFileAndRemove(source, destination string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	info, err := sourceFile.Stat()
	if err != nil {
		sourceFile.Close()
		return err
	}
	destinationFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		sourceFile.Close()
		return err
	}
	sourceHash, destinationHash := sha256.New(), sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(destinationFile, destinationHash), io.TeeReader(sourceFile, sourceHash))
	if copyErr == nil {
		copyErr = destinationFile.Sync()
	}
	closeDestinationErr := destinationFile.Close()
	closeSourceErr := sourceFile.Close()
	if copyErr != nil || closeDestinationErr != nil || closeSourceErr != nil || !strings.EqualFold(fmt.Sprintf("%x", sourceHash.Sum(nil)), fmt.Sprintf("%x", destinationHash.Sum(nil))) {
		os.Remove(destination)
		if copyErr != nil {
			return copyErr
		}
		if closeDestinationErr != nil {
			return closeDestinationErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
		return fmt.Errorf("copied file failed integrity verification")
	}
	if err := os.Chmod(destination, info.Mode().Perm()); err != nil {
		os.Remove(destination)
		return err
	}
	if err := os.Chtimes(destination, info.ModTime(), info.ModTime()); err != nil {
		os.Remove(destination)
		return err
	}
	if err := os.Remove(source); err != nil {
		return fmt.Errorf("destination copied but source removal failed: %w", err)
	}
	return nil
}

func copyDirectoryAndRemove(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if err := os.Mkdir(destination, info.Mode().Perm()); err != nil {
		return err
	}
	completed := false
	defer func() {
		if !completed {
			os.RemoveAll(destination)
		}
	}()
	err = filepath.WalkDir(source, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == source {
			return nil
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		entryInfo, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if entry.IsDir() {
			return os.Mkdir(target, entryInfo.Mode().Perm())
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported special file %s", current)
		}
		return copyFileWithoutRemove(current, target, entryInfo)
	})
	if err != nil {
		return err
	}
	if err := os.Chtimes(destination, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	if err := os.RemoveAll(source); err != nil {
		return fmt.Errorf("destination copied but source removal failed: %w", err)
	}
	completed = true
	return nil
}

func copyFileWithoutRemove(source, destination string, info os.FileInfo) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	if err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chtimes(destination, info.ModTime(), info.ModTime())
}
