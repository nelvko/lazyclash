package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

type terminalProxyBackend interface {
	SetTerminalProxy(context.Context, bool) error
}

func (m *Model) terminalProxyAction(enabled bool) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	backend, ok := m.backend.(terminalProxyBackend)
	if !ok {
		return nil
	}
	o := m.navigationOptions()
	id := terminalActionID(o, overviewTerminalToggle(o))
	if id == "" || enabled != (id == "terminal.enable") {
		return nil
	}
	return m.begin(m.tr("terminal.updating"), m.tr("terminal.saved"), func(ctx context.Context) error {
		return backend.SetTerminalProxy(ctx, enabled)
	}, notificationOperation{Key: "terminal-proxy", Context: m.tr("terminal.title")})
}
