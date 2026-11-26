package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSettingsColumnNavigation(t *testing.T) {
	model := func(t *testing.T, width, height int, focus string) Model {
		t.Helper()
		m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
		directionCleanup(t, &m)
		m.page, m.focus, m.width, m.height = "settings", focus, width, height
		m.revealContentFocus()
		m.layout()
		return m
	}
	move := func(t *testing.T, m Model, key tea.KeyPressMsg, want string) Model {
		t.Helper()
		next, _ := update(m, key)
		if next.focus != want || next.page != "settings" || next.form != "" || next.menu.open || next.busy {
			t.Fatalf("%s from %q: focus=%q page=%q form=%q menu=%t busy=%t, want %q", key, m.focus, next.focus, next.page, next.form, next.menu.open, next.busy, want)
		}
		return next
	}

	t.Run("wide left column", func(t *testing.T) {
		m := model(t, 160, 45, "settings.theme")
		m, _ = click(m, findTarget(t, m, "editor.configure"))
		m, _ = update(m, special(tea.KeyEscape))
		m = move(t, m, special(tea.KeyUp), "settings.theme")
		m = move(t, m, key("j"), "editor.configure")
		if row := settingsPaintedTarget(t, m.screen, m.targets, m.focus); !strings.HasPrefix(row, "›") {
			t.Fatalf("keyboard navigation did not restore the editor row's focus arrow: %q", row)
		}
		m = move(t, m, special(tea.KeyDown), "card.logs")
		m = move(t, m, key("j"), "card.logs")
		move(t, m, key("k"), "editor.configure")
	})
	t.Run("wide top boundaries and global Tab", func(t *testing.T) {
		m := model(t, 160, 45, "settings.language")
		m = move(t, m, key("k"), "settings.language")
		tab := coreSettingsTabs[accessTab]
		m = move(t, m, special(tea.KeyTab), tab)
		m = move(t, m, special(tea.KeyUp), tab)
		move(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "settings.language")
	})
	t.Run("wide right column skips disabled rows", func(t *testing.T) {
		m := model(t, 160, 45, coreSettingsTabs[accessTab])
		m.settingsTab, m.focus = networkTab, "settings.tcpKeepAlive"
		m.snapshot.CoreSettings.DisableKeepAlive = "on"
		m.layout()
		m = move(t, m, key("j"), "settings.unifiedDelay")
		move(t, m, key("k"), "settings.tcpKeepAlive")
	})
	t.Run("wide right column crosses cards", func(t *testing.T) {
		m := model(t, 160, 45, "settings.externalControl")
		m = move(t, m, special(tea.KeyDown), "install")
		move(t, m, key("k"), "settings.externalControl")
	})
	t.Run("narrow crosses cards and reveals focus", func(t *testing.T) {
		m := model(t, 40, 10, "editor.configure")
		before := m.scroll
		m = move(t, m, special(tea.KeyDown), coreSettingsTabs[accessTab])
		m = move(t, m, key("j"), "runtime-settings")
		findTarget(t, m, "runtime-settings")
		if m.scroll <= before {
			t.Fatalf("focus moved into the next card without scrolling in the short viewport: %d → %d", before, m.scroll)
		}
		m = move(t, m, key("k"), coreSettingsTabs[accessTab])
		move(t, m, special(tea.KeyUp), "editor.configure")
	})
	t.Run("open controls own their keys", func(t *testing.T) {
		m := model(t, 160, 45, "settings.language")
		m, _ = update(m, special(tea.KeyEnter))
		before := m.menu.selected
		m, _ = update(m, special(tea.KeyUp))
		if !m.menu.open || m.menu.selected == before || m.focus != "settings.language" {
			t.Fatal("Up should move the dropdown cursor and keep focus on the trigger")
		}
		m, _ = update(m, special(tea.KeyEscape))
		m, _ = click(m, findTarget(t, m, "editor.configure"))
		for _, letter := range "hjkl" {
			m, _ = update(m, key(string(letter)))
		}
		if m.form != "editor-preference" || m.input.Value() != "hjkl" || m.focus != "editor.configure" {
			t.Fatal("navigation letters should enter text when editing a command")
		}
	})
}
