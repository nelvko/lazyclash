package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestProxyWindowMatchesFullBody(t *testing.T) {
	for _, size := range [][2]int{{200, 45}, {120, 22}, {80, 24}, {40, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := proxyHoverModel(t, 48)
			m.width, m.height = size[0], size[1]
			if !m.proxiesNarrow() {
				m.expandProxyGroup("AUTO")
			}
			m.focus, m.hover = "node.0.4", "node.0.8"
			full := m.dashboardOptions()
			full.Snapshot = m.snapshot
			g := dashboardGeometry(full)
			full.Body, full.BodyTargets = m.proxiesBody(g.width)
			if full.BodyTopRows > 0 {
				full.BodyTop = m.proxiesFixedToolbar(full)
			}
			lines := strings.Count(full.Body, "\n") + 1
			for _, offset := range []int{0, 1, 2, 3, 4, 17, lines / 2, max(0, lines-g.height-1), lines} {
				m.scroll, full.Scroll = offset, offset
				window := full
				m.populatePageBody(&window)
				want, wantHits, wantTotal := renderDashboard(full)
				view, hits, total := renderDashboard(window)
				if view != want || !reflect.DeepEqual(hits, wantHits) || total != wantTotal {
					t.Fatalf("window differs from full body at offset %d (view equal %t, hit regions equal %t, total %d vs %d)",
						offset, view == want, reflect.DeepEqual(hits, wantHits), total, wantTotal)
				}
			}
		})
	}
}

func TestProxyKeyboardTargetsMatchMouseGeometry(t *testing.T) {
	for _, width := range []int{40, 80, 120, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _ := proxiesModel(t, width, 30)
			g := dashboardGeometry(m.dashboardOptions())
			_, all := m.proxiesBody(g.width)
			seen := make(map[string]bool)
			var want []hitRegion
			for _, target := range all {
				if !seen[target.ID] {
					seen[target.ID] = true
					want = append(want, target)
				}
			}
			var got []hitRegion
			clear(seen)
			for _, target := range m.proxyTargets(g.width, false) {
				if !seen[target.ID] {
					seen[target.ID] = true
					got = append(got, target)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("keyboard target geometry differs from first mouse rectangles:\ngot %#v\nwant %#v", got, want)
			}
		})
	}
}
