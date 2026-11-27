package tui

import (
	"errors"
	"fmt"
	"slices"
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
			o.Snapshot.TerminalProxy.State = "unsupported"
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
	if !hasAction(card.targets, "tun.toggle") || strings.Contains(plain, "Saved:") || strings.Contains(plain, "Running TUN:") || !strings.Contains(plain, "Host TUN") {
		t.Fatalf("confirmed host TUN state lost its scope or retained duplicate observations: %s", plain)
	}
	actual = false
	card = overviewNetworkCard(o, 100)
	plain = ansi.Strip(strings.Join(card.lines, "\n"))
	if !hasAction(card.targets, "tun.toggle") || !strings.Contains(plain, "Saved: On") || !strings.Contains(plain, "Running TUN: Off") {
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
		if hasAction(card.targets, "tun.toggle") {
			t.Fatalf("%s retained an actionable TUN operation", kind)
		}
	}
}

func TestNetworkCardAlignsReadOnlyAndInteractiveStatus(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o := dashboardOptions{Snapshot: hostTUNBackend().snapshot, Locale: i18n.New(language)}
		o.Snapshot.Service = service.Status{Active: true, State: "active"}
		o.Snapshot.TerminalProxy.State = "incomplete"
		o.Snapshot.Capabilities.TUN.Supported = false
		card := overviewNetworkCard(o, 46)
		if len(card.targets) != 3 || card.targets[0].ID != "terminal.toggle" || card.targets[2].ID != "network.tunConfig" {
			t.Fatalf("unexpected actions for repairable terminal and read-only TUN: %+v", card.targets)
		}
		var statusX int
		for i, row := range []struct{ title, state string }{
			{o.Locale.T("terminal.title"), "○ " + o.Locale.T("dashboard.network.incomplete")},
			{o.Locale.T("dashboard.network.tun"), o.Locale.T("dashboard.network.unsupported")},
		} {
			line := ansi.Strip(card.lines[card.targets[i+1].Y])
			before, _, ok := strings.Cut(line, row.state)
			if !ok || i == 1 && ansi.StringWidth(before) != statusX {
				t.Fatalf("statuses must share a visible start column:\n%s", ansi.Strip(strings.Join(card.lines, "\n")))
			}
			statusX = ansi.StringWidth(before)
			// Both translated settings fit on one line in Chinese. The longer
			// English repair status moves both headings above their controls.
			if strings.Contains(line, row.title) != (language == i18n.Chinese) {
				t.Fatalf("%s heading layout disagrees with translated control widths: %q", language, line)
			}
		}
	}
}

func TestNetworkCardAlignsSettingsAndWrapsControlsAtEachWidth(t *testing.T) {
	snapshot := hostTUNBackend().snapshot
	snapshot.Service = service.Status{Active: true, State: "active"}
	actual := snapshot.TUN
	snapshot.TUNActual = &actual
	snapshot.TerminalProxy.State = "off"
	ids := []string{"terminal.toggle", "network.primaryConfig", "tun.toggle", "network.tunConfig"}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{19, 29, 76} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				o := dashboardOptions{Snapshot: snapshot, Locale: i18n.New(language)}
				for _, plain := range []bool{false, true} {
					o.PlainButtons, o.Focus = plain, ""
					idle := overviewNetworkCard(o, width)
					view := ansi.Strip(strings.Join(idle.lines, "\n"))
					assertCanvas(t, view, width, len(idle.lines))
					assertRegions(t, idle.targets, width, len(idle.lines))
					if len(idle.targets) != len(ids) {
						t.Fatalf("expected two controls per setting: %+v", idle.targets)
					}
					terminal, editTerminal, tun, editTUN := idle.targets[0], idle.targets[1], idle.targets[2], idle.targets[3]
					if terminal.X != tun.X || editTerminal.X != editTUN.X || editTerminal.Y >= tun.Y {
						t.Fatalf("settings lost their aligned controls or reading order:\n%s", view)
					}
					for i, heading := range []string{"terminal.title", "dashboard.network.tun"} {
						toggle, edit := idle.targets[2*i], idle.targets[2*i+1]
						titleY := toggle.Y
						if width < 76 {
							titleY--
						}
						title := o.Locale.T(heading)
						if titleY < 0 || !strings.HasPrefix(ansi.Strip(idle.lines[titleY]), "  "+title) {
							t.Fatalf("heading %q missing from the left edge of its setting:\n%s", title, view)
						}
						if i == 1 && (titleY != editTerminal.Y+editTerminal.H+1 ||
							strings.TrimSpace(ansi.Strip(idle.lines[titleY-1])) != "") {
							t.Fatalf("expected one blank row between settings:\n%s", view)
						}
						if width == 19 {
							if edit.Y != toggle.Y+toggle.H {
								t.Fatalf("narrow setting should stack controls:\n%s", view)
							}
						} else if toggle.Y != edit.Y || toggle.X+toggle.W > edit.X || edit.X+edit.W != width-2 {
							t.Fatalf("expected the switch and its trailing editor on the same row:\n%s", view)
						}
					}
					for _, focus := range append([]string{""}, ids...) {
						o.Focus = focus
						card := overviewNetworkCard(o, width)
						if !slices.Equal(card.targets, idle.targets) {
							t.Fatalf("focusing %q moved a control: %+v vs %+v", focus, card.targets, idle.targets)
						}
						for i, id := range ids {
							h := card.targets[i]
							label := o.Locale.T("dashboard.network.configure")
							if i%2 == 0 {
								label = "○ " + o.Locale.T("dashboard.network.off")
							}
							if plain {
								label = "[" + label + "]"
							}
							face := ansi.Strip(ansi.Cut(card.lines[h.Y], h.X, h.X+h.W))
							if h.ID != id || !strings.Contains(face, label) || strings.Contains(face, "›") != (id == focus) {
								t.Fatalf("%s target misses its label or focus marker: %+v, %q", id, h, face)
							}
						}
					}
				}
			})
		}
	}
}

func TestOverviewNetworkKeepsExtraActionsAndObservationsWithTheirSettings(t *testing.T) {
	o := dashboardOptions{Snapshot: hostTUNBackend().snapshot, PlainButtons: true}
	o.Snapshot.Service = service.Status{Active: true, State: "active"}
	terminal := &o.Snapshot.TerminalProxy
	terminal.State, terminal.Pending, terminal.Desired, terminal.ActivationCommand = "pending", true, true, "source shell-hook"
	actual := false
	o.Snapshot.TUN, o.Snapshot.TUNActual, o.Snapshot.TUNError = true, &actual, "Mihomo route unavailable"
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o.Locale = i18n.New(language)
		for _, width := range []int{16, 36, 46, 76} {
			card := overviewNetworkCard(o, width)
			view := ansi.Strip(strings.Join(card.lines, "\n"))
			assertCanvas(t, view, width, len(card.lines))
			assertRegions(t, card.targets, width, len(card.lines))
			stops := card.overviewFocusRegions()
			if len(stops) != 5 {
				t.Fatalf("missing a setting action at width %d: %+v", width, stops)
			}
			config, command, tun, tunConfig := stops[1], stops[2], stops[3], stops[4]
			if config.ID != "network.primaryConfig" || command.ID != "terminal.activation" ||
				command.Y < config.Y+config.H || command.Y+command.H >= tun.Y {
				t.Fatalf("expected the activation command under its terminal setting:\n%s", view)
			}
			face := ansi.Strip(ansi.Cut(card.lines[command.Y], command.X, command.X+command.W))
			if !strings.Contains(face, o.Locale.T("dashboard.network.activationCommand")) {
				t.Fatalf("command target misses its label: %+v, %q", command, face)
			}
			notes := ansi.Strip(strings.Join(card.lines[tunConfig.Y+tunConfig.H:], ""))
			notes = strings.Join(strings.Fields(notes), "")
			for _, detail := range []string{o.Locale.T("dashboard.overview.tunActual", o.Locale.T("dashboard.control.off")), o.Snapshot.TUNError} {
				if !strings.Contains(notes, strings.Join(strings.Fields(detail), "")) {
					t.Fatalf("TUN detail %q missing below its setting:\n%s", detail, view)
				}
			}
		}
	}
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
				{special(tea.KeyEnter), "terminal.toggle"},
				{tab, "network.primaryConfig"}, {tab, "tun.toggle"}, {tab, "network.tunConfig"},
				{back, "tun.toggle"}, {back, "network.primaryConfig"}, {back, "terminal.toggle"},
			} {
				m, _ = update(m, step.key)
				if m.focus != step.id || m.ports.open || m.form != "" || m.busy {
					t.Fatalf("%s focused %q, want %q without activation", step.key.String(), m.focus, step.id)
				}
				h := findTarget(t, m, step.id)
				if step.key.String() == "tab" && (step.id == "network.primaryConfig" || step.id == "network.tunConfig") {
					// Click the displayed label and return to the same keyboard stop.
					row := strings.Split(m.screen, "\n")[h.Y]
					before, _, found := strings.Cut(ansi.Strip(ansi.Cut(row, h.X, h.X+h.W)), m.tr("dashboard.network.configure"))
					if !found {
						t.Fatalf("missing configuration label at %+v", h)
					}
					var cmd tea.Cmd
					m, cmd = update(m, tea.MouseClickMsg{X: h.X + ansi.StringWidth(before), Y: h.Y, Button: tea.MouseLeft})
					kind := "tun"
					if step.id == "network.primaryConfig" {
						kind = "terminal"
					}
					if !m.ports.open || m.ports.kind != kind {
						t.Fatalf("click on %s opened %q", step.id, m.ports.kind)
					}
					if cmd != nil {
						m, _ = update(m, actionCmd(t, cmd)())
					}
					m, _ = update(m, special(tea.KeyEscape))
					if m.ports.open || m.focus != step.id {
						t.Fatalf("closing %s lost the clicked control: %s", kind, m.focus)
					}
				}
			}
		})
	}
}

func TestNetworkToggleFocusFollowsNewObservation(t *testing.T) {
	snapshot := hostTUNBackend().snapshot
	snapshot.TerminalProxy.State = "on"
	snapshot.TerminalProxy.Desired = true
	m := Model{page: "overview", width: 120, height: 32, focus: "terminal.toggle"}
	m.adoptSnapshot(snapshot)
	if m.focus != "terminal.toggle" {
		t.Fatalf("updated terminal switch lost focus: %s", m.focus)
	}
	m.focus = "tun.toggle"
	snapshot.TUN = true
	m.adoptSnapshot(snapshot)
	if m.focus != "tun.toggle" {
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
	if !hasAction(card.targets, "terminal.toggle") || o.terminalSwitch().On {
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
			m.ports.kind, m.ports.step = "control", "edit"
			m.ports.current = app.RuntimeSettings{MixedPort: 18080, ControllerPort: 19090, ControllerHost: "::1"}
			m.initAccessDraft()
			doc, _, _ := m.portsDocument(80)
			plain := ansi.Strip(strings.Join(doc.lines, "\n"))
			for _, want := range []string{"[::1]:19090", m.tr("access.secretPlaceholder")} {
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

// A saved shell request and the foreground command producing it are distinct.
// While the command runs the switch cannot be reversed, including by mouse.
func TestOverviewTerminalSwitchWaitsForReadbackAndRejectsRepeatedInput(t *testing.T) {
	b := &terminalBackend{fakeBackend: hostTUNBackend()}
	b.snapshot.Service = service.Status{State: "active", Active: true}
	b.snapshot.TerminalProxy.State = "off"
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	m.focus, m.width, m.height = "terminal.toggle", 160, 45
	m.layout()
	initial := findTarget(t, m, m.focus)
	m, cmd := update(m, tea.MouseClickMsg{X: initial.X, Y: initial.Y, Button: tea.MouseLeft})
	s := m.navigationOptions().terminalSwitch()
	if !m.busy || s.Enabled() || !s.Pending || s.On || b.calls != 0 || hasAction(m.targets, "terminal.toggle") {
		t.Fatal("in-flight switch advertised an applied value or remained clickable")
	}
	repeat := special(tea.KeyEnter)
	repeat.IsRepeat = true
	id := m.id
	m, _ = update(m, repeat)
	if m.id != id || m.activate("terminal.toggle") != nil {
		t.Fatal("in-flight switch accepted another activation")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	s = m.navigationOptions().terminalSwitch()
	if b.calls != 1 || m.busy || !s.Enabled() || !s.On || s.Pending || s.Message != "◌ "+m.tr("dashboard.network.pending") {
		t.Fatal("observed shell activation requirement was lost or presented as applied")
	}
	m, _ = update(m, repeat)
	if m.busy || m.id != id {
		t.Fatal("held key reversed the saved request after completion")
	}
}

func TestNetworkSwitchesShowUnknownAfterActionReadbackFails(t *testing.T) {
	for _, id := range []string{"terminal.toggle", "tun.toggle"} {
		t.Run(id, func(t *testing.T) {
			b := &terminalBackend{fakeBackend: hostTUNBackend()}
			b.snapshot.Service = service.Status{State: "active", Active: true}
			b.snapshot.TerminalProxy.State = "off"
			b.snapshot.TUN = true
			m := ready(b.fakeBackend)
			defer m.stopRuntime()
			m.backend = b
			m.focus = id
			m.width, m.height = 120, 32
			m.layout()
			cmd := m.activate(id)
			b.snapshotErr = errors.New("fixture readback unavailable")
			m, _ = update(m, actionCmd(t, cmd)())
			s, _ := m.navigationOptions().overviewSwitch(id)
			if s.Enabled() || s.Message != m.tr("dashboard.status.unknown") || m.failure == "" || m.activate(id) != nil {
				t.Fatal("failed readback presented the earlier value as confirmed")
			}
			b.snapshotErr = nil
			m.adoptSnapshot(b.snapshot)
			s, _ = m.navigationOptions().overviewSwitch(id)
			if !s.Enabled() {
				t.Fatal("successful observation did not restore the switch")
			}
		})
	}
}
