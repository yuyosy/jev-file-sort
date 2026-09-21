package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Diagnostic struct {
	Level   string `json:"level" yaml:"level"`
	Subject string `json:"subject,omitempty" yaml:"subject,omitempty"`
	Message string `json:"message" yaml:"message"`
}

func Validate(cfg Config) []Diagnostic {
	var out []Diagnostic
	errorf := func(subject, format string, args ...any) {
		out = append(out, Diagnostic{Level: "error", Subject: subject, Message: fmt.Sprintf(format, args...)})
	}
	warnf := func(subject, format string, args ...any) {
		out = append(out, Diagnostic{Level: "warn", Subject: subject, Message: fmt.Sprintf(format, args...)})
	}
	if cfg.Version != 1 {
		errorf("version", "unsupported configuration version %d", cfg.Version)
	}
	if cfg.Mode != "simple" && cfg.Mode != "jev" {
		errorf("mode", "must be simple or jev")
	}
	if cfg.Output.Root == "" {
		errorf("output.root", "must not be empty")
	}
	if cfg.Output.Collision != "skip" && cfg.Output.Collision != "number" {
		errorf("output.collision", "must be skip or number")
	}
	if cfg.Uncategorized.Action != "leave" && cfg.Uncategorized.Action != "move" {
		errorf("uncategorized.action", "must be leave or move")
	}
	if cfg.Uncategorized.Action == "move" && cfg.Uncategorized.Directory == "" {
		errorf("uncategorized.directory", "is required when action is move")
	}
	if cfg.Scan.MaxDepth != nil && *cfg.Scan.MaxDepth < 0 {
		errorf("scan.max_depth", "must be zero or greater")
	}
	if cfg.Content.MaxBytes < 0 {
		errorf("content.max_bytes", "must be zero or greater")
	}
	if cfg.Jev.Threshold < 0 || cfg.Jev.Threshold > 1 {
		errorf("jev.threshold", "must be between zero and one")
	}
	if cfg.Jev.TimeoutSeconds <= 0 {
		errorf("jev.timeout_seconds", "must be greater than zero")
	}
	if cfg.Jev.MaxRetries < 0 {
		errorf("jev.max_retries", "must be zero or greater")
	}
	if cfg.Jev.Concurrency <= 0 {
		errorf("jev.concurrency", "must be greater than zero")
	}
	if cfg.Jev.FolderEvaluation.MaxEntries <= 0 {
		errorf("jev.folder_evaluation.max_entries", "must be greater than zero")
	}

	categories := map[string]Category{}
	for i, category := range cfg.Categories {
		subject := fmt.Sprintf("categories[%d]", i)
		if category.ID == "" {
			errorf(subject, "id must not be empty")
			continue
		}
		if _, exists := categories[category.ID]; exists {
			errorf(subject, "duplicate category id %q", category.ID)
		}
		categories[category.ID] = category
		if category.Directory == "" {
			errorf(subject, "directory must not be empty")
		}
		if filepath.IsAbs(category.Directory) || strings.Contains(filepath.ToSlash(category.Directory), "/") {
			errorf(subject, "directory must be a single relative path component")
		}
		if category.Threshold != nil && (*category.Threshold < 0 || *category.Threshold > 1) {
			errorf(subject, "threshold must be between zero and one")
		}
	}

	ruleIDs := map[string]struct{}{}
	seenMatchers := map[string]string{}
	for i, rule := range cfg.Rules {
		subject := fmt.Sprintf("rules[%d]", i)
		if rule.ID == "" {
			errorf(subject, "id must not be empty")
			continue
		}
		if _, exists := ruleIDs[rule.ID]; exists {
			errorf(subject, "duplicate rule id %q", rule.ID)
		}
		ruleIDs[rule.ID] = struct{}{}
		if !Enabled(rule.Enabled) {
			continue
		}
		category, exists := categories[rule.Category]
		if !exists {
			errorf(subject, "references unknown category %q", rule.Category)
		} else if !Enabled(category.Enabled) {
			warnf(subject, "references disabled category %q and cannot match", rule.Category)
		}
		if len(rule.Kinds) == 0 {
			errorf(subject, "kinds must contain file, folder, or both")
		}
		hasFile, hasFolder := false, false
		for _, kind := range rule.Kinds {
			switch kind {
			case "file":
				hasFile = true
			case "folder":
				hasFolder = true
			default:
				errorf(subject, "unknown kind %q", kind)
			}
		}
		if len(rule.Match.Extensions) > 0 && !hasFile {
			errorf(subject, "extensions matcher requires file kind")
		}
		if len(rule.Match.ChildExtensions) > 0 && !hasFolder {
			errorf(subject, "child_extensions matcher requires folder kind")
		}
		if len(rule.Match.Patterns) == 0 && len(rule.Match.Extensions) == 0 && len(rule.Match.ChildExtensions) == 0 {
			errorf(subject, "at least one matcher is required")
		}
		if rule.Match.Depth != nil {
			if rule.Match.Depth.Min != nil && *rule.Match.Depth.Min < 0 {
				errorf(subject, "depth.min must be zero or greater")
			}
			if rule.Match.Depth.Max != nil && *rule.Match.Depth.Max < 0 {
				errorf(subject, "depth.max must be zero or greater")
			}
			if rule.Match.Depth.Min != nil && rule.Match.Depth.Max != nil && *rule.Match.Depth.Min > *rule.Match.Depth.Max {
				errorf(subject, "depth.min must not exceed depth.max")
			}
		}
		for _, value := range append(append([]string{}, rule.Match.Patterns...), rule.Match.Extensions...) {
			key := strings.ToLower(strings.Join(rule.Kinds, ",") + "\x00" + value)
			if earlier, exists := seenMatchers[key]; exists {
				warnf(subject, "matcher %q also appears in earlier rule %q", value, earlier)
			} else {
				seenMatchers[key] = rule.ID
			}
		}
	}
	return out
}

func HasErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Level == "error" {
			return true
		}
	}
	return false
}
