package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSettingsSwitchRejectsRepeatedKeyAndUnobservedAutostart(t *testing.T) {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	m.page, m.focus = "settings", autostartToggleTarget
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyEnter, IsRepeat: true})
	if m.busy {
		t.Fatal("repeated key started an autostart change")
	}
	m.snapshot.Service.EnableState = "unknown"
	m, _ = update(m, special(tea.KeyEnter))
	if m.busy || hasAction(m.targets, autostartToggleTarget) {
		t.Fatal("unknown autostart state allowed a toggle")
	}
}
