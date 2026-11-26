package tui

import "cmp"

func settingsCore(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	title := tr("settings.core")
	if o.StateUnavailable {
		return settingsSection(o, width, title, "", []settingsRow{
			{label: "Mihomo", value: tr("dashboard.status.unknown"), control: settingsReadOnly},
		})
	}
	if !s.Installed {
		return settingsSection(o, width, title, "", []settingsRow{
			{label: "Mihomo", value: tr("dashboard.status.notInstalled"), control: settingsTrailing, rightAlign: true,
				actions: []overviewControlButton{{id: "install", label: tr("dashboard.action.install"), enabled: !o.Busy, primary: true}}},
		})
	}

	version := cmp.Or(clean(s.Version), tr("dashboard.status.unknown"))
	rows := []settingsRow{
		{label: tr("wizard.installed_version"), control: settingsLink, rightAlign: true,
			actions: []overviewControlButton{{id: "install", label: version + " ›", enabled: !o.Busy}}},
		{label: tr("core_update.check"), control: settingsLink,
			actions: []overviewControlButton{{id: "core.checkUpdate", label: "›", enabled: !o.Busy}}},
	}

	if runtimeStopIssue(s, o.Locale) == "" {
		autostart := settingsAutostart(o)
		if s.Capabilities.Scope == "user" {
			switch {
			case s.ServiceError != "" || !s.Service.LingerKnown:
				autostart.note = tr("dashboard.settings.lingerUnknown")
			case !s.Service.Linger:
				autostart.note = tr("dashboard.settings.lingerDisabled")
			}
		}
		rows = append(rows, autostart)
	}

	serviceTitle := tr("dashboard.overview.serviceLabel")
	switch s.Capabilities.Scope {
	case "user":
		serviceTitle = tr("settings.userService")
	case "system":
		serviceTitle = tr("settings.systemService")
	}
	restart := settingsRow{label: serviceTitle, control: settingsTrailing, rightAlign: true,
		actions: []overviewControlButton{{id: "restart", label: tr("dashboard.action.restart"), enabled: !o.Busy && runtimeControlIssue(s, o.Locale) == ""}}}
	if s.GeoRestartPending {
		restart.note, restart.color = tr("settings.geoRestart"), warning
	}
	rows = append(rows, restart)
	return settingsSection(o, width, title, "", rows)
}

const autostartToggleTarget = "autostart.toggle"

func autostartActionLabel(enabled bool) string {
	if enabled {
		return "dashboard.settings.disableAutostart"
	}
	return "dashboard.settings.enableAutostart"
}

func settingsAutostart(o dashboardOptions) settingsRow {
	tr, s := o.Locale.T, o.Snapshot
	label := tr("settings.autostart")
	if s.ServiceError == "" && (s.Service.EnableState == "enabled" || s.Service.EnableState == "disabled") {
		return settingsSwitchRow(autostartToggleTarget, label, s.Service.Enabled, !o.Busy)
	}
	return settingsRow{label: label, value: tr("dashboard.status.unknown"), control: settingsTrailing,
		actions: []overviewControlButton{{id: autostartToggleTarget, label: tr(autostartActionLabel(s.Service.Enabled)), enabled: !o.Busy}}}
}
