package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// The control is derived from the same observed service snapshot as the card.
// A foreground lifecycle request supplies only its pending presentation.
func overviewCoreToggle(o dashboardOptions) toggleModel {
	s := o.Snapshot
	state, reason := toggleUnknown, ""
	switch {
	case o.StateUnavailable || !s.Installed:
		state = toggleUnavailable
	case o.CoreAction == "start" || o.CoreAction == "stop" || o.CoreAction == "restart":
		state = togglePending
	case s.ServiceError != "" || runtimeStopIssue(s, o.Locale) != "":
		state, reason = toggleUnavailable, runtimeControlIssue(s, o.Locale)
	case s.Service.State == "activating" || s.Service.State == "deactivating" || s.Service.State == "reloading":
		state = togglePending
	case s.Service.State == "active" && s.Service.Active:
		state = toggleOn
	case (s.Service.State == "inactive" || s.Service.State == "failed") && !s.Service.Active:
		state = toggleOff
	}
	var control toggleModel
	control.setState(state, reason)
	return control
}

func (m *Model) coreToggleKey(key tea.KeyPressMsg) tea.Cmd {
	control := overviewCoreToggle(m.navigationOptions())
	if _, ok := control.intent(key, m.page == "overview" && m.focus == "core.toggle"); !ok {
		return nil
	}
	return m.coreToggleAction()
}

func (m *Model) coreToggleAction() tea.Cmd {
	if m.busy || m.quitting || m.page != "overview" {
		return nil
	}
	control := overviewCoreToggle(m.navigationOptions())
	if !control.canToggle() {
		return nil
	}
	action, desired := "start", toggleOn
	if control.state == toggleOn {
		action, desired = "stop", toggleOff
	}
	backend, locale := m.backend, m.locale
	return m.begin(m.tr("model.service_"+action), m.tr("model.service_"+action+"_done"), func(ctx context.Context) error {
		// Inspect again inside the asynchronous operation. The fixed start/stop
		// intent must never invert a newer external state change.
		fresh, err := backend.Snapshot(ctx)
		if err != nil {
			return err
		}
		observed := overviewCoreToggle(dashboardOptions{Snapshot: fresh, Locale: locale})
		if !observed.canToggle() {
			return diagnostic.New("diagnostic.core.toggleChanged", "core status is unavailable or changing; wait for a confirmed state before toggling")
		}
		if observed.state == desired {
			return nil
		}
		return backend.Control(ctx, action)
	}, notificationOperation{Key: "service." + action, Context: m.tr("notification.service." + action)})
}
