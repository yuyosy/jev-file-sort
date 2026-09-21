package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
)

func Load(explicitPath string) (Config, error) {
	result := Defaults()
	if userPath, err := userConfigPath(); err == nil {
		if err := mergeFile(&result, userPath, false); err != nil {
			return Config{}, err
		}
	}
	if explicitPath != "" {
		path, err := filepath.Abs(explicitPath)
		if err != nil {
			return Config{}, fmt.Errorf("resolve config path: %w", err)
		}
		if err := mergeFile(&result, path, true); err != nil {
			return Config{}, err
		}
	}
	result.LoadedAt = time.Now().UTC()
	return result, nil
}

func userConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "jev-file-sort", "config.yaml"), nil
}

func mergeFile(dst *Config, path string, required bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !required && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config %q: %w", path, err)
	}
	var overlay Config
	if err := yaml.UnmarshalWithOptions(data, &overlay, yaml.Strict()); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	merge(dst, overlay)
	dst.SourceFiles = append(dst.SourceFiles, path)
	return nil
}

func merge(dst *Config, src Config) {
	if src.Version != 0 {
		dst.Version = src.Version
	}
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	mergeSelection(&dst.Selection, src.Selection)
	mergeScan(&dst.Scan, src.Scan)
	if src.Output.Root != "" {
		dst.Output.Root = src.Output.Root
	}
	if src.Output.Collision != "" {
		dst.Output.Collision = src.Output.Collision
	}
	if src.Uncategorized.Action != "" {
		dst.Uncategorized.Action = src.Uncategorized.Action
	}
	if src.Uncategorized.Directory != "" {
		dst.Uncategorized.Directory = src.Uncategorized.Directory
	}
	if src.Content.Enabled != nil {
		dst.Content.Enabled = src.Content.Enabled
	}
	if src.Content.AllowPatterns != nil {
		dst.Content.AllowPatterns = src.Content.AllowPatterns
	}
	if src.Content.MaxBytes != 0 {
		dst.Content.MaxBytes = src.Content.MaxBytes
	}
	mergeJev(&dst.Jev, src.Jev)
	if src.History.Enabled != nil {
		dst.History.Enabled = src.History.Enabled
	}
	if src.History.Directory != "" {
		dst.History.Directory = src.History.Directory
	}
	if src.History.MaxEntries != 0 {
		dst.History.MaxEntries = src.History.MaxEntries
	}
	for _, category := range src.Categories {
		mergeCategory(&dst.Categories, category)
	}
	var additions []Rule
	for _, rule := range src.Rules {
		if !mergeRule(&dst.Rules, rule) {
			additions = append(additions, rule)
		}
	}
	if len(additions) > 0 {
		dst.Rules = append(additions, dst.Rules...)
	}
}

func mergeSelection(dst *Selection, src Selection) {
	if src.Include != nil {
		dst.Include = src.Include
	}
	if src.Exclude != nil {
		dst.Exclude = src.Exclude
	}
}

func mergeScan(dst *Scan, src Scan) {
	if src.Recursive != nil {
		dst.Recursive = src.Recursive
	}
	if src.MaxDepth != nil {
		dst.MaxDepth = src.MaxDepth
		if dst.Recursive == nil || !*dst.Recursive {
			dst.Recursive = Bool(true)
		}
	}
	if src.IncludeHidden != nil {
		dst.IncludeHidden = src.IncludeHidden
	}
	if src.FollowSymlinks != nil {
		dst.FollowSymlinks = src.FollowSymlinks
	}
}

func mergeJev(dst *Jev, src Jev) {
	if src.Endpoint != "" {
		dst.Endpoint = src.Endpoint
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.Threshold != 0 {
		dst.Threshold = src.Threshold
	}
	if src.TimeoutSeconds != 0 {
		dst.TimeoutSeconds = src.TimeoutSeconds
	}
	if src.MaxRetries != 0 {
		dst.MaxRetries = src.MaxRetries
	}
	if src.Concurrency != 0 {
		dst.Concurrency = src.Concurrency
	}
	if src.FolderEvaluation.Enabled != nil {
		dst.FolderEvaluation.Enabled = src.FolderEvaluation.Enabled
	}
	if src.FolderEvaluation.MaxEntries != 0 {
		dst.FolderEvaluation.MaxEntries = src.FolderEvaluation.MaxEntries
	}
}

func mergeCategory(categories *[]Category, src Category) {
	for i := range *categories {
		if (*categories)[i].ID != src.ID {
			continue
		}
		dst := &(*categories)[i]
		if src.Name != "" {
			dst.Name = src.Name
		}
		if src.Enabled != nil {
			dst.Enabled = src.Enabled
		}
		if src.Description != "" {
			dst.Description = src.Description
		}
		if src.Directory != "" {
			dst.Directory = src.Directory
		}
		if src.Threshold != nil {
			dst.Threshold = src.Threshold
		}
		return
	}
	*categories = append(*categories, src)
}

func mergeRule(rules *[]Rule, src Rule) bool {
	for i := range *rules {
		if (*rules)[i].ID != src.ID {
			continue
		}
		dst := &(*rules)[i]
		if src.Enabled != nil {
			dst.Enabled = src.Enabled
		}
		if src.Kinds != nil {
			dst.Kinds = src.Kinds
		}
		if src.Match.Patterns != nil {
			dst.Match.Patterns = src.Match.Patterns
		}
		if src.Match.Extensions != nil {
			dst.Match.Extensions = src.Match.Extensions
		}
		if src.Match.Depth != nil {
			dst.Match.Depth = src.Match.Depth
		}
		if src.Match.ChildExtensions != nil {
			dst.Match.ChildExtensions = src.Match.ChildExtensions
		}
		if src.Category != "" {
			dst.Category = src.Category
		}
		return true
	}
	return false
}
