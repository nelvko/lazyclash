package tui

import (
	"cmp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/buildinfo"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// Settings reuses the dashboard's scrolling, focus and button components.
// Bubbles has no settings form or switch component; these rows only lay out
// existing actions, without owning state or changing their activation paths.
type settingsRow struct {
	label, value, note string
	color              string
	actions            []overviewControlButton
}

var languageMenuItems = []menuItem{
	{ID: i18n.English, Label: "English"},
	{ID: i18n.Chinese, Label: "简体中文"},
}

var themeMenuItems = []menuItem{
	{ID: app.ThemeLight, Label: "Light"},
	{ID: app.ThemeDark, Label: "Dark"},
	{ID: app.ThemeAuto, Label: "Auto"},
}

func menuItemLabel(items []menuItem, id string) string {
	for _, item := range items {
		if item.ID == id {
			return item.Label
		}
	}
	return id
}

func settingsSelectRow(o dashboardOptions, id, label, current string, items []menuItem) settingsRow {
	return settingsRow{label: label, actions: []overviewControlButton{
		{id: id, label: menuItemLabel(items, current) + " ▾", enabled: !o.Busy},
	}}
}

func settingsContent(o dashboardOptions, width int) (string, []hitRegion) {
	sections := []func(dashboardOptions, int) dashboardBlock{
		settingsInterface, settingsService, settingsCore, settingsDiagnostics,
	}
	var body dashboardBlock
	columns := 1
	if width >= 80 {
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
		settingsSelectRow(o, "settings.language", tr("dashboard.language.title"), o.Locale.Language(), languageMenuItems),
		{label: tr("dashboard.interface.keyHints"), note: tr("settings.keyHintsHint"), actions: []overviewControlButton{
			{id: "key-hints.toggle", label: state, enabled: true, selected: !o.HideKeyHints},
		}},
		settingsThemeRow(o),
		{label: tr("settings.editor"), actions: []overviewControlButton{{id: "editor.configure", label: tr("settings.edit"), enabled: !o.Busy}}},
	})
}

func settingsThemeRow(o dashboardOptions) settingsRow {
	theme := cmp.Or(o.Theme, app.ThemeDark)
	row := settingsSelectRow(o, "settings.theme", o.Locale.T("settings.theme"), theme, themeMenuItems)
	if theme == app.ThemeAuto {
		row.note = o.Locale.T("settings.themeAuto", menuItemLabel(themeMenuItems, o.EffectiveTheme))
	}
	if theme == app.ThemeLight || theme == app.ThemeAuto && o.EffectiveTheme == app.ThemeLight {
		row.note = strings.TrimSpace(row.note + " " + o.Locale.T("settings.themeLightPlaceholder"))
	}
	return row
}

func settingsService(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
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
	ports := settingsRow{label: tr("settings.ports"), value: tr("settings.afterInstall")}
	if s.Installed {
		ports.value = ""
		ports.note = savedMixedPortLabel(o) + "\n" + overviewControllerAddress(s.Controller)
		ports.actions = []overviewControlButton{{id: "runtime-settings", label: tr("settings.edit"), enabled: !o.Busy}}
	}
	return settingsSection(o, width, tr("settings.serviceGroup"), []settingsRow{autostart, ports})
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
	rows := []settingsRow{core}
	if s.Installed {
		rows[0].actions = append(rows[0].actions, overviewControlButton{id: "restart", label: tr("dashboard.action.restart"), enabled: !o.Busy && runtimeControlIssue(s, o.Locale) == ""})
		rows = append(rows, settingsRow{label: tr("settings.remove"), note: tr("settings.removeHint")})
	}
	return settingsSection(o, width, tr("settings.core"), rows)
}

func settingsDiagnostics(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	rows := []settingsRow{
		{label: tr("settings.clientVersion"), value: buildinfo.Version},
		{label: tr("settings.logs"), actions: []overviewControlButton{{id: "card.logs", label: tr("settings.openLogs"), enabled: !o.Busy}}},
		{label: tr("settings.doctor"), value: "lazyclash doctor"},
	}
	if s.Service.Result != "" && s.Service.Result != "success" {
		rows = append(rows, settingsRow{note: tr("dashboard.settings.serviceResult", clean(s.Service.Result), s.Service.ExitCode), color: warning})
	}
	if s.ServiceError != "" {
		rows = append(rows, settingsRow{note: clean(o.Locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic)), color: errorColor})
	}
	if s.Drift != "" {
		rows = append(rows, settingsRow{note: clean(o.Locale.Detail(s.Drift)), color: warning})
	}
	return settingsSection(o, width, tr("settings.diagnostics"), rows)
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
	labelColumn := 0
	for _, row := range rows {
		labelColumn = max(labelColumn, ansi.StringWidth(row.label))
	}
	labelColumn = min(28, labelColumn)
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
		// A shared label column keeps values close to their names on wide cards.
		// Long labels, values and action groups flow below instead of truncating.
		if row.label != "" || row.value != "" {
			if row.label != "" && row.value != "" && ansi.StringWidth(label) <= labelColumn && labelColumn+2+ansi.StringWidth(value) <= inner {
				b.lines = append(b.lines, label+style(strings.Repeat(" ", labelColumn+2-ansi.StringWidth(label)), text)+value)
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
			selectionWidth := overviewSelectionWidth(row.actions)
			for _, action := range row.actions {
				options := dashboardButtonOptions(action.id, o, action.primary, panel)
				options.Disabled, options.Selected = !action.enabled, action.selected
				options.Selectable = action.selectable() || strings.HasPrefix(action.id, "autostart-")
				if options.Selectable {
					options.Width = max(6, selectionWidth)
				}
				value := cellCrop(renderButton(action.label, options), inner)
				values = append(values, value)
				total += ansi.StringWidth(value)
			}
			x := 0
			if row.value != "" && row.label != "" && ansi.StringWidth(label) <= labelColumn && labelColumn+ansi.StringWidth(value)+3+total <= inner {
				x = labelColumn + ansi.StringWidth(value) + 3
				gap := labelColumn + 2 - ansi.StringWidth(label)
				b.lines[len(b.lines)-1] = label + style(strings.Repeat(" ", gap), text) + value + style(" ", text)
			} else if row.value == "" && row.label != "" && ansi.StringWidth(label) <= labelColumn && labelColumn+2+total <= inner {
				x = labelColumn + 2
				b.lines[len(b.lines)-1] = label + style(strings.Repeat(" ", x-ansi.StringWidth(label)), text)
			} else {
				b.blank()
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
			}
		}
		if row.note != "" {
			addText(row.note, color)
		}
	}
	b.lines = panelFrame(b.lines, width, len(b.lines)+2, o)
	return b
}
