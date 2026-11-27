package tui

import (
	"context"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

var coreLogLevelItems = []menuItem{
	{ID: "debug", Label: "debug"},
	{ID: "info", Label: "info"},
	{ID: "warning", Label: "warning"},
	{ID: "error", Label: "error"},
	{ID: "silent", Label: "silent"},
}

type coreSettingsTab int

const (
	accessTab coreSettingsTab = iota
	networkTab
	resourcesTab
)

var coreSettingsTabs = [...]string{"settings.tab.access", "settings.tab.network", "settings.tab.resources"}

func (m *Model) selectSettingsTab(id string) {
	for tab, name := range coreSettingsTabs {
		if name == id {
			m.settingsTab, m.focus = coreSettingsTab(tab), id
			m.revealContentFocus()
			return
		}
	}
}

func (m *Model) settingsTabNavigation(key string) bool {
	if m.page != "settings" || m.sidebarOpen || m.focus != coreSettingsTabs[m.settingsTab] {
		return false
	}
	count := len(coreSettingsTabs)
	next := int(m.settingsTab)
	switch key {
	case "left", "h":
		next = (next + count - 1) % count
	case "right", "l":
		next = (next + 1) % count
	case "home":
		next = 0
	case "end":
		next = count - 1
	default:
		return false
	}
	m.selectSettingsTab(coreSettingsTabs[next])
	return true
}

// Lay tabs out from the left at their label widths. The underline identifies
// the open pane independently of the pointer and keyboard focus.
func settingsTabs(o dashboardOptions, width, inset int) dashboardBlock {
	const minGap, maxGap = 2, 6
	inner := width - 2*inset
	// Borrow one cell from the card inset so the first label still aligns
	// with the section heading. Between labels, use the existing gap.
	side := min(1, inset)
	pad := strings.Repeat(" ", side)
	var labels [len(coreSettingsTabs)]string
	labelWidth := 0
	for i, id := range coreSettingsTabs {
		labels[i] = o.Locale.T(id)
		labelWidth += ansi.StringWidth(labels[i])
	}
	gaps := len(labels) - 1
	gap := minGap
	if labelWidth+minGap*gaps <= inner {
		gap = min(maxGap, (inner-labelWidth)/gaps)
	} else {
		for i, id := range coreSettingsTabs {
			labels[i] = o.Locale.T(id + ".compact")
		}
	}

	var block dashboardBlock
	x := inset
	for i, id := range coreSettingsTabs {
		label := cellCrop(labels[i], inner)
		w := ansi.StringWidth(label)
		if x > inset && x+gap+w > width-inset {
			x = inset
		}
		if x == inset {
			rule := strings.Repeat(" ", inset) + themed(strings.Repeat("─", inner), divider, panel, o.Dim)
			block.lines = append(block.lines, strings.Repeat(" ", inset-side), rule)
		} else {
			block.lines[len(block.lines)-2] += strings.Repeat(" ", gap-2*side)
			x += gap
		}
		y := len(block.lines) - 2
		active := i == int(o.settingsTab)
		ink := muted
		if active {
			ink = accent
			mark := themed(strings.Repeat("━", w), accent, panel, o.Dim)
			block.lines[y+1] = ansi.Cut(block.lines[y+1], 0, x) + mark + ansi.Cut(block.lines[y+1], x+w, width)
			if o.Focus == id {
				label = lipgloss.NewStyle().Bold(true).Render(label)
			}
		}
		block.lines[y] += themed(pad+label+pad, ink, panel, o.Dim)
		if w > 0 && (active || o.settingsMouseTabs) {
			block.targets = append(block.targets, hitRegion{ID: id, X: x - side, Y: y, W: w + 2*side, H: 2})
		}
		x += w
	}
	for i, line := range block.lines {
		block.lines[i] = themed(cellPad(line, width), text, panel, o.Dim)
	}
	return block
}

func settingsMihomo(o dashboardOptions, width int) dashboardBlock {
	title, note := o.Locale.T("settings.mihomo"), ""
	if !o.Snapshot.Installed {
		note = o.Locale.T("settings.afterInstall")
	}
	// Match the tallest pane at this width, including any wrapping. Copy hit
	// regions from the selected pane only.
	var selected dashboardBlock
	height := 0
	for tab := range coreSettingsTabs {
		card := settingsSection(o, width, title, note, coreSettingsRows(o, coreSettingsTab(tab)))
		height = max(height, len(card.lines))
		if coreSettingsTab(tab) == o.settingsTab {
			selected = card
		}
	}
	for len(selected.lines) < height {
		selected.lines = append(selected.lines, selected.lines[0])
	}
	return selected
}

func coreSettingsRows(o dashboardOptions, tab coreSettingsTab) []settingsRow {
	tr, s := o.Locale.T, o.Snapshot
	p := s.CoreSettings
	enabled := s.Installed && !o.Busy
	group := func(id string) settingsRow { return settingsRow{label: tr(id), control: settingsGroup} }
	toggle := func(id string, on bool) settingsRow {
		return settingsSwitchRow(id, tr(id), on, enabled)
	}
	toggleWithTip := func(id string, on bool) settingsRow {
		row := toggle(id, on)
		row.tip = tooltipBinding{owner: id, text: tr(id + ".tooltip")}
		return row
	}
	selectRow := func(id string) settingsRow {
		row := settingsSelectRow(o, id, tr(id))
		row.actions[0].enabled = enabled
		return row
	}
	link := func(id, label, value string, available bool) settingsRow {
		return settingsRow{label: label, control: settingsLink, actions: []overviewControlButton{{id: id, label: value, enabled: enabled && available}}}
	}
	number := func(id, value, unit string, available bool) settingsRow {
		if value == "" || value == "default" {
			value = tr("settings.coreDefault")
		} else {
			value = tr(unit, value)
		}
		return link(id, "  "+tr(id), value, available)
	}
	rows := []settingsRow{{control: settingsTabStrip}}
	switch tab {
	case accessTab:
		runtime := s.RuntimeSettings
		listeners := runtime.SupportedListeners()
		port, count := 0, 0
		for _, p := range []int{runtime.MixedPort, listeners.HTTPPort, listeners.SOCKSPort, listeners.RedirPort, listeners.TProxyPort} {
			if p > 0 && p <= 65535 {
				port, count = p, count+1
			}
		}
		summary := tr("settings.portsDisabled")
		if count == 1 {
			summary = strconv.Itoa(port)
		}
		if count > 1 {
			summary = tr("settings.portCount", count)
		}
		rows = append(rows, group("settings.proxyInbound"),
			link("runtime-settings", tr("settings.proxyPorts"), summary+" "+settingsDetailChevron, true),
			toggleWithTip("settings.allowLAN", runtime.AllowLAN),
			link("settings.bindAddress", tr("settings.bindAddress"), bindAddressSummary(runtime, o.Locale)+" "+settingsDetailChevron, true),
			link("settings.accessRules", tr("settings.accessRules"), accessRulesSummary(runtime, o.Locale), true))
		var users string
		switch runtime.ProxyUserCount {
		case 0:
			users = tr("settings.authNotConfigured")
		case 1:
			users = tr("settings.authOneUser")
		default:
			users = tr("settings.authUserCount", runtime.ProxyUserCount)
		}
		rows = append(rows, link("settings.proxyAuth", tr("settings.proxyAuth"), users+" "+settingsDetailChevron, true))
		userinfo := ""
		if runtime.ProxyUserCount > 0 {
			userinfo = tr("settings.proxyUserinfo")
		}
		if address := runtime.ProxyAddress(userinfo); address != "" {
			row := settingsRow{label: tr("settings.proxyAddress"), value: localizedProxyAddress(address, o.Locale), control: settingsReadOnly}
			if runtime.ProxyUserCount > 0 {
				row.control = settingsAddress
				row.actions = []overviewControlButton{{id: "settings.copyProxyAddress", label: tr("access.copyAddress"), enabled: enabled}}
			}
			rows = append(rows, row)
		}
		rows = append(rows, group("settings.controlAPI"), link("settings.externalControl", tr("settings.externalControl"), settingsDetailChevron, true))
	case networkTab:
		keepAlive := p.DisableKeepAlive == "off"
		rows = append(rows, group("settings.networkGroup"), toggle("settings.ipv6", p.IPv6 == "on"),
			toggleWithTip("settings.tcpConcurrent", p.TCPConcurrent == "on"), toggle("settings.tcpKeepAlive", keepAlive),
			number("settings.keepAliveIdle", p.KeepAliveIdle, "settings.seconds", keepAlive),
			number("settings.keepAliveInterval", p.KeepAliveInterval, "settings.seconds", keepAlive),
			toggleWithTip("settings.unifiedDelay", p.UnifiedDelay == "on"), group("settings.runtimeGroup"),
			selectRow("settings.logLevel"), selectRow("settings.findProcessMode"))
	case resourcesTab:
		rows = append(rows, group("settings.geoGroup"), toggle("settings.geoAutoUpdate", p.GeoAutoUpdate == "on"),
			number("settings.geoUpdateInterval", p.GeoUpdateInterval, "settings.hours", p.GeoAutoUpdate == "on"),
			selectRow("settings.geodataLoader"), geoMaintenanceRow(o),
			group("settings.downloadGroup"), toggle("settings.etagSupport", p.ETagSupport == "on"),
			toggle("settings.storeFakeIP", p.StoreFakeIP == "on"))
	}
	for i, row := range rows {
		if !s.Installed && row.control != settingsTabStrip && row.control != settingsGroup {
			rows[i] = settingsRow{label: row.label, tip: row.tip, value: "—", control: settingsReadOnly}
		}
		rows[i].rightAlign = true
	}
	return rows
}

func localizedProxyAddress(address string, locale i18n.Localizer) string {
	return strings.NewReplacer("<LAN IP>", locale.T("settings.lanIP"), "<LAN IPv4>", locale.T("settings.lanIPv4"), "<LAN IPv6>", locale.T("settings.lanIPv6")).Replace(address)
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
	}, notificationOperation{Key: id, Context: m.tr(id)})
}
