package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewNetworkKeepsUnavailableControlsDisabledAndAddressesInDetails(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			o := dashboardOptions{Locale: i18n.New(language), Snapshot: app.Snapshot{
				Installed: true, MixedPort: 17890,
				Controller:   "http://private-user:private-password@127.0.0.1:19090/private-path?token=private-token",
				Service:      service.Status{Active: true, State: "active"},
				Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
			}}
			for _, width := range []int{16, 40, 100} {
				card := overviewNetworkCard(o, width)
				plain := ansi.Strip(strings.Join(card.lines, "\n"))
				assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
				assertRegions(t, card.targets, width, len(card.lines))
				for _, target := range card.targets {
					if target.ID != "card.network" {
						t.Fatalf("unavailable network control became actionable: %s", target.ID)
					}
				}
				for _, hidden := range []string{"127.0.0.1", "private-user", "private-password", "private-path", "private-token"} {
					if strings.Contains(plain, hidden) {
						t.Fatalf("network summary retained addresses or credentials: %s", plain)
					}
				}
				if width >= 40 && (!strings.Contains(plain, o.Locale.T("dashboard.control.tunHint")) || !strings.Contains(plain, o.Locale.T("dashboard.network.systemProxyState"))) {
					t.Fatalf("unsupported controls lost their reason: %s", plain)
				}
			}
			o.Snapshot.Installed = false
			if card := overviewNetworkCard(o, 100); len(card.targets) != 0 {
				t.Fatal("uninstalled network card exposed a settings or TUN action")
			}
		})
	}
}

func TestOverviewNetworkHostTUNKeepsObservedStateAndActionGuards(t *testing.T) {
	actual := true
	o := dashboardOptions{Snapshot: app.Snapshot{
		Installed: true, TUN: true, TUNActual: &actual,
		Service:      service.Status{Active: true, State: "active"},
		Capabilities: app.Capabilities{Scope: "system", Runtime: app.Capability{Supported: true}, TUN: app.Capability{Supported: true}},
	}}
	card := overviewNetworkCard(o, 100)
	plain := ansi.Strip(strings.Join(card.lines, "\n"))
	if !hasAction(card.targets, "tun.disable") || strings.Contains(plain, "Saved:") || strings.Contains(plain, "Running TUN:") || !strings.Contains(plain, "Entire host") {
		t.Fatalf("confirmed host TUN state lost its scope or retained duplicate observations: %s", plain)
	}
	actual = false
	card = overviewNetworkCard(o, 100)
	plain = ansi.Strip(strings.Join(card.lines, "\n"))
	if !hasAction(card.targets, "tun.disable") || !strings.Contains(plain, "Saved: On") || !strings.Contains(plain, "Running TUN: Off") {
		t.Fatalf("saved/runtime drift lost distinction or recovery control: %s", plain)
	}
	for _, kind := range []string{"service error", "transition", "busy"} {
		candidate := o
		switch kind {
		case "service error":
			candidate.Snapshot.ServiceError = "Manager unavailable"
		case "transition":
			candidate.Snapshot.Service.State = "activating"
		case "busy":
			candidate.Busy = true
		}
		card = overviewNetworkCard(candidate, 100)
		if hasAction(card.targets, "tun.disable") || hasAction(card.targets, "tun.enable") {
			t.Fatalf("%s retained an actionable TUN operation", kind)
		}
	}
}

func TestPortsDetailsShowFreshAddressesAndKeepControllerCredentialsPrivate(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := Model{locale: i18n.New(language), ports: newPortsState(), snapshot: app.Snapshot{
				Installed: true, MixedPort: 17890,
				Controller: "http://private-user:private-password@[::1]:19191/private-path?token=private-token#private-fragment",
			}}
			m.ports.step = "edit"
			m.ports.current = app.RuntimeSettings{MixedPort: 18080, ControllerPort: 19090}
			// Editing a draft must not turn its proposed ports into active addresses.
			m.ports.mixed.SetValue("18888")
			m.ports.controller.SetValue("19999")
			doc, _, _ := m.portsDocument(80)
			plain := ansi.Strip(strings.Join(doc.lines, "\n"))
			for _, want := range []string{"127.0.0.1:18080", "[::1]:19090"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("freshly loaded endpoint %q missing from settings: %s", want, plain)
				}
			}
			for _, hidden := range []string{"127.0.0.1:17890", "127.0.0.1:18888", "[::1]:19191", "[::1]:19999", "private-user", "private-password", "private-path", "private-token", "private-fragment"} {
				if strings.Contains(plain, hidden) {
					t.Fatalf("settings showed stale/proposed endpoint or leaked %q: %s", hidden, plain)
				}
			}
		})
	}
}
