package tui

import (
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
				if width == 40 {
					for _, edit := range card.targets {
						assertNetworkLabelCentered(t, card.lines[edit.Y-2], 2, width-2, o.Locale.T("dashboard.network.unsupported"))
					}
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
				o := dashboardOptions{Snapshot: snapshot, Locale: i18n.New(language)}
				for _, plain := range []bool{false, true} {
					o.PlainButtons, o.Focus = plain, ""
					idle := overviewNetworkCard(o, width)
					assertCanvas(t, strings.Join(idle.lines, "\n"), width, len(idle.lines))
					assertRegions(t, idle.targets, width, len(idle.lines))
					if len(idle.targets) != 4 {
						t.Fatalf("expected two controls per group: %+v", idle.targets)
					}
					ids := []string{"terminal.enable", "network.primaryConfig", "tun.enable", "network.tunConfig"}
					for _, focus := range append([]string{""}, ids...) {
						o.Focus = focus
						card := overviewNetworkCard(o, width)
						if !slices.Equal(card.targets, idle.targets) {
							t.Fatalf("focusing %q moved a control: %+v vs %+v", focus, card.targets, idle.targets)
						}
						for i, id := range ids {
							h := card.targets[i]
							if h.ID != id {
								t.Fatalf("unexpected reading order: %+v", card.targets)
							}
							start, end := 2, width-2
							if width >= 44 {
								leftEnd := start + (end-start-3)/2
								if i < 2 {
									end = leftEnd
								} else {
									start = leftEnd + 3
								}
							}
							label := o.Locale.T("dashboard.network.configure")
							if i%2 == 0 {
								label = "○ " + o.Locale.T("dashboard.network.off")
								heading := "terminal.title"
								if i == 2 {
									heading = "dashboard.network.tun"
								}
								assertNetworkLabelCentered(t, card.lines[h.Y-1], start, end, o.Locale.T(heading))
							}
							assertNetworkLabelCentered(t, card.lines[h.Y], start, end, label)
							face := ansi.Strip(ansi.Cut(card.lines[h.Y], h.X, h.X+h.W))
							if !strings.Contains(face, label) || strings.Contains(face, "›") != (id == focus) ||
								h.X < start || h.X+h.W > end {
								t.Fatalf("%s target misses its face or crosses its group: %+v, %q", id, h, face)
							}
							if i%2 == 1 {
								toggle := card.targets[i-1]
								if h.Y != toggle.Y+toggle.H+1 || strings.TrimSpace(ansi.Strip(ansi.Cut(card.lines[h.Y-1], start, end))) != "" {
									t.Fatalf("%s must have one blank row above it: %+v", id, card.targets)
								}
							}
						}
						if width < 44 && card.targets[2].Y-1 <= card.targets[1].Y ||
							width >= 44 && card.targets[0].Y != card.targets[2].Y {
							t.Fatalf("incorrect group arrangement at width %d: %+v", width, card.targets)
						}
					}
				}
			})
		}
	}
}

// Measure the label itself, excluding brackets and the reserved focus gutter.
func assertNetworkLabelCentered(t *testing.T, line string, start, end int, label string) {
	t.Helper()
	section := ansi.Strip(ansi.Cut(line, start, end))
	before, after, ok := strings.Cut(section, label)
	if !ok {
		t.Fatalf("section %q does not contain %q", section, label)
	}
	left, right := ansi.StringWidth(before), ansi.StringWidth(after)
	if max(left-right, right-left) > 1 {
		t.Fatalf("%q is off-center in %q: %d left, %d right", label, section, left, right)
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
		part := strings.TrimSpace(ansi.Strip(ansi.Cut(row, 24, 44)))
		right = append(right, part)
		if part != "" && strings.Contains(o.Snapshot.TUNError, part) {
			assertNetworkLabelCentered(t, row, 24, 44, part)
		}
	}
	rightText := strings.Join(strings.Fields(strings.Join(right, " ")), " ")
	leftText := strings.Join(strings.Fields(strings.Join(left, " ")), " ")
	if !strings.Contains(rightText, o.Snapshot.TUNError) || !strings.Contains(rightText, "Running TUN: Off") || strings.Contains(leftText, "Mihomo") {
		t.Fatalf("TUN diagnostic missing from its column: %s", ansi.Strip(strings.Join(card.lines, "\n")))
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		o.Locale = i18n.New(language)
		for _, width := range []int{16, 36, 44, 46, 76} {
			card := overviewNetworkCard(o, width)
			config, command := card.targets[1], card.targets[2]
			if config.ID != "network.primaryConfig" || command.ID != "terminal.activation" {
				t.Fatalf("unexpected action order: %+v", card.targets)
			}
			if width == 16 {
				if command.Y != config.Y+config.H {
					t.Fatalf("expected stacked actions: %+v", card.targets)
				}
			} else if config.Y != command.Y || config.X+config.W > command.X {
				t.Fatalf("expected two separate actions on one row: %+v", card.targets)
			}
			face := ansi.Strip(ansi.Cut(card.lines[command.Y], command.X, command.X+command.W))
			if !strings.Contains(face, o.Locale.T("dashboard.network.activationCommand")) {
				t.Fatalf("command target misses its label: %+v, %q", command, face)
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
				{special(tea.KeyEnter), "terminal.enable"},
				{tab, "network.primaryConfig"}, {tab, "tun.enable"}, {tab, "network.tunConfig"},
				{back, "tun.enable"}, {back, "network.primaryConfig"}, {back, "terminal.enable"},
			} {
				m, _ = update(m, step.key)
				if m.focus != step.id || m.ports.open || m.form != "" || m.busy {
					t.Fatalf("%s focused %q, want %q without activation", step.key.String(), m.focus, step.id)
				}
				h := findTarget(t, m, step.id)
				if step.key.String() == "tab" && (step.id == "network.primaryConfig" || step.id == "network.tunConfig") {
					// Click the displayed label, whose location is also checked against
					// the section center above, and return to the same keyboard stop.
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
