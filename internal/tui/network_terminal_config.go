package tui

import (
	"net"
	"strconv"
)

func (m *Model) terminalConfigFields(d *wizardDocument) {
	status := m.snapshot.TerminalProxy
	state := status.State
	if state == "" {
		state = "unsupported"
	}
	color := text
	switch state {
	case "on":
		color = success
	case "pending", "enabling", "disabling":
		color = accent
	case "error":
		color = warning
	}
	d.networkValue(m.tr("network.terminalConfig.state"), m.tr("terminal.state."+state), color)
	if status.Shell != "" {
		d.networkValue(m.tr("network.terminalConfig.shell"), status.Shell, text)
	}
	if state == "pending" && status.Pending && status.ActivationCommand != "" {
		d.line(clean(status.ActivationCommand))
	}
	p := &m.ports
	d.networkValue(m.tr("network.terminalConfig.address"), net.JoinHostPort("127.0.0.1", strconv.Itoa(p.current.MixedPort)), text)
	d.space()
	const id = "ports.mixed"
	if p.focus == id && !p.mixed.Focused() {
		p.mixed.Focus()
	} else if p.focus != id && p.mixed.Focused() {
		p.mixed.Blur()
	}
	// Five port digits plus the cursor, the clear action and the field edges.
	width := 14
	p.mixed = prepareInputField(p.mixed, inputFieldTextWidth(width, 0, true), inputBackground, p.mixed.Err != nil)
	field := renderInputField(p.mixed, inputFieldOptions{
		ID: id, ClearID: id + ".clear", Width: width, Surface: inputBackground, Compact: true,
		Focused: p.focus == id, Hovered: d.hover == id, Invalid: p.mixed.Err != nil,
		ClearFocused: p.focus == id+".clear", ClearHovered: d.hover == id+".clear",
	})
	d.networkRow(m.tr("network.terminalConfig.port"), field)
	d.line(themed(m.tr("network.terminalConfig.shared"), muted, raised, false))
}
