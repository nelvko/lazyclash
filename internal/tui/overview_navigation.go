package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Paging is a browsing action. Keep its focus on visible content, so Enter
// cannot unexpectedly return to a control on the page the user just left.
func (m *Model) overviewScrollKey(key tea.KeyPressMsg) bool {
	if m.page != "overview" || m.inlineInstallVisible() || key.String() != "pgdown" && key.String() != "pgup" {
		return false
	}
	g := dashboardGeometry(m.dashboardOptions())
	delta := max(1, g.height-1)
	if key.String() == "pgup" {
		delta = -delta
	}
	m.scroll = max(0, min(m.scroll+delta, max(0, m.total-g.height)))
	if m.focus == "menu" || m.focus == "logo" || strings.HasPrefix(m.focus, "nav.") || strings.HasPrefix(m.focus, "notification.") {
		return true
	}
	for _, target := range m.contentTargets() {
		if target.ID == m.focus && target.Y >= m.scroll && target.Y < m.scroll+g.height {
			return true
		}
	}
	cards := overviewCardRegionsFromModel(*m)
	for _, card := range cards {
		if card.Y >= m.scroll && card.Y < m.scroll+g.height {
			m.focus = card.ID
			return true
		}
	}
	for _, card := range cards {
		if card.Y < m.scroll+g.height && card.Y+card.H > m.scroll {
			m.focus = card.ID
			return true
		}
	}
	return true
}
