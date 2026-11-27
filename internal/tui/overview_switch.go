package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func (o dashboardOptions) overviewSwitch(id string) (Switch, bool) {
	switch id {
	case "core.toggle":
		control, _ := o.coreStatus()
		return control, true
	case "terminal.toggle":
		return o.terminalSwitch(), true
	case "tun.toggle":
		control, _ := o.tunSwitch()
		return control, true
	default:
		return Switch{}, false
	}
}

func (m *Model) applyOverviewSwitch(id string, on bool) tea.Cmd {
	switch id {
	case "core.toggle":
		return m.setCoreRunning(on)
	case "terminal.toggle":
		return m.terminalProxyAction(on)
	case "tun.toggle":
		return m.tunAction(on)
	}
	return nil
}

func (m *Model) setCoreRunning(on bool) tea.Cmd {
	action := "stop"
	if on {
		action = "start"
	}
	backend, locale := m.backend, m.locale
	return m.begin(m.tr("model.service_"+action), m.tr("model.service_"+action+"_done"), func(ctx context.Context) error {
		// Inspect again inside the asynchronous operation. The fixed start/stop
		// intent must never invert a newer external state change.
		fresh, err := backend.Snapshot(ctx)
		if err != nil {
			return err
		}
		observed, _ := (dashboardOptions{Snapshot: fresh, Locale: locale}).coreStatus()
		if !observed.Enabled() {
			return diagnostic.New("diagnostic.core.toggleChanged", "core status is unavailable or changing; wait for a confirmed state before toggling")
		}
		if observed.On == on {
			return nil
		}
		return backend.Control(ctx, action)
	}, notificationOperation{Key: "service." + action, Context: m.tr("notification.service." + action)})
}
