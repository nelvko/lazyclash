package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// profileGridKey uses the unscrolled card geometry so navigation can reach
// cards below the viewport and follows the current column count.
func (m *Model) profileGridKey(k tea.KeyPressMsg) bool {
	if m.page != "profiles" || m.form != "" || m.profilesUI.editing ||
		m.sidebarOpen || strings.HasPrefix(m.focus, "nav.") || m.quitting ||
		m.ports.open || m.wizard.open || m.wizard.networkOpen {
		return false
	}
	key := k.String()
	switch key {
	case "home", "end", "up", "k", "down", "j", "left", "h", "right", "l":
	default:
		return false
	}

	layout := m.layoutProfiles(dashboardBodyWidth(m.dashboardOptions()))
	count := len(layout.names)
	if count == 0 {
		return false
	}
	index, ok := profileActionIndex(m.focus, "profile.", count)
	if !ok {
		if key == "down" || key == "j" {
			switch m.focus {
			case "profiles.url", "profiles.add", "profiles.create", "add", "profiles.update-all":
				card := layout.card(0)
				m.focus = card.ID
				m.revealBodyTarget(card)
				return true
			}
		}
		return false
	}
	next := index
	switch key {
	case "home":
		next = 0
	case "end":
		next = count - 1
	case "up", "k":
		if index >= layout.columns {
			next -= layout.columns
		}
	case "down", "j":
		if index+layout.columns < count {
			next += layout.columns
		}
	case "left", "h":
		if index%layout.columns > 0 {
			next--
		}
	case "right", "l":
		if index%layout.columns+1 < layout.columns && index+1 < count {
			next++
		}
	}
	card := layout.card(next)
	m.focus = card.ID
	m.revealBodyTarget(card)
	return true
}

func (m *Model) profileGridCards() []hitRegion {
	layout := m.layoutProfiles(dashboardBodyWidth(m.dashboardOptions()))
	cards := make([]hitRegion, len(layout.names))
	for index := range cards {
		cards[index] = layout.card(index)
	}
	return cards
}
