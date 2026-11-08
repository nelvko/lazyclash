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
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func setProxyTestGroup(m *Model, name string) {
	m.proxies.expanded = make(map[string]bool)
	m.proxies.activeGroup = name
	if name != "" {
		m.proxies.expanded[name] = true
	}
}

func proxiesModel(t *testing.T, width, height int) (Model, *proxiesPTYBackend) {
	t.Helper()
	b := newProxiesPTYBackend()
	m := New(t.Context(), b, "en")
	m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	m.logoIntro.used = true
	m.width, m.height = width, height
	m.navigate("proxies")
	setProxyTestGroup(&m, "PROXY")
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
	currentLines, focusedLines := strings.Split(current, "\n"), strings.Split(focused, "\n")
	if len(currentLines) != 3 || len(focusedLines) != 3 || !strings.HasPrefix(currentLines[1], "▌   AUTO") || !strings.HasPrefix(focusedLines[1], "  › HK 01") || strings.Contains(current+focused, "✓") {
		t.Fatalf("focus/selection markers confused: current=%q focused=%q", current, focused)
	}
	for _, line := range currentLines {
		if !strings.HasPrefix(line, "▌ ") || strings.Contains(focused, "▌") {
			t.Fatalf("selection rail did not span only the selected card: current=%q focused=%q", current, focused)
		}
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
	if !strings.HasPrefix(strings.Split(selected, "\n")[1], "▌ › HK 01") {
		t.Fatalf("focus erased current selection: %q", selected)
	}
}

func TestProxiesAutomaticGroupsOpenDetailsAndReferencesBrowse(t *testing.T) {
	m, b := proxiesModel(t, 160, 40)
	setProxyTestGroup(&m, "AUTO")
	m.focus = "node.1.0"
	m.rememberPageView()
	m.layout()
	m, cmd := update(m, special(tea.KeyEnter))
	if cmd != nil || m.busy || m.proxies.detail != "HK 01" || b.selections != 0 || m.snapshot.Groups[1].Now != "HK 02" {
		t.Fatal("automatic group submitted a manual selection instead of opening details")
	}
	m.proxyBack()
	setProxyTestGroup(&m, "PROXY")
	m.focus = "node.0.0"
	m.rememberPageView()
	m.layout()
	m.proxyActivate("proxies.reference.0.0")
	m.layout()
	if m.proxies.activeGroup != "AUTO" || len(m.proxies.history) != 1 {
		t.Fatal("reference browsing did not preserve the parent location")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.proxies.activeGroup != "PROXY" || m.focus != "proxies.reference.0.0" {
		t.Fatalf("reference return lost its location: %+v focus=%s", m.proxies, m.focus)
	}
}

func TestProxiesReturnRestoresNamesAfterGroupAndMemberReordering(t *testing.T) {
	for _, origin := range []string{"node", "detail", "reference"} {
		t.Run(origin, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 40)
			m.focus = "node.0.0"
			m.rememberPageView()
			if origin == "detail" {
				m.openProxyDetail("PROXY", "AUTO")
			} else if origin == "reference" {
				m.proxyActivate("proxies.reference.0.0")
			} else {
				m.browseProxyReference("AUTO")
			}
			m.snapshot.Groups[0], m.snapshot.Groups[2] = m.snapshot.Groups[2], m.snapshot.Groups[0]
			g := &m.snapshot.Groups[2]
			g.All[0], g.All[2] = g.All[2], g.All[0]
			wantFocus := "node.2.2"
			if origin == "reference" {
				wantFocus = "proxies.reference.2.2"
			}
			if !m.proxyBack() || m.proxies.activeGroup != "PROXY" || m.focus != wantFocus {
				t.Fatalf("return followed stale row indexes: group=%s focus=%s", m.proxies.activeGroup, m.focus)
			}
		})
	}
}

func TestProxiesReferenceActionFocusSurvivesLiveReordering(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.focus = "proxies.reference.0.0"
	m.layout()
	m.snapshot.Groups[0], m.snapshot.Groups[2] = m.snapshot.Groups[2], m.snapshot.Groups[0]
	g := &m.snapshot.Groups[2]
	g.All[0], g.All[2] = g.All[2], g.All[0]
	m.layout()
	if m.focus != "proxies.reference.2.2" || m.pageViews["proxies"].group != "PROXY" || m.pageViews["proxies"].node != "AUTO" {
		t.Fatalf("live refresh reassigned reference action focus: focus=%s state=%+v", m.focus, m.pageViews["proxies"])
	}
	g.All = append(g.All[:2], g.All[3:]...)
	m.layout()
	if m.focus != "group.2" {
		t.Fatalf("removed reference did not return focus to its own group: %s", m.focus)
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
			setProxyTestGroup(&m, "GLOBAL")
			m.focus = "node.8.0"
			m.browseProxyReference("PROXY")
			groups = m.visibleProxyGroups()
			want := []string{"PROXY", "GLOBAL"}
			if width < 100 {
				want = []string{"PROXY"}
			}
			var names []string
			for _, gi := range groups {
				names = append(names, m.snapshot.Groups[gi].Name)
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("global mode prevented explicit reference browsing: got %v, want %v", names, want)
			}
			m.proxyBack()
			if m.proxies.activeGroup != "GLOBAL" || m.focus != "node.8.0" {
				t.Fatalf("global reference back failed: group=%s focus=%s", m.proxies.activeGroup, m.focus)
			}
		})
	}
}

func TestProxiesClickingModeUsesSharedAction(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m, cmd := click(m, findTarget(t, m, "mode.global"))
	if !m.busy || cmd == nil {
		t.Fatal("mode click did not begin its action")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.snapshot.Mode.Actual != "global" {
		t.Fatalf("mode click did not adopt the confirmed mode: busy=%t mode=%s", m.busy, m.snapshot.Mode.Actual)
	}
}

func TestProxiesGridVerticalAndHorizontalTraversal(t *testing.T) {
	for _, test := range []struct{ width, columns int }{{160, 3}, {200, 4}} {
		t.Run(fmt.Sprint(test.columns, "columns"), func(t *testing.T) {
			m, _ := proxiesModel(t, test.width, 40)
			m.snapshot.Groups[0].All = nil
			for i := 0; i < 11; i++ {
				m.snapshot.Groups[0].All = append(m.snapshot.Groups[0].All, fmt.Sprintf("leaf-%02d", i))
			}
			m.layout()
			if columns := m.proxyGrid(dashboardBodyWidth(m.dashboardOptions())).columns; columns != test.columns {
				t.Fatalf("width %d produced %d columns, want %d", test.width, columns, test.columns)
			}
			m, _ = update(m, special(tea.KeyDown))
			if m.focus != "node.0.0" {
				t.Fatalf("Down from expanded group skipped its first member: %s", m.focus)
			}
			m.focus = "node.0.1"
			m.rememberPageView()
			m, _ = update(m, special(tea.KeyRight))
			if m.focus != "node.0.2" {
				t.Fatalf("Right skipped the nearest column: %s", m.focus)
			}
			m, _ = update(m, special(tea.KeyLeft))
			if m.focus != "node.0.1" {
				t.Fatalf("Left skipped the nearest column: %s", m.focus)
			}
			m, _ = update(m, special(tea.KeyDown))
			if want := fmt.Sprintf("node.0.%d", 1+test.columns); m.focus != want {
				t.Fatalf("Down changed the active column: got %s, want %s", m.focus, want)
			}
			m, _ = update(m, special(tea.KeyUp))
			if m.focus != "node.0.1" {
				t.Fatalf("Up changed the active column: %s", m.focus)
			}
			lastIndex := len(m.snapshot.Groups[0].All) - 1
			lastRowStart := lastIndex / test.columns * test.columns
			m.focus = fmt.Sprintf("node.0.%d", lastRowStart-1)
			m.rememberPageView()
			m, _ = update(m, special(tea.KeyDown))
			if want := fmt.Sprintf("node.0.%d", lastIndex); m.focus != want {
				t.Fatalf("Down into a partial row missed its nearest card: got %s, want %s", m.focus, want)
			}
			m, _ = update(m, special(tea.KeyRight))
			if want := fmt.Sprintf("node.0.%d", lastIndex); m.focus != want {
				t.Fatalf("Right crossed the row boundary: got %s, want %s", m.focus, want)
			}
			m, _ = update(m, special(tea.KeyUp))
			if want := fmt.Sprintf("node.0.%d", lastIndex-test.columns); m.focus != want {
				t.Fatalf("Up from a partial row changed its column: got %s, want %s", m.focus, want)
			}
		})
	}
}

func TestProxiesVerticalTraversalStopsAtBoundaryRows(t *testing.T) {
	for _, test := range []struct{ width, columns int }{{160, 3}, {200, 4}} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprint(test.columns, "columns/partial=", partial), func(t *testing.T) {
				m, _ := proxiesModel(t, test.width, 40)
				m.snapshot.Groups = m.snapshot.Groups[:1]
				group := &m.snapshot.Groups[0]
				group.All = nil
				count := 2 * test.columns
				if partial {
					count--
				}
				for i := 0; i < count; i++ {
					group.All = append(group.All, fmt.Sprintf("leaf-%02d", i))
				}
				group.Now = group.All[0]
				m.layout()
				for index := test.columns; index < count; index++ {
					for _, down := range []tea.KeyPressMsg{special(tea.KeyDown), key("j")} {
						want := fmt.Sprintf("node.0.%d", index)
						m.focus = want
						m.rememberPageView()
						m, _ = update(m, down)
						if m.focus != want {
							t.Fatalf("Down at final row moved sideways: got %s, want %s", m.focus, want)
						}
					}
				}
				for column := 0; column < test.columns; column++ {
					m.focus = fmt.Sprintf("node.0.%d", column)
					m.rememberPageView()
					m, _ = update(m, special(tea.KeyUp))
					if m.focus != "group.0" {
						t.Fatalf("Up from the first card row skipped its group heading: %s", m.focus)
					}
					m, _ = update(m, special(tea.KeyUp))
					if m.focus != "group.0" {
						t.Fatalf("Up at the first list row moved focus elsewhere: %s", m.focus)
					}
				}
			})
		}
	}
}

func TestProxiesPendingSelectionKeepsVerticalGridNavigation(t *testing.T) {
	for _, test := range []struct{ width, columns int }{{160, 3}, {200, 4}} {
		t.Run(fmt.Sprint(test.columns, "columns"), func(t *testing.T) {
			m, backend := proxiesModel(t, test.width, 40)
			m.snapshot.Groups = m.snapshot.Groups[:1]
			m.focus = "node.0.1"
			m.layout()
			m, pending := update(m, special(tea.KeyEnter))
			if !m.busy || pending == nil || m.proxies.pendingNode != "HK 01" || backend.selections != 0 {
				t.Fatal("selection did not enter the pending backend operation")
			}
			// Keep the selection command pending while driving normal Update keys.
			m, _ = update(m, special(tea.KeyDown))
			if want := fmt.Sprintf("node.0.%d", 1+test.columns); m.focus != want {
				t.Fatalf("pending selection changed the column on Down: got %s, want %s", m.focus, want)
			}
			m, _ = update(m, special(tea.KeyUp))
			if m.focus != "node.0.1" {
				t.Fatalf("pending selection changed the column on Up: %s", m.focus)
			}
			lastRow := (len(m.snapshot.Groups[0].All) - 1) / test.columns * test.columns
			for index := lastRow; index < len(m.snapshot.Groups[0].All); index++ {
				m.focus = fmt.Sprintf("node.0.%d", index)
				m.rememberPageView()
				want := m.focus
				m, _ = update(m, special(tea.KeyDown))
				if m.focus != want {
					t.Fatalf("pending selection moved sideways at the final row: got %s, want %s", m.focus, want)
				}
			}
			if !m.busy || backend.selections != 0 || m.snapshot.Groups[0].Now != "AUTO" {
				t.Fatal("grid navigation completed or changed the pending selection")
			}
		})
	}
}

func TestProxiesShortGroupsKeepTheAvailableColumnWidth(t *testing.T) {
	for _, size := range []struct{ width, columns int }{{80, 2}, {120, 2}, {200, 4}} {
		for _, test := range []struct {
			name  string
			count int
		}{{"one member", 1}, {"two members", 2}} {
			t.Run(fmt.Sprint(size.width, "/", test.name), func(t *testing.T) {
				m, _ := proxiesModel(t, size.width, 40)
				width := dashboardBodyWidth(m.dashboardOptions())
				full := m.proxyGroupBody(0, width)
				left, right := width, 0
				for _, hit := range full.targets {
					if hit.ID == "node.0.0" {
						left, right = min(left, hit.X), max(right, hit.X+hit.W)
					}
				}
				cardWidth := right - left
				m.snapshot.Groups[0].All = m.snapshot.Groups[0].All[:test.count]
				body := m.proxyGroupBody(0, width)
				type bounds struct{ left, right int }
				cards := make(map[string]bounds)
				for _, hit := range body.targets {
					if !strings.HasPrefix(hit.ID, "node.") {
						continue
					}
					card, found := cards[hit.ID]
					if !found {
						card.left = hit.X
					}
					card.left, card.right = min(card.left, hit.X), max(card.right, hit.X+hit.W)
					cards[hit.ID] = card
				}
				wantCount := test.count
				if len(cards) != wantCount || m.proxyGrid(width).columns != size.columns {
					t.Fatalf("short group lost cards or changed its available slots: cards=%d columns=%d", len(cards), m.proxyGrid(width).columns)
				}
				positions := make(map[int]bool)
				for _, card := range cards {
					if card.right-card.left != cardWidth {
						t.Fatalf("short group stretched its card: bounds=%+v want width=%d", card, cardWidth)
					}
					positions[card.left] = true
				}
				for column := 0; column < wantCount; column++ {
					if !positions[m.proxyGrid(width).indent+column*(cardWidth+1)] {
						t.Fatalf("short group did not retain left-aligned column slots: %+v", positions)
					}
				}
			})
		}
	}
}

func TestProxiesColumnsStopAtFourOnWideScreens(t *testing.T) {
	for _, test := range []struct{ width, columns int }{{60, 1}, {76, 1}, {77, 2}, {80, 2}, {96, 2}, {100, 2}, {120, 2}, {137, 3}, {160, 3}, {174, 4}, {200, 4}, {240, 4}, {400, 4}} {
		t.Run(fmt.Sprint(test.width), func(t *testing.T) {
			m, _ := proxiesModel(t, test.width, 40)
			if got := m.proxyGrid(dashboardBodyWidth(m.dashboardOptions())).columns; got != test.columns {
				t.Fatalf("terminal width %d produced %d columns, want %d", test.width, got, test.columns)
			}
		})
	}
}

func TestProxiesCardPaddingKeepsItsReferenceActionIndependent(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.focus = "node.0.0"
	const width = 50
	card := m.proxyNode(0, 0, width)
	if len(card.lines) != 3 {
		t.Fatalf("card has %d rows, want three including top padding", len(card.lines))
	}
	for y, line := range card.lines {
		line = ansi.Strip(line)
		if ansi.StringWidth(line) != width || !strings.HasPrefix(line, "▌ ") || !strings.HasSuffix(line, "  ") {
			t.Fatalf("card row %d lost its rail or padding: %q", y, line)
		}
		if y == 0 && strings.TrimSpace(strings.TrimPrefix(line, "▌")) != "" {
			t.Fatalf("vertical padding row contains content: %q", line)
		}
	}
	if !strings.HasPrefix(ansi.Strip(card.lines[1]), "▌ › AUTO") || !strings.HasPrefix(ansi.Strip(card.lines[2]), "▌   Proxy group") {
		t.Fatalf("name and metadata lost their shared left padding: %+v", card.lines)
	}
	assertRegions(t, card.targets, width, 3)
	for i, hit := range card.targets {
		for _, other := range card.targets[i+1:] {
			if regionsOverlap(hit, other) {
				t.Fatalf("padded selection and reference actions overlap: %+v / %+v", hit, other)
			}
		}
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < width; x++ {
			var hitID string
			for _, hit := range card.targets {
				if x >= hit.X && x < hit.X+hit.W && y >= hit.Y && y < hit.Y+hit.H {
					hitID = hit.ID
				}
			}
			want := "node.0.0"
			for _, hit := range card.targets {
				switch hit.ID {
				case "node.0.0":
					continue
				case "proxies.reference.0.0":
					if hit.Y != 2 || hit.X+hit.W != width-2 {
						t.Fatalf("reference action lost its metadata row or right padding: %+v", hit)
					}
				case "proxies.delay.0.0":
					if hit.Y != 1 || hit.H != 1 || hit.W != 12 || hit.X+hit.W != width-2 {
						t.Fatalf("delay action lost its fixed name-row slot or right padding: %+v", hit)
					}
				default:
					t.Fatalf("unexpected card action: %+v", hit)
				}
				if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
					want = hit.ID
				}
			}
			if hitID != want {
				t.Fatalf("card cell (%d,%d) routes to %q, want %q", x, y, hitID, want)
			}
		}
	}
	if !hasAction(card.targets, "proxies.reference.0.0") || !hasAction(card.targets, "proxies.delay.0.0") {
		t.Fatal("reference card lost its independent entrance or delay action")
	}
	for _, scroll := range []int{0, 2} {
		if got := proxyVisibleRange(m.locale, card.targets, scroll, 1); got != "1–1 / 1" {
			t.Fatalf("visible card padding disappeared from the range: row=%d got=%q", scroll, got)
		}
	}
}

func TestProxiesCardPaddingMouseClicksSelectItsNode(t *testing.T) {
	for _, position := range []string{"top left", "top right", "metadata left", "metadata right"} {
		t.Run(position, func(t *testing.T) {
			m, backend := proxiesModel(t, 160, 40)
			m.snapshot.Groups[0].Now = "HK 01"
			m.layout()
			left, right, top := m.width, 0, m.height
			for _, hit := range m.targets {
				if hit.ID == "node.0.0" {
					left, right = min(left, hit.X), max(right, hit.X+hit.W)
					top = min(top, hit.Y)
				}
			}
			x, y := left, top
			if strings.Contains(position, "right") {
				x = right - 1
			}
			if strings.Contains(position, "metadata") {
				y = top + 2
			}
			m, cmd := update(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if cmd == nil || !m.busy || m.proxies.pendingNode != "AUTO" || m.proxies.activeGroup != "PROXY" {
				t.Fatalf("padding click did not select its reference card: position=%s focus=%s pending=%s expanded=%s", position, m.focus, m.proxies.pendingNode, m.proxies.activeGroup)
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if backend.selections != 1 || m.snapshot.Groups[0].Now != "AUTO" {
				t.Fatal("padding selection was not confirmed by the backend")
			}
		})
	}
}

func TestProxiesReferenceTraversalKeepsWideDirectoryAndRestoresLocation(t *testing.T) {
	for _, width := range []int{80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _ := proxiesModel(t, width, 20)
			m.focus = "node.0.0"
			m.scroll = 4
			m.rememberPageView()
			location := m.proxyLocation()
			m.browseProxyReference("AUTO")
			m.layout()
			groups := m.visibleProxyGroups()
			if width < 100 {
				if len(groups) != 1 || m.snapshot.Groups[groups[0]].Name != "AUTO" {
					t.Fatalf("narrow reference view mixed in the directory: %v", groups)
				}
			} else if len(groups) != len(m.snapshot.Groups)-1 {
				t.Fatalf("wide reference view hid root groups: %v", groups)
			}
			if m.proxies.activeGroup != "AUTO" || !hasAction(m.targets, "group.1") {
				t.Fatalf("reference target was not expanded and revealed: expanded=%s focus=%s scroll=%d", m.proxies.activeGroup, m.focus, m.scroll)
			}
			if !m.proxyBack() || m.proxies.activeGroup != location.group || m.focus != location.focus || m.scroll != location.scroll {
				t.Fatalf("reference return lost parent location: got %+v, want %+v", m.proxyLocation(), location)
			}
		})
	}
}

func TestProxiesDistantReferenceRevealsMembersAndRestoresParent(t *testing.T) {
	for _, width := range []int{80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _ := proxiesModel(t, width, 20)
			parent := m.snapshot.Groups[0]
			parent.All = []string{"HK 01", "HK 02", "JP 01", "SG 01", "US 01", "DIRECT", "REJECT"}
			for i := 7; i < 16; i++ {
				parent.All = append(parent.All, fmt.Sprintf("parent-leaf-%d", i))
			}
			parent.All = append(parent.All, "REMOTE")
			parent.Now = "HK 01"
			m.snapshot.Groups = []mihomo.Group{parent}
			for i := 0; i < 24; i++ {
				m.snapshot.Groups = append(m.snapshot.Groups, mihomo.Group{Name: fmt.Sprintf("spacer-%02d", i), Type: "Selector", Now: "HK 01", All: []string{"HK 01"}})
			}
			target := len(m.snapshot.Groups)
			m.snapshot.Groups = append(m.snapshot.Groups, mihomo.Group{Name: "REMOTE", Type: "Selector", Now: "JP 01", All: []string{"HK 01", "HK 02", "JP 01", "SG 01", "US 01"}})
			reference := fmt.Sprintf("proxies.reference.0.%d", len(parent.All)-1)
			m.focus, m.scroll = reference, 4
			m.rememberPageView()
			m.layout()
			location := m.proxyLocation()
			if location.scroll <= 0 {
				t.Fatal("fixture did not retain a valid scrolled parent position")
			}
			m.proxyActivate(reference)
			m.layout()
			if m.proxies.activeGroup != "REMOTE" || !hasAction(m.targets, fmt.Sprintf("node.%d.0", target)) {
				t.Fatalf("distant reference revealed only its heading: expanded=%s scroll=%d targets=%+v", m.proxies.activeGroup, m.scroll, m.targets)
			}
			m, _ = update(m, special(tea.KeyEscape))
			if m.proxies.activeGroup != location.group || m.focus != location.focus || m.scroll != location.scroll {
				t.Fatalf("return from distant reference lost the parent location: got %+v, want %+v", m.proxyLocation(), location)
			}
		})
	}
}

func TestProxiesReferenceKeysPreferAdjacentNodeBeforeBrowsing(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.focus = "node.0.0"
	m.rememberPageView()
	m, _ = update(m, special(tea.KeyRight))
	if m.focus != "node.0.1" || m.proxies.activeGroup != "PROXY" || len(m.proxies.history) != 0 {
		t.Fatalf("Right entered a reference before moving to its adjacent node: focus=%s expanded=%s", m.focus, m.proxies.activeGroup)
	}
	columns := m.proxyGrid(dashboardBodyWidth(m.dashboardOptions())).columns
	m.snapshot.Groups[0].All[0], m.snapshot.Groups[0].All[columns-1] = m.snapshot.Groups[0].All[columns-1], m.snapshot.Groups[0].All[0]
	m.focus = fmt.Sprintf("node.0.%d", columns-1)
	m.rememberPageView()
	m, _ = update(m, special(tea.KeyRight))
	if m.proxies.activeGroup != "AUTO" || len(m.proxies.history) != 1 {
		t.Fatalf("Right at the row edge did not enter its reference: expanded=%s history=%d", m.proxies.activeGroup, len(m.proxies.history))
	}
	m.focus = "node.1.1"
	m.rememberPageView()
	m, _ = update(m, special(tea.KeyLeft))
	if m.focus != "node.1.0" || m.proxies.activeGroup != "AUTO" || len(m.proxies.history) != 1 {
		t.Fatalf("Left returned before moving to its adjacent node: focus=%s expanded=%s", m.focus, m.proxies.activeGroup)
	}
	m, _ = update(m, special(tea.KeyLeft))
	if m.proxies.activeGroup != "PROXY" || m.focus != fmt.Sprintf("node.0.%d", columns-1) || len(m.proxies.history) != 0 {
		t.Fatalf("Left at the row edge lost its parent reference: focus=%s expanded=%s", m.focus, m.proxies.activeGroup)
	}
}

func TestProxiesReferenceActionDoesNotSelectItsNode(t *testing.T) {
	for _, width := range []int{80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, backend := proxiesModel(t, width, 40)
			m, cmd := click(m, findTarget(t, m, "proxies.reference.0.0"))
			if cmd != nil || m.busy || backend.selections != 0 || m.proxies.activeGroup != "AUTO" {
				t.Fatalf("reference action selected its parent node: expanded=%s selections=%d busy=%t", m.proxies.activeGroup, backend.selections, m.busy)
			}
			m.proxyBack()
			m.snapshot.Groups[0].Now = "HK 01"
			m.layout()
			m, cmd = click(m, findTarget(t, m, "node.0.0"))
			if cmd == nil || !m.busy || m.proxies.activeGroup != "PROXY" || m.proxies.pendingNode != "AUTO" {
				t.Fatal("reference card body lost its normal selector action")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if backend.selections != 1 || m.snapshot.Groups[0].Now != "AUTO" {
				t.Fatal("reference card selection was not confirmed through the backend")
			}
		})
	}
	// An automatic strategy may contain references, which remain inspectable.
	m, backend := proxiesModel(t, 160, 40)
	m.snapshot.Groups[1].All = append(m.snapshot.Groups[1].All, "Japan")
	setProxyTestGroup(&m, "AUTO")
	m.focus = "group.1"
	m.layout()
	m, cmd := click(m, findTarget(t, m, "proxies.reference.1.4"))
	if cmd != nil || backend.selections != 0 || m.proxies.activeGroup != "Japan" {
		t.Fatalf("automatic reference action was unavailable or selected a node: expanded=%s selections=%d", m.proxies.activeGroup, backend.selections)
	}
}

func TestProxiesToolbarKeepsModesAndCollapseActionReachable(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{160, 80, 40} {
			t.Run(fmt.Sprint(language, "/", width), func(t *testing.T) {
				m, _ := proxiesModel(t, width, 40)
				m.locale = i18n.New(language)
				bodyWidth := dashboardBodyWidth(m.dashboardOptions())
				toolbar := m.proxiesToolbar(bodyWidth)
				targets := make(map[string]hitRegion)
				for _, hit := range toolbar.targets {
					targets[hit.ID] = hit
					if hit.X+hit.W > bodyWidth || hit.Y+hit.H > len(toolbar.lines) {
						t.Fatalf("toolbar hit exceeds its rendered content: %+v", hit)
					}
				}
				for _, mode := range []string{"rule", "global", "direct"} {
					if targets["mode."+mode].ID == "" {
						t.Fatalf("toolbar lost %s mode", mode)
					}
				}
				if width >= 100 {
					mode, collapse := targets["mode.direct"], targets["proxies.collapse_all"]
					if collapse.ID == "" || collapse.Y != mode.Y || collapse.X < mode.X+mode.W {
						t.Fatalf("wide collapse action overlaps or left the mode row: mode=%+v collapse=%+v", mode, collapse)
					}
				}
			})
		}
	}
}

func TestProxiesGroupHeaderKeepsRightBadgeAndIndependentTestAction(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 40)
			m.locale = i18n.New(language)
			m.snapshot.Groups[1].All = make([]string, 100)
			width := dashboardBodyWidth(m.dashboardOptions())
			for _, gi := range []int{0, 1, 2} {
				header := m.proxyGroupHeader(gi, width)
				line := ""
				for _, candidate := range header.lines {
					if strings.TrimSpace(ansi.Strip(candidate)) != "" {
						line = ansi.Strip(candidate)
						break
					}
				}
				count := fmt.Sprint(len(m.snapshot.Groups[gi].All))
				index := strings.LastIndex(line, count)
				if ansi.StringWidth(line) != width || index < 0 {
					t.Fatalf("group %d count badge was not aligned to the fixed right column: %q", gi, line)
				}
				end := ansi.StringWidth(line[:index+len(count)])
				if end != width-4 {
					t.Fatalf("group %d count ended at %d, want shared right column %d: %q", gi, end, width-4, line)
				}
				for i, hit := range header.targets {
					for _, other := range header.targets[i+1:] {
						if regionsOverlap(hit, other) {
							t.Fatalf("header controls overlap: %+v / %+v", hit, other)
						}
					}
				}
			}
			m.layout()
			group, test := findTarget(t, m, "group.0"), findTarget(t, m, "proxies.test.0")
			if test.Y < group.Y || test.Y >= group.Y+group.H || test.X <= group.X {
				t.Fatalf("wide test action left the group heading: group=%+v test=%+v", group, test)
			}
			m, _ = click(m, test)
			defer m.stopProxyTests()
			if !m.proxies.testing || m.proxies.activeGroup != "PROXY" {
				t.Fatalf("group test action folded or switched the group: expanded=%s testing=%t", m.proxies.activeGroup, m.proxies.testing)
			}
		})
	}
}

func TestProxiesGroupSpacingPreservesCompactWhitespace(t *testing.T) {
	for _, test := range []struct {
		width, height, gap int
		expanded           string
	}{{200, 40, 0, ""}, {160, 40, 0, "PROXY"}, {100, 24, 0, ""}, {160, 23, 1, ""}, {120, 18, 1, "PROXY"}, {80, 40, 1, ""}, {40, 16, 1, ""}} {
		t.Run(fmt.Sprint(test.width, "/", test.height, "/", test.expanded), func(t *testing.T) {
			m, _ := proxiesModel(t, test.width, test.height)
			setProxyTestGroup(&m, test.expanded)
			body, hits := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
			lines := strings.Split(ansi.Strip(body), "\n")
			groups := m.visibleProxyGroups()
			for index := 1; index < len(groups); index++ {
				previous, next := groups[index-1], groups[index]
				end, start := 0, len(lines)
				for _, hit := range hits {
					if hit.ID == fmt.Sprintf("group.%d", previous) || strings.HasPrefix(hit.ID, fmt.Sprintf("node.%d.", previous)) || strings.HasPrefix(hit.ID, fmt.Sprintf("proxies.reference.%d.", previous)) {
						end = max(end, hit.Y+hit.H)
					}
					if hit.ID == fmt.Sprintf("group.%d", next) {
						start = min(start, hit.Y)
					}
				}
				if start-end != test.gap {
					t.Fatalf("groups %d/%d have the wrong external gap: end=%d start=%d want=%d", previous, next, end, start, test.gap)
				}
				for y := end; y < start; y++ {
					if strings.TrimSpace(lines[y]) != "" {
						t.Fatalf("group whitespace contains content at row %d", y)
					}
					for _, hit := range hits {
						if hit.Y <= y && hit.Y+hit.H > y {
							t.Fatalf("group whitespace activates a control: %+v", hit)
						}
					}
				}
			}
		})
	}
}

func TestProxiesExpandedGroupKeepsSortingInHeader(t *testing.T) {
	for _, test := range []struct{ width, height, gap int }{{200, 40, 0}, {160, 24, 0}, {100, 24, 0}, {160, 23, 1}, {120, 18, 1}, {80, 40, 1}, {40, 16, 1}} {
		t.Run(fmt.Sprint(test.width, "/", test.height), func(t *testing.T) {
			m, _ := proxiesModel(t, test.width, test.height)
			bodyWidth := dashboardBodyWidth(m.dashboardOptions())
			body := m.proxyGroupBody(0, bodyWidth)
			if len(body.targets) == 0 || body.targets[0].Y != 0 || hasAction(body.targets, "proxies.sort.0") {
				t.Fatalf("expanded group retained rows before its cards: %+v", body.targets)
			}
			full, hits := m.proxiesBody(bodyWidth)
			lines := strings.Split(ansi.Strip(full), "\n")
			groupY, firstNodeY := len(lines), len(lines)
			for _, hit := range hits {
				if hit.ID == "group.0" {
					groupY = min(groupY, hit.Y)
				}
				if strings.HasPrefix(hit.ID, "node.0.") {
					firstNodeY = min(firstNodeY, hit.Y)
				}
			}
			gapY := groupY + len(m.proxyGroupHeader(0, bodyWidth).lines)
			if firstNodeY-gapY != test.gap {
				t.Fatalf("header-to-card gap is %d, want %d", firstNodeY-gapY, test.gap)
			}
			for y := gapY; y < firstNodeY; y++ {
				if strings.TrimSpace(lines[y]) != "" {
					t.Fatal("header-to-card gap contains content")
				}
				for _, hit := range hits {
					if hit.Y <= y && hit.Y+hit.H > y {
						t.Fatalf("header-to-card gap activates a control: %+v", hit)
					}
				}
			}
			if hasAction(m.proxyGroupHeader(0, bodyWidth).targets, "proxies.sort.0") {
				t.Fatal("default sorting retained a header control")
			}
			m.sortProxies("delay")
			header := m.proxyGroupHeader(0, bodyWidth)
			if !hasAction(header.targets, "proxies.sort.0") || !hasAction(header.targets, "proxies.test.0") {
				t.Fatalf("nondefault sorting lost its header control: %+v", header.targets)
			}
			assertRegions(t, header.targets, bodyWidth, len(header.lines))
			for i, hit := range header.targets {
				for _, other := range header.targets[i+1:] {
					if regionsOverlap(hit, other) {
						t.Fatalf("header controls overlap: %+v / %+v", hit, other)
					}
				}
			}
			m.layout()
			m, _ = click(m, findTarget(t, m, "proxies.sort.0"))
			if !m.menu.open || !m.proxies.sortMenu || m.proxies.activeGroup != "PROXY" {
				t.Fatal("header sorting control folded the group or failed to open the menu")
			}
		})
	}
}

func TestProxiesSummaryKeepsFullReferenceRouteInDetails(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	setProxyTestGroup(&m, "YouTube")
	header := ansi.Strip(strings.Join(m.proxyGroupHeader(3, 120).lines, "\n"))
	card := ansi.Strip(strings.Join(m.proxyNode(3, 0, 60).lines, "\n"))
	for _, value := range []string{header, card} {
		if !strings.Contains(value, "HK 02") || strings.Contains(value, "PROXY →") || strings.Contains(value, "AUTO →") || strings.Contains(value, "Selector") {
			t.Fatalf("reference summary did not reduce to its leaf: %q", value)
		}
	}
	m.openProxyDetail("YouTube", "PROXY")
	detail := ansi.Strip(strings.Join(m.proxyDetail(120).lines, "\n"))
	if !strings.Contains(detail, "PROXY → AUTO → HK 02") || !strings.Contains(detail, "Selector") {
		t.Fatalf("short summaries removed the full route or strategy type from details: %q", detail)
	}
}

func TestProxiesTestStatusExpiresWithoutAffectingNewWork(t *testing.T) {
	for _, result := range []string{"done", "canceled"} {
		t.Run(result, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 40)
			m.testProxyGroup()
			var expiry tea.Cmd
			if result == "canceled" {
				expiry = m.testProxyGroup()
			} else {
				for m.proxies.testing {
					for key := range m.proxies.inflight {
						node := strings.SplitN(key, "\x00", 2)[1]
						expiry = m.acceptProxyDelay(proxyTestMessage(m, "PROXY", node, 44))
						break
					}
				}
			}
			if expiry == nil || !m.proxies.testStatusVisible || m.proxies.testStatus != result || m.proxies.testing {
				t.Fatalf("finished test did not expose a transient result: %+v", m.proxies)
			}
			generation, completed, measurements := m.proxies.generation, m.proxies.completed, len(m.proxies.results)
			fraction := fmt.Sprintf("%d/%d", completed, m.proxies.total)
			body, _ := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
			if !strings.Contains(ansi.Strip(body), fraction) {
				t.Fatal("transient test result was not visible in its group")
			}
			m, _ = update(m, proxyTestStatusExpiredMsg{generation: generation - 1})
			if !m.proxies.testStatusVisible {
				t.Fatal("stale expiry cleared a newer result")
			}
			m, _ = update(m, proxyTestStatusExpiredMsg{generation: generation})
			if m.proxies.testStatusVisible || m.proxies.testStatus != result || m.proxies.completed != completed || len(m.proxies.results) != measurements {
				t.Fatal("expiry removed measurements instead of only the visible status")
			}
			body, _ = m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
			if strings.Contains(ansi.Strip(body), fraction) {
				t.Fatal("expired test status remained visible")
			}
			m.testProxyGroup()
			m, _ = update(m, proxyTestStatusExpiredMsg{generation: generation})
			if !m.proxies.testing {
				t.Fatal("old expiry canceled a newly started test")
			}
			m.stopProxyTests()
			if m.proxies.testStatusVisible {
				t.Fatal("system cancellation exposed a stale result")
			}
		})
	}
}

func TestProxiesVisibleRangeCountsSplitAndPartiallyVisibleCards(t *testing.T) {
	hits := []hitRegion{
		{ID: "group.0", Y: 0, W: 80, H: 1},
		{ID: "node.0.4", Y: 2, W: 36, H: 1},
		{ID: "proxies.reference.0.4", X: 26, Y: 3, W: 10, H: 1},
		{ID: "node.0.4", Y: 3, W: 26, H: 1},
		{ID: "node.0.7", X: 37, Y: 2, W: 36, H: 2},
		{ID: "node.0.9", Y: 5, W: 36, H: 2},
		{ID: "node.0.12", X: 37, Y: 5, W: 36, H: 2},
		{ID: "node.0.16", Y: 8, W: 36, H: 2},
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		locale := i18n.New(language)
		for _, test := range []struct {
			name           string
			scroll, height int
			want           string
		}{
			{"first row", 2, 2, "1–2 / 5"},
			{"split second line", 3, 1, "1–2 / 5"},
			{"between cards", 4, 1, ""},
			{"partial first and last rows", 6, 3, "3–5 / 5"},
			{"last second line", 9, 1, "5–5 / 5"},
			{"after last card", 10, 2, ""},
		} {
			t.Run(language+"/"+test.name, func(t *testing.T) {
				if got := proxyVisibleRange(locale, hits, test.scroll, test.height); got != test.want {
					t.Fatalf("visible card range: got %q, want %q", got, test.want)
				}
			})
		}
		filtered := []hitRegion{hits[0], hits[4], hits[7]}
		if got := proxyVisibleRange(locale, filtered, 0, 10); got != "1–2 / 2" {
			t.Fatalf("filtered range used source indexes or hidden members: %q", got)
		}
	}
}

func TestProxiesVisibleRangeFooterStaysAboveKeyHints(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {80, 24}, {40, 16}, {24, 12}, {20, 7}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _ := proxiesModel(t, size[0], size[1])
			o := m.dashboardOptions()
			if !o.BodyRange {
				t.Fatal("ready expanded group omitted its range footer")
			}
			o.Body = strings.Repeat("candidate\n", 30)
			o.BodyTargets = nil
			for i := 0; i < 15; i++ {
				o.BodyTargets = append(o.BodyTargets, hitRegion{ID: fmt.Sprintf("node.0.%d", i), Y: 2 * i, W: 8, H: 2})
			}
			view, targets, _ := renderDashboard(o)
			assertCanvas(t, view, size[0], size[1])
			geometry := dashboardGeometry(o)
			footerY := geometry.y + geometry.height
			if footerY < dashboardContentBottom(o) {
				label := proxyVisibleRange(o.Locale, o.BodyTargets, 0, geometry.height)
				if label != "" && !strings.Contains(ansi.Strip(strings.Split(view, "\n")[footerY]), label) {
					t.Fatalf("footer row %d did not show its visible range %q", footerY, label)
				}
				for _, hit := range targets {
					if strings.HasPrefix(hit.ID, "node.") && hit.Y+hit.H > footerY {
						t.Fatalf("node target overlaps the range footer: %+v", hit)
					}
				}
			}
			m.proxies.detail = "HK 01"
			if m.dashboardOptions().BodyRange {
				t.Fatal("node details retained the group range footer")
			}
			m.proxies.detail = ""
			setProxyTestGroup(&m, "")
			if m.dashboardOptions().BodyRange {
				t.Fatal("collapsed directory retained the group range footer")
			}
			setProxyTestGroup(&m, "PROXY")
			m.snapshot.Service.Active = false
			if m.dashboardOptions().BodyRange {
				t.Fatal("unavailable group retained the range footer")
			}
		})
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
	if m.menu.open || m.proxySort("PROXY") != "delay" || m.focus != "node.0.1" || !hasAction(m.targets, m.focus) {
		t.Fatalf("sort lost focused member: sort=%s focus=%s", m.proxySort("PROXY"), m.focus)
	}
	m, _ = update(m, key("s"))
	m, _ = update(m, special(tea.KeyEscape))
	if m.focus != "node.0.1" {
		t.Fatal("canceling sorting lost focus")
	}
}

func TestProxiesDefaultSortReturnsToItsGroupAfterHeaderClick(t *testing.T) {
	for _, width := range []int{160, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _ := proxiesModel(t, width, 24)
			// Keep this location scrollable when compact browsing fits two columns.
			m.snapshot.Groups[1].All = append(m.snapshot.Groups[1].All, "JP 02", "US 01", "DIRECT", "REJECT")
			setProxyTestGroup(&m, "AUTO")
			m.focus = "group.1"
			m.sortProxies("delay")
			m.scroll = 3
			m.rememberPageView()
			m.layout()
			before := m.scroll
			if before == 0 {
				t.Fatal("fixture did not retain a scrolled group heading")
			}
			m, _ = click(m, findTarget(t, m, "proxies.sort.1"))
			if !m.menu.open || !m.proxies.sortMenu {
				t.Fatal("header click did not open the sorting menu")
			}
			m, _ = update(m, menuMouse(m.menu, 0, tea.MouseLeft))
			if m.menu.open || m.proxySort("AUTO") != "default" || m.proxies.activeGroup != "AUTO" || m.focus != "group.1" || m.scroll != before {
				t.Fatalf("hiding the sorting control lost its group location: sort=%s expanded=%s focus=%s scroll=%d want=%d", m.proxySort("AUTO"), m.proxies.activeGroup, m.focus, m.scroll, before)
			}
			if hasAction(m.targets, "proxies.sort.1") || !hasAction(m.targets, "group.1") {
				t.Fatal("default sorting retained its control or lost the restored group heading")
			}
		})
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
	setProxyTestGroup(&m, "EMPTY")
	m.focus = "group.7"
	if cmd := m.testProxyGroup(); cmd != nil || m.proxies.testing || m.proxies.cancel != nil {
		t.Fatal("an empty test group retained work or its cancellation context")
	}
	body, _ := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
	if m.proxies.testStatusVisible || strings.Contains(ansi.Strip(body), "0/0") {
		t.Fatal("an untestable group retained an idle progress status")
	}
}

func TestProxiesGeometryAtDesktopAndNarrowSizes(t *testing.T) {
	for _, size := range [][2]int{{240, 45}, {200, 40}, {160, 40}, {120, 30}, {100, 24}, {80, 24}, {60, 20}, {40, 16}, {24, 12}} {
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
			if size[0] >= 160 && width <= 112 {
				t.Fatalf("wide proxy content retained the 112-cell cap: terminal=%d body=%d", size[0], width)
			}
			body, hits := m.proxiesBody(width)
			lines := strings.Split(body, "\n")
			columns := make(map[int]bool)
			seen := make(map[string]bool)
			for i, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Fatalf("body line %d exceeds %d cells: %d", i, width, got)
				}
			}
			for i, hit := range hits {
				if strings.HasPrefix(hit.ID, "node.0.") && !seen[hit.ID] {
					columns[hit.X] = true
					seen[hit.ID] = true
				}
				if hit.X < 0 || hit.Y < 0 || hit.W <= 0 || hit.H <= 0 || hit.X+hit.W > width || hit.Y+hit.H > len(lines) {
					t.Fatalf("invalid hit geometry: %+v width=%d rows=%d", hit, width, len(lines))
				}
				for _, other := range hits[i+1:] {
					if hit.X < other.X+other.W && other.X < hit.X+hit.W && hit.Y < other.Y+other.H && other.Y < hit.Y+hit.H {
						t.Fatalf("overlapping hit regions: %+v / %+v", hit, other)
					}
				}
			}
			if len(columns) != min(len(m.snapshot.Groups[0].All), m.proxyGrid(width).columns) {
				t.Fatalf("rendered columns diverged from navigation: got %d, want %d", len(columns), m.proxyGrid(width).columns)
			}
			if size[0] < 100 {
				if groups := m.visibleProxyGroups(); len(groups) != 1 || m.snapshot.Groups[groups[0]].Name != "PROXY" {
					t.Fatal("narrow member view mixed in the directory")
				}
			}
		})
	}
}
