package tui

import tea "charm.land/bubbletea/v2"

// A motion event changes only hover styling. Existing clipped hit regions are
// already current, so it needs neither data reconciliation nor focus repair.
func (m *Model) updateProxyHover(event tea.MouseMotionMsg) bool {
	if m.page != "proxies" || m.menu.open || m.ports.open || m.helpOpen || m.wizard.open || m.wizard.networkOpen || m.form != "" || m.notification.panel.open {
		return false
	}
	previous := m.hover
	m.mouse(event)
	if m.hover != previous {
		m.renderLayout()
	}
	return true
}
