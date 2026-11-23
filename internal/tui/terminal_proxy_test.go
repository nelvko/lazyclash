package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/shellintegration"
)

type terminalBackend struct {
	*fakeBackend
	calls   int
	install bool
	fail    bool
}

func runningTerminalBackend() *terminalBackend {
	b := &terminalBackend{fakeBackend: hostTUNBackend()}
	b.snapshot.Service.Active, b.snapshot.Service.State = true, "active"
	return b
}

func (b *terminalBackend) SetTerminalProxy(_ context.Context, enabled, install bool) error {
	b.calls++
	b.install = install
	if b.fail {
		return errors.New("installation failed")
	}
	b.snapshot.TerminalProxy.Desired = enabled
	b.snapshot.TerminalProxy.Installed = true
	b.snapshot.TerminalProxy.State = "pending"
	return nil
}

func TestTerminalProxyInstallationRequiresConfirmationAndNeverClaimsApplied(t *testing.T) {
	b := runningTerminalBackend()
	b.snapshot.TerminalProxy = shellintegration.Status{Shell: "bash", State: "uninstalled"}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	m.width, m.height = 120, 32
	m.activate("terminal.enable")
	if m.form != "terminal-confirm" || m.tunFocus != "terminal.cancel" || b.calls != 0 {
		t.Fatal("installation did not wait for confirmation")
	}
	m, _ = update(m, key("enter"))
	if m.form != "" || b.calls != 0 {
		t.Fatal("default cancel installed integration")
	}
	m.activate("terminal.enable")
	m.layout()
	if !strings.Contains(ansi.Strip(m.screen), "bash") {
		t.Fatal("shell omitted from consent")
	}
	m, cmd := update(m, key("y"))
	if b.calls != 0 || !m.busy {
		t.Fatal("installation was not asynchronous")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 1 || !b.install || m.snapshot.TerminalProxy.Applied || m.snapshot.TerminalProxy.State != "pending" {
		t.Fatal("saved request was confused with applied environment")
	}
	cmd = m.activate("terminal.disable")
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 2 || b.install || m.snapshot.TerminalProxy.Desired {
		t.Fatal("disable reinstalled or lost request")
	}
}

func TestTerminalProxyInstallationFailureDoesNotSaveSuccess(t *testing.T) {
	b := runningTerminalBackend()
	b.fail = true
	b.snapshot.TerminalProxy = shellintegration.Status{Shell: "zsh", State: "uninstalled"}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	m.width, m.height = 120, 32
	m.activate("terminal.enable")
	m, cmd := update(m, key("y"))
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 1 || m.snapshot.TerminalProxy.Desired || m.snapshot.TerminalProxy.Installed || m.failure == "" {
		t.Fatal("failed install claimed success")
	}
}

func TestOverviewTerminalProxyShowsDesiredAndAppliedSeparately(t *testing.T) {
	for _, state := range []string{"uninstalled", "off", "on", "pending", "enabling", "disabling", "unsupported", "error"} {
		o := dashboardOptions{NetworkSection: "terminal", Snapshot: hostTUNBackend().snapshot}
		o.Snapshot.TerminalProxy = shellintegration.Status{State: state, Desired: state == "pending", Pending: state == "pending", ActivationCommand: "source '/private/example/bash.sh'"}
		for _, width := range []int{16, 40, 100} {
			card := overviewNetworkCard(o, width)
			assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
			assertRegions(t, card.targets, width, len(card.lines))
			if width >= 100 && !strings.Contains(ansi.Strip(strings.Join(card.lines, "\n")), o.Locale.T("terminal.state."+state)) {
				t.Fatalf("lost %s state", state)
			}
			if state == "pending" && !hasAction(card.targets, "terminal.activation") {
				t.Fatal("pending activation command unreachable")
			}
		}
	}
}

func TestLoadedTerminalCanToggleAfterAnotherTerminalUninstallsFiles(t *testing.T) {
	b := runningTerminalBackend()
	b.snapshot.TerminalProxy = shellintegration.Status{Shell: "bash", State: "off", Loaded: true}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	cmd := m.activate("terminal.enable")
	if m.form != "" || !m.busy || cmd == nil {
		t.Fatal("loaded terminal requested another install")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 1 || b.install {
		t.Fatal("loaded hook was not reused")
	}
}

func TestTerminalProxyReversesRequestedStateBeforeShellAcknowledges(t *testing.T) {
	for _, state := range []string{"pending", "enabling", "disabling", "error"} {
		t.Run(state, func(t *testing.T) {
			b := runningTerminalBackend()
			wantEnable := state == "disabling"
			b.snapshot.TerminalProxy = shellintegration.Status{
				State: state, Loaded: state != "pending", Desired: !wantEnable, Applied: wantEnable,
			}
			if !wantEnable {
				// Undoing a request only touches the shell, even if the core disappeared.
				b.snapshot.Installed = false
				b.snapshot.Service.Active = false
			}
			m := ready(b.fakeBackend)
			m.backend = b
			defer m.stopRuntime()
			if cmd := m.terminalProxyAction(!wantEnable); cmd != nil || m.busy {
				t.Fatal("duplicate or stale action sent the same request")
			}
			cmd := m.terminalProxyAction(wantEnable)
			if cmd == nil || !m.busy {
				t.Fatal("reverse action unavailable")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if b.calls != 1 || b.install || m.snapshot.TerminalProxy.Desired != wantEnable {
				t.Fatal("reverse action did not save the new request")
			}
		})
	}
}

func TestTerminalProxyRechecksEnableAvailabilityAtActivationAndConfirmation(t *testing.T) {
	for _, change := range []string{"stopped", "uninstalled", "unavailable", "service error", "transition", "unsupported shell", "requested"} {
		t.Run(change, func(t *testing.T) {
			b := runningTerminalBackend()
			b.snapshot.TerminalProxy = shellintegration.Status{State: "uninstalled", Shell: "zsh"}
			m := ready(b.fakeBackend)
			m.backend = b
			defer m.stopRuntime()
			m.width, m.height = 120, 32
			m.activate("terminal.enable")
			if m.form != "terminal-confirm" {
				t.Fatal("missing installation confirmation")
			}
			switch change {
			case "stopped":
				m.snapshot.Service.Active = false
			case "uninstalled":
				m.snapshot.Installed = false
			case "unavailable":
				m.snapshot.Capabilities.Runtime.Supported = false
			case "service error":
				m.snapshot.ServiceError = "service ownership could not be verified"
			case "transition":
				m.snapshot.Service.State = "reloading"
			case "unsupported shell":
				m.snapshot.TerminalProxy.State = "unsupported"
			case "requested":
				m.snapshot.TerminalProxy.Desired = true
			}
			if cmd := m.terminalConfirmationAction("terminal.confirm"); cmd != nil || m.form != "" || m.busy {
				t.Fatal("stale confirmation started installation")
			}
			if cmd := m.activate("terminal.enable"); cmd != nil || m.form != "" || m.busy || b.calls != 0 {
				t.Fatal("unavailable action reached the backend")
			}
		})
	}
}

func TestTerminalActivationCommandOpensScrollableDetailsAndReturnsToControl(t *testing.T) {
	b := runningTerminalBackend()
	command := "source '/a path  with spaces/bash.sh'"
	b.snapshot.TerminalProxy = shellintegration.Status{State: "pending", Desired: true, Pending: true, ActivationCommand: command}
	m := ready(b.fakeBackend)
	defer m.stopRuntime()
	m.width, m.height = 120, 45
	m.layout()
	m, _ = click(m, findTarget(t, m, "network.tab.terminal"))
	m, _ = click(m, findTarget(t, m, "terminal.activation"))
	if !m.notification.panel.open || !strings.Contains(ansi.Strip(m.screen), command) {
		t.Fatal("activation command did not open immediately or lost quoted whitespace")
	}
	m, _ = update(m, key("esc"))
	if m.notification.panel.open || m.focus != "terminal.activation" {
		t.Fatalf("Escape returned to %q", m.focus)
	}

	m.snapshot.TerminalProxy.ActivationCommand = "source '/" + strings.Repeat("long-path/", 24) + "bash.sh'"
	m.width, m.height = 36, 12
	m.activate("terminal.activation")
	m.layout()
	m, _ = update(m, key("end"))
	if !strings.Contains(ansi.Strip(m.screen), "bash.sh'") {
		t.Fatal("narrow terminal could not scroll to the full command")
	}
	m, _ = update(m, key("esc"))
	// A cancelled pending record still carries an activation command.
	m.snapshot.TerminalProxy.State, m.snapshot.TerminalProxy.Desired = "off", false
	m.activate("terminal.activation")
	if m.notification.panel.open {
		t.Fatal("cancelled shell request exposed an activation command")
	}
}
