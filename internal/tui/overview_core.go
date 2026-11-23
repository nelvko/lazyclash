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
	if !s.Installed && !o.StateUnavailable {
		return overviewControlPanel(o, width, tr("dashboard.overview.service"), []string{style(state, color)}, []overviewControlButton{
			{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy},
		})
	}
	inner := overviewInnerWidth(width)
	control := overviewCoreToggle(o)
	transition := control.state == togglePending
	running := !o.StateUnavailable && s.Service.Active && s.ServiceError == ""
	lines := []string{style(cellCrop(state, inner), color)}
	if running && !transition {
		if elapsed, ok := observedCoreUptime(s.Service, o.Now); ok {
			lines = append(lines, style(coreStartAge(elapsed, o.Locale), muted))
		}
	}
	if running && s.RunningVersion != "" && s.Version != "" && s.RunningVersion != s.Version {
		lines = append(lines, style(tr("dashboard.kernel.running", clean(s.RunningVersion)), warning),
			style(tr("dashboard.kernel.installed", clean(s.Version)), warning), style(tr("dashboard.core.restartVersion"), warning))
	}
	if running && s.Desired.Known && !s.Desired.Running && !transition {
		lines = append(lines, style(tr("dashboard.core.stopRequested"), warning))
	}
	stopped := s.Service.State == "inactive" || s.Service.State == "failed"
	unexpectedExit := stopped && s.Service.State != "failed" && s.Desired.Known && s.Desired.Running
	if s.Service.State == "failed" && !transition {
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
		if o.Runtime.RunningVersion != "" && (o.Runtime.Traffic != nil || o.Runtime.Memory != nil || o.Runtime.Connections != nil) {
			monitorUnavailable = false
		}
		monitorUnavailable = monitorUnavailable || o.Runtime.Error != "" || o.Runtime.TrafficError != "" || o.Runtime.MemoryError != "" || o.Runtime.ConnectionsError != ""
	}
	if s.ServiceError != "" {
		lines = append(lines, style(cellCrop(o.Locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic), inner), warning))
	} else if running && monitorUnavailable {
		lines = append(lines, style(tr("dashboard.core.monitorUnavailable"), warning))
	} else if control.state == toggleUnknown {
		lines = append(lines, style(tr("dashboard.core.unknownHint"), muted))
	}
	if issue := runtimeStopIssue(s, o.Locale); !o.StateUnavailable && issue != "" {
		lines = append(lines, style(issue, muted))
	}
	content := overviewTextBlock(lines, inner)
	// Reserve the age line after any diagnostics so the card stays steady
	// across startup, shutdown and changes of time unit on narrow screens.
	longestAge := coreStartAge(time.Duration(1<<63-1), o.Locale)
	minRows := 2 + strings.Count(wrapText(longestAge, inner), "\n")
	for len(content.lines) < minRows {
		content.blank()
	}
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

func coreStartAge(elapsed time.Duration, locale i18n.Localizer) string {
	switch {
	case elapsed < time.Minute:
		return locale.T("dashboard.core.startedNow")
	case elapsed < time.Hour:
		return locale.T("dashboard.core.startedMinutes", int(elapsed/time.Minute))
	case elapsed < 24*time.Hour:
		return locale.T("dashboard.core.startedHours", int(elapsed/time.Hour))
	default:
		return locale.T("dashboard.core.startedDays", int(elapsed/(24*time.Hour)))
	}
}

func formatCoreUptime(elapsed time.Duration) string {
	seconds := int64(max(0, elapsed) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
