package tui

import "fmt"

func overviewTrafficCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Runtime
	status := runtimeStatus(s, o.RuntimeReceived, o.RuntimePending, o.Snapshot.Installed, o.Locale)
	colors := trafficCurveColors()
	legend := themed(tr("dashboard.overview.upload"), colors.upload, panel, o.Dim) + themed("   ", text, panel, o.Dim) + themed(tr("dashboard.overview.download"), colors.download, panel, o.Dim)
	lines := []string{legend}
	if !o.RuntimeReceived || s.State != "ready" || s.Traffic == nil || s.Connections == nil || s.Memory == nil {
		lines = append(lines, themed(status, muted, panel, o.Dim))
	}
	plotHeight := 6
	if width < 76 || o.Height < 24 {
		plotHeight = 4
	}
	inner := max(1, width-4)
	lines = append(lines, overviewTrafficChart(o, inner, plotHeight)...)
	ready := o.RuntimeReceived && s.State == "ready" && s.Error == ""
	showMetrics := ready || o.Snapshot.Installed && o.Snapshot.Service.Active && s.State != "stopped" && s.State != "uninstalled"
	if showMetrics {
		up, down, connections, totalUp, totalDown, memory := "--", "--", "--", "--", "--", "--"
		if ready && s.Traffic != nil && s.TrafficError == "" {
			up, down = formatBytes(s.Traffic.Up)+"/s", formatBytes(s.Traffic.Down)+"/s"
			totalUp, totalDown = formatBytes(s.Traffic.UpTotal), formatBytes(s.Traffic.DownTotal)
		}
		if ready && s.Connections != nil && s.ConnectionsError == "" {
			connections = fmt.Sprint(len(s.Connections.Connections))
		}
		if ready && s.Memory != nil && s.MemoryError == "" {
			memory = formatBytes(int64(*s.Memory))
		}
		columns := 1
		if width >= 72 {
			columns = 3
		} else if width >= 34 {
			columns = 2
		}
		fields := []overviewField{
			{label: tr("dashboard.overview.upload"), value: up, detail: tr("dashboard.overview.totalUpload"), detailValue: totalUp, color: colors.upload},
			{label: tr("dashboard.overview.download"), value: down, detail: tr("dashboard.overview.totalDownload"), detailValue: totalDown, color: colors.download},
			{label: tr("dashboard.field.connections"), value: connections, detail: tr("dashboard.field.coreMemory"), detailValue: memory},
		}
		lines = append(lines, "")
		lines = append(lines, overviewFieldGrid(o, fields, inner, columns)...)
		lines = append(lines, "", themed(tr("dashboard.field.sinceStart")+" · "+tr("dashboard.traffic.resetHint"), muted, panel, o.Dim))
	} else if !o.Snapshot.Installed || s.State == "stopped" || s.State == "uninstalled" {
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
	return overviewControlPanel(o, width, tr("dashboard.overview.traffic"), lines, nil, tr("dashboard.overview.historyWindow"))
}
