package tui

import (
	"cmp"
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const followProfile = "inherit" // Empty action IDs dismiss a selection menu.

// The same choices drive the displayed value, checked menu item and save.
func coreSettingChoices(locale i18n.Localizer, saved mihomo.GeneralSettings, id string) (key, current string, items []menuItem) {
	items = []menuItem{{ID: followProfile, Label: locale.T("settings.followProfile")}}
	switch id {
	case "settings.ipv6":
		key, current = "ipv6", saved.IPv6
	case "settings.unifiedDelay":
		key, current = "unified-delay", saved.UnifiedDelay
	case "settings.logLevel":
		for _, level := range []string{"debug", "info", "warning", "error", "silent"} {
			items = append(items, menuItem{ID: level, Label: level})
		}
		return "log-level", cmp.Or(saved.LogLevel, followProfile), items
	default:
		return "", "", nil
	}
	items = append(items,
		menuItem{ID: "on", Label: locale.T("dashboard.control.on")},
		menuItem{ID: "off", Label: locale.T("dashboard.control.off")},
	)
	return key, cmp.Or(current, followProfile), items
}

func settingsMihomo(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	description := tr("settings.allProfiles")
	if !o.Snapshot.Installed {
		description = tr("settings.afterInstall")
	}
	var rows []settingsRow
	for _, id := range []string{"settings.ipv6", "settings.unifiedDelay", "settings.logLevel"} {
		row := settingsRow{label: tr(id)}
		if o.Snapshot.Installed {
			_, current, items := coreSettingChoices(o.Locale, o.Snapshot.CoreSettings, id)
			row = settingsSelectRow(o, id, tr(id), current, items)
			if id == "settings.ipv6" && o.Snapshot.TUN {
				row.note = tr("settings.tunIPv6")
			}
		}
		rows = append(rows, row)
	}
	return settingsSection(o, width, tr("settings.mihomo"), description, rows)
}

func (m *Model) changeCoreSetting(id, choice string) tea.Cmd {
	key, current, _ := coreSettingChoices(m.locale, m.snapshot.CoreSettings, id)
	if !m.snapshot.Installed || key == "" || choice == current {
		return nil
	}
	if choice == followProfile {
		choice = ""
	}
	label := m.tr("settings.coreSaving", m.tr(id))
	backend := m.backend
	return m.begin(label, m.tr("settings.coreSaved", m.tr(id)), func(ctx context.Context) error {
		return backend.SetCoreSetting(ctx, key, choice)
	}, notificationOperation{Key: id, Context: label})
}
