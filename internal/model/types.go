package model

import "time"

type DecisionKind string

const (
	DecisionCategory      DecisionKind = "category"
	DecisionUncategorized DecisionKind = "uncategorized"
	DecisionDescend       DecisionKind = "descend"
	DecisionSkip          DecisionKind = "skip"
	DecisionError         DecisionKind = "error"
)

type EntryKind string

const (
	EntryFile    EntryKind = "file"
	EntryFolder  EntryKind = "folder"
	EntrySymlink EntryKind = "symlink"
)

type Decision struct {
	Kind       DecisionKind `json:"kind"`
	CategoryID string       `json:"category_id,omitempty"`
	RuleID     string       `json:"rule_id,omitempty"`
	Confidence *float64     `json:"confidence,omitempty"`
	Warnings   []string     `json:"warnings,omitempty"`
}

type Fingerprint struct {
	SHA256     string    `json:"sha256,omitempty"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	EntryCount int       `json:"entry_count,omitempty"`
}
