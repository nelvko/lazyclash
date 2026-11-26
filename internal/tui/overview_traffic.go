package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func overviewTrafficCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Runtime
	inner := overviewInnerWidth(width)
	ready := o.RuntimeReceived && s.State == "ready" && s.Error == ""
	showMetrics := ready || o.Snapshot.Installed && o.Snapshot.Service.Active && s.State != "stopped" && s.State != "uninstalled"
	plotHeight := 6
	if width < 76 || o.Height < 24 {
		plotHeight = 4
	}
	chart := overviewTrafficChart(o, inner, plotHeight)
	title := tr("dashboard.overview.traffic")
	window := tr("dashboard.overview.historyWindow")
	if strings.Contains(chart[plotHeight], trafficChartGapMark) {
		withKey := trafficChartGapMark + " " + tr("dashboard.overview.trafficGap") + "   " + window
		if ansi.StringWidth(title)+2+ansi.StringWidth(withKey) <= inner {
			window = withKey
		}
	}

	var lines []string
	starting := !o.RuntimeReceived && showMetrics && o.Snapshot.ServiceError == "" && o.Snapshot.APIError == ""
	partial := (s.Traffic != nil || s.Connections != nil || s.Memory != nil) && (s.Traffic == nil || s.Connections == nil || s.Memory == nil)
	if !ready && !starting || ready && partial {
		status := runtimeStatus(s, o.RuntimeReceived, o.RuntimePending, o.Snapshot.Installed, o.Locale)
		lines = append(lines, themed(status, muted, panel, o.Dim))
	}
	lines = append(lines, chart...)
	lines = append(lines, "")
	if showMetrics {
		lines = append(lines, overviewTrafficReadings(o, ready, inner)...)
	} else {
		legend := themed(tr("dashboard.overview.upload"), trafficUploadColor, panel, o.Dim) + "   " +
			themed(tr("dashboard.overview.download"), trafficDownloadColor, panel, o.Dim)
		lines = append(lines, legend)
	}
	if !showMetrics && (!o.Snapshot.Installed || s.State == "stopped" || s.State == "uninstalled") {
		lines = append(lines, themed(tr("dashboard.traffic.startHint"), muted, panel, o.Dim))
	}
	for _, issue := range []struct {
		text  string
		cause error
	}{{s.Error, s.Diagnostic}, {s.ConnectionsError, s.ConnectionsDiagnostic}, {s.TrafficError, s.TrafficDiagnostic}, {s.MemoryError, s.MemoryDiagnostic}} {
		if issue.text != "" {
			lines = append(lines, themed(clean(o.Locale.Diagnostic(issue.text, issue.cause)), errorColor, panel, o.Dim))
			break
		}
	}
	return overviewControlPanel(o, width, title, lines, nil, window)
}

func overviewTrafficReadings(o dashboardOptions, ready bool, width int) []string {
	tr := o.Locale.T
	s := o.Runtime
	up, down, connections, totalUp, totalDown, memory := "--", "--", "--", "--", "--", "--"
	if ready && s.Traffic != nil && s.TrafficError == "" {
		up, down = connectionSpeed(s.Traffic.Up), connectionSpeed(s.Traffic.Down)
		totalUp, totalDown = formatBytes(s.Traffic.UpTotal), formatBytes(s.Traffic.DownTotal)
	}
	if ready && s.Connections != nil && s.ConnectionsError == "" {
		connections = fmt.Sprint(len(s.Connections.Connections))
	}
	if ready && s.Memory != nil && s.MemoryError == "" {
		memory = formatBytes(*s.Memory)
	}
	groups := []struct {
		label, value, color, detail, detailValue string
		reservedValueWidth                       int
	}{
		{tr("dashboard.overview.upload"), up, trafficUploadColor, tr("dashboard.overview.totalUpload"), totalUp, len("1024.0 KiB/s")},
		{tr("dashboard.overview.download"), down, trafficDownloadColor, tr("dashboard.overview.totalDownload"), totalDown, len("1024.0 KiB/s")},
		{tr("dashboard.field.connections"), connections, muted, tr("dashboard.field.coreMemory"), memory, len("1024.0 MiB")},
	}

	// Reserve space for typical rate and total units so a new sample does not
	// move a group to another row as its readout grows or shrinks.
	minWidth := 0
	for _, g := range groups {
		labelWidth := max(ansi.StringWidth(g.label), ansi.StringWidth(g.detail))
		valueWidth := max(g.reservedValueWidth, len(g.value), len(g.detailValue))
		minWidth = max(minWidth, labelWidth+2+valueWidth)
	}
	columns := min(len(groups), max(1, (width+2)/(minWidth+2)))
	cellWidth := (width - 2*(columns-1)) / columns
	pair := func(label, value string, labelWidth int) []string {
		if ansi.StringWidth(label)+2+ansi.StringWidth(value) <= cellWidth {
			return []string{cellPad(label, min(labelWidth, cellWidth-2-ansi.StringWidth(value))) + "  " + value}
		}
		return append(strings.Split(wrapText(label, cellWidth), "\n"), strings.Split(wrapText(value, cellWidth), "\n")...)
	}
	var rows []string
	for start := 0; start < len(groups); start += columns {
		var cells [][]string
		for _, g := range groups[start:min(start+columns, len(groups))] {
			current := themed(lipgloss.NewStyle().Bold(true).Render(g.value), text, panel, o.Dim)
			labelWidth := max(ansi.StringWidth(g.label), ansi.StringWidth(g.detail))
			cell := pair(themed(g.label, g.color, panel, o.Dim), current, labelWidth)
			if columns == len(groups) && o.Height >= 24 {
				cell = append(cell, "")
			}
			cell = append(cell, pair(themed(g.detail, muted, panel, o.Dim), themed(g.detailValue, muted, panel, o.Dim), labelWidth)...)
			cells = append(cells, cell)
		}
		if start > 0 {
			rows = append(rows, "")
			if o.Height >= 24 {
				rows = append(rows, "")
			}
		}
		rows = append(rows, overviewFieldRow(o, cells, cellWidth)...)
	}
	return rows
}
