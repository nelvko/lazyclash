package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

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
	control, compact := o.coreStatus()
	full, color = control.statusFace()
	if !o.PlainButtons {
		compact, _, _ = strings.Cut(full, " ")
	}
	return full, compact, color
}

// Both the read-only global badge and the interactive card use this service
// observation. A foreground operation can report progress but never a result.
func (o dashboardOptions) coreStatus() (Switch, string) {
	s, tr := o.Snapshot, o.Locale.T
	control := Switch{
		Variant: Status, OnLabel: tr("dashboard.status.running"), OffLabel: tr("dashboard.status.stopped"),
		Disabled: true,
	}
	switch {
	case o.StateUnavailable:
		control.Message, control.Color = "? "+tr("dashboard.status.unknown"), muted
		return control, "?"
	case !s.Installed:
		control.Message, control.Color = "− "+tr("dashboard.status.notInstalled"), muted
		return control, "N/A"
	}

	var transition string
	switch o.CoreAction {
	case "start":
		transition = "starting"
	case "stop":
		transition = "stopping"
	case "restart":
		transition = "restarting"
	}
	if transition == "" && s.ServiceError == "" {
		switch s.Service.State {
		case "activating":
			transition = "starting"
		case "deactivating":
			transition = "stopping"
		case "reloading":
			transition = "reloading"
		}
	}
	if transition != "" {
		control.Pending, control.Message, control.Color = true, tr("dashboard.status."+transition), warning
		return control, "WAIT"
	}
	if s.ServiceError != "" {
		label, color := coreSummary(s, o.Locale)
		control.Message, control.Color = "! "+label, color
		return control, "ERR"
	}

	control.Disabled = o.Busy || runtimeStopIssue(s, o.Locale) != ""
	switch {
	case s.Service.State == "failed":
		control.Disabled = control.Disabled || s.Service.Active
		control.Message, control.Color = "! "+tr("dashboard.status.failed"), errorColor
		return control, "ERR"
	case s.Service.State == "active" && s.Service.Active:
		control.On, control.Color = true, success
		return control, "ON"
	case s.Service.State == "inactive" && !s.Service.Active:
		control.Color = muted
		return control, "OFF"
	case s.Service.State == "unsupported":
		control.Disabled, control.Message, control.Color = true, "! "+tr("dashboard.status.unavailable"), errorColor
		return control, "N/A"
	default:
		control.Disabled, control.Message, control.Color = true, "? "+tr("dashboard.status.unknown"), muted
		return control, "?"
	}
}
