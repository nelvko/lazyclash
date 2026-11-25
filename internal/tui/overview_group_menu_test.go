package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestOverviewRuleGroupMenuOnlyChangesTheDisplayedGroup(t *testing.T) {
	for _, entry := range []string{"keyboard", "mouse"} {
		t.Run(entry, func(t *testing.T) {
			m, backend := proxiesModel(t, 160, 45)
			defer m.stopRuntime()
			m.navigate("overview")
			m.focus = overviewRouteGroupTarget
			m.revealContentFocus()
			m.layout()
			if entry == "mouse" {
				m, _ = click(m, findTarget(t, m, overviewRouteGroupTarget))
			} else {
				m, _ = update(m, special(tea.KeyEnter))
			}
			if !m.menu.dropdownOpen(overviewRouteGroupTarget) || m.menu.items[m.menu.selected].ID != "PROXY" {
				t.Fatal("the group menu did not open on the displayed group")
			}
			for _, item := range m.menu.items {
				if item.ID == "GLOBAL" {
					t.Fatal("the rule group menu included GLOBAL")
				}
			}
			m, _ = update(m, special(tea.KeyDown))
			if m.menu.items[m.menu.selected].ID != "AUTO" || m.overviewGroup != "" {
				t.Fatal("browsing the menu committed a group prematurely")
			}
			if entry == "mouse" {
				m, _ = update(m, menuMouse(m.menu, m.menu.selected-m.menu.offset, tea.MouseLeft))
			} else {
				m, _ = update(m, special(tea.KeyEnter))
			}
			if m.menu.open || m.focus != overviewRouteGroupTarget || m.overviewGroup != "AUTO" ||
				m.navigationOptions().SelectedGroup != "AUTO" || !strings.Contains(m.screen, "HK 02") {
				t.Fatal("choosing a group did not update the overview and restore focus")
			}
			if m.busy || m.proxies.activeGroup != "PROXY" || backend.selections != 0 || backend.modes != 0 {
				t.Fatal("inspecting a group changed proxy browsing or wrote to the core")
			}
			m, _ = update(m, special(tea.KeyEnter))
			m, _ = update(m, special(tea.KeyDown))
			m, _ = update(m, special(tea.KeyEscape))
			if m.menu.open || m.focus != overviewRouteGroupTarget || m.overviewGroup != "AUTO" {
				t.Fatal("cancelling the menu changed the displayed group or lost focus")
			}
		})
	}
}

func TestOverviewRuleGroupMenuReconcilesSnapshots(t *testing.T) {
	m, _ := proxiesModel(t, 160, 45)
	defer m.stopRuntime()
	m.navigate("overview")
	m.focus, m.overviewGroup = overviewRouteGroupTarget, "AUTO"
	m.revealContentFocus()
	m.layout()
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyDown))
	if !m.menu.open || m.menu.items[m.menu.selected].ID != "ChatGPT" {
		t.Fatal("missing pending group")
	}
	refresh := func() {
		m, _ = update(m, coreStatusMsg{generation: m.statusGeneration, snapshot: m.snapshot})
	}
	// Reorder existing groups with another snapshot while the menu is open.
	m.snapshot.Groups = slices.Clone(m.snapshot.Groups)
	slices.Reverse(m.snapshot.Groups)
	refresh()
	if !m.menu.open || m.menu.items[m.menu.selected].ID != "ChatGPT" || m.overviewGroup != "AUTO" {
		t.Fatal("refresh moved or committed the pending choice")
	}
	m.snapshot.Mode.Actual = "global"
	refresh()
	if m.menu.open || m.focus != "overview.card.dashboard.control.routingMode" || hasAction(m.contentTargets(), overviewRouteGroupTarget) {
		t.Fatal("a mode change retained the rule group's control")
	}
	m.snapshot.Mode.Actual = "rule"
	refresh()
	if m.navigationOptions().SelectedGroup != "AUTO" || !hasAction(m.contentTargets(), overviewRouteGroupTarget) {
		t.Fatal("returning to rule mode lost the chosen group")
	}
	m.focus = overviewRouteGroupTarget
	m.revealContentFocus()
	m.layout()
	m, _ = update(m, special(tea.KeyEnter))
	m.snapshot.Applied = "other-profile"
	refresh()
	if m.menu.open || m.overviewGroup != "" || m.navigationOptions().overviewRuleGroupDropdown().Value != "PROXY" {
		t.Fatal("switching profile retained the previous profile's group or open menu")
	}
	m.overviewGroup = "AUTO"
	m.snapshot.Groups = []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "DIRECT"}}
	refresh()
	if m.overviewGroup != "" || hasAction(m.contentTargets(), overviewRouteGroupTarget) {
		t.Fatal("a removed group or a single-option selector survived the refresh")
	}
}

func TestOverviewRuleGroupMenuRevealsTheEndOfTheRoute(t *testing.T) {
	m, _ := proxiesModel(t, 40, 16)
	defer m.stopRuntime()
	m.snapshot.Groups = []mihomo.Group{
		{Name: "PROXY", Type: "Selector", Now: "DIRECT"},
		{Name: "Video streaming", Type: "Selector", Now: "Automatic selection"},
		{Name: "Automatic selection", Type: "URLTest", Now: "Hong Kong main endpoint"},
	}
	m.navigate("overview")
	m.focus = overviewRouteGroupTarget
	m.scroll = 0
	m.revealContentFocus()
	m.layout()
	start := m.scroll
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyEnter))
	if m.menu.open || m.focus != overviewRouteGroupTarget || m.scroll <= start || !strings.Contains(m.screen, "Hong Kong main endpoint") {
		t.Fatalf("the selected route did not scroll into view: %s", m.screen)
	}
	findTarget(t, m, overviewRouteGroupTarget)
	start = m.scroll
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyUp))
	m, _ = update(m, special(tea.KeyEnter))
	if m.overviewGroup != "PROXY" || m.scroll != start {
		t.Fatal("selecting an already visible shorter route moved the viewport")
	}
}

func TestOverviewRuleGroupMenuReopensAboveAVeryLongRoute(t *testing.T) {
	m, _ := proxiesModel(t, 40, 16)
	defer m.stopRuntime()
	m.snapshot.Groups = []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "DIRECT"}}
	name := func(i int) string { return fmt.Sprintf("Long route group %02d", i) }
	for i := range 24 {
		m.snapshot.Groups = append(m.snapshot.Groups, mihomo.Group{Name: name(i), Type: "Selector", Now: name(i + 1)})
	}
	m.snapshot.Groups[len(m.snapshot.Groups)-1].Now = "Last endpoint"
	m.navigate("overview")
	m.focus = overviewRouteGroupTarget
	m.revealContentFocus()
	m.layout()
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyEnter))
	if !strings.Contains(m.screen, "Last endpoint") || hasAction(m.targets, overviewRouteGroupTarget) {
		t.Fatal("fixture did not expose the end of a longer-than-screen route with its trigger scrolled out")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if !m.menu.dropdownOpen(overviewRouteGroupTarget) || m.menu.items[m.menu.selected].ID != name(0) {
		t.Fatal("Enter failed to reveal and reopen the offscreen group selector")
	}
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyEscape))
	if m.menu.open || m.overviewGroup != name(0) || !strings.Contains(m.screen, "Last endpoint") {
		t.Fatal("Escape did not restore the complete previously chosen route")
	}
}

func TestOverviewRuleGroupPopupCentersOnItsExpandedTrigger(t *testing.T) {
	for _, width := range []int{40, 120, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, backend := proxiesModel(t, width, 24)
			defer m.stopRuntime()
			m.navigate("overview")
			m.focus = overviewRouteGroupTarget
			m.revealContentFocus()
			m.layout()
			for _, arrow := range []rune{tea.KeyDown, tea.KeyUp} {
				m, _ = update(m, special(arrow))
				for _, size := range [][2]int{{width, 24}, {40, 16}, {width, 24}} {
					m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					if !m.menu.dropdownOpen(overviewRouteGroupTarget) || strings.Contains(m.screen, "HK 02") {
						t.Fatal("expanded picker should keep showing group names and hide the selected route")
					}
					anchor, popup := findTarget(t, m, overviewRouteGroupTarget), m.menu.bounds
					if diff := 2*(popup.X-anchor.X) + popup.W - anchor.W; diff < -1 || diff > 1 ||
						popup.X < 0 || popup.X+popup.W > m.width || popup.Y < 0 || popup.Y+popup.H > dashboardContentBottom(m.dashboardOptions()) {
						t.Fatalf("misplaced popup %+v for trigger %+v after resize to %v", popup, anchor, size)
					}
					rowY := popup.Y + 1 + m.menu.selected - m.menu.offset
					row := ansi.Cut(strings.Split(m.screen, "\n")[rowY], popup.X, popup.X+popup.W)
					if visible := ansi.Strip(row); !strings.Contains(visible, "› PROXY") || !strings.Contains(visible, "✓") {
						t.Fatalf("selected group is missing its cursor or check: %q", visible)
					}
					if !strings.Contains(row, themed("›", focus, selectionBackground, false)) {
						t.Fatalf("selected group is missing its focus-colored cursor: %q", row)
					}
				}
				m, _ = update(m, special(tea.KeyEscape))
				if m.menu.open || m.focus != overviewRouteGroupTarget || !strings.Contains(m.screen, "HK 02") || backend.selections != 0 {
					t.Fatal("Escape should restore the route without changing its selection")
				}
			}
		})
	}
}
