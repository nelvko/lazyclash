package tui

import (
	"cmp"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type themeSavedMsg struct {
	id    uint64
	theme string
	err   error
}

func (m *Model) changeTheme(theme string) tea.Cmd {
	if m.busy || m.quitting || m.page != "settings" || m.theme == theme {
		return nil
	}
	ctx := m.newJob("theme", notificationOperation{Key: "theme", Context: m.tr("settings.theme")})
	id, backend := m.id, m.backend
	m.message = m.tr("settings.themeSaving")
	return tea.Batch(func() tea.Msg {
		return themeSavedMsg{id, theme, backend.SaveThemePreference(ctx, theme)}
	}, m.spinner.Tick)
}

func (m *Model) applyThemePreference(theme string) tea.Cmd {
	m.theme = cmp.Or(theme, app.ThemeDark)
	m.terminalLight = false
	if m.theme == app.ThemeAuto {
		return tea.RequestBackgroundColor
	}
	return nil
}

func (m *Model) effectiveTheme() string {
	if m.theme == app.ThemeLight || m.theme == app.ThemeAuto && m.terminalLight {
		return app.ThemeLight
	}
	return app.ThemeDark
}
