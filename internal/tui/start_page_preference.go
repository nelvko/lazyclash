package tui

import (
	"cmp"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type startPageSavedMsg struct {
	id   uint64
	page string
	err  error
}

func (m *Model) changeStartPage(page string) tea.Cmd {
	if !m.pageSelectionMenuAvailable("settings") || page == cmp.Or(m.startPage, app.StartPageOverview) {
		return nil
	}
	ctx := m.newJob("start-page", notificationOperation{Key: "settings.startPage", Context: m.tr("settings.startPage")})
	id, backend := m.id, m.backend
	m.message = m.tr("settings.startPageSaving")
	return tea.Batch(func() tea.Msg {
		return startPageSavedMsg{id, page, backend.SaveStartPagePreference(ctx, page)}
	}, m.spinner.Tick)
}
