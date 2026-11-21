package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type proxySearchReturn struct {
	location proxyLocation
	history  []proxyLocation
}

func (m Model) proxySearchQuery() string {
	return strings.ToLower(strings.TrimSpace(m.proxies.search.Value()))
}

func proxySearchMatchesGroup(query string, group mihomo.Group) bool {
	return strings.Contains(strings.ToLower(group.Name), query) || strings.Contains(strings.ToLower(group.Type), query)
}

func (m Model) proxyMemberMatches(group mihomo.Group, node string) bool {
	query := m.proxySearchQuery()
	if query == "" || proxySearchMatchesGroup(query, group) || strings.Contains(strings.ToLower(node), query) {
		return true
	}
	if _, reference, ok := m.proxyGroup(node); ok {
		// A group's current route is an observation, not the reference's name.
		return strings.Contains(strings.ToLower(reference.Type), query)
	}
	proxy := m.snapshot.Proxies[node]
	protocol := proxy.Type
	if proxy.UDP {
		protocol += " · UDP"
	}
	return strings.Contains(strings.ToLower(protocol), query)
}

func (m Model) proxyGroupMatches(group mihomo.Group) bool {
	query := m.proxySearchQuery()
	if proxySearchMatchesGroup(query, group) {
		return true
	}
	for _, node := range group.All {
		if m.proxyMemberMatches(group, node) {
			return true
		}
	}
	return false
}

func (m Model) proxySearchVisible() bool {
	return m.proxiesReady() && !m.proxiesDirect() && m.proxies.detail == "" &&
		(m.proxies.searchReturn != nil || m.proxies.editing || m.proxySearchQuery() != "")
}

func (m Model) proxyNarrowGroupLabelVisible() bool {
	if !m.proxiesNarrow() || !m.proxiesReady() || m.proxiesDirect() || m.proxies.detail != "" || m.proxies.activeGroup == "" {
		return false
	}
	_, _, exists := m.proxyGroup(m.proxies.activeGroup)
	return exists
}

// Search is opened explicitly, keeping the unfiltered directory compact.
func (m *Model) focusProxySearch() tea.Cmd {
	if m.busy || m.quitting || !m.proxiesReady() || m.proxiesDirect() || m.proxies.detail != "" {
		return nil
	}
	if m.proxies.searchReturn == nil {
		location := m.proxyLocation()
		location.group = m.proxies.activeGroup
		m.proxies.searchReturn = &proxySearchReturn{location: location, history: append([]proxyLocation(nil), m.proxies.history...)}
	}
	m.proxies.editing = true
	m.focus, m.scroll = "proxies.search", 0
	return m.proxies.search.Focus()
}

func (m *Model) closeProxySearch() bool {
	saved := m.proxies.searchReturn
	if saved == nil {
		return false
	}
	m.proxies.searchReturn = nil
	m.proxies.editing = false
	m.proxies.search.Blur()
	m.proxies.search.Reset()
	m.proxies.searchCollapsed = nil
	m.proxies.detail, m.proxies.detailGroup = "", ""
	m.proxies.history = saved.history
	m.hover = ""
	m.restoreProxyLocation(saved.location)
	return true
}

func (m *Model) proxySearchKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "tab", "shift+tab":
		m.proxies.editing = false
		m.proxies.search.Blur()
		if key.String() == "tab" {
			m.cycleFocus(1)
		} else {
			m.cycleFocus(-1)
		}
		return nil
	case "esc":
		m.closeProxySearch()
		return nil
	case "enter":
		query := m.proxySearchQuery()
		if query == "" {
			m.closeProxySearch()
			return nil
		}
		m.proxies.editing = false
		m.proxies.search.Blur()
		m.focus = "content"
		var first hitRegion
		for _, hit := range m.contentTargets() {
			if !listTarget(hit.ID) || hit.ID == first.ID {
				continue
			}
			if first.ID != "" && strings.HasPrefix(hit.ID, "group.") {
				break
			}
			if first.ID == "" {
				first = hit
			}
			if strings.HasPrefix(hit.ID, "node.") {
				first = hit
				break
			}
			var index int
			if _, err := fmt.Sscanf(hit.ID, "group.%d", &index); err == nil &&
				(proxySearchMatchesGroup(query, m.snapshot.Groups[index]) || !m.proxyGroupExpanded(m.snapshot.Groups[index].Name)) {
				break
			}
		}
		if first.ID != "" {
			m.focus = first.ID
			m.revealBodyTarget(first)
		}
		return nil
	}
	var cmd tea.Cmd
	before := m.proxies.search.Value()
	m.proxies.search, cmd = m.proxies.search.Update(key)
	if before != m.proxies.search.Value() {
		m.proxySearchChanged()
	}
	return cmd
}

func (m *Model) proxySearchChanged() {
	m.proxies.searchCollapsed = nil
	m.scroll = 0
	if m.proxies.searchReturn != nil && m.proxySearchQuery() != "" {
		// Start a new query in the directory so it can match collapsed groups,
		// even when search was opened from a narrow member view.
		m.proxies.activeGroup = ""
		m.proxies.history = nil
	}
}

// Opening a group reveals its observed selection without selecting a node.
func (m *Model) focusProxyGroupMember(index int) {
	group := m.snapshot.Groups[index]
	m.focus = fmt.Sprintf("group.%d", index)
	members := m.proxyMembers(group)
	if len(members) > 0 {
		member := members[0]
		for _, candidate := range members {
			if group.All[candidate] == group.Now {
				member = candidate
				break
			}
		}
		m.focus = fmt.Sprintf("node.%d.%d", index, member)
	}
	m.revealContentFocus()
}

func (m Model) proxySearchBlock(width int) dashboardBlock {
	return m.searchField("proxies.search", m.tr("proxies.search_placeholder"), m.proxies.search, width, false)
}

func (m Model) proxyFixedRows(o dashboardOptions) int {
	rows, groupRows := 0, 0
	if m.proxySearchVisible() {
		rows = 1
	}
	if m.proxyNarrowGroupLabelVisible() {
		groupRows = 1
	}
	height := dashboardGeometry(o).height
	if height >= rows+groupRows+2 {
		return rows + groupRows
	}
	if height >= 3 {
		return groupRows
	}
	return 0
}

func (m Model) proxySearchFixed(rows int) bool {
	groupRows := 0
	if m.proxyNarrowGroupLabelVisible() {
		groupRows = 1
	}
	return m.proxySearchVisible() && rows > groupRows
}

func (m Model) proxiesFixedToolbar(o dashboardOptions) dashboardBlock {
	g := dashboardGeometry(o)
	var bar dashboardBlock
	if m.proxySearchFixed(o.BodyTopRows) {
		bar = m.proxySearchBlock(g.width)
	}
	if o.BodyTopRows > len(bar.lines) && m.proxyNarrowGroupLabelVisible() {
		bar.lines = append(bar.lines, themed(cellPad(m.tr("proxies.search_group", clean(m.proxies.activeGroup)), g.width), accent, bg, false))
	}
	return bar
}
