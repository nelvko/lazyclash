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
	primary           bool
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
			options := dashboardButtonOptions(button.id, o, button.primary, panel)
			options.Disabled = !button.enabled
			options.Selected = button.selected
			options.Selectable = strings.HasPrefix(button.id, "mode.") || strings.HasPrefix(button.id, "language.") || button.id == "key-hints.toggle"
			value := renderButton(button.label, options)
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

func overviewCoreCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	status, color := coreSummary(s, o.Locale)
	lines := []string{lipgloss.NewStyle().Bold(true).Render(style("● "+status, color))}
	if !s.Installed {
		return overviewControlPanel(o, width, tr("dashboard.overview.service"), lines, []overviewControlButton{{id: "core.install", label: tr("dashboard.action.install"), enabled: !o.Busy}})
	}
	installed := overviewVersion(s.Version, "dashboard.status.unknown", o.Locale)
	if s.Service.Active && s.ServiceError == "" && s.RunningVersion != "" && s.RunningVersion == s.Version {
		lines = append(lines, style(tr("dashboard.overview.versionAligned", installed), muted))
	} else {
		lines = append(lines, style(tr("dashboard.kernel.installed", installed), muted))
		if s.Service.Active {
			running := overviewVersion(s.RunningVersion, "dashboard.status.unavailable", o.Locale)
			if s.ServiceError != "" {
				running = tr("dashboard.status.unavailable")
			}
			lines = append(lines, style(tr("dashboard.kernel.running", running), warning))
		}
	}
	var details []string
	if s.Service.Active && s.ServiceError == "" && s.Service.PID > 0 {
		details = append(details, tr("dashboard.kernel.pid", strconv.Itoa(s.Service.PID)))
	}
	if s.Desired.Known {
		desired := tr("dashboard.status.stopped")
		if s.Desired.Running {
			desired = tr("dashboard.status.running")
		}
		details = append(details, tr("dashboard.kernel.desired", desired))
	}
	if len(details) > 0 {
		lines = append(lines, style(strings.Join(details, " · "), muted))
	}
	if s.Drift != "" {
		lines = append(lines, style(o.Locale.Detail(s.Drift), warning))
	}
	if !s.Service.Active && !s.HasRun && s.ConfigReady && (s.Applied == "default" || s.Applied == "") {
		lines = append(lines, style(tr("dashboard.profile.builtinReady"), muted))
	}
	issue := runtimeControlIssue(s, o.Locale)
	var buttons []overviewControlButton
	if s.Service.Active {
		buttons = append(buttons, overviewControlButton{id: "core.stop", label: tr("dashboard.action.stop"), enabled: !o.Busy && runtimeStopIssue(s, o.Locale) == ""})
	} else {
		buttons = append(buttons, overviewControlButton{id: "core.start", label: tr("dashboard.action.start"), enabled: !o.Busy && issue == "", primary: true})
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
	buttons = append(buttons, overviewControlButton{id: "card.install", label: tr("dashboard.action.manage"), enabled: !o.Busy})
	return overviewControlPanel(o, width, tr("dashboard.overview.service"), lines, buttons)
}

func overviewModeCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	if !s.Installed {
		return overviewControlPanel(o, width, tr("dashboard.overview.route"), []string{
			themed(tr("dashboard.control.modeAfterInstall"), muted, panel, o.Dim),
		}, nil)
	}
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
		if modeNames[s.Mode.Actual] != "" && s.Mode.Actual == s.Mode.Desired && s.Mode.Error == "" && s.ServiceError == "" {
			lines = []string{themed(tr("dashboard.overview.modeAligned", actual), muted, panel, o.Dim)}
		} else {
			lines = append(lines, themed(tr("dashboard.control.runningMode", actual), warning, panel, o.Dim))
		}
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
	return overviewControlPanel(o, width, tr("dashboard.overview.route"), lines, buttons)
}

func overviewSystemCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	scope := clean(s.Capabilities.Scope)
	if scope == "" || scope == "user" {
		scope = tr("dashboard.system.user")
	} else if scope == "system" {
		scope = tr("dashboard.system.system")
	}
	manager, autostart := tr("dashboard.status.unavailable"), tr("dashboard.status.unknown")
	if s.Capabilities.Runtime.Supported {
		manager = tr("dashboard.system.manager")
		if s.Capabilities.Scope == "system" {
			manager = tr("dashboard.system.systemManager")
		}
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
		themed(runtime.GOOS+" / "+runtime.GOARCH+" · "+scope+" · "+manager, muted, panel, o.Dim),
		themed(tr("dashboard.system.autostart", autostart), text, panel, o.Dim),
	}
	var buttons []overviewControlButton
	tunHint := tr("dashboard.control.tunHint")
	if s.Capabilities.TUN.Supported {
		state, id, label := tr("dashboard.control.off"), "tun.enable", tr("tun.enable")
		if s.TUN {
			state, id, label = tr("dashboard.control.on"), "tun.disable", tr("tun.disable")
		}
		tunHint = tr("tun.saved_state", state) + " · " + tr("tun.host_scope")
		buttons = append(buttons, overviewControlButton{id: id, label: label, enabled: !o.Busy && runtimeControlIssue(s, o.Locale) == ""})
	}
	lines = append(lines, themed(tr("dashboard.field.tun")+" · "+tunHint, muted, panel, o.Dim))
	if s.TUNError != "" {
		lines = append(lines, themed(clean(o.Locale.Detail(s.TUNError)), warning, panel, o.Dim))
	}
	lines = append(lines, themed(tr("dashboard.overview.systemProxy"), muted, panel, o.Dim))
	return overviewControlPanel(o, width, tr("dashboard.overview.environment"), lines, buttons)
}
