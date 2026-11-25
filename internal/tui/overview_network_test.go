package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
				if width >= 40 && strings.Count(plain, o.Locale.T("dashboard.network.unsupported")) != 2 {
					t.Fatalf("expected one unavailable state per group: %s", plain)
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
	if !hasAction(card.targets, "tun.disable") || strings.Contains(plain, "Saved:") || strings.Contains(plain, "Running TUN:") || !strings.Contains(plain, "Host TUN") {
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

func TestNetworkCardGroupsSwitchAndConfigurationAtEachWidth(t *testing.T) {
	snapshot := hostTUNBackend().snapshot
	snapshot.Service = service.Status{Active: true, State: "active"}
	actual := snapshot.TUN
	snapshot.TUNActual = &actual
	snapshot.TerminalProxy.State = "off"
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{36, 43, 44, 46, 47, 76} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				o := dashboardOptions{Snapshot: snapshot, Locale: i18n.New(language), PlainButtons: true, Focus: "network.primaryConfig"}
				card := overviewNetworkCard(o, width)
				assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
				assertRegions(t, card.targets, width, len(card.lines))
				if len(card.targets) != 4 {
					t.Fatalf("expected two controls per group: %+v", card.targets)
				}
				for i, id := range []string{"terminal.enable", "network.primaryConfig", "tun.enable", "network.tunConfig"} {
					h := card.targets[i]
					if h.ID != id {
						t.Fatalf("unexpected reading order: %+v", card.targets)
					}
					face := ansi.Strip(ansi.Cut(card.lines[h.Y], h.X, h.X+h.W))
					label := o.Locale.T("dashboard.network.configure")
					if i%2 == 0 {
						label = "○ " + o.Locale.T("dashboard.network.off")
					}
					if !strings.Contains(face, label) || (h.ID == o.Focus) != strings.Contains(face, "›") {
						t.Fatalf("%s target misses its monochrome face or focus marker: %q", id, face)
					}
					if width >= 44 && (i < 2 && h.X+h.W > width/2 || i >= 2 && h.X < width/2) {
						t.Fatalf("%s hit crosses into the other group: %+v", id, h)
					}
				}
				heading := func(key string) (x, y int) {
					t.Helper()
					for y, row := range card.lines {
						if before, _, found := strings.Cut(ansi.Strip(row), o.Locale.T(key)); found {
							return ansi.StringWidth(before), y
						}
					}
					t.Fatalf("missing group heading %q", o.Locale.T(key))
					return 0, 0
				}
				terminalX, terminalY := heading("terminal.title")
				tunX, tunY := heading("dashboard.network.tun")
				terminalToggle, terminalConfig := card.targets[0], card.targets[1]
				tunToggle, tunConfig := card.targets[2], card.targets[3]
				if terminalToggle.Y <= terminalY || terminalConfig.Y <= terminalToggle.Y ||
					tunToggle.Y <= tunY || tunConfig.Y <= tunToggle.Y {
					t.Fatalf("each group must place its switch and configuration under its heading: %+v", card.targets)
				}
				if width < 44 {
					if terminalX != tunX || tunY <= terminalConfig.Y {
						t.Fatalf("narrow card did not stack whole groups: %+v", card.targets)
					}
				} else if terminalY != tunY || terminalX >= width/2 || tunX < width/2 ||
					terminalToggle.Y != tunToggle.Y || terminalConfig.Y != tunConfig.Y {
					t.Fatalf("wide card misplaced its paired headings or controls: %+v", card.targets)
				}
			})
		}
	}
}

func TestOverviewNetworkKeepsExtraActionsAndObservationsInTheirColumns(t *testing.T) {
	o := dashboardOptions{Snapshot: hostTUNBackend().snapshot, PlainButtons: true}
	o.Snapshot.Service = service.Status{Active: true, State: "active"}
	terminal := &o.Snapshot.TerminalProxy
	terminal.State, terminal.Pending, terminal.Desired, terminal.ActivationCommand = "pending", true, true, "source shell-hook"
	actual := false
	o.Snapshot.TUN, o.Snapshot.TUNActual, o.Snapshot.TUNError = true, &actual, "Mihomo route unavailable"
	card := overviewNetworkCard(o, 46)
	var left, right []string
	for _, row := range card.lines {
		left = append(left, strings.TrimSpace(ansi.Strip(ansi.Cut(row, 0, 22))))
		right = append(right, strings.TrimSpace(ansi.Strip(ansi.Cut(row, 24, 46))))
	}
	rightText := strings.Join(strings.Fields(strings.Join(right, " ")), " ")
	leftText := strings.Join(strings.Fields(strings.Join(left, " ")), " ")
	if !strings.Contains(rightText, o.Snapshot.TUNError) || !strings.Contains(rightText, "Running TUN: Off") || strings.Contains(leftText, "Mihomo") {
		t.Fatalf("TUN diagnostic missing from its column: %s", ansi.Strip(strings.Join(card.lines, "\n")))
	}
	for _, target := range card.targets {
		if target.ID == "terminal.activation" {
			if target.H < 2 || target.X+target.W > 23 {
				t.Fatalf("wrapped activation action has incorrect click area: %+v", target)
			}
			var label []string
			for _, row := range card.lines[target.Y : target.Y+target.H] {
				label = append(label, ansi.Strip(ansi.Cut(row, target.X, target.X+target.W)))
			}
			if !strings.Contains(strings.Join(strings.Fields(strings.Join(label, " ")), " "), o.Locale.T("terminal.activationAction")) {
				t.Fatalf("click area does not contain the full activation label: %q", label)
			}
			return
		}
	}
	t.Fatal("pending terminal activation action is unreachable")
}

func TestNetworkCardTabVisitsEachGroupBeforeTheNext(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {40, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			b := hostTUNBackend()
			b.snapshot.Service.Active, b.snapshot.Service.State = true, "active"
			b.snapshot.TerminalProxy.State = "off"
			m := ready(b)
			defer m.stopRuntime()
			m.width, m.height = size[0], size[1]
			m.focus = "overview.card.dashboard.overview.network"
			m.layout()
			tab := special(tea.KeyTab)
			back := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
			for _, step := range []struct {
				key tea.KeyPressMsg
				id  string
			}{
				{special(tea.KeyEnter), "terminal.enable"},
				{tab, "network.primaryConfig"}, {tab, "tun.enable"}, {tab, "network.tunConfig"},
				{back, "tun.enable"}, {back, "network.primaryConfig"}, {back, "terminal.enable"},
			} {
				m, _ = update(m, step.key)
				if m.focus != step.id || m.ports.open || m.form != "" || m.busy {
					t.Fatalf("%s focused %q, want %q without activation", step.key.String(), m.focus, step.id)
				}
				findTarget(t, m, step.id)
			}
		})
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
