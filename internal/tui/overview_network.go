package tui

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type overviewNetworkItem struct {
	title, config, status string
	configID, toggleID    string
	observation           string
	extra                 dashboardBlock
}

func overviewNetworkCard(o dashboardOptions, width int) dashboardBlock {
	inner := overviewInnerWidth(width)
	terminal, tun := overviewTerminalItem(o, inner), overviewTUNItem(o, inner)
	titleWidth := max(ansi.StringWidth(terminal.title), ansi.StringWidth(tun.title))
	configWidth := ansi.StringWidth(terminal.config)
	statusWidth := max(ansi.StringWidth(terminal.status), ansi.StringWidth(tun.status))
	// Share column starts between rows. Cap the gaps on wide dashboards so
	// the status still reads as part of its feature.
	free := max(0, inner-titleWidth-configWidth-statusWidth)
	firstGap := min(4, free/3)
	configX := titleWidth + firstGap
	statusX := configX + configWidth + min(8, free-firstGap)
	content := terminal.render(o, inner, configX, statusX)
	if o.Height >= 30 && inner >= 44 {
		content.blank()
	}
	content.append(tun.render(o, inner, configX, statusX))
	title := overviewTextBlock([]string{overviewHeading(o, o.Locale.T("dashboard.overview.network"))}, inner)
	return overviewPanel(o, width, title, content, nil)
}

func networkToggleLabels(o dashboardOptions) toggleLabels {
	tr := o.Locale.T
	return toggleLabels{On: tr("dashboard.network.on"), Off: tr("dashboard.network.off"),
		Unavailable: tr("dashboard.status.unavailable"), Unsupported: tr("dashboard.network.unsupported"),
		Unknown: tr("dashboard.status.unknown")}
}

func newOverviewNetworkItem(o dashboardOptions, title, configID, toggleID string, state toggleState, labels toggleLabels) overviewNetworkItem {
	config := overviewControlButton{id: configID, label: o.Locale.T("dashboard.network.configure"), enabled: o.Snapshot.Installed && !o.Busy && !o.StateUnavailable}
	kind := buttonText
	if o.PlainButtons {
		kind = buttonSecondary // Brackets distinguish controls without color.
	}
	item := overviewNetworkItem{title: title, config: overviewButton(o, config, kind), toggleID: toggleID}
	if config.enabled {
		item.configID = configID
	}

	label, _ := labels.resolved().face(state)
	focused := toggleID != "" && o.Focus == toggleID
	surface, marker, left, right := panel, " ", " ", " "
	if toggleID != "" {
		surface = raised
		if focused {
			marker = "›"
		}
		if o.PlainButtons {
			left, right = "[", "]"
		}
	}
	item.status = themed(marker, focus, panel, o.Dim) + themed(left, muted, surface, o.Dim) +
		renderToggle(toggleModel{state: state}, toggleOptions{Labels: labels, Width: ansi.StringWidth(label), Surface: surface,
			Focused: focused || toggleID != "" && o.Hover == toggleID, Dim: o.Dim}) + themed(right, muted, surface, o.Dim)
	return item
}

func (item overviewNetworkItem) render(o dashboardOptions, width, configX, statusX int) dashboardBlock {
	b := overviewTextBlock([]string{themed(item.title, text, panel, o.Dim)}, width)
	add := func(face, id string, column int) {
		face = cellCrop(face, width)
		w := ansi.StringWidth(face)
		y := len(b.lines) - 1
		used := ansi.StringWidth(b.lines[y])
		x := max(used, min(column, width-w))
		if x+w > width {
			b.blank()
			y, used, x = y+1, 0, 0
		}
		b.lines[y] += themed(strings.Repeat(" ", x-used), text, panel, o.Dim) + face
		if id != "" && w > 0 {
			b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: y, W: w, H: 1})
		}
	}
	add(item.config, item.configID, configX)
	add(item.status, item.toggleID, statusX)
	if item.observation != "" {
		indent := statusX + 2
		if indent+ansi.StringWidth(item.observation) > width {
			indent = min(2, max(0, width-ansi.StringWidth(item.observation)))
		}
		b.append(overviewTextBlock([]string{themed(strings.Repeat(" ", indent)+item.observation, warning, panel, o.Dim)}, width))
	}
	b.append(item.extra)
	return b
}

func overviewTerminalItem(o dashboardOptions, width int) overviewNetworkItem {
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
	row := newOverviewNetworkItem(o, tr("terminal.title"), "network.primaryConfig", id, state, labels)
	if !o.StateUnavailable && t.State == "pending" && t.Pending && t.ActivationCommand != "" {
		row.extra.append(overviewButtons(o, width, []overviewControlButton{{id: "terminal.activation", label: tr("terminal.activationAction"), enabled: !o.Busy && !o.StateUnavailable}}))
	}
	if !o.StateUnavailable && t.State == "error" {
		row.observation = tr("dashboard.network.inspectShell")
	}
	return row
}

func overviewTUNItem(o dashboardOptions, width int) overviewNetworkItem {
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
	row := newOverviewNetworkItem(o, tr("dashboard.network.tun"), "network.tunConfig", id, state, labels)
	row.observation = observed
	if s.TUNError != "" {
		row.extra.append(overviewTextBlock([]string{themed(clean(o.Locale.Detail(s.TUNError)), warning, panel, o.Dim)}, width))
	}
	return row
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
