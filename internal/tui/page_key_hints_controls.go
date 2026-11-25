package tui

import "strings"

func (m *Model) controlActionHelp(label string) string {
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
		if m.hasRefreshableProfiles() {
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

func (m *Model) overviewContextHelp() string {
	if strings.HasPrefix(m.focus, "overview.card.") {
		if m.overviewCardHasControls() {
			return m.tr("overview.cardHint")
		}
		return m.tr("navigation.overview.readOnlyCard")
	}
	if m.focus == "core.toggle" {
		control := overviewCoreToggle(m.navigationOptions())
		if m.busy || !control.canToggle() {
			return m.tr("navigation.controls.browse")
		}
		action := "dashboard.settings.start"
		if control.state == toggleOn {
			action = "dashboard.settings.stop"
		}
		return m.controlActionHelp(m.tr(action))
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
	switch m.focus {
	case "settings.language":
		label = "settings.chooseLanguage"
	case "settings.theme":
		label = "settings.chooseTheme"
	case "settings.ipv6", "settings.unifiedDelay", "settings.logLevel":
		return m.controlActionHelp(m.tr("settings.chooseCoreSetting", m.tr(m.focus)))
	case "key-hints.toggle":
		label = "navigation.controls.hideHints"
		if m.keyHints.hidden {
			label = "navigation.controls.showHints"
		}
	case "runtime-settings":
		label = "navigation.controls.editPorts"
	case "network.primaryConfig", "network.tunConfig":
		label = "dashboard.network.configure"
	case "card.profiles":
		label = "navigation.controls.openProfiles"
	case "card.proxies":
		label = "navigation.controls.openProxies"
	case "card.logs":
		label = "navigation.controls.openLogs"
	case "network":
		return m.tr("navigation.network")
	case "install", "core.install", "card.install":
		label = "dashboard.action.install"
		if m.snapshot.Installed {
			label = "navigation.controls.manageCore"
		}
	case "import", "add", "subscription.add":
		label = "navigation.controls.importProfile"
	case "start", "card.start", "core.start":
		label = "dashboard.settings.start"
	case "stop", "core.stop":
		label = "dashboard.settings.stop"
	case "restart", "core.restart":
		label = "dashboard.action.restart"
	case "autostart-enable":
		label = "dashboard.settings.enableAutostart"
	case "autostart-disable":
		label = "dashboard.settings.disableAutostart"
	case "tun.enable":
		label = "navigation.controls.reviewTUN"
	case "tun.disable":
		label = "navigation.controls.disableTUN"
	case "terminal.enable":
		label = "navigation.controls.enableTerminal"
	case "terminal.disable":
		label = "navigation.controls.disableTerminal"
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
