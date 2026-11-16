package tui

import "github.com/charmbracelet/x/ansi"

// The sidebar owns the global core state. Compact layouts keep it in the title
// without taking space from page actions, shedding the repeated logo first.
func dashboardTitle(o dashboardOptions, title, page string, width int) string {
	if width <= 0 {
		return ""
	}
	if dashboardGeometry(o).sidebar {
		return cellCrop(title, width)
	}
	full, compact, color := headerCoreState(o)
	badge := full
	if o.Width < 40 || ansi.StringWidth(page)+1+ansi.StringWidth(full) > width {
		badge = compact
	}
	if ansi.StringWidth(badge) > width {
		return themed("?", color, bg, o.Dim)
	}
	room := width - ansi.StringWidth(badge) - 1
	if room <= 0 {
		return themed(badge, color, bg, o.Dim)
	}
	if ansi.StringWidth(title) > room {
		title = page
	}
	return cellCrop(title, room) + themed(" "+badge, color, bg, o.Dim)
}

func headerCoreState(o dashboardOptions) (full, compact, color string) {
	tr := o.Locale.T
	label, color := coreSummary(o.Snapshot, o.Locale)
	symbol, plain := "?", "?"
	s := o.Snapshot
	switch {
	case o.StateUnavailable:
		label, color = tr("dashboard.status.unknown"), muted
	case !s.Installed:
		symbol, plain = "−", "N/A"
	case o.CoreAction == "start" || o.CoreAction == "stop" || o.CoreAction == "restart":
		symbol, plain, color = "◌", "WAIT", warning
		switch o.CoreAction {
		case "start":
			label = tr("dashboard.status.starting")
		case "stop":
			label = tr("dashboard.status.stopping")
		case "restart":
			label = tr("dashboard.core.toggle.restarting")
		}
	case s.ServiceError != "":
		symbol, plain = "!", "ERR"
	case s.Service.State == "activating" || s.Service.State == "deactivating" || s.Service.State == "reloading":
		symbol, plain, color = "◌", "WAIT", warning
		switch s.Service.State {
		case "activating":
			label = tr("dashboard.status.starting")
		case "deactivating":
			label = tr("dashboard.status.stopping")
		case "reloading":
			label = tr("dashboard.status.reloading")
		}
	case s.Service.State == "failed":
		symbol, plain, color = "!", "ERR", errorColor
		label = tr("dashboard.status.failed")
	case s.Service.Active:
		symbol, plain = "●", "ON"
	case s.Service.State == "inactive":
		symbol, plain = "○", "OFF"
	case s.Service.State == "unsupported":
		symbol, plain = "!", "N/A"
	default:
		label, color = tr("dashboard.status.unknown"), muted
	}
	compact = symbol
	if o.PlainButtons {
		compact = plain
	}
	return symbol + " " + label, compact, color
}
