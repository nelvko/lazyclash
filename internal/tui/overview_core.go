package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func overviewCoreCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	name := lipgloss.NewStyle().Bold(true).Render(style("Mihomo", text))
	if !s.Installed {
		return overviewControlPanel(o, width, tr("dashboard.overview.service"), []string{name}, []overviewControlButton{
			{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy},
		})
	}
	inner := max(1, width-4)
	if width < 4 {
		inner = max(1, width)
	}
	control, _ := overviewCoreToggle(o)
	transition := control.state == togglePending
	running := s.Service.Active && s.ServiceError == ""
	version := s.Version
	if running && s.RunningVersion != "" {
		version = s.RunningVersion
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	uptime := ""
	if control.state == toggleOn && s.Service.PID > 0 && !s.Service.StartedAt.IsZero() && !s.Service.StartedAt.After(now) {
		uptime = tr("dashboard.core.uptime", formatCoreUptime(now.Sub(s.Service.StartedAt)))
	}
	content := overviewTextBlock([]string{name}, inner)
	content.append(overviewCoreStatusRow(o, inner, uptime))
	var details []string
	if running && s.RunningVersion != "" && s.Version != "" && s.RunningVersion != s.Version {
		details = append(details, style(tr("dashboard.kernel.installed", clean(s.Version)), warning), style(tr("dashboard.core.restartVersion"), warning))
	}
	if running && s.Desired.Known && !s.Desired.Running && !transition {
		details = append(details, style(tr("dashboard.core.stopRequested"), warning))
	}
	stopped := s.Service.State == "inactive" || s.Service.State == "failed"
	unexpectedExit := stopped && s.Service.State != "failed" && s.Desired.Known && s.Desired.Running
	if s.Service.State == "failed" && !transition {
		reason := tr("dashboard.core.failedHint")
		if s.Service.Result != "" && s.Service.Result != "success" {
			reason = tr("dashboard.core.failureReason", clean(s.Service.Result), s.Service.ExitCode)
		}
		details = append(details, style(reason, errorColor))
	} else if unexpectedExit && !transition {
		details = append(details, style(tr("dashboard.core.exitHint"), warning))
	}
	monitorUnavailable := s.APIError != ""
	if o.RuntimeReceived && o.Runtime.State == "ready" {
		if o.Runtime.RunningVersion != "" && (o.Runtime.Traffic != nil || o.Runtime.Memory != nil || o.Runtime.Connections != nil) {
			monitorUnavailable = false
		}
		monitorUnavailable = monitorUnavailable || o.Runtime.Error != "" || o.Runtime.TrafficError != "" || o.Runtime.MemoryError != "" || o.Runtime.ConnectionsError != ""
	}
	if s.ServiceError != "" {
		details = append(details, style(cellCrop(o.Locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic), inner), warning))
	} else if running && monitorUnavailable {
		details = append(details, style(tr("dashboard.core.monitorUnavailable"), warning))
	} else if control.state == toggleUnknown {
		details = append(details, style(tr("dashboard.core.unknownHint"), muted))
	}
	if issue := runtimeStopIssue(s, o.Locale); issue != "" {
		details = append(details, style(issue, muted))
	}
	content.append(overviewTextBlock(details, inner))
	// Keep a narrow card's version right-aligned when the heading cannot hold
	// both fields. Other overview panels retain their existing metadata layout.
	version = clean(version)
	if version != "" && inner-ansi.StringWidth(tr("dashboard.overview.service"))-ansi.StringWidth(version) < 2 {
		var before dashboardBlock
		for _, line := range strings.Split(wrapText(version, inner), "\n") {
			before.lines = append(before.lines, themed(strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)))+line, muted, panel, o.Dim))
		}
		before.append(content)
		content, version = before, ""
	}
	return overviewPanel(o, width, tr("dashboard.overview.service"), content, nil, version, nil)
}

func overviewCoreStatusRow(o dashboardOptions, width int, uptime string) dashboardBlock {
	control, labels := overviewCoreToggle(o)
	slot := min(width, overviewCoreToggleWidth(o, labels))
	toggle := renderToggle(control, toggleOptions{Labels: labels, Width: slot, RightAligned: true,
		Surface: panel, Focused: o.Focus == "core.toggle", Dim: o.Dim})
	var row dashboardBlock
	// Reserve the uptime's row even when absent, so lifecycle changes do not
	// move the control. Narrow cards give the two fields separate rows.
	// time.Sub saturates at this duration. Budget its full hour width so the
	// same layout works after 99 hours and when a stopped snapshot has no time.
	uptimeBudget := o.Locale.T("dashboard.core.uptime", formatCoreUptime(time.Duration(1<<63-1)))
	leftWidth := ansi.StringWidth(uptimeBudget)
	if width >= leftWidth+2+slot {
		row.lines = []string{themed(cellPad(uptime, width-slot), muted, panel, o.Dim) + toggle}
	} else {
		rows := len(strings.Split(wrapText(uptimeBudget, width), "\n"))
		lines := make([]string, rows)
		if uptime != "" {
			copy(lines, strings.Split(wrapText(uptime, width), "\n"))
		}
		row = overviewTextBlock(lines, width)
		row.lines = append(row.lines, themed(strings.Repeat(" ", width-slot), text, panel, o.Dim)+toggle)
	}
	row.targets = []hitRegion{{ID: "core.toggle", X: width - slot, Y: len(row.lines) - 1, W: slot, H: 1}}
	return row
}

func formatCoreUptime(elapsed time.Duration) string {
	seconds := int64(max(0, elapsed) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
