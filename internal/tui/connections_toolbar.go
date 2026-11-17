package tui

import (
	"fmt"
	"strconv"
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
	countWidth := m.connectionCountWidth()
	if len(tabs.lines) == 1 {
		offset = ansi.StringWidth(tabs.lines[0]) + 2
		fieldWidth = min(inputFieldMaxWidth, width-offset-countWidth-m.connectionSortWidth()-2)
		if fieldWidth >= 24 {
			return fieldWidth, offset
		}
	}
	if width-countWidth >= 12 {
		width -= countWidth
	}
	return min(inputFieldMaxWidth, max(1, width)), 0
}

func (m Model) connectionTotalCount() (int, bool) {
	if m.connections.closedView {
		return len(m.connectionClosedEntries()), true
	}
	if snapshot := m.connectionSnapshot(); snapshot != nil {
		return len(snapshot.Connections), true
	}
	return 0, false
}

func (m Model) connectionCount() string {
	if strings.TrimSpace(m.connections.search.Value()) == "" {
		return ""
	}
	total, available := m.connectionTotalCount()
	if !available {
		return ""
	}
	return fmt.Sprintf("%d / %d", len(m.filteredConnections()), total)
}

// Keep space for the filtered ratio even while it is hidden, so typing cannot
// resize the frame. Sorting reserves its longest label for the same reason.
func (m Model) connectionCountWidth() int {
	total, available := m.connectionTotalCount()
	if !available {
		return 0
	}
	return 2*len(strconv.Itoa(total)) + 5
}

func (m Model) connectionSortWidth() int {
	width := 0
	for order := connectionSortNewest; order < connectionSortCount; order++ {
		width = max(width, ansi.StringWidth(m.tr("connections.sort", m.tr(order.key())))+4)
	}
	return width
}

// The input owns only editing and Clear. View and sort controls sit outside
// its frame; narrow terminals move those controls together above the search.
func (m Model) connectionFilterBar(width int) dashboardBlock {
	tabs := m.connectionTabs(width)
	sort := m.connectionControls()
	fieldWidth, offset := m.connectionSearchGeometry(width)
	search := m.searchField("conn.search", m.tr("connections.search_placeholder"), m.connections.search, fieldWidth, false)
	count := m.connectionCount()
	if count != "" && offset+fieldWidth+2+ansi.StringWidth(count) <= width {
		search.lines[1] += themed("  "+count, muted, bg, false)
	}
	if offset == 0 {
		y := len(tabs.lines) - 1
		x := width - m.connectionSortWidth()
		if x < ansi.StringWidth(tabs.lines[y])+2 {
			y++
			tabs.lines = append(tabs.lines, "")
		}
		x = max(0, x)
		tabs.lines[y] = themed(cellPad(tabs.lines[y], x), text, bg, false) + sort.lines[0]
		for _, target := range sort.targets {
			target.X += x
			target.Y += y
			tabs.targets = append(tabs.targets, target)
		}
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
	x := offset + fieldWidth + m.connectionCountWidth() + 2
	b.lines[1] = themed(cellPad(b.lines[1], x), text, bg, false) + sort.lines[0]
	for _, target := range tabs.targets {
		target.Y++
		b.targets = append(b.targets, target)
	}
	for _, target := range search.targets {
		target.X += offset
		b.targets = append(b.targets, target)
	}
	for _, target := range sort.targets {
		target.X += x
		target.Y++
		b.targets = append(b.targets, target)
	}
	return b
}

func (m Model) connectionStatus() (string, string) {
	if m.connections.closedView {
		status := m.tr("connections.history_scope")
		if m.connections.paused {
			status = m.tr("connections.paused") + " · " + status
		}
		return status, muted
	}
	if !m.snapshotReady {
		if m.busy {
			return m.tr("empty.loading"), muted
		}
		return m.tr("empty.read_failed"), errorColor
	}
	if state, coreStatus := m.corePageState(); state != "" {
		return coreStatus, muted
	}
	if m.connections.paused && m.connectionSnapshot() != nil {
		return m.tr("connections.paused"), warning
	}
	if m.connectionSnapshot() == nil {
		status := runtimeStatus(m.runtime.snapshot, m.runtime.received, m.runtime.pending, m.snapshot.Installed, m.locale)
		return status, muted
	}
	return "", muted
}
