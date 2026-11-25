package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestSettingsSwitchShowsLoadingThenAnimatesReadback(t *testing.T) {
	const id = "settings.unifiedDelay"
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	calls := 0
	b.setCoreSetting = func(_ context.Context, key, value string) error {
		calls++
		b.snapshot.CoreSettings.UnifiedDelay = value
		return nil
	}
	m := ready(b)
	directionCleanup(t, &m)
	m, _ = update(m, key("7"))
	m.width, m.height, m.focus = 160, 45, id
	m.layout()
	face := func() string {
		o := m.navigationOptions()
		return ansi.Strip(o.switchFace(id, o.settingsToggle(id), toggleOptions{}))
	}
	hit := findTarget(t, m, id)
	m, _ = update(m, tea.MouseMotionMsg{X: hit.X + hit.W - 1, Y: hit.Y})
	if hit.W != switchWidth || m.hover != id || face() != "●▱▱" {
		t.Fatalf("initial face=%q hover=%q hit=%+v", face(), m.hover, hit)
	}
	m, op := click(m, hit)
	if face() != "..." || hasAction(m.targets, id) || calls != 0 {
		t.Fatalf("pending switch face=%q calls=%d", face(), calls)
	}
	m, _ = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, op)
	if calls != 1 || face() != "●▱▱" || m.snapshot.CoreSettings.UnifiedDelay != "on" {
		t.Fatalf("initial frame after readback=%q calls=%d", face(), calls)
	}
	token := m.switches.items[id].token
	m, _ = update(m, switchFrameMsg{id: id, token: token, position: 1})
	if face() != "▰●▱" || findTarget(t, m, id) != hit {
		t.Fatalf("middle frame=%q or hit area moved", face())
	}
	m, _ = update(m, switchFrameMsg{id: id, token: token, position: 2})
	if face() != "▰▰●" {
		t.Fatalf("confirmed switch face=%q", face())
	}

	// A new external observation starts a reverse transition. Navigating away
	// destroys it, so its queued tick cannot affect the next visit.
	b.snapshot.CoreSettings.UnifiedDelay = "off"
	m, _ = update(m, coreStatusMsg{generation: m.statusGeneration, snapshot: b.snapshot})
	stale := switchFrameMsg{id: id, token: m.switches.items[id].token, position: 1}
	m, _ = update(m, key("1"))
	m, _ = update(m, key("7"))
	m, _ = update(m, stale)
	if face() != "●▱▱" || calls != 1 {
		t.Fatalf("reopened settings face=%q calls=%d", face(), calls)
	}
}

func TestSettingsTextSwitchIgnoresKeyboardRepeatAfterConfirmation(t *testing.T) {
	b := &fakeBackend{snapshot: settingsTestSnapshot(), prefs: app.Preferences{SwitchStyle: app.SwitchStyleText}}
	m := ready(b)
	directionCleanup(t, &m)
	m, _ = update(m, key("7"))
	m.focus = "key-hints.toggle"
	m, cmd := update(m, special(tea.KeySpace))
	m = finishKeyHintsSave(t, m, cmd)
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeySpace, Text: " ", IsRepeat: true})
	o := m.navigationOptions()
	if m.keyHints.pending || ansi.Strip(o.switchFace(m.focus, o.settingsToggle(m.focus), toggleOptions{})) != "OFF" {
		t.Fatal("a held activation key reversed the saved choice or text style animated")
	}
}
