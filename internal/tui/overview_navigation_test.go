package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/shellintegration"
)

func TestOverviewNetworkTabsBrowseWithoutChangingProxySettings(t *testing.T) {
	b := runningTerminalBackend()
	b.snapshot.TerminalProxy = shellintegration.Status{Shell: "zsh", State: "off", Loaded: true}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	m.width, m.height, m.focus = 160, 45, "overview.card.dashboard.overview.network"
	m.layout()
	m, _ = update(m, key("enter"))
	if m.focus != "network.tab.tun" || !hasAction(m.targets, "tun.enable") || hasAction(m.targets, "terminal.enable") {
		t.Fatalf("system instance did not enter its selected host TUN tab: %s", m.focus)
	}

	m, _ = click(m, findTarget(t, m, "network.tab.terminal"))
	if m.networkSection != "terminal" || !hasAction(m.targets, "terminal.enable") || hasAction(m.targets, "tun.enable") {
		t.Fatal("click did not switch the visible panel")
	}
	m, _ = update(m, key("tab"))
	if m.focus != "network.tab.tun" || m.networkSection != "terminal" {
		t.Fatalf("Tab did not browse independently of selection: %s / %s", m.focus, m.networkSection)
	}
	label := m.tr("navigation.controls.viewNetworkSection", m.tr("dashboard.network.tun"))
	if want := m.controlActionHelp(label); m.overviewContextHelp() != want {
		t.Fatalf("section hint described a service action: %s", m.overviewContextHelp())
	}
	m, _ = update(m, key("enter"))
	if m.networkSection != "tun" || m.form != "" || !hasAction(m.targets, "tun.enable") {
		t.Fatal("Enter on the TUN tab should only display host policy")
	}
	m, _ = update(m, key("tab"))
	m, _ = update(m, key("enter"))
	if m.networkSection != "system" || m.focus != "network.tab.system" || hasAction(m.targets, "tun.enable") || hasAction(m.targets, "terminal.enable") {
		t.Fatal("system tab did not remain independently browsable")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.networkSection != "system" || m.focus != "network.tab.system" || m.form != "" || m.busy || b.calls != 0 {
		t.Fatal("browsing or resizing the sections changed proxy settings or lost selection")
	}
}

func TestOverviewUserNetworkCardEntersTerminalByDefault(t *testing.T) {
	b := runningTerminalBackend()
	b.snapshot.Capabilities.Scope, b.snapshot.Capabilities.TUN.Supported = "user", false
	m := ready(b.fakeBackend)
	defer m.stopRuntime()
	m.width, m.height, m.focus = 120, 45, "overview.card.dashboard.overview.network"
	m.layout()
	m, _ = update(m, key("enter"))
	if m.focus != "network.tab.terminal" || m.networkSection != "" {
		t.Fatalf("user instance opened with %s / %s", m.focus, m.networkSection)
	}
}

func TestOverviewTerminalSwitchKeepsFocusOnItsRow(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(fmt.Sprintf("core-active=%t", active), func(t *testing.T) {
			b := runningTerminalBackend()
			b.snapshot.TerminalProxy = shellintegration.Status{Shell: "zsh", State: "on", Desired: true, Applied: true, Loaded: true}
			if !active {
				b.snapshot.Service.Active, b.snapshot.Service.State = false, "inactive"
			}
			m := ready(b.fakeBackend)
			m.backend = b
			defer m.stopRuntime()
			m.width, m.height = 120, 45
			m.layout()
			m, _ = click(m, findTarget(t, m, "network.tab.terminal"))
			m, cmd := click(m, findTarget(t, m, "terminal.disable"))
			m, _ = update(m, actionCmd(t, cmd)())
			if m.snapshot.TerminalProxy.Desired || b.calls != 1 {
				t.Fatal("failed to observe saved disable request")
			}
			if !active {
				if m.focus != "network.tab.terminal" {
					t.Fatalf("unavailable terminal row moved focus to %s", m.focus)
				}
				return
			}
			if m.focus != "terminal.enable" {
				t.Fatalf("terminal switch moved focus to %s", m.focus)
			}
			m, cmd = update(m, key("enter"))
			m, _ = update(m, actionCmd(t, cmd)())
			if m.focus != "terminal.disable" || !m.snapshot.TerminalProxy.Desired || b.calls != 2 {
				t.Fatalf("keyboard switch failed to stay on the terminal row: %s", m.focus)
			}
		})
	}
}

func TestOverviewTUNSwitchKeepsFocusThroughHostConfirmation(t *testing.T) {
	b := hostTUNBackend()
	b.tun = func(_ context.Context, enabled bool) error {
		b.snapshot.TUN = enabled
		return nil
	}
	m := ready(b)
	defer m.stopRuntime()
	m.width, m.height = 120, 45
	m.layout()
	m, _ = click(m, findTarget(t, m, "tun.enable"))
	m, _ = update(m, key("tab"))
	m, cmd := update(m, key("enter"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.focus != "tun.disable" || !m.snapshot.TUN {
		t.Fatalf("after confirming host TUN, focus moved to %s", m.focus)
	}
	m, cmd = update(m, key("enter"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.focus != "tun.enable" || m.snapshot.TUN {
		t.Fatalf("after disabling host TUN, focus moved to %s", m.focus)
	}
}

func TestOverviewPagingKeepsFocusVisibleAndEnterDoesNotToggleCore(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {120, 18}, {60, 18}} {
		for _, initial := range []string{"overview.card.dashboard.control.core", "core.toggle"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], initial), func(t *testing.T) {
				m := themeSample("overview-running")
				m.width, m.height, m.focus = size[0], size[1], initial
				m.layout()
				for range 2 {
					m, _ = update(m, special(tea.KeyPgDown))
				}
				g := dashboardGeometry(m.dashboardOptions())
				visible := false
				for _, card := range overviewCardRegionsFromModel(m) {
					if card.ID == m.focus && card.Y < m.scroll+g.height && card.Y+card.H > m.scroll {
						visible = true
					}
				}
				if !visible || !strings.HasPrefix(m.focus, "overview.card.") {
					t.Fatalf("paging left focus outside the visible cards: %s at scroll %d", m.focus, m.scroll)
				}
				m, _ = update(m, special(tea.KeyEnter))
				if m.busy || m.focus == "core.toggle" {
					t.Fatalf("Enter after paging returned to core control: focus=%s busy=%t", m.focus, m.busy)
				}
				m, _ = update(m, special(tea.KeyPgUp))
				if m.busy {
					t.Fatal("paging initiated an operation")
				}
			})
		}
	}
}
