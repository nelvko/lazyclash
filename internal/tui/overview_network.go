package tui

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

func overviewNetworkCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	inner := max(1, width-4)
	label := func(value string) string {
		return themed(lipgloss.NewStyle().Bold(true).Render(value), text, panel, o.Dim)
	}
	var content dashboardBlock
	content.append(overviewLabelControl(o, inner, label(tr("dashboard.field.systemProxy")), overviewControlButton{
		id: "network.systemProxy", label: tr("dashboard.network.enable"),
	}, buttonSecondary))
	content.append(overviewTextBlock([]string{
		themed(tr("dashboard.network.systemProxyState"), muted, panel, o.Dim),
	}, inner))
	content.append(overviewTextBlock([]string{
		themed(strings.Repeat("─", inner), divider, panel, o.Dim),
	}, inner))
	terminal := s.TerminalProxy
	state := terminal.State
	if state == "" {
		state = "unsupported"
	}
	terminalButton := overviewControlButton{id: "terminal.enable", label: tr("dashboard.network.enable"), enabled: !o.Busy && state != "unsupported" && s.Installed && s.Service.Active}
	if terminal.Desired || terminal.Applied {
		terminalButton = overviewControlButton{id: "terminal.disable", label: tr("dashboard.network.disable"), enabled: !o.Busy && state != "unsupported"}
	}
	content.append(overviewLabelControl(o, inner, label(tr("terminal.title")), terminalButton, buttonSecondary))
	content.append(overviewTextBlock([]string{themed(tr("terminal.state."+state), muted, panel, o.Dim)}, inner))
	if terminal.Pending && terminal.ActivationCommand != "" {
		content.append(overviewButtons(o, inner, []overviewControlButton{{id: "terminal.activation", label: tr("terminal.activationAction"), enabled: !o.Busy}}))
	}
	content.append(overviewTextBlock([]string{themed(strings.Repeat("─", inner), divider, panel, o.Dim)}, inner))
	button := overviewControlButton{id: "tun.enable", label: tr("dashboard.network.enable")}
	tun := tr("dashboard.control.tunHint")
	var actualLine string
	if s.Capabilities.TUN.Supported {
		tun = tr("settings.afterInstall")
		if s.Installed {
			state := tr("dashboard.control.off")
			if s.TUN {
				state, button.id, button.label = tr("dashboard.control.on"), "tun.disable", tr("dashboard.network.disable")
			}
			tun = tr("tun.saved_state", state) + " · " + tr("tun.host_scope")
			button.enabled = !o.Busy && runtimeControlIssue(s, o.Locale) == ""
			if s.Service.Active {
				actual := tr("dashboard.status.unknown")
				if s.TUNActual != nil && s.APIError == "" && runtimeControlIssue(s, o.Locale) == "" {
					actual = tr("dashboard.control.off")
					if *s.TUNActual {
						actual = tr("dashboard.control.on")
					}
					if *s.TUNActual == s.TUN {
						// A confirmed match needs only one state. Drift and unknown
						// observations keep the saved policy and runtime separate.
						tun = state + " · " + tr("tun.host_scope")
					} else {
						actualLine = tr("dashboard.overview.tunActual", actual)
					}
				} else {
					actualLine = tr("dashboard.overview.tunActual", actual)
				}
			}
		}
	}
	content.append(overviewLabelControl(o, inner, label(tr("dashboard.network.tun")), button, buttonSecondary))
	content.append(overviewTextBlock([]string{themed(tun, muted, panel, o.Dim)}, inner))
	if actualLine != "" {
		content.append(overviewTextBlock([]string{themed(actualLine, warning, panel, o.Dim)}, inner))
	}
	if s.TUNError != "" {
		content.append(overviewTextBlock([]string{themed(clean(o.Locale.Detail(s.TUNError)), warning, panel, o.Dim)}, inner))
	}
	return overviewActionPanel(o, width, tr("dashboard.overview.network"), content, overviewControlButton{
		id: "card.network", label: tr("dashboard.overview.settingsAction"), enabled: s.Installed && !o.Busy,
	})
}

// Controller metadata contains a local address; display only its host and port,
// excluding any user info, path or query even in an incomplete observation.
func overviewControllerAddress(value string) string {
	value = clean(value)
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	endpoint, err := url.Parse(value)
	if err != nil {
		return "--"
	}
	if _, port, err := net.SplitHostPort(endpoint.Host); err == nil {
		if number, err := strconv.Atoi(port); err == nil && number > 0 && number <= 65535 {
			return endpoint.Host
		}
	}
	return "--"
}
