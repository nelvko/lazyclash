package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m Model) proxySearchQuery() string {
	return strings.ToLower(strings.TrimSpace(m.proxies.search.Value()))
}

func (m Model) proxyMemberMatches(group mihomo.Group, node string) bool {
	query := m.proxySearchQuery()
	return query == "" || strings.Contains(strings.ToLower(group.Name), query) ||
		strings.Contains(strings.ToLower(group.Type), query) ||
		strings.Contains(strings.ToLower(node), query) ||
		strings.Contains(strings.ToLower(m.proxyMeta(node)), query)
}

func (m Model) proxyGroupMatches(group mihomo.Group) bool {
	query := m.proxySearchQuery()
	if query == "" || strings.Contains(strings.ToLower(group.Name), query) || strings.Contains(strings.ToLower(group.Type), query) {
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
	return m.proxiesReady() && !m.proxiesDirect() && m.proxies.detail == "" && (m.proxies.editing || m.proxySearchQuery() != "")
}

func (m Model) proxyGroupLabelVisible() bool {
	if !m.proxiesReady() || m.proxiesDirect() || m.proxies.detail != "" {
		return false
	}
	for _, index := range m.visibleProxyGroups() {
		if m.proxyGroupExpanded(m.snapshot.Groups[index].Name) {
			return true
		}
	}
	return false
}

// Search is opened explicitly, keeping the unfiltered directory compact.
func (m *Model) focusProxySearch() tea.Cmd {
	if m.busy || m.quitting || !m.proxiesReady() || m.proxiesDirect() || m.proxies.detail != "" {
		return nil
	}
	m.proxies.editing = true
	m.focus, m.scroll = "proxies.search", 0
	return m.proxies.search.Focus()
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
	case "enter", "esc":
		m.proxies.editing = false
		m.proxies.search.Blur()
		m.focus = "content"
		for _, hit := range m.contentTargets() {
			if listTarget(hit.ID) {
				m.focus = hit.ID
				m.revealBodyTarget(hit)
				break
			}
		}
		return nil
	}
	var cmd tea.Cmd
	before := m.proxies.search.Value()
	m.proxies.search, cmd = m.proxies.search.Update(key)
	if before != m.proxies.search.Value() {
		m.proxies.searchCollapsed = nil
	}
	m.scroll = 0
	return cmd
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
		rows = inputFieldRows(min(inputFieldMaxWidth, dashboardBodyWidth(o)), 0, true)
	}
	if m.proxyGroupLabelVisible() {
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
	if m.proxyGroupLabelVisible() {
		groupRows = 1
	}
	return m.proxySearchVisible() && rows > groupRows
}

func (m Model) proxiesFixedToolbar(width, rows int) dashboardBlock {
	var bar dashboardBlock
	if m.proxySearchFixed(rows) {
		bar = m.proxySearchBlock(width)
	}
	if rows > len(bar.lines) && m.proxyGroupLabelVisible() {
		if group := m.proxyVisibleGroup(width); group != "" {
			bar.lines = append(bar.lines, themed(cellPad(m.tr("proxies.search_group", clean(group)), width), accent, bg, false))
		} else {
			bar.blank()
		}
	}
	return bar
}

// Follow the section at the viewport's top, not keyboard focus: wheel scrolling
// intentionally keeps focus unchanged, and several wide-screen groups may be open.
func (m Model) proxyVisibleGroup(width int) string {
	body, hits := m.proxiesBody(width)
	scroll := min(m.scroll, max(0, len(strings.Split(body, "\n"))-dashboardGeometry(m.dashboardOptions()).height))
	group, lastY := "", -1
	for _, hit := range hits {
		var index int
		if _, err := fmt.Sscanf(hit.ID, "group.%d", &index); err != nil || index < 0 || index >= len(m.snapshot.Groups) {
			continue
		}
		if group == "" || hit.Y <= scroll && hit.Y > lastY {
			group, lastY = m.snapshot.Groups[index].Name, hit.Y
		}
	}
	return group
}
