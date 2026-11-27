package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
)

func (m *Model) renderWebsiteEditor(base string) (string, []hitRegion) {
	if m.width < 24 || m.height < 10 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	width := min(58, m.width)
	inner := width - 2*modalPaddingX
	e := &m.websiteEditor
	title := m.tr("websiteEditor.add")
	var body dashboardBlock
	buttons := []profileButton{{id: "websiteEditor.cancel", label: m.tr("websiteEditor.cancel")}}
	if !e.confirmRemove {
		if e.original.URL != "" {
			title = m.tr("websiteEditor.edit")
		}
		for _, field := range []struct {
			id    string
			input *textinput.Model
		}{{"websiteEditor.address", &e.address}, {"websiteEditor.name", &e.name}} {
			body.lines = append(body.lines, themed(m.tr(field.id), muted, modalBackground, false))
			*field.input = prepareInputField(*field.input, inputFieldTextWidth(inner, 0, true), modalBackground, false)
			body.append(renderInputField(*field.input, inputFieldOptions{ID: field.id, ClearID: field.id + ".clear",
				Width: inner, Surface: modalBackground, Focused: m.focus == field.id, Hovered: m.hover == field.id,
				ClearHovered: m.hover == field.id+".clear", Disabled: m.busy, Compact: m.height < 17}))
		}
		buttons = append([]profileButton{{id: "websiteEditor.save", label: m.tr("websiteEditor.save"), primary: true}}, buttons...)
		if e.original.URL != "" {
			buttons = append(buttons, profileButton{id: "websiteEditor.remove", label: m.tr("websiteEditor.remove")})
		}
	} else {
		title = m.tr("websiteEditor.removeTitle")
		body.lines = append(body.lines, themed(cellCrop(clean(e.original.Name), inner), text, modalBackground, false))
		body.lines = append(body.lines, themed(cellCrop(clean(e.original.URL), inner), muted, modalBackground, false))
		buttons = append(buttons, profileButton{id: "websiteEditor.confirm", label: m.tr("websiteEditor.confirm")})
	}
	footer := m.profileButtons(buttons, inner, modalBackground)
	if e.err != "" || e.canceled || m.busy {
		message, color := e.err, errorColor
		if e.canceled && e.err == "" {
			message, color = m.tr("websiteEditor.notSaved"), muted
		}
		if m.busy {
			message, color = m.tr("websiteEditor.saving"), muted
			if m.opCtx.Err() != nil {
				message = m.tr("model.canceling")
			}
		}
		rows := strings.Split(wrapText(message, inner), "\n")
		space := max(1, m.height-4-len(body.lines)-len(footer.lines))
		if len(rows) > space {
			rows = rows[:space]
			rows[space-1] = cellCrop(rows[space-1], inner-1) + "…"
		}
		for _, line := range rows {
			body.lines = append(body.lines, themed(line, color, modalBackground, false))
		}
	}
	if 4+len(body.lines)+len(footer.lines) < m.height {
		body.blank()
	}
	body.append(footer)
	surface := m.newModalSurface(width, 4+len(body.lines))
	surface.accessHeader(title, "", modalPaddingX)
	for i, line := range body.lines {
		surface.put(modalPaddingX, 3+i, line)
	}
	for i, hit := range body.targets {
		hit.X, hit.Y = hit.X+modalPaddingX, hit.Y+3
		body.targets[i] = surface.screen(hit)
	}
	if m.busy {
		body.targets = nil
	}
	return surface.render(base), body.targets
}
