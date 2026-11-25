package tui

import (
	"net"
	"strconv"
)

func (m *Model) terminalConfigFields(d *wizardDocument) {
	status := m.snapshot.TerminalProxy
	if status.Shell != "" {
		d.pair(m.tr("network.terminalConfig.shell"), status.Shell)
	}
	state := status.State
	if state == "" {
		state = "unsupported"
	}
	d.pair(m.tr("network.terminalConfig.state"), m.tr("terminal.state."+state))
	if state == "conflict" && status.Detail != "" {
		d.line(clean(m.locale.Diagnostic(status.Detail, status.Diagnostic)))
	}
	if state == "pending" && status.Pending && status.ActivationCommand != "" {
		d.line(clean(status.ActivationCommand))
	}
	p := &m.ports
	d.pair(m.tr("ports.proxy_address"), net.JoinHostPort("127.0.0.1", strconv.Itoa(p.current.MixedPort)))
	d.field("ports.mixed", m.tr("network.terminalConfig.port"), &p.mixed, p.focus)
	d.line(m.tr("network.terminalConfig.shared"))
}
