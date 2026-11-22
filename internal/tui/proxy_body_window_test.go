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

func TestProxyMemberRangeCountsVisibleTextRatherThanPadding(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	width := dashboardBodyWidth(m.dashboardOptions())
	layout := m.layoutProxiesBody(width)
	_, hits := m.proxiesBody(width)
	first := listSearchTarget(t, hits, "node.0.0").Y
	for _, tc := range []struct {
		name               string
		start, end         int
		wantGroup, members string
	}{
		{"top padding", first, first + 1, "", ""},
		{"name only", first + 1, first + 2, "PROXY", "1–3 / 8"},
		{"protocol only", first + 2, first + 3, "PROXY", "1–3 / 8"},
		{"bottom padding", first + 3, first + 4, "", ""},
		{"next row's padding at viewport bottom", 0, first + 6, "", "1–3 / 8"},
		{"previous row's padding at viewport top", first + 3, first + 7, "PROXY", "4–6 / 8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantLabel := ""
			if tc.members != "" {
				wantLabel = m.tr("proxies.range.group", tc.members)
			}
			if group, label := m.proxyBodyRange(layout, tc.start, tc.end); group != tc.wantGroup || label != wantLabel {
				t.Fatalf("viewport shows sticky group %q and range %q, want %q and %q", group, label, tc.wantGroup, wantLabel)
			}
			height := tc.end - tc.start
			if group := proxyStickyGroup(m.snapshot.Groups, hits, tc.start, height); group != tc.wantGroup {
				t.Fatalf("full body shows sticky group %q, want %q", group, tc.wantGroup)
			}
			if label := proxyVisibleRange(m.locale, hits, tc.start, height, width); label != wantLabel {
				t.Fatalf("full body shows range %q, want %q", label, wantLabel)
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
