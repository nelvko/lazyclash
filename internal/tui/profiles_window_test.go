package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestProfileWindowMatchesFullGrid(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 22}, {80, 24}, {30, 10}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := profileNavigationModel(size[0], size[1])
			for i := range 12 {
				m.snapshot.Profiles = append(m.snapshot.Profiles, fmt.Sprintf("p-%02d", i))
			}
			for _, detail := range []string{"", "beta", "p-11"} {
				m.profilesUI.detail = detail
				o := m.dashboardOptions()
				g := dashboardGeometry(o)
				o.Body, o.BodyTargets = m.profilesBody(g.width)
				if !reflect.DeepEqual(o.BodyTargets, m.profilesTargets(g.width)) {
					t.Fatal("logical focus and full-grid mouse targets differ")
				}
				rows := strings.Count(o.Body, "\n") + 1
				for _, offset := range []int{0, 1, 9, 14, rows / 2, rows - g.height - 1, rows} {
					m.scroll, o.Scroll = offset, offset
					window := o
					m.populatePageBody(&window)
					want, wantHits, wantRows := renderDashboard(o)
					got, hits, total := renderDashboard(window)
					if want != got || !reflect.DeepEqual(wantHits, hits) || wantRows != total {
						t.Fatalf("window differs at offset %d with detail %q", offset, detail)
					}
				}
			}
		})
	}
}
