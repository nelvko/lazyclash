package tui

import (
	"fmt"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func proxySearchScrolledReferenceModel(t *testing.T, width int) Model {
	t.Helper()
	m := proxyHoverModel(t, 120)
	m.width, m.height = width, 24
	m.snapshot.Groups[1].All = append([]string(nil), m.snapshot.Groups[0].All[:60]...)
	m.snapshot.Groups[1].Now = m.snapshot.Groups[1].All[25]
	m.focus = "node.0.0"
	m.browseProxyReference("AUTO")
	m.focus = "node.1.25"
	m.revealContentFocus()
	m.layout()
	if m.scroll <= 0 || len(m.proxies.history) == 0 || m.proxies.activeGroup != "AUTO" {
		t.Fatalf("fixture did not enter a scrolled referenced group: scroll=%d active=%s history=%+v", m.scroll, m.proxies.activeGroup, m.proxies.history)
	}
	return m
}

func TestProxiesSearchEscapeRestoresOriginalBrowsingSession(t *testing.T) {
	for _, width := range []int{80, 160} {
		for _, entry := range []string{"slash", "header"} {
			for _, exit := range []string{"editor", "results"} {
				t.Run(fmt.Sprintf("%d/%s/%s", width, entry, exit), func(t *testing.T) {
					m := proxySearchScrolledReferenceModel(t, width)
					location := m.proxyLocation()
					activeGroup := m.proxies.activeGroup
					history := append([]proxyLocation(nil), m.proxies.history...)
					for i := range history {
						history[i].expanded = cloneProxyExpansion(history[i].expanded)
					}
					if entry == "header" {
						m, _ = click(m, findTarget(t, m, "proxies.search-open"))
					} else {
						m, _ = update(m, key("/"))
					}
					if !m.proxies.editing || m.focus != "proxies.search" {
						t.Fatal("explicit search entry did not open its editor")
					}
					m, _ = update(m, tea.PasteMsg{Content: "节点 059"})
					if exit == "results" {
						m, _ = update(m, special(tea.KeyEnter))
						if m.proxies.editing || m.proxySearchQuery() == "" {
							t.Fatal("Enter did not leave the filtered search results active")
						}
					}
					m, _ = update(m, special(tea.KeyEscape))
					if m.proxies.editing || m.proxySearchVisible() || m.proxySearchQuery() != "" {
						t.Fatalf("Escape did not end the complete search session: editing=%t query=%q", m.proxies.editing, m.proxySearchQuery())
					}
					if !reflect.DeepEqual(m.proxyLocation(), location) || m.proxies.activeGroup != activeGroup ||
						!reflect.DeepEqual(m.proxies.history, history) {
						t.Fatalf("Escape changed the original browsing location: got=%+v active=%s history=%+v want=%+v active=%s history=%+v", m.proxyLocation(), m.proxies.activeGroup, m.proxies.history, location, activeGroup, history)
					}
				})
			}
		}
	}
}

func TestProxiesSearchEnterFocusesFirstMatchingResult(t *testing.T) {
	for _, test := range []struct {
		name, query, focus string
		width              int
	}{
		{"wide node match", "US 01", "node.0.5", 160},
		{"wide group match", "PROXY", "group.0", 160},
		{"narrow directory", "HK 02", "group.0", 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, backend := proxiesModel(t, test.width, 24)
			setProxyTestGroup(&m, "")
			m.layout()
			m, _ = update(m, key("/"))
			m, _ = update(m, tea.PasteMsg{Content: test.query})
			m, _ = update(m, special(tea.KeyEnter))
			if m.proxies.editing || m.focus != test.focus || backend.selections != 0 || m.busy {
				t.Fatalf("Enter did not focus the first matching result without selecting: focus=%s want=%s editing=%t selections=%d", m.focus, test.focus, m.proxies.editing, backend.selections)
			}
		})
	}
}

func TestProxiesSearchFromNarrowMembersIncludesOtherCollapsedGroups(t *testing.T) {
	m, backend := proxiesModel(t, 80, 24)
	if m.proxies.activeGroup != "PROXY" || m.proxyGroupExpanded("Japan") {
		t.Fatal("fixture did not start inside another group's members")
	}
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.PasteMsg{Content: "JP 02"})
	if !hasAction(m.contentTargets(), "group.5") || hasAction(m.contentTargets(), "group.0") || backend.selections != 0 {
		t.Fatalf("narrow search stayed confined to the current group: visible=%v targets=%+v", m.visibleProxyGroups(), m.contentTargets())
	}
}

func TestProxiesSearchShortcutWorksWhileClosedSidebarOwnsFocus(t *testing.T) {
	m := proxySearchScrolledReferenceModel(t, 160)
	m.returnToNavigation()
	m.layout()
	scroll := m.scroll
	browsingFocus := m.pageViews["proxies"].focus
	if m.focus != "nav.proxies" || m.sidebarOpen || scroll <= 0 {
		t.Fatal("fixture did not retain navigation ownership over a scrolled proxy view")
	}
	m, _ = update(m, key("/"))
	if !m.proxies.editing || m.focus != "proxies.search" {
		t.Fatal("slash did not open proxy search from the visible sidebar")
	}
	m, _ = update(m, tea.PasteMsg{Content: "unmatched search"})
	m, _ = update(m, special(tea.KeyEscape))
	if m.focus != "nav.proxies" || m.scroll != scroll || m.proxies.editing {
		t.Fatalf("Escape lost sidebar ownership or its original scroll: focus=%s scroll=%d want=%d", m.focus, m.scroll, scroll)
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != browsingFocus {
		t.Fatalf("entering the list after search lost its original focus: got=%s want=%s", m.focus, browsingFocus)
	}
}

func TestProxiesSearchShortcutDoesNotEscapeAnOpenNavigationDrawer(t *testing.T) {
	m, _ := proxiesModel(t, 80, 24)
	m.sidebarOpen, m.focus = true, "menu"
	m.layout()
	m, _ = update(m, key("/"))
	if m.proxies.editing || !m.sidebarOpen || m.focus != "menu" {
		t.Fatal("slash opened proxy search through the navigation drawer")
	}
}

func TestProxiesSearchInputUsesOnePinnedRow(t *testing.T) {
	for _, width := range []int{40, 80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := proxySearchScrolledReferenceModel(t, width)
			m, _ = update(m, key("/"))
			block := m.proxySearchBlock(dashboardBodyWidth(m.dashboardOptions()))
			if len(block.lines) != 1 {
				t.Fatalf("proxy search occupied %d rows instead of one", len(block.lines))
			}
			before := findTarget(t, m, "proxies.search")
			m, _ = update(m, tea.PasteMsg{Content: "节点"})
			m.scroll = 10
			m.layout()
			after := findTarget(t, m, "proxies.search")
			if before.Y != after.Y || after.H != 1 || hasAction(m.contentTargets(), "proxies.search") {
				t.Fatalf("search did not remain in one fixed row while results scrolled: before=%+v after=%+v", before, after)
			}
		})
	}
}
