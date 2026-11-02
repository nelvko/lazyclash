package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type pageViewState struct {
	scroll               int
	focus                string
	profile, group, node string
}

func (m *Model) rememberPageView() {
	if m.pageViews == nil {
		m.pageViews = make(map[string]pageViewState)
	}
	state := m.pageViews[m.page]
	state.scroll = m.scroll
	if m.focus != "menu" && !strings.HasPrefix(m.focus, "nav.") && !strings.HasPrefix(m.focus, "notification.") {
		state.focus = m.focus
		state.profile, state.group, state.node = "", "", ""
		if index, ok := profileActionIndex(m.focus, "profile.", len(m.profiles())); ok {
			state.profile = m.profiles()[index]
		}
		var gi, ni int
		if strings.HasPrefix(m.focus, "group.") {
			gi, _ = strconv.Atoi(strings.TrimPrefix(m.focus, "group."))
			if gi >= 0 && gi < len(m.snapshot.Groups) {
				state.group = m.snapshot.Groups[gi].Name
			}
		} else if _, err := fmt.Sscanf(m.focus, "node.%d.%d", &gi, &ni); err == nil && gi >= 0 && gi < len(m.snapshot.Groups) {
			state.group = m.snapshot.Groups[gi].Name
			if ni >= 0 && ni < len(m.snapshot.Groups[gi].All) {
				state.node = m.snapshot.Groups[gi].All[ni]
			}
		}
	}
	m.pageViews[m.page] = state
}

// Browsing changes the page while retaining ownership of the navigation keys.
func (m *Model) browsePage(page string) {
	m.sidebar.reveal = true
	if page != m.page {
		m.rememberPageView()
		m.blurInlineInput()
		m.dismissProfileMenu()
		m.profilesUI.editing = false
		m.profilesUI.url.Blur()
		m.connections.editing = false
		m.connections.search.Blur()
		m.rules.editing = false
		m.rules.search.Blur()
		m.logs.editing = false
		m.logs.search.Blur()
		if !dashboardGeometry(m.dashboardOptions()).sidebar {
			m.stopRuntime()
		}
		m.stopRules()
		m.stopLogs()
		m.page = page
		m.scroll = m.pageViews[page].scroll
		m.hover = ""
	}
	m.focus = "nav." + page
}

func (m *Model) returnToNavigation() {
	m.rememberPageView()
	m.blurInlineInput()
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	m.connections.editing = false
	m.connections.search.Blur()
	m.rules.editing = false
	m.rules.search.Blur()
	m.logs.editing = false
	m.logs.search.Blur()
	m.sidebar.reveal = true
	if dashboardGeometry(m.dashboardOptions()).sidebar {
		m.focus = "nav." + m.page
	} else {
		m.focus = "menu"
		m.sidebarOpen = false
	}
}

func (m Model) navigationOptions() dashboardOptions {
	o := m.dashboardOptions()
	o.Snapshot, o.Focus, o.Hover = m.snapshot, m.focus, m.hover
	o.GuideDismissed = m.snapshot.GuideDismissed
	o.SelectedGroup = m.wizard.expanded
	o.Runtime, o.RuntimeReceived, o.RuntimePending = m.runtime.snapshot, m.runtime.received, m.runtime.pending
	o.StateUnavailable = !m.snapshotReady
	return o
}

// Keyboard targets include controls outside the visible viewport. Mouse targets
// remain clipped by renderDashboard and never include the logical containers.
func (m Model) contentTargets() []hitRegion {
	o := m.navigationOptions()
	width := dashboardBodyWidth(o)
	switch m.page {
	case "overview":
		if m.inlineInstallVisible() {
			_, targets := m.inlineInstallBody(width)
			return targets
		}
		body, actions := overviewContent(o, width)
		return append(overviewCardRegions(body, o), actions...)
	case "settings":
		_, targets := settingsContent(o, width)
		return targets
	default:
		_, targets := m.pageBody(width)
		return targets
	}
}

func (m Model) restoredContentFocus(state pageViewState) string {
	if state.profile != "" {
		for index, name := range m.profiles() {
			if name == state.profile {
				return "profile." + strconv.Itoa(index)
			}
		}
		return ""
	}
	if state.group != "" {
		for gi, group := range m.snapshot.Groups {
			if group.Name != state.group {
				continue
			}
			if state.node == "" {
				return fmt.Sprintf("group.%d", gi)
			}
			for ni, node := range group.All {
				if node == state.node {
					return fmt.Sprintf("node.%d.%d", gi, ni)
				}
			}
			return fmt.Sprintf("group.%d", gi)
		}
		return ""
	}
	return state.focus
}

func (m *Model) enterContent() {
	m.sidebarOpen = false
	targets := m.contentTargets()
	remembered := m.restoredContentFocus(m.pageViews[m.page])
	if m.page == "overview" && m.inlineInstallVisible() && m.wizard.step == stepVersion && m.wizard.err != "" && remembered == "setup.install" {
		remembered = "setup.resolve"
	}
	chosen := ""
	if remembered == "content" {
		chosen = remembered
	}
	for _, target := range targets {
		if target.ID == remembered {
			chosen = remembered
			break
		}
	}
	if chosen == "" {
		switch m.page {
		case "overview":
			if m.inlineInstallVisible() {
				chosen = "setup.install"
				if m.wizard.step == stepInstalling {
					chosen = "setup.cancel"
				} else if m.wizard.step == stepResult {
					chosen = "setup.start"
					if m.wizard.err != "" {
						chosen = "setup.retry"
					}
				} else if m.wizard.step == stepConfirm && m.wizard.plan.BlockedReason != "" {
					chosen = "setup.later"
				} else if m.wizard.step == stepVersion && m.wizard.err != "" {
					chosen = "setup.resolve"
				}
				break
			}
			if overviewSetupVisible(m.navigationOptions()) {
				chosen = "install"
				break
			}
			for _, target := range targets {
				if strings.HasPrefix(target.ID, "overview.card.") {
					chosen = target.ID
					break
				}
			}
		case "logs":
			chosen = "logs.list"
		case "profiles":
			for i, name := range m.profiles() {
				if name == m.snapshot.Applied {
					chosen = "profile." + strconv.Itoa(i)
					break
				}
			}
			if chosen == "" {
				for _, target := range targets {
					if listTarget(target.ID) {
						chosen = target.ID
						break
					}
				}
			}
		case "connections":
			if m.connections.selected != "" {
				chosen = "connection." + m.connections.selected
			}
		case "rules":
			for _, rule := range m.filteredRules() {
				if ruleIdentity(rule) == m.rules.selected {
					chosen = "rule." + strconv.Itoa(rule.Index)
					break
				}
			}
		case "proxies":
			for _, target := range targets {
				if listTarget(target.ID) {
					chosen = target.ID
					break
				}
			}
		case "settings":
			for _, target := range targets {
				if strings.HasPrefix(target.ID, "language.") || target.ID == "key-hints.toggle" {
					chosen = target.ID
					break
				}
			}
		}
	}
	if chosen == "" {
		chosen = "content"
	}
	m.focus = chosen
	if chosen == "profiles.url" {
		m.profilesUI.editing = true
		m.profilesUI.url.Focus()
	}
	for _, target := range targets {
		if target.ID == chosen {
			m.revealBodyTarget(target)
			break
		}
	}
	m.rememberPageView()
}

func (m *Model) revealContentFocus() {
	if m.helpOpen || m.form != "" || m.wizard.open || m.wizard.networkOpen || m.ports.open || m.notification.panel.open || m.menu.open || m.sidebarOpen {
		return
	}
	for _, target := range m.contentTargets() {
		if target.ID == m.focus {
			m.revealBodyTarget(target)
			return
		}
	}
}

func (m *Model) contentNavigationKey(k tea.KeyPressMsg) bool {
	if strings.HasPrefix(m.focus, "overview.card.") {
		cards := overviewCardRegionsFromModel(*m)
		current := 0
		for i, card := range cards {
			if card.ID == m.focus {
				current = i
				break
			}
		}
		next := current
		switch k.String() {
		case "down", "j":
			next = min(len(cards)-1, current+1)
		case "up", "k":
			next = max(0, current-1)
		case "home":
			next = 0
		case "end":
			next = len(cards) - 1
		case "enter", "space", " ":
			m.enterOverviewCard()
			return true
		default:
			return false
		}
		if len(cards) > 0 {
			m.focus = cards[next].ID
			m.revealBodyTarget(cards[next])
		}
		return true
	}
	if m.focus == "content" {
		switch k.String() {
		case "enter", "space", " ":
			return true
		case "down", "j":
			for _, target := range m.contentTargets() {
				if listTarget(target.ID) {
					m.focus = target.ID
					if strings.HasPrefix(target.ID, "connection.") {
						m.connections.selected = strings.TrimPrefix(target.ID, "connection.")
					}
					if strings.HasPrefix(target.ID, "rule.") {
						for _, rule := range m.filteredRules() {
							if target.ID == "rule."+strconv.Itoa(rule.Index) {
								m.rules.selected = ruleIdentity(rule)
								break
							}
						}
					}
					m.revealBodyTarget(target)
					return true
				}
			}
		}
	}
	return false
}

func overviewCardRegionsFromModel(m Model) []hitRegion {
	if m.inlineInstallVisible() {
		return nil
	}
	o := m.navigationOptions()
	body, _ := overviewContent(o, dashboardBodyWidth(o))
	return overviewCardRegions(body, o)
}

// The existing frames are the card geometry; keyboard navigation adds no new
// clickable surface and cannot overlap a card's real action buttons.
func overviewCardRegions(body string, o dashboardOptions) []hitRegion {
	lines := strings.Split(body, "\n")
	names := []string{"dashboard.control.core", "dashboard.control.routingMode", "dashboard.field.tun", "dashboard.field.systemProxy", "dashboard.profile.title", "dashboard.groups.title", "dashboard.traffic.title", "dashboard.kernel.title", "dashboard.system.title"}
	if overviewSetupVisible(o) {
		names = []string{"dashboard.guide.title"}
	}
	var cards []hitRegion
	for y, line := range lines {
		plain := ansi.Strip(line)
		for x, r := range plain {
			if r != '┌' {
				continue
			}
			// Frame rows contain only single-cell border characters; x is a byte
			// position, so convert the prefix before using terminal cell coordinates.
			left := ansi.StringWidth(plain[:x])
			tail := plain[x+len("┌"):]
			right := strings.Index(tail, "┐")
			if right < 0 {
				continue
			}
			width := ansi.StringWidth(tail[:right]) + 2
			bottom := y + 1
			for ; bottom < len(lines); bottom++ {
				if ansi.Strip(ansi.Cut(lines[bottom], left, left+1)) == "└" {
					break
				}
			}
			if bottom >= len(lines) {
				continue
			}
			id := fmt.Sprintf("overview.card.%d", len(cards))
			if len(cards) < len(names) {
				id = "overview.card." + names[len(cards)]
			}
			cards = append(cards, hitRegion{ID: id, X: left, Y: y, W: width, H: bottom - y + 1})
		}
	}
	return cards
}

func (m *Model) enterOverviewCard() {
	for _, card := range overviewCardRegionsFromModel(*m) {
		if card.ID != m.focus {
			continue
		}
		for _, target := range m.contentTargets() {
			if strings.HasPrefix(target.ID, "overview.card.") {
				continue
			}
			if target.X >= card.X && target.X < card.X+card.W && target.Y >= card.Y && target.Y < card.Y+card.H {
				m.focus = target.ID
				m.revealBodyTarget(target)
				return
			}
		}
	}
}

func (m *Model) decorateContentFocus(g dashboardLayout, overlays []notificationOverlay) {
	covered := func(x, y int) bool {
		for _, overlay := range overlays {
			b := overlay.Bounds
			if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
				return true
			}
		}
		return false
	}
	if m.focus == "content" {
		rows := strings.Split(m.screen, "\n")
		x := max(0, g.x-1)
		if !covered(x, g.y) {
			putRows(rows, x, g.y, themed("›", focus, bg, false), m.width)
		}
		m.screen = strings.Join(rows, "\n")
		return
	}
	if !strings.HasPrefix(m.focus, "overview.card.") {
		return
	}
	rows := strings.Split(m.screen, "\n")
	highlight := func(x, y, width int) {
		for offset := 0; offset < width; {
			if covered(x+offset, y) {
				offset++
				continue
			}
			start := offset
			for offset < width && !covered(x+offset, y) {
				offset++
			}
			value := ansi.Cut(rows[y], x+start, x+offset)
			putRows(rows, x+start, y, themed(ansi.Strip(value), focus, panel, false), m.width)
		}
	}
	for _, card := range overviewCardRegionsFromModel(*m) {
		if card.ID != m.focus {
			continue
		}
		for row := 0; row < card.H; row++ {
			y := g.y + card.Y + row - m.scroll
			if y < g.y || y >= g.y+g.height {
				continue
			}
			x := g.x + card.X
			if row == 0 || row == card.H-1 {
				highlight(x, y, card.W)
			} else {
				highlight(x, y, 1)
				highlight(x+card.W-1, y, 1)
			}
		}
	}
	m.screen = strings.Join(rows, "\n")
}

func (m *Model) requestQuit() tea.Cmd {
	m.stopRuntime()
	m.stopRules()
	m.stopLogs()
	m.stopCoreStatus()
	m.quitting = true
	if m.busy {
		m.cancelTask(true)
		return nil
	}
	if m.keyHints.pending {
		return nil
	}
	return tea.Quit
}

func (m *Model) repairPageFocus() {
	if m.helpOpen || m.form != "" || m.wizard.open || m.wizard.networkOpen || m.ports.open || m.notification.panel.open || m.menu.open {
		return
	}
	state := m.pageViews[m.page]
	if !m.busy && m.focus != "content" && m.focus != "menu" && !strings.HasPrefix(m.focus, "nav.") && !strings.HasPrefix(m.focus, "notification.") {
		valid := m.pageHeader().owns(m.focus)
		for _, target := range m.contentTargets() {
			if target.ID == m.focus {
				valid = true
				break
			}
		}
		if !valid {
			m.enterContent()
			return
		}
	}
	if state.focus != m.focus || (state.profile == "" && state.group == "") {
		return
	}
	restored := m.restoredContentFocus(state)
	if restored != "" {
		m.focus = restored
		return
	}
	// The focused object was removed; prefer the nearest surviving list item.
	list := []hitRegion{}
	for _, target := range m.contentTargets() {
		if listTarget(target.ID) {
			list = append(list, target)
		}
	}
	index := 0
	if state.profile != "" {
		index, _ = strconv.Atoi(strings.TrimPrefix(state.focus, "profile."))
	}
	if state.group != "" {
		fmt.Sscanf(state.focus, "group.%d", &index)
	}
	m.focus = "content"
	if len(list) > 0 {
		m.focus = list[min(max(0, index), len(list)-1)].ID
	}
}
