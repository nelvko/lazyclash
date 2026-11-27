package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestSettingsPointerFocus(t *testing.T) {
	newSettings := func(t *testing.T) Model {
		t.Helper()
		m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
		directionCleanup(t, &m)
		m.page, m.focus = "settings", "nav.settings"
		m.width, m.height = 160, 45
		m.noColor, m.colorProfile = false, colorprofile.TrueColor
		m.layout()
		return m
	}
	checkRow := func(t *testing.T, m Model, id string, highlighted bool) {
		t.Helper()
		if m.focus != id {
			t.Fatalf("expected focus on %s, got %s", id, m.focus)
		}
		hit := findTarget(t, m, id)
		row := uv.NewScreenBuffer(m.width, 1)
		uv.NewStyledString(strings.Split(m.screen, "\n")[hit.Y]).Draw(row, row.Bounds())
		// Include the gap before switches and dropdowns to sample their row background.
		found := false
		for x := max(0, hit.X-1); x < hit.X+hit.W; x++ {
			style := row.CellAt(x, 0).Style
			want := style
			want.Bg = lipgloss.Color(settingsRowHighlight)
			found = found || style.Equal(&want)
		}
		if found != highlighted {
			t.Errorf("%s has focus background = %t, want %t", id, found, highlighted)
		}
	}

	t.Run("switch and Tab", func(t *testing.T) {
		m := newSettings(t)
		const id = "key-hints.toggle"
		m, save := click(m, findTarget(t, m, id))
		m = finishKeyHintsSave(t, m, save)
		if !m.keyHints.hidden {
			t.Fatal("switch did not hide the footer")
		}
		checkRow(t, m, id, false)
		m, _ = update(m, special(tea.KeyF1))
		if m.tooltip.bounds.W == 0 {
			t.Fatal("mouse-focused switch lost its keyboard tooltip")
		}
		m, _ = update(m, special(tea.KeyEscape))
		checkRow(t, m, id, false)
		m, _ = update(m, special(tea.KeyTab))
		checkRow(t, m, "settings.theme", true)
	})
	t.Run("dropdown", func(t *testing.T) {
		m := newSettings(t)
		const id = "settings.theme"
		m, _ = click(m, findTarget(t, m, id))
		if !m.menu.open {
			t.Fatal("theme menu did not open")
		}
		m, _ = update(m, special(tea.KeyUp))
		m, _ = update(m, special(tea.KeyEscape))
		if m.menu.open {
			t.Fatal("Esc did not close the menu")
		}
		checkRow(t, m, id, false)
		m, _ = update(m, special(tea.KeyTab))
		checkRow(t, m, "editor.configure", true)
	})
	t.Run("proxy ports", func(t *testing.T) {
		m := newSettings(t)
		const id = "runtime-settings"
		m, cmd := click(m, findTarget(t, m, id))
		m, _ = update(m, actionCmd(t, cmd)())
		if !m.ports.open || m.ports.step != "edit" {
			t.Fatal("port editor did not open")
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.ports.open {
			t.Fatal("Esc did not close the port editor")
		}
		checkRow(t, m, id, false)
	})
	t.Run("tab strip", func(t *testing.T) {
		m := newSettings(t)
		m, _ = click(m, findTarget(t, m, coreSettingsTabs[networkTab]))
		checkTab := func(tab coreSettingsTab, bold bool) {
			t.Helper()
			id := coreSettingsTabs[tab]
			if m.settingsTab != tab || m.focus != id {
				t.Fatalf("expected active tab and focus on %s", id)
			}
			hit := findTarget(t, m, id)
			screen := uv.NewScreenBuffer(m.width, m.height)
			uv.NewStyledString(m.screen).Draw(screen, screen.Bounds())
			label := screen.CellAt(hit.X+1, hit.Y)
			underline := screen.CellAt(hit.X+1, hit.Y+1)
			if underline.Content != "━" || (label.Style.Attrs&uv.AttrBold != 0) != bold {
				t.Errorf("%s underline = %q, label attrs = %d, want selected tab with bold = %t", id, underline.Content, label.Style.Attrs, bold)
			}
		}
		checkTab(networkTab, false)
		m, _ = update(m, special(tea.KeyRight))
		checkTab(resourcesTab, true)
		m, _ = update(m, special(tea.KeyLeft))
		// Selecting a pane while a numeric edit is pending also commits the edit.
		m, _ = click(m, findTarget(t, m, "settings.keepAliveIdle"))
		m, _ = update(m, special(tea.KeyEscape))
		m, _ = update(m, special(tea.KeyEnter))
		m, _ = update(m, key("5"))
		m, _ = click(m, findTarget(t, m, coreSettingsTabs[resourcesTab]))
		if m.coreNumber.id != "" || !m.busy {
			t.Fatal("clicking another tab did not submit the pending edit")
		}
		checkTab(resourcesTab, false)
	})
	t.Run("inline number", func(t *testing.T) {
		m := newSettings(t)
		m, _ = click(m, findTarget(t, m, coreSettingsTabs[networkTab]))
		const id = "settings.keepAliveIdle"
		m, _ = click(m, findTarget(t, m, id))
		m, _ = update(m, key("5"))
		if m.coreNumber.id != id || m.coreNumber.input.Value() != "5" {
			t.Fatal("mouse-opened inline editor did not accept keyboard input")
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.coreNumber.id != "" {
			t.Fatal("Esc did not close the inline editor")
		}
		checkRow(t, m, id, false)
		m, _ = update(m, special(tea.KeyTab))
		checkRow(t, m, "settings.keepAliveInterval", true)
	})
}
