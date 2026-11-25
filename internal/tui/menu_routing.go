package tui

import tea "charm.land/bubbletea/v2"

// Each menu shares presentation and input; its owner validates the result
// against current page state before applying an action or selection.
func (m *Model) applyMenuResult(result menuResult) tea.Cmd {
	switch {
	case m.form == "profile-delete":
		if result.Closed && result.Action != "" && !m.busy && !m.quitting {
			m.profilesUI.deleteReplacement = result.Action
		}
		return nil
	case m.logs.levelMenu:
		return m.logsLevelMenuResult(result)
	case m.page == "settings":
		if result.Closed {
			m.hover = ""
			if result.Action != "" && m.pageSelectionMenuAvailable("settings") {
				switch m.menu.anchor.ID {
				case "settings.language":
					return m.changeLanguage(result.Action)
				case "settings.theme":
					return m.changeTheme(result.Action)
				case "settings.switchStyle":
					return m.changeSwitchStyle(result.Action)
				case "settings.logLevel":
					if result.Action != m.snapshot.CoreSettings.LogLevel {
						return m.saveCoreSetting(m.menu.anchor.ID, "log-level", result.Action)
					}
				}
			}
		}
		return nil
	case m.proxies.sortMenu:
		return m.proxyMenuResult(result)
	default:
		return m.profileMenuResult(result)
	}
}

func (m *Model) reconcileMenu() {
	switch {
	case m.form == "profile-delete":
		if m.menu.open {
			m.menu.setItems(m.profileReplacementItems())
		}
	case m.logs.levelMenu:
		m.reconcileLogsLevelMenu()
	case m.page == "settings":
		if m.menu.open && !m.pageSelectionMenuAvailable("settings") {
			m.menu.dismiss()
		}
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
	if m.form == "profile-delete" || m.page == "settings" {
		m.menu.dismiss()
		return
	}
	if m.logs.levelMenu || m.proxies.sortMenu {
		m.applyMenuResult(menuResult{Closed: true})
		return
	}
	m.dismissProfileMenu()
}

func (m *Model) pageSelectionMenuAvailable(page string) bool {
	return m.page == page && !m.busy && !m.quitting && !m.sidebarOpen &&
		!m.providers.open && !m.helpOpen && !m.ports.open && !m.wizard.open &&
		!m.wizard.networkOpen && m.form == "" && !m.notification.panel.open
}

func (m *Model) openPageSelectionMenu(page, trigger, current string, items []menuItem) bool {
	if m.menu.open || !m.pageSelectionMenuAvailable(page) {
		return false
	}
	m.focus, m.hover = trigger, ""
	m.layout()
	for _, target := range m.targets {
		if target.ID == trigger {
			m.menu.showSelection(items, current, target)
			return true
		}
	}
	return false
}

func (m *Model) anchorPageSelectionMenu(targets []hitRegion) {
	if !m.menu.anchored {
		return
	}
	for _, target := range targets {
		if target.ID == m.menu.anchor.ID {
			m.menu.setAnchor(target)
			return
		}
	}
	// A hidden trigger must not leave a floating, unreachable menu behind.
	m.dismissMenu()
}
