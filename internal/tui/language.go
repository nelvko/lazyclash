package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func (m *Model) tr(id string, args ...any) string    { return m.locale.T(id, args...) }
func (m *Model) localizeError(err error) string      { return clean(m.locale.Error(err)) }
func (m *Model) localizeDetail(detail string) string { return m.locale.Detail(detail) }

func (m *Model) changeLanguage(language string) tea.Cmd {
	if m.busy || m.quitting || m.page != "settings" {
		return nil
	}
	if language != i18n.English && language != i18n.Chinese {
		return nil
	}
	if language == m.locale.Language() && language == m.savedLanguage {
		return nil
	}
	ctx := m.newJob("language", notificationOperation{Key: "language." + language, Context: m.tr("dashboard.language.title")})
	id, backend := m.id, m.backend
	m.message = m.tr("language.saving")
	return tea.Batch(func() tea.Msg {
		return languageSavedMsg{id: id, language: language, err: backend.SaveLanguagePreference(ctx, language)}
	}, m.spinner.Tick)
}

func (m *Model) refreshLocalizedInputs() {
	m.connections.search.Placeholder = m.tr("model.connection_search")
	m.rules.search.Placeholder = m.tr("model.rule_search")
	m.profilesUI.url.Placeholder = m.tr("profiles.url_placeholder")
	m.logs.search.Placeholder = m.tr("logs.placeholder")
	if m.form == "source" {
		m.input.Placeholder = m.tr("model.source_placeholder")
	}
	// Preserve current values, cursor, focus and task state when changing language.
	if m.focus == "settings.language" {
		m.scroll = 0
	}
}
