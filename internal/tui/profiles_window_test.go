package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestProfileSlotsDependOnAvailableWidth(t *testing.T) {
	m := profileNavigationModel(240, 40)
	names := m.snapshot.Profiles
	for _, tc := range []struct{ available, columns, cardWidth int }{
		{40, 1, 40}, {97, 1, 97}, {98, 2, 48}, {147, 2, 72},
		{148, 3, 48}, {197, 3, 64}, {198, 4, 48}, {260, 4, 63},
	} {
		for _, count := range []int{1, len(names)} {
			m.snapshot.Profiles = names[:count]
			l := m.layoutProfiles(tc.available)
			if l.columns != tc.columns || l.cardWidth != tc.cardWidth {
				t.Errorf("%d content cells, %d profiles: got %d columns of %d, want %d of %d",
					tc.available, count, l.columns, l.cardWidth, tc.columns, tc.cardWidth)
			}
		}
	}
}

func TestProfileWindowMatchesFullGrid(t *testing.T) {
	for _, size := range [][2]int{{240, 21}, {200, 24}, {160, 45}, {120, 22}, {80, 24}, {30, 10}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := profileNavigationModel(size[0], size[1])
			for i := range 12 {
				m.snapshot.Profiles = append(m.snapshot.Profiles, fmt.Sprintf("p-%02d", i))
			}

			o := m.dashboardOptions()
			g := dashboardGeometry(o)
			o.Body, o.BodyTargets = m.profilesBody(g.width)
			if !reflect.DeepEqual(o.BodyTargets, m.profilesTargets(g.width)) {
				t.Fatal("logical focus and full-grid mouse targets differ")
			}
			rows := strings.Count(o.Body, "\n") + 1
			cards := m.profileGridCards()
			last := cards[len(cards)-1]
			if rows != last.Y+last.H {
				t.Fatalf("grid scrolls past its last card: %d rows, last card %+v", rows, last)
			}
			for _, offset := range []int{0, 1, 9, 14, rows / 2, rows - g.height - 1, rows} {
				m.scroll, o.Scroll = offset, offset
				window := o
				m.populatePageBody(&window)
				want, wantHits, wantRows := renderDashboard(o)
				got, hits, total := renderDashboard(window)
				if want != got || !reflect.DeepEqual(wantHits, hits) || wantRows != total {
					t.Fatalf("window differs at offset %d", offset)
				}
			}
		})
	}
}
