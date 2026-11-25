package tui

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func overviewNetworkCard(o dashboardOptions, width int) dashboardBlock {
	inner := overviewInnerWidth(width)
	var content dashboardBlock
	// At 40 content cells the divider leaves 18–19 cells per group.
	const gapWidth = 3
	if inner >= 40 {
		leftWidth := (inner - gapWidth) / 2
		left := overviewTerminalSection(o, leftWidth)
		right := overviewTUNSection(o, inner-leftWidth-gapWidth)
		gap := themed(" │ ", divider, panel, o.Dim)
		for y := 0; y < max(len(left.lines), len(right.lines)); y++ {
			l, r := "", ""
			if y < len(left.lines) {
				l = left.lines[y]
			}
			if y < len(right.lines) {
				r = right.lines[y]
			}
			content.lines = append(content.lines, cellPad(l, leftWidth)+gap+r)
		}
		content.targets = append(content.targets, left.targets...)
		for _, hit := range right.targets {
			hit.X += leftWidth + gapWidth
			content.targets = append(content.targets, hit)
		}
	} else {
		content = overviewTerminalSection(o, inner)
		content.lines = append(content.lines, themed(strings.Repeat("─", inner), divider, panel, o.Dim))
		content.append(overviewTUNSection(o, inner))
	}
	title := overviewTextBlock([]string{overviewHeading(o, o.Locale.T("dashboard.overview.network"))}, inner)
	return overviewPanel(o, width, title, content, nil)
}

func networkToggleLabels(o dashboardOptions) toggleLabels {
	tr := o.Locale.T
	return toggleLabels{On: tr("dashboard.network.on"), Off: tr("dashboard.network.off"),
		Unavailable: tr("dashboard.status.unavailable"), Unsupported: tr("dashboard.network.unsupported"),
		Unknown: tr("dashboard.status.unknown")}
}

// A wrapped control has one keyboard stop and a mouse target covering all its lines.
func networkActionBlock(face, id string, width int) dashboardBlock {
	b := overviewTextBlock([]string{face}, width)
	if id != "" {
		b.targets = []hitRegion{{ID: id, W: min(width, ansi.StringWidth(face)), H: len(b.lines)}}
	}
	return b
}

func overviewNetworkSection(o dashboardOptions, width int, title, configID, toggleID string, state toggleState, labels toggleLabels) dashboardBlock {
	b := overviewTextBlock([]string{overviewHeading(o, title)}, width)
	label, _ := labels.resolved().face(state)
	focused := toggleID != "" && o.Focus == toggleID
	face := renderToggle(toggleModel{state: state}, toggleOptions{Labels: labels, Width: ansi.StringWidth(label), Surface: panel, Focused: focused || toggleID != "" && o.Hover == toggleID, Dim: o.Dim})
	if toggleID != "" {
		marker := "  "
		if focused {
			marker = " ›"
		}
		left, right := " ", " "
		if o.PlainButtons {
			left, right = "[", "]"
		}
		face = themed(marker+left, focus, panel, o.Dim) + face + themed(right, muted, panel, o.Dim)
	}
	b.append(networkActionBlock(face, toggleID, width))
	config := overviewControlButton{id: configID, label: o.Locale.T("dashboard.network.configure"), enabled: o.Snapshot.Installed && !o.Busy && !o.StateUnavailable}
	if !config.enabled {
		configID = ""
	}
	kind := buttonText
	if o.PlainButtons {
		kind = buttonSecondary
	}
	b.append(networkActionBlock(overviewButton(o, config, kind), configID, width))
	return b
}

func overviewTerminalSection(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	t := s.TerminalProxy
	labels := networkToggleLabels(o)
	state := toggleUnsupported
	switch t.State {
	case "off", "uninstalled":
		state = toggleOff
		if t.State == "uninstalled" {
			labels.Off = tr("dashboard.network.notSetUp")
		}
	case "on":
		state = toggleOn
	case "pending", "enabling", "disabling":
		state = togglePending
		labels.Pending = tr("dashboard.network." + t.State)
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
	section := overviewNetworkSection(o, width, tr("terminal.title"), "network.primaryConfig", id, state, labels)
	if !o.StateUnavailable && t.State == "pending" && t.Pending && t.ActivationCommand != "" {
		action := overviewControlButton{id: "terminal.activation", label: tr("terminal.activationAction"), enabled: !o.Busy}
		id := ""
		if action.enabled {
			id = action.id
		}
		section.append(networkActionBlock(overviewButton(o, action, buttonSecondary), id, width))
	}
	if !o.StateUnavailable && t.State == "error" {
		section.append(overviewTextBlock([]string{themed(tr("terminal.state.error"), warning, panel, o.Dim)}, width))
	}
	return section
}

func overviewTUNSection(o dashboardOptions, width int) dashboardBlock {
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
	section := overviewNetworkSection(o, width, tr("dashboard.network.tun"), "network.tunConfig", id, state, labels)
	if observed != "" {
		section.append(overviewTextBlock([]string{themed(observed, warning, panel, o.Dim)}, width))
	}
	if s.TUNError != "" {
		section.append(overviewTextBlock([]string{themed(clean(o.Locale.Detail(s.TUNError)), warning, panel, o.Dim)}, width))
	}
	return section
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

func savedMixedPortLabel(o dashboardOptions) string {
	port := "--"
	if o.Snapshot.Installed && o.Snapshot.MixedPort > 0 && o.Snapshot.MixedPort <= 65535 {
		port = strconv.Itoa(o.Snapshot.MixedPort)
	}
	return o.Locale.T("dashboard.network.mixedPort", port)
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
