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
func (m *Model) focusTargets() []hitRegion {
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
		if target.tip.text == "" && target.ID != "menu" && target.ID != "logo" && !strings.HasPrefix(target.ID, "nav.") && (target.Y < g.y || strings.HasPrefix(target.ID, "notification.")) {
			controls = append(controls, target)
		}
	}
	body := m.contentTargets()
	var networkCard hitRegion
	for _, target := range body {
		target.X += g.x
		target.Y += g.y
		if target.ID == "overview.card.dashboard.overview.network" {
			networkCard = target
		}
		if listTarget(target.ID) || strings.HasPrefix(target.ID, "proxies.delay.") || strings.HasPrefix(target.ID, "overview.card.") || target.ID == "logs.list" {
			list = append(list, target)
		} else {
			controls = append(controls, target)
		}
	}
	// The install form and the empty profile library already have actionable
	// controls. A synthetic list stop would interrupt their field tab order.
	controlsOnly := m.page == "overview" && m.inlineInstallVisible() || m.page == "profiles" && m.snapshotReady
	if len(list) == 0 && !controlsOnly || m.focus == "content" {
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
	tabPosition := func(r hitRegion) (int, int) {
		// Treat the network card as one group in the page order. Its renderer
		// supplies the controls of each section together at either width.
		if networkCard.W > 0 && r.ID != networkCard.ID && !strings.HasPrefix(r.ID, "notification.") && regionsOverlap(r, networkCard) {
			return networkCard.Y + 1, networkCard.X
		}
		// Read framed searches at their text row, alongside scope controls,
		// rather than placing the frame's top before controls to its left.
		if r.H == 3 && searchClearID(r.ID) != "" {
			r.Y++
		}
		return r.Y, r.X
	}
	sort.SliceStable(controls[1:], func(i, j int) bool {
		ay, ax := tabPosition(controls[i+1])
		by, bx := tabPosition(controls[j+1])
		if ay == by {
			return ax < bx
		}
		return ay < by
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

func (m *Model) contextHelp() string {
	if m.notification.panel.open {
		return m.notification.panel.contextHelp(m.locale)
	}
	if m.form == "profile-settings" {
		return m.tr("profile_settings.footer")
	}
	if m.providers.open {
		return m.providersContextHelp()
	}
	if m.menu.open {
		if m.profilesUI.menuProfile != "" && m.menu.selected >= 0 && m.menu.items[m.menu.selected].ID == "edit-config" && m.profileInfo(m.profilesUI.menuProfile).Kind == "remote" {
			return m.tr("profile_editor.replaced")
		}
		if m.menu.selected < 0 {
			return m.tr("navigation.hints.menuClose")
		}
		return m.tr("navigation.actionMenu")
	}
	if m.ports.open {
		return m.portsContextHelp()
	}
	if m.page == "connections" && (m.connections.detail != nil || m.connections.confirming) {
		return m.connectionsContextHelp()
	}
	if m.page == "overview" && m.inlineInstallVisible() && strings.HasPrefix(m.focus, "setup.") {
		return m.inlineInstallHint()
	}
	// Editors own Enter, Escape and printable shortcut keys before page actions.
	switch {
	case m.page == "profiles" && m.focus == "profiles.url" && !m.busy && !m.quitting && !m.sidebarOpen:
		return m.profilesContextHelp()
	case m.page == "connections" && m.connections.editing:
		return m.connectionsContextHelp()
	case m.page == "proxies" && m.proxies.editing:
		return m.proxiesContextHelp()
	case m.page == "rules" && m.rules.editing:
		return m.rulesContextHelp()
	case m.page == "logs" && m.logs.editing:
		return m.logsContextHelp()
	}
	if m.sidebarOpen {
		return m.tr("navigation.drawer")
	}
	if strings.HasPrefix(m.focus, "notification.") {
		return m.notificationContextHelp()
	}
	if strings.HasPrefix(m.focus, "nav.") || m.focus == "menu" {
		hint := m.tr("navigation.sidebar")
		if m.focus == "menu" {
			hint = m.tr("navigation.menu")
		}
		if m.page == "proxies" && m.canFocusProxySearch() {
			hint += " · " + m.tr("navigation.proxies.searchKey")
		}
		return hint
	}
	if m.busy {
		return m.tr("navigation.hints.busy")
	}
	if m.page == "overview" && m.focus == "install" && overviewSetupVisible(m.navigationOptions()) {
		return m.tr("dashboard.guide.hint")
	}
	if m.focus == "core.start" || m.focus == "card.install" || m.focus == "snapshot.retry" {
		return m.tr("navigation.default")
	}
	if m.page == "proxies" || m.page == "rules" {
		if header := m.pageHeader(); header.owns(m.focus) {
			if !header.enabled(m.focus) {
				return m.tr("navigation.hints.browse")
			}
			switch m.focus {
			case "mode.rule", "mode.global", "mode.direct":
				return m.tr("providers.mode_hint")
			case "rules.providers", "proxies.providers":
				return m.tr("providers.open_hint")
			}
		}
	}
	switch m.page {
	case "connections":
		return m.connectionsContextHelp()
	case "rules":
		return m.rulesContextHelp()
	case "logs":
		return m.logsContextHelp()
	case "profiles":
		return m.profilesContextHelp()
	case "proxies":
		return m.proxiesContextHelp()
	case "overview":
		return m.overviewContextHelp()
	case "settings":
		return m.settingsContextHelp()
	default:
		return m.tr("navigation.default")
	}
}

func (m *Model) rulesContextHelp() string {
	if m.rules.editing {
		return m.tr("navigation.rules.searching")
	}
	if m.focus == "rules.empty.clear" {
		return m.tr("empty.rules.clear_hint")
	}
	base := m.tr("navigation.rules")
	list := m.rulesList()
	index, found := list.positions[m.focus]
	if !found {
		return base
	}
	rule := list.rows[index]
	if _, linked := m.ruleProxyGroup(rule.Proxy); linked {
		base = m.tr("navigation.rules.open_proxy") + " · " + base
	}
	if m.rules.expanded == ruleIdentity(rule) {
		return m.tr("navigation.rules.collapse") + " · " + base
	}
	if m.ruleCanExpand(rule) {
		return m.tr("navigation.rules.expand") + " · " + base
	}
	return base
}
