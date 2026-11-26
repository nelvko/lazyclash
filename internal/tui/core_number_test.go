package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCoreNumberValidatesSavesAndClearsInPlace(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	m := ready(b)
	directionCleanup(t, &m)
	m.page, m.focus, m.settingsTab, m.width, m.height = "settings", "settings.keepAliveIdle", networkTab, 160, 45
	b.setCoreSetting = func(_ context.Context, key, value string) error {
		if key != "keep-alive-idle" {
			t.Fatalf("edited %s", key)
		}
		next, err := b.snapshot.CoreSettings.With(key, value)
		b.snapshot.CoreSettings = next
		return err
	}
	m.layout()
	m, _ = update(m, special(tea.KeyEnter))
	hit := findTarget(t, m, "settings.keepAliveIdle")
	if m.form != "" || hit.InputWidth == 0 || m.coreNumber.id != m.focus {
		t.Fatal("expected an editable field in the row")
	}
	m, _ = update(m, tea.PasteMsg{Content: "invalid"})
	m, _ = update(m, special(tea.KeyEnter))
	if !m.coreNumber.invalid || m.busy || m.contextHelp() != m.tr("settings.numberInvalid") {
		t.Fatal("invalid draft escaped validation")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.coreNumber.id != "" || m.focus != "settings.keepAliveIdle" {
		t.Fatal("cancel lost focus")
	}
	for _, edit := range []struct{ input, want string }{{"0033", "33"}, {"", "default"}} {
		m, _ = update(m, special(tea.KeyEnter))
		m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		m, _ = update(m, tea.PasteMsg{Content: edit.input})
		var cmd tea.Cmd
		m, cmd = update(m, special(tea.KeyEnter))
		if !m.busy || m.coreNumber.id != "" {
			t.Fatal("edit did not submit")
		}
		m = finishProfileJob(t, m, cmd)
		if m.snapshot.CoreSettings.KeepAliveIdle != edit.want {
			t.Fatalf("saved %q", m.snapshot.CoreSettings.KeepAliveIdle)
		}
	}
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, tea.PasteMsg{Content: "19"})
	tab := findTarget(t, m, "settings.tab.resources")
	m, cmd := update(m, tea.MouseClickMsg{X: tab.X, Y: tab.Y, Button: tea.MouseLeft})
	if m.settingsTab != resourcesTab || m.coreNumber.id != "" || !m.busy {
		t.Fatal("clicking another tab should save the valid draft and switch tabs")
	}
	m = finishProfileJob(t, m, cmd)
	if m.snapshot.CoreSettings.KeepAliveIdle != "19" {
		t.Fatal("switching tabs lost the draft")
	}
	m.snapshot.CoreSettings.DisableKeepAlive = "on"
	if m.openCoreNumber("settings.keepAliveIdle") != nil || m.coreNumber.id != "" {
		t.Fatal("disabled field opened")
	}
}
