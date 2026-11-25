package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) renderProfileCreateForm(base string) (string, []hitRegion) {
	settings := m.form == "profile-settings"
	width, height := min(inputFieldMaxWidth+4, m.width-4), m.height-2
	if settings {
		width, height = min(inputImportMaxWidth+4, m.width), min(32, height)
	}
	if width < 26 || height < 12 {
		return overlay(base, m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
	}
	inner := width - 4
	title := m.tr("profiles.create_title")
	if settings {
		title = m.tr("profile_settings.title", clean(m.profileInfo(m.name).Name))
	}
	header := themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(title, inner)), text, raised, false)
	bodyY := 2
	if !settings {
		hint := m.tr("profiles.create_hint")
		if ansi.StringWidth(header)+2+ansi.StringWidth(hint) <= inner {
			header = cellPad(header, inner-ansi.StringWidth(hint)) + themed(hint, muted, raised, false)
		}
		if height >= 18 {
			bodyY++
		}
	}
	footer := m.profileFormFooter(inner)
	// Keep one row of space above and below the footer.
	bodyH := height - bodyY - len(footer.lines) - 2
	body, scroll := m.profileFormBody(inner, bodyH)
	if !settings {
		bodyH = len(body.lines)
		height = bodyY + bodyH + len(footer.lines) + 2
	}
	x, y := (m.width-width)/2, (m.height-height)/2
	var rows []string
	if settings {
		rows = profileDialogCanvas(width, height)
	} else {
		blank := themed(strings.Repeat(" ", width), text, raised, false)
		rows = slices.Repeat([]string{blank}, height)
	}
	put := func(row int, line string) {
		putRows(rows, 2, row, themed(cellCrop(line, inner), text, raised, false), width)
	}
	put(1, header)
	for i, line := range body.lines[scroll:min(scroll+bodyH, len(body.lines))] {
		put(bodyY+i, line)
	}
	var targets []hitRegion
	for _, hit := range body.targets {
		if hit, ok := clipRegion(hit, 0, scroll, inner, bodyH); ok {
			hit.X += x + 2
			hit.Y += y + bodyY - scroll
			targets = append(targets, hit)
		}
	}
	footerY := bodyY + bodyH + 1
	for i, line := range footer.lines {
		put(footerY+i, line)
	}
	for _, hit := range footer.targets {
		hit.X += x + 2
		hit.Y += y + footerY
		targets = append(targets, hit)
	}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), targets
}

func (m *Model) profileFormField(field, width int) dashboardBlock {
	label := ""
	switch field {
	case profileCreateSource:
		label = "profiles.create_source_label"
		if m.form == "profile-settings" {
			label = "profiles.create_source"
		}
	case profileCreateUserAgent:
		label = "profiles.user_agent"
	case profileCreateName:
		label = "profiles.create_name"
		if m.form == "profile-settings" {
			label = "profiles.display_name"
		}
	case profileCreateDescription:
		label = "profiles.create_description"
	}
	id := profileCreateFieldID(field)
	color := muted
	if m.focus == id {
		color = text
	}
	heading := themed(m.tr(label), color, raised, false)
	if m.form == "profile-create" && field == profileCreateSource && strings.TrimSpace(m.profileCreate.source.Value()) != "" {
		kind := m.tr("profiles.create_local")
		if m.profileCreate.remoteSource() {
			kind = m.tr("profiles.create_remote")
		}
		if ansi.StringWidth(heading)+2+ansi.StringWidth(kind) <= width {
			heading = cellPad(heading, width-ansi.StringWidth(kind)) + themed(kind, muted, raised, false)
		}
	}
	block := dashboardBlock{lines: []string{heading}}
	block.append(renderInputField(*m.profileCreate.input(field), inputFieldOptions{
		ID: id, ClearID: id + ".clear", Width: width, Surface: raised,
		Focused: m.focus == id, Hovered: m.hover == id, Invalid: m.failure != "" && m.focus == id,
		Disabled: m.busy || m.quitting, ClearHovered: m.hover == id+".clear", ClearFocused: m.focus == id+".clear",
	}))
	return block
}

func (m *Model) profileFormBody(width, height int) (dashboardBlock, int) {
	settings := m.form == "profile-settings"
	var rows []dashboardBlock
	for _, field := range m.profileFieldOrder() {
		if field == profileCreateSave {
			break
		}
		fieldWidth := width
		if settings && field != profileCreateSource {
			fieldWidth = min(width, inputFieldMaxWidth)
		}
		row := m.profileFormField(field, fieldWidth)
		if field == profileCreateSource && m.profileCreate.field == field {
			row.lines = append(row.lines, m.paths.view(width, min(4, height-len(row.lines)), m.locale)...)
		}
		rows = append(rows, row)
	}
	if settings {
		return m.profileSettingsBody(rows, width, height)
	}

	// Show complete rows around the focused editor. A candidate list is part
	// of its source row, so it cannot be stranded on the far side of a scroll.
	active, total := len(rows)-1, 0
	for i, row := range rows {
		total += len(row.lines)
		for _, hit := range row.targets {
			if hit.ID == m.focus {
				active = i
			}
		}
	}
	gap := 0
	if total+len(rows)-1 <= height {
		gap = 1
	}
	first, used := active, len(rows[active].lines)
	for first > 0 && used+gap+len(rows[first-1].lines) <= height {
		first--
		used += len(rows[first].lines) + gap
	}
	var body dashboardBlock
	for _, row := range rows[first:] {
		if len(body.lines) > 0 {
			if len(body.lines)+gap+len(row.lines) > height {
				break
			}
			if gap > 0 {
				body.blank()
			}
		}
		body.append(row)
	}
	return body, 0
}

func (m *Model) profileSettingsBody(rows []dashboardBlock, width, height int) (dashboardBlock, int) {
	var body dashboardBlock
	focusEnd := 0
	for i, row := range rows {
		if i > 0 {
			body.blank()
		}
		body.append(row)
		for _, hit := range row.targets {
			if hit.ID == m.focus {
				focusEnd = len(body.lines)
			}
		}
	}
	for _, line := range m.settingsInfoLines(m.profileInfo(m.name)) {
		body.lines = append(body.lines, themed(cellCrop(line, width), muted, raised, false))
	}
	buttons := []profileButton{{"profile-settings.overrides", m.tr("profile_settings.import"), false}}
	if m.canUseDefaultFrom(m.name) {
		buttons = append(buttons, profileButton{"profile-settings.deactivate", m.tr("profile_settings.deactivate"), false})
	}
	body.append(m.profileButtons(buttons, width))
	if focusEnd == 0 {
		focusEnd = len(body.lines)
	}
	scroll := max(0, focusEnd-height)
	if m.profileCreate.manualScroll {
		scroll = min(m.profileCreate.scroll, max(0, len(body.lines)-height))
	}
	m.profileCreate.scroll = scroll
	return body, scroll
}

func (m *Model) profileFormFooter(width int) dashboardBlock {
	settings := m.form == "profile-settings"
	save, hint := m.tr("profiles.create_save"), m.tr("profiles.create_footer")
	if settings {
		save, hint = m.tr("profile_settings.save"), m.tr("profile_settings.footer")
	} else if m.profileCreate.field == profileCreateSource && m.paths.active {
		hint = m.tr("profiles.create_path_keys")
	} else if m.profileCreate.field == profileCreateSource {
		hint = m.tr("profiles.create_source_keys")
	}
	saveButton := renderButton(save, buttonOptions{Kind: buttonPrimary, Surface: raised,
		Focused: m.focus == "profile-create.save", Hovered: m.hover == "profile-create.save",
		Busy: m.busy, Disabled: m.quitting, Plain: m.buttonPlain()})
	cancelButton := renderButton(m.tr("profiles.create_cancel"), buttonOptions{Surface: raised,
		Focused: m.focus == "profile-create.cancel", Hovered: m.hover == "profile-create.cancel",
		Disabled: m.busy || m.quitting, Plain: m.buttonPlain()})
	feedback := ""
	if m.failure != "" {
		feedback = themed(m.failure, errorColor, raised, false)
	} else if m.busy {
		feedback = themed(m.spinner.View()+" "+m.message, accent, raised, false)
	}

	var footer dashboardBlock
	saveW, cancelW := ansi.StringWidth(saveButton), ansi.StringWidth(cancelButton)
	saveX, cancelX, buttonY := cancelW, 0, 0
	if settings {
		footer.lines = []string{cancelButton + saveButton, themed(hint, muted, raised, false), feedback}
	} else {
		saveX = width - saveW - cancelW
		cancelX = saveX + saveW
		prompt := themed(cellCrop(hint, width), muted, raised, false)
		if feedback != "" {
			prompt = cellCrop(feedback, width)
		}
		if ansi.StringWidth(prompt)+2 > saveX {
			footer.lines = append(footer.lines, prompt)
			buttonY, prompt = 1, ""
		}
		footer.lines = append(footer.lines, cellPad(prompt, saveX)+saveButton+cancelButton)
	}
	if !m.busy && !m.quitting {
		footer.targets = []hitRegion{
			{ID: "profile-create.save", X: saveX, Y: buttonY, W: saveW, H: 1},
			{ID: "profile-create.cancel", X: cancelX, Y: buttonY, W: cancelW, H: 1},
		}
	}
	return footer
}
