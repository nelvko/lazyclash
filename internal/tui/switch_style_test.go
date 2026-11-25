package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestSettingsSwitchStylePreviewsAndImmediateSelection(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot(), prefs: app.Preferences{Theme: app.ThemeLight, SwitchStyle: app.SwitchStyleLine}}
	m := ready(b)
	directionCleanup(t, &m)
	m.page, m.focus = "settings", "settings.switchStyle"
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	m.noColor, m.colorProfile = false, colorprofile.TrueColor
	m, _ = update(m, special(tea.KeyEnter))
	if m.switchStyle != app.SwitchStyleLine || !m.menu.open || len(m.menu.items) != 4 || !m.menu.items[3].Checked {
		t.Fatal("saved style did not open as the selected item")
	}
	for _, face := range []string{"●▱▱ → ▰▰●", "OFF →  ON", "●── → ━━●"} {
		if !strings.Contains(ansi.Strip(m.screen), face) {
			t.Fatalf("missing switch preview %q", face)
		}
	}
	if face := m.menu.items[1].preview; strings.Contains(ansi.Strip(face), "OFF") || !strings.Contains(face, "\x1b[") {
		t.Fatalf("missing color slider preview: %q", face)
	}
	m, _ = update(m, tea.ColorProfileMsg{Profile: colorprofile.ASCII})
	if !strings.Contains(ansi.Strip(m.menu.items[1].preview), "OFF →  ON") {
		t.Fatal("open preview did not adapt to the terminal's color capability")
	}
	m, _ = update(m, special(tea.KeyUp))
	if m.switchStyle != app.SwitchStyleLine || !m.menu.items[3].Checked {
		t.Fatal("moving the cursor committed a style")
	}
	before := m.snapshot
	var cmd tea.Cmd
	m, cmd = update(m, special(tea.KeyEnter))
	if m.menu.open || m.busy || !m.styleSavePending || m.switchStyle != app.SwitchStyleText || m.dashboardOptions().SwitchStyle != app.SwitchStyleText {
		t.Fatal("style choice did not immediately reach the dashboard")
	}
	m, _ = update(m, cmd())
	if m.styleSavePending || b.prefs.SwitchStyle != app.SwitchStyleText || m.theme != app.ThemeLight || !reflect.DeepEqual(m.snapshot, before) {
		t.Fatal("saving the style changed the theme or the runtime state")
	}
	cmd = m.changeTheme(app.ThemeDark)
	m, _ = update(m, actionCmd(t, cmd)())
	if m.switchStyle != app.SwitchStyleText || m.theme != app.ThemeDark {
		t.Fatal("changing the theme changed the switch style")
	}
}

func TestSwitchStyleSaveFailureRollsBackOnlyCurrentSelection(t *testing.T) {
	b := &fakeBackend{saveSwitchStyle: func(context.Context, string) error { return errors.New("disk full") }}
	m := ready(b)
	m.page = "settings"
	cmd := m.changeSwitchStyle(app.SwitchStyleSlider)
	m, _ = update(m, switchStyleSavedMsg{id: m.styleSaveGeneration - 1, previous: app.SwitchStyleLine, err: errors.New("stale error")})
	if !m.styleSavePending || m.switchStyle != app.SwitchStyleSlider || m.failure != "" {
		t.Fatal("an old save changed the current style")
	}
	m, _ = update(m, cmd())
	if m.styleSavePending || m.switchStyle != app.SwitchStyleSlanted || len(m.notification.items) == 0 || !strings.Contains(m.notification.items[len(m.notification.items)-1].Message, "disk full") {
		t.Fatalf("failed style save: pending=%v style=%q notifications=%+v", m.styleSavePending, m.switchStyle, m.notification.items)
	}
}

func TestSwitchStylePendingSavePreservesOtherControlsAndQuitWaits(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	m := ready(b)
	m.page, m.focus = "settings", "settings.switchStyle"
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	before := findTarget(t, m, "settings.ipv6")
	cmd := m.changeSwitchStyle(app.SwitchStyleLine)
	m.layout()
	if m.busy || m.focus != "settings.switchStyle" || findTarget(t, m, "settings.ipv6") != before || m.changeSwitchStyle(app.SwitchStyleSlider) != nil {
		t.Fatal("saving the style moved a neighbor, lost focus, or allowed a duplicate write")
	}
	m, _ = update(m, resultMsg{id: m.id, prefs: &app.Preferences{SwitchStyle: app.SwitchStyleText}})
	if m.switchStyle != app.SwitchStyleLine {
		t.Fatal("a late preference read replaced a user choice")
	}
	if m.requestQuit() != nil {
		t.Fatal("quit lost an in-flight style preference")
	}
	if quit := m.acceptSwitchStyleSaved(cmd().(switchStyleSavedMsg)); quit == nil {
		t.Fatal("completed style write did not release quit")
	}
}
