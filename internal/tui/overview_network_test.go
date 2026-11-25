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
				if len(card.targets) != 2 || card.targets[0].ID != "network.primaryConfig" || card.targets[1].ID != "network.tunConfig" {
					t.Fatalf("expected only the two configuration actions: %+v", card.targets)
				}
				for _, hidden := range []string{"17890", o.Locale.T("dashboard.field.systemProxy"), "127.0.0.1", "private-user", "private-password", "private-path", "private-token"} {
					if strings.Contains(plain, hidden) {
						t.Fatalf("network summary retained addresses or credentials: %s", plain)
					}
				}
				if width >= 40 && (strings.Count(plain, o.Locale.T("dashboard.network.unsupported")) != 2 || len(card.lines) != 6) {
					t.Fatalf("expected compact two-row unavailable state: %s", plain)
				}
			}
			o.Snapshot.Installed = false
			if card := overviewNetworkCard(o, 100); len(card.targets) != 0 {
				t.Fatal("uninstalled network card exposed a settings or TUN action")
			}
			o.StateUnavailable = true
			card := overviewNetworkCard(o, 100)
			if len(card.targets) != 0 || strings.Count(ansi.Strip(strings.Join(card.lines, "\n")), o.Locale.T("dashboard.status.unknown")) != 2 {
				t.Fatal("missing snapshot was reported as a known capability or active control")
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
	if !hasAction(card.targets, "tun.disable") || strings.Contains(plain, "Saved:") || strings.Contains(plain, "Running:") || !strings.Contains(plain, "Host TUN") {
		t.Fatalf("confirmed host TUN state lost its scope or retained duplicate observations: %s", plain)
	}
	actual = false
	card = overviewNetworkCard(o, 100)
	plain = ansi.Strip(strings.Join(card.lines, "\n"))
	if !hasAction(card.targets, "tun.disable") || !strings.Contains(plain, "Saved: On") || !strings.Contains(plain, "Running: Off") {
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

func TestNetworkCardPairsConfigurationAndSwitchInReadingOrder(t *testing.T) {
	snapshot := hostTUNBackend().snapshot
	snapshot.Service = service.Status{Active: true, State: "active"}
	actual := snapshot.TUN
	snapshot.TUNActual = &actual
	snapshot.TerminalProxy.State = "off"
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{36, 46, 76} {
			o := dashboardOptions{Snapshot: snapshot, Locale: i18n.New(language), PlainButtons: true, Focus: "network.primaryConfig"}
			card := overviewNetworkCard(o, width)
			assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
			assertRegions(t, card.targets, width, len(card.lines))
			if len(card.targets) != 4 || len(card.lines) != 6 {
				t.Fatalf("%s/%d: expected two control rows, got %v\n%s", language, width, card.targets, ansi.Strip(strings.Join(card.lines, "\n")))
			}
			for i, id := range []string{"network.primaryConfig", "terminal.enable", "network.tunConfig", "tun.enable"} {
				h := card.targets[i]
				if h.ID != id || h.Y != 3+i/2 {
					t.Fatalf("%s/%d: control %d misplaced: %+v", language, width, i, card.targets)
				}
				face := ansi.Strip(ansi.Cut(card.lines[h.Y], h.X, h.X+h.W))
				if i == 0 && !strings.Contains(face, "›") {
					t.Fatalf("focused configuration lacks a monochrome cursor: %q", face)
				}
				if i >= 2 && h.X != card.targets[i-2].X {
					t.Fatalf("row controls must share the same columns: %+v", card.targets)
				}
				if i%2 == 1 && !strings.Contains(face, "○") {
					t.Fatalf("toggle target missed its observed state: %q / %+v", face, h)
				}
			}
		}
	}
}

func TestNetworkCardAlignsObservedAndSavedStateBelowItsSwitch(t *testing.T) {
	actual := false
	o := dashboardOptions{Snapshot: hostTUNBackend().snapshot, Height: 45, Locale: i18n.New(i18n.Chinese)}
	o.Snapshot.Service = service.Status{Active: true, State: "active"}
	o.Snapshot.TerminalProxy.State = "off"
	o.Snapshot.TUN, o.Snapshot.TUNActual = true, &actual
	card := overviewNetworkCard(o, 66)
	terminal, tun := card.targets[1], card.targets[3]
	if terminal.X != tun.X || terminal.Y+2 != tun.Y {
		t.Fatalf("wide rows lost their common control column or vertical separation: %+v", card.targets)
	}
	detail := o.Locale.T("dashboard.overview.tunActual", o.Locale.T("dashboard.control.off"))
	line := ansi.Strip(card.lines[tun.Y+1])
	offset := strings.Index(line, detail)
	if offset < 0 || ansi.StringWidth(line[:offset]) != tun.X+2 {
		t.Fatalf("running observation did not line up under the switch: %q / %+v", line, tun)
	}
}

func TestNetworkToggleFocusFollowsNewObservation(t *testing.T) {
	snapshot := hostTUNBackend().snapshot
	snapshot.TerminalProxy.State = "on"
	snapshot.TerminalProxy.Desired = true
	m := Model{page: "overview", width: 120, height: 32, focus: "terminal.enable"}
	m.adoptSnapshot(snapshot)
	if m.focus != "terminal.disable" {
		t.Fatalf("updated terminal switch lost focus: %s", m.focus)
	}
	m.focus = "tun.enable"
	snapshot.TUN = true
	m.adoptSnapshot(snapshot)
	if m.focus != "tun.disable" {
		t.Fatalf("updated host TUN switch lost focus: %s", m.focus)
	}
	snapshot.Capabilities.TUN.Supported = false
	m.adoptSnapshot(snapshot)
	if m.focus != "network.tunConfig" {
		t.Fatalf("unavailable switch stole focus from the TUN row: %s", m.focus)
	}
}

func TestNetworkSwitchCanReversePendingShellRequest(t *testing.T) {
	o := dashboardOptions{Snapshot: hostTUNBackend().snapshot}
	o.Snapshot.Service.Active = true
	terminal := &o.Snapshot.TerminalProxy
	terminal.State, terminal.Applied = "disabling", true
	card := overviewNetworkCard(o, 46)
	if !hasAction(card.targets, "terminal.enable") || hasAction(card.targets, "terminal.disable") {
		t.Fatal("a pending off request must allow the user to request on again")
	}
	terminal.State, terminal.Applied, terminal.Pending, terminal.ActivationCommand = "off", false, true, "source shell-hook"
	if hasAction(overviewNetworkCard(o, 46).targets, "terminal.activation") {
		t.Fatal("an off request waiting in a shell ticket must not offer first-time activation")
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
