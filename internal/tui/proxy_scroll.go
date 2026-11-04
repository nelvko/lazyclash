package tui

import tea "charm.land/bubbletea/v2"

// A plain wheel event changes the viewport, not the data or keyboard focus.
// Popups and notifications keep their normal event and visibility handling.
func (m *Model) updateProxyWheel(event tea.MouseWheelMsg) bool {
	if m.page != "proxies" || m.menu.open || m.ports.open || m.helpOpen || m.wizard.open || m.wizard.networkOpen || m.form != "" || m.notification.panel.open || len(m.notification.items) > 0 {
		return false
	}
	scroll, hover, sidebarOffset := m.scroll, m.hover, m.sidebar.viewport.YOffset()
	m.mouse(event)
	g := dashboardGeometry(m.dashboardOptions())
	m.scroll = min(m.scroll, max(0, m.total-g.height))
	if m.scroll != scroll || m.hover != hover || m.sidebar.viewport.YOffset() != sidebarOffset {
		m.renderLayout()
	}
	return true
}
