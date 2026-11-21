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
	content := overviewCoreSummary(o, inner, version, uptime)
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
	return overviewPanel(o, width, tr("dashboard.overview.service"), content, nil, "", nil)
}

func overviewCoreSummary(o dashboardOptions, width int, version, uptime string) dashboardBlock {
	control, labels := overviewCoreToggle(o)
	startHint, stopHint := o.Locale.T("dashboard.core.toggle.startHint"), o.Locale.T("dashboard.core.toggle.stopHint")
	slot := min(width, max(overviewCoreToggleWidth(o, labels)+4, ansi.StringWidth(startHint), ansi.StringWidth(stopHint)))
	// The brackets keep the control recognizable even without terminal colors.
	face := renderToggle(control, toggleOptions{Labels: labels, Width: max(1, slot-4),
		Surface: panel, Focused: o.Focus == "core.toggle" || o.Hover == "core.toggle" && control.canToggle() && !o.Busy, Dim: o.Dim})
	toggle := cellCrop(themed("[ ", muted, panel, o.Dim)+face+themed(" ]", muted, panel, o.Dim), slot)
	hint := ""
	if control.canToggle() && !o.Busy {
		hint = startHint
		if control.state == toggleOn {
			hint = stopHint
		}
	}
	hint = cellCrop(hint, slot)
	hint = themed(strings.Repeat(" ", max(0, slot-ansi.StringWidth(hint)))+hint, muted, panel, o.Dim)
	identity := themed(lipgloss.NewStyle().Bold(true).Render("Mihomo"), text, panel, o.Dim)
	if version = clean(version); version != "" {
		identity += themed("  "+version, muted, panel, o.Dim)
	}
	// Budget the full time.Sub range and reserve both supporting rows, so long
	// uptimes and lifecycle changes never move the control or nearby cards.
	uptimeBudget := o.Locale.T("dashboard.core.uptime", formatCoreUptime(time.Duration(1<<63-1)))
	leftWidth := max(ansi.StringWidth(identity), ansi.StringWidth(uptimeBudget))
	var row dashboardBlock
	x, y := width-slot, 0
	if width >= leftWidth+2+slot {
		row.lines = []string{
			themed(cellPad(identity, x), text, panel, o.Dim) + toggle,
			themed(cellPad(uptime, x), muted, panel, o.Dim) + hint,
		}
	} else {
		row = overviewTextBlock([]string{identity}, width)
		rows := len(strings.Split(wrapText(uptimeBudget, width), "\n"))
		lines := make([]string, rows)
		if uptime != "" {
			copy(lines, strings.Split(wrapText(uptime, width), "\n"))
		}
		for i := range lines {
			lines[i] = themed(lines[i], muted, panel, o.Dim)
		}
		row.append(overviewTextBlock(lines, width))
		y = len(row.lines)
		padding := themed(strings.Repeat(" ", x), text, panel, o.Dim)
		row.lines = append(row.lines, padding+toggle, padding+hint)
	}
	row.targets = []hitRegion{{ID: "core.toggle", X: x, Y: y, W: slot, H: 2}}
	return row
}

func formatCoreUptime(elapsed time.Duration) string {
	seconds := int64(max(0, elapsed) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
