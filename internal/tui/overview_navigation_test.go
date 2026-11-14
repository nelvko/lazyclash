package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestOverviewPagingKeepsFocusVisibleAndEnterDoesNotToggleCore(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {120, 18}, {60, 18}} {
		for _, initial := range []string{"overview.card.dashboard.control.core", "core.toggle"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], initial), func(t *testing.T) {
				m := themeSample("overview-running")
				m.width, m.height, m.focus = size[0], size[1], initial
				m.layout()
				for range 2 {
					m, _ = update(m, special(tea.KeyPgDown))
				}
				g := dashboardGeometry(m.dashboardOptions())
				visible := false
				for _, card := range overviewCardRegionsFromModel(m) {
					if card.ID == m.focus && card.Y < m.scroll+g.height && card.Y+card.H > m.scroll {
						visible = true
					}
				}
				if !visible || !strings.HasPrefix(m.focus, "overview.card.") {
					t.Fatalf("paging left focus outside the visible cards: %s at scroll %d", m.focus, m.scroll)
				}
				m, _ = update(m, special(tea.KeyEnter))
				if m.busy || m.focus == "core.toggle" {
					t.Fatalf("Enter after paging returned to core control: focus=%s busy=%t", m.focus, m.busy)
				}
				m, _ = update(m, special(tea.KeyPgUp))
				if m.busy {
					t.Fatal("paging initiated an operation")
				}
			})
		}
	}
}
