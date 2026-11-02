package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func visibleSidebarTargets(targets []hitRegion) []hitRegion {
	var visible []hitRegion
	for _, target := range targets {
		if strings.HasPrefix(target.ID, "nav.") {
			visible = append(visible, target)
		}
	}
	return visible
}

func assertSidebarFocusVisible(t *testing.T, m Model, id string, height int) {
	t.Helper()
	hit := findTarget(t, m, id)
	if hit.H != height {
		t.Fatalf("focused menu was compressed: %+v", hit)
	}
	row := strings.Split(m.screen, "\n")[hit.Y+hit.H/2]
	label := ansi.Strip(ansi.Cut(row, hit.X, hit.X+hit.W))
	if !strings.Contains(label, "› "+pageTitle(strings.TrimPrefix(id, "nav."))) {
		t.Fatalf("focused menu label is not visible: %+v %q", hit, label)
	}
	assertCanvas(t, m.screen, m.width, m.height)
	assertRegions(t, m.targets, m.width, m.height)
}

func TestSidebarKeepsThreeLineItemsWhenHeightShrinks(t *testing.T) {
	for _, size := range [][2]int{{120, 26}, {120, 25}, {120, 24}, {100, 20}, {80, 24}, {80, 12}, {60, 10}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			o := dashboardOptions{Width: size[0], Height: size[1], Page: "overview", SidebarOpen: true}
			screen, targets, _ := renderDashboard(o)
			visible := visibleSidebarTargets(targets)
			if len(visible) == 0 {
				t.Fatal("short menu is empty")
			}
			for i, target := range visible {
				if target.H != 3 {
					t.Fatalf("menu item shrank: %+v", target)
				}
				if i > 0 && target.Y != visible[i-1].Y+visible[i-1].H {
					t.Fatal("menu spacing changed between visible items")
				}
			}
			if size[1] <= 24 {
				if len(visible) >= len(navPages()) || hasAction(targets, "nav.settings") {
					t.Fatal("overflowing navigation retained offscreen targets")
				}
				if !strings.Contains(sidebarText(screen), "↓") {
					t.Fatal("overflowing navigation lost its downward hint")
				}
			}
			assertCanvas(t, screen, size[0], size[1])
			assertRegions(t, targets, size[0], size[1])
		})
	}
}

func TestSidebarKeyboardRevealsFirstAndLastWithoutScrollingBody(t *testing.T) {
	for _, size := range [][2]int{{120, 20}, {80, 24}, {80, 12}, {60, 10}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = size[0], size[1]
			m.sidebarOpen, m.focus = size[0] < 100, "nav.overview"
			m.layout()
			bodyScroll := m.scroll
			m, _ = update(m, special(tea.KeyEnd))
			if m.page != "settings" || m.scroll != bodyScroll || m.focus != "nav.settings" || m.sidebar.viewport.YOffset() == 0 {
				t.Fatal("End did not reveal the last menu independently")
			}
			assertSidebarFocusVisible(t, m, "nav.settings", 3)
			if hasAction(m.targets, "nav.overview") || !strings.Contains(sidebarText(m.screen), "↑") {
				t.Fatal("last menu view retained the first menu or lost its upward hint")
			}
			m, _ = update(m, special(tea.KeyHome))
			if m.focus != "nav.overview" || m.sidebar.viewport.YOffset() != 0 || m.page != "overview" || m.scroll != bodyScroll {
				t.Fatal("Home did not restore the first menu independently")
			}
			assertSidebarFocusVisible(t, m, "nav.overview", 3)
		})
	}
}

func TestSidebarWheelIsIndependentAndDoesNotSnapBackOnRender(t *testing.T) {
	m := ready(&fakeBackend{})
	m.guideSkipped = true
	m.width, m.height, m.focus = 120, 20, "nav.overview"
	m.layout()
	hit := findTarget(t, m, "nav.overview")
	page, focus, bodyScroll := m.page, m.focus, m.scroll
	m, _ = update(m, tea.MouseWheelMsg{X: hit.X + 2, Y: hit.Y + 1, Button: tea.MouseWheelDown})
	offset := m.sidebar.viewport.YOffset()
	if offset == 0 || m.page != page || m.focus != focus || m.scroll != bodyScroll {
		t.Fatal("sidebar wheel changed the page, focus or body instead of the menu viewport")
	}
	if hasAction(m.targets, "nav.overview") {
		t.Fatal("scrolled-away menu retained its mouse target")
	}
	m.layout()
	if m.sidebar.viewport.YOffset() != offset {
		t.Fatal("render snapped mouse-scrolled navigation back to the focused item")
	}
	g := dashboardGeometry(m.dashboardOptions())
	m, _ = update(m, tea.MouseWheelMsg{X: g.x + 2, Y: g.y + 2, Button: tea.MouseWheelDown})
	if m.scroll <= bodyScroll || m.sidebar.viewport.YOffset() != offset {
		t.Fatal("body wheel failed to scroll independently of navigation")
	}
	bodyScroll = m.scroll
	menuOffset := m.sidebar.viewport.YOffset()
	for _, y := range []int{0, m.height - 3} {
		m, _ = update(m, tea.MouseWheelMsg{X: 2, Y: y, Button: tea.MouseWheelDown})
		if m.scroll != bodyScroll || m.page != page || m.focus != focus || m.sidebar.viewport.YOffset() != menuOffset {
			t.Fatal("wheel over sidebar chrome scrolled the menu or body")
		}
	}
}

func TestSidebarScrolledMouseTargetOpensVisiblePage(t *testing.T) {
	for _, width := range []int{120, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height, m.focus, m.sidebarOpen = width, 20, "nav.overview", width < 100
			m.layout()
			hit := findTarget(t, m, "nav.overview")
			for range len(navPages()) {
				m, _ = update(m, tea.MouseWheelMsg{X: 2, Y: hit.Y + 1, Button: tea.MouseWheelDown})
			}
			if hasAction(m.targets, "nav.overview") || m.scroll != 0 {
				t.Fatal("scrolling to the last menu retained a stale target or scrolled the body")
			}
			hit = findTarget(t, m, "nav.settings")
			m, _ = update(m, tea.MouseClickMsg{X: hit.X + 2, Y: hit.Y + hit.H/2, Button: tea.MouseLeft})
			if m.page != "settings" || m.sidebarOpen {
				t.Fatal("click did not open the menu rendered at the scrolled position")
			}
		})
	}
}

func TestSidebarWheelDoesNotPauseOrScrollLogs(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 20
	m.navigate("logs")
	m.layout()
	offset := m.sidebar.viewport.YOffset()
	hit := visibleSidebarTargets(m.targets)[0]
	m, _ = update(m, tea.MouseWheelMsg{X: 2, Y: hit.Y + 1, Button: tea.MouseWheelUp})
	if m.sidebar.viewport.YOffset() >= offset || m.logs.paused || m.logs.offset != 0 {
		t.Fatal("sidebar wheel paused or scrolled the log stream")
	}
}

func TestSidebarTabReturnsToLogicalNavigationAfterMouseScrolling(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height, m.focus = 120, 20, "nav.overview"
	m.layout()
	hit := findTarget(t, m, "nav.overview")
	m, _ = update(m, tea.MouseWheelMsg{X: 2, Y: hit.Y + 1, Button: tea.MouseWheelDown})
	if hasAction(m.targets, "nav.overview") {
		t.Fatal("test did not scroll the focused item out of view")
	}
	m, _ = update(m, special(tea.KeyTab))
	if strings.HasPrefix(m.focus, "nav.") {
		t.Fatal("Tab walked visible menu entries instead of entering the content")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != "nav.overview" || m.sidebar.viewport.YOffset() != 0 {
		t.Fatal("reverse Tab forgot the logical navigation target")
	}
	assertSidebarFocusVisible(t, m, "nav.overview", 3)
	m, _ = update(m, key("7"))
	if m.page != "settings" || strings.HasPrefix(m.focus, "nav.") {
		t.Fatal("page shortcut did not enter the last page content")
	}
	m.returnToNavigation()
	m.layout()
	assertSidebarFocusVisible(t, m, "nav.settings", 3)
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 25})
	assertSidebarFocusVisible(t, m, "nav.settings", 3)
}

func TestSidebarPopupWheelAndOutsideClickRemainModal(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 80, 12
	m.sidebarOpen, m.focus = true, "nav.overview"
	m.layout()
	hit := findTarget(t, m, "nav.overview")
	page, focus, bodyScroll := m.page, m.focus, m.scroll
	m, _ = update(m, tea.MouseWheelMsg{X: hit.X + 1, Y: hit.Y + 1, Button: tea.MouseWheelDown})
	offset := m.sidebar.viewport.YOffset()
	if offset == 0 || !m.sidebarOpen || m.page != page || m.focus != focus || m.scroll != bodyScroll {
		t.Fatal("popup wheel changed state outside its viewport")
	}
	m.layout()
	if m.sidebar.viewport.YOffset() != offset {
		t.Fatal("popup render reset its mouse scroll")
	}
	m, _ = update(m, tea.MouseWheelMsg{X: sidebarWidth + 3, Y: hit.Y + 1, Button: tea.MouseWheelDown})
	if m.sidebar.viewport.YOffset() != offset || m.scroll != bodyScroll || !m.sidebarOpen {
		t.Fatal("wheel outside the popup escaped its modal boundary")
	}
	m, _ = update(m, tea.MouseClickMsg{X: sidebarWidth, Y: hit.Y + 1, Button: tea.MouseLeft})
	if m.sidebarOpen || m.focus != "menu" || m.page != page || m.scroll != bodyScroll {
		t.Fatal("outside click did not only dismiss the popup")
	}
	m.activate("menu")
	m.layout()
	assertSidebarFocusVisible(t, m, "nav.overview", 3)
}

func TestSidebarTinyPopupFallsBackWithoutEscapingCanvas(t *testing.T) {
	for _, size := range [][2]int{{20, 7}, {10, 4}, {3, 2}, {1, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = size[0], size[1]
			m.sidebarOpen, m.focus = true, "nav.settings"
			m.layout()
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			if size[0] >= 20 {
				assertSidebarFocusVisible(t, m, "nav.settings", 1)
				m, _ = update(m, special(tea.KeyHome))
				assertSidebarFocusVisible(t, m, "nav.overview", 1)
			}
		})
	}
}
