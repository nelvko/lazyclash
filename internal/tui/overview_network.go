package tui

import (
	"strconv"
	"strings"
)

type networkSectionDetail struct {
	state, warning string
	action         overviewControlButton
}

func (o dashboardOptions) selectedNetworkSection() string {
	switch o.NetworkSection {
	case "terminal", "tun", "system":
		return o.NetworkSection
	}
	if o.Snapshot.Capabilities.TUN.Supported {
		return "tun"
	}
	return "terminal"
}

func overviewNetworkCard(o dashboardOptions, width int) dashboardBlock {
	tr, inner := o.Locale.T, overviewInnerWidth(width)
	section := o.selectedNetworkSection()
	tabs := []headerAction{
		{id: "network.tab.terminal", label: tr("terminal.title"), enabled: true},
		{id: "network.tab.tun", label: tr("dashboard.network.tun"), enabled: true},
		{id: "network.tab.system", label: tr("dashboard.field.systemProxy"), enabled: true},
	}
	content := renderSegments(o, tabs, inner, panel, "network.tab."+section)
	content.blank()
	var detail networkSectionDetail
	switch section {
	case "terminal":
		detail = terminalAccessDetail(o)
	case "tun":
		detail = tunAccessDetail(o)
	case "system":
		detail.state = tr("dashboard.network.systemProxyState")
	}
	content.append(overviewTextBlock([]string{themed(tr("dashboard.network.scope."+section), muted, panel, o.Dim)}, inner))
	state := overviewHeading(o, detail.state)
	if detail.action.id != "" {
		content.append(overviewLabelControl(o, inner, state, detail.action, buttonSecondary))
	} else {
		content.append(overviewTextBlock([]string{state}, inner))
	}
	if detail.warning != "" {
		content.append(overviewTextBlock([]string{themed(detail.warning, warning, panel, o.Dim)}, inner))
	}
	if section == "terminal" && terminalActivationAvailable(o.Snapshot) {
		content.append(overviewButtons(o, inner, []overviewControlButton{
			{id: "terminal.activation", label: tr("terminal.activationAction"), enabled: !o.Busy},
		}))
	}
	content.append(overviewTextBlock([]string{
		themed(strings.Repeat("─", inner), divider, panel, o.Dim),
		themed(savedMixedPortLabel(o), muted, panel, o.Dim),
	}, inner))
	return overviewPanel(o, width, overviewTextBlock([]string{overviewHeading(o, tr("dashboard.overview.network"))}, inner), content, nil)
}

func terminalAccessDetail(o dashboardOptions) networkSectionDetail {
	tr, s := o.Locale.T, o.Snapshot
	state := s.TerminalProxy.State
	if state == "" {
		state = "unsupported"
	}
	row := networkSectionDetail{state: tr("terminal.state." + state)}
	id, available := terminalProxyControl(s)
	if available {
		label := tr("dashboard.network.enable")
		if id == "terminal.disable" {
			label = tr("dashboard.network.disable")
		}
		row.action = overviewControlButton{id: id, label: label, enabled: !o.Busy}
	} else if state != "unsupported" {
		row.warning = runtimeControlIssue(s, o.Locale)
		if !s.Installed {
			row.warning = tr("settings.afterInstall")
		} else if row.warning == "" && !s.Service.Active {
			row.warning = tr("terminal.startCore")
		}
	}
	return row
}

func tunAccessDetail(o dashboardOptions) networkSectionDetail {
	tr, s := o.Locale.T, o.Snapshot
	row := networkSectionDetail{state: tr("dashboard.control.tunHint")}
	if !s.Capabilities.TUN.Supported {
		return row
	}
	if !s.Installed {
		row.state = tr("settings.afterInstall")
		return row
	}
	id, action, saved := "tun.enable", tr("dashboard.network.enable"), tr("dashboard.control.off")
	if s.TUN {
		id, action, saved = "tun.disable", tr("dashboard.network.disable"), tr("dashboard.control.on")
	}
	issue := runtimeControlIssue(s, o.Locale)
	if issue == "" {
		row.action = overviewControlButton{id: id, label: action, enabled: !o.Busy}
	}
	row.state = tr("tun.saved_state", saved) + " · " + tr("tun.host_scope")
	var details []string
	if s.Service.Active {
		switch {
		case issue != "" || s.TUNActual == nil:
			details = append(details, tr("dashboard.overview.tunActual", tr("dashboard.status.unknown")))
		case *s.TUNActual == s.TUN:
			row.state = saved + " · " + tr("tun.host_scope")
		default:
			actual := tr("dashboard.control.off")
			if *s.TUNActual {
				actual = tr("dashboard.control.on")
			}
			details = append(details, tr("dashboard.overview.tunActual", actual))
		}
	}
	if issue != "" {
		details = append(details, issue)
	}
	if s.TUNError != "" {
		details = append(details, clean(o.Locale.Detail(s.TUNError)))
	}
	row.warning = strings.Join(details, "\n")
	return row
}

func savedMixedPortLabel(o dashboardOptions) string {
	port := "--"
	if o.Snapshot.Installed && o.Snapshot.MixedPort > 0 && o.Snapshot.MixedPort <= 65535 {
		port = strconv.Itoa(o.Snapshot.MixedPort)
	}
	return o.Locale.T("dashboard.network.mixedPort", port)
}
