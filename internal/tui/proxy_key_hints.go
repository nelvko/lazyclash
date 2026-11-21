package tui

import "strings"

func (m *Model) proxiesContextHelp() string {
	tr := m.tr
	if m.proxiesDirect() || !m.proxiesReady() {
		return tr("navigation.proxies.directBrowse")
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
	case "proxies.clear", "proxies.empty.clear":
		control = "clear"
	}
	_, group, node, ok := m.proxyAtFocus()
	for prefix, action := range map[string]string{
		"proxies.reference.": "open", "proxies.delay.": "testNode", "proxies.sort.": "sort", "proxies.test.": "testGroup",
	} {
		if strings.HasPrefix(m.focus, prefix) {
			control = action
		}
	}
	if control == "testGroup" {
		if ok && m.proxies.testing && m.proxies.testGroup == group.Name {
			control = "cancelTest"
		} else if !ok || !m.proxyGroupHasTestAction(group.Name) {
			control = ""
		}
	}
	if control == "testNode" && (!ok || !m.proxyNodeHasTestAction(group.Name, node)) {
		control = ""
	}
	if control == "open" {
		name := node
		if m.focus == "proxies.enter" {
			name = m.proxies.detail
		}
		if _, _, exists := m.proxyGroup(name); !exists {
			control = ""
		}
		for _, location := range m.proxies.history {
			if location.group == name {
				control = ""
				break
			}
		}
	}
	if control != "" {
		return tr("navigation.proxies.control", tr("navigation.proxies.action."+control))
	}
	if m.proxies.detail != "" {
		return tr("navigation.proxies.directBrowse")
	}
	var hints []string
	if ok && strings.HasPrefix(m.focus, "group.") {
		action := "expand"
		if m.proxyGroupExpanded(group.Name) {
			action = "collapse"
			if m.proxiesNarrow() {
				action = "directory"
			}
		}
		hints = append(hints, tr("navigation.proxies.action."+action))
	} else if ok && node != "" && strings.HasPrefix(m.focus, "node.") {
		if group.Type != "Selector" {
			hints = append(hints, tr("navigation.proxies.action.inspect"))
		} else if group.Now != node {
			hints = append(hints, tr("navigation.proxies.action.select"))
		}
	}
	if len(m.visibleProxyGroups()) == 0 {
		return tr("navigation.proxies.emptyBrowse")
	}
	hints = append(hints, tr("navigation.proxies.listBrowse"))
	if ok && node != "" {
		if m.proxyGrid(dashboardBodyWidth(m.dashboardOptions())).columns > 1 && len(m.proxyMembers(group)) > 1 {
			hints = append(hints, tr("navigation.proxies.columnsKey"))
		}
		hints = append(hints, tr("navigation.proxies.inspectKey"))
	}
	if ok {
		hints = append(hints, tr("navigation.proxies.sortKey"))
		if m.proxies.testing && m.proxies.testGroup == group.Name {
			hints = append(hints, tr("navigation.proxies.cancelTestKey"))
		} else if m.proxyGroupHasTestAction(group.Name) {
			hints = append(hints, tr("navigation.proxies.testKey"))
		}
	}
	hints = append(hints, tr("navigation.proxies.backKey"))
	return strings.Join(hints, " · ")
}

func (m *Model) proxyGroupHasTestAction(name string) bool {
	if _, supported := m.backend.(proxyDelayBackend); !supported {
		return false
	}
	_, group, ok := m.proxyGroup(name)
	if !ok {
		return false
	}
	for _, node := range group.All {
		if m.proxyTestable(node) {
			return true
		}
	}
	return false
}

func (m *Model) proxyNodeHasTestAction(group, node string) bool {
	if _, supported := m.backend.(proxyDelayBackend); !supported || !m.proxyTestable(node) {
		return false
	}
	if m.proxyNodeTestState(group, node) != "" {
		return false
	}
	key := proxyResultKey(group, node)
	return !m.proxies.testing || m.proxies.testGroup != group || !m.proxies.inflight[key] || m.proxies.inflightPaths[key] != m.proxyPathKey(node)
}
