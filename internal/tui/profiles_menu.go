package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

func (m Model) profileMenuAvailable() bool {
	return m.page == "profiles" && m.snapshotReady && !m.busy && !m.quitting &&
		!m.sidebarOpen && !m.helpOpen && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && m.form == ""
}

func (m Model) profileMenuItems(id string) []menuItem {
	info := m.profileInfo(id)
	apply := menuItem{ID: "apply", Label: m.tr("profile_menu.use")}
	if id == m.snapshot.Applied {
		apply.DisabledReason = m.tr("profile_menu.already_selected")
		if info.Pending || m.snapshot.ProfilePending {
			apply.Label, apply.DisabledReason = m.tr("profile_menu.apply_changes"), ""
		}
	}
	if !m.snapshot.Installed {
		apply.DisabledReason = m.tr("profile_menu.install_first")
	}
	items := []menuItem{apply}
	if id != "default" {
		update := menuItem{ID: "refresh", Label: m.tr("profile_menu.update")}
		if !info.Refreshable {
			update.DisabledReason = m.tr("profile_menu.no_address")
		}
		items = append(items, update,
			menuItem{ID: "rename", Label: m.tr("profile_menu.rename")},
			menuItem{ID: "description", Label: m.tr("profile_menu.description")},
			menuItem{ID: "address", Label: m.tr("profile_menu.address")},
			menuItem{ID: "overrides", Label: m.tr("profile_menu.overrides")})
	}
	items = append(items, menuItem{ID: "details", Label: m.tr("profile_menu.details")})
	if id != "default" {
		remove := menuItem{ID: "delete", Label: m.tr("profile_menu.delete"), Danger: true}
		if id == m.snapshot.Applied {
			remove.DisabledReason = m.tr("profile_menu.switch_first")
		}
		items = append(items, remove)
	}
	return items
}

func (m *Model) openProfileMenu(index, x, y int) {
	if !m.profileMenuAvailable() || index < 0 || index >= len(m.profiles()) {
		return
	}
	id := m.profiles()[index]
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	m.profilesUI.menuProfile = id
	m.focus, m.hover = fmt.Sprintf("profile.%d", index), ""
	m.menu.show(m.profileMenuItems(id), x, y)
}

func (m *Model) openProfileMenuAt(x, y int) {
	if !m.profileMenuAvailable() {
		return
	}
	options := m.dashboardOptions()
	_, options.BodyTargets = m.pageBody(dashboardGeometry(options).width)
	options.Scroll = m.scroll
	for _, notification := range notificationLayout(options) {
		toast := notification.Bounds
		if x >= toast.X && x < toast.X+toast.W && y >= toast.Y && y < toast.Y+toast.H {
			return
		}
	}
	for _, hit := range m.profileMenuCardRegions() {
		if x < hit.X || x >= hit.X+hit.W || y < hit.Y || y >= hit.Y+hit.H {
			continue
		}
		if index, ok := profileActionIndex(hit.ID, "profile.", len(m.profiles())); ok {
			m.openProfileMenu(index, x, y)
		}
		return
	}
}

// Use visible card geometry so a toast covering one corner does not disable
// the rest of the card. The toast's own cells remain excluded above.
func (m Model) profileMenuCardRegions() []hitRegion {
	g := dashboardGeometry(m.dashboardOptions())
	var regions []hitRegion
	for _, hit := range m.profileGridCards() {
		hit.X += g.x
		hit.Y += g.y - m.scroll
		if visible, ok := clipRegion(hit, g.x, g.y, g.width, g.height); ok {
			regions = append(regions, visible)
		}
	}
	return regions
}

func (m *Model) openProfileMenuForCard(index int) {
	if !m.profileMenuAvailable() || index < 0 || index >= len(m.profiles()) {
		return
	}
	cardID := fmt.Sprintf("profile.%d", index)
	m.focusProfileControl(cardID)
	m.layout()
	for _, hit := range m.profileMenuCardRegions() {
		if hit.ID == cardID {
			m.openProfileMenu(index, hit.X+hit.W-1, hit.Y+1)
			return
		}
	}
}

func (m *Model) profileMenuKey(k tea.KeyPressMsg) bool {
	if !m.profileMenuAvailable() || m.profilesUI.editing {
		return false
	}
	switch k.String() {
	case "m", "shift+f10", "menu":
		for _, prefix := range []string{"profile.", "profiles.menu.", "profiles.refresh."} {
			if index, ok := profileActionIndex(m.focus, prefix, len(m.profiles())); ok {
				m.openProfileMenuForCard(index)
				return true
			}
		}
	}
	return false
}

func (m *Model) dismissProfileMenu() {
	id := m.profilesUI.menuProfile
	m.menu.dismiss()
	m.profilesUI.menuProfile, m.hover = "", ""
	if id == "" || m.page != "profiles" {
		return
	}
	for index, profile := range m.profiles() {
		if profile == id {
			m.focusProfileControl(fmt.Sprintf("profile.%d", index))
			return
		}
	}
	m.focusProfileControl("profile.0")
}

func (m *Model) reconcileProfileMenu() {
	if m.proxies.sortMenu {
		if m.page != "proxies" || m.busy {
			m.menu.dismiss()
			m.proxies.sortMenu = false
		}
		return
	}
	if !m.menu.open {
		return
	}
	if !m.profileMenuAvailable() {
		m.dismissProfileMenu()
		return
	}
	for index, id := range m.profiles() {
		if id != m.profilesUI.menuProfile {
			continue
		}
		m.focus = fmt.Sprintf("profile.%d", index)
		// Keep actions current without changing the identity under an open menu.
		m.menu.setItems(m.profileMenuItems(id))
		return
	}
	m.dismissProfileMenu()
}

func (m *Model) profileMenuResult(result menuResult) tea.Cmd {
	if m.proxies.sortMenu {
		return m.proxyMenuResult(result)
	}
	if !result.Closed {
		return nil
	}
	id := m.profilesUI.menuProfile
	m.dismissProfileMenu()
	if result.Action == "" || !m.profileMenuAvailable() {
		return nil
	}
	for _, item := range m.profileMenuItems(id) {
		if item.ID != result.Action || item.DisabledReason != "" {
			continue
		}
		for index, profile := range m.profiles() {
			if profile != id {
				continue
			}
			prefix := map[string]string{
				"apply": "profiles.apply.", "refresh": "profiles.update.",
				"rename": "profiles.rename.", "description": "profiles.description.", "address": "profiles.url-edit.",
				"overrides": "profiles.overrides.", "details": "profile.", "delete": "profiles.delete.",
			}[result.Action]
			_, cmd := m.profileAction(fmt.Sprintf("%s%d", prefix, index))
			return cmd
		}
	}
	return nil
}
