package tui

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func listTarget(id string) bool {
	if strings.HasPrefix(id, "profile.") {
		_, err := strconv.Atoi(strings.TrimPrefix(id, "profile."))
		return err == nil
	}
	for _, prefix := range []string{"provider.row.", "connection.", "rule.", "group.", "node."} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// Navigation and the active list each form one Tab stop; arrows move inside
// those groups. All other controls retain their visual reading order.
func (m Model) focusTargets() []hitRegion {
	if m.providers.open {
		return m.providers.controls
	}
	if m.sidebarOpen {
		return []hitRegion{{ID: "nav." + m.page}}
	}
	var controls, list []hitRegion
	g := dashboardGeometry(m.dashboardOptions())
	if g.sidebar {
		controls = append(controls, hitRegion{ID: "nav." + m.page})
	} else {
		controls = append(controls, hitRegion{ID: "menu"})
	}
	for _, target := range m.targets {
		if target.ID != "menu" && target.ID != "logo" && !strings.HasPrefix(target.ID, "nav.") && (target.Y < g.y || strings.HasPrefix(target.ID, "notification.")) {
			controls = append(controls, target)
		}
	}
	body := m.contentTargets()
	for _, target := range body {
		target.X += g.x
		target.Y += g.y
		if listTarget(target.ID) || strings.HasPrefix(target.ID, "proxies.delay.") || strings.HasPrefix(target.ID, "overview.card.") || target.ID == "logs.list" {
			list = append(list, target)
		} else {
			controls = append(controls, target)
		}
	}
	if len(list) == 0 && !(m.page == "overview" && m.inlineInstallVisible()) || m.focus == "content" {
		list = append([]hitRegion{{ID: "content", X: g.x, Y: g.y, W: g.width, H: 1}}, list...)
	}
	if len(list) > 0 {
		chosen := list[0]
		for _, target := range list {
			if target.ID == m.focus {
				chosen = target
				break
			}
		}
		controls = append(controls, chosen)
	} else if len(body) == 0 {
		controls = append(controls, hitRegion{ID: "content", X: g.x, Y: g.y, W: g.width, H: 1})
	}
	sort.SliceStable(controls[1:], func(i, j int) bool {
		a, b := controls[i+1], controls[j+1]
		if m.page == "connections" {
			// Read the input at its text row, alongside the tabs, rather than
			// sorting its top frame above the tabs to its left.
			if a.ID == "conn.search" {
				a.Y++
			}
			if b.ID == "conn.search" {
				b.Y++
			}
		}
		if a.Y == b.Y {
			return a.X < b.X
		}
		return a.Y < b.Y
	})
	return controls
}

func (m *Model) navigationKey(k tea.KeyPressMsg) bool {
	if m.focus == "menu" {
		switch k.String() {
		case "up", "k", "down", "j":
			m.sidebarOpen = true
			m.focus = "nav." + m.page
			m.sidebar.reveal = true
			return true
		}
	}
	if !strings.HasPrefix(m.focus, "nav.") {
		return false
	}
	if k.String() == "right" || k.String() == "l" {
		m.enterContent()
		return true
	}
	pages := navPages()
	index := 0
	for i, page := range pages {
		if page == m.page {
			index = i
			break
		}
	}
	switch k.String() {
	case "up", "k":
		index = max(0, index-1)
	case "down", "j":
		index = min(len(pages)-1, index+1)
	case "home":
		index = 0
	case "end":
		index = len(pages) - 1
	default:
		return false
	}
	m.browsePage(pages[index])
	return true
}

func (m *Model) bodyListKey(k tea.KeyPressMsg) bool {
	if m.page != "proxies" || strings.HasPrefix(m.focus, "nav.") {
		return false
	}
	switch k.String() {
	case "up", "k", "down", "j":
		return m.proxyListKey(k)
	default:
		return false
	}
}

func (m *Model) revealBodyTarget(hit hitRegion) {
	height := max(1, dashboardGeometry(m.dashboardOptions()).height)
	if hit.Y < m.scroll {
		m.scroll = hit.Y
	}
	if hit.Y+hit.H > m.scroll+height {
		m.scroll = max(0, hit.Y+hit.H-height)
	}
}

func (m Model) contextHelp() string {
	tr := m.tr
	if m.providers.open {
		return tr("providers.keys")
	}
	if m.menu.open {
		return tr("navigation.actionMenu")
	}
	if m.sidebarOpen {
		return tr("navigation.drawer")
	}
	if m.page == "overview" && m.inlineInstallVisible() && strings.HasPrefix(m.focus, "setup.") {
		return m.inlineInstallHint()
	}
	if m.busy {
		return tr("navigation.busy")
	}
	if m.page == "overview" && m.focus == "install" && overviewSetupVisible(m.navigationOptions()) {
		return tr("dashboard.guide.hint")
	}
	if strings.HasPrefix(m.focus, "overview.card.") {
		return tr("overview.cardHint")
	}
	if m.focus == "content" {
		switch m.page {
		case "proxies", "connections", "rules", "logs":
			// Page searches remain discoverable before entering a list.
		default:
			return tr("navigation.content")
		}
	}
	if strings.HasPrefix(m.focus, "nav.") {
		return tr("navigation.sidebar")
	}
	if m.focus == "menu" {
		return tr("navigation.menu")
	}
	if m.focus == "core.start" || m.focus == "card.install" {
		return tr("navigation.default")
	}
	if m.pageHeader().owns(m.focus) {
		switch m.focus {
		case "mode.rule", "mode.global", "mode.direct":
			return tr("providers.mode_hint")
		case "rules.providers", "proxies.providers":
			return tr("providers.open_hint")
		case "proxies.search-open":
			return m.proxiesContextHelp()
		case "profiles.update-all":
			return tr("navigation.updateProfiles")
		case "conn.close-all":
			return tr("connections.close_all_hint")
		case "logs.follow":
			return tr("navigation.logsFollow")
		case "logs.retry":
			return tr("navigation.logsReconnect")
		case "logs.clear":
			return tr("logs.clear_hint")
		case "network":
			return tr("navigation.network")
		default:
			return tr("navigation.refresh")
		}
	}
	switch m.page {
	case "connections":
		if m.connections.closedView {
			return tr("navigation.connections_closed")
		}
		return tr("navigation.connections")
	case "rules":
		if m.providers.open {
			return tr("providers.keys")
		}
		return m.rulesContextHelp()
	case "logs":
		return tr("navigation.logs")
	case "profiles":
		return tr("navigation.profiles")
	case "proxies":
		return m.proxiesContextHelp()
	default:
		return tr("navigation.default")
	}
}

func (m Model) rulesContextHelp() string {
	if m.rules.editing {
		return m.tr("navigation.rules.searching")
	}
	base := m.tr("navigation.rules")
	for _, r := range m.filteredRules() {
		if m.focus != "rule."+strconv.Itoa(r.Index) {
			continue
		}
		if m.rules.expanded == ruleIdentity(r) {
			return m.tr("navigation.rules.collapse") + " · " + base
		}
		if m.ruleCanExpand(r) {
			return m.tr("navigation.rules.expand") + " · " + base
		}
		break
	}
	return base
}

func (m Model) proxiesContextHelp() string {
	tr := m.tr
	if m.proxiesDirect() {
		return tr("proxies.direct.keys")
	}
	if m.proxies.editing {
		return tr("navigation.proxies.searching")
	}
	control := ""
	switch m.focus {
	case "proxies.back":
		control = "back"
	case "proxies.directory":
		control = "directory"
	case "proxies.enter":
		control = "open"
	case "proxies.collapse_all":
		control = "collapseAll"
	case "proxies.search", "proxies.search-open":
		control = "search"
	case "proxies.clear":
		control = "clear"
	}
	for prefix, action := range map[string]string{
		"proxies.reference.": "open", "proxies.delay.": "testNode", "proxies.sort.": "sort", "proxies.test.": "testGroup",
	} {
		if strings.HasPrefix(m.focus, prefix) {
			control = action
		}
	}
	if control == "testGroup" && m.proxies.testing {
		control = "cancelTest"
	}
	if control != "" {
		return tr("navigation.proxies.control", tr("navigation.proxies.action."+control))
	}
	_, group, node, ok := m.proxyAtFocus()
	if ok && strings.HasPrefix(m.focus, "group.") {
		action := "expand"
		if m.proxyGroupExpanded(group.Name) {
			action = "collapse"
			if m.proxiesNarrow() {
				action = "directory"
			}
		}
		return tr("navigation.proxies.groups", tr("navigation.proxies.action."+action))
	}
	if ok && node != "" && strings.HasPrefix(m.focus, "node.") {
		action := "inspect"
		if group.Type == "Selector" {
			action = "select"
		}
		return tr("navigation.proxies.nodes", tr("navigation.proxies.action."+action))
	}
	return tr("navigation.proxies.browse")
}
