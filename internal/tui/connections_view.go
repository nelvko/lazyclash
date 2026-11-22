package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const connectionTableMinWidth = 85

func (m *Model) connectionNow() time.Time {
	if m.connections.paused {
		return m.connections.frozenAt
	}
	return m.connectionObservedAt()
}

func (m *Model) connectionObservedAt() time.Time {
	if observed := m.runtime.observed[app.RuntimeConnections]; !observed.IsZero() {
		return observed
	}
	if !m.runtime.snapshot.ObservedAt.IsZero() {
		return m.runtime.snapshot.ObservedAt
	}
	return time.Now()
}

func connectionDuration(c mihomo.Connection, now time.Time) string {
	if c.Start.IsZero() || now.Before(c.Start) {
		return "--"
	}
	d := now.Sub(c.Start)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func connectionSpeed(n int64) string {
	if n < 0 {
		return "--"
	}
	return formatBytes(n) + "/s"
}

func connectionRule(c mihomo.Connection) string {
	if c.RulePayload == "" {
		return connectionValue(c.Rule)
	}
	return clean(c.Rule + "(" + c.RulePayload + ")")
}

func connectionChain(c mihomo.Connection) string {
	// Mihomo returns the outbound first; display the routing path from the
	// policy group to the final outbound, as in desktop connection inspectors.
	chain := make([]string, len(c.Chains))
	for i, name := range c.Chains {
		chain[len(chain)-1-i] = clean(name)
	}
	return connectionValue(strings.Join(chain, " → "))
}

func connectionChainCell(c mihomo.Connection, width int) string {
	chain := connectionChain(c)
	if ansi.StringWidth(chain) <= width || len(c.Chains) == 0 {
		return cellCrop(chain, width)
	}
	// The outbound is the meaningful end of a routed chain. Drop intermediate
	// groups before shortening its name; details retain the complete path.
	outbound := connectionValue(c.Chains[0])
	if ansi.StringWidth(outbound)+4 <= width {
		return "… → " + outbound
	}
	return cellCrop(outbound, width)
}

func connectionValue(s string) string {
	if strings.TrimSpace(s) == "" {
		return "--"
	}
	return clean(s)
}

func (m *Model) connectionControls() dashboardBlock {
	o := m.searchControlOptions("conn.sort")
	o.Kind, o.Disabled = buttonText, m.busy || m.quitting
	o.Compact, o.AlignLeft = true, true
	o.Width = m.connectionSortLabelWidth()
	value := renderButton(m.connectionSortLabel(), o)
	b := dashboardBlock{lines: []string{value}}
	if !o.Disabled {
		b.targets = []hitRegion{{ID: "conn.sort", W: ansi.StringWidth(value), H: 1}}
	}
	return b
}

func (m *Model) connectionToolbarRows(width int) int {
	_, available := m.connectionTotalCount()
	rows := m.connectionSearchToolbar(width).rows(width, available && strings.TrimSpace(m.connections.search.Value()) != "") + 1
	if status, _ := m.connectionStatus(); status != "" {
		rows++
	}
	return rows
}

func (m *Model) connectionSearchWidth(width int) int {
	fieldWidth, _ := m.connectionSearchGeometry(width)
	return inputFieldTextWidth(fieldWidth, searchControlWidth(m.connectionFilterButtons()), true)
}

func (m *Model) connectionsToolbar(width, rows int) dashboardBlock {
	b := m.connectionFilterBar(width)
	status, color := m.connectionStatus()
	if status != "" {
		b.lines = append(b.lines, themed(cellCrop(status, width), color, bg, false))
	}
	if width >= connectionTableMinWidth {
		b.append(m.connectionTable(nil, width))
	} else {
		b.lines = append(b.lines, themed(strings.Repeat("─", width), divider, bg, false))
	}
	b.lines = b.lines[:min(rows, len(b.lines))]
	var visible []hitRegion
	for _, hit := range b.targets {
		if clipped, ok := clipRegion(hit, 0, 0, width, len(b.lines)); ok {
			visible = append(visible, clipped)
		}
	}
	b.targets = visible
	return b
}

func connectionNumberCell(value string, width int) string {
	value = cellCrop(value, width)
	return strings.Repeat(" ", max(0, width-ansi.StringWidth(value))) + value
}

// Bubbles table handles cell sizing, ANSI-aware truncation and selection. The
// dashboard retains the only scrolling viewport and maps rows to opaque IDs.
func (m *Model) connectionTable(list []mihomo.Connection, width int) dashboardBlock {
	columns, chainColumn, numericColumn := m.connectionColumns(width)
	var rows []table.Row
	for _, c := range list {
		marker := " "
		if m.connectionRowKey(c) == m.connections.selected {
			marker = "▌"
		}
		if m.focus == "connection."+m.connectionRowKey(c) {
			marker = "›"
		}
		row := table.Row{marker, connectionDestination(c), strings.ToUpper(clean(c.Metadata.Network))}
		if width >= 125 {
			row = append(row, connectionValue(c.Metadata.Process))
		}
		row = append(row, connectionChainCell(c, columns[chainColumn].Width))
		if width >= 125 {
			row = append(row, connectionRule(c))
		}
		rate := m.connectionRowRate(c)
		row = append(row, connectionSpeed(rate.Download), connectionSpeed(rate.Upload))
		if width >= 155 {
			row = append(row, formatBytes(c.Download), formatBytes(c.Upload))
		}
		row = append(row, connectionDuration(c, m.connectionRowTime(c)))
		for index := numericColumn; index < len(row); index++ {
			row[index] = connectionNumberCell(row[index], columns[index].Width)
		}
		rows = append(rows, row)
	}
	headers, hits := m.connectionHeaders(columns, numericColumn)
	t := table.New(table.WithColumns(headers), table.WithRows(rows), table.WithWidth(width), table.WithHeight(max(2, len(rows)+1)), table.WithStyles(table.Styles{
		Header: lipgloss.NewStyle().PaddingRight(1), Cell: lipgloss.NewStyle().PaddingRight(1), Selected: lipgloss.NewStyle(),
	}))
	lines := strings.Split(t.View(), "\n")[:len(rows)+1]
	lines[0] = themed(lines[0], muted, panel, false)
	for i, c := range list {
		foreground, surface := text, bg
		if m.connectionRowKey(c) == m.connections.selected {
			surface = selectionBackground
		}
		if m.hover == "connection."+m.connectionRowKey(c) {
			surface = hoverBackground
		}
		if m.focus == "connection."+m.connectionRowKey(c) {
			foreground = focus
		}
		lines[i+1] = themed(cellPad(lines[i+1], width), foreground, surface, false)
	}
	return dashboardBlock{lines: lines, targets: hits}
}

func (m *Model) compactConnectionRows(c mihomo.Connection, width int) []string {
	id, surface, marker := "connection."+m.connectionRowKey(c), bg, "  "
	if m.connectionRowKey(c) == m.connections.selected {
		surface, marker = panel, "▌ "
	}
	if m.hover == id {
		surface = hoverBackground
	}
	foreground := text
	if m.focus == id {
		marker, foreground = "› ", focus
	}
	rate := m.connectionRowRate(c)
	first := marker + connectionDestination(c) + " · " + strings.ToUpper(clean(c.Metadata.Network))
	second := "  ↓ " + connectionSpeed(rate.Download) + " ↑ " + connectionSpeed(rate.Upload) + " · " + connectionValue(c.Metadata.Process) + " · " + connectionChain(c) + " · " + connectionRule(c) + " · " + connectionDuration(c, m.connectionRowTime(c))
	return []string{themed(cellPad(first, width), foreground, surface, false), themed(cellPad(second, width), muted, surface, false)}
}

func (m *Model) connectionsBody(width int) (string, []hitRegion) {
	var lines []string
	var hits []hitRegion
	addText := func(s, color string) {
		lines = append(lines, strings.Split(wrapText(themed(clean(s), color, bg, false), max(1, width)), "\n")...)
	}
	addButton := func(id, label string) {
		o := dashboardButtonOptions(id, m.navigationOptions(), false, bg)
		o.Disabled = m.busy
		if id == "conn.confirm" {
			o.Kind = buttonDangerConfirm
		}
		s := renderButton(label, o)
		if !o.Disabled {
			hits = append(hits, hitRegion{ID: id, Y: len(lines), W: min(width, ansi.StringWidth(s)), H: 1})
		}
		lines = append(lines, cellCrop(s, width))
	}
	if m.connections.confirming && m.connections.closeAll {
		addText(m.tr("connections.confirm_all_title"), warning)
		addText(m.tr("connections.confirm_all_hint"), muted)
		addButton("conn.cancel", m.tr("connections.cancel"))
		addButton("conn.confirm", m.tr("connections.close_all"))
		return strings.Join(lines, "\n"), hits
	}
	if m.dashboardOptions().BodyTopRows == 0 {
		bar := m.connectionsToolbar(width, m.connectionToolbarRows(width))
		lines, hits = bar.lines, bar.targets
	}
	list := m.filteredConnections()
	if len(list) == 0 {
		body := m.emptyState(width, max(2, dashboardGeometry(m.dashboardOptions()).height-len(lines)), m.connectionsEmptyState())
		for _, target := range body.targets {
			target.Y += len(lines)
			hits = append(hits, target)
		}
		lines = append(lines, body.lines...)
		return strings.Join(lines, "\n"), hits
	}
	for _, diagnostic := range []struct {
		message string
		err     error
	}{{m.runtime.snapshot.Error, m.runtime.snapshot.Diagnostic}, {m.runtime.snapshot.ConnectionsError, m.runtime.snapshot.ConnectionsDiagnostic}} {
		if !m.connections.closedView && diagnostic.message != "" {
			addText(m.locale.Diagnostic(diagnostic.message, diagnostic.err), errorColor)
		}
	}
	// Keep logical rows and hit regions for the dashboard's shared navigation,
	// but let Bubbles size and render only the visible part of a busy list.
	rowHeight := 1
	if width < connectionTableMinWidth {
		rowHeight = 2
	}
	prefix := len(lines)
	height := max(1, dashboardGeometry(m.dashboardOptions()).height)
	start := max(0, min(m.scroll, max(0, prefix+len(list)*rowHeight-height)))
	first := min(len(list), max(0, (start-prefix)/rowHeight))
	end := min(len(list), max(first, (start+height-prefix+rowHeight-1)/rowHeight))
	var tableRows []string
	if width >= connectionTableMinWidth && end > first {
		tableRows = m.connectionTable(list[first:end], width).lines[1:]
	}
	for i, c := range list {
		row := len(lines)
		if i < first || i >= end {
			lines = append(lines, make([]string, rowHeight)...)
		} else if width >= connectionTableMinWidth {
			lines = append(lines, tableRows[i-first])
		} else {
			lines = append(lines, m.compactConnectionRows(c, width)...)
		}
		hits = append(hits, hitRegion{ID: "connection." + m.connectionRowKey(c), Y: row, W: width, H: len(lines) - row})
	}
	return strings.Join(lines, "\n"), hits
}

func (m *Model) connectionsEmptyState() emptyStateOptions {
	state := emptyStateOptions{Title: m.tr("connections.empty")}
	filtered := strings.TrimSpace(m.connections.search.Value()) != ""
	filterState := func() emptyStateOptions {
		state := emptyStateOptions{Title: m.tr("connections.no_matches"),
			ActionID: "conn.reset-filter", ActionLabel: m.tr("empty.clear_filters")}
		if _, err := m.connectionMatcher(); err != nil {
			state.Title, state.Error = m.tr("connections.invalid_regex"), true
		}
		return state
	}
	if m.connections.closedView {
		if filtered {
			return filterState()
		}
		state.Title = m.tr("connections.closed_empty")
		return state
	}
	if core, ok := m.coreEmptyState(); ok {
		if core.Error && core.ActionID == "" {
			core.ActionID, core.ActionLabel, core.Disabled = "conn.retry", m.tr("empty.retry"), m.runtime.pending
		}
		return core
	}
	for _, diagnostic := range []struct {
		message string
		err     error
	}{{m.runtime.snapshot.Error, m.runtime.snapshot.Diagnostic}, {m.runtime.snapshot.ConnectionsError, m.runtime.snapshot.ConnectionsDiagnostic}} {
		if diagnostic.message != "" {
			return emptyStateOptions{Title: m.tr("empty.read_failed"), Detail: m.locale.Diagnostic(diagnostic.message, diagnostic.err),
				ActionID: "conn.retry", ActionLabel: m.tr("empty.retry"), Error: true, Disabled: m.runtime.pending}
		}
	}
	if m.connectionSnapshot() == nil {
		state.Title = m.tr("empty.loading")
		if m.runtime.received && (m.runtime.snapshot.State == "unavailable" || m.runtime.snapshot.State == "unsupported") {
			state.Title, state.Error = m.tr("empty.read_failed"), true
			state.Detail = m.tr("connections.unavailable_hint")
			state.ActionID, state.ActionLabel, state.Disabled = "conn.retry", m.tr("empty.retry"), m.runtime.pending
		}
		return state
	}
	if filtered {
		return filterState()
	}
	return state
}
