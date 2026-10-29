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
	s := o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	if index == 1 {
		return overviewControlPanel(o, width, "TUN", []string{style("Unavailable", muted), style("Not supported by the user service.", muted)}, []overviewControlButton{{label: "Unavailable"}})
	}
	if index == 2 {
		return overviewControlPanel(o, width, "System proxy", []string{style("Unmanaged", muted), style("System proxy control is not available.", muted)}, []overviewControlButton{{label: "Unavailable"}})
	}
	status, color := coreSummary(s)
	lines := []string{style(status, color)}
	if !s.Installed {
		return overviewControlPanel(o, width, "Core", lines, []overviewControlButton{{id: "core.install", label: "Install core", enabled: !o.Busy}})
	}
	issue := runtimeControlIssue(s)
	var buttons []overviewControlButton
	if s.Service.Active {
		label := "On"
		if s.ServiceError != "" || s.Service.State == "unknown" || s.Service.State == "unsupported" {
			label = "Stop core"
		}
		buttons = append(buttons, overviewControlButton{id: "core.stop", label: label, enabled: !o.Busy && runtimeStopIssue(s) == "", selected: label == "On"})
	} else {
		label := "Off"
		if issue != "" || status == "Unavailable" {
			label = "Start core"
		}
		buttons = append(buttons, overviewControlButton{id: "core.start", label: label, enabled: !o.Busy && issue == ""})
		if s.Desired.Running {
			buttons = append(buttons, overviewControlButton{id: "core.stop", label: "Stop core", enabled: !o.Busy && runtimeStopIssue(s) == ""})
		}
	}
	buttons = append(buttons, overviewControlButton{id: "core.restart", label: "Restart", enabled: !o.Busy && issue == ""})
	if issue != "" {
		lines = append(lines, style(issue, muted))
	}
	if o.Busy {
		lines = append(lines, style("Operation in progress…", muted))
	}
	return overviewControlPanel(o, width, "Core", lines, buttons)
}

func overviewModeCard(o dashboardOptions, width int) dashboardBlock {
	s := o.Snapshot
	modeNames := map[string]string{"rule": "Rule", "global": "Global", "direct": "Direct"}
	saved := modeNames[s.Mode.Desired]
	if saved == "" {
		saved = "Unknown"
	}
	lines := []string{themed("Saved mode: "+saved, text, panel, o.Dim)}
	if s.Service.Active {
		actual := modeNames[s.Mode.Actual]
		if actual == "" || s.Mode.Error != "" || s.ServiceError != "" {
			actual = "Unknown"
		}
		lines = append(lines, themed("Running mode: "+actual, muted, panel, o.Dim))
	}
	if s.Mode.Error != "" {
		lines = append(lines, themed(clean(s.Mode.Error), warning, panel, o.Dim))
	}
	issue := runtimeControlIssue(s)
	if issue != "" {
		lines = append(lines, themed(issue, muted, panel, o.Dim))
	}
	var buttons []overviewControlButton
	for _, mode := range []string{"rule", "global", "direct"} {
		buttons = append(buttons, overviewControlButton{id: "mode." + mode, label: modeNames[mode], enabled: s.Installed && !o.Busy && issue == "", selected: s.Mode.Desired == mode})
	}
	return overviewControlPanel(o, width, "Routing mode", lines, buttons)
}

func overviewSystemCard(o dashboardOptions, width int) dashboardBlock {
	s := o.Snapshot
	scope := clean(s.Capabilities.Scope)
	if scope == "" {
		scope = "user"
	}
	manager, autostart := "Unavailable", "Unknown"
	if s.Capabilities.Runtime.Supported {
		manager = "systemd user"
		if s.ServiceError == "" {
			switch s.Service.EnableState {
			case "enabled":
				autostart = "Enabled"
			case "disabled":
				autostart = "Disabled"
			default:
				if s.Service.EnableState != "" {
					autostart = clean(s.Service.EnableState)
				}
			}
		}
	}
	lines := []string{
		themed("OS: "+runtime.GOOS, text, panel, o.Dim),
		themed("Architecture: "+runtime.GOARCH, text, panel, o.Dim),
		themed("Scope: "+scope, muted, panel, o.Dim),
		themed("Service manager: "+manager, muted, panel, o.Dim),
		themed("Autostart: "+autostart, muted, panel, o.Dim),
	}
	return overviewControlPanel(o, width, "System information", lines, nil)
}

func overviewKernelCard(o dashboardOptions, width int) dashboardBlock {
	s := o.Snapshot
	status, color := coreSummary(s)
	lines := []string{themed(status, color, panel, o.Dim)}
	if s.Installed {
		version := clean(s.Version)
		if version == "" {
			version = "Unknown"
		}
		lines = append(lines, themed("Installed version: "+version, text, panel, o.Dim))
		if s.Service.Active {
			running := clean(s.RunningVersion)
			if running == "" || s.ServiceError != "" {
				running = "Unavailable"
			}
			lines = append(lines, themed("Running version: "+running, text, panel, o.Dim))
			if s.Service.PID > 0 && s.ServiceError == "" {
				lines = append(lines, themed("Process ID: "+strconv.Itoa(s.Service.PID), muted, panel, o.Dim))
			}
		}
		if s.Desired.Known {
			desired := "Stopped"
			if s.Desired.Running {
				desired = "Running"
			}
			lines = append(lines, themed("Desired state: "+desired, muted, panel, o.Dim))
		}
		if s.Drift != "" {
			lines = append(lines, themed(clean(s.Drift), warning, panel, o.Dim))
		}
	}
	label := "Install core"
	if s.Installed {
		label = "Manage core"
	}
	return overviewControlPanel(o, width, "Kernel information", lines, []overviewControlButton{{id: "card.install", label: label, enabled: !o.Busy}})
}
