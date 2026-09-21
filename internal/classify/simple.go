package classify

import (
	"context"
	"path/filepath"
	"strings"

	"jev-file-sort/internal/config"
	"jev-file-sort/internal/model"
	matchpattern "jev-file-sort/internal/pattern"
	"jev-file-sort/internal/plan"
)

type Simple struct{ Config config.Config }

func (s Simple) Classify(_ context.Context, entry plan.Entry) (plan.Classification, error) {
	if entry.Kind == model.EntryFolder && !config.Enabled(s.Config.Folders.RulesEnabled) {
		return plan.Classification{Decision: model.Decision{Kind: model.DecisionDescend}}, nil
	}
	var matches []config.Rule
	for _, rule := range s.Config.Rules {
		if !config.Enabled(rule.Enabled) || !supports(rule.Kinds, entry.Kind) {
			continue
		}
		category, exists := s.Config.Category(rule.Category)
		if !exists || !config.Enabled(category.Enabled) {
			continue
		}
		matched, err := matchRule(rule, entry)
		if err != nil {
			return plan.Classification{}, err
		}
		if matched {
			matches = append(matches, rule)
		}
	}
	if len(matches) == 0 {
		if entry.Kind == model.EntryFolder {
			return plan.Classification{Decision: model.Decision{Kind: model.DecisionDescend}}, nil
		}
		return plan.Classification{Decision: model.Decision{Kind: model.DecisionUncategorized}}, nil
	}
	decision := model.Decision{Kind: model.DecisionCategory, CategoryID: matches[0].Category, RuleID: matches[0].ID}
	for _, conflict := range matches[1:] {
		decision.Warnings = append(decision.Warnings, "also matched later rule "+conflict.ID)
	}
	return plan.Classification{Decision: decision}, nil
}

func matchRule(rule config.Rule, entry plan.Entry) (bool, error) {
	if rule.Match.Depth != nil {
		if rule.Match.Depth.Min != nil && entry.Depth < *rule.Match.Depth.Min {
			return false, nil
		}
		if rule.Match.Depth.Max != nil && entry.Depth > *rule.Match.Depth.Max {
			return false, nil
		}
	}
	if len(rule.Match.Patterns) > 0 {
		matched := false
		for _, candidate := range rule.Match.Patterns {
			ok, err := matchpattern.Match(candidate, entry.Relative, entry.Name)
			if err != nil {
				return false, err
			}
			if ok {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	if len(rule.Match.Extensions) > 0 {
		matched := false
		lower := strings.ToLower(entry.Name)
		for _, extension := range rule.Match.Extensions {
			normalized := strings.ToLower(extension)
			if !strings.HasPrefix(normalized, ".") {
				normalized = "." + normalized
			}
			if strings.HasSuffix(lower, normalized) {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	if len(rule.Match.ChildExtensions) > 0 {
		matched := false
		for _, child := range entry.Children {
			for _, extension := range rule.Match.ChildExtensions {
				if child.Extension == strings.ToLower(filepath.Ext(extension)) || child.Extension == strings.ToLower(extension) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

func supports(kinds []string, kind model.EntryKind) bool {
	for _, candidate := range kinds {
		if candidate == string(kind) {
			return true
		}
	}
	return false
}
