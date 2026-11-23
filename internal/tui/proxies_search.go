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

func (m *Model) proxySearchQuery() string {
	return strings.ToLower(strings.TrimSpace(m.proxies.search.Value()))
}

func (m *Model) proxySearchMatchesGroup(query string, group mihomo.Group) bool {
	if query == "" || strings.Contains(strings.ToLower(group.Name), query) || strings.Contains(strings.ToLower(group.Type), query) {
		return true
	}
	kind := proxyGroupConfigType(group.Type)
	return kind != "" && (strings.Contains(kind, query) || strings.Contains(strings.ToLower(m.tr("proxies.type."+kind)), query))
}

func (m *Model) proxySearchMatchesNode(query, node string) bool {
	if strings.Contains(strings.ToLower(node), query) {
		return true
	}
	if _, reference, ok := m.proxyGroup(node); ok {
		// A group's current route is an observation, not the reference's name.
		return m.proxySearchMatchesGroup(query, reference)
	}
	proxy := m.snapshot.Proxies[node]
	protocol := proxy.Type
	if proxy.UDP {
		protocol += " · UDP"
	}
	return strings.Contains(strings.ToLower(protocol), query)
}

func (m *Model) proxyGroupMatches(query string, group mihomo.Group) bool {
	if m.proxySearchMatchesGroup(query, group) {
		return true
	}
	for _, node := range group.All {
		if m.proxySearchMatchesNode(query, node) {
			return true
		}
	}
	return false
}

func (m *Model) proxySearchVisible() bool {
	return m.proxiesReady() && !m.proxiesDirect() &&
		(m.proxies.searchReturn != nil || m.proxies.editing || m.proxySearchQuery() != "")
}

func (m *Model) proxyNarrowGroupLabelVisible() bool {
	if !m.proxiesNarrow() || !m.proxiesReady() || m.proxiesDirect() || m.proxies.activeGroup == "" {
		return false
	}
	_, _, exists := m.proxyGroup(m.proxies.activeGroup)
	return exists
}

// Search is opened explicitly, keeping the unfiltered directory compact.
func (m *Model) focusProxySearch() tea.Cmd {
	if m.busy || m.quitting || !m.proxiesReady() || m.proxiesDirect() {
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
				(m.proxySearchMatchesGroup(query, m.snapshot.Groups[index]) || !m.proxyGroupExpanded(m.snapshot.Groups[index].Name)) {
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

func (m *Model) proxySearchBlock(width int) dashboardBlock {
	return m.searchField("proxies.search", m.tr("proxies.search_placeholder"), m.proxies.search, width, false)
}

func (m *Model) proxyModeRows(width int) int {
	if m.width >= 50 {
		return 0
	}
	actions := m.proxyModeActions()
	perRow := max(1, width/proxyModeSegmentWidth(actions))
	return (len(actions) + perRow - 1) / perRow
}

func (m *Model) proxyFixedRows(o dashboardOptions) int {
	g := dashboardGeometry(o)
	modeRows := m.proxyModeRows(g.width)
	rows, groupRows := 0, 0
	if m.proxySearchVisible() {
		rows = 1
	}
	if m.proxyNarrowGroupLabelVisible() {
		groupRows = 1
	}
	height := g.height - modeRows
	if height >= rows+groupRows+2 {
		return modeRows + rows + groupRows
	}
	if height >= 3 {
		return modeRows + groupRows
	}
	return modeRows
}

func (m *Model) proxySearchFixed(o dashboardOptions) bool {
	rows := m.proxyModeRows(dashboardBodyWidth(o))
	if m.proxyNarrowGroupLabelVisible() {
		rows++
	}
	return m.proxySearchVisible() && o.BodyTopRows > rows
}

func (m *Model) proxiesFixedToolbar(o dashboardOptions) dashboardBlock {
	width := dashboardBodyWidth(o)
	bar := m.proxiesToolbar(width)
	if m.proxySearchFixed(o) {
		bar.append(m.proxySearchBlock(width))
	}
	if o.BodyTopRows > len(bar.lines) && m.proxyNarrowGroupLabelVisible() {
		bar.lines = append(bar.lines, themed(cellPad(m.tr("proxies.search_group", clean(m.proxies.activeGroup)), width), accent, bg, false))
	}
	return bar
}
