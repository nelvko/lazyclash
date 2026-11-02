package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestNavigationShellBusyNoticeCancelsRealOperationWithoutQuitting(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 32
	ctx := m.newJob("action")
	m.message = "Updating subscription"
	m.syncNotification()
	m.layout()
	if len(m.notification.items) != 1 || m.notification.items[0].Kind != "working" {
		t.Fatal("real operation did not produce a working notification")
	}
	id := m.notification.items[0].ID
	m.focus = "content"
	m, _ = update(m, special(tea.KeyEscape))
	if ctx.Err() != nil || m.focus != "nav.overview" || !m.busy {
		t.Fatal("Esc must return to navigation without cancelling the operation")
	}
	m, _ = click(m, findTarget(t, m, fmt.Sprintf("notification.cancel.%d", id)))
	if ctx.Err() == nil || m.quitting || !m.busy || m.job != "action" {
		t.Fatal("notice cancellation did not wait for operation cleanup independently of quitting")
	}
}

func TestNavigationShellConnectionConfirmationOwnsPageShortcuts(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, key("x"))
	for _, digit := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		m, _ = update(m, key(digit))
		if m.page != "connections" || !m.connections.confirming || m.focus != "conn.cancel" {
			t.Fatalf("%s escaped the connection confirmation", digit)
		}
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.keyHints.hidden || m.keyHints.changed || m.keyHints.pending || !m.connections.confirming {
		t.Fatal("connection confirmation leaked Ctrl+T to the global display shortcut")
	}
	// Once help owns the interface, its own display control remains available.
	m.keyHintsAction("help.open")
	m, _ = update(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if !m.helpOpen || !m.keyHints.hidden || !m.connections.confirming {
		t.Fatal("help could not own its shortcut above a connection confirmation")
	}
}

func TestNavigationShellTextInputsOwnCtrlT(t *testing.T) {
	for _, page := range []string{"profiles", "connections", "rules", "logs"} {
		t.Run(page, func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.page, m.focus = page, "content"
			switch page {
			case "profiles":
				m.focus, m.profilesUI.editing = "profiles.url", true
				m.profilesUI.url.Focus()
			case "connections":
				m.connections.editing = true
				m.connections.search.Focus()
			case "rules":
				m.rules.editing = true
				m.rules.search.Focus()
			case "logs":
				m.logs.editing = true
				m.logs.search.Focus()
			}
			m, _ = update(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
			if m.keyHints.hidden || m.keyHints.changed || m.keyHints.pending || m.page != page {
				t.Fatal("text input leaked Ctrl+T to the global display shortcut")
			}
		})
	}
}

func TestNavigationShellBusyHelpShortcutAndQuitRespectCleanup(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 32
	ctx := m.newJob("action")
	m.message = "Updating subscription"
	m.syncNotification()
	m.focus = "content"
	m.layout()
	m, _ = update(m, key("?"))
	if !m.helpOpen || !m.busy || ctx.Err() != nil {
		t.Fatal("keyboard could not open help during an operation")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.helpOpen || m.focus != "content" || !m.busy || ctx.Err() != nil {
		t.Fatal("closing help disturbed the underlying focus or operation")
	}
	m, _ = update(m, key("?"))
	m, cmd := update(m, key("q"))
	if cmd != nil || !m.quitting || !m.busy || ctx.Err() == nil {
		t.Fatal("quitting from busy help did not wait for safe operation cleanup")
	}
	m, cmd = update(m, resultMsg{id: m.id})
	if m.busy {
		t.Fatal("operation completion did not release busy state")
	}
	expectQuit(t, cmd)
}

func TestNavigationShellDrawerTabRestoresContentWithoutActivation(t *testing.T) {
	for _, modifiers := range []tea.KeyMod{0, tea.ModShift} {
		t.Run(fmt.Sprint(modifiers), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = 80, 24
			m.pageViews = map[string]pageViewState{"settings": {focus: "key-hints.toggle"}}
			m.browsePage("settings")
			m.sidebarOpen = true
			m.layout()
			m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: modifiers})
			if m.sidebarOpen || m.focus != "key-hints.toggle" || m.keyHints.changed || m.keyHints.hidden {
				t.Fatal("drawer Tab did not restore content without activating it")
			}
			m, _ = update(m, special(tea.KeyEscape))
			if m.sidebarOpen || m.focus != "menu" || m.page != "settings" {
				t.Fatal("Esc did not return to the compact navigation entry")
			}
		})
	}
}

func TestNavigationShellHintResizeKeepsFocusedListRowVisible(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 32
	m.page, m.keyHints.hidden = "proxies", true
	for i := 0; i < 40; i++ {
		m.snapshot.Groups = append(m.snapshot.Groups, mihomo.Group{Name: fmt.Sprintf("Group %02d", i), Type: "Selector", Now: "DIRECT"})
	}
	m.focus = "group.30"
	for _, target := range m.contentTargets() {
		if target.ID == m.focus {
			m.revealBodyTarget(target)
			break
		}
	}
	m.layout()
	if !hasAction(m.targets, m.focus) {
		t.Fatal("fixture did not position the focused row in the visible viewport")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if m.keyHints.hidden || !hasAction(m.targets, m.focus) {
		t.Fatal("restoring the hint bar hid the focused row")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	if !hasAction(m.targets, m.focus) || strings.HasPrefix(m.focus, "nav.") || m.sidebarOpen {
		t.Fatal("resize lost the visible content focus after the footer changed")
	}
	assertCanvas(t, m.screen, m.width, m.height)
	assertRegions(t, m.targets, m.width, m.height)
}
