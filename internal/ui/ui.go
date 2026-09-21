package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"jev-file-sort/internal/config"
	"jev-file-sort/internal/execute"
	"jev-file-sort/internal/plan"
)

type Model struct {
	root       string
	config     config.Config
	classifier plan.Classifier
	plan       *plan.Plan
	cursor     int
	width      int
	height     int
	loading    bool
	applying   bool
	confirm    bool
	err        error
	result     *execute.Result
	runs       []execute.Run
	screen     string
	action     string
	cancel     context.CancelFunc
}

type planMsg struct {
	value plan.Plan
	err   error
}
type applyMsg struct {
	result execute.Result
	err    error
}
type historyMsg struct {
	runs []execute.Run
	err  error
}

func New(root string, cfg config.Config, classifier plan.Classifier) Model {
	return Model{root: root, config: cfg, classifier: classifier, loading: true, screen: "plan"}
}

func (m Model) Init() tea.Cmd { return m.buildPlan() }

func (m Model) buildPlan() tea.Cmd {
	return func() tea.Msg {
		value, err := (plan.Builder{Config: m.config, Classifier: m.classifier}).Build(context.Background(), m.root)
		return planMsg{value: value, err: err}
	}
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case planMsg:
		m.loading, m.err = false, message.err
		if message.err == nil {
			m.plan = &message.value
		}
	case applyMsg:
		m.applying, m.err, m.cancel = false, message.err, nil
		if message.err == nil {
			m.result = &message.result
		}
	case historyMsg:
		m.loading, m.err, m.runs = false, message.err, message.runs
		if m.cursor >= len(m.runs) {
			m.cursor = max(0, len(m.runs)-1)
		}
	case tea.KeyPressMsg:
		key := message.String()
		if key == "ctrl+c" && m.applying && m.cancel != nil {
			m.cancel()
			return m, nil
		}
		if key == "ctrl+c" || (key == "q" && !m.applying) {
			return m, tea.Quit
		}
		if m.loading || m.applying || m.plan == nil {
			return m, nil
		}
		if m.screen == "history" {
			return m.updateHistory(key)
		}
		switch key {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.confirm = false
		case "down", "j":
			if m.cursor+1 < len(m.plan.Operations) {
				m.cursor++
			}
			m.confirm = false
		case "space":
			if len(m.plan.Operations) == 0 {
				return m, nil
			}
			op := &m.plan.Operations[m.cursor]
			if op.Status == "planned" {
				op.Status, op.Reason = "skipped", "manually skipped"
			} else if op.Reason == "manually skipped" {
				op.Status, op.Reason = "planned", ""
			}
			m.confirm = false
		case "c":
			m.cycleCategory()
			m.confirm = false
		case "a":
			if !m.confirm {
				m.confirm = true
				return m, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			m.applying, m.confirm, m.cancel = true, false, cancel
			value := *m.plan
			return m, func() tea.Msg {
				result, err := execute.Apply(ctx, value)
				return applyMsg{result: result, err: err}
			}
		case "h":
			m.screen, m.cursor, m.loading = "history", 0, true
			return m, m.loadHistory()
		}
	}
	return m, nil
}

func (m Model) loadHistory() tea.Cmd {
	return func() tea.Msg {
		store, err := execute.NewStore(m.config.History.Directory)
		if err != nil {
			return historyMsg{err: err}
		}
		runs, err := store.List()
		return historyMsg{runs: runs, err: err}
	}
}

func (m Model) updateHistory(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "h":
		m.screen, m.cursor, m.action = "plan", 0, ""
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.action = ""
	case "down", "j":
		if m.cursor+1 < len(m.runs) {
			m.cursor++
		}
		m.action = ""
	case "u", "r":
		if len(m.runs) == 0 {
			return m, nil
		}
		action := "undo"
		if key == "r" {
			action = "redo"
		}
		if m.action != action {
			m.action = action
			return m, nil
		}
		id := m.runs[m.cursor].ID
		ctx, cancel := context.WithCancel(context.Background())
		m.applying, m.action, m.cancel = true, "", cancel
		return m, func() tea.Msg {
			var result execute.Result
			var err error
			if action == "undo" {
				result, err = execute.Undo(ctx, id, m.config.History.Directory)
			} else {
				result, err = execute.Redo(ctx, id, m.config.History.Directory)
			}
			return applyMsg{result: result, err: err}
		}
	}
	return m, nil
}

func (m *Model) cycleCategory() {
	if len(m.plan.Operations) == 0 || len(m.config.Categories) == 0 {
		return
	}
	op := m.plan.Operations[m.cursor]
	if op.Status == "excluded" || op.Status == "error" {
		return
	}
	start := -1
	for i, category := range m.config.Categories {
		if category.ID == op.Decision.CategoryID {
			start = i
			break
		}
	}
	for offset := 1; offset <= len(m.config.Categories); offset++ {
		index := (start + offset) % len(m.config.Categories)
		category := m.config.Categories[index]
		if config.Enabled(category.Enabled) {
			_ = plan.SetCategory(m.plan, m.cursor, category.ID)
			return
		}
	}
}

func (m Model) View() tea.View {
	var content strings.Builder
	content.WriteString("jev-file-sort\n\n")
	switch {
	case m.loading:
		content.WriteString("Building plan…\n")
	case m.applying:
		content.WriteString("Applying plan…\n")
	case m.err != nil:
		fmt.Fprintf(&content, "Error: %v\n\nPress q to quit.\n", m.err)
	case m.result != nil:
		fmt.Fprintf(&content, "Run %s: %s\n", m.result.RunID, m.result.Status)
		for state, count := range m.result.Counts {
			fmt.Fprintf(&content, "%s: %d\n", state, count)
		}
		content.WriteString("\nPress q to quit.\n")
	case m.plan != nil:
		if m.screen == "history" {
			m.renderHistory(&content)
			break
		}
		fmt.Fprintf(&content, "Target: %s\nOutput: %s\n\n", m.plan.Root, m.plan.OutputRoot)
		start, end := visibleRange(m.cursor, len(m.plan.Operations), max(1, m.height-9))
		for index := start; index < end; index++ {
			op := m.plan.Operations[index]
			marker := " "
			if index == m.cursor {
				marker = ">"
			}
			destination := op.Destination
			if destination != "" {
				destination = filepath.Base(filepath.Dir(destination)) + "/" + filepath.Base(destination)
			}
			fmt.Fprintf(&content, "%s %-9s %-12s %-28s %s\n", marker, op.Status, op.Decision.Kind, truncate(op.RelativeSource, 28), destination)
		}
		content.WriteString("\n↑/↓ or j/k move · space skip · c category · a apply · h history · q quit\n")
		if m.confirm {
			content.WriteString("Press a again to apply the displayed plan.\n")
		}
	}
	view := tea.NewView(content.String())
	view.AltScreen = true
	view.WindowTitle = "jev-file-sort"
	return view
}

func (m Model) renderHistory(content *strings.Builder) {
	content.WriteString("History\n\n")
	if len(m.runs) == 0 {
		content.WriteString("No retained runs.\n")
	}
	start, end := visibleRange(m.cursor, len(m.runs), max(1, m.height-7))
	for index := start; index < end; index++ {
		marker := " "
		if index == m.cursor {
			marker = ">"
		}
		run := m.runs[index]
		fmt.Fprintf(content, "%s %-24s %-14s %s\n", marker, run.ID, run.Status, run.CreatedAt.Format("2006-01-02 15:04"))
	}
	content.WriteString("\n↑/↓ or j/k move · u undo · r redo · h/Esc back · q quit\n")
	if m.action != "" {
		fmt.Fprintf(content, "Press %s again to confirm %s.\n", string(m.action[0]), m.action)
	}
}

func visibleRange(cursor, length, limit int) (int, int) {
	start := cursor - limit/2
	if start < 0 {
		start = 0
	}
	end := min(length, start+limit)
	if end-start < limit {
		start = max(0, end-limit)
	}
	return start, end
}
func truncate(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:max(0, width-1)]) + "…"
}
