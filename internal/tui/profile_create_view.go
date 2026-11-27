package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) renderProfileCreateForm(base string) (string, []hitRegion) {
	settings := m.form == "profile-settings"
	width, height := min(inputFieldMaxWidth+2*modalPaddingX, m.width-4), m.height-2
	if settings {
		width = min(inputImportMaxWidth+2*modalPaddingX, m.width)
	}
	if width < 26 || height < 12 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	inner := width - 2*modalPaddingX
	title, detail := m.tr("profiles.create_title"), m.tr("profiles.create_hint")
	if settings {
		title = m.tr("profile_settings.title")
		detail = m.profileSourceKind()
	}
	header := themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(title, inner)), text, modalBackground, false)
	if detail != "" && ansi.StringWidth(header)+2+ansi.StringWidth(detail) <= inner {
		header = cellPad(header, inner-ansi.StringWidth(detail)) + themed(detail, muted, modalBackground, false)
	}
	bodyY := 2
	if height >= 18 {
		bodyY++
	}
	footer := m.profileFormFooter(inner)
	bodyH := height - bodyY - len(footer.lines) - 2
	body, overflow := m.profileFormBody(inner, bodyH)
	if !settings || !overflow {
		bodyH = len(body.lines)
	}
	height = bodyY + bodyH + len(footer.lines) + 2
	surface := m.newModalSurface(width, height)
	put := func(row int, line string) { surface.put(modalPaddingX, row, cellCrop(line, inner)) }
	put(1, header)
	for i, line := range body.lines {
		put(bodyY+i, line)
	}
	if settings {
		surface.divider(bodyY + bodyH)
		if overflow {
			indicator := " " + m.tr("profile_settings.scroll")
			surface.put(width-modalPaddingX-ansi.StringWidth(indicator), bodyY+bodyH, themed(indicator, muted, modalBackground, false))
		}
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

func (m *Model) profileSourceKind() string {
	if strings.TrimSpace(m.profileCreate.source.Value()) == "" {
		return ""
	}
	if m.profileCreate.remoteSource() {
		return m.tr("profiles.create_remote")
	}
	return m.tr("profiles.create_local")
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
	if m.form == "profile-create" && field == profileCreateSource {
		if kind := m.profileSourceKind(); kind != "" && ansi.StringWidth(heading)+2+ansi.StringWidth(kind) <= width {
			heading = cellPad(heading, width-ansi.StringWidth(kind)) + themed(kind, muted, modalBackground, false)
		}
	}
	block := dashboardBlock{lines: []string{heading}}
	block.append(renderInputField(*m.profileCreate.input(field), inputFieldOptions{
		ID: id, ClearID: id + ".clear", Width: width, Surface: modalBackground,
		Focused: m.focus == id, Hovered: m.hover == id, Invalid: m.failure != "" && m.focus == id,
		Disabled: m.busy || m.quitting, ClearHovered: m.hover == id+".clear", ClearFocused: m.focus == id+".clear",
	}))
	return block
}

func (m *Model) profileFormBody(width, height int) (dashboardBlock, bool) {
	settings := m.form == "profile-settings"
	var rows []dashboardBlock
	order := m.profileFieldOrder()
	if settings && width >= 58 {
		// Leave room for a filename and a short description on the same row.
		const gap = 2
		left := (width - gap) / 2
		rows = append(rows, joinFormFields(gap,
			m.profileFormField(profileCreateName, left),
			m.profileFormField(profileCreateDescription, width-left-gap)))
		order = order[2:]
	}
	for _, field := range order {
		if field == profileCreateSave {
			break
		}
		row := m.profileFormField(field, width)
		if field == profileCreateSource && m.profileCreate.field == field {
			row.lines = append(row.lines, m.paths.view(width, min(4, height-len(row.lines)), m.locale)...)
		}
		rows = append(rows, row)
	}
	if settings {
		meta := m.profileSettingsMetadata(m.profileInfo(m.name), width)
		for lines := range slices.Chunk(meta.lines, height) {
			rows = append(rows, dashboardBlock{lines: lines})
		}
	}

	// A row contains a whole editor (including its path candidates), a pair
	// of editors, or the subscription metadata. Scroll between complete rows.
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
	if settings && m.profileCreate.manualScroll {
		active = len(rows) - 1
	}
	first, used := active, len(rows[active].lines)
	for first > 0 && used+gap+len(rows[first-1].lines) <= height {
		first--
		used += len(rows[first].lines) + gap
	}
	if settings && m.profileCreate.manualScroll {
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
	if settings {
		m.profileCreate.firstRow, m.profileCreate.pageRows = first, visible
	}
	return body, visible < len(rows)
}

func (m *Model) profileFormFooter(width int) dashboardBlock {
	settings := m.form == "profile-settings"
	save, hint := m.tr("profiles.create_save"), m.tr("profiles.create_footer")
	if settings {
		save = m.tr("profile_settings.save")
	} else if m.profileCreate.field == profileCreateSource && m.paths.active {
		hint = m.tr("profiles.create_path_keys")
	} else if m.profileCreate.field == profileCreateSource {
		hint = m.tr("profiles.create_source_keys")
	}
	saveButton := renderButton(save, buttonOptions{Kind: buttonPrimary, Surface: modalBackground,
		Focused: m.focus == "profile-create.save", Hovered: m.hover == "profile-create.save",
		Busy: m.busy, Disabled: m.quitting, Plain: m.buttonPlain()})
	cancelButton := renderButton(m.tr("profiles.create_cancel"), buttonOptions{Surface: modalBackground,
		Focused: m.focus == "profile-create.cancel", Hovered: m.hover == "profile-create.cancel",
		Disabled: m.busy || m.quitting, Plain: m.buttonPlain()})
	feedback := ""
	if m.failure != "" {
		feedback = themed(m.failure, errorColor, modalBackground, false)
	} else if m.busy {
		feedback = themed(m.spinner.View()+" "+m.message, accent, modalBackground, false)
	}

	var footer, leading dashboardBlock
	if settings {
		actions := []profileButton{{"profile-settings.overrides", m.tr("profile_settings.import"), false}}
		if m.canUseDefaultFrom(m.name) {
			actions = append(actions, profileButton{"profile-settings.deactivate", m.tr("profile_settings.deactivate"), false})
		}
		leading = m.profileButtons(actions, width, modalBackground)
		if feedback != "" {
			footer.lines = append(footer.lines, cellCrop(feedback, width))
		}
	} else {
		prompt := themed(hint, muted, modalBackground, false)
		if feedback != "" {
			prompt = feedback
		}
		leading.lines = []string{cellCrop(prompt, width)}
	}
	saveW, cancelW := ansi.StringWidth(saveButton), ansi.StringWidth(cancelButton)
	saveX := width - saveW - cancelW
	footer.append(leading)
	if len(leading.lines) > 1 || ansi.StringWidth(leading.lines[0])+2 > saveX {
		footer.blank()
	}
	buttonY := len(footer.lines) - 1
	footer.lines[buttonY] = cellPad(footer.lines[buttonY], saveX) + saveButton + cancelButton
	if !m.busy && !m.quitting {
		footer.targets = append(footer.targets,
			hitRegion{ID: "profile-create.save", X: saveX, Y: buttonY, W: saveW, H: 1},
			hitRegion{ID: "profile-create.cancel", X: saveX + saveW, Y: buttonY, W: cancelW, H: 1})
	}
	return footer
}
