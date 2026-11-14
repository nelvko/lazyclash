package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// The control is derived from the same observed service snapshot as the card.
// A foreground lifecycle request supplies only its pending presentation.
func overviewCoreToggle(o dashboardOptions) (toggleModel, toggleLabels) {
	tr := o.Locale.T
	labels := toggleLabels{
		On: tr("dashboard.core.toggle.on"), Off: tr("dashboard.core.toggle.off"),
		Pending:     tr("dashboard.core.toggle.starting"),
		Unknown:     tr("dashboard.core.toggle.unknown"),
		Unavailable: tr("dashboard.core.toggle.unavailable"), Unsupported: tr("dashboard.core.toggle.unavailable"),
	}
	s := o.Snapshot
	state, reason := toggleUnknown, ""
	switch {
	case !s.Installed:
		state = toggleUnavailable
	case o.CoreAction == "start" || o.CoreAction == "stop" || o.CoreAction == "restart":
		state = togglePending
		switch o.CoreAction {
		case "stop":
			labels.Pending = tr("dashboard.core.toggle.stopping")
		case "restart":
			labels.Pending = tr("dashboard.core.toggle.restarting")
		}
	case s.ServiceError != "" || runtimeStopIssue(s, o.Locale) != "":
		state, reason = toggleUnavailable, runtimeControlIssue(s, o.Locale)
	case s.Service.State == "activating":
		state = togglePending
	case s.Service.State == "deactivating":
		state, labels.Pending = togglePending, tr("dashboard.core.toggle.stopping")
	case s.Service.State == "reloading":
		state, labels.Pending = togglePending, tr("dashboard.core.toggle.restarting")
	case s.Service.State == "active" && s.Service.Active:
		state = toggleOn
	case (s.Service.State == "inactive" || s.Service.State == "failed") && !s.Service.Active:
		state = toggleOff
	}
	var control toggleModel
	control.setState(state, reason)
	return control, labels
}

func overviewCoreToggleWidth(o dashboardOptions, labels toggleLabels) int {
	width := 0
	for _, state := range []toggleState{toggleOn, toggleOff, togglePending, toggleUnknown, toggleUnavailable} {
		face, _ := labels.face(state)
		width = max(width, ansi.StringWidth(face))
	}
	for _, key := range []string{"starting", "stopping", "restarting"} {
		width = max(width, ansi.StringWidth("◌ "+o.Locale.T("dashboard.core.toggle."+key)))
	}
	return width
}

func (m *Model) coreToggleKey(key tea.KeyPressMsg) tea.Cmd {
	control, _ := overviewCoreToggle(m.navigationOptions())
	if _, ok := control.intent(key, m.page == "overview" && m.focus == "core.toggle"); !ok {
		return nil
	}
	return m.coreToggleAction()
}

func (m *Model) coreToggleAction() tea.Cmd {
	if m.busy || m.quitting || m.page != "overview" {
		return nil
	}
	control, _ := overviewCoreToggle(m.navigationOptions())
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
		observed, _ := overviewCoreToggle(dashboardOptions{Snapshot: fresh, Locale: locale})
		if !observed.canToggle() {
			return diagnostic.New("diagnostic.core.toggleChanged", "core status is unavailable or changing; wait for a confirmed state before toggling")
		}
		if observed.state == desired {
			return nil
		}
		return backend.Control(ctx, action)
	}, notificationOperation{Key: "service." + action, Context: m.tr("notification.service." + action)})
}
