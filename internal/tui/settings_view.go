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
	settingsLink
	settingsReadOnly
	settingsGroup
	settingsTabStrip
)

type settingsRow struct {
	label, value, note string
	color              string
	actions            []overviewControlButton
	control            settingsControl
	tip                tooltipBinding
	rightAlign         bool // Keep the current value at the trailing edge when the label wraps.
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
	case "settings.geodataLoader":
		return dropdown{Value: o.Snapshot.CoreSettings.GeodataLoader, Items: []menuItem{
			{ID: "memconservative", Label: o.Locale.T("settings.loaderMemory")},
			{ID: "standard", Label: o.Locale.T("settings.loaderStandard")},
		}}
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
	railColor, thumbColor := divider, muted
	if action.selected {
		railColor, thumbColor = buttonSelectedBackground, settingsSwitchOnThumb
	}
	marker := "  "
	if o.Focused && action.enabled {
		marker = "› "
	}
	if !action.enabled {
		railColor, thumbColor = divider, disabled
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
		column := stack(width, settingsInterface, settingsMihomo, settingsCore, settingsDiagnostics)
		return strings.Join(column.lines, "\n"), column.targets
	}

	const gap = 2
	leftWidth := (width - gap) / 2
	rightWidth := width - leftWidth - gap
	left := stack(leftWidth, settingsInterface, settingsDiagnostics)
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

func settingsAutostart(o dashboardOptions) settingsRow {
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
	return autostart
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
	rows := []settingsRow{core, settingsAutostart(o)}
	if s.GeoRestartPending {
		rows[0].note = tr("settings.geoRestart")
	}
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

// Keep keyboard focus distinct from both the card and the inactive rail in ANSI 256.
const settingsRowHighlight = "#353d45"

func settingsSection(o dashboardOptions, width int, title, description string, rows []settingsRow) dashboardBlock {
	// The dashboard still tracks the pointer for delayed tooltips. Only
	// keyboard focus and selection change the appearance of settings controls.
	o.Hover = ""
	inset := min(2, max(0, (width-1)/2))
	inner := max(1, width-2*inset)
	padding := strings.Repeat(" ", inset)
	blank := themed(strings.Repeat(" ", width), text, panel, o.Dim)
	b := dashboardBlock{lines: []string{blank}}
	header := lipgloss.NewStyle().Bold(true).Render(title)
	for _, line := range strings.Split(wrapText(header, inner), "\n") {
		b.lines = append(b.lines, themed(padding+cellPad(line, inner)+padding, text, panel, o.Dim))
	}
	if description != "" {
		for _, line := range strings.Split(wrapText(description, inner), "\n") {
			b.lines = append(b.lines, themed(padding+cellPad(line, inner)+padding, muted, panel, o.Dim))
		}
	}

	labelColumn := 0
	for _, row := range rows {
		labelColumn = max(labelColumn, ansi.StringWidth(row.label))
	}
	tabbed := len(rows) > 0 && rows[0].control == settingsTabStrip
	spacious := tabbed && width >= 48 && o.Height >= 36
	rowIndent := 0
	if spacious {
		rowIndent = 2
	}
	for i, row := range rows {
		if !tabbed || spacious || row.control == settingsGroup || row.control == settingsTabStrip {
			b.lines = append(b.lines, blank)
		}
		if spacious && row.control == settingsGroup && i > 0 && rows[i-1].control != settingsTabStrip {
			b.lines = append(b.lines, blank)
		}
		if row.control == settingsTabStrip {
			b.append(settingsTabs(o, width, inset))
			continue
		}
		if row.control == settingsGroup {
			for _, line := range strings.Split(wrapText(row.label, inner), "\n") {
				b.lines = append(b.lines, themed(padding+cellPad(line, inner)+padding, accent, panel, o.Dim))
			}
			continue
		}
		surface, focused := panel, false
		for _, action := range row.actions {
			if action.enabled && o.Focus == action.id {
				surface, focused = settingsRowHighlight, true
				break
			}
		}
		item := renderSettingsRow(o, row, inner-rowIndent, min(28, labelColumn), surface)
		for y, line := range item.lines {
			prefix := padding
			if y == 0 && focused && (o.Monochrome || row.control == settingsLink) && inset > 0 {
				prefix = "›" + padding[1:]
			}
			item.lines[y] = themed(prefix+cellPad(strings.Repeat(" ", rowIndent)+line, inner)+padding, text, surface, o.Dim)
		}
		for j := range item.targets {
			item.targets[j].X += inset + rowIndent
		}
		if row.control == settingsLink && len(item.targets) == 1 && item.targets[0].InputWidth == 0 {
			item.targets[0] = hitRegion{ID: item.targets[0].ID, W: width, H: len(item.lines)}
		}
		b.append(item)
	}
	b.lines = append(b.lines, blank)
	return b
}

func renderSettingsRow(o dashboardOptions, row settingsRow, inner, labelColumn int, surface string) dashboardBlock {
	if o.coreNumber != nil && len(row.actions) == 1 && row.actions[0].id == o.coreNumber.id {
		return o.coreNumber.row(o, row.label, inner, surface)
	}
	var b dashboardBlock
	style := func(s, color string) string { return themed(s, color, surface, o.Dim) }
	addText := func(s, color string) {
		for _, line := range strings.Split(wrapText(s, inner), "\n") {
			b.lines = append(b.lines, style(line, color))
		}
	}
	color := row.color
	if color == "" {
		color = muted
	}
	if row.tip.text != "" {
		b = row.tip.label(row.label, o, surface, inner)
	} else if row.label != "" {
		addText(row.label, text)
	}
	// A shared label column keeps values close to their names on wide cards.
	// Long labels, values and action groups flow below instead of truncating.
	if row.value != "" {
		valueWidth := ansi.StringWidth(row.value)
		column := labelColumn + 2
		if row.control == settingsReadOnly {
			column = inner - valueWidth
		}
		if len(b.lines) == 1 && ansi.StringWidth(b.lines[0])+2 <= column && column+valueWidth <= inner {
			gap := column - ansi.StringWidth(b.lines[0])
			b.lines[0] += style(strings.Repeat(" ", gap), text) + style(row.value, color)
		} else if row.rightAlign {
			for _, line := range strings.Split(wrapText(row.value, inner), "\n") {
				b.lines = append(b.lines, style(strings.Repeat(" ", inner-ansi.StringWidth(line))+line, color))
			}
		} else {
			addText(row.value, color)
		}
	}
	if len(row.actions) > 0 {
		var values []string
		total := 0
		for _, action := range row.actions {
			options := dashboardButtonOptions(action.id, o, action.primary, surface)
			options.Disabled = !action.enabled
			// One row marker suffices for a single control. When actions share
			// a row, mark the focused action as well.
			if o.Monochrome && len(row.actions) == 1 {
				options.Focused = false
			}
			var face string
			switch row.control {
			case settingsSelect:
				face, _ = o.settingsDropdown(action.id).render(action.id, options, o.OpenDropdown == action.id, inner)
			case settingsLink:
				ink := focus
				if !action.enabled {
					ink = disabled
				}
				face = style(action.label, ink)
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
		if len(b.lines) == 1 {
			used := ansi.StringWidth(b.lines[0])
			available := inner - used - total
			gap := labelColumn + 2 - used
			switch {
			case row.control != settingsInline:
				gap = max(2, available)
			case row.value != "":
				gap = 1
			}
			if gap > 0 && gap <= available {
				b.lines[0] += style(strings.Repeat(" ", gap), text)
				x = used + gap
			}
		}
		if x == 0 {
			b.blank()
			if row.rightAlign && total < inner {
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
				b.targets = append(b.targets, hitRegion{ID: action.id, X: x, Y: len(b.lines) - 1, W: w, H: 1, DropdownRight: row.control == settingsSelect && x > 0})
			}
			b.lines[len(b.lines)-1] += values[i]
			x += w
		}
	}
	if row.note != "" {
		addText(row.note, color)
	}
	return b
}
