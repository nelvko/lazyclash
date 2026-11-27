package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) renderProfileFieldsForm(base string) (string, []hitRegion) {
	width, height := min(inputImportMaxWidth+2*modalPaddingX, m.width), m.height-2
	if width < 26 || height < 12 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	inner := width - 2*modalPaddingX
	title := m.tr("profiles.create_title")
	if m.form == "profile-settings" {
		title = m.tr("profile_settings.title")
	}
	header := themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(title, inner)), text, modalBackground, false)
	bodyY := 2
	if height >= 18 {
		bodyY++
	}
	footer := m.profileFormFooter(inner)
	bodyH := height - bodyY - len(footer.lines) - 2
	body, overflow := m.profileFormBody(inner, bodyH)
	if !overflow {
		bodyH = len(body.lines)
	}
	height = bodyY + bodyH + len(footer.lines) + 2
	surface := m.newModalSurface(width, height)
	put := func(row int, line string) { surface.put(modalPaddingX, row, cellCrop(line, inner)) }
	put(1, header)
	for i, line := range body.lines {
		put(bodyY+i, line)
	}
	surface.divider(bodyY + bodyH)
	if overflow {
		indicator := " " + m.tr("profiles.form_scroll")
		surface.put(width-modalPaddingX-ansi.StringWidth(indicator), bodyY+bodyH, themed(indicator, muted, modalBackground, false))
	}
	var targets []hitRegion
	for _, hit := range body.targets {
		hit.X += modalPaddingX
		hit.Y += bodyY
		targets = append(targets, surface.screen(hit))
	}
	footerY := bodyY + bodyH + 1
	for i, line := range footer.lines {
		put(footerY+i, line)
	}
	for _, hit := range footer.targets {
		hit.X += modalPaddingX
		hit.Y += footerY
		targets = append(targets, surface.screen(hit))
	}
	return surface.render(base), targets
}

func (m *Model) profileFormField(field, width int) dashboardBlock {
	label := ""
	switch field {
	case profileCreateSource:
		label = "profiles.create_source_label"
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
	heading := themed(m.tr(label), color, modalBackground, false)
	block := dashboardBlock{lines: []string{heading}}
	block.append(renderInputField(*m.profileCreate.input(field), inputFieldOptions{
		ID: id, ClearID: id + ".clear", Width: width, Surface: modalBackground,
		Focused: m.focus == id, Hovered: m.hover == id, Invalid: m.failure != "" && m.focus == id,
		Disabled: m.busy || m.quitting, ClearHovered: m.hover == id+".clear", ClearFocused: m.focus == id+".clear",
	}))
	return block
}

func (m *Model) profileFormBody(width, height int) (dashboardBlock, bool) {
	var rows []dashboardBlock
	for _, field := range m.profileFieldOrder() {
		if m.profileCreate.input(field) == nil {
			break // The remaining controls live in the fixed footer.
		}
		row := m.profileFormField(field, width)
		if field == profileCreateSource && m.profileCreate.field == field {
			row.lines = append(row.lines, m.paths.view(width, min(4, height-len(row.lines)), m.locale)...)
		}
		rows = append(rows, row)
	}
	if m.form == "profile-settings" {
		meta := m.profileSettingsMetadata(m.profileInfo(m.name), width)
		for lines := range slices.Chunk(meta.lines, height) {
			rows = append(rows, dashboardBlock{lines: lines})
		}
	}

	// Keep each editor with its label and any path candidates while scrolling.
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
	if m.profileCreate.manualScroll {
		active = len(rows) - 1
	}
	first, used := active, len(rows[active].lines)
	for first > 0 && used+gap+len(rows[first-1].lines) <= height {
		first--
		used += len(rows[first].lines) + gap
	}
	if m.profileCreate.manualScroll {
		// Stop at the first row of the last page, even after a resize.
		first = min(m.profileCreate.firstRow, first)
	}
	var body dashboardBlock
	visible := 0
	for _, row := range rows[first:] {
		if visible > 0 {
			if len(body.lines)+gap+len(row.lines) > height {
				break
			}
			if gap > 0 {
				body.blank()
			}
		}
		body.append(row)
		visible++
	}
	m.profileCreate.firstRow, m.profileCreate.pageRows = first, visible
	return body, visible < len(rows)
}

func (m *Model) profileFormFooter(width int) dashboardBlock {
	var footer dashboardBlock
	if m.failure != "" {
		footer.lines = append(footer.lines, themed(cellCrop(m.failure, width), errorColor, modalBackground, false))
	} else if m.busy {
		footer.lines = append(footer.lines, themed(cellCrop(m.spinner.View()+" "+m.message, width), accent, modalBackground, false))
	}

	var buttons []profileButton
	for _, action := range m.profileFormActions() {
		buttons = append(buttons, profileButton{
			id: profileCreateFieldID(action.field), label: m.tr(action.label), primary: action.field == profileCreateSave,
		})
	}
	actions := m.profileButtons(buttons, width, modalBackground)
	last := len(actions.lines) - 1
	padding := width - ansi.StringWidth(actions.lines[last])
	actions.lines[last] = strings.Repeat(" ", padding) + actions.lines[last]
	for i := range actions.targets {
		if actions.targets[i].Y == last {
			actions.targets[i].X += padding
		}
	}
	footer.append(actions)
	return footer
}
