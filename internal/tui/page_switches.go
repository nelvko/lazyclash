package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func boolToggle(on bool) toggleModel {
	if on {
		return toggleModel{state: toggleOn}
	}
	return toggleModel{state: toggleOff}
}

func switchGutterWidth(width int) int {
	if width < switchSlotWidth+2 {
		return 0
	}
	return 2
}

func (o dashboardOptions) switchGutter(opts toggleOptions, width int) string {
	if switchGutterWidth(width) == 0 {
		return ""
	}
	marker := "  "
	if opts.Focused {
		marker = "› "
		if ansi.StringWidth(marker) != 2 {
			marker = "> "
		}
	} else if opts.Hovered && o.Monochrome {
		marker = ". "
	}
	return themed(marker, focus, opts.Surface, o.Dim)
}

// A switch keeps one layout slot across skins and animation frames. Status
// messages retain their natural width so their explanation can wrap normally.
func (o dashboardOptions) switchSlot(id string, control toggleModel, opts toggleOptions, width int, align lipgloss.Position) string {
	opts.Style, opts.Monochrome, opts.Dim = o.SwitchStyle, o.Monochrome, o.Dim
	if width < switchSlotWidth {
		switch app.NormalizeSwitchStyle(opts.Style) {
		case app.SwitchStyleSlanted, app.SwitchStyleSlider:
			opts.Style = app.SwitchStyleText
		}
	}
	if opts.Labels.Unavailable == "" {
		opts.Labels.Unavailable = o.Locale.T("dashboard.status.unavailable")
	}
	if opts.Labels.Unsupported == "" {
		opts.Labels.Unsupported = o.Locale.T("dashboard.network.unsupported")
	}
	if opts.Labels.Unknown == "" {
		opts.Labels.Unknown = o.Locale.T("dashboard.status.unknown")
	}
	if o.Switches != nil {
		control = o.Switches.control(id, control)
	}
	face := renderToggle(control, opts)
	if !control.canToggle() && control.state != togglePending {
		return face
	}
	return lipgloss.PlaceHorizontal(min(width, switchSlotWidth), align, cellCrop(face, width))
}

func pageSwitchAction(id string) bool {
	switch id {
	case "core.toggle", "terminal.enable", "terminal.disable", "tun.enable", "tun.disable",
		"settings.ipv6", "settings.unifiedDelay", "key-hints.toggle", "autostart-enable", "autostart-disable":
		return true
	}
	return false
}

func settingsSwitchID(id string) string {
	switch id {
	case "autostart-enable", "autostart-disable":
		return "settings.autostart"
	default:
		return id
	}
}

func (o dashboardOptions) settingsToggle(id string) toggleModel {
	id = settingsSwitchID(id)
	if id == "key-hints.toggle" {
		if o.KeyHintsPending {
			return toggleModel{state: togglePending}
		}
		return boolToggle(!o.HideKeyHints)
	}
	if id == settingsSwitchID(o.PendingAction) {
		return toggleModel{state: togglePending}
	}
	if o.Busy || !o.Snapshot.Installed || o.StateUnavailable {
		return toggleModel{state: toggleUnavailable}
	}
	var value string
	switch id {
	case "settings.ipv6":
		value = o.Snapshot.CoreSettings.IPv6
	case "settings.unifiedDelay":
		value = o.Snapshot.CoreSettings.UnifiedDelay
	case "settings.autostart":
		s := o.Snapshot
		if s.ServiceError == "" && (s.Service.EnableState == "" || s.Service.EnableState == "enabled" || s.Service.EnableState == "disabled") {
			return boolToggle(s.Service.Enabled)
		}
	}
	switch value {
	case "on", "off":
		return boolToggle(value == "on")
	default:
		return toggleModel{state: toggleUnknown}
	}
}

// Only mounted controls keep animation history. The presentation uses the
// same observations as input handling and never supplies a desired value.
func (m *Model) syncSwitches(msg tea.Msg) tea.Cmd {
	if m.page != "overview" && m.page != "settings" && !m.helpOpen {
		m.switches.forget("key-hints.toggle")
		return nil
	}
	o := m.navigationOptions()
	animate := effectiveSwitchStyle(o.SwitchStyle, o.Monochrome) != app.SwitchStyleText
	if result, ok := msg.(resultMsg); ok && result.err != nil {
		animate = false
	}
	var cmds []tea.Cmd
	observe := func(id string, t toggleModel) {
		if cmd := m.switches.observe(id, t.state, animate); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if m.page == "overview" && m.inlineInstallVisible() {
		m.switches.forget("core.toggle")
		m.switches.forget("terminal.toggle")
		m.switches.forget("tun.toggle")
	} else if m.page == "overview" {
		if o.Snapshot.Installed || o.StateUnavailable {
			observe("core.toggle", overviewCoreToggle(o))
		} else {
			m.switches.forget("core.toggle")
		}
		observe("terminal.toggle", overviewTerminalToggle(o))
		observe("tun.toggle", overviewTUNToggle(o))
	}
	if m.page == "settings" {
		for _, id := range []string{"settings.ipv6", "settings.unifiedDelay", "settings.autostart"} {
			if o.Snapshot.Installed {
				observe(id, o.settingsToggle(id))
			} else {
				m.switches.forget(id)
			}
		}
	}
	if m.helpOpen || m.page == "settings" {
		observe("key-hints.toggle", o.settingsToggle("key-hints.toggle"))
	} else {
		m.switches.forget("key-hints.toggle")
	}
	if m.ports.open && m.ports.kind == "tun" {
		observe("tun.policy", boolToggle(m.ports.tunEnabled))
	} else {
		m.switches.forget("tun.policy")
	}
	return tea.Batch(cmds...)
}
