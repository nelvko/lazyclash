package tui

import (
	"net"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/shellintegration"
)

func pendingActivationCommand(status shellintegration.Status) string {
	if status.State == "pending" && status.Pending {
		return status.ActivationCommand
	}
	return ""
}

func (m *Model) openTerminalActivation() {
	if m.busy || m.quitting || !m.snapshotReady || pendingActivationCommand(m.snapshot.TerminalProxy) == "" {
		return
	}
	p := newPortsState()
	p.kind, p.open = "activation", true
	p.focus, p.returnFocus = "terminal.copyActivation", m.focus
	m.ports = p
}

func (m *Model) terminalActivationDocument(d *wizardDocument) []wizardButton {
	buttons := []wizardButton{{"ports.cancel", m.tr("profiles.close")}}
	command := pendingActivationCommand(m.snapshot.TerminalProxy)
	if command == "" {
		d.line(m.tr("terminal.activationInactive"))
		return buttons
	}
	// Preserve spaces inside quoted paths, including at a soft line break.
	for _, line := range strings.Split(ansi.Hardwrap(clean(command), d.width, true), "\n") {
		d.lines = append(d.lines, themed(line, info, modalBackground, false))
	}
	d.space()
	d.line(m.tr("terminal.activation"))
	copyLabel := m.tr("terminal.activationCopy")
	if m.ports.step == "copy-requested" {
		copyLabel = m.tr("terminal.activationCopyRequested")
	}
	return append(buttons, wizardButton{"terminal.copyActivation", copyLabel})
}

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
	if command := pendingActivationCommand(status); command != "" {
		d.line(clean(command))
	}
	p := &m.ports
	d.pair(m.tr("ports.proxy_address"), net.JoinHostPort("127.0.0.1", strconv.Itoa(p.current.MixedPort)))
	d.field("ports.mixed", m.tr("network.terminalConfig.port"), &p.mixed, p.focus)
	d.line(m.tr("network.terminalConfig.shared"))
}
