package tui

import "strings"

func (m *Model) controlActionHelp(label string) string {
	if m.page == "settings" {
		width := dashboardBodyWidth(m.dashboardOptions())
		if width >= settingsColumnsMinWidth {
			return m.tr("settings.columnsActionHelp", label)
		}
		return m.tr("settings.actionHelp", label)
	}
	return m.tr("navigation.controls.activate", label)
}

func (m *Model) profilesContextHelp() string {
	if m.focus == "profiles.url" {
		if !m.profilesUI.pasteDeadline.IsZero() {
			return m.tr("profiles.clipboard_waiting")
		}
		if m.profilesUI.importSource() == "" {
			return m.tr("navigation.profiles.inputEmpty")
		}
		return m.tr("navigation.profiles.importing")
	}
	base := m.tr("navigation.profiles.controls")

	action := ""
	switch m.focus {
	case "profiles.url.clear":
		action = m.tr("navigation.controls.clear")
	case "profiles.url.paste":
		action = m.tr("profiles.paste")
	case "profiles.add":
		action = m.tr("profiles.add")
	case "profiles.create":
		action = m.tr("profiles.create")
	case "profiles.update-all":
		if len(m.profiles()) > 0 {
			return m.tr("navigation.updateProfiles")
		}
	case "page.refresh":
		return m.tr("navigation.refresh")
	}
	profiles := m.profiles()
	if _, ok := profileActionIndex(m.focus, "profile.", len(profiles)); ok {
		return m.tr("navigation.profiles")
	}
	for _, item := range []struct{ prefix, label string }{
		{"profiles.menu.", "navigation.controls.menu"}, {"profiles.refresh.", "profiles.update"},
	} {
		if _, ok := profileActionIndex(m.focus, item.prefix, len(profiles)); ok {
			action = m.tr(item.label)
			break
		}
	}
	if action == "" {
		return base
	}
	return "Enter " + action + " · " + base
}

func (m *Model) overviewWebsiteHelp() string {
	if d := m.websiteDrag; d.active() {
		if d.neighbor.URL != "" && d.neighbor != d.source {
			key := "dashboard.website.dropBefore"
			if d.after {
				key = "dashboard.website.dropAfter"
			}
			return m.tr(key, clean(d.neighbor.Name))
		}
		return m.tr("dashboard.website.dragging")
	}
	for _, id := range []string{m.hover, m.focus} {
		switch id {
		case "website.add", "website.prev", "website.next":
			return m.controlActionHelp(m.tr("dashboard." + id))
		case "website.all":
			if m.navigationOptions().websiteTestsAvailable() && m.websiteTests.canTestAll(m.websites) {
				return m.controlActionHelp(m.tr("dashboard.website.all"))
			}
			return m.tr("navigation.controls.browse")
		}
		if i := m.overviewWebsiteIndex(id); i >= 0 {
			site := m.websites[i]
			if id == overviewWebsiteDragID(site) {
				if len(m.websites) > 1 {
					return m.tr("dashboard.website.dragName") + " · " + m.tr("dashboard.website.contextMenu")
				}
				return m.tr("dashboard.website.contextMenu")
			}
			menu := m.tr("dashboard.website.menuKey")
			if m.navigationOptions().websiteTestsAvailable() && !m.websiteTests.rows[site.URL].pending() {
				action := "Enter " + m.tr("dashboard.website.testSite", clean(site.Name))
				if id == m.hover {
					action = m.tr("dashboard.website.clickTest", clean(site.Name))
				}
				return action + " · " + menu
			}
			return menu
		}
	}
	return ""
}

func (m *Model) overviewContextHelp() string {
	if hint := m.overviewWebsiteHelp(); hint != "" {
		return hint
	}
	if m.focus == "ip.refresh" {
		if m.busy || m.ipInfo.loading || !m.snapshotReady || !overviewProxyAvailable(m.snapshot) {
			return m.tr("navigation.controls.browse")
		}
		return m.controlActionHelp(m.tr("header.refresh"))
	}
	if strings.HasPrefix(m.focus, "mode.") {
		return m.tr("navigation.overview.modeChoice")
	}
	if strings.HasPrefix(m.focus, "overview.card.") {
		if m.overviewCardHasControls() {
			return m.tr("overview.cardHint")
		}
		return m.tr("navigation.overview.readOnlyCard")
	}
	if m.focus == "core.manage" {
		if !m.snapshotReady || !m.snapshot.Installed || m.quitting {
			return m.tr("navigation.controls.browse")
		}
		return m.controlActionHelp(m.tr("navigation.controls.manageCore"))
	}
	if control, ok := m.navigationOptions().overviewSwitch(m.focus); ok {
		if !control.Enabled() {
			return m.tr("navigation.controls.browse")
		}
		var off, on string
		switch m.focus {
		case "core.toggle":
			off, on = "dashboard.settings.stop", "dashboard.settings.start"
		case "terminal.toggle":
			off, on = "navigation.controls.disableTerminal", "navigation.controls.enableTerminal"
		case "tun.toggle":
			off, on = "navigation.controls.disableTUN", "navigation.controls.reviewTUN"
		}
		if control.On {
			return m.controlActionHelp(m.tr(off))
		}
		return m.controlActionHelp(m.tr(on))
	}
	return m.settingsContextHelp()
}

// Render only the focused card to reuse its actual enabled controls, without
// rebuilding the complete Overview layout merely to produce a key hint.
func (m *Model) overviewCardHasControls() bool {
	var render func(dashboardOptions, int) dashboardBlock
	paired := true
	switch m.focus {
	case "overview.card.dashboard.control.core":
		render = overviewCoreCard
	case "overview.card.dashboard.profile.title":
		render = overviewProfileCard
	case "overview.card.dashboard.overview.network":
		render = overviewNetworkCard
	case "overview.card.dashboard.control.routingMode":
		render = overviewModeCard
	case "overview.card.dashboard.website.title":
		render = overviewWebsiteCard
	case "overview.card.dashboard.overview.ipInfo":
		render = overviewIPCard
	case "overview.card.dashboard.guide.title":
		render, paired = getStarted, false
	default:
		return false
	}
	o := m.navigationOptions()
	width := dashboardBodyWidth(o)
	if paired && width >= 90 {
		width = (width - 2) / 2
	}
	return len(render(o, width).targets) > 0
}

func (m *Model) settingsContextHelp() string {
	var label string
	if strings.HasPrefix(m.focus, "settings.tab.") {
		return m.tr("settings.tabHelp")
	}
	if m.page == "settings" {
		hint := ""
		switch m.focus {
		case "settings.ipv6":
			if m.snapshot.TUN {
				hint = "settings.tunIPv6"
			}
		case "settings.geoAutoUpdate", "settings.geoUpdateInterval":
			hint = "settings.geoRestart"
		case "settings.storeFakeIP":
			hint = "settings.fakeIPHint"
		case "settings.allowLAN":
			hint = "settings.allowLANHint"
		case "settings.etagSupport":
			hint = "settings.etagHint"
		case "settings.findProcessMode":
			hint = "settings.processHint"
		}
		if hint != "" {
			return m.controlActionHelp(m.tr(m.focus)) + " · " + m.tr(hint)
		}
	}
	switch m.focus {
	case "settings.copyProxyAddress":
		return m.controlActionHelp(m.tr("settings.copyProxyAddress"))
	case "settings.bindAddress", "settings.accessRules", "settings.externalControl", "settings.proxyAuth", "settings.keepAliveIdle", "settings.keepAliveInterval", "settings.geoUpdateInterval":
		return m.controlActionHelp(m.tr("settings.editValue", m.tr(m.focus)))
	case "settings.language":
		label = "settings.chooseLanguage"
	case "settings.startPage":
		label = "settings.chooseStartPage"
	case "settings.theme":
		label = "settings.chooseTheme"
	case "editor.configure":
		label = "settings.editEditor"
	case "settings.ipv6", "settings.unifiedDelay", "settings.tcpConcurrent", "settings.tcpKeepAlive", "settings.etagSupport":
		return m.controlActionHelp(m.tr("settings.toggleCoreSetting", m.tr(m.focus)))
	case "settings.logLevel", "settings.findProcessMode", "settings.geodataLoader":
		return m.controlActionHelp(m.tr("settings.chooseCoreSetting", m.tr(m.focus)))
	case "key-hints.toggle":
		if m.keyHints.pending {
			return m.tr("settings.coreSaving", m.tr("dashboard.interface.keyHints"))
		}
		label = "navigation.controls.hideHints"
		if m.keyHints.hidden {
			label = "navigation.controls.showHints"
		}
	case "runtime-settings":
		label = "navigation.controls.editPorts"
	case "core.checkUpdate":
		label = "core_update.check"
	case "network.primaryConfig", "network.tunConfig":
		label = "dashboard.network.configure"
	case "card.profiles":
		label = "navigation.controls.openProfiles"
	case "card.proxies":
		label = "navigation.controls.openProxies"
	case overviewRouteGroupTarget:
		label = "navigation.controls.viewGroup"
	case "network":
		label = "settings.downloadSettings"
	case "install", "core.install", "card.install":
		label = "dashboard.action.install"
		if m.snapshot.Installed {
			label = "navigation.controls.manageCore"
		}
	case "import", "add":
		label = "navigation.controls.importProfile"
	case "start", "card.start", "core.start":
		label = "dashboard.settings.start"
	case "stop", "core.stop":
		label = "dashboard.settings.stop"
	case "restart", "core.restart":
		label = "dashboard.action.restart"
	case autostartToggleTarget:
		label = autostartActionLabel(m.snapshot.Service.Enabled)
	case "terminal.activation":
		label = "terminal.activationAction"
	}
	if strings.HasPrefix(m.focus, "mode.") {
		label = "navigation.controls.applyMode"
	}
	if label != "" {
		return m.controlActionHelp(m.tr(label))
	}
	return m.tr("navigation.controls.browse")
}
