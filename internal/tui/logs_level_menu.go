package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

var logLevelKeys = [...]string{"logs.level_all", "logs.level_info", "logs.level_warn", "logs.level_error"}

func (m *Model) logLevelDropdown() dropdown {
	items := make([]menuItem, len(logLevelKeys))
	for level, key := range logLevelKeys {
		items[level] = menuItem{ID: strconv.Itoa(level), Label: m.tr(key)}
	}
	return dropdown{Items: items, Value: strconv.Itoa(m.logs.level)}
}

func (m *Model) openLogLevelMenu() {
	if m.openPageSelectionMenu("logs", "logs.level", m.logLevelDropdown()) {
		m.logs.editing = false
		m.logs.search.Blur()
		m.logs.levelMenu = true
	}
}

func (m *Model) reconcileLogsLevelMenu() {
	if !m.menu.open || !m.pageSelectionMenuAvailable("logs") {
		m.logsLevelMenuResult(menuResult{Closed: true})
		return
	}
	m.logLevelDropdown().sync(&m.menu)
}

func (m *Model) logsLevelMenuResult(result menuResult) tea.Cmd {
	if !result.Closed || !m.logs.levelMenu {
		return nil
	}
	available := m.pageSelectionMenuAvailable("logs")
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
