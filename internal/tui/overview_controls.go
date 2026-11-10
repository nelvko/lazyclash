package tui

import (
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type overviewControlButton struct {
	id, label         string
	enabled, selected bool
	primary           bool
}

func (b overviewControlButton) selectable() bool {
	return strings.HasPrefix(b.id, "mode.") || strings.HasPrefix(b.id, "language.") || b.id == "key-hints.toggle"
}

func overviewSelectionWidth(buttons []overviewControlButton) int {
	width := 0
	for _, button := range buttons {
		if button.selectable() {
			width = max(width, 6, ansi.StringWidth(button.label))
		}
	}
	return width
}

func overviewControlPanel(o dashboardOptions, width int, title string, content []string, buttons []overviewControlButton, metadata ...string) dashboardBlock {
	inner, left, top := max(1, width-4), 2, 1
	if width < 4 {
		inner, left, top = max(1, width), 0, 0
	}
	heading := themed(lipgloss.NewStyle().Bold(true).Render(title), text, panel, o.Dim)
	if len(metadata) > 0 && metadata[0] != "" {
		detail := clean(metadata[0])
		gap := inner - ansi.StringWidth(title) - ansi.StringWidth(detail)
		if gap >= 2 {
			heading += themed(strings.Repeat(" ", gap)+detail, muted, panel, o.Dim)
		} else {
			content = append([]string{themed(detail, muted, panel, o.Dim)}, content...)
		}
	}
	lines := strings.Split(wrapText(heading, inner), "\n")
	lines = append(lines, "")
	for _, line := range content {
		lines = append(lines, strings.Split(wrapText(line, inner), "\n")...)
	}
	b := dashboardBlock{}
	if len(buttons) > 0 {
		lines = append(lines, "")
		b.footerY = top + len(lines)
		row, x := "", 0
		selectionWidth := overviewSelectionWidth(buttons)
		for _, button := range buttons {
			options := dashboardButtonOptions(button.id, o, button.primary, panel)
			options.Disabled = !button.enabled
			options.Selected = button.selected
			options.Selectable = button.selectable()
			if options.Selectable {
				options.Width = selectionWidth
			}
			value := renderButton(button.label, options)
			value = cellCrop(value, inner)
			buttonWidth := ansi.StringWidth(value)
			if x > 0 && x+buttonWidth > inner {
				lines = append(lines, row)
				row, x = "", 0
			}
			if button.enabled {
				b.targets = append(b.targets, hitRegion{ID: button.id, X: left + x, Y: top + len(lines), W: buttonWidth, H: 1})
			}
			row += value
			x += buttonWidth
		}
		lines = append(lines, row)
	}
	b.lines = overviewSurface(lines, width, len(lines)+2, o)
	visible := b.targets[:0]
	for _, target := range b.targets {
		if target, ok := clipRegion(target, 0, 0, width, len(b.lines)); ok {
			visible = append(visible, target)
		}
	}
	b.targets = visible
	return b
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
	return overviewControlPanel(o, width, tr("dashboard.overview.environment"), lines, nil)
}
