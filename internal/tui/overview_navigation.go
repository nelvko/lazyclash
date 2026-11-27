package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Wrapped controls contribute a mouse region per painted line. Keyboard
// navigation uses the bounding box to reveal the whole control as one stop.
func (b dashboardBlock) overviewFocusRegions() []hitRegion {
	stops := b.cards
	for _, hit := range b.targets {
		if n := len(stops); n > 0 && stops[n-1].ID == hit.ID {
			stop := &stops[n-1]
			stop.W = max(stop.W, hit.W)
			stop.H = hit.Y + hit.H - stop.Y
			continue
		}
		stops = append(stops, hit)
	}
	return stops
}

func (m *Model) overviewModeNavigation(key string) bool {
	if m.page != "overview" || m.sidebarOpen || !strings.HasPrefix(m.focus, "mode.") {
		return false
	}
	var step int
	switch key {
	case "left", "h":
		step = -1
	case "right", "l":
		step = 1
	default:
		return false
	}
	current := -1
	var choices []hitRegion
	for _, hit := range m.contentTargets() {
		if strings.HasPrefix(hit.ID, "mode.") {
			if hit.ID == m.focus {
				current = len(choices)
			}
			choices = append(choices, hit)
		}
	}
	if current < 0 {
		return false
	}
	next := choices[(current+step+len(choices))%len(choices)]
	m.focus, m.hover = next.ID, ""
	m.revealBodyTarget(next)
	return true
}

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
