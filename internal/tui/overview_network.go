package tui

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type overviewNetworkSetting struct {
	title   string
	status  overviewControl
	config  overviewControl
	command overviewControl
	notes   []string
}

func overviewNetworkCard(o dashboardOptions, width int) dashboardBlock {
	inner := overviewInnerWidth(width)
	settings := []overviewNetworkSetting{overviewTerminalSetting(o), overviewTUNSetting(o)}
	nameWidth, controlsWidth := 0, 0
	for _, setting := range settings {
		nameWidth = max(nameWidth, ansi.StringWidth(setting.title))
		controlsWidth = max(controlsWidth, ansi.StringWidth(setting.status.face)+2+ansi.StringWidth(setting.config.face))
	}
	statusX := nameWidth + 2
	if statusX+controlsWidth > inner {
		statusX = 0
	}
	var content dashboardBlock
	for i, setting := range settings {
		if i > 0 {
			content.blank()
		}
		content.append(setting.render(inner, statusX))
	}
	title := overviewTextBlock([]string{overviewHeading(o, o.Locale.T("dashboard.overview.network"))}, inner)
	return overviewPanel(o, width, title, content, nil)
}

func (s overviewNetworkSetting) render(width, statusX int) dashboardBlock {
	b := overviewTextBlock([]string{s.title}, width)
	y := 0
	if statusX == 0 {
		y = len(b.lines)
	}
	s.status.place(&b, statusX, y, width)
	configX := max(0, width-ansi.StringWidth(s.config.face))
	if statusX+ansi.StringWidth(s.status.face)+2 > configX {
		y = len(b.lines)
	}
	s.config.place(&b, configX, y, width)
	if s.command.face != "" {
		s.command.place(&b, statusX, len(b.lines), width)
	}
	// Put observations under the visible state, past its focus gutter and padding.
	if len(s.notes) > 0 {
		indent := min(statusX+3, max(0, width-2))
		for _, line := range overviewTextBlock(s.notes, width-indent).lines {
			b.lines = append(b.lines, strings.Repeat(" ", indent)+line)
		}
	}
	return b
}

func networkAction(o dashboardOptions, id, label string, enabled bool) overviewControl {
	opts := dashboardButtonOptions(id, o, false, panel)
	opts.Disabled = !enabled
	if !o.PlainButtons {
		opts.Kind = buttonText
	}
	if !enabled {
		id = ""
	}
	return overviewControl{id: id, face: renderButton(o.Locale.T(label), opts)}
}

func (o dashboardOptions) networkSetting(title, configID, switchID string, control Switch) overviewNetworkSetting {
	return overviewNetworkSetting{
		title:  overviewHeading(o, o.Locale.T(title)),
		status: o.switchControl(switchID, control),
		config: networkAction(o, configID, "dashboard.network.configure", o.Snapshot.Installed && !o.Busy && !o.StateUnavailable),
	}
}

func (o dashboardOptions) networkSwitch() Switch {
	return Switch{Variant: Status, OnLabel: o.Locale.T("dashboard.network.on"), OffLabel: o.Locale.T("dashboard.network.off"), Disabled: o.Busy}
}

func (o dashboardOptions) terminalSwitch() Switch {
	tr, s := o.Locale.T, o.Snapshot
	t := s.TerminalProxy
	control := o.networkSwitch()
	// A saved, outstanding shell request is reversible even before the next
	// shell prompt applies it. Its message names the observed phase explicitly.
	control.On = t.Desired || t.Applied && t.State != "disabling"
	if o.StateUnavailable {
		control.Disabled, control.Message = true, tr("dashboard.status.unknown")
		return control
	}
	switch t.State {
	case "off":
		control.On = false
	case "on":
		control.On = true
	case "uninstalled", "incomplete":
		label := "dashboard.network.notSetUp"
		if t.State == "incomplete" {
			label = "dashboard.network.incomplete"
		}
		control.OffLabel = tr(label)
	case "pending", "enabling", "disabling":
		control.Message, control.Color = "◌ "+tr("dashboard.network."+t.State), accent
	case "conflict":
		control.Disabled, control.Message = true, tr("dashboard.network.conflict")
	case "error":
		control.Message = tr("dashboard.network.syncError")
		// A shell acknowledgment can fail after the saved request was read.
		// Without its revision even the direction to request is unknown.
		control.Disabled = control.Disabled || t.Revision == 0
	case "unsupported":
		control.Disabled, control.Message = true, tr("dashboard.network.unsupported")
	default:
		control.Disabled, control.Message = true, tr("dashboard.status.unknown")
	}
	control.Disabled = control.Disabled || !control.On && (!s.Installed || !s.Service.Active || s.ServiceError != "")
	if o.PendingOperation == "terminal-proxy" {
		control.Pending, control.Message, control.Color = true, tr("terminal.updating"), accent
	}
	return control
}

func overviewTerminalSetting(o dashboardOptions) overviewNetworkSetting {
	tr, t := o.Locale.T, o.Snapshot.TerminalProxy
	setting := o.networkSetting("terminal.title", "network.primaryConfig", "terminal.toggle", o.terminalSwitch())
	if !o.StateUnavailable && pendingActivationCommand(t) != "" {
		setting.command = networkAction(o, "terminal.activation", "dashboard.network.activationCommand", !o.Busy)
	}
	if !o.StateUnavailable && t.State == "error" {
		setting.notes = append(setting.notes, themed(tr("terminal.state.error"), warning, panel, o.Dim))
	}
	return setting
}

func (o dashboardOptions) tunSwitch() (Switch, string) {
	tr, s := o.Locale.T, o.Snapshot
	control := o.networkSwitch()
	switch {
	case o.StateUnavailable:
		control.Disabled, control.Message = true, tr("dashboard.status.unknown")
	case !s.Capabilities.TUN.Supported:
		control.Disabled, control.Message = true, tr("dashboard.network.unsupported")
	case !s.Installed:
		control.Disabled, control.Message = true, tr("dashboard.status.unavailable")
	default:
		control.On = s.TUN
		issue := runtimeControlIssue(s, o.Locale)
		control.Disabled = control.Disabled || issue != ""
		if o.PendingOperation == "tun" {
			control.Pending, control.Message = true, tr("tun.applying")
		}
		verified := s.Service.Active && s.TUNActual != nil && s.APIError == "" && issue == ""
		if verified && *s.TUNActual == s.TUN {
			return control, ""
		}
		control.OffLabel = tr("tun.saved_state", tr("dashboard.control.off"))
		control.OnLabel = tr("tun.saved_state", tr("dashboard.control.on"))
		if s.Service.Active {
			actual := tr("dashboard.status.unknown")
			if verified {
				actual = tr("dashboard.control.off")
				if *s.TUNActual {
					actual = tr("dashboard.control.on")
				}
			}
			return control, tr("dashboard.overview.tunActual", actual)
		}
	}
	return control, ""
}

func overviewTUNSetting(o dashboardOptions) overviewNetworkSetting {
	control, observed := o.tunSwitch()
	setting := o.networkSetting("dashboard.network.tun", "network.tunConfig", "tun.toggle", control)
	if observed != "" {
		setting.notes = append(setting.notes, themed(observed, warning, panel, o.Dim))
	}
	if problem := o.Snapshot.TUNError; !o.StateUnavailable && problem != "" {
		setting.notes = append(setting.notes, themed(clean(o.Locale.Detail(problem)), warning, panel, o.Dim))
	}
	return setting
}

// If a fresh observation removes the focused action, stay in its network row.
func (m *Model) syncNetworkActionFocus() {
	if m.page != "overview" || m.busy {
		return
	}
	o := m.navigationOptions()
	switch m.focus {
	case "terminal.activation":
		if pendingActivationCommand(o.Snapshot.TerminalProxy) != "" {
			return
		}
		m.focus = "terminal.toggle"
		fallthrough
	case "terminal.toggle":
		if o.terminalSwitch().Enabled() {
			return
		}
		m.focus = "network.primaryConfig"
	case "tun.toggle":
		if control, _ := o.tunSwitch(); control.Enabled() {
			return
		}
		m.focus = "network.tunConfig"
	default:
		return
	}
	if !o.Snapshot.Installed {
		m.focus = "overview.card.dashboard.overview.network"
	}
}

// Controller metadata contains a local address; display only its host and port,
// excluding any user info, path or query even in an incomplete observation.
func overviewControllerAddress(value string) string {
	value = clean(value)
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	endpoint, err := url.Parse(value)
	if err != nil {
		return "--"
	}
	if _, port, err := net.SplitHostPort(endpoint.Host); err == nil {
		if number, err := strconv.Atoi(port); err == nil && number > 0 && number <= 65535 {
			return endpoint.Host
		}
	}
	return "--"
}
