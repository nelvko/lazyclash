package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

var coreLogLevelItems = []menuItem{
	{ID: "debug", Label: "debug"},
	{ID: "info", Label: "info"},
	{ID: "warning", Label: "warning"},
	{ID: "error", Label: "error"},
	{ID: "silent", Label: "silent"},
}

func settingsMihomo(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	switchRow := func(id string, on bool) settingsRow {
		return settingsSwitchRow(id, tr(id), on, !o.Busy)
	}
	prefs := s.CoreSettings
	ipv6 := switchRow("settings.ipv6", prefs.IPv6 == "on")
	if s.TUN {
		ipv6.note = tr("settings.tunIPv6")
	}
	etag := switchRow("settings.etagSupport", prefs.ETagSupport == "on")
	etag.note = tr("settings.etagHint")
	rows := []settingsRow{
		ipv6,
		switchRow("settings.unifiedDelay", prefs.UnifiedDelay == "on"),
		settingsSelectRow(o, "settings.logLevel", tr("settings.logLevel")),
		switchRow("settings.tcpConcurrent", prefs.TCPConcurrent == "on"),
		switchRow("settings.tcpKeepAlive", prefs.DisableKeepAlive == "off"),
		settingsSelectRow(o, "settings.findProcessMode", tr("settings.findProcessMode")),
		etag,
	}
	note := tr("settings.allProfiles")
	if !s.Installed {
		note = tr("settings.afterInstall")
		for i, row := range rows {
			rows[i] = settingsRow{label: row.label}
		}
	}
	return settingsSection(o, width, tr("settings.mihomo"), note, rows)
}

func (m *Model) toggleCoreSetting(id, key, current string) tea.Cmd {
	switch current {
	case "on":
		return m.saveCoreSetting(id, key, current, "off")
	case "off":
		return m.saveCoreSetting(id, key, current, "on")
	}
	return nil
}

func (m *Model) saveCoreSetting(id, key, current, value string) tea.Cmd {
	if value == current || !m.pageSelectionMenuAvailable("settings") || !m.snapshot.Installed {
		return nil
	}
	label := m.tr("settings.coreSaving", m.tr(id))
	backend := m.backend
	return m.begin(label, m.tr("settings.coreSaved", m.tr(id)), func(ctx context.Context) error {
		return backend.SetCoreSetting(ctx, key, value)
	}, notificationOperation{Key: id, Context: label})
}
