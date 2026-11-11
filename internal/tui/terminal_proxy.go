package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

type terminalProxyBackend interface {
	SetTerminalProxy(context.Context, bool, bool) error
}

func (m Model) controlConfirmation() bool {
	return m.form == "tun-confirm" || m.form == "terminal-confirm"
}

func (m *Model) terminalProxyAction(enabled bool) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	if _, ok := m.backend.(terminalProxyBackend); !ok {
		return nil
	}
	s := m.snapshot.TerminalProxy
	if s.State == "" || s.State == "unsupported" {
		return nil
	}
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
	return m.applyTerminalProxy(true, true)
}
