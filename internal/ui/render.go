package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type styles struct {
	title, subtle, accent, success, warning, danger lipgloss.Style
	selected, panel, dialog, key                    lipgloss.Style
}

func newStyles(dark bool) styles {
	muted, border, surface := "#8B93A7", "#3B4261", "#202436"
	if !dark {
		muted, border, surface = "#687080", "#CBD0DA", "#EEF1F6"
	}
	return styles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C6FF7")),
		subtle:   lipgloss.NewStyle().Foreground(lipgloss.Color(muted)),
		accent:   lipgloss.NewStyle().Foreground(lipgloss.Color("#67D4E8")),
		success:  lipgloss.NewStyle().Foreground(lipgloss.Color("#62D49B")),
		warning:  lipgloss.NewStyle().Foreground(lipgloss.Color("#E8B866")),
		danger:   lipgloss.NewStyle().Foreground(lipgloss.Color("#EE6D85")),
		selected: lipgloss.NewStyle().Bold(true).Background(lipgloss.Color(surface)),
		panel:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(border)).Padding(0, 1),
		dialog:   lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#7C6FF7")).Padding(1, 2),
		key:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#67D4E8")),
	}
}

func (m Model) render() string {
	s := newStyles(m.dark)
	width, height := max(50, m.width), max(12, m.height)
	header, footer := m.renderHeader(s, width), m.renderFooter(s, width)
	bodyHeight := max(4, height-lipgloss.Height(header)-lipgloss.Height(footer))
	var body string
	switch {
	case m.loading:
		body = s.panel.Width(width - 2).Height(bodyHeight - 2).Render("Building plan…")
	case m.applying:
		body = s.panel.Width(width - 2).Height(bodyHeight - 2).Render("Applying changes…\n\n" + s.subtle.Render("Ctrl+C requests cancellation at the next safe boundary."))
	case m.err != nil:
		body = s.panel.Width(width - 2).Height(bodyHeight - 2).Render(s.danger.Render("Error") + "\n\n" + m.err.Error())
	case m.result != nil:
		body = m.renderResult(s, width, bodyHeight)
	case m.screen == "history":
		body = m.renderHistory(s, width, bodyHeight)
	default:
		body = m.renderPlan(s, width, bodyHeight)
	}
	page := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.overlay != "" {
		return m.renderOverlay(s, page, width, height)
	}
	return page
}

func (m Model) renderHeader(s styles, width int) string {
	name := s.title.Render("JEV FILE SORT")
	section := "Plan"
	if m.screen == "history" {
		section = "History"
	}
	right := s.subtle.Render(section)
	line := name + strings.Repeat(" ", max(1, width-lipgloss.Width(name)-lipgloss.Width(right))) + right
	if m.plan == nil {
		return line + "\n"
	}
	meta := fmt.Sprintf("%s  →  %s  ·  %s", m.plan.Root, m.plan.OutputRoot, m.plan.Mode)
	return line + "\n" + s.subtle.Render(truncate(meta, max(1, width-1)))
}

func (m Model) renderPlan(s styles, width, height int) string {
	listWidth, detailWidth := width-2, 0
	if width >= 96 {
		listWidth, detailWidth = width*3/5-1, width-(width*3/5-1)-1
	}
	list := m.renderOperationList(s, listWidth, height)
	if detailWidth == 0 {
		return list
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, list, m.renderOperationDetail(s, detailWidth, height))
}

func (m Model) renderOperationList(s styles, width, height int) string {
	indices := m.visibleOperations()
	start, end := visibleRange(m.cursor, len(indices), max(1, height-3))
	lines := []string{s.subtle.Render(fmt.Sprintf(" OPERATIONS  %d/%d", len(indices), len(m.plan.Operations)))}
	for position := start; position < end; position++ {
		op := m.plan.Operations[indices[position]]
		marker := "  "
		if position == m.cursor {
			marker = "› "
		}
		category := op.Decision.CategoryID
		if category == "" {
			category = string(op.Decision.Kind)
		}
		available := max(8, width-24)
		line := fmt.Sprintf("%s%s %-*s %s", marker, statusGlyph(op.Status), available, truncate(op.RelativeSource, available), truncate(category, 14))
		if position == m.cursor {
			line = s.selected.Width(max(1, width-4)).Render(line)
		} else if op.Status == "skipped" || op.Status == "excluded" {
			line = s.subtle.Render(line)
		}
		lines = append(lines, line)
	}
	if len(indices) == 0 {
		lines = append(lines, "", s.subtle.Render("  No operations match the current filter."))
	}
	return s.panel.Width(max(1, width-2)).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func (m Model) renderOperationDetail(s styles, width, height int) string {
	index, ok := m.selectedOperation()
	if !ok {
		return s.panel.Width(max(1, width-2)).Height(max(1, height-2)).Render(s.subtle.Render("No selection"))
	}
	op := m.plan.Operations[index]
	lines := []string{s.subtle.Render("DETAIL"), "", s.title.Render(filepath.Base(op.Source)), labelValue(s, "Kind", string(op.Kind)), labelValue(s, "Status", op.Status), labelValue(s, "Category", emptyValue(op.Decision.CategoryID)), labelValue(s, "Rule", emptyValue(op.Decision.RuleID))}
	if op.Decision.Confidence != nil {
		lines = append(lines, labelValue(s, "Confidence", fmt.Sprintf("%.0f%%", *op.Decision.Confidence*100)))
	}
	lines = append(lines, "", s.subtle.Render("Destination"), truncate(op.Destination, max(8, width-5)))
	if op.Reason != "" {
		lines = append(lines, "", s.subtle.Render("Reason"), truncate(op.Reason, max(8, width-5)))
	}
	if op.ContentSent || op.FolderSummarySent {
		sent := "content"
		if op.FolderSummarySent {
			sent = "folder summary"
		}
		lines = append(lines, "", s.warning.Render("Jev received "+sent))
	}
	if len(op.Decision.Warnings) > 0 {
		lines = append(lines, "", s.warning.Render(strings.Join(op.Decision.Warnings, "\n")))
	}
	return s.panel.Width(max(1, width-2)).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
}

func (m Model) renderHistory(s styles, width, height int) string {
	listWidth, detailWidth := width-2, 0
	if width >= 96 {
		listWidth, detailWidth = width*3/5-1, width-(width*3/5-1)-1
	}
	start, end := visibleRange(m.cursor, len(m.runs), max(1, height-3))
	lines := []string{s.subtle.Render(fmt.Sprintf(" RUNS  %d", len(m.runs)))}
	for index := start; index < end; index++ {
		run := m.runs[index]
		line := fmt.Sprintf("  %s  %-13s  %s", run.CreatedAt.Format("2006-01-02 15:04"), run.Status, run.ID)
		if index == m.cursor {
			line = s.selected.Width(max(1, listWidth-4)).Render("›" + line[1:])
		}
		lines = append(lines, line)
	}
	if len(m.runs) == 0 {
		lines = append(lines, "", s.subtle.Render("  No retained runs."))
	}
	list := s.panel.Width(max(1, listWidth-2)).Height(max(1, height-2)).Render(strings.Join(lines, "\n"))
	if detailWidth == 0 || len(m.runs) == 0 {
		return list
	}
	run := m.runs[m.cursor]
	detail := strings.Join([]string{s.subtle.Render("RUN DETAIL"), "", labelValue(s, "ID", run.ID), labelValue(s, "Status", run.Status), labelValue(s, "Created", run.CreatedAt.Format("2006-01-02 15:04:05")), labelValue(s, "Operations", fmt.Sprint(len(run.Operations))), labelValue(s, "History", fmt.Sprint(run.HistoryEnabled))}, "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, list, s.panel.Width(max(1, detailWidth-2)).Height(max(1, height-2)).Render(detail))
}

func (m Model) renderResult(s styles, width, height int) string {
	lines := []string{s.success.Bold(true).Render("Operation complete"), "", labelValue(s, "Run", m.result.RunID), labelValue(s, "Status", m.result.Status), ""}
	keys := make([]string, 0, len(m.result.Counts))
	for key := range m.result.Counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%-16s %d", key, m.result.Counts[key]))
	}
	return s.panel.Width(width - 2).Height(height - 2).Render(strings.Join(lines, "\n"))
}

func (m Model) renderFooter(s styles, width int) string {
	if m.filtering {
		return s.accent.Render("/ ") + m.filter + "█\n" + s.subtle.Render("Enter apply filter · Esc clear")
	}
	if m.applying {
		return s.subtle.Render("Ctrl+C cancel")
	}
	if m.result != nil || m.err != nil {
		return keyHelp(s, "q", "quit")
	}
	var help string
	if m.screen == "history" {
		help = keyHelp(s, "↑↓", "move", "u", "undo", "r", "redo", "h", "back", "?", "help", "q", "quit")
	} else {
		help = keyHelp(s, "↑↓", "move", "space", "skip", "c", "category", "m", "mode", "/", "filter", "a", "apply", "h", "history", "?", "help", "q", "quit")
	}
	if m.filter != "" {
		help = s.accent.Render("filter: "+m.filter) + "  " + help
	}
	return ansi.Truncate(help, width, "…")
}

func (m Model) renderOverlay(s styles, page string, width, height int) string {
	var content string
	switch m.overlay {
	case "help":
		content = s.title.Render("Keyboard help") + "\n\n" + strings.Join([]string{keyHelp(s, "↑/↓ j/k", "move selection"), keyHelp(s, "space", "skip or restore operation"), keyHelp(s, "c", "choose category"), keyHelp(s, "m", "switch Simple / Jev mode"), keyHelp(s, "/", "filter operations"), keyHelp(s, "a", "apply plan"), keyHelp(s, "h", "toggle history"), keyHelp(s, "Esc", "close or clear"), keyHelp(s, "q", "quit")}, "\n") + "\n\n" + s.subtle.Render("Press ? or Enter to close")
	case "category":
		lines := []string{s.title.Render("Choose category"), ""}
		for index, category := range m.enabledCategories() {
			line := "  " + category.Name + "  " + s.subtle.Render(category.ID)
			if index == m.chooser {
				line = s.selected.Width(36).Render("›" + line[1:])
			}
			lines = append(lines, line)
		}
		content = strings.Join(append(lines, "", s.subtle.Render("Enter select · Esc cancel")), "\n")
	case "mode":
		modes := []struct {
			name, description string
		}{
			{"Simple", "Classify with local rules"},
			{"Jev", "Classify with local rules and Jev"},
		}
		lines := []string{s.title.Render("Choose mode"), ""}
		for index, mode := range modes {
			line := fmt.Sprintf("  %-8s %s", mode.name, s.subtle.Render(mode.description))
			if index == m.chooser {
				line = s.selected.Width(46).Render("›" + line[1:])
			}
			lines = append(lines, line)
		}
		content = strings.Join(append(lines, "", s.subtle.Render("Enter rebuild plan · Esc cancel")), "\n")
	case "notice":
		content = s.danger.Bold(true).Render("Mode not changed") + "\n\n" + m.notice + "\n\n" + s.subtle.Render("Press Enter or Esc to close")
	default:
		action, id := m.overlay, "the current plan"
		if action != "apply" && len(m.runs) > 0 {
			id = m.runs[m.cursor].ID
		}
		content = s.title.Render(strings.ToUpper(action)) + "\n\n" + fmt.Sprintf("%s %s?", upperFirst(action), id) + "\n\n" + keyHelp(s, "Enter/y", "confirm", "Esc/n", "cancel")
	}
	dialog := s.dialog.Render(content)
	canvas := lipgloss.NewCanvas(width, height)
	background := lipgloss.NewLayer(page)
	foreground := lipgloss.NewLayer(dialog).
		X(max(0, (width-lipgloss.Width(dialog))/2)).
		Y(max(0, (height-lipgloss.Height(dialog))/2)).
		Z(1)
	canvas.Compose(lipgloss.NewCompositor(background, foreground))
	return canvas.Render()
}

func keyHelp(s styles, pairs ...string) string {
	parts := make([]string, 0, len(pairs)/2)
	for index := 0; index+1 < len(pairs); index += 2 {
		parts = append(parts, s.key.Render(pairs[index])+" "+s.subtle.Render(pairs[index+1]))
	}
	return strings.Join(parts, "  ")
}

func labelValue(s styles, label, value string) string {
	return s.subtle.Width(12).Render(label) + value
}
func emptyValue(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
func upperFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
func statusGlyph(status string) string {
	switch status {
	case "planned":
		return "●"
	case "skipped", "excluded":
		return "○"
	case "error", "failed":
		return "×"
	default:
		return "·"
	}
}
