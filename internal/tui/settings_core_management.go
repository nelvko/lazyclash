package tui

import (
	"cmp"
	"strings"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func (m *Model) openCoreSettings() {
	m.browsePage("settings")
	m.settingsFromOverview = true
	m.settingsPointerFocus = false
	m.focus = "install" // The installed version row opens version details.

	o := m.navigationOptions()
	left, _, _ := settingsColumns(o, dashboardBodyWidth(o))
	for _, card := range left.cards {
		if card.ID == "settings.core" {
			// Reveal the section from its heading if it is taller than the viewport.
			card.H = min(card.H, max(1, dashboardGeometry(o).height))
			m.revealBodyTarget(card)
			break
		}
	}
	m.revealContentFocus()
}

func settingsCore(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	title := tr("settings.core")
	download := settingsRow{
		label: tr("settings.downloadSettings"), value: tr("wizard.method." + cmp.Or(o.DownloadMethod, downloadnet.MethodDirect)),
		control: settingsLink, rightAlign: true,
		actions: []overviewControlButton{{id: "network", label: settingsDetailChevron, enabled: !o.Busy}},
	}
	if o.StateUnavailable {
		return settingsSection(o, width, title, "", []settingsRow{
			{label: "Mihomo", value: tr("dashboard.status.unknown"), control: settingsReadOnly},
			download,
		})
	}
	if !s.Installed {
		return settingsSection(o, width, title, "", []settingsRow{
			{label: "Mihomo", value: tr("dashboard.status.notInstalled"), control: settingsTrailing, rightAlign: true,
				actions: []overviewControlButton{{id: "install", label: tr("dashboard.action.install"), enabled: !o.Busy, primary: true}}},
			download,
		})
	}

	version := cmp.Or(clean(s.Version), tr("dashboard.status.unknown"))
	rows := []settingsRow{
		{label: tr("wizard.installed_version"), control: settingsLink, rightAlign: true,
			actions: []overviewControlButton{{id: "install", label: version + " " + settingsDetailChevron, enabled: !o.Busy}}},
		{label: tr("core_update.check"), control: settingsLink,
			actions: []overviewControlButton{{id: "core.checkUpdate", label: settingsDetailChevron, enabled: !o.Busy}}},
		download,
	}

	if runtimeStopIssue(s, o.Locale) == "" {
		rows = append(rows, settingsAutostart(o))
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
	card := settingsSection(o, width, title, "", rows)
	card.cards = []hitRegion{{ID: "settings.core", W: width, H: len(card.lines)}}
	return card
}

const autostartToggleTarget = "autostart.toggle"

func autostartActionLabel(enabled bool) string {
	if enabled {
		return "dashboard.settings.disableAutostart"
	}
	return "dashboard.settings.enableAutostart"
}

func autostartStateKnown(s app.Snapshot) bool {
	return s.ServiceError == "" && (s.Service.EnableState == "enabled" || s.Service.EnableState == "disabled")
}

func settingsAutostart(o dashboardOptions) settingsRow {
	tr, s := o.Locale.T, o.Snapshot
	label := tr("settings.autostart")
	note := ""
	if s.Capabilities.Scope == "user" {
		switch {
		case s.ServiceError != "" || !s.Service.LingerKnown:
			note = tr("dashboard.settings.lingerUnknown")
		case !s.Service.Linger:
			note = tr("dashboard.settings.lingerDisabled")
		}
	}
	if autostartStateKnown(s) {
		row := o.switchRow(autostartToggleTarget, label)
		row.note = note
		if s.Desired.Known && s.Desired.Autostart != s.Service.Enabled {
			row.note = strings.TrimSpace(note + "\n" + tr("diagnostic.snapshot.autostartDrift"))
			row.color = warning
		}
		return row
	}
	return settingsRow{label: label, note: note, value: tr("dashboard.status.unknown"), control: settingsReadOnly, rightAlign: true}
}
