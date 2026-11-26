package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSettingsHorizontalNavigation(t *testing.T) {
	model := func(t *testing.T, width, height int, focus string) Model {
		t.Helper()
		m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
		directionCleanup(t, &m)
		m.page, m.focus, m.width, m.height = "settings", focus, width, height
		m.revealContentFocus()
		m.layout()
		return m
	}
	move := func(t *testing.T, m Model, msg tea.KeyPressMsg, want string) Model {
		t.Helper()
		next, _ := update(m, msg)
		if next.focus != want || next.page != "settings" || next.menu.open || next.form != "" || next.ports.open || next.busy {
			t.Fatalf("%s from %q: focus=%q page=%q menu=%t form=%q ports=%t busy=%t, want %q", msg, m.focus, next.focus, next.page, next.menu.open, next.form, next.ports.open, next.busy, want)
		}
		return next
	}

	t.Run("same row and mouse to keyboard", func(t *testing.T) {
		m := model(t, 148, 22, "settings.language")
		m, _ = click(m, findTarget(t, m, "editor.configure"))
		m, _ = update(m, special(tea.KeyEscape))
		if findTarget(t, m, "editor.configure").Y != findTarget(t, m, "settings.proxyAuth").Y {
			t.Fatal("fixture needs two controls on the same screen row")
		}
		m = move(t, m, special(tea.KeyRight), "settings.proxyAuth")
		if row := settingsPaintedTarget(t, m.screen, m.targets, m.focus); !strings.HasPrefix(row, "›") {
			t.Fatalf("crossing from a mouse-focused control did not show keyboard focus: %q", row)
		}
		m = move(t, m, key("l"), "settings.proxyAuth")
		m = move(t, m, key("h"), "editor.configure")
		move(t, m, special(tea.KeyLeft), "nav.settings")
	})

	t.Run("nearest row and upper tie", func(t *testing.T) {
		// Start page is just below the active two-line tab strip.
		m := model(t, 160, 45, "settings.startPage")
		move(t, m, key("l"), coreSettingsTabs[accessTab])

		m = model(t, 160, 45, "editor.configure")
		above := findTarget(t, m, "settings.allowLAN").Y
		below := findTarget(t, m, "settings.bindAddress").Y
		y := findTarget(t, m, m.focus).Y
		if y <= above || y-above != below-y {
			t.Fatalf("expected equidistant controls above and below %d, got %d and %d", y, above, below)
		}
		move(t, m, key("l"), "settings.allowLAN")
	})

	t.Run("reveal other column outside viewport", func(t *testing.T) {
		m := model(t, 115, 9, "editor.configure")
		// With no core installed, the settings in the right pane are read-only.
		m.snapshot.Installed = false
		m.revealContentFocus()
		m.layout()
		findTarget(t, m, "editor.configure")
		tab := coreSettingsTabs[accessTab]
		if hasAction(m.targets, tab) {
			t.Fatal("fixture needs the active tab above the viewport")
		}
		before := m.scroll
		m = move(t, m, special(tea.KeyRight), tab)
		findTarget(t, m, tab)
		if m.scroll >= before {
			t.Fatalf("viewport did not scroll up to the destination: %d → %d", before, m.scroll)
		}
	})

	t.Run("single column keeps back navigation", func(t *testing.T) {
		m := model(t, 40, 24, "editor.configure")
		m = move(t, m, special(tea.KeyRight), "editor.configure")
		move(t, m, key("h"), "menu")
	})

	t.Run("focused switch while its save is pending", func(t *testing.T) {
		m := model(t, 160, 45, "settings.allowLAN")
		m.busy = true
		m.layout()
		if hasAction(m.targets, m.focus) {
			t.Fatal("a pending save should disable the focused switch")
		}
		m, _ = update(m, key("h"))
		if m.focus != "key-hints.toggle" {
			t.Fatalf("left during save: focus=%q, want the enabled switch in the left column", m.focus)
		}
	})

	t.Run("crossing into tab strip keeps tab navigation", func(t *testing.T) {
		m := model(t, 160, 45, "settings.language")
		m = move(t, m, key("l"), coreSettingsTabs[accessTab])
		m = move(t, m, key("l"), coreSettingsTabs[networkTab])
		if m.settingsTab != networkTab {
			t.Fatal("second l did not switch the active settings pane")
		}
		m = move(t, m, special(tea.KeyTab), "settings.ipv6")
		move(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, coreSettingsTabs[networkTab])
	})

	t.Run("theme dropdown owns keys until dismissed", func(t *testing.T) {
		m := model(t, 148, 22, "settings.theme")
		m, _ = update(m, special(tea.KeyEnter))
		if !m.menu.open {
			t.Fatal("Enter did not open the theme dropdown")
		}
		saved, initial := m.theme, m.menu.selected
		m, _ = update(m, key("j"))
		selected := m.menu.selected
		if selected == initial {
			t.Fatal("j did not move the menu cursor")
		}
		for _, msg := range []tea.KeyPressMsg{special(tea.KeyRight), key("h"), special(tea.KeyLeft), key("l")} {
			m, _ = update(m, msg)
			if !m.menu.open || m.menu.selected != selected || m.focus != "settings.theme" || m.theme != saved || m.busy {
				t.Fatalf("%s moved focus or changed the theme while the dropdown was open", msg)
			}
		}
		m, _ = update(m, special(tea.KeyEscape))
		m = move(t, m, key("l"), "settings.bindAddress")
		m = move(t, m, key("h"), "settings.theme")
		move(t, m, key("h"), "nav.settings")
	})
}
