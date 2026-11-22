package tui

import (
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type connectionField struct {
	order           connectionSort
	target, title   string
	width, minWidth int
}

// The column order and widths drive both the Bubbles header and its hit regions.
var connectionFields = [...]connectionField{
	{connectionSortDestination, "conn.sort.destination", "connections.destination", 24, connectionTableMinWidth},
	{connectionSortProtocol, "conn.sort.protocol", "connections.protocol", 6, connectionTableMinWidth},
	{connectionSortProcess, "conn.sort.process", "connections.process", 12, 125},
	{connectionSortChain, "conn.sort.chain", "connections.chain", 15, connectionTableMinWidth},
	{connectionSortRule, "conn.sort.rule", "connections.rule", 16, 125},
	{connectionSortDownloadRate, "conn.sort.download_rate", "connections.download_speed", 11, connectionTableMinWidth},
	{connectionSortUploadRate, "conn.sort.upload_rate", "connections.upload_speed", 11, connectionTableMinWidth},
	{connectionSortDownloadTotal, "conn.sort.download_total", "connections.download_total", 10, 155},
	{connectionSortUploadTotal, "conn.sort.upload_total", "connections.upload_total", 10, 155},
	{connectionSortDuration, "conn.sort.duration", "connections.duration", 10, connectionTableMinWidth},
}

type connectionColumn struct {
	table.Column
	field connectionField
}

func (m *Model) connectionColumns(width int) (columns []connectionColumn, chainColumn, numericColumn int) {
	// The first cell is reserved for the row selection marker.
	columns = []connectionColumn{{Column: table.Column{Width: 1}}}
	used, ruleColumn := 2, -1
	for _, field := range connectionFields {
		if field.minWidth > width {
			continue
		}
		switch field.order {
		case connectionSortChain:
			chainColumn = len(columns)
		case connectionSortRule:
			ruleColumn = len(columns)
		case connectionSortDownloadRate:
			numericColumn = len(columns)
		}
		columns = append(columns, connectionColumn{table.Column{Title: m.tr(field.title), Width: field.width}, field})
		used += field.width + 1 // Bubbles adds one cell of right padding.
	}
	extra := max(0, width-used)
	destinationExtra := (2*extra + 2) / 3
	columns[1].Width += destinationExtra
	extra -= destinationExtra
	if ruleColumn >= 0 {
		columns[chainColumn].Width += (extra + 1) / 2
		columns[ruleColumn].Width += extra / 2
	} else {
		columns[chainColumn].Width += extra
	}
	return columns, chainColumn, numericColumn
}

func (m *Model) connectionSortAllowed(order connectionSort) bool {
	return !m.connections.closedView || order != connectionSortDownloadRate && order != connectionSortUploadRate
}

func (m *Model) connectionSortTarget(width int) string {
	for _, field := range connectionFields {
		if field.order == m.connections.sort && width >= field.minWidth {
			return field.target
		}
	}
	return ""
}

// Keep keyboard focus on an actionable control if a resize hides its column.
func (m *Model) resizeConnectionSortFocus() {
	if m.focus != "conn.sort" && !strings.HasPrefix(m.focus, "conn.sort.") {
		return
	}
	width := dashboardBodyWidth(m.dashboardOptions())
	if width < connectionTableMinWidth {
		m.focus = "conn.sort"
		return
	}
	for _, field := range connectionFields {
		if field.target == m.focus && width >= field.minWidth && m.connectionSortAllowed(field.order) {
			return
		}
	}
	m.focus = m.connectionSortTarget(width)
	if m.focus == "" {
		m.focus = "conn.sort.destination"
	}
}

func (m *Model) connectionSortArrow() string {
	if m.connections.sort.descending(m.connections.sortReversed) {
		return "▼"
	}
	return "▲"
}

func (m *Model) connectionSortLabel() string {
	key := m.connections.sort.key()
	if m.connections.sort == connectionSortNewest && m.connections.sortReversed {
		key = "connections.sort_oldest"
	}
	return m.tr("connections.sort", m.tr(key)) + " " + m.connectionSortArrow()
}

func (m *Model) connectionHeaders(columns []connectionColumn, numericColumn int) ([]table.Column, []hitRegion) {
	headers := make([]table.Column, len(columns))
	var hits []hitRegion
	x := 0
	for i, col := range columns {
		id := col.field.target
		label := col.Title
		enabled := id != "" && !m.busy && !m.quitting && m.connectionSortAllowed(col.field.order)
		focused := enabled && m.focus == id
		selected := id != "" && m.connections.sort == col.field.order
		if enabled {
			hits = append(hits, hitRegion{ID: id, X: x, W: col.Width + 1, H: 1})
		}
		if focused && m.buttonPlain() {
			label = "›" + label
		}
		if selected {
			if ansi.StringWidth(label)+2 <= col.Width {
				label += " "
			}
			label = cellCrop(label, col.Width-1) + m.connectionSortArrow()
		}
		if focused || enabled && m.hover == id {
			label = lipgloss.NewStyle().Foreground(lipgloss.Color(focus)).Underline(true).Render(label)
		} else if selected {
			label = themed(label, accent, panel, false)
		}
		if i >= numericColumn {
			label = connectionNumberCell(label, col.Width)
		}
		headers[i] = table.Column{Title: label, Width: col.Width}
		x += col.Width + 1
	}
	return headers, hits
}

func (m *Model) changeConnectionSort(id string) {
	if m.busy || m.quitting || m.connections.detail != nil || m.connections.confirming {
		return
	}
	width := dashboardBodyWidth(m.dashboardOptions())
	order := m.connections.sort
	switch id {
	case "conn.reverse-sort":
		m.connections.sortReversed = !m.connections.sortReversed
	case "conn.sort":
		// Compact cards have no headers. The next-field shortcut there can
		// also reach columns that only fit in a wider table.
		available := [connectionSortCount]bool{connectionSortNewest: true}
		for _, field := range connectionFields {
			available[field.order] = (width < connectionTableMinWidth || width >= field.minWidth) && m.connectionSortAllowed(field.order)
		}
		for {
			order = (order + 1) % connectionSortCount
			if available[order] {
				break
			}
		}
		m.connections.sort, m.connections.sortReversed = order, false
		if target := m.connectionSortTarget(width); target != "" && strings.HasPrefix(m.focus, "conn.sort.") {
			m.focus = target
		}
	default:
		for _, field := range connectionFields {
			if field.target == id && width >= field.minWidth && m.connectionSortAllowed(field.order) {
				m.connections.sortReversed = field.order == order && !m.connections.sortReversed
				m.connections.sort = field.order
				m.reconcileConnections()
				m.scroll = 0
				return
			}
		}
		return
	}
	m.reconcileConnections()
	// A click should show the start of the new order. When sorting from
	// a focused row, keep the same connection in view.
	if strings.HasPrefix(m.focus, "connection.") {
		m.revealConnection(0)
	} else {
		m.scroll = 0
	}
}
