package tui

import tea "charm.land/bubbletea/v2"

// Motion and wheel messages only change pointer styling and the viewport.
// Avoid data reconciliation and focus repair for these high frequency events.
func (m *Model) updateRulesMouse(event tea.MouseMsg) bool {
	if m.page != "rules" || m.providers.open || m.menu.open || m.ports.open || m.helpOpen || m.wizard.open || m.wizard.networkOpen || m.form != "" || m.notification.panel.open || len(m.notification.items) > 0 {
		return false
	}
	switch event.(type) {
	case tea.MouseMotionMsg:
		previous := m.hover
		m.mouse(event)
		if m.sidebarOpen && event.Mouse().X >= min(sidebarWidth, m.width) {
			m.hover = ""
		}
		if m.hover != previous {
			m.renderLayout()
		}
		return true
	case tea.MouseWheelMsg:
		scroll, hover, sidebarOffset := m.scroll, m.hover, m.sidebar.viewport.YOffset()
		m.mouse(event)
		g := dashboardGeometry(m.dashboardOptions())
		m.scroll = min(m.scroll, max(0, m.total-g.height))
		point := event.Mouse()
		m.hover = m.rulesPointerTarget(point.X, point.Y)
		if m.sidebarOpen || m.sidebar.viewport.YOffset() != sidebarOffset {
			m.hover = ""
		}
		if m.scroll != scroll || m.hover != hover || m.sidebar.viewport.YOffset() != sidebarOffset {
			m.renderLayout()
		}
		return true
	}
	return false
}

// A wheel event also contains the current pointer position. Resolve its target
// against the new logical offset, including rules newly entering the viewport.
func (m Model) rulesPointerTarget(x, y int) string {
	g := dashboardGeometry(m.dashboardOptions())
	if x >= g.x && x < g.x+g.width && y >= g.y && y < g.y+g.height {
		for _, target := range m.rulesTargets(g.width) {
			if x-g.x >= target.X && x-g.x < target.X+target.W && y-g.y+m.scroll >= target.Y && y-g.y+m.scroll < target.Y+target.H {
				return target.ID
			}
		}
		return ""
	}
	for _, target := range m.targets {
		if x >= target.X && x < target.X+target.W && y >= target.Y && y < target.Y+target.H {
			return target.ID
		}
	}
	return ""
}
