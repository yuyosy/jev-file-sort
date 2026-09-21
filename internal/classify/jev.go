package classify

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"jev-file-sort/internal/config"
	"jev-file-sort/internal/jev"
	"jev-file-sort/internal/model"
	matchpattern "jev-file-sort/internal/pattern"
	"jev-file-sort/internal/plan"
)

type Jev struct {
	Config config.Config
	Client jev.Client
}

func (j Jev) Classify(ctx context.Context, entry plan.Entry) (plan.Classification, error) {
	if entry.Kind == model.EntryFolder {
		explicit, err := (Simple{Config: j.Config}).Classify(ctx, entry)
		if err != nil || explicit.Decision.Kind == model.DecisionCategory {
			return explicit, err
		}
		if !config.Enabled(j.Config.Jev.FolderEvaluation.Enabled) {
			return plan.Classification{Decision: model.Decision{Kind: model.DecisionDescend}}, nil
		}
	}
	criteria, tokens := j.criteria(entry.Kind == model.EntryFolder)
	state, contentSent, summarySent, err := j.state(entry)
	if err != nil {
		return plan.Classification{}, err
	}
	instructions := "Choose the single category that best fits this file. Choose uncategorized when none fit."
	if entry.Kind == model.EntryFolder {
		instructions = "Choose a category only when the folder is cohesive and should move as one unit. Otherwise choose descend so its contents are classified individually."
	}
	result, err := j.Client.Choose(ctx, state, instructions, criteria)
	if err != nil {
		if entry.Kind == model.EntryFolder {
			return plan.Classification{Decision: model.Decision{Kind: model.DecisionDescend, Warnings: []string{err.Error()}}, FolderSummarySent: summarySent}, nil
		}
		return plan.Classification{}, err
	}
	confidence := result.Confidence
	decision := model.Decision{Confidence: &confidence}
	classification := plan.Classification{ContentSent: contentSent, FolderSummarySent: summarySent}
	if result.Choice == "system_descend" {
		decision.Kind = model.DecisionDescend
		classification.Decision = decision
		return classification, nil
	}
	if result.Choice == "system_uncategorized" {
		decision.Kind = model.DecisionUncategorized
		classification.Decision = decision
		return classification, nil
	}
	categoryID, exists := tokens[result.Choice]
	if !exists {
		return plan.Classification{}, fmt.Errorf("unknown internal choice token %q", result.Choice)
	}
	category, _ := j.Config.Category(categoryID)
	threshold := j.Config.Jev.Threshold
	if category.Threshold != nil {
		threshold = *category.Threshold
	}
	if confidence < threshold {
		if entry.Kind == model.EntryFolder {
			decision.Kind = model.DecisionDescend
		} else {
			decision.Kind = model.DecisionUncategorized
		}
		decision.Warnings = append(decision.Warnings, fmt.Sprintf("confidence %.3f is below threshold %.3f", confidence, threshold))
		classification.Decision = decision
		return classification, nil
	}
	decision.Kind, decision.CategoryID = model.DecisionCategory, categoryID
	classification.Decision = decision
	return classification, nil
}

func (j Jev) criteria(folder bool) (map[string]any, map[string]string) {
	criteria, tokens := map[string]any{}, map[string]string{}
	index := 0
	for _, category := range j.Config.Categories {
		if !config.Enabled(category.Enabled) {
			continue
		}
		token := fmt.Sprintf("c_%d", index)
		index++
		criteria[token] = category.Description
		tokens[token] = category.ID
	}
	if folder {
		criteria["system_descend"] = "The folder is mixed, ambiguous, or should be inspected item by item"
	} else {
		criteria["system_uncategorized"] = "None of the available categories fit"
	}
	return criteria, tokens
}

func (j Jev) state(entry plan.Entry) (any, bool, bool, error) {
	state := map[string]any{"name": entry.Name, "relative_path": entry.Relative, "depth": entry.Depth, "kind": entry.Kind}
	if entry.Kind == model.EntryFolder {
		limit := j.Config.Jev.FolderEvaluation.MaxEntries
		children := entry.Children
		truncated := len(children) > limit
		if truncated {
			children = children[:limit]
		}
		counts := map[string]int{"files": 0, "folders": 0, "symlinks": 0}
		extensions := map[string]int{}
		for _, child := range entry.Children {
			switch child.Kind {
			case model.EntryFile:
				counts["files"]++
				extensions[child.Extension]++
			case model.EntryFolder:
				counts["folders"]++
			default:
				counts["symlinks"]++
			}
		}
		state["direct_children"], state["child_counts"], state["extension_counts"], state["children_truncated"] = children, counts, extensions, truncated
		return state, false, true, nil
	}
	state["extension"], state["size"] = strings.ToLower(filepath.Ext(entry.Name)), entry.Info.Size()
	allowed, err := j.contentAllowed(entry)
	if err != nil || !allowed {
		return state, false, false, err
	}
	file, err := os.Open(entry.Path)
	if err != nil {
		return nil, false, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, j.Config.Content.MaxBytes+1))
	if err != nil {
		return nil, false, false, err
	}
	truncated := int64(len(data)) > j.Config.Content.MaxBytes
	if truncated {
		data = data[:j.Config.Content.MaxBytes]
	}
	state["content"], state["content_truncated"] = string(data), truncated
	return state, true, false, nil
}

func (j Jev) contentAllowed(entry plan.Entry) (bool, error) {
	if !config.Enabled(j.Config.Content.Enabled) || !j.Config.Content.Authorized || len(j.Config.Content.AllowPatterns) == 0 || j.Config.Content.MaxBytes <= 0 {
		return false, nil
	}
	textExtensions := []string{".txt", ".md", ".rst", ".csv", ".tsv", ".json", ".yaml", ".yml", ".xml", ".toml", ".ini", ".log"}
	extension := strings.ToLower(filepath.Ext(entry.Name))
	supported := false
	for _, candidate := range textExtensions {
		if extension == candidate {
			supported = true
			break
		}
	}
	if !supported {
		return false, nil
	}
	for _, candidate := range j.Config.Content.AllowPatterns {
		matched, err := matchpattern.Match(candidate, entry.Relative, entry.Name)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}
