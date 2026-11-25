package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNotificationShortcutPreservesPageOnEmptyAndTogglesWithItems(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {40, 24}} {
		m := profileNavigationModel(size[0], size[1])
		m.layout()
		page, focus, scroll, screen := m.page, m.focus, m.scroll, m.screen
		short := tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
		m, _ = update(m, short)
		if m.notification.panel.open || m.page != page || m.focus != focus || m.scroll != scroll || m.screen != screen {
			t.Fatalf("%v: empty notification shortcut changed the page", size)
		}
		publishNotice(&m, "profile.refresh", "error", "Unavailable")
		m, _ = update(m, short)
		if !m.notification.panel.open || m.notification.panel.detailID != 0 {
			t.Fatalf("%v: Ctrl+N did not open the collection", size)
		}
		m, _ = update(m, short)
		if m.notification.panel.open || m.page != page || m.focus != focus || len(m.notification.items) != 1 {
			t.Fatalf("%v: toggling the collection lost the page focus or the notice", size)
		}
	}
}

func TestNotificationDirectDetailsCollapseAndOutsideClickIsConsumed(t *testing.T) {
	m := noticeModel()
	id := publishNotice(&m, "profile.refresh", "error", "Unavailable")
	focus := m.focus
	details := fmt.Sprintf("notification.details.%d", id)
	outside := findTarget(t, m, "nav.settings")
	for _, outsideClick := range []bool{false, true} {
		m, _ = click(m, findTarget(t, m, details))
		if m.notification.panel.detailID != id {
			t.Fatal("clicking the card did not open its details")
		}
		if outsideClick {
			m, _ = click(m, outside)
		} else {
			m, _ = update(m, special(tea.KeyEscape))
		}
		if m.notification.panel.open || m.focus != focus || m.page != "overview" || len(m.notification.items) != 1 {
			t.Fatalf("close from card details: outside=%v focus=%q page=%q", outsideClick, m.focus, m.page)
		}
	}
}

func TestNotificationShortcutReturnsToOpenCompactNavigation(t *testing.T) {
	m := profileNavigationModel(40, 24)
	m.activate("menu")
	m.layout()
	focus := m.focus
	short := tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	m, _ = update(m, short)
	if m.notification.panel.open || !m.sidebarOpen || m.focus != focus {
		t.Fatal("empty shortcut moved compact navigation focus")
	}
	publishNotice(&m, "profile.refresh", "error", "Unavailable")
	m, _ = update(m, short)
	if !m.notification.panel.open {
		t.Fatal("nonempty collection was unreachable from compact navigation")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.notification.panel.open || !m.sidebarOpen || m.focus != focus {
		t.Fatal("collapsing the collection did not return to compact navigation")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.sidebarOpen || m.focus != "menu" {
		t.Fatal("navigation lost ownership of the next Escape")
	}
}

func TestEmptyNotificationShortcutKeepsProfileInputDraftAndCursor(t *testing.T) {
	m := profileNavigationModel(40, 24)
	m.activate("profiles.url")
	m.profilesUI.url.SetValue("draft subscription")
	m.profilesUI.url.SetCursor(3)
	m.layout()
	m, _ = update(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.notification.panel.open || m.page != "profiles" || m.focus != "profiles.url" ||
		!m.profilesUI.url.Focused() || m.profilesUI.url.Value() != "draft subscription" || m.profilesUI.url.Position() != 3 {
		t.Fatal("empty shortcut changed the active input")
	}
}

func TestNotificationListReturnsToScrolledItemAndClosesWhenEmpty(t *testing.T) {
	m := noticeModel()
	m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	for i := range 7 {
		publishNotice(&m, fmt.Sprintf("item-%d", i), "error", fmt.Sprintf("Diagnostic %d", i))
	}
	focus := m.focus
	m, _ = update(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	m, _ = update(m, special(tea.KeyEnd))
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	list := &m.notification.panel
	chosen, visible := list.focusedAction()
	offset := list.viewport.YOffset()
	last := m.notification.items[len(m.notification.items)-1].ID
	if !visible || offset == 0 || chosen.ID != fmt.Sprintf("notification.panel.details.%d", last) {
		t.Fatalf("last item not focused: %+v at offset %d", chosen, offset)
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.notification.panel.detailID != last {
		t.Fatal("Enter did not open the focused item's details")
	}
	m, _ = update(m, special(tea.KeyEscape))
	list = &m.notification.panel
	returned, visible := list.focusedAction()
	if !list.open || list.detailID != 0 || !visible || returned.ID != chosen.ID || list.viewport.YOffset() != offset {
		t.Fatalf("list did not restore selection and scroll: %+v, %d", returned, list.viewport.YOffset())
	}
	for remaining := len(m.notification.items); remaining > 0; remaining-- {
		m, _ = update(m, special(tea.KeyEnd))
		m, _ = update(m, key("x"))
		if len(m.notification.items) != remaining-1 || m.notification.panel.open != (remaining > 1) {
			t.Fatalf("dismissing item %d left %d records, open=%t", remaining, len(m.notification.items), m.notification.panel.open)
		}
	}
	if m.focus != focus {
		t.Fatalf("dismissing last item: focus=%q, want %q", m.focus, focus)
	}
}
