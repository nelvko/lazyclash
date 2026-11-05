package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m Model) connectionNow() time.Time {
	if m.connections.paused && m.connections.detail == nil {
		return m.connections.frozenAt
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

func connectionValue(s string) string {
	if strings.TrimSpace(s) == "" {
		return "--"
	}
	return clean(s)
}

func (m Model) connectionControls() dashboardBlock {
	var b dashboardBlock
	line := ""
	pause := m.tr("connections.pause")
	if m.connections.paused {
		pause = m.tr("connections.resume")
	}
	for _, item := range []struct{ id, label string }{
		{"conn.clear", "×"}, {"conn.pause", pause},
		{"conn.sort", m.tr("connections.sort", m.tr(m.connections.sort.key()))},
	} {
		o := dashboardButtonOptions(item.id, m.navigationOptions(), false, bg)
		o.Disabled = m.busy || (item.id == "conn.pause" && m.connectionSnapshot() == nil && !m.connections.paused)
		value := renderButton(item.label, o)
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: item.id, X: ansi.StringWidth(line), W: ansi.StringWidth(value), H: 1})
		}
		line += value
	}
	b.lines = []string{line}
	return b
}

func (m Model) connectionToolbarRows(width int) int {
	if width >= 100 {
		return 6
	}
	return 7
}

func (m Model) connectionSearchWidth(width int) int {
	if width >= 100 {
		width -= ansi.StringWidth(m.connectionControls().lines[0]) + 2
	}
	return searchInputWidth(width)
}

func (m Model) connectionsToolbar(width, rows int) dashboardBlock {
	var b dashboardBlock
	stats := m.tr("connections.count", 0, 0)
	if snapshot := m.connectionSnapshot(); snapshot != nil {
		stats = m.tr("connections.count", len(m.filteredConnections()), len(snapshot.Connections))
		stats += "   Σ ↓ " + formatBytes(snapshot.DownloadTotal) + "  ↑ " + formatBytes(snapshot.UploadTotal)
	}
	b.lines = append(b.lines, themed(cellCrop(stats, width), accent, bg, false))
	b.append(m.searchToolbar("conn.search", m.tr("connections.search_placeholder"), m.connections.search,
		m.connectionControls(), width, width >= 100, false))
	status, color := runtimeStatus(m.runtime.snapshot, m.runtime.received, m.runtime.pending, m.snapshot.Installed, m.locale), muted
	if m.connections.paused && m.connectionSnapshot() != nil {
		status, color = m.tr("connections.paused"), warning
	}
	b.lines = append(b.lines, themed(cellCrop(status, width), color, bg, false))
	if width >= 100 {
		b.lines = append(b.lines, m.connectionTable(nil, width)[0])
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

// Bubbles table handles cell sizing, ANSI-aware truncation and selection. The
// dashboard retains the only scrolling viewport and maps rows to opaque IDs.
func (m Model) connectionTable(list []mihomo.Connection, width int) []string {
	columns := []table.Column{{Title: "", Width: 1}, {Title: m.tr("connections.destination"), Width: 24}, {Title: m.tr("connections.protocol"), Width: 5}}
	if width >= 125 {
		columns = append(columns, table.Column{Title: m.tr("connections.process"), Width: 12})
	}
	chainColumn := len(columns)
	columns = append(columns, table.Column{Title: m.tr("connections.chain"), Width: 18})
	ruleColumn := -1
	if width >= 125 {
		ruleColumn = len(columns)
		columns = append(columns, table.Column{Title: m.tr("connections.rule"), Width: 16})
	}
	columns = append(columns, table.Column{Title: m.tr("connections.download_speed"), Width: 11}, table.Column{Title: m.tr("connections.upload_speed"), Width: 11})
	if width >= 155 {
		columns = append(columns, table.Column{Title: m.tr("connections.download_total"), Width: 10}, table.Column{Title: m.tr("connections.upload_total"), Width: 10})
	}
	columns = append(columns, table.Column{Title: m.tr("connections.duration"), Width: 8})
	used := len(columns)
	for _, c := range columns {
		used += c.Width
	}
	extra := max(0, width-used)
	if ruleColumn >= 0 {
		columns[1].Width += extra / 3
		columns[chainColumn].Width += extra / 3
		columns[ruleColumn].Width += extra - 2*(extra/3)
	} else {
		columns[1].Width += extra / 2
		columns[chainColumn].Width += extra - extra/2
	}
	var rows []table.Row
	for _, c := range list {
		marker := " "
		if c.ID == m.connections.selected {
			marker = "▌"
		}
		if m.focus == "connection."+c.ID {
			marker = "›"
		}
		row := table.Row{marker, connectionDestination(c), strings.ToUpper(clean(c.Metadata.Network))}
		if width >= 125 {
			row = append(row, connectionValue(c.Metadata.Process))
		}
		row = append(row, connectionChain(c))
		if width >= 125 {
			row = append(row, connectionRule(c))
		}
		rate := m.displayedConnectionRates().get(c.ID)
		row = append(row, connectionSpeed(rate.Download), connectionSpeed(rate.Upload))
		if width >= 155 {
			row = append(row, formatBytes(c.Download), formatBytes(c.Upload))
		}
		row = append(row, connectionDuration(c, m.connectionNow()))
		rows = append(rows, row)
	}
	style := func(fg, surface string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(surface))
	}
	t := table.New(table.WithColumns(columns), table.WithRows(rows), table.WithWidth(width), table.WithHeight(max(2, len(rows)+1)), table.WithStyles(table.Styles{
		Header: style(muted, panel).PaddingRight(1), Cell: lipgloss.NewStyle().PaddingRight(1), Selected: lipgloss.NewStyle(),
	}))
	lines := strings.Split(t.View(), "\n")[:len(rows)+1]
	for i, c := range list {
		foreground, surface := text, bg
		if c.ID == m.connections.selected {
			surface = selectionBackground
		}
		if m.hover == "connection."+c.ID {
			surface = hoverBackground
		}
		if m.focus == "connection."+c.ID {
			foreground = focus
		}
		lines[i+1] = themed(cellPad(lines[i+1], width), foreground, surface, false)
	}
	return lines
}

func (m Model) compactConnectionRows(c mihomo.Connection, width int) []string {
	id, surface, marker := "connection."+c.ID, bg, "  "
	if c.ID == m.connections.selected {
		surface, marker = panel, "▌ "
	}
	if m.hover == id {
		surface = hoverBackground
	}
	foreground := text
	if m.focus == id {
		marker, foreground = "› ", focus
	}
	rate := m.displayedConnectionRates().get(c.ID)
	first := marker + connectionDestination(c) + " · " + strings.ToUpper(clean(c.Metadata.Network))
	second := "  ↓ " + connectionSpeed(rate.Download) + " ↑ " + connectionSpeed(rate.Upload) + " · " + connectionValue(c.Metadata.Process) + " · " + connectionChain(c) + " · " + connectionRule(c) + " · " + connectionDuration(c, m.connectionNow())
	return []string{themed(cellPad(first, width), foreground, surface, false), themed(cellPad(second, width), muted, surface, false)}
}

func (m Model) connectionsBody(width int) (string, []hitRegion) {
	var lines []string
	var hits []hitRegion
	addText := func(s, color string) {
		lines = append(lines, strings.Split(wrapText(themed(clean(s), color, bg, false), max(1, width)), "\n")...)
	}
	addButton := func(id, label string) {
		o := dashboardButtonOptions(id, m.navigationOptions(), false, bg)
		o.Disabled = m.busy
		if id == "conn.close" {
			o.Kind = buttonDanger
		} else if id == "conn.confirm" {
			o.Kind = buttonDangerConfirm
		}
		s := renderButton(label, o)
		if !o.Disabled {
			hits = append(hits, hitRegion{ID: id, Y: len(lines), W: min(width, ansi.StringWidth(s)), H: 1})
		}
		lines = append(lines, cellCrop(s, width))
	}
	if c := m.connections.detail; c != nil {
		addText(m.tr("connections.details"), text)
		if m.detailActive() {
			addText(m.tr("connections.active"), success)
		} else {
			addText(m.tr("connections.inactive_detail"), warning)
		}
		lines = append(lines, "")
		if m.connections.confirming {
			addText(m.tr("connections.confirm_title"), warning)
			addText(m.tr("connections.confirm_hint"), muted)
			addButton("conn.cancel", m.tr("connections.cancel"))
			addButton("conn.confirm", m.tr("connections.close"))
		} else {
			addButton("conn.back", m.tr("connections.back"))
			if m.detailActive() {
				addButton("conn.close", m.tr("connections.close_shortcut"))
			}
		}
		md := c.Metadata
		rate := m.connections.rates.get(c.ID)
		if !m.detailActive() {
			rate = connectionRate{Upload: -1, Download: -1}
		}
		started := "--"
		if !c.Start.IsZero() {
			started = c.Start.Local().Format(time.RFC3339)
		}
		fields := [][2]string{
			{m.tr("connections.destination"), connectionDestination(*c)}, {m.tr("connections.process"), md.Process},
			{m.tr("connections.download_speed"), connectionSpeed(rate.Download)}, {m.tr("connections.upload_speed"), connectionSpeed(rate.Upload)},
			{m.tr("connections.download_total"), formatBytes(c.Download)}, {m.tr("connections.upload_total"), formatBytes(c.Upload)},
			{m.tr("connections.chain"), connectionChain(*c)}, {m.tr("connections.rule"), connectionRule(*c)},
			{m.tr("connections.duration"), connectionDuration(*c, m.connectionNow())}, {m.tr("connections.started"), started},
			{m.tr("connections.source"), joinAddress(md.SourceIP, md.SourcePort)}, {m.tr("connections.resolved_ip"), md.DestinationIP},
			{m.tr("connections.network_inbound"), md.Network + " / " + md.Type}, {m.tr("connections.process_path"), md.ProcessPath}, {m.tr("connections.id"), c.ID},
		}
		for _, f := range fields {
			addText(f[0]+": "+connectionValue(f[1]), text)
		}
		return strings.Join(lines, "\n"), hits
	}
	if m.dashboardOptions().BodyTopRows == 0 {
		bar := m.connectionsToolbar(width, m.connectionToolbarRows(width))
		lines, hits = bar.lines, bar.targets
	}
	for _, diagnostic := range []struct {
		message string
		err     error
	}{{m.runtime.snapshot.Error, m.runtime.snapshot.Diagnostic}, {m.runtime.snapshot.ConnectionsError, m.runtime.snapshot.ConnectionsDiagnostic}} {
		if diagnostic.message != "" {
			addText(m.locale.Diagnostic(diagnostic.message, diagnostic.err), errorColor)
		}
	}
	list := m.filteredConnections()
	if len(list) == 0 {
		empty := m.tr("connections.empty")
		if m.connections.search.Value() != "" {
			empty = m.tr("connections.no_matches")
		}
		if m.connectionSnapshot() == nil {
			empty = m.tr("connections.unavailable_hint")
		}
		addText(empty, muted)
	}
	// Keep logical rows and hit regions for the dashboard's shared navigation,
	// but let Bubbles size and render only the visible part of a busy list.
	rowHeight := 1
	if width < 100 {
		rowHeight = 2
	}
	prefix := len(lines)
	height := max(1, dashboardGeometry(m.dashboardOptions()).height)
	start := max(0, min(m.scroll, max(0, prefix+len(list)*rowHeight-height)))
	first := min(len(list), max(0, (start-prefix)/rowHeight))
	end := min(len(list), max(first, (start+height-prefix+rowHeight-1)/rowHeight))
	var tableRows []string
	if width >= 100 && end > first {
		tableRows = m.connectionTable(list[first:end], width)[1:]
	}
	for i, c := range list {
		row := len(lines)
		if i < first || i >= end {
			lines = append(lines, make([]string, rowHeight)...)
		} else if width >= 100 {
			lines = append(lines, tableRows[i-first])
		} else {
			lines = append(lines, m.compactConnectionRows(c, width)...)
		}
		hits = append(hits, hitRegion{ID: "connection." + c.ID, Y: row, W: width, H: len(lines) - row})
	}
	return strings.Join(lines, "\n"), hits
}
