package ui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"jev-file-sort/internal/config"
	"jev-file-sort/internal/execute"
	"jev-file-sort/internal/model"
	"jev-file-sort/internal/plan"
)

type Model struct {
	root                    string
	config                  config.Config
	classifier              plan.Classifier
	classifierFactory       ClassifierFactory
	plan                    *plan.Plan
	result                  *execute.Result
	runs                    []execute.Run
	cursor, width, height   int
	loading, applying, dark bool
	screen, overlay, filter string
	notice                  string
	filtering               bool
	chooser                 int
	err                     error
	cancel                  context.CancelFunc
}

type ClassifierFactory func(config.Config) (plan.Classifier, error)

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

func New(root string, cfg config.Config, classifier plan.Classifier, classifierFactory ClassifierFactory) Model {
	return Model{root: root, config: cfg, classifier: classifier, classifierFactory: classifierFactory, loading: true, screen: "plan", dark: true}
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.buildPlan(), tea.RequestBackgroundColor) }

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
	case tea.BackgroundColorMsg:
		m.dark = message.IsDark()
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
		m.clampCursor()
	case tea.KeyPressMsg:
		return m.updateKey(message)
	}
	return m, nil
}

func (m Model) updateKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := message.String()
	if key == "ctrl+c" && m.applying && m.cancel != nil {
		m.cancel()
		return m, nil
	}
	if key == "ctrl+c" || (key == "q" && !m.applying && !m.filtering && m.overlay == "") {
		return m, tea.Quit
	}
	if m.applying {
		return m, nil
	}
	if m.filtering {
		return m.updateFilter(message)
	}
	if m.overlay != "" {
		return m.updateOverlay(key)
	}
	if key == "?" && !m.loading {
		m.overlay = "help"
		return m, nil
	}
	if m.loading || m.plan == nil {
		return m, nil
	}
	if m.screen == "history" {
		return m.updateHistory(key)
	}
	return m.updatePlan(key)
}

func (m Model) updatePlan(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor+1 < len(m.visibleOperations()) {
			m.cursor++
		}
	case "/":
		m.filtering = true
	case "esc":
		if m.filter != "" {
			m.filter, m.cursor = "", 0
		}
	case "space":
		index, ok := m.selectedOperation()
		if !ok {
			break
		}
		op := &m.plan.Operations[index]
		if op.Status == "planned" {
			op.Status, op.Reason = "skipped", "manually skipped"
		} else if op.Reason == "manually skipped" {
			op.Status, op.Reason = "planned", ""
		}
	case "c", "right":
		m.openCategoryChooser()
	case "m":
		m.openModeChooser()
	case "f":
		m.openFolderModeChooser()
	case "a":
		m.overlay = "apply"
	case "h":
		m.screen, m.cursor, m.loading = "history", 0, true
		return m, m.loadHistory()
	}
	return m, nil
}

func (m Model) updateFilter(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "enter":
		m.filtering = false
	case "esc":
		m.filtering, m.filter = false, ""
	case "backspace":
		runes := []rune(m.filter)
		if len(runes) > 0 {
			m.filter = string(runes[:len(runes)-1])
		}
	default:
		if message.Text != "" && !message.Mod.Contains(tea.ModCtrl) && !message.Mod.Contains(tea.ModAlt) {
			m.filter += message.Text
		}
	}
	m.cursor = 0
	return m, nil
}

func (m Model) updateOverlay(key string) (tea.Model, tea.Cmd) {
	if key == "esc" || key == "left" || key == "n" || key == "q" {
		m.overlay = ""
		return m, nil
	}
	if m.overlay == "help" {
		if key == "?" || key == "enter" {
			m.overlay = ""
		}
		return m, nil
	}
	if m.overlay == "notice" {
		if key == "enter" {
			m.overlay, m.notice = "", ""
		}
		return m, nil
	}
	if m.overlay == "category" {
		categories := m.enabledCategories()
		switch key {
		case "up", "k":
			if m.chooser > 0 {
				m.chooser--
			}
		case "down", "j":
			if m.chooser+1 < len(categories) {
				m.chooser++
			}
		case "enter":
			if index, ok := m.selectedOperation(); ok && len(categories) > 0 {
				if err := plan.SetCategory(m.plan, index, categories[m.chooser].ID); err != nil {
					m.notice = err.Error()
					m.overlay = "notice"
					return m, nil
				}
			}
			m.overlay = ""
		}
		return m, nil
	}
	if m.overlay == "mode" {
		switch key {
		case "up", "k":
			if m.chooser > 0 {
				m.chooser--
			}
		case "down", "j":
			if m.chooser < 1 {
				m.chooser++
			}
		case "enter":
			return m.selectMode()
		}
		return m, nil
	}
	if m.overlay == "folder-mode" {
		last := 1
		if m.config.Mode == "jev" {
			last = 2
		}
		switch key {
		case "up", "k":
			if m.chooser > 0 {
				m.chooser--
			}
		case "down", "j":
			if m.chooser < last {
				m.chooser++
			}
		case "enter":
			return m.selectFolderMode()
		}
		return m, nil
	}
	if key != "enter" && key != "y" {
		return m, nil
	}
	action := m.overlay
	m.overlay = ""
	ctx, cancel := context.WithCancel(context.Background())
	m.applying, m.cancel = true, cancel
	if action == "apply" {
		value := *m.plan
		return m, func() tea.Msg { result, err := execute.Apply(ctx, value); return applyMsg{result: result, err: err} }
	}
	if len(m.runs) == 0 {
		m.applying, m.cancel = false, nil
		return m, nil
	}
	id := m.runs[m.cursor].ID
	return m, func() tea.Msg {
		if action == "undo" {
			result, err := execute.Undo(ctx, id, m.config.History.Directory)
			return applyMsg{result: result, err: err}
		}
		result, err := execute.Redo(ctx, id, m.config.History.Directory)
		return applyMsg{result: result, err: err}
	}
}

func (m *Model) openModeChooser() {
	m.chooser = 0
	if m.config.Mode == "jev" {
		m.chooser = 1
	}
	m.overlay = "mode"
}

func (m *Model) openFolderModeChooser() {
	m.chooser = 0
	if config.Enabled(m.config.Folders.RulesEnabled) {
		m.chooser = 1
	}
	if m.config.Mode == "jev" && config.Enabled(m.config.Jev.FolderEvaluation.Enabled) {
		m.chooser = 2
	}
	m.overlay = "folder-mode"
}

func (m Model) selectFolderMode() (tea.Model, tea.Cmd) {
	rulesEnabled := m.chooser >= 1
	jevEnabled := config.Enabled(m.config.Jev.FolderEvaluation.Enabled)
	if m.config.Mode == "jev" {
		jevEnabled = m.chooser == 2
	}
	if config.Enabled(m.config.Folders.RulesEnabled) == rulesEnabled && config.Enabled(m.config.Jev.FolderEvaluation.Enabled) == jevEnabled {
		m.overlay = ""
		return m, nil
	}
	nextConfig := m.config
	nextConfig.Folders.RulesEnabled = config.Bool(rulesEnabled)
	nextConfig.Jev.FolderEvaluation.Enabled = config.Bool(jevEnabled)
	classifier, err := m.classifierFactory(nextConfig)
	if err != nil {
		m.notice = err.Error()
		m.overlay = "notice"
		return m, nil
	}
	m.config = nextConfig
	m.classifier = classifier
	m.plan, m.result, m.err = nil, nil, nil
	m.cursor, m.loading, m.overlay, m.filter = 0, true, "", ""
	return m, m.buildPlan()
}

func (m Model) selectMode() (tea.Model, tea.Cmd) {
	mode := "simple"
	if m.chooser == 1 {
		mode = "jev"
	}
	if mode == m.config.Mode {
		m.overlay = ""
		return m, nil
	}
	nextConfig := m.config
	nextConfig.Mode = mode
	classifier, err := m.classifierFactory(nextConfig)
	if err != nil {
		m.notice = err.Error()
		m.overlay = "notice"
		return m, nil
	}
	m.config = nextConfig
	m.classifier = classifier
	m.plan, m.result, m.err = nil, nil, nil
	m.cursor, m.loading, m.overlay, m.filter = 0, true, "", ""
	return m, m.buildPlan()
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
	case "esc", "left", "h":
		m.screen, m.cursor = "plan", 0
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor+1 < len(m.runs) {
			m.cursor++
		}
	case "u":
		if len(m.runs) > 0 {
			m.overlay = "undo"
		}
	case "r":
		if len(m.runs) > 0 {
			m.overlay = "redo"
		}
	}
	return m, nil
}

func (m *Model) openCategoryChooser() {
	index, ok := m.selectedOperation()
	if !ok {
		return
	}
	op := m.plan.Operations[index]
	if op.Kind != model.EntryFile && op.Kind != model.EntryFolder {
		return
	}
	categories := m.enabledCategories()
	if len(categories) == 0 {
		return
	}
	m.chooser = 0
	for index, category := range categories {
		if category.ID == op.Decision.CategoryID {
			m.chooser = index
			break
		}
	}
	m.overlay = "category"
}

func (m Model) enabledCategories() []config.Category {
	var categories []config.Category
	for _, category := range m.config.Categories {
		if config.Enabled(category.Enabled) {
			categories = append(categories, category)
		}
	}
	return categories
}

func (m Model) visibleOperations() []int {
	if m.plan == nil {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(m.filter))
	indices := make([]int, 0, len(m.plan.Operations))
	for index, operation := range m.plan.Operations {
		haystack := strings.ToLower(strings.Join([]string{operation.RelativeSource, operation.Destination, operation.Status, string(operation.Kind), string(operation.Decision.Kind), operation.Decision.CategoryID, operation.Decision.RuleID}, " "))
		if query == "" || strings.Contains(haystack, query) {
			indices = append(indices, index)
		}
	}
	return indices
}

func (m Model) selectedOperation() (int, bool) {
	indices := m.visibleOperations()
	if m.cursor < 0 || m.cursor >= len(indices) {
		return 0, false
	}
	return indices[m.cursor], true
}

func (m *Model) clampCursor() {
	length := len(m.runs)
	if m.screen == "plan" {
		length = len(m.visibleOperations())
	}
	if m.cursor >= length {
		m.cursor = max(0, length-1)
	}
}

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "jev-file-sort"
	return view
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
