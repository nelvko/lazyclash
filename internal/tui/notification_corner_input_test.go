package tui

import (
	"fmt"
	"strings"
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
	m.notification.items[0].Details = "The source returned HTTP 503."
	m.layout()
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
		publishNotice(&m, fmt.Sprintf("item-%d", i), "error", fmt.Sprintf("Refresh failed %d", i))
		m.notification.items[i].Details = fmt.Sprintf("Diagnostic %d", i)
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

func TestShortSuccessHasNoHiddenDetailsAction(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {40, 24}} {
		m := profileNavigationModel(size[0], size[1])
		id := publishNotice(&m, "profile.apply", "done", "Saved")
		closeID := fmt.Sprintf("notification.close.%d", id)
		detailsID := fmt.Sprintf("notification.details.%d", id)
		if !hasAction(m.targets, closeID) || hasAction(m.targets, detailsID) || hasAction(m.focusTargets(), detailsID) {
			t.Fatalf("%v: short notification has incorrect targets: %+v", size, m.targets)
		}
		// A stale focused control can no longer open a detail view after a
		// message gets shorter or the terminal expands.
		m.focus = detailsID
		m, _ = update(m, special(tea.KeyEnter))
		if m.notification.panel.open || m.focus == detailsID || len(m.notification.items) != 1 {
			t.Fatalf("%v: the hidden Details action was still reachable", size)
		}
		m, _ = update(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
		p := &m.notification.panel
		if len(p.actions) != 1 || p.actions[0].ID != fmt.Sprintf("notification.panel.dismiss.%d", id) {
			t.Fatalf("%v: short notification in the list exposed extra controls: %+v", size, p.actions)
		}
		m, _ = update(m, special(tea.KeyEnter))
		if m.notification.panel.open || len(m.notification.items) != 0 {
			t.Fatalf("%v: Enter did not dismiss the short notice", size)
		}
	}
}

func TestEscapeOnTruncatedCardDismissesOnlyItsNotice(t *testing.T) {
	m := profileNavigationModel(40, 24)
	older := publishNotice(&m, "first", "done", "Saved")
	// A quiet entry still exists in Ctrl+N, but never competes for a toast.
	m.notification.items[0].Silent = true
	clipped := publishNotice(&m, "profile.apply", "done", "Selected subscription “"+strings.Repeat("office ", 15)+"”")
	details := fmt.Sprintf("notification.details.%d", clipped)
	findTarget(t, m, details)
	if !noticeByID(t, m, clipped).started.IsZero() {
		t.Fatal("truncated toast consumed its display time")
	}
	m.focus = details
	m, _ = update(m, special(tea.KeyEscape))
	if len(m.notification.items) != 1 || m.notification.items[0].ID != older || m.notification.panel.open {
		t.Fatalf("Esc dismissed a different notification: %+v", m.notification.items)
	}
}
