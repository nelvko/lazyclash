package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestProxyGridUsesMinimumCardWidth(t *testing.T) {
	for _, test := range []struct {
		width, indent, columns, cardWidth int
	}{
		{0, 0, 1, 1}, {28, 0, 1, 28}, {29, 0, 1, 29},
		{58, 0, 1, 58}, {59, 0, 2, 29},
		{88, 0, 2, 43}, {89, 0, 3, 29},
		{118, 0, 3, 38}, {119, 0, 4, 29},
		{60, 2, 1, 58}, {61, 2, 2, 29},
		{91, 2, 3, 29}, {121, 2, 4, 29},
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
	for _, width := range []int{40, 62, 63, 80, 92, 93, 120, 145, 146, 160, 200} {
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
			nameRow, cardRows := 0, 2
			if width >= 115 {
				nameRow, cardRows = 1, 4
			}
			for i := range names {
				card := m.proxyNode(0, i, grid.cardWidth)
				if len(card.lines) != cardRows {
					t.Fatalf("node card occupies %d rows, want %d", len(card.lines), cardRows)
				}
				for _, line := range card.lines {
					if ansi.StringWidth(line) != grid.cardWidth {
						t.Fatalf("Unicode card changed its slot width: %q", ansi.Strip(line))
					}
				}
				prefix := "  "
				if i == 1 {
					prefix = "▌ "
				}
				if !strings.Contains(ansi.Strip(card.lines[nameRow]), "42 ms") || !strings.HasPrefix(ansi.Strip(card.lines[nameRow+1]), prefix+"Shadowsocks") {
					t.Fatal("node name displaced its delay or protocol")
				}
				if i >= 1 && i <= 4 && !strings.Contains(ansi.Strip(card.lines[nameRow]), "…") {
					t.Fatal("long node name was not truncated within its name slot")
				}
			}
			if m.focus != "node.0.2" || m.snapshot.Groups[0].Now != names[1] {
				t.Fatal("rendering changed focus or the observed selection")
			}
		})
	}
}

func TestProxyGridFitsItsCardEdgesToTerminalSizeAndColors(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		profile       colorprofile.Profile
		noColor       bool
		cardRows, gap int
	}{
		{115, 25, colorprofile.TrueColor, false, 4, 0},
		{160, 40, colorprofile.ANSI256, false, 4, 0},
		{160, 40, colorprofile.ANSI, false, 2, 1},
		{160, 40, colorprofile.ASCII, false, 2, 1},
		{160, 40, colorprofile.TrueColor, true, 2, 1},
		{114, 25, colorprofile.TrueColor, false, 2, 0},
		{115, 24, colorprofile.TrueColor, false, 2, 0},
	} {
		t.Run(fmt.Sprintf("%dx%d/%v/noColor=%t", tc.width, tc.height, tc.profile, tc.noColor), func(t *testing.T) {
			m, _ := proxiesModel(t, tc.width, tc.height)
			m.colorProfile, m.noColor = tc.profile, tc.noColor
			width := dashboardBodyWidth(m.dashboardOptions())
			grid := m.proxyGrid(width)
			body := m.proxyGroupBody(0, width)
			rows := (len(m.snapshot.Groups[0].All) + grid.columns - 1) / grid.columns
			stride := tc.cardRows + tc.gap
			if got, want := len(body.lines), rows*stride-tc.gap; got != want {
				t.Fatalf("%d rows of cards occupy %d lines, want %d", rows, got, want)
			}
			for y, line := range body.lines {
				plain := ansi.Strip(line)
				position := y % stride
				if tc.cardRows == 4 && position == 0 && !strings.Contains(plain, "▄") ||
					tc.cardRows == 4 && position == 3 && !strings.Contains(plain, "▀") ||
					tc.cardRows == 2 && strings.ContainsAny(plain, "▄▀▗▝") {
					t.Fatalf("unexpected card edge at row %d: %q", y, plain)
				}
				if blank := strings.TrimSpace(plain) == ""; blank != (position == tc.cardRows) {
					t.Fatalf("unexpected grid spacing at row %d: %q", y, plain)
				}
				if position == tc.cardRows {
					for _, hit := range body.targets {
						if hit.Y <= y && y < hit.Y+hit.H {
							t.Fatalf("gap at row %d activates a card: %+v", y, hit)
						}
					}
				}
			}
		})
	}
}

func TestProxyCardsCenterBothTextRowsAndAlignTheSelectedRail(t *testing.T) {
	for _, tc := range []struct {
		name, surface, meta string
		selected, hover     bool
	}{
		{"normal", gridCardBackground, muted, false, false},
		{"hover", raised, proxyNodeEmphasizedMeta, false, true},
		{"selected", proxyNodeSelectedBackground, proxyNodeEmphasizedMeta, true, false},
		{"hover on selected", proxyNodeSelectedBackground, proxyNodeEmphasizedMeta, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := proxiesModel(t, 150, 40)
			if tc.selected {
				m.snapshot.Groups[0].Now = "HK 01"
			}
			if tc.hover {
				m.hover = "node.0.1"
			}
			card := m.proxyNode(0, 1, 29)
			screen := uv.NewScreenBuffer(29, 4)
			uv.NewStyledString(strings.Join(card.lines, "\n")).Draw(screen, screen.Bounds())
			for _, pixel := range []struct {
				x, y                          int
				glyph, foreground, background string
				attrs                         uint8
			}{
				{1, 0, "▄", tc.surface, bg, 0},
				{2, 1, "H", text, tc.surface, uv.AttrBold}, {2, 2, "S", tc.meta, tc.surface, 0},
				{1, 3, "▀", tc.surface, bg, 0},
			} {
				got := screen.CellAt(pixel.x, pixel.y)
				want := uv.Style{Fg: lipgloss.Color(pixel.foreground), Bg: lipgloss.Color(pixel.background), Attrs: pixel.attrs}
				if got == nil || got.Content != pixel.glyph || !got.Style.Equal(&want) {
					t.Fatalf("wrong face at (%d,%d): got %+v, want %q with %+v", pixel.x, pixel.y, got, pixel.glyph, want)
				}
			}
			if !tc.selected {
				for _, y := range []int{0, 3} {
					first, next := screen.CellAt(0, y), screen.CellAt(1, y)
					if first.Content != next.Content || !first.Style.Equal(&next.Style) {
						t.Fatalf("unselected edge has a seam on row %d: %+v", y, first)
					}
				}
				return
			}
			for y, glyph := range []string{"▗", "▌", "▌", "▝"} {
				fg, background := accent, bg
				if glyph == "▌" {
					fg, background = bg, accent // the accent fills the right half of the cell
				}
				got := screen.CellAt(0, y)
				if got.Content != glyph || !got.Style.Equal(&uv.Style{Fg: lipgloss.Color(fg), Bg: lipgloss.Color(background)}) {
					t.Fatalf("half-cell rail differs on row %d: %+v", y, got)
				}
			}
		})
	}
}

func TestProxyNetworkDialogClearsCardEdgesWithoutReflow(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	width := dashboardBodyWidth(m.dashboardOptions())
	grid := m.proxyGrid(width)
	total := m.total
	before := m.proxyNode(0, 0, grid.cardWidth)
	if len(before.lines) != 4 || !strings.Contains(m.screen, "▄") || !strings.Contains(m.screen, "▗") {
		t.Fatal("rich grid fixture lost its visible card edges")
	}

	m.openNetwork(false)
	m.layout()
	card := m.proxyNode(0, 0, grid.cardWidth)
	if !m.wizard.networkOpen || strings.ContainsAny(m.screen, "▄▀▗▝") {
		t.Fatal("opening network settings left block glyphs in the backdrop")
	}
	if m.total != total || len(card.lines) != len(before.lines) || m.proxyGrid(width) != grid {
		t.Fatal("opening network settings changed the grid geometry")
	}
	for _, y := range []int{0, 3} {
		if ansi.Strip(card.lines[y]) != strings.Repeat(" ", grid.cardWidth) {
			t.Fatalf("dimmed edge at row %d did not keep its blank cells", y)
		}
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.wizard.networkOpen || !strings.Contains(m.screen, "▄") || !strings.Contains(m.screen, "▗") || m.total != total {
		t.Fatal("closing network settings did not restore the card edges in place")
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
			cardHeight := len(m.proxyNode(0, 37, m.proxyGrid(g.width).cardWidth).lines)
			if hit.H != cardHeight || hit.Y < g.y || hit.Y+hit.H > g.y+g.height || m.scroll < 0 || m.scroll > max(0, m.total-g.height) {
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
