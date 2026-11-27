package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func overviewCoreCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	state, _, color := headerCoreState(o)
	inner := overviewInnerWidth(width)
	name, status := overviewHeading(o, "Mihomo"), style(cellCrop(state, inner), color)
	var diagnostics []string
	control := overviewCoreToggle(o)
	transition := control.state == togglePending
	if !o.StateUnavailable && !transition {
		failedResult := s.Service.Result != "" && s.Service.Result != "success"
		if failedResult {
			diagnostics = append(diagnostics, style(tr("dashboard.core.serviceResult", clean(s.Service.Result), s.Service.ExitCode), warning))
		}
		if s.ServiceError != "" {
			diagnostics = append(diagnostics, style(o.Locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic), warning))
		}
		if failedResult || s.Service.State == "failed" || s.ServiceError != "" {
			diagnostics = append(diagnostics, style(tr("dashboard.core.doctorHint"), muted))
		}
	}
	if !s.Installed && !o.StateUnavailable {
		lines := append([]string{name, status}, diagnostics...)
		return overviewControlPanel(o, width, tr("dashboard.overview.service"), lines, []overviewControlButton{
			{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy},
		})
	}
	running := !o.StateUnavailable && s.Service.Active && s.ServiceError == ""
	if running && s.RunningVersion != "" && s.Version != "" && s.RunningVersion != s.Version {
		diagnostics = append(diagnostics, style(tr("dashboard.kernel.running", clean(s.RunningVersion)), warning),
			style(tr("dashboard.kernel.installed", clean(s.Version)), warning), style(tr("dashboard.core.restartVersion"), warning))
	}
	if !o.StateUnavailable && s.ServiceError == "" && (s.Service.Active || s.Service.PID != 0) && s.Desired.Known && !s.Desired.Running && !transition {
		diagnostics = append(diagnostics, style(tr("dashboard.core.stopRequested"), warning))
	}
	if s.Service.State == "inactive" && s.Desired.Known && s.Desired.Running && !transition {
		diagnostics = append(diagnostics, style(tr("dashboard.core.exitHint"), warning))
	}
	monitorUnavailable := s.APIError != ""
	if o.RuntimeReceived && o.Runtime.State == "ready" {
		if o.Runtime.RunningVersion != "" && (o.Runtime.Traffic != nil || o.Runtime.Memory != nil || o.Runtime.Connections != nil) {
			monitorUnavailable = false
		}
		monitorUnavailable = monitorUnavailable || o.Runtime.Error != "" || o.Runtime.TrafficError != "" || o.Runtime.MemoryError != "" || o.Runtime.ConnectionsError != ""
	}
	if running && monitorUnavailable {
		diagnostics = append(diagnostics, style(tr("dashboard.core.monitorUnavailable"), warning))
	} else if control.state == toggleUnknown {
		diagnostics = append(diagnostics, style(tr("dashboard.core.unknownHint"), muted))
	}
	if issue := runtimeStopIssue(s, o.Locale); !o.StateUnavailable && issue != "" {
		diagnostics = append(diagnostics, style(issue, muted))
	}
	content := overviewTextBlock([]string{name}, inner)
	// Choose the row using the pending label too, so a click cannot move the
	// same button onto a second line while systemd is starting or stopping.
	pendingWidth := 2 + max(ansi.StringWidth(tr("dashboard.status.starting")), ansi.StringWidth(tr("dashboard.status.stopping")))
	status = cellPad(status, min(inner, max(ansi.StringWidth(status), pendingWidth)))
	statusRow := overviewLabelSlot(o, inner, status, coreActionFace(o, control), "core.toggle", true)
	details := overviewTextBlock(diagnostics, inner)
	if sidebarGeometry(o, false).hasClockRow() {
		content.append(details)
		// Keep the status row at the bottom when the neighboring card is taller.
		content.blank()
		content.footerY = len(content.lines)
		content.append(statusRow)
	} else {
		content.append(statusRow)
		longestUptime := coreUptimeLabel(time.Duration(1<<63-1), o.Locale)
		minRows := len(content.lines) + 1 + strings.Count(wrapText(longestUptime, inner), "\n")
		if elapsed, ok := o.coreUptime(); ok {
			content.append(overviewTextBlock([]string{style(coreUptimeLabel(elapsed, o.Locale), muted)}, inner))
		}
		content.append(details)
		for len(content.lines) < minRows {
			content.blank()
		}
	}

	return overviewPanel(o, width, coreManagementHeading(o, inner), content, nil)
}

func coreManagementHeading(o dashboardOptions, inner int) dashboardBlock {
	manage := dashboardButtonOptions("core.manage", o, false, panel)
	manage.Kind, manage.Disabled = buttonText, !o.Snapshot.Installed || o.StateUnavailable || o.Busy
	id := "core.manage"
	if manage.Disabled {
		id = ""
	}
	face := renderButton(o.Locale.T("dashboard.core.manageAction"), manage)
	// End the link at its chevron so it lines up with the gray button's edge.
	face = ansi.Cut(face, 0, ansi.StringWidth(face)-1)
	return overviewLabelSlot(o, inner, overviewHeading(o, o.Locale.T("dashboard.overview.service")), face, id, true)
}

func coreActionFace(o dashboardOptions, control toggleModel) string {
	start, stop := o.Locale.T("dashboard.settings.start"), o.Locale.T("dashboard.settings.stop")
	options := dashboardButtonOptions("core.toggle", o, false, panel)
	options.Width = max(ansi.StringWidth(start), ansi.StringWidth(stop))
	if o.Busy || !control.canToggle() {
		// Keep keyboard focus visible during a transition. A plain dash occupies
		// the same slot as the button, without advertising a service action.
		options.Kind, options.Busy = buttonText, true
		return renderButton("—", options)
	}
	if control.state == toggleOn {
		return renderButton(stop, options)
	}
	return renderButton(start, options)
}

func (o dashboardOptions) coreUptime() (time.Duration, bool) {
	if o.StateUnavailable || !o.Snapshot.Installed || o.Snapshot.ServiceError != "" || o.CoreAction != "" {
		return 0, false
	}
	return observedCoreUptime(o.Snapshot.Service, o.Now)
}

func observedCoreUptime(status service.Status, now time.Time) (time.Duration, bool) {
	if !status.Active || status.State != "active" || status.PID <= 0 || status.StartedAt.IsZero() {
		return 0, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	elapsed := now.Sub(status.StartedAt)
	return elapsed, elapsed >= 0
}

func coreUptimeLabel(elapsed time.Duration, locale i18n.Localizer) string {
	minutes := int(elapsed / time.Minute)
	hours := minutes / 60
	switch {
	case minutes < 1:
		return locale.T("dashboard.core.uptimeUnderMinute")
	case hours == 0:
		return locale.T("dashboard.core.uptimeMinutes", minutes)
	case hours < 24:
		if minutes%60 == 0 {
			return locale.T("dashboard.core.uptimeHours", hours)
		}
		return locale.T("dashboard.core.uptimeHoursMinutes", hours, minutes%60)
	case hours%24 == 0:
		return locale.T("dashboard.core.uptimeDays", hours/24)
	default:
		return locale.T("dashboard.core.uptimeDaysHours", hours/24, hours%24)
	}
}

func formatCoreUptime(elapsed time.Duration) string {
	seconds := int64(max(0, elapsed) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
