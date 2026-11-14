package tui

import "strings"

func (m Model) connectionsContextHelp() string {
	tr := m.tr
	if m.connections.editing {
		return tr("navigation.connections.searching")
	}
	if m.connections.confirming {
		action := "cancel"
		if m.focus == "conn.confirm" {
			action = "confirm"
		}
		return tr("navigation.connections.confirming", tr("navigation.connections.action."+action))
	}
	if m.focus == "conn.close-all" {
		if m.canCloseAllConnections() {
			return tr("connections.close_all_hint")
		}
		return tr("navigation.connections.controls")
	}
	if m.connections.detail != nil {
		parts := []string{}
		if m.focus == "conn.back" {
			parts = append(parts, tr("navigation.connections.action.back"))
		} else if m.focus == "conn.close" && m.detailActive() {
			parts = append(parts, tr("navigation.connections.action.close"))
		}
		parts = append(parts, tr("navigation.connections.detail"))
		if m.detailActive() {
			parts = append(parts, tr("navigation.connections.close"))
		}
		return strings.Join(parts, " · ")
	}
	parts := []string{}
	action := ""
	switch m.focus {
	case "conn.search":
		action = "search"
	case "conn.clear":
		action = "clear"
	case "conn.active":
		action = "active"
	case "conn.closed":
		action = "closed"
	case "conn.sort":
		action = "sort"
	case "conn.pause":
		if m.connections.paused {
			action = "resume"
		} else if m.connectionSnapshot() != nil {
			action = "pause"
		}
	}
	list := m.filteredConnections()
	for _, c := range list {
		if m.focus == "connection."+m.connectionRowKey(c) {
			action = "details"
			break
		}
	}
	if action != "" {
		parts = append(parts, tr("navigation.connections.action."+action))
	}
	parts = append(parts, tr("navigation.connections.search"))
	if m.connections.closedView {
		parts = append(parts, tr("navigation.connections.active"))
	} else {
		parts = append(parts, tr("navigation.connections.closed"))
	}
	if len(list) > 0 {
		parts = append(parts, tr("navigation.connections.browse"))
	}
	parts = append(parts, tr("navigation.connections.sort"))
	if m.connections.paused {
		parts = append(parts, tr("navigation.connections.resume"))
	} else if m.connectionSnapshot() != nil {
		parts = append(parts, tr("navigation.connections.pause"))
	}
	if !m.connections.closedView {
		// The paused list may retain a connection that has already ended. Use the
		// same exact-ID and lifetime check as the detail close action.
		inspection := m
		inspection.openSelectedConnection()
		if inspection.detailActive() {
			parts = append(parts, tr("navigation.connections.close"))
		}
	}
	parts = append(parts, tr("navigation.connections.controls"))
	return strings.Join(parts, " · ")
}

func (m Model) logsContextHelp() string {
	tr := m.tr
	if m.logs.editing {
		return tr("navigation.logs.searching")
	}
	state := "pause"
	if m.logs.paused {
		state = "resume"
	}
	parts := []string{}
	action := ""
	switch m.focus {
	case "logs.search":
		action = "search"
	case "logs.reset-search":
		action = "reset"
	case "logs.follow":
		action = state
	case "logs.retry":
		action = "retry"
	case "logs.clear":
		action = "clear"
	case "logs.latest":
		action = "latest"
	case "logs.level":
		level := []string{"logs.level_all", "logs.level_info", "logs.level_warn", "logs.level_error"}[(m.logs.level+1)%4]
		parts = append(parts, tr("navigation.logs.action.level", tr(level)))
	case "logs.view":
		action = "compact"
		if m.logs.compact {
			action = "detailed"
		}
	}
	if action != "" {
		parts = append(parts, tr("navigation.logs.action."+action))
	}
	parts = append(parts, tr("navigation.logs.search"))
	entries, _ := m.logsDisplayEntries()
	for _, entry := range entries {
		if m.logMatches(entry) {
			parts = append(parts, tr("navigation.logs.browse"))
			break
		}
	}
	if len(entries) > 0 || m.logs.paused || m.logs.holding {
		parts = append(parts, tr("navigation.logs.latest"))
	}
	parts = append(parts, tr("navigation.logs."+state))
	if m.focus == "logs.list" {
		parts = append(parts, tr("navigation.logs.space."+state))
	}
	parts = append(parts, tr("navigation.logs.controls"))
	return strings.Join(parts, " · ")
}
