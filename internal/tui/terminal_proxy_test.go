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
	calls int
	fail  bool
}

func (b *terminalBackend) SetTerminalProxy(_ context.Context, enabled bool) error {
	b.calls++
	if b.fail {
		return errors.New("installation failed")
	}
	b.snapshot.TerminalProxy.Desired = enabled
	if enabled && !b.snapshot.TerminalProxy.Loaded {
		b.snapshot.TerminalProxy.Installed = true
		b.snapshot.TerminalProxy.State = "pending"
	}
	return nil
}

func TestFirstTerminalProxyEnableInstallsWithoutConfirmationAndAwaitsActivation(t *testing.T) {
	b := &terminalBackend{fakeBackend: hostTUNBackend()}
	b.snapshot.Service.Active, b.snapshot.Service.State = true, "active"
	b.snapshot.TerminalProxy = shellintegration.Status{Shell: "bash", State: "uninstalled"}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	m.width, m.height = 120, 32
	cmd := m.activate("terminal.enable")
	if m.form != "" || b.calls != 0 || !m.busy || cmd == nil {
		t.Fatal("first enable must directly start the asynchronous operation")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 1 || !m.snapshot.TerminalProxy.Installed || m.snapshot.TerminalProxy.Applied || m.snapshot.TerminalProxy.State != "pending" {
		t.Fatal("saved request was confused with applied environment")
	}
	cmd = m.activate("terminal.disable")
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 2 || m.snapshot.TerminalProxy.Desired {
		t.Fatal("disable reinstalled or lost request")
	}
}

func TestTerminalProxyInstallationFailureDoesNotSaveSuccess(t *testing.T) {
	b := &terminalBackend{fakeBackend: hostTUNBackend(), fail: true}
	b.snapshot.Service.Active, b.snapshot.Service.State = true, "active"
	b.snapshot.TerminalProxy = shellintegration.Status{Shell: "zsh", State: "uninstalled"}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	m.width, m.height = 120, 32
	cmd := m.activate("terminal.enable")
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 1 || m.snapshot.TerminalProxy.Desired || m.snapshot.TerminalProxy.Installed || m.failure == "" {
		t.Fatal("failed install claimed success")
	}
}

func TestOverviewTerminalProxyShowsDesiredAndAppliedSeparately(t *testing.T) {
	for state, label := range map[string]string{"uninstalled": "", "incomplete": "incomplete", "conflict": "conflict", "off": "", "on": "", "pending": "pending", "enabling": "enabling", "disabling": "disabling", "unsupported": "unsupported", "error": "syncError"} {
		o := dashboardOptions{Snapshot: hostTUNBackend().snapshot}
		o.Snapshot.Service.Active, o.Snapshot.Service.State = true, "active"
		o.Snapshot.TerminalProxy = shellintegration.Status{State: state, Desired: state == "pending", Pending: state == "pending", ActivationCommand: "source '/private/example/bash.sh'"}
		for _, width := range []int{16, 40, 100} {
			card := overviewNetworkCard(o, width)
			assertCanvas(t, strings.Join(card.lines, "\n"), width, len(card.lines))
			assertRegions(t, card.targets, width, len(card.lines))
			var want string
			switch state {
			case "on":
				want = slantedFrames[2]
			case "off", "uninstalled":
				want = slantedFrames[0]
			default:
				want = o.Locale.T("dashboard.network." + label)
			}
			if width >= 100 && !strings.Contains(ansi.Strip(strings.Join(card.lines, "\n")), want) {
				t.Fatalf("lost %s state", state)
			}
			if state == "pending" && !hasAction(card.targets, "terminal.activation") {
				t.Fatal("pending activation command unreachable")
			}
		}
	}
}

func TestLoadedTerminalCanToggleAfterAnotherTerminalUninstallsFiles(t *testing.T) {
	b := &terminalBackend{fakeBackend: hostTUNBackend()}
	b.snapshot.Service.Active, b.snapshot.Service.State = true, "active"
	b.snapshot.TerminalProxy = shellintegration.Status{Shell: "bash", State: "off", Loaded: true}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	cmd := m.activate("terminal.enable")
	if m.form != "" || !m.busy || cmd == nil {
		t.Fatal("loaded terminal requested another install")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if b.calls != 1 || m.snapshot.TerminalProxy.Installed {
		t.Fatal("loaded hook was not reused")
	}
}

func TestTerminalProxyHookErrorOffersRecoveryOnlyAfterAcknowledgment(t *testing.T) {
	b := &terminalBackend{fakeBackend: hostTUNBackend()}
	b.snapshot.Service.Active, b.snapshot.Service.State = true, "active"
	b.snapshot.TerminalProxy = shellintegration.Status{State: "error", HookConfirmed: true, Desired: true}
	m := ready(b.fakeBackend)
	m.backend = b
	defer m.stopRuntime()
	card := overviewNetworkCard(m.navigationOptions(), 76)
	if !hasAction(card.targets, "terminal.disable") || overviewTerminalToggle(m.navigationOptions()).canToggle() {
		t.Fatal("shell acknowledgment error must expose an explicit recovery action next to an inert state")
	}
	cmd := m.activate("terminal.disable")
	if cmd == nil || !m.busy {
		t.Fatal("the hook error could not be recovered")
	}
	b.snapshotErr = errors.New("fixture read failed")
	m, _ = update(m, actionCmd(t, cmd)())
	card = overviewNetworkCard(m.navigationOptions(), 76)
	if hasAction(card.targets, "terminal.disable") || hasAction(card.targets, "terminal.enable") || m.activate("terminal.disable") != nil {
		t.Fatal("failed postread reused stale acknowledgment to enable a recovery action")
	}
}
