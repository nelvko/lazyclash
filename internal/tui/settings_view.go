package tui

import (
	"cmp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
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
	settingsAddress
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
	case "settings.startPage":
		pages := navPages()
		items := make([]menuItem, len(pages))
		for i, page := range pages {
			items[i] = menuItem{ID: page, Label: pageTitle(page, o.Locale)}
		}
		return dropdown{Items: items, Value: cmp.Or(o.StartPage, app.StartPageOverview)}
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

const (
	settingsColumnGap       = 2
	settingsColumnsMinWidth = 80
)

func settingsColumns(o dashboardOptions, width int) (left, right dashboardBlock, leftWidth int) {
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
	if width < settingsColumnsMinWidth {
		return stack(width, settingsInterface, settingsMihomo, settingsCore), dashboardBlock{}, width
	}
	leftWidth = (width - settingsColumnGap) / 2
	left = stack(leftWidth, settingsInterface, settingsCore)
	right = settingsMihomo(o, width-leftWidth-settingsColumnGap)
	return left, right, leftWidth
}

func settingsContent(o dashboardOptions, width int) (string, []hitRegion) {
	left, right, leftWidth := settingsColumns(o, width)
	if leftWidth == width {
		return strings.Join(left.lines, "\n"), left.targets
	}
	blank := func(cells int) string { return themed(strings.Repeat(" ", cells), text, bg, o.Dim) }
	leftBlank, gutter, rightBlank := blank(leftWidth), blank(settingsColumnGap), blank(width-leftWidth-settingsColumnGap)
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
		hit.X += leftWidth + settingsColumnGap
		targets = append(targets, hit)
	}
	return strings.Join(lines, "\n"), targets
}

func settingsInterface(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	hints := settingsSwitchRow("key-hints.toggle", tr("dashboard.interface.keyHints"), !o.HideKeyHints, true)
	hints.tip = tooltipBinding{owner: "key-hints.toggle", text: tr("settings.keyHints.tooltip")}
	return settingsSection(o, width, tr("settings.interface"), "", []settingsRow{
		settingsSelectRow(o, "settings.language", tr("dashboard.language.title")),
		settingsSelectRow(o, "settings.startPage", tr("settings.startPage")),
		hints,
		settingsThemeRow(o),
		{label: tr("settings.editor"), value: clean(o.EditorCommand), control: settingsLink, rightAlign: true,
			actions: []overviewControlButton{{id: "editor.configure", label: "›", enabled: !o.Busy}}},
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

// Keep keyboard focus distinct from both the card and the inactive rail in ANSI 256.
const settingsRowHighlight = "#353d45"

// Navigation, hints and tooltips use the logical focus even when the settings
// control itself has no visible focus indicator.
func (o dashboardOptions) settingsControlAppearance() dashboardOptions {
	o.Hover = ""
	if o.settingsPointerFocus {
		o.Focus = ""
	}
	return o
}

func settingsSection(o dashboardOptions, width int, title, description string, rows []settingsRow) dashboardBlock {
	o = o.settingsControlAppearance()
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
	addTrailingValue := func(value, ink string) {
		for _, line := range strings.Split(ansi.Hardwrap(value, inner, true), "\n") {
			line = cellCrop(line, inner)
			b.lines = append(b.lines, style(strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)))+line, ink))
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
	if row.control == settingsLink && len(row.actions) == 1 && ansi.StringWidth(row.actions[0].label) > inner {
		action := row.actions[0]
		ink := text
		if action.enabled {
			b.targets = append(b.targets, hitRegion{ID: action.id, W: inner, H: 1})
		} else {
			ink = disabled
		}
		addTrailingValue(action.label, ink)
		return b
	}
	// A shared label column keeps values close to their names on wide cards.
	// Long labels, values and action groups flow below instead of truncating.
	valueWithAction := row.control == settingsTrailing && row.rightAlign && row.value != ""
	if row.value != "" && row.control != settingsLink && !valueWithAction {
		valueWidth := ansi.StringWidth(row.value)
		column := labelColumn + 2
		if row.control == settingsReadOnly {
			column = inner - valueWidth
		} else if row.control == settingsAddress {
			column = inner - valueWidth - 2 - ansi.StringWidth(row.actions[0].label)
		}
		if len(b.lines) == 1 && ansi.StringWidth(b.lines[0])+2 <= column && column+valueWidth <= inner {
			gap := column - ansi.StringWidth(b.lines[0])
			b.lines[0] += style(strings.Repeat(" ", gap), text) + style(row.value, color)
		} else if row.rightAlign {
			addTrailingValue(row.value, color)
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
				ink := text
				if !action.enabled {
					ink = disabled
				}
				caption := action.label
				if row.value != "" {
					// Keep the arrow visible when a command is too long for the row.
					suffixWidth := 1 + ansi.StringWidth(caption)
					room := inner - ansi.StringWidth(row.label) - 2
					if room <= suffixWidth {
						room = inner
					}
					if preview := cellCrop(row.value, room-suffixWidth); preview != "" {
						caption = preview + " " + caption
					}
				}
				face = style(caption, ink)
			case settingsAddress:
				ink := accent
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
		// Keep the observed status beside its action when the pair fits.
		prefix := ""
		if valueWithAction {
			if w := ansi.StringWidth(row.value) + 1; w+total <= inner {
				prefix = style(row.value+" ", color)
				total += w
			} else {
				addTrailingValue(row.value, color)
			}
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
		b.lines[len(b.lines)-1] += prefix
		x += ansi.StringWidth(prefix)
		for i, action := range row.actions {
			w := ansi.StringWidth(values[i])
			if x+w > inner {
				b.blank()
				x = 0
			}
			if action.enabled {
				hit := hitRegion{ID: action.id, X: x, Y: len(b.lines) - 1, W: w, H: 1, DropdownRight: row.control == settingsSelect && x > 0}
				if action.id == "settings.copyProxyAddress" && o.tooltips != nil {
					hit.tip = tooltipBinding{owner: action.id, text: o.Locale.T(action.id)}
				}
				b.targets = append(b.targets, hit)
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
