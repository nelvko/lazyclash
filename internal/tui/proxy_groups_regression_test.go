package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestProxiesSourceAndModeChangesInvalidateOpenSortMenu(t *testing.T) {
	for _, change := range []string{"source", "mode"} {
		t.Run(change, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 70)
			m.expandProxyGroup("AUTO")
			m.focus = "group.1"
			m.openProxySort()
			if !m.proxies.sortMenu || !m.menu.open || len(m.proxies.sortReturn.expanded) != 2 {
				t.Fatal("fixture did not capture an open sorting menu and expanded groups")
			}
			if change == "source" {
				m.snapshot.Service.PID++
			} else {
				m.snapshot.Mode.Actual = "global"
			}
			m.syncProxies()
			if m.menu.open || m.proxies.sortMenu || m.proxies.sortReturn.expanded != nil || m.proxies.sortReturn.focus != "" {
				t.Fatal("a changed proxy context retained its sorting menu or return location")
			}
			m.proxyMenuResult(menuResult{Action: "name", Closed: true})
			if m.hasExpandedProxyGroups() || m.proxies.activeGroup != "" || m.proxySort("AUTO") != "default" {
				t.Fatalf("a stale sorting result restored the previous context: expanded=%v active=%s sorts=%v", m.proxies.expanded, m.proxies.activeGroup, m.proxies.sorts)
			}
		})
	}
}

func TestProxiesContextResetPreservesOtherActionMenus(t *testing.T) {
	for _, change := range []string{"source", "mode"} {
		t.Run(change, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 70)
			m.menu.show([]menuItem{{ID: "ordinary", Label: "Ordinary action"}}, 0, 0)
			if change == "source" {
				m.snapshot.Service.PID++
			} else {
				m.snapshot.Mode.Actual = "global"
			}
			m.syncProxies()
			if !m.menu.open || m.proxies.sortMenu {
				t.Fatal("proxy state cleanup dismissed an unrelated action menu")
			}
		})
	}
}

func TestProxiesScopedHeaderActionsNavigateFromTheirOwnGroup(t *testing.T) {
	for _, kind := range []string{"test", "sort"} {
		for _, direction := range []string{"up", "down"} {
			t.Run(kind+"/"+direction, func(t *testing.T) {
				m, _ := proxiesModel(t, 160, 70)
				m.expandProxyGroup("AUTO")
				m.focus = "group.1"
				if kind == "sort" {
					m.sortProxies("name")
				}
				m.focus = fmt.Sprintf("proxies.%s.1", kind)
				m.layout()
				want := "node.1.0"
				key := tea.KeyDown
				if direction == "up" {
					columns := m.proxyGrid(dashboardBodyWidth(m.dashboardOptions())).columns
					lastRow := (len(m.snapshot.Groups[0].All) - 1) / columns * columns
					want = fmt.Sprintf("node.0.%d", lastRow)
					key = tea.KeyUp
				}
				m, _ = update(m, special(key))
				if m.focus != want {
					t.Fatalf("%s from group 1's %s action jumped to %s, want %s", direction, kind, m.focus, want)
				}
			})
		}
	}
}
