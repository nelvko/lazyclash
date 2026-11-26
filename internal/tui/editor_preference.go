package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type editorPreferenceSavedMsg struct {
	id    uint64
	value string
	err   error
}

func (m *Model) openEditorPreference() tea.Cmd {
	if m.busy || m.quitting || m.page != "settings" {
		return nil
	}
	m.form, m.failure = "editor-preference", ""
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = m.tr("settings.editorAuto")
	m.input.SetValue(m.editorPreference)
	return m.input.Focus()
}

func (m *Model) saveEditorPreference() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	if value == m.editorPreference {
		m.closeEditorPreference()
		return nil
	}
	ctx := m.newJob("editor-preference")
	id, backend := m.id, m.backend
	return func() tea.Msg {
		return editorPreferenceSavedMsg{id, value, backend.SaveEditorPreference(ctx, value)}
	}
}

func (m *Model) closeEditorPreference() {
	m.form, m.failure, m.focus = "", "", "editor.configure"
	m.input.Reset()
	m.input.Blur()
}

func (m *Model) renderEditorPreference(base string) (string, []hitRegion) {
	if m.width < 24 || m.height < 7 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	width := min(56, max(24, m.width-2*modalPaddingX))
	inner := width - 2*modalPaddingX
	message, color := m.tr("settings.editorHint"), muted
	if m.failure != "" {
		message, color = m.failure, errorColor
	}
	notes := strings.Split(wrapText(message, inner), "\n")
	footer := strings.Split(wrapText(m.tr("settings.saveKeys"), inner), "\n")
	bodyY := 3 // Top padding, title and divider.
	// Use the single-line input when its frame would crowd out the explanation.
	compact := m.height < bodyY+3+len(notes)+len(footer)+1
	if compact {
		bodyY = 2
	}
	body := m.formInputField(inner, compact, "")
	room := m.height - bodyY - len(body.lines) - len(footer) - 1
	for i, line := range notes[:min(len(notes), room)] {
		if i == room-1 && len(notes) > room {
			line = cellCrop(line+"…", inner)
		}
		body.lines = append(body.lines, themed(line, color, modalBackground, false))
	}
	if len(notes) < room {
		body.blank()
	}
	for _, line := range footer {
		body.lines = append(body.lines, helpText(line, modalBackground, false))
	}

	surface := m.newModalSurface(width, bodyY+len(body.lines)+1)
	title := lipgloss.NewStyle().Bold(true).Render(m.tr("settings.editorCommand"))
	surface.put(modalPaddingX, 1, themed(title, pageTitleForeground, modalBackground, false))
	if !compact {
		surface.divider(2)
	}
	for i, line := range body.lines {
		surface.put(modalPaddingX, bodyY+i, line)
	}
	for i, hit := range body.targets {
		hit.X += modalPaddingX
		hit.Y += bodyY
		body.targets[i] = surface.screen(hit)
	}
	return surface.render(base), body.targets
}
