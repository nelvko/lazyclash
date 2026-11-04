package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestProxiesWideGroupsExpandAndCollapseIndependently(t *testing.T) {
	m, _ := proxiesModel(t, 160, 70)
	m.proxyActivate("group.1")
	m.layout()
	if !m.proxyGroupExpanded("PROXY") || !m.proxyGroupExpanded("AUTO") || !hasAction(m.targets, "node.0.0") || !hasAction(m.targets, "node.1.0") {
		t.Fatalf("opening another group hid earlier members: expanded=%+v", m.proxies.expanded)
	}
	m.proxyActivate("group.0")
	m.layout()
	if m.proxyGroupExpanded("PROXY") || !m.proxyGroupExpanded("AUTO") || hasAction(m.targets, "node.0.0") || !hasAction(m.targets, "node.1.0") {
		t.Fatalf("collapsing one group changed another: expanded=%+v", m.proxies.expanded)
	}
	m, _ = click(m, findTarget(t, m, "proxies.collapse_all"))
	_, hits := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
	if m.hasExpandedProxyGroups() || m.proxies.activeGroup != "" {
		t.Fatalf("collapse all retained an open group: expanded=%+v active=%s", m.proxies.expanded, m.proxies.activeGroup)
	}
	for _, hit := range hits {
		if strings.HasPrefix(hit.ID, "node.") || strings.HasPrefix(hit.ID, "proxies.reference.") {
			t.Fatalf("collapse all retained a member action: %+v", hit)
		}
	}
}

func TestProxiesSortButtonOnlyChangesItsOwnGroup(t *testing.T) {
	m, _ := proxiesModel(t, 160, 70)
	m.expandProxyGroup("AUTO")
	m.focus = "group.1"
	m.sortProxies("delay")
	m.focus = "group.0"
	m.rememberPageView()
	m.layout()
	before := cloneProxyExpansion(m.proxies.expanded)
	m, _ = click(m, findTarget(t, m, "proxies.sort.1"))
	if !m.menu.open || !m.proxies.sortMenu {
		t.Fatal("another group's sorting button did not open its menu")
	}
	m, _ = update(m, menuMouse(m.menu, 2, tea.MouseLeft))
	if m.proxySort("AUTO") != "name" || m.proxySort("PROXY") != "default" || !reflect.DeepEqual(m.proxies.expanded, before) {
		t.Fatalf("sorting changed another group's order or expansion: sorts=%+v expanded=%+v", m.proxies.sorts, m.proxies.expanded)
	}
	if m.focus != "proxies.sort.1" || !hasAction(m.targets, m.focus) {
		t.Fatalf("sort result lost its owning group: focus=%s", m.focus)
	}
}

func TestProxiesTestButtonsBindTheOwnerAndCancelPreviousGroup(t *testing.T) {
	m, _ := proxiesModel(t, 160, 70)
	m.expandProxyGroup("AUTO")
	m.focus = "group.0"
	m.layout()
	before := cloneProxyExpansion(m.proxies.expanded)
	m, cmd := click(m, findTarget(t, m, "proxies.test.1"))
	if cmd == nil || !m.proxies.testing || m.proxies.testGroup != "AUTO" || !reflect.DeepEqual(m.proxies.expanded, before) {
		t.Fatalf("test button used the previously active group: testing=%t group=%s expanded=%+v", m.proxies.testing, m.proxies.testGroup, m.proxies.expanded)
	}
	defer m.stopProxyTests()
	oldContext, generation := m.proxies.testCtx, m.proxies.generation
	late := proxyTestMessage(m, "AUTO", "HK 01", 777)
	for _, gi := range []int{0, 1} {
		header := ansi.Strip(strings.Join(m.proxyGroupHeader(gi, dashboardBodyWidth(m.dashboardOptions())).lines, "\n"))
		if strings.Contains(header, m.tr("proxies.cancel_test")) != (gi == 1) {
			t.Fatalf("group %d displayed another group's cancellation action: %q", gi, header)
		}
	}
	m, cmd = click(m, findTarget(t, m, "proxies.test.0"))
	if cmd == nil || oldContext.Err() != context.Canceled || !m.proxies.testing || m.proxies.testGroup != "PROXY" || m.proxies.generation <= generation || !reflect.DeepEqual(m.proxies.expanded, before) {
		t.Fatalf("testing a different group did not replace the previous run: group=%s generation=%d oldError=%v", m.proxies.testGroup, m.proxies.generation, oldContext.Err())
	}
	m.acceptProxyDelay(late)
	if _, exists := m.proxies.results[proxyResultKey("AUTO", "HK 01")]; exists {
		t.Fatal("a canceled group's late measurement reached the new run")
	}
}

func TestProxiesReferenceAndDetailReturnsRestoreAllOpenGroups(t *testing.T) {
	for _, origin := range []string{"reference", "detail"} {
		t.Run(origin, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 80)
			m.expandProxyGroup("ChatGPT")
			m.focus = "node.0.0"
			m.layout()
			before := cloneProxyExpansion(m.proxies.expanded)
			if origin == "reference" {
				m.proxyActivate("proxies.reference.0.0")
				if !m.proxyGroupExpanded("AUTO") || !m.proxyGroupExpanded("PROXY") || !m.proxyGroupExpanded("ChatGPT") {
					t.Fatalf("following a reference collapsed another group: %+v", m.proxies.expanded)
				}
			} else {
				m.openProxyDetail("PROXY", "AUTO")
			}
			m.expandProxyGroup("YouTube")
			if !m.proxyBack() || !reflect.DeepEqual(m.proxies.expanded, before) || m.proxies.activeGroup != "PROXY" {
				t.Fatalf("return lost the original expansion set: expanded=%+v want=%+v active=%s", m.proxies.expanded, before, m.proxies.activeGroup)
			}
			wantFocus := "node.0.0"
			if origin == "reference" {
				wantFocus = "proxies.reference.0.0"
			}
			if m.focus != wantFocus {
				t.Fatalf("return lost its original control: focus=%s want=%s", m.focus, wantFocus)
			}
		})
	}
}

func TestProxiesNarrowDirectoryPreservesWideExpansion(t *testing.T) {
	m, _ := proxiesModel(t, 160, 70)
	m.expandProxyGroup("AUTO")
	m.focus = "group.1"
	m.layout()
	before := cloneProxyExpansion(m.proxies.expanded)
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	groups := m.visibleProxyGroups()
	if len(groups) != 1 || m.snapshot.Groups[groups[0]].Name != "AUTO" {
		t.Fatalf("narrow members mixed multiple groups: %v", groups)
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.proxies.activeGroup != "" || !reflect.DeepEqual(m.proxies.expanded, before) {
		t.Fatalf("narrow directory erased the wide expansion set: active=%s expanded=%+v", m.proxies.activeGroup, m.proxies.expanded)
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 70})
	if !m.proxyGroupExpanded("PROXY") || !m.proxyGroupExpanded("AUTO") || !hasAction(m.targets, "node.0.0") || !hasAction(m.targets, "node.1.0") {
		t.Fatalf("returning to wide mode lost open groups: %+v", m.proxies.expanded)
	}
}

func TestProxiesExpansionAndSortFollowGroupNamesUntilSourceChanges(t *testing.T) {
	for _, change := range []string{"source", "mode"} {
		t.Run(change, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 70)
			m.expandProxyGroup("AUTO")
			m.focus = "group.1"
			m.sortProxies("name")
			m.snapshot.Groups[0], m.snapshot.Groups[1] = m.snapshot.Groups[1], m.snapshot.Groups[0]
			m.syncProxies()
			if !m.proxyGroupExpanded("PROXY") || !m.proxyGroupExpanded("AUTO") || m.proxySort("AUTO") != "name" {
				t.Fatal("reordering changed the expansion or sort owner")
			}
			m.snapshot.Groups = append(m.snapshot.Groups[:1], m.snapshot.Groups[2:]...)
			m.syncProxies()
			if m.proxies.expanded["PROXY"] || !m.proxyGroupExpanded("AUTO") {
				t.Fatalf("removed group retained expansion or erased another: %+v", m.proxies.expanded)
			}
			if change == "source" {
				m.snapshot.Generation += "-new"
			} else {
				m.snapshot.Mode.Actual = "global"
			}
			m.syncProxies()
			if m.hasExpandedProxyGroups() || m.proxies.activeGroup != "" || m.proxySort("AUTO") != "default" {
				t.Fatalf("%s change retained the previous groups: expanded=%+v active=%s sorts=%+v", change, m.proxies.expanded, m.proxies.activeGroup, m.proxies.sorts)
			}
		})
	}
}

func TestProxiesHeaderUsesRightChevronAndClickableVerticalPadding(t *testing.T) {
	for _, edge := range []string{"top", "bottom"} {
		t.Run(edge, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 70)
			m.expandProxyGroup("AUTO")
			m.focus = "group.0"
			m.layout()
			width := dashboardBodyWidth(m.dashboardOptions())
			header := m.proxyGroupHeader(0, width)
			if len(header.lines) != 3 {
				t.Fatalf("wide header lacks vertical padding: rows=%d", len(header.lines))
			}
			for _, row := range []int{0, 2} {
				if strings.TrimSpace(ansi.Strip(header.lines[row])) != "" {
					t.Fatalf("header padding contains content: %q", ansi.Strip(header.lines[row]))
				}
			}
			content := ansi.Strip(header.lines[1])
			arrow := strings.LastIndex(content, "▾")
			if !strings.HasPrefix(content, "› PROXY") || arrow < 0 || ansi.StringWidth(content[:arrow]) < width/2 {
				t.Fatalf("header kept a left disclosure arrow: %q", content)
			}
			assertRegions(t, header.targets, width, len(header.lines))
			for i, hit := range header.targets {
				for _, other := range header.targets[i+1:] {
					if regionsOverlap(hit, other) {
						t.Fatalf("header button overlaps its folding region: %+v / %+v", hit, other)
					}
				}
			}
			left, right, top, bottom := m.width, 0, m.height, 0
			for _, hit := range m.targets {
				if hit.ID == "group.0" {
					left, right = min(left, hit.X), max(right, hit.X+hit.W)
					top, bottom = min(top, hit.Y), max(bottom, hit.Y+hit.H)
				}
			}
			y := top
			if edge == "bottom" {
				y = bottom - 1
			}
			m, _ = update(m, tea.MouseClickMsg{X: right - 1, Y: y, Button: tea.MouseLeft})
			if m.proxyGroupExpanded("PROXY") || !m.proxyGroupExpanded("AUTO") {
				t.Fatalf("header %s padding did not fold only its owner: expanded=%+v bounds=%v", edge, m.proxies.expanded, []int{left, right, top, bottom})
			}
		})
	}
	m, _ := proxiesModel(t, 160, 20)
	if rows := len(m.proxyGroupHeader(0, dashboardBodyWidth(m.dashboardOptions())).lines); rows != 1 {
		t.Fatalf("short window retained padded group headings: rows=%d", rows)
	}
}

func TestProxiesEachOpenGroupKeepsItsOwnHeaderActions(t *testing.T) {
	m, _ := proxiesModel(t, 160, 70)
	for _, gi := range []int{0, 1} {
		m.expandProxyGroup(m.snapshot.Groups[gi].Name)
		m.focus = fmt.Sprintf("group.%d", gi)
		m.sortProxies("delay")
	}
	_, hits := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
	for _, gi := range []int{0, 1} {
		for _, kind := range []string{"test", "sort"} {
			id := fmt.Sprintf("proxies.%s.%d", kind, gi)
			if !hasAction(hits, id) {
				t.Fatalf("open group lost its independent action: %s", id)
			}
		}
	}
}

func TestProxiesGroupActionFocusSurvivesLiveReordering(t *testing.T) {
	for _, kind := range []string{"sort", "test"} {
		t.Run(kind, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 70)
			m.expandProxyGroup("AUTO")
			m.focus = "group.1"
			if kind == "sort" {
				m.sortProxies("delay")
			}
			m.focus = fmt.Sprintf("proxies.%s.1", kind)
			m.layout()
			m.snapshot.Groups[1], m.snapshot.Groups[2] = m.snapshot.Groups[2], m.snapshot.Groups[1]
			m.layout()
			want := fmt.Sprintf("proxies.%s.2", kind)
			if m.focus != want || !hasAction(m.targets, want) || m.pageViews["proxies"].group != "AUTO" {
				t.Fatalf("group action followed a stale row index: focus=%s state=%+v", m.focus, m.pageViews["proxies"])
			}
		})
	}
}

func TestProxiesGlobalReferenceChainKeepsEarlierOpenGroups(t *testing.T) {
	m, _ := proxiesModel(t, 160, 90)
	m.snapshot.Mode.Actual = "global"
	m.syncProxies()
	m.expandProxyGroup("GLOBAL")
	m.focus = "node.8.0"
	m.proxyActivate("proxies.reference.8.0")
	m.proxyActivate("proxies.reference.0.0")
	var visible []string
	for _, gi := range m.visibleProxyGroups() {
		visible = append(visible, m.snapshot.Groups[gi].Name)
	}
	if !reflect.DeepEqual(visible, []string{"PROXY", "AUTO", "GLOBAL"}) {
		t.Fatalf("following another global reference hid an earlier group: %v", visible)
	}
	if !m.proxyBack() || m.proxies.activeGroup != "PROXY" || !m.proxyGroupExpanded("GLOBAL") || !m.proxyGroupExpanded("PROXY") || m.proxyGroupExpanded("AUTO") {
		t.Fatalf("return did not restore the previous global reference level: active=%s expanded=%+v", m.proxies.activeGroup, m.proxies.expanded)
	}
	if !m.proxyBack() || m.proxies.activeGroup != "GLOBAL" || !m.proxyGroupExpanded("GLOBAL") || m.proxyGroupExpanded("PROXY") {
		t.Fatalf("second return did not restore the global origin: active=%s expanded=%+v", m.proxies.activeGroup, m.proxies.expanded)
	}
}
