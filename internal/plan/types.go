package plan

import (
	"time"

	"jev-file-sort/internal/config"
	"jev-file-sort/internal/model"
)

const FormatVersion = 1

type Plan struct {
	Version     int                 `json:"version"`
	ID          string              `json:"id"`
	CreatedAt   time.Time           `json:"created_at"`
	Root        string              `json:"root"`
	OutputRoot  string              `json:"output_root"`
	Mode        string              `json:"mode"`
	Model       string              `json:"model,omitempty"`
	Config      config.Config       `json:"config"`
	Operations  []Operation         `json:"operations"`
	Diagnostics []config.Diagnostic `json:"diagnostics,omitempty"`
}

type Operation struct {
	ID                string            `json:"id"`
	Source            string            `json:"source"`
	RelativeSource    string            `json:"relative_source"`
	Destination       string            `json:"destination,omitempty"`
	Kind              model.EntryKind   `json:"kind"`
	Decision          model.Decision    `json:"decision"`
	Status            string            `json:"status"`
	Reason            string            `json:"reason,omitempty"`
	Fingerprint       model.Fingerprint `json:"fingerprint"`
	ContentSent       bool              `json:"content_sent"`
	FolderSummarySent bool              `json:"folder_summary_sent"`
	Manual            bool              `json:"manual"`
}

type Summary struct {
	PlanID     string         `json:"plan_id"`
	OutputPath string         `json:"output_path"`
	Counts     map[string]int `json:"counts"`
}
