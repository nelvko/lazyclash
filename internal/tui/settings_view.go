package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Settings reuses the dashboard's scrolling, focus and button components.
// Bubbles has no settings form or switch component; these rows only lay out
// existing actions, without owning state or changing their activation paths.
type settingsRow struct {
	label, value, note string
	color              string
	actions            []overviewControlButton
}

func settingsContent(o dashboardOptions, width int) (string, []hitRegion) {
	sections := []func(dashboardOptions, int) dashboardBlock{
		settingsInterface, settingsService, settingsNetwork, settingsCore,
	}
	var body dashboardBlock
	columns := 1
	if width >= 80 && o.Height >= 20 {
		columns = 2
	}
	for i := 0; i < len(sections); i += columns {
		if i > 0 {
			body.blank()
		}
		body.append(overviewRow(o, width, columns, func(column, cardWidth int) dashboardBlock {
			return sections[i+column](o, cardWidth)
		}))
	}
	return strings.Join(body.lines, "\n"), body.targets
}

func settingsInterface(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	state := tr("dashboard.control.on")
	if o.HideKeyHints {
		state = tr("dashboard.control.off")
	}
	return settingsSection(o, width, tr("settings.interface"), []settingsRow{
		{label: tr("dashboard.language.title"), note: tr("settings.languageHint"), actions: []overviewControlButton{
			{id: "language.en", label: "English", enabled: !o.Busy, selected: o.Locale.Language() == "en"},
			{id: "language.zh-Hans", label: "简体中文", enabled: !o.Busy, selected: o.Locale.Language() == "zh-Hans"},
		}},
		{label: tr("dashboard.interface.keyHints"), note: tr("settings.keyHintsHint"), actions: []overviewControlButton{
			{id: "key-hints.toggle", label: state, enabled: true, selected: !o.HideKeyHints},
		}},
		{label: tr("settings.theme"), value: themeName},
	})
}

func settingsService(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	status, color := coreSummary(s, o.Locale)
	service := settingsRow{label: tr("settings.service"), value: status, color: color}
	if s.Installed {
		if !s.Service.Active {
			service.actions = append(service.actions, overviewControlButton{id: "start", label: tr("dashboard.settings.start"), enabled: !o.Busy})
		}
		if s.Service.Active || s.Desired.Running {
			service.actions = append(service.actions, overviewControlButton{id: "stop", label: tr("dashboard.settings.stop"), enabled: !o.Busy})
		}
		service.actions = append(service.actions, overviewControlButton{id: "restart", label: tr("dashboard.action.restart"), enabled: !o.Busy})
	} else {
		service.note = tr("settings.afterInstall")
	}
	rows := []settingsRow{service}
	autostart := settingsRow{label: tr("settings.autostart"), value: tr("settings.afterInstall")}
	if s.Installed {
		id, state := "autostart-enable", tr("dashboard.control.off")
		if s.Service.Enabled {
			id, state = "autostart-disable", tr("dashboard.control.on")
		}
		autostart.value = ""
		autostart.actions = []overviewControlButton{{id: id, label: state, enabled: !o.Busy, selected: s.Service.Enabled}}
		if s.ServiceError != "" || s.Service.EnableState != "" && s.Service.EnableState != "enabled" && s.Service.EnableState != "disabled" {
			autostart.value = tr("dashboard.status.unknown")
			autostart.actions[0].label = tr("dashboard.settings.enableAutostart")
			if s.Service.Enabled {
				autostart.actions[0].label = tr("dashboard.settings.disableAutostart")
			}
		}
		if !s.Service.Linger && s.Capabilities.Scope != "system" {
			autostart.note = tr("dashboard.settings.lingerDisabled")
			if !s.Service.LingerKnown {
				autostart.note = tr("dashboard.settings.lingerUnknown")
			}
		}
	}
	rows = append(rows, autostart)
	if s.Service.Result != "" && s.Service.Result != "success" {
		rows = append(rows, settingsRow{note: tr("dashboard.settings.serviceResult", clean(s.Service.Result), s.Service.ExitCode), color: warning})
	}
	if s.ServiceError != "" {
		rows = append(rows, settingsRow{note: clean(o.Locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic)), color: errorColor})
	}
	return settingsSection(o, width, tr("settings.serviceGroup"), rows)
}

func settingsNetwork(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	mode := settingsRow{label: tr("dashboard.control.routingMode")}
	if s.Installed {
		for _, name := range []string{"rule", "global", "direct"} {
			mode.actions = append(mode.actions, overviewControlButton{id: "mode." + name, label: dashboardModeLabel(name, o.Locale), enabled: !o.Busy, selected: s.Mode.Desired == name})
		}
		if s.Mode.Actual != "" && s.Mode.Actual != s.Mode.Desired {
			mode.note = tr("dashboard.settings.actualMode", dashboardModeLabel(s.Mode.Actual, o.Locale))
		}
		if s.Service.Active && (s.Mode.Actual == "" || s.Mode.Error != "" || s.APIError != "" || s.ServiceError != "") {
			mode.note = tr("dashboard.settings.actualMode", tr("dashboard.status.unknown"))
		}
	} else {
		mode.value = tr("settings.afterInstall")
	}
	ports := settingsRow{label: tr("settings.ports"), value: tr("settings.afterInstall")}
	if s.Installed {
		ports.value = ""
		ports.note = clean(s.Controller)
		ports.actions = []overviewControlButton{{id: "runtime-settings", label: tr("settings.edit"), enabled: !o.Busy}}
	}
	tun := settingsRow{label: tr("dashboard.field.tun"), value: tr("settings.unsupported"), note: tr("dashboard.control.tunHint")}
	if s.Capabilities.TUN.Supported {
		tun.value, tun.note = tr("settings.afterInstall"), tr("tun.host_scope")
		if s.Installed {
			id, state := "tun.enable", tr("dashboard.control.off")
			if s.TUN {
				id, state = "tun.disable", tr("dashboard.control.on")
			}
			tun.value = ""
			tun.actions = []overviewControlButton{{id: id, label: state, enabled: !o.Busy && runtimeControlIssue(s, o.Locale) == "", selected: s.TUN}}
		}
	}
	rows := []settingsRow{mode, ports, tun}
	for _, issue := range []string{o.Locale.Diagnostic(s.Mode.Error, s.Mode.ErrorDiagnostic), o.Locale.Detail(s.TUNError)} {
		if issue != "" {
			rows = append(rows, settingsRow{note: clean(issue), color: warning})
		}
	}
	// Logical keyboard targets are built before the rendered header is attached.
	// The Settings header always owns this action, so both paths must omit it.
	if o.Page != "settings" && !o.Header.owns("network") {
		rows = append(rows, settingsRow{label: tr("header.network"), actions: []overviewControlButton{{id: "network", label: tr("settings.edit"), enabled: !o.Busy}}})
	}
	return settingsSection(o, width, tr("settings.network"), rows)
}

func settingsCore(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	version, action := tr("dashboard.status.notInstalled"), tr("dashboard.action.install")
	if s.Installed {
		version, action = clean(s.Version), tr("settings.manage")
		if version == "" {
			version = tr("dashboard.status.unknown")
		}
	}
	core := settingsRow{label: "Mihomo", value: version, actions: []overviewControlButton{{id: "install", label: action, enabled: !o.Busy, primary: !s.Installed}}}
	if s.RunningVersion != "" && s.RunningVersion != s.Version {
		core.note = tr("dashboard.settings.running", clean(s.RunningVersion))
	}
	rows := []settingsRow{
		core,
		{label: tr("settings.profiles"), note: tr("settings.importHint"), actions: []overviewControlButton{{id: "import", label: tr("settings.import"), enabled: !o.Busy}}},
	}
	if s.Installed {
		rows = append(rows, settingsRow{label: tr("settings.remove"), note: tr("dashboard.settings.removeHint")})
	}
	if s.Drift != "" {
		rows = append(rows, settingsRow{note: clean(o.Locale.Detail(s.Drift)), color: warning})
	}
	return settingsSection(o, width, tr("dashboard.settings.title"), rows)
}

func settingsSection(o dashboardOptions, width int, title string, rows []settingsRow) dashboardBlock {
	inner, left, top := max(1, width-4), 2, 1
	if width < 4 {
		inner, left, top = max(1, width), 0, 0
	}
	style := func(s, color string) string { return themed(s, color, panel, o.Dim) }
	var b dashboardBlock
	addText := func(s, color string) {
		b.lines = append(b.lines, strings.Split(wrapText(style(s, color), inner), "\n")...)
	}
	addText(lipgloss.NewStyle().Bold(true).Render(title), text)
	b.blank()
	for index, row := range rows {
		if index > 0 {
			b.lines = append(b.lines, style(strings.Repeat("─", inner), divider))
		}
		color := row.color
		if color == "" {
			color = muted
		}
		label := style(row.label, text)
		value := style(row.value, color)
		// Short values share the label row. Long values and action groups flow
		// beneath it instead of truncating their names or hit regions.
		if row.label != "" || row.value != "" {
			if row.label != "" && row.value != "" && ansi.StringWidth(label)+ansi.StringWidth(value)+2 <= inner {
				b.lines = append(b.lines, label+style(strings.Repeat(" ", inner-ansi.StringWidth(label)-ansi.StringWidth(value)), text)+value)
			} else {
				if row.label != "" {
					addText(row.label, text)
				}
				if row.value != "" {
					addText(row.value, color)
				}
			}
		}
		if len(row.actions) > 0 {
			var values []string
			total := 0
			for _, action := range row.actions {
				options := dashboardButtonOptions(action.id, o, action.primary, panel)
				options.Disabled, options.Selected = !action.enabled, action.selected
				options.Selectable = strings.HasPrefix(action.id, "language.") || strings.HasPrefix(action.id, "mode.") || action.id == "key-hints.toggle" || strings.HasPrefix(action.id, "autostart-") || strings.HasPrefix(action.id, "tun.")
				value := cellCrop(renderButton(action.label, options), inner)
				values = append(values, value)
				total += ansi.StringWidth(value)
			}
			total += len(values) - 1
			x := 0
			if row.value != "" && row.label != "" && ansi.StringWidth(label)+ansi.StringWidth(value)+3+total <= inner {
				x = inner - total
				gap := x - ansi.StringWidth(label) - ansi.StringWidth(value) - 1
				b.lines[len(b.lines)-1] = label + style(strings.Repeat(" ", gap), text) + value + style(" ", text)
			} else if row.value == "" && row.label != "" && ansi.StringWidth(label)+2+total <= inner {
				x = inner - total
				b.lines[len(b.lines)-1] = label + style(strings.Repeat(" ", x-ansi.StringWidth(label)), text)
			} else {
				b.blank()
				if total <= inner {
					x = inner - total
					b.lines[len(b.lines)-1] = style(strings.Repeat(" ", x), text)
				}
			}
			for i, action := range row.actions {
				w := ansi.StringWidth(values[i])
				if x+w > inner {
					b.blank()
					x = 0
				}
				if action.enabled {
					b.targets = append(b.targets, hitRegion{ID: action.id, X: left + x, Y: top + len(b.lines) - 1, W: w, H: 1})
				}
				b.lines[len(b.lines)-1] += values[i]
				x += w
				if i < len(row.actions)-1 && x < inner {
					b.lines[len(b.lines)-1] += style(" ", text)
					x++
				}
			}
		}
		if row.note != "" {
			addText(row.note, color)
		}
	}
	b.lines = panelFrame(b.lines, width, len(b.lines)+2, o)
	return b
}
