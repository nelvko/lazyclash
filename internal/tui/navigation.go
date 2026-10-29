package tui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func listTarget(id string) bool {
	for _, prefix := range []string{"connection.", "rule.", "profile.", "group.", "node."} {
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
	for _, r := range m.targets {
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
	if m.page != "proxies" && m.page != "profiles" || strings.HasPrefix(m.focus, "nav.") {
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
	height := max(1, dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height}).height)
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
	switch m.page {
	case "connections":
		return "↑↓ Select · Enter Details · / Search · x Close · ? Help"
	case "rules":
		return "↑↓ Select · Enter Details · / Search · g Refresh · ? Help"
	case "logs":
		return "↑↓ Scroll/Pause · End Follow · f Pause · / Search · ? Help"
	case "proxies", "profiles":
		return "↑↓ Select · Enter Apply/Open · Tab Focus · ? Help"
	default:
		return "Tab Focus · Enter Open · 1–7 Pages · ? Help"
	}
}
