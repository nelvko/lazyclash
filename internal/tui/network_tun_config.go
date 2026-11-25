package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) canConfigureHostTUN() bool {
	return m.snapshot.Capabilities.TUN.Supported && runtimeControlIssue(m.snapshot, m.locale) == ""
}

func (m *Model) networkTUNDocument(width int) (wizardDocument, []wizardButton, string) {
	d := wizardDocument{locale: m.locale, width: width, hover: m.hover, plain: m.buttonPlain(), busy: m.busy}
	subtitle := m.tr("network.tunConfig.scope")
	buttons := []wizardButton{{"ports.cancel", m.tr("profiles.close")}}
	s := m.snapshot
	if !s.Capabilities.TUN.Supported {
		d.line(m.tr("diagnostic.runtime.tunUnavailable"))
		return d, buttons, ""
	}
	if issue := runtimeControlIssue(s, m.locale); issue != "" {
		d.line(themed(issue, warning, raised, false))
		return d, buttons, subtitle
	}

	actual, color := m.tr("dashboard.status.unknown"), muted
	if s.Service.Active {
		if s.APIError == "" && s.TUNActual != nil {
			actual = "○ " + m.tr("dashboard.control.off")
			if *s.TUNActual {
				actual, color = "● "+m.tr("dashboard.control.on"), success
			}
		}
	} else if s.Service.State == "inactive" || s.Service.State == "failed" {
		actual = m.tr("dashboard.status.stopped")
	}
	d.networkValue(m.tr("network.tunConfig.runtime"), actual, color)
	if s.TUNError != "" {
		d.line(themed(clean(m.locale.Detail(s.TUNError)), warning, raised, false))
	}
	d.networkValue(m.tr("network.tunConfig.preset"), m.tr("network.tunConfig.presetValue"), text)
	d.space()
	check := "[ ] "
	if m.ports.tunEnabled {
		check = "[x] "
	}
	button := renderButton(check+m.tr("network.tunConfig.enable"), buttonOptions{
		Kind: buttonText, Surface: raised, Compact: true, Plain: d.plain,
		Focused: m.ports.focus == "tun.policy", Hovered: d.hover == "tun.policy",
	})
	d.networkRow(m.tr("network.tunConfig.policy"), dashboardBlock{
		lines:   []string{button},
		targets: []hitRegion{{ID: "tun.policy", W: ansi.StringWidth(button), H: 1}},
	})
	if s.Service.Active {
		hintColor := muted
		if m.ports.tunEnabled != m.ports.tunOriginal && m.ports.tunEnabled != s.TUN {
			hintColor = warning
		}
		d.line(themed(m.tr("network.tunConfig.restart"), hintColor, raised, false))
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
