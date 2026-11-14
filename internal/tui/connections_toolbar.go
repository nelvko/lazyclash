package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m Model) connectionTabs(width int) dashboardBlock {
	active := "--"
	if snapshot := m.connectionSnapshot(); snapshot != nil {
		active = fmt.Sprint(len(snapshot.Connections))
	}
	labels := []string{m.tr("connections.tab_active", active), m.tr("connections.tab_closed", fmt.Sprint(len(m.connectionClosedEntries())))}
	minimum := max(ansi.StringWidth(m.tr("connections.tab_active", "9999")), ansi.StringWidth(m.tr("connections.tab_closed", "500")))
	var b dashboardBlock
	line := ""
	for i, id := range []string{"conn.active", "conn.closed"} {
		o := m.searchControlOptions(id)
		o.Selectable, o.Selected, o.Width = true, m.connections.closedView == (i == 1), minimum
		o.Disabled = m.busy || m.quitting
		value := renderButton(labels[i], o)
		if line != "" && ansi.StringWidth(line)+ansi.StringWidth(value) > width {
			b.lines = append(b.lines, cellCrop(line, width))
			line = ""
		}
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: id, X: ansi.StringWidth(line), Y: len(b.lines), W: ansi.StringWidth(value), H: 1})
		}
		line += value
	}
	b.lines = append(b.lines, cellCrop(line, width))
	return b
}

func (m Model) connectionSearchGeometry(width int) (fieldWidth, offset int) {
	tabs := m.connectionTabs(width)
	if len(tabs.lines) == 1 {
		offset = ansi.StringWidth(tabs.lines[0]) + 2
		fieldWidth = min(inputFieldMaxWidth, max(1, width-offset))
		if inputFieldInlineTrailing(fieldWidth, searchControlWidth(m.connectionControls()), true) {
			return fieldWidth, offset
		}
	}
	return min(inputFieldMaxWidth, max(1, width)), 0
}

// The tab group precedes the shared Bubbles input. When they cannot both fit,
// the complete tab group gets its own line above the input.
func (m Model) connectionFilterBar(width int) dashboardBlock {
	tabs := m.connectionTabs(width)
	fieldWidth, offset := m.connectionSearchGeometry(width)
	search := m.searchToolbar("conn.search", m.tr("connections.search_placeholder"), m.connections.search, m.connectionControls(), fieldWidth, false)
	if offset == 0 {
		tabs.append(search)
		return tabs
	}
	var b dashboardBlock
	for i, line := range search.lines {
		left := ""
		if i == 1 {
			left = tabs.lines[0]
		}
		b.lines = append(b.lines, themed(cellPad(left, offset), text, bg, false)+line)
	}
	for _, target := range tabs.targets {
		target.Y++
		b.targets = append(b.targets, target)
	}
	for _, target := range search.targets {
		target.X += offset
		b.targets = append(b.targets, target)
	}
	return b
}

func (m Model) connectionStatus() (string, string) {
	if m.connections.closedView {
		return m.tr("connections.closed_count", len(m.filteredConnections()), len(m.connectionClosedEntries())) + " · " + m.tr("connections.history_scope"), muted
	}
	status, color := runtimeStatus(m.runtime.snapshot, m.runtime.received, m.runtime.pending, m.snapshot.Installed, m.locale), muted
	if state, coreStatus := m.corePageState(); state != "" {
		return coreStatus, color
	}
	if m.connections.paused && m.connectionSnapshot() != nil {
		status, color = m.tr("connections.paused"), warning
	}
	if snapshot := m.connectionSnapshot(); snapshot != nil {
		status = m.tr("connections.count", len(m.filteredConnections()), len(snapshot.Connections)) + " · " + status
	}
	return strings.TrimSpace(status), color
}
