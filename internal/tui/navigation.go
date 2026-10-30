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
	for _, prefix := range []string{"connection.", "rule.", "group.", "node."} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// Navigation and the active list each form one Tab stop; arrows move inside
// those groups. All other controls retain their visual reading order.
func (m Model) focusTargets() []hitRegion {
	var nav, list []hitRegion
	var controls []hitRegion
	targets := m.targets
	if m.page == "profiles" && m.form == "" && !m.helpOpen && !m.ports.open && !m.wizard.open && !m.wizard.networkOpen && !m.sidebarOpen && m.width >= 20 && m.height >= 7 {
		// Keyboard focus can reach controls below the fold. Mouse targets stay
		// clipped to the viewport; cycleFocus reveals the chosen body control.
		targets = nil
		for _, target := range m.targets {
			if strings.HasPrefix(target.ID, "nav.") || target.ID == "menu" {
				targets = append(targets, target)
			}
		}
		g := dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: m.page})
		for _, target := range m.targets {
			if target.ID != "menu" && !strings.HasPrefix(target.ID, "nav.") && target.Y < g.y {
				targets = append(targets, target)
			}
		}
		_, body := m.pageBody(g.width)
		for _, target := range body {
			target.X += g.x
			target.Y += g.y
			targets = append(targets, target)
		}
	}
	for _, r := range targets {
		switch {
		case strings.HasPrefix(r.ID, "nav."):
			nav = append(nav, r)
		case listTarget(r.ID):
			list = append(list, r)
		default:
			controls = append(controls, r)
		}
	}
	choose := func(items []hitRegion, fallback string) hitRegion {
		selected := items[0]
		for _, item := range items {
			if item.ID == fallback {
				selected = item
			}
			if item.ID == m.focus {
				return item
			}
		}
		return selected
	}
	if len(list) > 0 {
		controls = append(controls, choose(list, ""))
	}
	sort.SliceStable(controls, func(i, j int) bool {
		if controls[i].Y == controls[j].Y {
			return controls[i].X < controls[j].X
		}
		return controls[i].Y < controls[j].Y
	})
	if len(nav) > 0 {
		selected := choose(nav, "nav."+m.page)
		if m.sidebarOpen {
			return []hitRegion{selected}
		}
		controls = append([]hitRegion{selected}, controls...)
	}
	return controls
}

func (m *Model) navigationKey(k tea.KeyPressMsg) bool {
	if m.focus == "menu" {
		switch k.String() {
		case "up", "k", "down", "j":
			m.sidebarOpen = true
			m.focus = "nav." + m.page
		}
	}
	if !strings.HasPrefix(m.focus, "nav.") {
		return false
	}
	pages := navPages()
	index := 0
	for i, page := range pages {
		if m.focus == "nav."+page {
			index = i
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
	m.focus = "nav." + pages[index]
	return true
}

func (m *Model) bodyListKey(k tea.KeyPressMsg) bool {
	if m.page != "proxies" || strings.HasPrefix(m.focus, "nav.") {
		return false
	}
	delta := 0
	switch k.String() {
	case "up", "k":
		delta = -1
	case "down", "j":
		delta = 1
	default:
		return false
	}
	_, hits := m.pageBody(dashboardBodyWidth(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}))
	var list []hitRegion
	index := -1
	for _, hit := range hits {
		if !listTarget(hit.ID) {
			continue
		}
		if hit.ID == m.focus {
			index = len(list)
		}
		list = append(list, hit)
	}
	if len(list) == 0 {
		return false
	}
	index = min(len(list)-1, max(0, index+delta))
	m.focus = list[index].ID
	m.revealBodyTarget(list[index])
	return true
}

func (m *Model) revealBodyTarget(hit hitRegion) {
	height := max(1, dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}).height)
	if hit.Y < m.scroll {
		m.scroll = hit.Y
	}
	if hit.Y+hit.H > m.scroll+height {
		m.scroll = max(0, hit.Y+hit.H-height)
	}
}

func (m Model) contextHelp() string {
	if m.busy {
		return "Esc Cancel · Ctrl+C Quit after cleanup"
	}
	if strings.HasPrefix(m.focus, "nav.") || m.sidebarOpen {
		return "↑↓ Navigate · Enter Open · Tab Content · ? Help"
	}
	if m.focus == "menu" {
		return "Enter Menu · 1–7 Pages · Tab Content · ? Help"
	}
	if m.pageHeader().owns(m.focus) {
		switch m.focus {
		case "profiles.update-all":
			return "Enter Update profiles · Tab Content · ? Help"
		case "logs.follow":
			return "Enter Pause/Resume capture · Tab Content · ? Help"
		case "logs.retry":
			return "Enter Reconnect · Tab Content · ? Help"
		case "network":
			return "Enter Network settings · Tab Content · ? Help"
		default:
			return "Enter Refresh · Tab Content · ? Help"
		}
	}
	switch m.page {
	case "connections":
		return "↑↓ Select · Enter Details · / Search · x Close · ? Help"
	case "rules":
		return "↑↓ Select · Enter Details · / Search · g Refresh · ? Help"
	case "logs":
		return "↑↓ Scroll/Pause · End Follow · f Pause · / Search · ? Help"
	case "profiles":
		return "↑↓←→ Select · Enter Details · a Add · Tab Actions · ? Help"
	case "proxies":
		return "↑↓ Select · Enter Apply/Open · Tab Focus · ? Help"
	default:
		return "Tab Focus · Enter Open · 1–7 Pages · ? Help"
	}
}
