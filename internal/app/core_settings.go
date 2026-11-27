package app

import (
	"context"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// Mihomo starts its GEO updater once per process. Remember the schedule that
// process started with across hot reloads, including edits that undo a change.
type runningGeoSchedule struct {
	Process    reloadProcess `json:"process"`
	AutoUpdate string        `json:"auto_update"`
	Interval   string        `json:"interval"`
}

func (s *runningGeoSchedule) pending(status service.Status, desired mihomo.GeneralSettings) bool {
	return s != nil && status.Active && s.Process == profileProcess(status) &&
		(s.AutoUpdate != desired.GeoAutoUpdate || s.AutoUpdate == "on" && s.Interval != desired.GeoUpdateInterval)
}

// SetCoreSetting updates one instance preference under the lock so concurrent
// edits to different settings compose.
func (a *App) SetCoreSetting(ctx context.Context, key, value string) error {
	return a.locked(ctx, func() error {
		m, target, err := a.load()
		if err != nil {
			return err
		}
		next, err := m.CoreSettings.With(key, value)
		if err != nil {
			return err
		}
		if next == m.CoreSettings {
			return nil
		}
		deployed, err := a.readGenerationConfig(target, m.SHA)
		if err != nil {
			return err
		}
		previous := m.CoreSettings
		m.CoreSettings = next
		cfg, err := mihomo.WithGeneralSettings(deployed, next, m.TUN)
		if err != nil {
			return err
		}
		var candidate *geoCandidate
		if digest(cfg) != m.SHA {
			bin, _, _ := a.paths()
			candidate, err = a.prepareGeoCandidate(ctx, bin, m, cfg)
			if err != nil {
				return err
			}
			defer candidate.Close()
		}
		if previous.GeoAutoUpdate != next.GeoAutoUpdate || previous.GeoUpdateInterval != next.GeoUpdateInterval {
			status, err := a.profileServiceStatus(ctx)
			if err != nil {
				return err
			}
			if !m.GeoScheduleAtStart.pending(status, previous) {
				m.GeoScheduleAtStart = &runningGeoSchedule{profileProcess(status), previous.GeoAutoUpdate, previous.GeoUpdateInterval}
			}
			if !m.GeoScheduleAtStart.pending(status, next) {
				m.GeoScheduleAtStart = nil
			}
			return a.publishProfileGeneration(ctx, target, m, cfg, status, true, candidate)
		}
		return a.publishProfileRefreshCandidate(ctx, target, m, cfg, candidate)
	})
}
