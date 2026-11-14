package tui

// Live pages share the confirmed service state. A stopped core is not a failed
// connection, even while the monitor stays subscribed to observe its next start.
func (m Model) corePageState() (state, label string) {
	if !m.snapshotReady {
		return "", ""
	}
	if !m.snapshot.Installed {
		return "uninstalled", m.tr("runtime.status.not_installed")
	}
	if m.snapshot.ServiceError != "" {
		label, _ = coreSummary(m.snapshot, m.locale)
		return "unavailable", label
	}
	switch m.snapshot.Service.State {
	case "activating":
		return "transition", m.tr("runtime.status.starting")
	case "deactivating":
		return "transition", m.tr("runtime.status.stopping")
	case "reloading":
		return "transition", m.tr("dashboard.status.reloading")
	case "failed":
		if !m.snapshot.Service.Active {
			return "stopped", m.tr("runtime.status.failed")
		}
	case "inactive":
		if !m.snapshot.Service.Active {
			return "stopped", m.tr("runtime.status.stopped")
		}
	case "unknown", "unsupported":
		label, _ = coreSummary(m.snapshot, m.locale)
		return "unavailable", label
	}
	return "", ""
}

func (m Model) corePageAction() (id, label string) {
	state, _ := m.corePageState()
	switch state {
	case "uninstalled":
		return "card.install", m.tr("dashboard.action.install")
	case "stopped":
		return "core.start", m.tr("dashboard.action.start")
	}
	return "", ""
}
