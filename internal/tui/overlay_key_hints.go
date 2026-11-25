package tui

import (
	"strings"

	"github.com/nelvko/lazyclash/internal/i18n"
)

func (m *Model) notificationContextHelp() string {
	action := "details"
	switch {
	case m.focus == "notification.expand":
		action = "list"
	case strings.HasPrefix(m.focus, "notification.close."):
		action = "dismiss"
	case strings.HasPrefix(m.focus, "notification.cancel."):
		action = "cancel"
	}
	return m.tr("notification.hints."+action) + " · " + m.tr("notification.hints.controls")
}

func (p notificationPanel) contextHelp(locale i18n.Localizer) string {
	if p.detailID != 0 {
		return locale.T("notification.hints.detail")
	}
	if p.focus < 0 || p.focus >= len(p.actions) {
		return locale.T("notification.hints.empty")
	}
	action := "details"
	id := p.actions[p.focus].ID
	if strings.HasPrefix(id, "notification.panel.dismiss.") {
		action = "dismiss"
	} else if strings.HasPrefix(id, "notification.cancel.") {
		action = "cancel"
	}
	return locale.T("notification.hints."+action) + " · " + locale.T("notification.hints.listControls")
}

func (m *Model) portsContextHelp() string {
	if m.busy {
		return m.tr("ports.busy_help")
	}
	label := ""
	switch m.ports.focus {
	case "ports.mixed", "ports.controller", "ports.review":
		label = m.tr("ports.review")
		if m.ports.kind == "terminal" {
			label = m.tr("network.terminalConfig.save")
		}
	case "ports.cancel", "ports.done", "ports.back":
		label = m.tr(m.ports.focus)
		if m.ports.kind == "tun" && !m.canConfigureHostTUN() {
			label = m.tr("profiles.close")
		}
	case "tun.policy":
		label = m.tr("network.tunConfig.toggle")
	case "tun.save":
		label = m.tr("profile_settings.save")
	case "ports.apply":
		label = m.tr("ports.apply")
		if !m.ports.plan.Changed {
			label = m.tr("ports.keep_current")
		} else if m.ports.plan.Active {
			label = m.tr("ports.apply_restart")
		}
	case "ports.retry":
		label = m.tr("ports.review_retry")
		if m.ports.step == "load-error" {
			label = m.tr("ports.retry")
		}
	default:
		if strings.HasSuffix(m.ports.focus, ".clear") {
			label = m.tr("navigation.hints.clear")
		}
	}
	if label == "" {
		return m.tr("navigation.hints.browse")
	}
	return m.tr("navigation.hints.control", label)
}

func (m *Model) profileDeleteContextHelp() string {
	label := m.tr("navigation.hints.continue")
	switch m.profilesUI.deleteFocus {
	case "profile-delete.cancel":
		label = m.tr("profiles.create_cancel")
	case deleteChoice:
		label = m.tr("profiles.choose_replacement")
	case "profile-delete.confirm":
		label = m.tr("profiles.delete_confirm")
	}
	return m.tr("navigation.hints.control", label)
}
