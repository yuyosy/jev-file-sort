package execute

import (
	"time"

	"jev-file-sort/internal/plan"
)

const historyVersion = 1

type Run struct {
	Version        int            `json:"version"`
	ID             string         `json:"id"`
	PlanID         string         `json:"plan_id"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	Status         string         `json:"status"`
	HistoryEnabled bool           `json:"history_enabled"`
	Plan           plan.Plan      `json:"plan"`
	Operations     []RunOperation `json:"operations"`
}

type RunOperation struct {
	PlanOperation     plan.Operation `json:"plan_operation"`
	ActualDestination string         `json:"actual_destination,omitempty"`
	State             string         `json:"state"`
	Error             string         `json:"error,omitempty"`
}

type Result struct {
	RunID  string         `json:"run_id"`
	Status string         `json:"status"`
	Counts map[string]int `json:"counts"`
}
