package tui

import (
	"slices"
	"strings"
)

func overviewIPCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	inner := overviewInnerWidth(width)
	available := !o.StateUnavailable && overviewIPAvailable(o.Snapshot)
	state := o.ipInfo
	info := state.info
	var lines []string
	switch {
	case !available:
		lines = []string{themed(tr("dashboard.ip.unavailable"), muted, panel, o.Dim)}
	case state.failed && !state.loading:
		lines = []string{themed(tr("dashboard.ip.failed"), warning, panel, o.Dim)}
	case info.IP != "":
		var location []string
		for _, part := range []string{info.Country, info.Region, info.City} {
			if part != "" && !slices.Contains(location, part) {
				location = append(location, part)
			}
		}
		fields := []overviewField{
			{tr("dashboard.ip.address"), info.IP}, {tr("dashboard.ip.location"), strings.Join(location, " · ")},
			{tr("dashboard.ip.isp"), info.ISP}, {tr("dashboard.ip.organization"), info.Organization},
			{tr("dashboard.ip.asn"), info.ASN}, {tr("dashboard.ip.timezone"), info.Timezone},
		}
		fields = slices.DeleteFunc(fields, func(f overviewField) bool { return f.value == "" })
		columns := 1
		if width >= 44 {
			columns = 2
		}
		lines = overviewFieldGrid(o, fields, inner, columns)
	case state.loading:
		lines = []string{themed(tr("dashboard.ip.loading"), muted, panel, o.Dim)}
	default:
		lines = []string{themed(tr("dashboard.ip.waiting"), muted, panel, o.Dim)}
	}
	refresh := overviewControlButton{id: "ip.refresh", label: tr("header.refresh"), enabled: available && !o.Busy}
	if available && state.loading {
		refresh.label = tr("model.refreshing")
		// Preserve the focus of a manually refreshed card while its request finishes.
		refresh.enabled = refresh.enabled && o.Focus == refresh.id
	}
	return overviewActionPanel(o, width, tr("dashboard.overview.ipInfo"), overviewTextBlock(lines, inner), refresh)
}
