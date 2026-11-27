package tui

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type networkControl struct {
	face, id string
}

type overviewNetworkSetting struct {
	title   string
	status  networkControl
	config  networkControl
	command networkControl
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

// Place a face and its hit region together. Very narrow cards wrap the label
// independently of the two-cell focus marker, so tabbing never reflows a row.
func (c networkControl) place(b *dashboardBlock, x, y, width int) {
	w := ansi.StringWidth(c.face)
	lines := []string{c.face}
	if w > width-x {
		w = width - x
		gutter := min(2, max(0, w-2))
		marker := ansi.Cut(c.face, 2-gutter, 2)
		lines = overviewTextBlock([]string{ansi.Cut(c.face, 2, ansi.StringWidth(c.face))}, w-gutter).lines
		for i, line := range lines {
			lines[i] = marker + line
			marker = strings.Repeat(" ", gutter)
		}
	}
	for i, line := range lines {
		if y+i == len(b.lines) {
			b.blank()
		}
		b.lines[y+i] = cellPad(b.lines[y+i], x) + line
	}
	if c.id != "" {
		b.targets = append(b.targets, hitRegion{ID: c.id, X: x, Y: y, W: w, H: len(lines)})
	}
}

func networkToggleLabels(o dashboardOptions) toggleLabels {
	tr := o.Locale.T
	return toggleLabels{On: tr("dashboard.network.on"), Off: tr("dashboard.network.off"),
		Unavailable: tr("dashboard.status.unavailable"), Unsupported: tr("dashboard.network.unsupported"),
		Unknown: tr("dashboard.status.unknown")}
}

func networkStatus(o dashboardOptions, id string, state toggleState, labels toggleLabels) networkControl {
	label, _ := labels.resolved().face(state)
	focused := id != "" && o.Focus == id
	face := renderToggle(toggleModel{state: state}, toggleOptions{Labels: labels, Width: ansi.StringWidth(label), Surface: panel, Focused: focused || id != "" && o.Hover == id, Dim: o.Dim})
	marker, left, right := "  ", " ", " "
	if focused {
		marker = " ›"
	}
	if id != "" && o.PlainButtons {
		left, right = "[", "]"
	}
	return networkControl{id: id, face: themed(marker+left, focus, panel, o.Dim) + face + themed(right, muted, panel, o.Dim)}
}

func networkAction(o dashboardOptions, id, label string, enabled bool) networkControl {
	opts := dashboardButtonOptions(id, o, false, panel)
	opts.Disabled = !enabled
	if !o.PlainButtons {
		opts.Kind = buttonText
	}
	if !enabled {
		id = ""
	}
	return networkControl{id: id, face: renderButton(o.Locale.T(label), opts)}
}

func networkSetting(o dashboardOptions, title, configID, toggleID string, state toggleState, labels toggleLabels) overviewNetworkSetting {
	return overviewNetworkSetting{
		title:  overviewHeading(o, o.Locale.T(title)),
		status: networkStatus(o, toggleID, state, labels),
		config: networkAction(o, configID, "dashboard.network.configure", o.Snapshot.Installed && !o.Busy && !o.StateUnavailable),
	}
}

func overviewTerminalSetting(o dashboardOptions) overviewNetworkSetting {
	tr, s := o.Locale.T, o.Snapshot
	t := s.TerminalProxy
	labels := networkToggleLabels(o)
	state := toggleUnsupported
	switch t.State {
	case "off", "uninstalled", "incomplete":
		state = toggleOff
		if t.State == "uninstalled" {
			labels.Off = tr("dashboard.network.notSetUp")
		} else if t.State == "incomplete" {
			labels.Off = tr("dashboard.network.incomplete")
		}
	case "on":
		state = toggleOn
	case "pending", "enabling", "disabling":
		state = togglePending
		labels.Pending = tr("dashboard.network." + t.State)
	case "conflict":
		state = toggleUnknown
		labels.Unknown = tr("dashboard.network.conflict")
	case "error":
		state = toggleUnknown
		labels.Unknown = tr("dashboard.network.syncError")
	}
	if o.StateUnavailable {
		state = toggleUnknown
		labels.Unknown = tr("dashboard.status.unknown")
	}
	id := ""
	if state != toggleUnsupported && !o.Busy && !o.StateUnavailable {
		if t.Desired || t.Applied && t.State != "disabling" {
			id = "terminal.disable"
		} else if s.Installed && s.Service.Active {
			id = "terminal.enable"
		}
	}
	setting := networkSetting(o, "terminal.title", "network.primaryConfig", id, state, labels)
	if !o.StateUnavailable && pendingActivationCommand(t) != "" {
		setting.command = networkAction(o, "terminal.activation", "dashboard.network.activationCommand", !o.Busy)
	}
	if !o.StateUnavailable && t.State == "error" {
		setting.notes = append(setting.notes, themed(tr("terminal.state.error"), warning, panel, o.Dim))
	}
	return setting
}

func overviewTUNSetting(o dashboardOptions) overviewNetworkSetting {
	tr, s := o.Locale.T, o.Snapshot
	labels := networkToggleLabels(o)
	state, id, observed := toggleUnsupported, "", ""
	switch {
	case o.StateUnavailable:
		state = toggleUnknown
	case !s.Capabilities.TUN.Supported:
	case !s.Installed:
		state = toggleUnavailable
	default:
		state = toggleOff
		if s.TUN {
			state = toggleOn
		}
		issue := runtimeControlIssue(s, o.Locale)
		if !o.Busy && issue == "" {
			id = "tun.enable"
			if s.TUN {
				id = "tun.disable"
			}
		}
		verified := s.Service.Active && s.TUNActual != nil && s.APIError == "" && issue == ""
		if !verified || *s.TUNActual != s.TUN {
			labels.Off = tr("tun.saved_state", tr("dashboard.control.off"))
			labels.On = tr("tun.saved_state", tr("dashboard.control.on"))
			if s.Service.Active {
				actual := tr("dashboard.status.unknown")
				if verified {
					actual = tr("dashboard.control.off")
					if *s.TUNActual {
						actual = tr("dashboard.control.on")
					}
				}
				observed = tr("dashboard.overview.tunActual", actual)
			}
		}
	}
	setting := networkSetting(o, "dashboard.network.tun", "network.tunConfig", id, state, labels)
	if observed != "" {
		setting.notes = append(setting.notes, themed(observed, warning, panel, o.Dim))
	}
	if s.TUNError != "" {
		setting.notes = append(setting.notes, themed(clean(o.Locale.Detail(s.TUNError)), warning, panel, o.Dim))
	}
	return setting
}

// The target names carry explicit enable/disable intent. After a refreshed
// observation, keep keyboard focus on that same row's current control.
func (m *Model) syncNetworkActionFocus() {
	if m.page != "overview" || m.busy {
		return
	}
	var prefix, fallback string
	switch m.focus {
	case "terminal.enable", "terminal.disable", "terminal.activation":
		prefix, fallback = "terminal.", "network.primaryConfig"
	case "tun.enable", "tun.disable":
		prefix, fallback = "tun.", "network.tunConfig"
	default:
		return
	}
	o := m.navigationOptions()
	for _, target := range overviewNetworkCard(o, dashboardBodyWidth(o)).targets {
		if target.ID == m.focus {
			return
		}
		if strings.HasPrefix(target.ID, prefix) && target.ID != "terminal.activation" {
			fallback = target.ID
		}
	}
	if !o.Snapshot.Installed {
		fallback = "overview.card.dashboard.overview.network"
	}
	m.focus = fallback
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
