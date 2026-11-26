package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
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

func TestProxyMemberRangeCountsVisibleCardRowsAndSkipsGaps(t *testing.T) {
	type viewport struct {
		name           string
		start, end     int // offsets from the first card
		group, members string
	}
	for _, mode := range []struct {
		profile colorprofile.Profile
		windows []viewport
	}{
		{colorprofile.TrueColor, []viewport{
			{"heading above visible names", -1, 2, "", "1–4 / 8"},
			{"top edge only", 0, 1, "", ""},
			{"edge above visible names", 0, 2, "PROXY", "1–4 / 8"},
			{"name only", 1, 2, "PROXY", "1–4 / 8"},
			{"protocol only", 2, 3, "PROXY", "1–4 / 8"},
			{"two adjacent edges only", 3, 5, "", ""},
			{"last edge at viewport bottom", 1, 5, "PROXY", "1–4 / 8"},
			{"first edge at viewport top", 3, 6, "PROXY", "5–8 / 8"},
		}},
		{colorprofile.ASCII, []viewport{
			{"heading above visible names", -2, 1, "", "1–4 / 8"},
			{"gutter only", -1, 0, "", ""},
			{"gutter above visible names", -1, 1, "PROXY", "1–4 / 8"},
			{"name only", 0, 1, "PROXY", "1–4 / 8"},
			{"protocol only", 1, 2, "PROXY", "1–4 / 8"},
			{"row gap only", 2, 3, "", ""},
			{"row gap at viewport top", 2, 4, "PROXY", "5–8 / 8"},
		}},
	} {
		m, _ := proxiesModel(t, 160, 40)
		m.colorProfile = mode.profile
		width := dashboardBodyWidth(m.dashboardOptions())
		layout := m.layoutProxiesBody(width)
		_, hits := m.proxiesBody(width)
		first := listSearchTarget(t, hits, "node.0.0").Y
		for _, tc := range mode.windows {
			t.Run(fmt.Sprintf("%v/%s", mode.profile, tc.name), func(t *testing.T) {
				start, end := first+tc.start, first+tc.end
				label := ""
				if tc.members != "" {
					label = m.tr("proxies.range.group", tc.members)
				}
				if gotGroup, gotLabel := m.proxyBodyRange(layout, start, end); gotGroup != tc.group || gotLabel != label {
					t.Fatalf("viewport shows sticky group %q and range %q, want %q and %q", gotGroup, gotLabel, tc.group, label)
				}
				if group := proxyStickyGroup(m.snapshot.Groups, hits, start, end-start); group != tc.group {
					t.Fatalf("full body shows sticky group %q, want %q", group, tc.group)
				}
				if got := proxyVisibleRange(m.locale, hits, start, end-start, width); got != label {
					t.Fatalf("full body shows range %q, want %q", got, label)
				}
			})
		}
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
