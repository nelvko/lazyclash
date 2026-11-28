package tui

import (
	"strings"

	"github.com/nelvko/lazyclash/internal/app"
)

// A resource suffix identifies the item to update on retry. Delivery depends
// on the operation itself and where its result can already be seen.
func (op notificationOperation) announcesSuccess(page string) bool {
	action, _, _ := strings.Cut(op.Key, ":")
	switch action {
	case geoMaintenanceID:
		return true
	case "profile.apply", "profile.deactivate", "profile.add", "profile.create",
		"profile.delete", "profile.refresh", "profile.refresh-all", "service.restart", "terminal-proxy", "settings.copyProxyAddress":
		return true
	case "service.start", "service.stop", "tun":
		return page != "overview"
	case "install", "upgrade", "ports-apply":
		return page != op.Page
	default:
		return false
	}
}

// The command has succeeded. Use its own follow-up snapshot to distinguish a
// confirmed change from a saved setting or an unconfirmed live selection.
func (m *Model) controlCompletion(s app.Snapshot, message string) (string, bool) {
	key := m.notification.operation.Key
	switch {
	case strings.HasPrefix(key, "profile.add:"), strings.HasPrefix(key, "profile.create:"):
		if !s.Installed {
			return m.tr("feedback.profile_install_to_use", message), false
		}
		return m.tr("feedback.profile_ready_to_use", message), false
	case key == "terminal-proxy" && pendingActivationCommand(s.TerminalProxy) != "":
		return m.tr("terminal.savedActivation"), true
	case key == "tun":
		switch {
		case s.ServiceError == "" && !s.Service.Active && (s.Service.State == "inactive" || s.Service.State == "failed"):
			return m.tr("tun.savedForStart"), false
		case s.ServiceError != "" || s.APIError != "" || s.TUNError != "" || s.TUNActual == nil || *s.TUNActual != s.TUN:
			return m.tr("tun.savedUnconfirmed"), true
		case s.TUN:
			return m.tr("tun.enabled"), false
		default:
			return m.tr("tun.disabled"), false
		}
	case strings.HasPrefix(key, "mode."):
		requested := strings.TrimPrefix(key, "mode.")
		if s.Mode.Desired != requested || s.ServiceError != "" || (s.Service.Active && (s.Mode.Error != "" || s.APIError != "" || s.Mode.Actual != requested)) {
			return m.tr("notification.modeUnconfirmed"), true
		}
	case strings.HasPrefix(key, "node.select:") && m.proxies.pendingGroup != "":
		if s.Service.Active && s.APIError == "" && s.ServiceError == "" {
			for _, group := range s.Groups {
				if group.Name == m.proxies.pendingGroup && group.Now == m.proxies.pendingNode {
					return message, false
				}
			}
		}
		return m.tr("notification.nodeUnconfirmed"), true
	}
	return message, false
}
