package tui

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

func overviewAccessCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	proxy, controller := "--", "--"
	if s.Installed {
		if s.MixedPort > 0 && s.MixedPort <= 65535 {
			proxy = net.JoinHostPort("127.0.0.1", strconv.Itoa(s.MixedPort))
		}
		controller = overviewControllerAddress(s.Controller)
	}
	lines := []string{
		themed(tr("dashboard.overview.proxyAddress"), muted, panel, o.Dim),
		themed(lipgloss.NewStyle().Bold(true).Render(proxy), text, panel, o.Dim),
		themed(tr("dashboard.overview.controller", controller), muted, panel, o.Dim),
		"",
	}
	var buttons []overviewControlButton
	tun := tr("dashboard.control.tunHint")
	if s.Capabilities.TUN.Supported {
		tun = tr("settings.afterInstall")
		if s.Installed {
			state, id, label := tr("dashboard.control.off"), "tun.enable", tr("tun.enable")
			if s.TUN {
				state, id, label = tr("dashboard.control.on"), "tun.disable", tr("tun.disable")
			}
			tun = tr("tun.saved_state", state) + " · " + tr("tun.host_scope")
			buttons = append(buttons, overviewControlButton{id: id, label: label, enabled: !o.Busy && runtimeControlIssue(s, o.Locale) == ""})
		}
	}
	lines = append(lines, themed(tr("dashboard.field.tun")+" · "+tun, muted, panel, o.Dim))
	if s.Installed && s.Capabilities.TUN.Supported && s.Service.Active {
		actual := tr("dashboard.status.unknown")
		actualColor := warning
		if s.TUNActual != nil && s.ServiceError == "" && s.APIError == "" {
			actual = tr("dashboard.control.off")
			if *s.TUNActual {
				actual = tr("dashboard.control.on")
			}
			if *s.TUNActual == s.TUN {
				actualColor = muted
			}
		}
		lines = append(lines, themed(tr("dashboard.overview.tunActual", actual), actualColor, panel, o.Dim))
	}
	if s.TUNError != "" {
		lines = append(lines, themed(clean(o.Locale.Detail(s.TUNError)), warning, panel, o.Dim))
	}
	lines = append(lines, themed(tr("dashboard.overview.systemProxy"), muted, panel, o.Dim))
	buttons = append(buttons, overviewControlButton{id: "card.access", label: tr("dashboard.settings.ports"), enabled: s.Installed && !o.Busy})
	return overviewControlPanel(o, width, tr("dashboard.overview.access"), lines, buttons)
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
