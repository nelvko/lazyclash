package tui

import "runtime"

func overviewSystemCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	scope := clean(s.Capabilities.Scope)
	switch scope {
	case "user":
		scope = tr("dashboard.system.user")
	case "system":
		scope = tr("dashboard.system.system")
	case "":
		scope = tr("dashboard.status.unknown")
	}
	manager := scope + " · " + tr("dashboard.status.unavailable")
	autostart := tr("dashboard.status.unknown")
	if s.Capabilities.Runtime.Supported {
		manager = tr("dashboard.system.manager")
		if s.Capabilities.Scope == "system" {
			manager = tr("dashboard.system.systemManager")
		}
		if s.ServiceError == "" {
			switch s.Service.EnableState {
			case "enabled":
				autostart = tr("dashboard.status.enabled")
			case "disabled":
				autostart = tr("dashboard.status.disabled")
			default:
				if s.Service.EnableState != "" {
					autostart = clean(s.Service.EnableState)
				}
			}
		}
	}
	columns := 1
	if width >= 72 {
		columns = 3
	} else if width >= 46 {
		columns = 2
	}
	fields := []overviewField{
		{label: tr("dashboard.field.platform"), value: runtime.GOOS + " / " + runtime.GOARCH},
		{label: tr("dashboard.overview.serviceLabel"), value: manager},
		{label: tr("settings.autostart"), value: autostart},
	}
	return overviewControlPanel(o, width, tr("dashboard.overview.environment"), overviewFieldGrid(o, fields, max(1, width-4), columns), nil)
}
