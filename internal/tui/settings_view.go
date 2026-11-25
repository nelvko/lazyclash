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

type settingsControl uint8

const (
	settingsInline settingsControl = iota
	settingsTrailing
	settingsSelect
	settingsSwitch
)

type settingsRow struct {
	label, value, note string
	color              string
	actions            []overviewControlButton
	control            settingsControl
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

func (o dashboardOptions) settingsDropdown(id string) dropdown {
	switch id {
	case "settings.language":
		return dropdown{Items: languageMenuItems, Value: o.Locale.Language()}
	case "settings.theme":
		return dropdown{Items: themeMenuItems, Value: cmp.Or(o.Theme, app.ThemeDark)}
	case "settings.logLevel":
		return dropdown{Items: coreLogLevelItems, Value: o.Snapshot.CoreSettings.LogLevel}
	case "settings.findProcessMode":
		return dropdown{Value: o.Snapshot.CoreSettings.FindProcessMode, Items: []menuItem{
			{ID: "strict", Label: o.Locale.T("settings.processAsNeeded")},
			{ID: "always", Label: o.Locale.T("settings.processAlways")},
			{ID: "off", Label: o.Locale.T("settings.processOff")},
		}}
	}
	return dropdown{}
}

func settingsSelectRow(o dashboardOptions, id, label string) settingsRow {
	return settingsRow{label: label, control: settingsSelect, actions: []overviewControlButton{
		{id: id, enabled: !o.Busy},
	}}
}

func settingsSwitchRow(id, label string, on, enabled bool) settingsRow {
	return settingsRow{label: label, control: settingsSwitch, actions: []overviewControlButton{
		{id: id, selected: on, enabled: enabled},
	}}
}

// Use the same six terminal cells in all color modes. Shading the rail glyph
// and its background identically makes a solid track in full color.
func renderSettingsSwitch(action overviewControlButton, o buttonOptions) string {
	railColor, thumbColor := divider, disabled
	if action.selected {
		railColor, thumbColor = "#456652", "#addbb0"
	}
	marker := "  "
	if o.Focused && action.enabled {
		marker = "› "
	}
	if !action.enabled {
		railColor, thumbColor = divider, disabled
	} else if o.Hovered {
		thumbColor = pageTitleForeground
	}
	railBG, thumbBG := railColor, thumbColor
	if o.Plain {
		railBG, thumbBG = o.Surface, o.Surface
	}
	rail := themed("░░░░", railColor, railBG, o.Dim)
	thumb := themed("██", thumbColor, thumbBG, o.Dim)
	track := thumb + rail
	if action.selected {
		track = rail + thumb
	}
	return themed(marker, focus, o.Surface, o.Dim) + track
}

func settingsContent(o dashboardOptions, width int) (string, []hitRegion) {
	stack := func(cardWidth int, sections ...func(dashboardOptions, int) dashboardBlock) dashboardBlock {
		var column dashboardBlock
		for i, section := range sections {
			if i > 0 {
				column.blank()
			}
			column.append(section(o, cardWidth))
		}
		return column
	}
	if width < 80 {
		column := stack(width, settingsInterface, settingsMihomo, settingsService, settingsCore, settingsDiagnostics)
		return strings.Join(column.lines, "\n"), column.targets
	}

	const gap = 2
	leftWidth := (width - gap) / 2
	rightWidth := width - leftWidth - gap
	left := stack(leftWidth, settingsInterface, settingsService, settingsDiagnostics)
	right := stack(rightWidth, settingsMihomo, settingsCore)

	blank := func(cells int) string { return themed(strings.Repeat(" ", cells), text, bg, o.Dim) }
	leftBlank, gutter, rightBlank := blank(leftWidth), blank(gap), blank(rightWidth)
	lines := make([]string, max(len(left.lines), len(right.lines)))
	for y := range lines {
		l, r := leftBlank, rightBlank
		if y < len(left.lines) && left.lines[y] != "" {
			l = left.lines[y]
		}
		if y < len(right.lines) && right.lines[y] != "" {
			r = right.lines[y]
		}
		lines[y] = l + gutter + r
	}
	targets := left.targets
	for _, hit := range right.targets {
		hit.X += leftWidth + gap
		targets = append(targets, hit)
	}
	return strings.Join(lines, "\n"), targets
}

func settingsInterface(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	hints := settingsSwitchRow("key-hints.toggle", tr("dashboard.interface.keyHints"), !o.HideKeyHints, true)
	hints.note = tr("settings.keyHintsHint")
	return settingsSection(o, width, tr("settings.interface"), "", []settingsRow{
		settingsSelectRow(o, "settings.language", tr("dashboard.language.title")),
		hints,
		settingsThemeRow(o),
		{label: tr("settings.editor"), actions: []overviewControlButton{{id: "editor.configure", label: tr("settings.edit"), enabled: !o.Busy}}},
	})
}

func settingsThemeRow(o dashboardOptions) settingsRow {
	theme := cmp.Or(o.Theme, app.ThemeDark)
	row := settingsSelectRow(o, "settings.theme", o.Locale.T("settings.theme"))
	if theme == app.ThemeAuto {
		row.note = o.Locale.T("settings.themeAuto", (dropdown{Items: themeMenuItems, Value: o.EffectiveTheme}).label())
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
		id, command := "autostart-enable", "dashboard.settings.enableAutostart"
		if s.Service.Enabled {
			id, command = "autostart-disable", "dashboard.settings.disableAutostart"
		}
		if s.ServiceError != "" || s.Service.EnableState != "" && s.Service.EnableState != "enabled" && s.Service.EnableState != "disabled" {
			autostart.control = settingsTrailing
			autostart.value = tr("dashboard.status.unknown")
			autostart.actions = []overviewControlButton{{id: id, label: tr(command), enabled: !o.Busy}}
		} else {
			autostart = settingsSwitchRow(id, autostart.label, s.Service.Enabled, !o.Busy)
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
	return settingsSection(o, width, tr("settings.serviceGroup"), "", []settingsRow{autostart, ports})
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
	return settingsSection(o, width, tr("settings.core"), "", rows)
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
	return settingsSection(o, width, tr("settings.diagnostics"), "", rows)
}

func settingsSection(o dashboardOptions, width int, title, description string, rows []settingsRow) dashboardBlock {
	inner, left, top := max(1, width-4), 2, 1
	if width < 4 {
		inner, left, top = max(1, width), 0, 0
	}
	// panelFrame supplies one blank cell inside each border. Add the second
	// when the card can still show at least one cell of content.
	inset := ""
	if width >= 7 {
		inset = " "
		inner -= 2
		left++
	}
	style := func(s, color string) string { return themed(s, color, panel, o.Dim) }
	var b dashboardBlock
	addText := func(s, color string) {
		b.lines = append(b.lines, strings.Split(wrapText(style(s, color), inner), "\n")...)
	}
	addText(lipgloss.NewStyle().Bold(true).Render(title), text)
	if description != "" {
		addText(description, muted)
	}
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
		rowStart := len(b.lines)
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
			for _, action := range row.actions {
				options := dashboardButtonOptions(action.id, o, action.primary, panel)
				options.Disabled = !action.enabled
				var face string
				switch row.control {
				case settingsSelect:
					face, _ = o.settingsDropdown(action.id).render(action.id, options, o.OpenDropdown == action.id, inner)
				case settingsSwitch:
					face = renderSettingsSwitch(action, options)
				default:
					face = renderButton(action.label, options)
				}
				value := cellCrop(face, inner)
				values = append(values, value)
				total += ansi.StringWidth(value)
			}
			x := 0
			if len(b.lines) == rowStart+1 {
				used := ansi.StringWidth(b.lines[rowStart])
				available := inner - used - total
				gap := labelColumn + 2 - used
				switch {
				case row.control != settingsInline:
					gap = max(2, available)
				case row.value != "":
					gap = 1
				}
				if gap > 0 && gap <= available {
					b.lines[rowStart] += style(strings.Repeat(" ", gap), text)
					x = used + gap
				}
			}
			if x == 0 {
				b.blank()
			}
			for i, action := range row.actions {
				w := ansi.StringWidth(values[i])
				if x+w > inner {
					b.blank()
					x = 0
				}
				if action.enabled {
					b.targets = append(b.targets, hitRegion{ID: action.id, X: left + x, Y: top + len(b.lines) - 1, W: w, H: 1, DropdownRight: row.control == settingsSelect && x > 0})
				}
				b.lines[len(b.lines)-1] += values[i]
				x += w
			}
		}
		if row.note != "" {
			addText(row.note, color)
		}
	}
	for i := range b.lines {
		b.lines[i] = inset + b.lines[i]
	}
	b.lines = panelFrame(b.lines, width, len(b.lines)+2, o)
	return b
}
