package tui

import tea "charm.land/bubbletea/v2"

// Each menu shares presentation and input; its owner validates the result
// against current page state before applying an action or selection.
func (m *Model) applyMenuResult(result menuResult) tea.Cmd {
	switch {
	case m.logs.levelMenu:
		return m.logsLevelMenuResult(result)
	case m.proxies.sortMenu:
		return m.proxyMenuResult(result)
	default:
		return m.profileMenuResult(result)
	}
}

func (m *Model) reconcileMenu() {
	switch {
	case m.logs.levelMenu:
		m.reconcileLogsLevelMenu()
	case m.proxies.sortMenu:
		if m.page != "proxies" || m.busy {
			m.menu.dismiss()
			m.proxies.sortMenu = false
		}
	default:
		m.reconcileProfileMenu()
	}
}

func (m *Model) dismissMenu() {
	if m.logs.levelMenu || m.proxies.sortMenu {
		m.applyMenuResult(menuResult{Closed: true})
		return
	}
	m.dismissProfileMenu()
}
