package tui

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// profileGridKey uses the unscrolled card geometry so navigation can reach
// cards below the viewport and follows the current one- or two-column layout.
func (m *Model) profileGridKey(k tea.KeyPressMsg) bool {
	if m.page != "profiles" || m.form != "" || m.profilesUI.editing ||
		m.sidebarOpen || strings.HasPrefix(m.focus, "nav.") || m.quitting ||
		m.helpOpen || m.ports.open || m.wizard.open || m.wizard.networkOpen {
		return false
	}
	if k.String() == "esc" && m.profilesUI.detail != "" {
		name := m.profilesUI.detail
		m.profilesUI.detail = ""
		for i, profile := range m.profiles() {
			if profile == name {
				m.focus = "profile." + strconv.Itoa(i)
				break
			}
		}
		for _, card := range m.profileGridCards() {
			if card.ID == m.focus {
				m.revealBodyTarget(card)
				break
			}
		}
		return true
	}

	cards := m.profileGridCards()
	if len(cards) == 0 {
		return false
	}
	index := -1
	for i, card := range cards {
		if card.ID == m.focus {
			index = i
			break
		}
	}
	if index == -1 {
		if k.String() != "down" && k.String() != "j" {
			return false
		}
		switch m.focus {
		case "profiles.url", "profiles.add", "profiles.create", "add", "profiles.update-all":
			m.focus = cards[0].ID
			m.revealBodyTarget(cards[0])
			return true
		}
		return false
	}

	next := index
	current := cards[index]
	key := k.String()
	switch key {
	case "home":
		next = 0
	case "end":
		next = len(cards) - 1
	case "up", "k", "down", "j", "left", "h", "right", "l":
		for i, candidate := range cards {
			switch key {
			case "up", "k":
				if candidate.X == current.X && candidate.Y < current.Y &&
					(next == index || candidate.Y > cards[next].Y) {
					next = i
				}
			case "down", "j":
				if candidate.X == current.X && candidate.Y > current.Y &&
					(next == index || candidate.Y < cards[next].Y) {
					next = i
				}
			case "left", "h":
				if candidate.Y == current.Y && candidate.X < current.X &&
					(next == index || candidate.X > cards[next].X) {
					next = i
				}
			case "right", "l":
				if candidate.Y == current.Y && candidate.X > current.X &&
					(next == index || candidate.X < cards[next].X) {
					next = i
				}
			}
		}
	default:
		return false
	}
	if next != index {
		// Details belong to the profile that opened them. Moving to another
		// card closes them instead of leaving actions aimed at the old profile.
		m.profilesUI.detail = ""
		m.focus = cards[next].ID
	}
	for _, card := range m.profileGridCards() {
		if card.ID == m.focus {
			m.revealBodyTarget(card)
			break
		}
	}
	return true
}

func (m Model) profileGridCards() []hitRegion {
	_, hits := m.pageBody(dashboardBodyWidth(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}))
	var cards []hitRegion
	for _, hit := range hits {
		if _, ok := profileActionIndex(hit.ID, "profile.", len(m.profiles())); ok {
			cards = append(cards, hit)
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Y == cards[j].Y {
			return cards[i].X < cards[j].X
		}
		return cards[i].Y < cards[j].Y
	})
	return cards
}
