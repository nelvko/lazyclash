package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestProxyGridUsesMinimumCardWidth(t *testing.T) {
	for _, test := range []struct {
		width, indent, columns, cardWidth int
	}{
		{0, 0, 1, 1}, {35, 0, 1, 35}, {36, 0, 1, 36},
		{72, 0, 1, 72}, {73, 0, 2, 36},
		{109, 0, 2, 54}, {110, 0, 3, 36},
		{146, 0, 3, 48}, {147, 0, 4, 36},
		{74, 2, 1, 72}, {75, 2, 2, 36},
		{112, 2, 3, 36}, {149, 2, 4, 36},
		{175, 2, 4, 42}, {375, 2, 4, 92},
	} {
		t.Run(fmt.Sprint(test.width, "/", test.indent), func(t *testing.T) {
			grid := layoutProxyGrid(test.width, test.indent)
			if grid.columns != test.columns || grid.cardWidth != test.cardWidth || grid.indent != test.indent {
				t.Fatalf("unexpected grid: %+v", grid)
			}
			available := max(1, test.width-test.indent)
			used := grid.columns*grid.cardWidth + (grid.columns-1)*proxyColumnGap
			if used > available || available-used >= grid.columns || grid.columns > 1 && grid.cardWidth < proxyNodeMinWidth {
				t.Fatalf("grid overflowed, wasted a full column of cells or made cards too narrow: %+v available=%d", grid, available)
			}
		})
	}
}

func TestProxyGridKeepsUnicodeCardsInsideTheirSlots(t *testing.T) {
	names := []string{
		"English node", strings.Repeat("香港节点", 16), strings.Repeat("🇭🇰 香港🧪", 12),
		strings.Repeat("ＦＵＬＬＷＩＤＴＨ", 8), strings.Repeat("very-long-node-", 10), "短",
	}
	for _, width := range []int{40, 76, 77, 80, 96, 100, 120, 160, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _ := proxiesModel(t, width, 40)
			for _, name := range names {
				proxy := m.snapshot.Proxies["HK 01"]
				proxy.Name = name
				m.snapshot.Proxies[name] = proxy
			}
			m.snapshot.Groups[0].All = append([]string{}, names...)
			m.snapshot.Groups[0].Now = names[1]
			m.focus = "node.0.2"
			m.rememberPageView()
			m.layout()
			bodyWidth := dashboardBodyWidth(m.dashboardOptions())
			grid := m.proxyGrid(bodyWidth)
			body := m.proxyGroupBody(0, bodyWidth)
			assertRegions(t, body.targets, bodyWidth, len(body.lines))
			for _, line := range body.lines {
				if ansi.StringWidth(line) > bodyWidth {
					t.Fatalf("Unicode text overflowed the grid: %q", ansi.Strip(line))
				}
			}
			for i := range names {
				card := m.proxyNode(0, i, grid.cardWidth)
				if len(card.lines) != 3 {
					t.Fatal("node type or name changed the card height")
				}
				for _, line := range card.lines {
					if ansi.StringWidth(line) != grid.cardWidth {
						t.Fatalf("Unicode card changed its slot width: %q", ansi.Strip(line))
					}
				}
				prefix := "    "
				if i == 1 {
					prefix = "▌   "
				}
				if !strings.Contains(ansi.Strip(card.lines[1]), "42 ms") || !strings.HasPrefix(ansi.Strip(card.lines[2]), prefix+"Shadowsocks") {
					t.Fatal("node name displaced its delay or protocol")
				}
				if i >= 1 && i <= 4 && !strings.Contains(ansi.Strip(card.lines[1]), "…") {
					t.Fatal("long node name was not truncated within its name slot")
				}
			}
			if m.focus != "node.0.2" || m.snapshot.Groups[0].Now != names[1] {
				t.Fatal("rendering changed focus or the observed selection")
			}
		})
	}
}

func TestProxyGridResizeKeepsNodeIdentityAndRevealsItsNewRow(t *testing.T) {
	m := proxyHoverModel(t, 48)
	m.focus = "node.0.37"
	m.revealContentFocus()
	m.layout()
	selected := m.snapshot.Groups[0].Now
	for _, size := range [][2]int{{200, 40}, {160, 40}, {96, 40}, {80, 24}, {60, 20}, {40, 16}, {100, 40}, {120, 18}, {200, 40}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			if m.focus != "node.0.37" || m.snapshot.Groups[0].Now != selected || m.proxies.activeGroup != "PROXY" {
				t.Fatalf("resize lost the focused node or selected another node: focus=%s", m.focus)
			}
			g := dashboardGeometry(m.dashboardOptions())
			hit := findTarget(t, m, m.focus)
			if hit.H != 3 || hit.Y < g.y || hit.Y+hit.H > g.y+g.height || m.scroll < 0 || m.scroll > max(0, m.total-g.height) {
				t.Fatalf("resize failed to reveal the new card row: hit=%+v scroll=%d", hit, m.scroll)
			}
			assertCanvas(t, m.View().Content, size[0], size[1])
			columns := m.proxyGrid(g.width).columns
			m, _ = update(m, special(tea.KeyDown))
			if want := fmt.Sprintf("node.0.%d", 37+columns); m.focus != want {
				t.Fatalf("Down after resize used the previous column count: got %s want %s", m.focus, want)
			}
			m, _ = update(m, special(tea.KeyUp))
			if m.focus != "node.0.37" {
				t.Fatal("Up after resize did not return to the same node")
			}
			if columns > 1 {
				m, _ = update(m, special(tea.KeyLeft))
				if m.focus != "node.0.36" {
					t.Fatal("Left after resize did not reach the adjacent card")
				}
				m, _ = update(m, special(tea.KeyRight))
				if m.focus != "node.0.37" {
					t.Fatal("Right after resize did not return to the same node")
				}
			}
		})
	}
}
