package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func settingsTooltipModel() Model {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	m.locale = i18n.New(i18n.Chinese)
	m.page, m.settingsTab, m.focus = "settings", networkTab, "settings.tcpConcurrent"
	m.width, m.height = 160, 45
	m.layout()
	return m
}

func TestTooltipKeyboardNavigationAndOverlays(t *testing.T) {
	m := settingsTooltipModel()
	owner := m.focus
	info := m.tr("tooltip.shortcut")
	footer := func() string { return ansi.Strip(strings.Split(m.screen, "\n")[m.height-1]) }
	if !strings.Contains(footer(), info) || !strings.Contains(footer(), m.tr("dashboard.footer.hideHints")) {
		t.Fatalf("missing tooltip or footer shortcut: %s", footer())
	}
	for _, button := range []string{"?", "？"} {
		m, _ = update(m, key(button))
		if m.focus != owner || m.tooltip.open != "" || m.keyHints.hidden != (button == "?") {
			t.Fatalf("%s on an annotated setting changed tooltip or focus, or failed to toggle the footer", button)
		}
	}
	for _, target := range m.focusTargets() {
		if target.tip.text != "" {
			t.Fatalf("icon %s became a keyboard stop", target.ID)
		}
	}
	icon := findTarget(t, m, tooltipPrefix+owner)
	m, _ = click(m, icon)
	if m.focus != owner || m.busy {
		t.Fatal("clicking the icon activated a control")
	}
	for _, close := range []tea.KeyPressMsg{special(tea.KeyF1), special(tea.KeyEscape)} {
		m, _ = update(m, special(tea.KeyF1))
		if m.tooltip.bounds.W == 0 || m.keyHints.hidden || !strings.Contains(footer(), m.tr("tooltip.dismiss")) {
			t.Fatal("keyboard should show the focused explanation")
		}
		m, _ = update(m, close)
		if m.tooltip.open != "" || m.focus != owner || m.keyHints.hidden {
			t.Fatalf("%s did not dismiss just the tooltip", close)
		}
	}
	m, _ = update(m, special(tea.KeyF1))
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "settings.tcpKeepAlive" || m.tooltip.open != "" || strings.Contains(footer(), info) {
		t.Fatal("Tab did not advance directly to the next control")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m, _ = update(m, special(tea.KeyF1))
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	if m.tooltip.bounds.W == 0 || m.focus != owner {
		t.Fatal("resizing lost a visible keyboard tooltip")
	}
	assertCanvas(t, m.screen, 40, 16)
	m, _ = update(m, key("1"))
	if m.page == "settings" || m.tooltip.open != "" {
		t.Fatal("leaving the page kept its tooltip")
	}
}

func TestTooltipRevealsFocusedSettingAfterScrolling(t *testing.T) {
	m := settingsTooltipModel()
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	for m.scroll > 0 {
		m, _ = update(m, tea.MouseWheelMsg{X: 30, Y: 6, Button: tea.MouseWheelUp})
	}
	if m.tooltip.offscreen.tip.owner != m.focus {
		t.Fatal("expected to have scrolled the focused control out of view")
	}
	m, _ = update(m, special(tea.KeyF1))
	if m.keyHints.changed || m.tooltip.bounds.W == 0 || m.tooltip.offscreen.ID != "" {
		t.Fatal("F1 did not reveal the off-screen focused explanation")
	}
}

func TestTooltipReadOnlyIconDoesNotActivateTheSetting(t *testing.T) {
	m := settingsTooltipModel()
	m.snapshot.Installed, m.focus = false, coreSettingsTabs[networkTab]
	m.layout()
	icon := findTarget(t, m, tooltipPrefix+"settings.unifiedDelay")
	before := strings.Split(m.screen, "\n")[icon.Y]
	m, _ = update(m, tea.MouseMotionMsg{X: icon.X, Y: icon.Y})
	if m.tooltip.open != "" || strings.Split(m.screen, "\n")[icon.Y] != before {
		t.Fatal("hover changed the read-only row before the tooltip delay elapsed")
	}
	m, _ = update(m, m.tooltip.generation)
	if m.tooltip.open != icon.ID || hasAction(m.focusTargets(), icon.ID) || hasAction(m.targets, icon.tip.owner) {
		t.Fatal("uninstalled core should offer hover information on a read-only row")
	}
}
