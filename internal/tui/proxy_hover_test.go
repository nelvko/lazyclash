package tui

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func proxyHoverModel(t testing.TB, count int) Model {
	t.Helper()
	backend := newProxiesPTYBackend()
	group := &backend.snapshot.Groups[0]
	group.All = nil
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("节点 %03d", i)
		group.All = append(group.All, name)
		backend.snapshot.Proxies[name] = mihomo.Proxy{Name: name, Type: "Shadowsocks", UDP: true}
	}
	group.Now = group.All[0]
	m := New(context.Background(), backend, "zh-Hans")
	t.Cleanup(m.cancel)
	m.snapshot = backend.snapshot
	m.snapshotReady, m.busy, m.logoIntro.used = true, false, true
	m.message = ""
	m.width, m.height, m.page = 200, 45, "proxies"
	m.syncProxies()
	m.expandProxyGroup("PROXY")
	m.focus = "group.0"
	m.layout()
	return m
}

// This wrapper keeps identical mouse hit testing while exercising the ordinary
// update/reconciliation path for comparison with the motion-only path.
type reconciledProxyHoverMsg struct{ tea.MouseMotionMsg }

func BenchmarkProxyHover(b *testing.B) {
	for _, count := range []int{48, 300} {
		for _, mode := range []string{"reconcile", "switch", "same-card"} {
			b.Run(fmt.Sprintf("%d/%s", count, mode), func(b *testing.B) {
				m := proxyHoverModel(b, count)
				var nodes []hitRegion
				seen := make(map[string]bool)
				for _, hit := range m.targets {
					if !seen[hit.ID] && (hit.ID == "node.0.1" || hit.ID == "node.0.2") {
						nodes = append(nodes, hit)
						seen[hit.ID] = true
					}
				}
				if len(nodes) < 2 {
					b.Fatal("need two visible nodes")
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					hit := nodes[0]
					if mode != "same-card" {
						hit = nodes[i%len(nodes)]
					}
					var event tea.Msg = tea.MouseMotionMsg{X: hit.X, Y: hit.Y}
					if mode == "reconcile" {
						event = reconciledProxyHoverMsg{event.(tea.MouseMotionMsg)}
					}
					next, _ := m.Update(event)
					m = next.(Model)
					_ = m.View()
				}
			})
		}
	}
}

func TestProxyHoverMatchesFullLayoutWithoutChangingSelection(t *testing.T) {
	m, backend := proxiesModel(t, 200, 45)
	// Enter and leave ordinary cards, delay/reference actions, the sidebar and
	// noninteractive whitespace. Rendering must match a full fresh layout.
	ids := []string{"node.0.1", "proxies.delay.0.1", "node.0.2", "proxies.reference.0.0", "node.0.0", "nav.profiles", ""}
	for _, id := range ids {
		event := tea.MouseMotionMsg{X: 199, Y: 44}
		if id != "" {
			hit := findTarget(t, m, id)
			event.X, event.Y = hit.X, hit.Y
		}
		focus, scroll, selected := m.focus, m.scroll, m.snapshot.Groups[0].Now
		want := m
		want.mouse(event)
		want.layout()
		var cmd tea.Cmd
		m, cmd = update(m, event)
		if m.screen != want.screen || !reflect.DeepEqual(m.targets, want.targets) {
			t.Fatalf("hover %q diverged from a full layout", id)
		}
		if cmd != nil || m.focus != focus || m.scroll != scroll || m.snapshot.Groups[0].Now != selected || backend.selections != 0 {
			t.Fatalf("hover %q changed selection, focus, scrolling or started a command", id)
		}
		// Motion inside the same hit remains a no-op.
		screen := m.screen
		m, cmd = update(m, event)
		if m.screen != screen || cmd != nil {
			t.Fatalf("repeated hover %q changed the view or started work", id)
		}
	}
}

func TestProxyHoverDefersToOpenMenu(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.openProxySort()
	m.layout()
	move := menuMouse(m.menu, 1, tea.MouseNone)
	m, _ = update(m, tea.MouseMotionMsg(move))
	if !m.menu.open || m.menu.selected != 1 {
		t.Fatal("proxy hover intercepted the sorting menu's mouse motion")
	}
}

func TestProxyResizeKeepsFocusedExpandedGroup(t *testing.T) {
	m, _ := proxiesModel(t, 200, 40)
	m.expandProxyGroup("AUTO")
	m.expandProxyGroup("PROXY")
	// Tab can reach another expanded group's action without changing the last
	// active group. Narrow browsing must follow the actual keyboard location.
	m.focus = "proxies.test.1"
	m.layout()
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.proxies.activeGroup != "AUTO" || m.focus != "proxies.test.1" || !hasAction(m.targets, "node.1.0") {
		t.Fatal("narrow resize lost the focused expanded group")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 200, Height: 40})
	if !m.proxyGroupExpanded("PROXY") || !m.proxyGroupExpanded("AUTO") {
		t.Fatal("returning to wide layout lost the original expansion state")
	}
}
