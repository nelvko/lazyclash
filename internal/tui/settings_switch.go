package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// The UI names Keep-alive positively, while Mihomo stores disable-keep-alive.
type coreBoolSetting struct {
	key, current string
	inverted     bool
}

func coreBool(s mihomo.GeneralSettings, id string) coreBoolSetting {
	switch id {
	case "settings.geoAutoUpdate":
		return coreBoolSetting{"geo-auto-update", s.GeoAutoUpdate, false}
	case "settings.storeFakeIP":
		return coreBoolSetting{"profile.store-fake-ip", s.StoreFakeIP, false}
	case "settings.ipv6":
		return coreBoolSetting{"ipv6", s.IPv6, false}
	case "settings.tcpKeepAlive":
		return coreBoolSetting{"disable-keep-alive", s.DisableKeepAlive, true}
	case "settings.unifiedDelay":
		return coreBoolSetting{"unified-delay", s.UnifiedDelay, false}
	case "settings.tcpConcurrent":
		return coreBoolSetting{"tcp-concurrent", s.TCPConcurrent, false}
	case "settings.etagSupport":
		return coreBoolSetting{"etag-support", s.ETagSupport, false}
	}
	return coreBoolSetting{}
}

func (o dashboardOptions) settingsSwitch(id string) (Switch, bool) {
	if id == "key-hints.toggle" {
		return Switch{On: !o.HideKeyHints, Pending: o.KeyHintsPending}, true
	}
	s := o.Snapshot
	control := Switch{Disabled: o.Busy || o.StateUnavailable || !s.Installed, Pending: o.PendingOperation == id}
	switch id {
	case autostartToggleTarget:
		control.On = s.Service.Enabled
		control.Disabled = control.Disabled || runtimeStopIssue(s, o.Locale) != "" || !autostartStateKnown(s)
	case "settings.allowLAN":
		control.On = s.RuntimeSettings.AllowLAN
	default:
		setting := coreBool(s.CoreSettings, id)
		if setting.key == "" {
			return Switch{}, false
		}
		control.On = (setting.current == "on") != setting.inverted
		control.Disabled = control.Disabled || (setting.current != "on" && setting.current != "off")
	}
	return control, true
}

// The event already carries the requested UI value. Business commands translate
// it to the persisted representation and publish a fresh observation on return.
func (m *Model) applySettingsSwitch(id string, on bool) tea.Cmd {
	switch id {
	case "key-hints.toggle":
		return m.saveKeyHints(!on)
	case autostartToggleTarget:
		return m.begin(m.tr("model.updating_autostart"), m.tr("model.autostart_saved"), func(ctx context.Context) error {
			return m.backend.SetAutostart(ctx, on)
		}, notificationOperation{Key: id, Context: m.tr("settings.autostart")})
	case "settings.allowLAN":
		return m.setAccessLAN(on)
	}
	setting := coreBool(m.snapshot.CoreSettings, id)
	value := "off"
	if on != setting.inverted {
		value = "on"
	}
	return m.saveCoreSetting(id, setting.key, setting.current, value)
}
