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
	if !s.Installed {
		return settingsSection(o, width, tr("settings.mihomo"), tr("settings.afterInstall"), []settingsRow{
			{label: tr("settings.ipv6")}, {label: tr("settings.unifiedDelay")}, {label: tr("settings.logLevel")},
		})
	}
	ipv6 := settingsSwitchRow(o, "settings.ipv6", tr("settings.ipv6"))
	if s.TUN {
		ipv6.note = tr("settings.tunIPv6")
	}
	return settingsSection(o, width, tr("settings.mihomo"), tr("settings.allProfiles"), []settingsRow{
		ipv6,
		settingsSwitchRow(o, "settings.unifiedDelay", tr("settings.unifiedDelay")),
		settingsSelectRow(o, "settings.logLevel", tr("settings.logLevel"), s.CoreSettings.LogLevel, coreLogLevelItems),
	})
}

func (m *Model) toggleCoreSetting(id, key, current string) tea.Cmd {
	switch current {
	case "on":
		return m.saveCoreSetting(id, key, "off")
	case "off":
		return m.saveCoreSetting(id, key, "on")
	}
	return nil
}

func (m *Model) saveCoreSetting(id, key, value string) tea.Cmd {
	if !m.pageSelectionMenuAvailable("settings") || !m.snapshot.Installed {
		return nil
	}
	label := m.tr("settings.coreSaving", m.tr(id))
	backend := m.backend
	return m.begin(label, m.tr("settings.coreSaved", m.tr(id)), func(ctx context.Context) error {
		return backend.SetCoreSetting(ctx, key, value)
	}, notificationOperation{Key: id, Context: label})
}
