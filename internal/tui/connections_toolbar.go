package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) connectionTabs(width, height int) dashboardBlock {
	active := "--"
	if snapshot := m.connectionSnapshot(); snapshot != nil {
		active = fmt.Sprint(len(snapshot.Connections))
	}
	labels := []string{m.tr("connections.tab_active", active), m.tr("connections.tab_closed", fmt.Sprint(len(m.connectionClosedEntries())))}
	minimum := max(ansi.StringWidth(labels[0]), ansi.StringWidth(labels[1]),
		ansi.StringWidth(m.tr("connections.tab_active", "9999")), ansi.StringWidth(m.tr("connections.tab_closed", "500")))
	var b dashboardBlock
	lines := make([]string, height)
	for i, id := range []string{"conn.active", "conn.closed"} {
		o := m.searchControlOptions(id)
		o.Selectable, o.Selected, o.Width = true, m.connections.closedView == (i == 1), minimum
		o.Compact, o.Height = true, height
		o.Disabled = m.busy || m.quitting
		buttonRows := strings.Split(renderButton(labels[i], o), "\n")
		x, buttonWidth := ansi.StringWidth(lines[0]), ansi.StringWidth(buttonRows[0])
		if x > 0 && x+buttonWidth > width {
			b.lines = append(b.lines, lines...)
			clear(lines)
			x = 0
		}
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: len(b.lines), W: buttonWidth, H: height})
		}
		for row, text := range buttonRows {
			lines[row] += text
		}
	}
	b.lines = append(b.lines, lines...)
	for row, line := range b.lines {
		b.lines[row] = cellCrop(line, width)
	}
	return b
}

func (m Model) connectionSearchToolbar(width int) listSearchToolbar {
	return listSearchToolbar{scope: m.connectionTabs(width, 1), functions: m.connectionControls(),
		countWidth: m.connectionCountWidth(), matchingWidth: searchControlWidth(m.connectionFilterButtons()),
		countAfterFunctions: true}
}

func (m Model) connectionSearchGeometry(width int) (fieldWidth, offset int) {
	return m.connectionSearchToolbar(width).searchGeometry(width)
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
	return 2*len(strconv.Itoa(total)) + 3
}

func (m Model) connectionSortLabelWidth() int {
	width := 0
	for order := connectionSortNewest; order < connectionSortCount; order++ {
		width = max(width, ansi.StringWidth(m.tr("connections.sort", m.tr(order.key()))))
	}
	return width
}

// Matching controls belong to the input; scope and sort remain outside its
// frame and move together above the search on narrow terminals.
func (m Model) connectionFilterBar(width int) dashboardBlock {
	bar := m.connectionSearchToolbar(width)
	fieldWidth, offset := bar.searchGeometry(width)
	_, err := m.connectionMatcher()
	search := m.searchToolbar("conn.search", m.tr("connections.search_placeholder"), m.connections.search,
		m.connectionFilterButtons(), fieldWidth, err != nil)
	if offset > 0 && !m.noColor && (m.colorProfile == colorprofile.Unknown || m.colorProfile >= colorprofile.ANSI) {
		bar.inlineScope = m.connectionTabs(width, len(search.lines))
	}
	count := m.connectionCount()
	return bar.render(search, width, count, count)
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
