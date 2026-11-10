package tui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"
)

func overviewCoreCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	status, color := coreSummary(s, o.Locale)
	transition := s.Service.State == "activating" || s.Service.State == "deactivating" || s.Service.State == "reloading"
	switch o.CoreAction {
	case "start":
		status, color, transition = tr("dashboard.status.starting"), warning, true
	case "stop":
		status, color, transition = tr("dashboard.status.stopping"), warning, true
	case "restart":
		status, color, transition = tr("dashboard.core.restarting"), warning, true
	}
	stopped := s.Service.State == "inactive" || s.Service.State == "failed"
	unknown := !s.Service.Active && !stopped && !transition
	if s.Installed && s.ServiceError == "" && unknown {
		status, color = tr("dashboard.status.unknown"), warning
	}
	unexpectedExit := stopped && s.Service.State != "failed" && s.Desired.Known && s.Desired.Running
	if unexpectedExit && !transition {
		status, color = tr("dashboard.core.unexpectedExit"), errorColor
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render(style("Mihomo", text)),
		lipgloss.NewStyle().Bold(true).Render(style("● "+status, color)),
	}
	if !s.Installed {
		return overviewControlPanel(o, width, tr("dashboard.overview.service"), lines, []overviewControlButton{
			{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy},
		})
	}
	running := s.Service.Active && s.ServiceError == ""
	version := s.Version
	if running && s.RunningVersion != "" {
		version = s.RunningVersion
	}
	differentVersions := running && s.RunningVersion != "" && s.Version != "" && s.RunningVersion != s.Version
	headerVersion := overviewVersion(version, "dashboard.status.unknown", o.Locale)
	if differentVersions {
		headerVersion = ""
		lines = append(lines, style(tr("dashboard.kernel.running", clean(s.RunningVersion)), text),
			style(tr("dashboard.kernel.installed", clean(s.Version)), warning),
			style(tr("dashboard.core.restartVersion"), warning))
	}
	if running && s.Desired.Known && !s.Desired.Running && !transition {
		lines = append(lines, style(tr("dashboard.core.stopRequested"), warning))
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	if running && !transition && s.Service.PID > 0 && !s.Service.StartedAt.IsZero() && !s.Service.StartedAt.After(now) {
		lines = append(lines, style(tr("dashboard.core.uptime", formatCoreUptime(now.Sub(s.Service.StartedAt))), muted))
	}
	failed := s.Service.State == "failed" && !transition
	if failed {
		reason := tr("dashboard.core.failedHint")
		if s.Service.Result != "" && s.Service.Result != "success" {
			reason = tr("dashboard.core.failureReason", clean(s.Service.Result), s.Service.ExitCode)
		}
		lines = append(lines, style(reason, errorColor))
	} else if unexpectedExit && !transition {
		lines = append(lines, style(tr("dashboard.core.exitHint"), warning))
	}
	monitorUnavailable := s.APIError != ""
	if o.RuntimeReceived && o.Runtime.State == "ready" {
		// Successful live observations supersede an older full snapshot's
		// controller diagnostic. Independent stream errors remain visible.
		if o.Runtime.RunningVersion != "" && (o.Runtime.Traffic != nil || o.Runtime.Memory != nil || o.Runtime.Connections != nil) {
			monitorUnavailable = false
		}
		monitorUnavailable = monitorUnavailable || o.Runtime.Error != "" || o.Runtime.TrafficError != "" || o.Runtime.MemoryError != "" || o.Runtime.ConnectionsError != ""
	}
	if s.ServiceError != "" {
		lines = append(lines, style(cellCrop(o.Locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic), max(1, width-4)), warning))
	} else if running && monitorUnavailable {
		// Controller health is separate from the systemd process state.
		lines = append(lines, style(tr("dashboard.core.monitorUnavailable"), warning))
	} else if !running && !transition && !stopped {
		lines = append(lines, style(tr("dashboard.core.unknownHint"), muted))
	}
	if issue := runtimeStopIssue(s, o.Locale); issue != "" {
		lines = append(lines, style(issue, muted))
	}
	canControl := !o.Busy && !transition && !unknown && runtimeControlIssue(s, o.Locale) == ""
	var buttons []overviewControlButton
	if stopped && s.ServiceError == "" {
		buttons = append(buttons, overviewControlButton{id: "core.start", label: tr("dashboard.core.start"), enabled: canControl, primary: true})
	} else {
		buttons = append(buttons, overviewControlButton{id: "core.stop", label: tr("dashboard.core.stop"), enabled: !o.Busy && !transition && runtimeStopIssue(s, o.Locale) == ""})
	}
	buttons = append(buttons, overviewControlButton{id: "core.restart", label: tr("dashboard.action.restart"), enabled: canControl})
	if failed || unexpectedExit || running && monitorUnavailable {
		buttons = append(buttons, overviewControlButton{id: "card.logs", label: tr("dashboard.core.logs"), enabled: !o.Busy})
	}
	return overviewControlPanel(o, width, tr("dashboard.overview.service"), lines, buttons, headerVersion)
}

func formatCoreUptime(elapsed time.Duration) string {
	seconds := int64(max(0, elapsed) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
