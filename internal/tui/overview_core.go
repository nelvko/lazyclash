package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func overviewCoreCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	state, _, color := headerCoreState(o)
	inner := overviewInnerWidth(width)
	lines := []string{overviewHeading(o, "Mihomo"), style(cellCrop(state, inner), color)}
	if !s.Installed && !o.StateUnavailable {
		return overviewControlPanel(o, width, tr("dashboard.overview.service"), lines, []overviewControlButton{
			{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy},
		})
	}
	control := overviewCoreToggle(o)
	transition := control.state == togglePending
	running := !o.StateUnavailable && s.Service.Active && s.ServiceError == ""
	showCardUptime := !sidebarGeometry(o, false).hasClockRow()
	if showCardUptime {
		if elapsed, ok := o.coreUptime(); ok {
			lines = append(lines, style(coreUptimeLabel(elapsed, o.Locale), muted))
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
	if showCardUptime {
		// Keep the narrow card steady when the clock starts or changes units.
		longestUptime := coreUptimeLabel(time.Duration(1<<63-1), o.Locale)
		minRows := 3 + strings.Count(wrapText(longestUptime, inner), "\n")
		for len(content.lines) < minRows {
			content.blank()
		}
	}
	const id = "core.toggle"
	face := o.switchFace(id, control, toggleOptions{Surface: panel, Focused: o.Focus == id, Hovered: o.Hover == id})
	// Reserve a gutter independently of the switch. In particular, the
	// all-space slider must retain each of its three painted hit cells.
	heading := overviewLabelSlot(o, inner, overviewHeading(o, tr("dashboard.overview.service")), switchGutter(o.Focus == id, o.Hover == id, o.Monochrome, panel, o.Dim)+face, id)
	if len(heading.targets) > 0 && heading.targets[0].W > 2 {
		heading.targets[0].X += 2
		heading.targets[0].W -= 2
	}
	return overviewPanel(o, width, heading, content, nil)
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
