package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) canConfigureHostTUN() bool {
	return m.snapshot.Capabilities.TUN.Supported && m.tunConfigIssue() == ""
}

func (m *Model) tunConfigIssue() string {
	if m.tunPolicyUnknown {
		return m.tr("notification.state_unknown")
	}
	return runtimeControlIssue(m.snapshot, m.locale)
}

func (m *Model) networkTUNDocument(width int) (wizardDocument, []wizardButton, string) {
	d := wizardDocument{locale: m.locale, width: width, hover: m.hover, plain: m.buttonPlain(), busy: m.busy}
	subtitle := m.tr("network.tunConfig.scope")
	buttons := []wizardButton{{"ports.cancel", m.tr("profiles.close")}}
	s := m.snapshot
	if !s.Capabilities.TUN.Supported {
		d.line(m.tr("diagnostic.runtime.tunUnavailable"))
		return d, buttons, subtitle
	}
	if issue := m.tunConfigIssue(); issue != "" {
		d.line(themed(issue, warning, raised, false))
		return d, buttons, subtitle
	}

	actual := m.tr("dashboard.status.unknown")
	if s.Service.Active {
		if s.APIError == "" && s.TUNActual != nil {
			actual = m.tr("dashboard.control.off")
			if *s.TUNActual {
				actual = m.tr("dashboard.control.on")
			}
		}
	} else if s.Service.State == "inactive" || s.Service.State == "failed" {
		actual = m.tr("dashboard.status.stopped")
	}
	d.pair(m.tr("network.tunConfig.runtime"), actual)
	if s.TUNError != "" {
		d.line(themed(clean(m.locale.Detail(s.TUNError)), warning, raised, false))
	}
	d.space()
	o := m.navigationOptions()
	o.Focus, o.Dim = m.ports.focus, false
	control := boolToggle(m.ports.tunEnabled)
	const id = "tun.policy"
	opts := toggleOptions{Surface: raised, Focused: o.Focus == id, Hovered: o.Hover == id}
	face := o.switchSlot(id, control, opts, width, lipgloss.Left)
	gutter := o.switchGutter(opts, width)
	d.targets = append(d.targets, hitRegion{ID: id, X: ansi.StringWidth(gutter), Y: len(d.lines), W: ansi.StringWidth(face), H: 1})
	controlLine := gutter + face
	label := themed(m.tr("network.tunConfig.policy"), text, raised, false)
	if ansi.StringWidth(controlLine)+2+ansi.StringWidth(label) <= width {
		d.lines = append(d.lines, controlLine+"  "+label)
	} else {
		d.lines = append(d.lines, controlLine)
		d.line(label)
	}
	d.line(m.tr("network.tunConfig.preset"))
	if s.Service.Active {
		d.line(m.tr("network.tunConfig.restart"))
	}
	buttons = []wizardButton{{"ports.cancel", m.tr("ports.cancel")}, {"tun.save", m.tr("profile_settings.save")}}
	return d, buttons, subtitle
}

func (m *Model) networkTUNActivate(id string) tea.Cmd {
	if !m.ports.open || m.ports.kind != "tun" || m.busy || m.quitting {
		return nil
	}
	if !m.canConfigureHostTUN() {
		return nil
	}
	switch id {
	case "tun.policy":
		m.ports.tunEnabled = !m.ports.tunEnabled
	case "tun.save":
		enabled := m.ports.tunEnabled
		changed := enabled != m.ports.tunOriginal && enabled != m.snapshot.TUN
		m.closePorts()
		if changed {
			return m.tunAction(enabled)
		}
	}
	return nil
}
