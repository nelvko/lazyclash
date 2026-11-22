package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
