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
	if !s.Installed && !o.StateUnavailable {
		return overviewControlPanel(o, width, tr("dashboard.overview.service"), []string{name}, []overviewControlButton{
			{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy},
		})
	}
	inner := overviewInnerWidth(width)
	control := overviewCoreToggle(o)
	transition := control.state == togglePending
	running := !o.StateUnavailable && s.Service.Active && s.ServiceError == ""
	version := s.Version
	if running && s.RunningVersion != "" {
		version = s.RunningVersion
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	uptime := ""
	if running && s.Service.State == "active" && !transition && s.Service.PID > 0 && !s.Service.StartedAt.IsZero() && !s.Service.StartedAt.After(now) {
		uptime = tr("dashboard.core.uptime", formatCoreUptime(now.Sub(s.Service.StartedAt)))
	}
	state, _, color := headerCoreState(o)
	if version = clean(version); version != "" {
		name += themed("  "+version, muted, panel, o.Dim)
	}
	content := overviewTextBlock([]string{themed(cellCrop(state, inner), color, panel, o.Dim), name}, inner)
	// A long-running process can add hour digits. Reserve its time row across
	// lifecycle changes, including any extra lines needed on narrow screens.
	timeRows := overviewTextBlock([]string{themed(uptime, muted, panel, o.Dim)}, inner)
	budget := tr("dashboard.core.uptime", formatCoreUptime(time.Duration(1<<63-1)))
	for n := len(strings.Split(wrapText(budget, inner), "\n")); len(timeRows.lines) < n; {
		timeRows.blank()
	}
	content.append(timeRows)
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
	if issue := runtimeStopIssue(s, o.Locale); !o.StateUnavailable && issue != "" {
		details = append(details, style(issue, muted))
	}
	content.append(overviewTextBlock(details, inner))
	heading := overviewLabelSlot(o, inner, overviewHeading(o, tr("dashboard.overview.service")), coreActionFace(o, control), "core.toggle")
	return overviewPanel(o, width, heading, content, nil)
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

func formatCoreUptime(elapsed time.Duration) string {
	seconds := int64(max(0, elapsed) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
