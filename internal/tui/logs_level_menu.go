package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

var logLevelKeys = [...]string{"logs.level_all", "logs.level_info", "logs.level_warn", "logs.level_error"}

func (m *Model) logLevelMenuAvailable() bool {
	return m.page == "logs" && !m.busy && !m.quitting && !m.sidebarOpen &&
		!m.providers.open && !m.helpOpen && !m.ports.open && !m.wizard.open &&
		!m.wizard.networkOpen && m.form == "" && !m.notification.panel.open
}

func (m *Model) logLevelMenuItems() []menuItem {
	items := make([]menuItem, len(logLevelKeys))
	for level, key := range logLevelKeys {
		items[level] = menuItem{ID: strconv.Itoa(level), Label: m.tr(key), Checked: level == m.logs.level}
	}
	return items
}

func (m *Model) openLogLevelMenu() {
	if !m.logLevelMenuAvailable() || m.menu.open {
		return
	}
	m.logs.editing = false
	m.logs.search.Blur()
	m.focus, m.hover = "logs.level", ""
	m.layout()
	for _, target := range m.targets {
		if target.ID == "logs.level" {
			m.logs.levelMenu = true
			m.menu.showSelection(m.logLevelMenuItems(), strconv.Itoa(m.logs.level), target)
			return
		}
	}
}

func (m *Model) reconcileLogsLevelMenu() {
	if !m.menu.open || !m.logLevelMenuAvailable() {
		m.logsLevelMenuResult(menuResult{Closed: true})
		return
	}
	m.menu.setItems(m.logLevelMenuItems())
}

func (m *Model) logsLevelMenuResult(result menuResult) tea.Cmd {
	if !result.Closed || !m.logs.levelMenu {
		return nil
	}
	available := m.logLevelMenuAvailable()
	m.menu.dismiss()
	m.logs.levelMenu, m.hover = false, ""
	if m.page == "logs" {
		m.focus = "logs.level"
	}
	if available && result.Action != "" {
		level, err := strconv.Atoi(result.Action)
		if err == nil && level >= 0 && level < len(logLevelKeys) && level != m.logs.level {
			m.logs.level = level
			m.resetLogsView()
		}
	}
	return nil
}

func (m *Model) anchorLogsLevelMenu(targets []hitRegion) {
	if !m.logs.levelMenu {
		return
	}
	for _, target := range targets {
		if target.ID == "logs.level" {
			m.menu.setAnchor(target)
			return
		}
	}
	// A hidden trigger must not leave a floating, unreachable menu behind.
	m.logsLevelMenuResult(menuResult{Closed: true})
}
