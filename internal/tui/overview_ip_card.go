package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func overviewIPCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	inner := overviewInnerWidth(width)
	available := !o.StateUnavailable && overviewProxyAvailable(o.Snapshot)
	state := o.ipInfo
	info := state.info
	var updated string
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
			{label: tr("dashboard.ip.address"), value: info.IP, color: overviewAddressColor},
			{label: tr("dashboard.ip.location"), value: cmp.Or(strings.Join(location, " · "), "—")},
			{label: tr("dashboard.ip.isp"), value: cmp.Or(info.ISP, "—")},
			{label: tr("dashboard.ip.asn"), value: cmp.Or(info.ASN, "—")},
		}
		columns := 1
		if width >= 44 {
			columns = 2
		}
		lines = overviewFieldGrid(o, fields, inner, columns)
		if !state.failed {
			updated = profileAge(state.checkedAt, o.Now, o.Locale)
		}
	case state.loading:
		lines = []string{themed(tr("dashboard.ip.loading"), muted, panel, o.Dim)}
	default:
		lines = []string{themed(tr("dashboard.ip.waiting"), muted, panel, o.Dim)}
	}
	refresh := overviewControlButton{id: "ip.refresh", label: "↻", enabled: available && !o.Busy}
	if available && state.loading {
		refresh.label = "…"
		// Preserve the focus of a manually refreshed card while its request finishes.
		refresh.enabled = refresh.enabled && o.Focus == refresh.id
	}
	heading := overviewIPHeading(o, inner, updated, refresh)
	return overviewPanel(o, width, heading, overviewTextBlock(lines, inner), nil)
}

func overviewIPHeading(o dashboardOptions, width int, updated string, refresh overviewControlButton) dashboardBlock {
	title := overviewHeading(o, o.Locale.T("dashboard.overview.ipInfo"))
	face := overviewButton(o, refresh, buttonIcon)
	id := ""
	if refresh.enabled {
		id = refresh.id
	}
	if updated == "" {
		return overviewLabelSlot(o, width, title, face, id, true)
	}
	stamp := themed(updated, muted, panel, o.Dim)
	right := stamp + face
	if ansi.StringWidth(right) > width {
		b := overviewLabelSlot(o, width, title, face, id, true)
		b.append(overviewTextBlock([]string{stamp}, width))
		return b
	}
	// Right-align the timestamp and refresh as a group, but only the button is clickable.
	b := overviewLabelSlot(o, width, title, right, "", true)
	if refresh.enabled {
		w := ansi.StringWidth(face)
		b.targets = append(b.targets, hitRegion{ID: id, X: width - w, Y: len(b.lines) - 1, W: w, H: 1})
	}
	return b
}
