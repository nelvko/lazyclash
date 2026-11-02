package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func testMenuItems() []menuItem {
	return []menuItem{
		{ID: "apply", Label: "Use profile"},
		{ID: "update", Label: "Update", DisabledReason: "No saved address"},
		{ID: "rename", Label: "Rename"},
		{ID: "delete", Label: "Delete", Danger: true},
	}
}

func menuMouse(m actionMenu, row int, button tea.MouseButton) tea.MouseClickMsg {
	padding := 0
	if m.framed {
		padding = 1
	}
	return tea.MouseClickMsg{X: m.bounds.X + padding, Y: m.bounds.Y + padding + row, Button: button}
}

func TestActionMenuKeyboardSkipsDisabledItemsAndReturnsAction(t *testing.T) {
	var m actionMenu
	items := testMenuItems()
	m.show(items, 10, 10)
	items[0].ID = "changed outside menu"
	if m.selected != 0 {
		t.Fatal("menu did not select its first available action")
	}
	m.key(special(tea.KeyDown))
	if m.items[m.selected].ID != "rename" {
		t.Fatal("Down did not skip disabled Update")
	}
	m.key(special(tea.KeyUp))
	if m.items[m.selected].ID != "apply" {
		t.Fatal("Up did not skip disabled Update")
	}
	m.key(special(tea.KeyUp))
	if m.items[m.selected].ID != "delete" {
		t.Fatal("Up did not wrap to the last available action")
	}
	m.key(special(tea.KeyHome))
	if result := m.key(special(tea.KeyEnter)); result.Action != "apply" || !result.Closed || m.open {
		t.Fatalf("selection did not return a stable action and close: %+v", result)
	}
	if result := m.key(special(tea.KeyEnter)); result != (menuResult{}) {
		t.Fatal("closed menu accepted another action")
	}
}

func TestActionMenuOwnsKeyboardAndDismissesWithoutActivation(t *testing.T) {
	for _, closeKey := range []tea.KeyPressMsg{
		special(tea.KeyEscape), special(tea.KeyTab), {Code: tea.KeyTab, Mod: tea.ModShift},
	} {
		var m actionMenu
		m.show(testMenuItems(), 0, 0)
		for _, globalKey := range []string{"q", "1", "a", "s"} {
			if result := m.key(key(globalKey)); result != (menuResult{}) || !m.open {
				t.Fatalf("global key %q escaped or activated the menu: %+v", globalKey, result)
			}
		}
		if result := m.key(closeKey); result.Action != "" || !result.Closed || m.open {
			t.Fatalf("%s did not close without action: %+v", closeKey, result)
		}
	}
}

func TestActionMenuDisabledItemsCannotBeSelected(t *testing.T) {
	var m actionMenu
	m.show([]menuItem{{ID: "delete", Label: "Delete", DisabledReason: "Current profile"}}, 0, 0)
	view := m.render("", 80, 10)
	if !strings.Contains(ansi.Strip(view), "Delete · Current profile") || m.selected != -1 {
		t.Fatal("disabled action lost its explanation or became selected")
	}
	for _, msg := range []tea.KeyPressMsg{special(tea.KeyDown), special(tea.KeyUp), special(tea.KeyHome), special(tea.KeyEnd), special(tea.KeyEnter)} {
		if result := m.key(msg); result.Action != "" || !m.open || m.selected != -1 {
			t.Fatal("keyboard activated a disabled item", result)
		}
	}
	if result := m.mouse(menuMouse(m, 0, tea.MouseLeft)); result.Action != "" || !m.open {
		t.Fatal("mouse activated a disabled item", result)
	}
}

func TestActionMenuMouseHoverReleaseAndClick(t *testing.T) {
	var m actionMenu
	m.show(testMenuItems(), 5, 4)
	m.render("", 80, 20)
	disabled := menuMouse(m, 1, tea.MouseLeft)
	if result := m.mouse(disabled); result.Action != "" || !m.open || m.selected != 0 {
		t.Fatal("disabled mouse target changed selection or ran an action")
	}
	delete := menuMouse(m, 3, tea.MouseLeft)
	if result := m.mouse(tea.MouseMotionMsg(delete)); result.Action != "" || !m.open || m.items[m.selected].ID != "delete" {
		t.Fatal("hover did not select without activating")
	}
	if result := m.mouse(tea.MouseReleaseMsg(delete)); result.Action != "" || !m.open {
		t.Fatal("release activated the hovered item")
	}
	if result := m.mouse(delete); result.Action != "delete" || !result.Closed || m.open {
		t.Fatalf("left click did not return selected action: %+v", result)
	}
}

func TestActionMenuOutsideClickOnlyDismisses(t *testing.T) {
	for _, button := range []tea.MouseButton{tea.MouseLeft, tea.MouseRight} {
		var m actionMenu
		m.show(testMenuItems(), 5, 4)
		m.render("", 80, 20)
		if result := m.mouse(tea.MouseClickMsg{X: 79, Y: 19, Button: button}); result.Action != "" || !result.Closed || m.open {
			t.Fatalf("outside %s click did not dismiss without action: %+v", button, result)
		}
	}
}

func TestActionMenuWheelOnlyMovesInsideMenu(t *testing.T) {
	var m actionMenu
	m.show(testMenuItems(), 5, 4)
	m.render("", 80, 20)
	m.mouse(tea.MouseWheelMsg{X: 79, Y: 19, Button: tea.MouseWheelDown})
	if m.selected != 0 {
		t.Fatal("wheel outside the popup scrolled menu selection")
	}
	m.mouse(tea.MouseWheelMsg{X: m.bounds.X + 1, Y: m.bounds.Y + 1, Button: tea.MouseWheelDown})
	if m.items[m.selected].ID != "rename" {
		t.Fatal("wheel inside the popup failed to skip a disabled item")
	}
}

func TestActionMenuClampsToWindowAndKeepsSelectionVisible(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {80, 24}, {24, 7}, {8, 4}, {3, 2}, {1, 1}} {
		for _, anchor := range [][2]int{{0, 0}, {size[0] - 1, size[1] - 1}, {-100, -100}, {1000, 1000}} {
			var m actionMenu
			m.show(testMenuItems(), anchor[0], anchor[1])
			view := m.render("", size[0], size[1])
			assertCanvas(t, view, size[0], size[1])
			if m.bounds.X < 0 || m.bounds.Y < 0 || m.bounds.X+m.bounds.W > size[0] || m.bounds.Y+m.bounds.H > size[1] {
				t.Fatalf("menu escaped %v at %v: %+v", size, anchor, m.bounds)
			}
			m.key(special(tea.KeyEnd))
			view = m.render("", size[0], size[1])
			assertCanvas(t, view, size[0], size[1])
			if size[0] >= 8 && !strings.Contains(ansi.Strip(view), "Del") {
				t.Fatalf("selected last item is not visible in %v", size)
			}
			m.key(special(tea.KeyHome))
			view = m.render("", size[0], size[1])
			if size[0] >= 24 && !strings.Contains(ansi.Strip(view), "Use profile") {
				t.Fatalf("selected first item is not visible in %v", size)
			}
		}
	}
}

func TestActionMenuEmptyAndZeroSizedWindows(t *testing.T) {
	var m actionMenu
	m.show(nil, 0, 0)
	if m.open || m.render("unchanged", 20, 10) != "unchanged" {
		t.Fatal("empty menu obscured the page")
	}
	m.show(testMenuItems(), 0, 0)
	for _, size := range [][2]int{{0, 10}, {10, 0}, {-1, -1}} {
		if actual := m.render("", size[0], size[1]); actual != "" {
			t.Fatal("zero-size canvas produced content")
		}
	}
}

func TestActionMenuItemUpdatesPreserveSelectionAndSkipNewlyDisabledActions(t *testing.T) {
	var m actionMenu
	m.show(testMenuItems(), 0, 0)
	m.key(special(tea.KeyDown))
	m.setItems([]menuItem{{ID: "rename", Label: "Rename"}, {ID: "apply", Label: "Use profile"}})
	if m.selected != 0 || m.items[m.selected].ID != "rename" {
		t.Fatal("item reorder changed the selected action's identity")
	}
	m.setItems([]menuItem{{ID: "rename", Label: "Rename", DisabledReason: "Unavailable"}, {ID: "apply", Label: "Use profile"}})
	if m.selected != 1 {
		t.Fatal("newly disabled selection did not move to an available action")
	}
	m.setItems([]menuItem{{ID: "apply", Label: "Use profile", DisabledReason: "Unavailable"}})
	if m.selected != -1 || m.key(special(tea.KeyEnter)).Action != "" {
		t.Fatal("a fully disabled updated menu retained an actionable selection")
	}
	m.setItems(nil)
	if m.open {
		t.Fatal("empty updated menu did not close")
	}
}
