package tui

import (
	"runtime"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type overviewControlButton struct {
	id, label         string
	enabled, selected bool
}

func overviewControlPanel(o dashboardOptions, width int, title string, content []string, buttons []overviewControlButton) dashboardBlock {
	inner, left, top := max(1, width-4), 2, 1
	if width < 4 {
		inner, left, top = max(1, width), 0, 0
	}
	lines := strings.Split(wrapText(themed(lipgloss.NewStyle().Bold(true).Render(title), text, panel, o.Dim), inner), "\n")
	for _, line := range content {
		lines = append(lines, strings.Split(wrapText(line, inner), "\n")...)
	}
	b := dashboardBlock{}
	if len(buttons) > 0 {
		lines = append(lines, "")
		b.footerY = top + len(lines)
		row, x := "", 0
		for _, button := range buttons {
			label := button.label
			if button.selected {
				label += " •"
			}
			value := themed("[ "+label+" ]", disabled, panel, o.Dim)
			if button.enabled {
				value = dashboardButton(button.id, label, o, button.selected)
			}
			value = cellCrop(value, inner)
			buttonWidth := ansi.StringWidth(value)
			if x > 0 && x+2+buttonWidth > inner {
				lines = append(lines, row)
				row, x = "", 0
			}
			if x > 0 {
				row += themed("  ", text, panel, o.Dim)
				x += 2
			}
			if button.enabled {
				b.targets = append(b.targets, hitRegion{ID: button.id, X: left + x, Y: top + len(lines), W: buttonWidth, H: 1})
			}
			row += value
			x += buttonWidth
		}
		lines = append(lines, row)
	}
	b.lines = panelFrame(lines, width, len(lines)+2, o)
	visible := b.targets[:0]
	for _, target := range b.targets {
		if target, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok {
			visible = append(visible, target)
		}
	}
	b.targets = visible
	return b
}

func overviewControlCard(o dashboardOptions, index, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	if index == 1 {
		return overviewControlPanel(o, width, tr("dashboard.field.tun"), []string{style(tr("dashboard.status.unavailable"), muted), style(tr("dashboard.control.tunHint"), muted)}, []overviewControlButton{{label: tr("dashboard.status.unavailable")}})
	}
	if index == 2 {
		return overviewControlPanel(o, width, tr("dashboard.field.systemProxy"), []string{style(tr("dashboard.control.unmanaged"), muted), style(tr("dashboard.control.systemProxyHint"), muted)}, []overviewControlButton{{label: tr("dashboard.status.unavailable")}})
	}
	status, color := coreSummary(s, o.Locale)
	lines := []string{style(status, color)}
	if !s.Installed {
		return overviewControlPanel(o, width, tr("dashboard.control.core"), lines, []overviewControlButton{{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy}})
	}
	if !s.Service.Active && !s.HasRun && s.ConfigReady && (s.Applied == "default" || s.Applied == "") {
		lines = append(lines, style(tr("dashboard.profile.builtinReady"), muted))
	}
	issue := runtimeControlIssue(s, o.Locale)
	var buttons []overviewControlButton
	if s.Service.Active {
		selected := s.ServiceError == "" && s.Service.State != "unknown" && s.Service.State != "unsupported"
		label := tr("dashboard.control.on")
		if s.ServiceError != "" || s.Service.State == "unknown" || s.Service.State == "unsupported" {
			label = tr("dashboard.action.stop")
		}
		buttons = append(buttons, overviewControlButton{id: "core.stop", label: label, enabled: !o.Busy && runtimeStopIssue(s, o.Locale) == "", selected: selected})
	} else {
		buttons = append(buttons, overviewControlButton{id: "core.start", label: tr("dashboard.action.start"), enabled: !o.Busy && issue == ""})
		if s.Desired.Running {
			buttons = append(buttons, overviewControlButton{id: "core.stop", label: tr("dashboard.action.stop"), enabled: !o.Busy && runtimeStopIssue(s, o.Locale) == ""})
		}
	}
	buttons = append(buttons, overviewControlButton{id: "core.restart", label: tr("dashboard.action.restart"), enabled: !o.Busy && issue == ""})
	if issue != "" {
		lines = append(lines, style(issue, muted))
	}
	if o.Busy {
		lines = append(lines, style(tr("dashboard.control.busy"), muted))
	}
	return overviewControlPanel(o, width, tr("dashboard.control.core"), lines, buttons)
}

func overviewModeCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	modeNames := map[string]string{"rule": tr("dashboard.mode.rule"), "global": tr("dashboard.mode.global"), "direct": tr("dashboard.mode.direct")}
	saved := modeNames[s.Mode.Desired]
	if saved == "" {
		saved = tr("dashboard.status.unknown")
	}
	lines := []string{themed(tr("dashboard.control.savedMode", saved), text, panel, o.Dim)}
	if s.Service.Active {
		actual := modeNames[s.Mode.Actual]
		if actual == "" || s.Mode.Error != "" || s.ServiceError != "" {
			actual = tr("dashboard.status.unknown")
		}
		lines = append(lines, themed(tr("dashboard.control.runningMode", actual), muted, panel, o.Dim))
	}
	if s.Mode.Error != "" {
		lines = append(lines, themed(clean(o.Locale.Diagnostic(s.Mode.Error, s.Mode.ErrorDiagnostic)), warning, panel, o.Dim))
	}
	issue := runtimeControlIssue(s, o.Locale)
	if issue != "" {
		lines = append(lines, themed(issue, muted, panel, o.Dim))
	}
	var buttons []overviewControlButton
	for _, mode := range []string{"rule", "global", "direct"} {
		buttons = append(buttons, overviewControlButton{id: "mode." + mode, label: modeNames[mode], enabled: s.Installed && !o.Busy && issue == "", selected: s.Mode.Desired == mode})
	}
	return overviewControlPanel(o, width, tr("dashboard.control.routingMode"), lines, buttons)
}

func overviewSystemCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	scope := clean(s.Capabilities.Scope)
	if scope == "" || scope == "user" {
		scope = tr("dashboard.system.user")
	}
	manager, autostart := tr("dashboard.status.unavailable"), tr("dashboard.status.unknown")
	if s.Capabilities.Runtime.Supported {
		manager = tr("dashboard.system.manager")
		if s.ServiceError == "" {
			switch s.Service.EnableState {
			case "enabled":
				autostart = tr("dashboard.status.enabled")
			case "disabled":
				autostart = tr("dashboard.status.disabled")
			default:
				if s.Service.EnableState != "" {
					autostart = clean(s.Service.EnableState)
				}
			}
		}
	}
	lines := []string{
		themed(tr("dashboard.system.os", runtime.GOOS), text, panel, o.Dim),
		themed(tr("dashboard.system.architecture", runtime.GOARCH), text, panel, o.Dim),
		themed(tr("dashboard.system.scope", scope), muted, panel, o.Dim),
		themed(tr("dashboard.system.serviceManager", manager), muted, panel, o.Dim),
		themed(tr("dashboard.system.autostart", autostart), muted, panel, o.Dim),
	}
	return overviewControlPanel(o, width, tr("dashboard.system.title"), lines, nil)
}

func overviewKernelCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	status, color := coreSummary(s, o.Locale)
	lines := []string{themed(status, color, panel, o.Dim)}
	if s.Installed {
		version := clean(s.Version)
		if version == "" {
			version = tr("dashboard.status.unknown")
		}
		lines = append(lines, themed(tr("dashboard.kernel.installed", version), text, panel, o.Dim))
		if s.Service.Active {
			running := clean(s.RunningVersion)
			if running == "" || s.ServiceError != "" {
				running = tr("dashboard.status.unavailable")
			}
			lines = append(lines, themed(tr("dashboard.kernel.running", running), text, panel, o.Dim))
			if s.Service.PID > 0 && s.ServiceError == "" {
				lines = append(lines, themed(tr("dashboard.kernel.pid", strconv.Itoa(s.Service.PID)), muted, panel, o.Dim))
			}
		}
		if s.Desired.Known {
			desired := tr("dashboard.status.stopped")
			if s.Desired.Running {
				desired = tr("dashboard.status.running")
			}
			lines = append(lines, themed(tr("dashboard.kernel.desired", desired), muted, panel, o.Dim))
		}
		if s.Drift != "" {
			lines = append(lines, themed(clean(o.Locale.Detail(s.Drift)), warning, panel, o.Dim))
		}
	}
	label := tr("dashboard.action.install")
	if s.Installed {
		label = tr("dashboard.action.manage")
	}
	return overviewControlPanel(o, width, tr("dashboard.kernel.title"), lines, []overviewControlButton{{id: "card.install", label: label, enabled: !o.Busy}})
}
