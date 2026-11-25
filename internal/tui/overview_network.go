package tui

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
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
	// Automatic system proxy management has no supported backend.
	system := o.switchSlot("", toggleModel{state: toggleUnsupported}, toggleOptions{Surface: panel}, inner, lipgloss.Right)
	content.blank()
	content.append(overviewLabelSlot(o, inner, themed(o.Locale.T("dashboard.field.systemProxy"), text, panel, o.Dim), system, ""))
	title := overviewTextBlock([]string{overviewHeading(o, o.Locale.T("dashboard.overview.network"))}, inner)
	return overviewPanel(o, width, title, content, nil)
}

// Wrap the face separately from its focus gutter so a focus change cannot
// reflow a label. A wrapped action still has a single tab stop and hit box.
func centeredNetworkBlock(face string, width, gutter int, id string) dashboardBlock {
	marker := ansi.Cut(face, 0, gutter)
	if gutter > 0 {
		face = ansi.Cut(face, gutter, ansi.StringWidth(face))
	}
	b := overviewTextBlock([]string{face}, max(1, width-gutter))
	left, right := width, 0
	for i, line := range b.lines {
		plain := ansi.Strip(line)
		indent := len(plain) - len(strings.TrimLeft(plain, " "))
		visible := ansi.StringWidth(strings.Trim(plain, " "))
		lineWidth := ansi.StringWidth(plain)
		x := max(0, min(width-lineWidth, (width-visible)/2-indent))
		right = max(right, x+lineWidth)
		if i == 0 && gutter > 0 {
			n := min(x, gutter)
			line = ansi.Cut(marker, gutter-n, gutter) + line
			x -= n
		}
		left = min(left, x)
		b.lines[i] = strings.Repeat(" ", x) + line
	}
	if id != "" {
		b.targets = []hitRegion{{ID: id, X: left, W: right - left, H: len(b.lines)}}
	}
	return b
}

func overviewNetworkSection(o dashboardOptions, width int, title, id, stableID string, control toggleModel) dashboardBlock {
	b := centeredNetworkBlock(overviewHeading(o, title), width, 0, "")
	opts := toggleOptions{Surface: panel,
		Focused: id != "" && o.Focus == id, Hovered: id != "" && o.Hover == id,
		Labels: toggleLabels{Unavailable: control.reason, Unknown: control.reason}}
	face := o.switchSlot(stableID, control, opts, width, lipgloss.Center)
	// A slider contains only styled spaces. Center the full face by display
	// width, without trimming its track or the leading space of " ON".
	w := ansi.StringWidth(face)
	if w <= width {
		x := (width - w + 1) / 2
		gutter := ""
		if x >= 2 {
			gutter = o.switchGutter(opts, width)
		}
		line := strings.Repeat(" ", x-ansi.StringWidth(gutter)) + gutter + face
		if id != "" {
			b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: len(b.lines), W: w, H: 1})
		}
		b.lines = append(b.lines, line)
	} else {
		b.append(centeredNetworkBlock(face, width, 0, ""))
	}
	b.blank()
	return b
}

func networkConfigActions(o dashboardOptions, width int, configID string, showCommand bool) dashboardBlock {
	button := func(id, label string, enabled bool) (string, string) {
		opts := dashboardButtonOptions(id, o, false, panel)
		opts.Disabled, opts.Compact = !enabled, showCommand
		if !o.PlainButtons {
			opts.Kind = buttonText
		}
		if !enabled {
			id = ""
		}
		return renderButton(o.Locale.T(label), opts), id
	}
	config, configID := button(configID, "dashboard.network.configure", o.Snapshot.Installed && !o.Busy && !o.StateUnavailable)
	if !showCommand {
		return centeredNetworkBlock(config, width, 2, configID)
	}
	command, commandID := button("terminal.activation", "dashboard.network.activationCommand", !o.Busy)
	configWidth, commandWidth := ansi.StringWidth(config), ansi.StringWidth(command)
	free := width - configWidth - commandWidth
	if free < 0 {
		stacked := centeredNetworkBlock(config, width, 1, configID)
		stacked.append(centeredNetworkBlock(command, width, 1, commandID))
		return stacked
	}
	left := free / 2
	row := dashboardBlock{lines: []string{strings.Repeat(" ", left) + config + command}}
	if configID != "" {
		row.targets = append(row.targets, hitRegion{ID: configID, X: left, W: configWidth, H: 1})
	}
	if commandID != "" {
		row.targets = append(row.targets, hitRegion{ID: commandID, X: left + configWidth, W: commandWidth, H: 1})
	}
	return row
}

func overviewTerminalToggle(o dashboardOptions) toggleModel {
	s := o.Snapshot
	switch {
	case o.StateUnavailable:
		return toggleModel{state: toggleUnknown}
	case o.PendingAction == "terminal-proxy":
		return toggleModel{state: togglePending}
	}
	t := s.TerminalProxy
	switch t.State {
	case "off", "uninstalled", "incomplete":
		if !s.Installed || !s.Service.Active || s.ServiceError != "" || o.Busy {
			return toggleModel{state: toggleUnavailable}
		}
		return toggleModel{state: toggleOff}
	case "on":
		if o.Busy {
			return toggleModel{state: toggleUnavailable}
		}
		return toggleModel{state: toggleOn}
	case "pending", "enabling", "disabling":
		// The request has finished. Only the calling shell can apply it. An
		// explicit cancel action can withdraw it without implying it is on.
		return toggleModel{state: toggleUnavailable, reason: o.Locale.T("dashboard.network." + t.State)}
	case "conflict":
		return toggleModel{state: toggleUnknown, reason: o.Locale.T("dashboard.network.conflict")}
	case "error":
		return toggleModel{state: toggleUnknown, reason: o.Locale.T("dashboard.network.syncError")}
	default:
		return toggleModel{state: toggleUnsupported}
	}
}

func terminalRequestWaiting(state string) bool {
	return state == "pending" || state == "enabling" || state == "disabling"
}

func terminalActionID(o dashboardOptions, control toggleModel) string {
	if o.Busy || o.StateUnavailable {
		return ""
	}
	t := o.Snapshot.TerminalProxy
	if control.canToggle() {
		if control.state == toggleOn {
			return "terminal.disable"
		}
		return "terminal.enable"
	}
	// A hook acknowledgment can report failure after the request was saved.
	// Its user may still withdraw the requested proxy or retry enabling it.
	ackError := t.State == "error" && t.HookConfirmed
	if terminalRequestWaiting(t.State) || ackError {
		if t.Desired || ackError && t.Applied {
			return "terminal.disable"
		}
		if o.Snapshot.Installed && o.Snapshot.Service.Active && o.Snapshot.ServiceError == "" {
			return "terminal.enable"
		}
	}
	return ""
}

func overviewTerminalSection(o dashboardOptions, width int) dashboardBlock {
	t := o.Snapshot.TerminalProxy
	control := overviewTerminalToggle(o)
	id := terminalActionID(o, control)
	faceID := id
	if !control.canToggle() {
		faceID = ""
	}
	section := overviewNetworkSection(o, width, o.Locale.T("terminal.title"), faceID, "terminal.toggle", control)
	if faceID == "" && id != "" {
		label := "dashboard.network.cancelRequest"
		if t.State == "error" {
			label = "navigation.controls.disableTerminal"
			if id == "terminal.enable" {
				label = "navigation.controls.enableTerminal"
			}
		}
		recovery := overviewControlButton{id: id, label: o.Locale.T(label), enabled: true}
		section.append(centeredNetworkBlock(overviewButton(o, recovery, buttonText), width, 2, id))
	}
	section.append(networkConfigActions(o, width, "network.primaryConfig", !o.StateUnavailable && pendingActivationCommand(t) != ""))
	if !o.StateUnavailable && t.State == "incomplete" {
		section.append(centeredNetworkBlock(themed(o.Locale.T("dashboard.network.incomplete"), warning, panel, o.Dim), width, 0, ""))
	}
	return section
}

func overviewTUNToggle(o dashboardOptions) toggleModel {
	s := o.Snapshot
	switch {
	case o.StateUnavailable:
		return toggleModel{state: toggleUnknown}
	case !s.Capabilities.TUN.Supported:
		return toggleModel{state: toggleUnsupported}
	case o.TUNPolicyUnknown:
		return toggleModel{state: toggleUnknown}
	case o.PendingAction == "tun":
		return toggleModel{state: togglePending}
	case !s.Installed || o.Busy || runtimeControlIssue(s, o.Locale) != "":
		return toggleModel{state: toggleUnavailable}
	case s.TUN:
		return toggleModel{state: toggleOn}
	default:
		return toggleModel{state: toggleOff}
	}
}

func overviewTUNSection(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	control := overviewTUNToggle(o)
	id := ""
	if control.canToggle() {
		id = "tun.enable"
		if s.TUN {
			id = "tun.disable"
		}
	}
	section := overviewNetworkSection(o, width, tr("dashboard.network.tun"), id, "tun.toggle", control)
	section.append(networkConfigActions(o, width, "network.tunConfig", false))
	// The switch controls the saved host policy. Only show extra state when
	// the running observation differs or cannot confirm the policy.
	verified := s.Service.Active && s.TUNActual != nil && s.APIError == "" && runtimeControlIssue(s, o.Locale) == ""
	if !o.TUNPolicyUnknown && !o.StateUnavailable && s.Installed && s.Capabilities.TUN.Supported && (!verified || *s.TUNActual != s.TUN) {
		saved := tr("dashboard.control.off")
		if s.TUN {
			saved = tr("dashboard.control.on")
		}
		section.append(centeredNetworkBlock(themed(tr("tun.saved_state", saved), muted, panel, o.Dim), width, 0, ""))
		if s.Service.Active {
			actual := tr("dashboard.status.unknown")
			if verified {
				actual = tr("dashboard.control.off")
				if *s.TUNActual {
					actual = tr("dashboard.control.on")
				}
			}
			section.append(centeredNetworkBlock(themed(tr("dashboard.overview.tunActual", actual), warning, panel, o.Dim), width, 0, ""))
		}
	}
	if s.TUNError != "" {
		section.append(centeredNetworkBlock(themed(clean(o.Locale.Detail(s.TUNError)), warning, panel, o.Dim), width, 0, ""))
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
