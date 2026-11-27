package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

const geoMaintenanceID = "settings.geoMaintenance"

func geoMaintenanceRow(o dashboardOptions) settingsRow {
	s := o.Snapshot.Service
	key := "geo.action.prepare"
	if s.Active {
		key = "geo.action.update"
	}
	if o.geoMaintenanceBusy {
		key = "geo.action.cancelPreparation"
		if s.Active {
			key = "geo.action.stopWaiting"
		}
	}
	available := s.State == "active" && s.Active || !s.Active && s.PID == 0 && (s.State == "inactive" || s.State == "failed")
	return settingsRow{label: o.Locale.T(key), control: settingsLink, actions: []overviewControlButton{{
		id: geoMaintenanceID, label: settingsDetailChevron, enabled: o.Snapshot.Installed && (o.geoMaintenanceBusy || !o.Busy && available),
	}}}
}

func (m *Model) manageGeo() tea.Cmd {
	if m.busy {
		if m.notification.operation.Key == geoMaintenanceID && m.cancel != nil {
			m.cancel()
		}
		return nil
	}
	if m.quitting || !m.snapshotReady || !m.snapshot.Installed {
		return nil
	}
	backend, locale, network := m.backend, m.locale, m.wizard.cfg
	running := m.snapshot.Service.Active
	label := locale.T("geo.action.prepare")
	if running {
		label = locale.T("geo.action.update")
	}
	return m.beginResult(label, func(ctx context.Context) (string, error) {
		if running {
			if err := backend.UpdateActiveGeo(ctx); err != nil {
				var issue *diagnostic.Error
				if errors.As(err, &issue) && issue.Code == "diagnostic.geo.updateWaitStopped" {
					return locale.T("geo.result.stoppedWaiting"), err
				}
				return "", err
			}
			return locale.T("geo.result.checked"), nil
		}
		result, err := backend.PrepareDefaultGeo(ctx, network, nil)
		return result.Summary(locale), err
	}, notificationOperation{Key: geoMaintenanceID, Context: label})
}
