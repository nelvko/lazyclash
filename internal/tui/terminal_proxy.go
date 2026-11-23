package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type terminalProxyBackend interface {
	SetTerminalProxy(context.Context, bool, bool) error
}

func (m *Model) controlConfirmation() bool {
	return m.form == "tun-confirm" || m.form == "terminal-confirm"
}

// The shell applies requests after exiting the TUI. While it catches up,
// the next action should reverse the saved request.
func terminalProxyControl(s app.Snapshot) (id string, available bool) {
	switch s.TerminalProxy.State {
	case "uninstalled", "off", "on", "enabling", "disabling", "pending", "error":
		if s.TerminalProxy.Desired {
			return "terminal.disable", true
		}
		return "terminal.enable", s.Service.Active && runtimeControlIssue(s) == ""
	default:
		return "terminal.enable", false
	}
}

func terminalActivationAvailable(s app.Snapshot) bool {
	return s.TerminalProxy.State == "pending" && s.TerminalProxy.ActivationCommand != ""
}

func (m *Model) showTerminalActivation() {
	if m.busy || m.quitting || !terminalActivationAvailable(m.snapshot) {
		return
	}
	m.notification.restoreFocus = m.focus
	m.notification.panel.showMessage(m.tr("terminal.activationAction"), m.tr("terminal.activation", m.snapshot.TerminalProxy.ActivationCommand))
}

func (m *Model) terminalProxyAction(enabled bool) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	if _, ok := m.backend.(terminalProxyBackend); !ok {
		return nil
	}
	action, available := terminalProxyControl(m.snapshot)
	if !available || enabled != (action == "terminal.enable") {
		return nil
	}
	s := m.snapshot.TerminalProxy
	if enabled && !s.Installed && !s.Loaded {
		m.form, m.tunFocus = "terminal-confirm", "terminal.cancel"
		m.failure = ""
		m.input.Blur()
		return nil
	}
	return m.applyTerminalProxy(enabled, false)
}

func (m *Model) applyTerminalProxy(enabled, install bool) tea.Cmd {
	backend, ok := m.backend.(terminalProxyBackend)
	if !ok {
		return nil
	}
	return m.begin(m.tr("terminal.updating"), m.tr("terminal.saved"), func(ctx context.Context) error {
		return backend.SetTerminalProxy(ctx, enabled, install)
	}, notificationOperation{Key: "terminal-proxy", Context: m.tr("terminal.title")})
}

func (m *Model) terminalConfirmationAction(id string) tea.Cmd {
	if m.form != "terminal-confirm" || m.busy || m.quitting {
		return nil
	}
	if id != "terminal.cancel" && id != "terminal.confirm" {
		return nil
	}
	if id == "terminal.confirm" {
		if _, targets := m.renderTUNConfirmation(""); len(targets) == 0 {
			return nil
		}
	}
	m.form, m.tunFocus = "", ""
	if id == "terminal.cancel" {
		return nil
	}
	action, available := terminalProxyControl(m.snapshot)
	if !available || action != "terminal.enable" {
		return nil
	}
	return m.applyTerminalProxy(true, true)
}
