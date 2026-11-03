package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func proxiesModel(t *testing.T, width, height int) (Model, *proxiesPTYBackend) {
	t.Helper()
	b := newProxiesPTYBackend()
	m := New(t.Context(), b, "en")
	m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	m.logoIntro.used = true
	m.width, m.height = width, height
	m.navigate("proxies")
	m.proxies.expanded = "PROXY"
	m.focus = "group.0"
	m.syncProxies()
	m.layout()
	return m, b
}

func proxyTestMessage(m Model, group, node string, delay int) proxyDelayMsg {
	return proxyDelayMsg{generation: m.proxies.generation, target: proxyTestTarget{group: group, node: node, path: m.proxyPathKey(node)}, result: mihomo.ProxyDelay{State: "success", Delay: delay, Time: time.Now(), URL: mihomo.DefaultProxyDelayURL}}
}

func TestProxiesFocusAndObservedSelectionRemainIndependent(t *testing.T) {
	m, b := proxiesModel(t, 160, 40)
	m.focus = "node.0.1"
	m.layout()
	if _, g, node, ok := m.proxyAtFocus(); !ok || node != "HK 01" || g.Now != "AUTO" {
		t.Fatal("moving focus changed the core's selection")
	}
	current := ansi.Strip(strings.Join(m.proxyNode(0, 0, 50).lines, "\n"))
	focused := ansi.Strip(strings.Join(m.proxyNode(0, 1, 50).lines, "\n"))
	if !strings.HasPrefix(current, "  ✓ ") || !strings.HasPrefix(focused, "›   ") {
		t.Fatalf("focus/selection markers confused: current=%q focused=%q", current, focused)
	}
	m, cmd := update(m, special(tea.KeyEnter))
	if !m.busy || m.snapshot.Groups[0].Now != "AUTO" || m.proxies.pendingNode != "HK 01" {
		t.Fatal("selection was shown optimistically before the core confirmed it")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.snapshot.Groups[0].Now != "HK 01" || m.proxies.pendingNode != "" || b.selections != 1 {
		t.Fatalf("selection confirmation failed: group=%+v count=%d", m.snapshot.Groups[0], b.selections)
	}
	selected := ansi.Strip(strings.Join(m.proxyNode(0, 1, 50).lines, "\n"))
	if !strings.HasPrefix(selected, "› ✓ ") {
		t.Fatalf("focus erased current selection: %q", selected)
	}
}

func TestProxiesAutomaticGroupsOpenDetailsAndReferencesBrowse(t *testing.T) {
	m, b := proxiesModel(t, 160, 40)
	m.proxies.expanded, m.focus = "AUTO", "node.1.0"
	m.rememberPageView()
	m.layout()
	m, cmd := update(m, special(tea.KeyEnter))
	if cmd != nil || m.busy || m.proxies.detail != "HK 01" || b.selections != 0 || m.snapshot.Groups[1].Now != "HK 02" {
		t.Fatal("automatic group submitted a manual selection instead of opening details")
	}
	m.proxyBack()
	m.proxies.expanded, m.focus = "PROXY", "node.0.0"
	m.proxies.search.SetValue("proxy")
	m.rememberPageView()
	m.layout()
	m, _ = update(m, special(tea.KeyRight))
	if m.proxies.expanded != "AUTO" || m.proxies.search.Value() != "" || len(m.proxies.history) != 1 {
		t.Fatal("reference browsing retained an incompatible search")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.proxies.expanded != "PROXY" || m.proxies.search.Value() != "proxy" || m.focus != "node.0.0" {
		t.Fatalf("reference return lost its location: %+v focus=%s", m.proxies, m.focus)
	}
}

func TestProxiesReturnRestoresNamesAfterGroupAndMemberReordering(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(fmt.Sprint("detail=", detail), func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 40)
			m.focus = "node.0.0"
			m.rememberPageView()
			if detail {
				m.openProxyDetail("PROXY", "AUTO")
			} else {
				m.browseProxyReference("AUTO")
			}
			m.snapshot.Groups[0], m.snapshot.Groups[2] = m.snapshot.Groups[2], m.snapshot.Groups[0]
			g := &m.snapshot.Groups[2]
			g.All[0], g.All[2] = g.All[2], g.All[0]
			if !m.proxyBack() || m.proxies.expanded != "PROXY" || m.focus != "node.2.2" {
				t.Fatalf("return followed stale row indexes: group=%s focus=%s", m.proxies.expanded, m.focus)
			}
		})
	}
}

func TestProxiesModeChangeResetsNarrowAndReferencedLocations(t *testing.T) {
	for _, width := range []int{80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _ := proxiesModel(t, width, 40)
			m.focus = "node.0.0"
			m.browseProxyReference("AUTO")
			m.snapshot.Mode.Actual = "global"
			m.syncProxies()
			m.layout()
			groups := m.visibleProxyGroups()
			if len(groups) != 1 || m.snapshot.Groups[groups[0]].Name != "GLOBAL" || len(m.proxies.history) != 0 || m.proxies.detail != "" {
				t.Fatalf("global mode retained an earlier directory: %+v history=%+v", groups, m.proxies.history)
			}
			m.proxies.expanded = "GLOBAL"
			m.focus = "node.8.0"
			m.browseProxyReference("PROXY")
			if groups := m.visibleProxyGroups(); len(groups) != 1 || m.snapshot.Groups[groups[0]].Name != "PROXY" {
				t.Fatal("global mode prevented explicit reference browsing")
			}
			m.proxyBack()
			if m.proxies.expanded != "GLOBAL" || m.focus != "node.8.0" {
				t.Fatalf("global reference back failed: group=%s focus=%s", m.proxies.expanded, m.focus)
			}
		})
	}
}

func TestProxiesSearchOwnsKeysAndLongQueryCursorAcrossResize(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m, _ = update(m, key("/"))
	for _, text := range []string{"q", "1", "2", "3", "t", "s"} {
		m, _ = update(m, key(text))
	}
	if m.quitting || m.page != "proxies" || m.proxies.search.Value() != "q123ts" || m.proxies.testing || m.menu.open {
		t.Fatal("search text triggered page shortcuts")
	}
	m.proxies.search.Reset()
	query := strings.Repeat("long-query-", 18) + "VISIBLE-TAIL"
	m, _ = update(m, tea.PasteMsg{Content: query})
	for _, width := range []int{160, 80, 40, 120} {
		m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: 24})
		if m.proxies.search.Width() <= 0 || !strings.Contains(ansi.Strip(m.screen), "VISIBLE-TAIL") || m.proxies.search.Position() != len([]rune(query)) {
			t.Fatalf("width %d hid current query cursor: inputWidth=%d position=%d\n%s", width, m.proxies.search.Width(), m.proxies.search.Position(), ansi.Strip(m.screen))
		}
		assertCanvas(t, m.View().Content, width, 24)
	}
	m.proxies.search.Reset()
	m, _ = update(m, tea.PasteMsg{Content: "hk 02"})
	m, _ = update(m, special(tea.KeyEnter))
	if m.proxies.editing || len(m.visibleProxyGroups()) == 0 || m.proxies.search.Value() != "hk 02" {
		t.Fatal("search completion lost its filtered result")
	}
}

func TestProxiesClickingModeLeavesSearchEditing(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m, _ = update(m, key("/"))
	m, cmd := click(m, findTarget(t, m, "mode.global"))
	if !m.busy || cmd == nil {
		t.Fatal("mode click did not begin its action")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.proxies.editing || m.proxies.search.Focused() || m.focus == "proxies.search" || m.snapshot.Mode.Actual != "global" {
		t.Fatalf("mode click left a hidden input editing: editing=%t focused=%t focus=%s mode=%s", m.proxies.editing, m.proxies.search.Focused(), m.focus, m.snapshot.Mode.Actual)
	}
}

func TestProxiesGridVerticalAndHorizontalTraversal(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m, _ = update(m, special(tea.KeyDown))
	if m.focus != "node.0.0" {
		t.Fatalf("Down from expanded group skipped its first member: %s", m.focus)
	}
	// AUTO is a group reference, so use an ordinary leaf for horizontal movement.
	m.focus = "node.0.2"
	m.rememberPageView()
	m, _ = update(m, special(tea.KeyRight))
	if m.focus != "node.0.3" {
		t.Fatalf("Right did not move to adjacent leaf: %s", m.focus)
	}
	m, _ = update(m, special(tea.KeyDown))
	if m.focus != "node.0.5" {
		t.Fatalf("Down did not preserve the right column: %s", m.focus)
	}
	m, _ = update(m, special(tea.KeyLeft))
	if m.focus != "node.0.4" {
		t.Fatalf("Left did not move to adjacent leaf: %s", m.focus)
	}
}

func TestProxiesDelaySortFreezesUntilExplicitResort(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.sortProxies("delay")
	before := m.proxyMembers(m.snapshot.Groups[0])
	m.proxies.results[proxyResultKey("PROXY", "US 01")] = proxyMeasurement{result: mihomo.ProxyDelay{State: "success", Delay: 1}, path: m.proxyPathKey("US 01")}
	if after := m.proxyMembers(m.snapshot.Groups[0]); !reflect.DeepEqual(after, before) {
		t.Fatal("incoming delay sample reordered the focused list")
	}
	m.sortProxies("delay")
	indices := m.proxyMembers(m.snapshot.Groups[0])
	if m.snapshot.Groups[0].All[indices[0]] != "US 01" {
		t.Fatal("explicit resort did not use the new observation")
	}
}

func TestProxiesReferencedGroupHistoryDoesNotSurviveRouteChanges(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	observed := time.Now().Add(-time.Minute)
	auto := m.snapshot.Proxies["AUTO"]
	auto.History = []mihomo.ProxyHistory{{Time: observed, Delay: 40}}
	m.snapshot.Proxies["AUTO"] = auto
	m.proxies.results[proxyResultKey("PROXY", "AUTO")] = proxyMeasurement{result: mihomo.ProxyDelay{State: "success", Delay: 40, Time: observed, URL: mihomo.DefaultProxyDelayURL}, path: m.proxyPathKey("AUTO")}
	if result := m.proxyDelay("PROXY", "AUTO").result; result.State != "success" || result.Delay != 40 {
		t.Fatal("current path-bound group observation was unavailable")
	}
	m.snapshot.Groups[1].Now = "HK 01"
	auto.Now = "HK 01"
	m.snapshot.Proxies["AUTO"] = auto
	m.syncProxies()
	if result := m.proxyDelay("PROXY", "AUTO").result; result.State != "" {
		t.Fatalf("old core group history described a newly selected route: %+v", result)
	}
	if label, _ := m.proxyDelayLabel("PROXY", "AUTO"); label != m.tr("proxies.not_tested") {
		t.Fatalf("changed group route did not become untested: %q", label)
	}
	if leaf := m.proxyDelay("PROXY", "HK 01").result; leaf.State != "success" || leaf.Delay != 42 {
		t.Fatalf("leaf history was lost along with unattributed group history: %+v", leaf)
	}
	// A newly opened session also lacks a path identity for core group history.
	if result := m.proxyDelay("GLOBAL", "AUTO").result; result.State != "" {
		t.Fatalf("unbound core group history became a session measurement: %+v", result)
	}
}

func TestProxiesSortingMenuReturnsToFocusedMember(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.focus = "node.0.1"
	m.rememberPageView()
	m, _ = update(m, key("s"))
	if !m.menu.open || !m.proxies.sortMenu {
		t.Fatal("sort menu did not open")
	}
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyEnter))
	if m.menu.open || m.proxies.sort != "delay" || m.focus != "node.0.1" || !hasAction(m.targets, m.focus) {
		t.Fatalf("sort lost focused member: sort=%s focus=%s", m.proxies.sort, m.focus)
	}
	m, _ = update(m, key("s"))
	m, _ = update(m, special(tea.KeyEscape))
	if m.focus != "node.0.1" {
		t.Fatal("canceling sorting lost focus")
	}
}

func TestProxiesDelaySchedulerIsBoundedAndKeepsFocus(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.focus = "node.0.1"
	cmd := m.testProxyGroup()
	if cmd == nil || !m.proxies.testing || len(m.proxies.inflight) != 4 || len(m.proxies.queue) != 2 || m.proxies.total != 6 {
		t.Fatalf("unexpected bounded test queue: inflight=%d queue=%d total=%d", len(m.proxies.inflight), len(m.proxies.queue), m.proxies.total)
	}
	for m.proxies.testing {
		var node string
		for key := range m.proxies.inflight {
			node = strings.SplitN(key, "\x00", 2)[1]
			break
		}
		if node == "" {
			t.Fatal("testing stalled without an in-flight request")
		}
		m.acceptProxyDelay(proxyTestMessage(m, "PROXY", node, 44))
		if len(m.proxies.inflight) > 4 || m.focus != "node.0.1" || m.snapshot.Groups[0].Now != "AUTO" {
			t.Fatal("delay completion exceeded concurrency or changed focus/selection")
		}
	}
	if m.proxies.completed != 6 || m.proxies.testStatus != "done" || m.proxies.cancel != nil || len(m.proxies.results) != 6 {
		t.Fatalf("batch did not finish cleanly: %+v", m.proxies)
	}
}

func TestProxiesCanceledOrChangedTestsCannotPublishLateResults(t *testing.T) {
	for _, action := range []string{"cancel", "page", "configuration", "core", "path", "member"} {
		t.Run(action, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 40)
			m.testProxyGroup()
			ctx := m.proxies.testCtx
			msg := proxyTestMessage(m, "PROXY", "AUTO", 777)
			switch action {
			case "cancel":
				m.stopProxyTests()
			case "page":
				m.navigate("profiles")
			case "configuration":
				m.snapshot.Generation = "new-generation"
				m.syncProxies()
			case "core":
				m.snapshot.Service.PID++
				m.syncProxies()
			case "path":
				m.snapshot.Groups[1].Now = "HK 01"
				m.syncProxies()
			case "member":
				m.snapshot.Groups[0].All = m.snapshot.Groups[0].All[1:]
				m.syncProxies()
			}
			m, _ = update(m, msg)
			if _, exists := m.proxies.results[proxyResultKey("PROXY", "AUTO")]; exists {
				t.Fatal("late test result reached a new or removed route")
			}
			if action != "path" && action != "member" && ctx.Err() != context.Canceled {
				t.Fatal("cancelled test retained a live context")
			}
			m.stopProxyTests()
		})
	}
}

func TestProxiesUntestableRoutesAndHistoricalFailuresStayHonest(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	for _, node := range []string{"DIRECT", "REJECT", "BALANCE", "EMPTY", "missing"} {
		if m.proxyTestable(node) {
			t.Fatalf("untestable route %q was enabled", node)
		}
	}
	m.snapshot.Groups[1].Now = "DIRECT"
	if m.proxyTestable("AUTO") {
		t.Fatal("a selector pointing to DIRECT was testable")
	}
	m.snapshot.Groups[1].Now = "AUTO"
	if m.proxyTestable("AUTO") {
		t.Fatal("a cyclic route was testable")
	}
	m.snapshot.Groups[1].Now = "US 01"
	if m.proxyTestable("AUTO") {
		t.Fatal("a selector's nonmember current route was testable")
	}
	p := m.snapshot.Proxies["HK 01"]
	p.History = []mihomo.ProxyHistory{{Time: time.Now(), Delay: 0}}
	m.snapshot.Proxies["HK 01"] = p
	if state := m.proxyDelay("PROXY", "HK 01").result.State; state != "failed" {
		t.Fatalf("history without failure cause was classified as %s", state)
	}
	m.proxies.expanded, m.focus = "EMPTY", "group.7"
	if cmd := m.testProxyGroup(); cmd != nil || m.proxies.testing || m.proxies.cancel != nil {
		t.Fatal("an empty test group retained work or its cancellation context")
	}
}

func TestProxiesGeometryAtDesktopAndNarrowSizes(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {120, 30}, {100, 24}, {80, 24}, {60, 20}, {40, 16}, {24, 12}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _ := proxiesModel(t, size[0], size[1])
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
				m.noColor, m.colorProfile = false, profile
				view := m.View().Content
				assertCanvas(t, view, size[0], size[1])
				if profile == colorprofile.ASCII && strings.Contains(view, "\x1b") {
					t.Fatal("uncolored terminal retained control sequences")
				}
			}
			width := dashboardBodyWidth(m.dashboardOptions())
			body, hits := m.proxiesBody(width)
			lines := strings.Split(body, "\n")
			for i, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("body line %d exceeds %d cells: %d", i, width, got)
				}
			}
			for i, hit := range hits {
				if hit.X < 0 || hit.Y < 0 || hit.W <= 0 || hit.H <= 0 || hit.X+hit.W > width || hit.Y+hit.H > len(lines) {
					t.Fatalf("invalid hit geometry: %+v width=%d rows=%d", hit, width, len(lines))
				}
				for _, other := range hits[i+1:] {
					if hit.X < other.X+other.W && other.X < hit.X+hit.W && hit.Y < other.Y+other.H && other.Y < hit.Y+hit.H {
						t.Fatalf("overlapping hit regions: %+v / %+v", hit, other)
					}
				}
			}
			if size[0] < 100 {
				if groups := m.visibleProxyGroups(); len(groups) != 1 || m.snapshot.Groups[groups[0]].Name != "PROXY" {
					t.Fatal("narrow member view mixed in the directory")
				}
			}
		})
	}
}
